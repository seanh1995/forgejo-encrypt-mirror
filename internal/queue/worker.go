package queue

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/metrics"
)

// Handler processes a single job. Returning an error causes the job to be
// retried (up to a configured maximum) before being marked as permanently
// failed.
type Handler func(ctx context.Context, job Job) error

// Pool is a fixed-size pool of workers that pull jobs from a Queue and
// process them with a Handler, retrying on failure.
type Pool struct {
	queue      *Queue
	handler    Handler
	workers    int
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Option configures optional Pool behavior.
type Option func(*Pool)

// WithMaxRetries sets how many times a failed job is retried before being
// marked permanently failed. Defaults to 3.
func WithMaxRetries(n int) Option {
	return func(p *Pool) { p.maxRetries = n }
}

// WithRetryDelay sets the base delay used for exponential backoff between
// retries (attempt N waits baseDelay * 2^(N-1)). Defaults to 5 seconds.
func WithRetryDelay(d time.Duration) Option {
	return func(p *Pool) { p.baseDelay = d }
}

// WithMaxRetryDelay caps the exponential backoff delay. Defaults to 5
// minutes.
func WithMaxRetryDelay(d time.Duration) Option {
	return func(p *Pool) { p.maxDelay = d }
}

// NewPool creates a worker pool with the given number of workers that call
// handler for each job pulled from q.
func NewPool(q *Queue, workers int, handler Handler, opts ...Option) *Pool {
	if workers < 1 {
		workers = 1
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &Pool{
		queue:      q,
		handler:    handler,
		workers:    workers,
		maxRetries: 3,
		baseDelay:  5 * time.Second,
		maxDelay:   5 * time.Minute,
		ctx:        ctx,
		cancel:     cancel,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Start launches the worker goroutines.
func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// Stop signals all workers to stop and blocks until they (and any pending
// retries) have finished.
func (p *Pool) Stop() {
	p.cancel()
	p.wg.Wait()
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.queue.jobs:
			if !ok {
				return
			}
			p.process(id, job)
		}
	}
}

func (p *Pool) process(workerID int, job Job) {
	p.queue.setStatus(job, StatusRunning, "")

	start := time.Now()
	err := p.handler(p.ctx, job)
	if err == nil {
		metrics.JobDuration.Observe(time.Since(start).Seconds())
		metrics.JobsTotal.WithLabelValues("succeeded").Inc()
		p.queue.setStatus(job, StatusSucceeded, "")
		return
	}

	job.Attempts++
	if job.Attempts > p.maxRetries {
		log.Printf("worker %d: job %s failed permanently after %d attempts: %v", workerID, job.ID, job.Attempts, err)
		metrics.JobDuration.Observe(time.Since(start).Seconds())
		metrics.JobsTotal.WithLabelValues("failed").Inc()
		p.queue.setStatus(job, StatusFailed, err.Error())
		return
	}

	delay := p.backoffDelay(job.Attempts)
	log.Printf("worker %d: job %s failed (attempt %d/%d): %v, retrying in %s", workerID, job.ID, job.Attempts, p.maxRetries, err, delay)
	metrics.JobRetries.Inc()
	p.queue.setStatus(job, StatusRetrying, err.Error())

	p.scheduleRetry(job, delay)
}

// backoffDelay returns the exponential backoff delay for the given attempt
// number (1-indexed), capped at maxDelay.
func (p *Pool) backoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	// Guard against overflow from large attempt counts/shifts.
	shift := attempt - 1
	if shift > 30 {
		shift = 30
	}

	delay := p.baseDelay * time.Duration(1<<uint(shift))
	if delay <= 0 || delay > p.maxDelay {
		delay = p.maxDelay
	}
	return delay
}

func (p *Pool) scheduleRetry(job Job, delay time.Duration) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		select {
		case <-p.ctx.Done():
			return
		case <-time.After(delay):
		}

		select {
		case p.queue.jobs <- job:
		case <-p.ctx.Done():
		}
	}()
}
