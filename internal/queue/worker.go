package queue

import (
	"context"
	"log"
	"sync"
	"time"
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
	retryDelay time.Duration

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

// WithRetryDelay sets the delay before a failed job is retried. Defaults to
// 5 seconds.
func WithRetryDelay(d time.Duration) Option {
	return func(p *Pool) { p.retryDelay = d }
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
		retryDelay: 5 * time.Second,
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

	err := p.handler(p.ctx, job)
	if err == nil {
		p.queue.setStatus(job, StatusSucceeded, "")
		return
	}

	job.Attempts++
	if job.Attempts > p.maxRetries {
		log.Printf("worker %d: job %s failed permanently after %d attempts: %v", workerID, job.ID, job.Attempts, err)
		p.queue.setStatus(job, StatusFailed, err.Error())
		return
	}

	log.Printf("worker %d: job %s failed (attempt %d/%d): %v, retrying", workerID, job.ID, job.Attempts, p.maxRetries, err)
	p.queue.setStatus(job, StatusRetrying, err.Error())

	p.scheduleRetry(job)
}

func (p *Pool) scheduleRetry(job Job) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		select {
		case <-p.ctx.Done():
			return
		case <-time.After(p.retryDelay):
		}

		select {
		case p.queue.jobs <- job:
		case <-p.ctx.Done():
		}
	}()
}
