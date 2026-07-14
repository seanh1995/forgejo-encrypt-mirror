package queue

import (
	"context"
	"testing"
	"time"
)

func TestPruneOlderThanRemovesOldTerminalJobs(t *testing.T) {
	q := New(10)

	q.status["old-succeeded"] = &Record{
		Job:       Job{ID: "old-succeeded"},
		Status:    StatusSucceeded,
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}
	q.status["old-failed"] = &Record{
		Job:       Job{ID: "old-failed"},
		Status:    StatusFailed,
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}
	q.status["recent-succeeded"] = &Record{
		Job:       Job{ID: "recent-succeeded"},
		Status:    StatusSucceeded,
		UpdatedAt: time.Now(),
	}
	q.status["old-pending"] = &Record{
		Job:       Job{ID: "old-pending"},
		Status:    StatusPending,
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}
	q.status["old-running"] = &Record{
		Job:       Job{ID: "old-running"},
		Status:    StatusRunning,
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}

	removed := q.PruneOlderThan(time.Hour)
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}

	if _, ok := q.Status("old-succeeded"); ok {
		t.Fatal("expected old-succeeded to be pruned")
	}
	if _, ok := q.Status("old-failed"); ok {
		t.Fatal("expected old-failed to be pruned")
	}
	if _, ok := q.Status("recent-succeeded"); !ok {
		t.Fatal("expected recent-succeeded to survive pruning")
	}
	if _, ok := q.Status("old-pending"); !ok {
		t.Fatal("expected old-pending to survive pruning regardless of age")
	}
	if _, ok := q.Status("old-running"); !ok {
		t.Fatal("expected old-running to survive pruning regardless of age")
	}
}

func TestStartCleanupPrunesPeriodically(t *testing.T) {
	q := New(10)
	q.status["old-succeeded"] = &Record{
		Job:       Job{ID: "old-succeeded"},
		Status:    StatusSucceeded,
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.StartCleanup(ctx, 10*time.Millisecond, time.Hour)

	deadline := time.After(time.Second)
	for {
		if _, ok := q.Status("old-succeeded"); !ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("expected background cleanup to prune old-succeeded within timeout")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
