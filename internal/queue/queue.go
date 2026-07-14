package queue

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Status represents the lifecycle state of a job.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusRetrying  Status = "retrying"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// ErrQueueFull is returned by Enqueue when the queue's buffer is full.
var ErrQueueFull = errors.New("queue: full")

// Job describes a repository that needs to be mirrored.
type Job struct {
	ID        string
	Owner     string
	Repo      string
	Branch    string
	Commit    string
	Attempts  int
	CreatedAt time.Time
}

// Record tracks the current status of a job.
type Record struct {
	Job       Job
	Status    Status
	Error     string
	UpdatedAt time.Time
}

// Queue is a buffered FIFO queue of jobs with in-memory status tracking.
type Queue struct {
	jobs chan Job

	mu     sync.RWMutex
	status map[string]*Record
	nextID uint64
}

// New creates a Queue with the given buffer size.
func New(bufferSize int) *Queue {
	return &Queue{
		jobs:   make(chan Job, bufferSize),
		status: make(map[string]*Record),
	}
}

// Enqueue adds a job to the queue, assigning it an ID and pending status if
// it doesn't already have one. Returns ErrQueueFull if the queue's buffer is
// full.
func (q *Queue) Enqueue(job Job) error {
	if job.ID == "" {
		job.ID = q.generateID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}

	select {
	case q.jobs <- job:
		q.setStatus(job, StatusPending, "")
		return nil
	default:
		q.setStatus(job, StatusFailed, ErrQueueFull.Error())
		return ErrQueueFull
	}
}

func (q *Queue) generateID() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	return fmt.Sprintf("job-%d-%d", time.Now().UnixNano(), q.nextID)
}

func (q *Queue) setStatus(job Job, status Status, errMsg string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.status[job.ID] = &Record{
		Job:       job,
		Status:    status,
		Error:     errMsg,
		UpdatedAt: time.Now(),
	}
}

// Status returns the current status record for the job with the given ID.
func (q *Queue) Status(id string) (Record, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	rec, ok := q.status[id]
	if !ok {
		return Record{}, false
	}
	return *rec, true
}

// Statuses returns a snapshot of all known job status records.
func (q *Queue) Statuses() []Record {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]Record, 0, len(q.status))
	for _, rec := range q.status {
		out = append(out, *rec)
	}
	return out
}
