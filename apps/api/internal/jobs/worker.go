// Package jobs runs background work from the PostgreSQL job queue inside the API
// process (no separate queue infrastructure).
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/repository/postgres"
)

// Handler processes a job payload.
type Handler func(ctx context.Context, payload json.RawMessage) error

// Worker polls the jobs table and dispatches to handlers.
type Worker struct {
	store       *postgres.Store
	handlers    map[string]Handler
	log         *slog.Logger
	concurrency int
	poll        time.Duration
	id          string
	wake        chan struct{}
	wg          sync.WaitGroup
}

// NewWorker creates a worker.
func NewWorker(store *postgres.Store, handlers map[string]Handler, log *slog.Logger, concurrency int, poll time.Duration) *Worker {
	if concurrency <= 0 {
		concurrency = 1
	}
	if poll <= 0 {
		poll = 2 * time.Second
	}
	host, _ := os.Hostname()
	return &Worker{store: store, handlers: handlers, log: log, concurrency: concurrency, poll: poll,
		id: fmt.Sprintf("%s-%d", host, os.Getpid()), wake: make(chan struct{}, 1)}
}

// Notify wakes idle workers immediately (called after enqueueing).
func (w *Worker) Notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Start launches worker goroutines until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	// Recover jobs stranded by a previous crash.
	if n, err := w.store.RequeueStaleJobs(ctx, time.Now().Add(-10*time.Minute)); err == nil && n > 0 {
		w.log.Info("requeued stale jobs", "count", n)
	}
	for i := 0; i < w.concurrency; i++ {
		w.wg.Add(1)
		go w.loop(ctx, i)
	}
	w.wg.Add(1)
	go w.maintenance(ctx)
}

// Wait blocks until all workers stopped.
func (w *Worker) Wait() { w.wg.Wait() }

func (w *Worker) loop(ctx context.Context, n int) {
	defer w.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		worked, err := w.RunOnce(ctx)
		if err != nil {
			w.log.Error("job loop error", "worker", n, "error", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-time.After(w.poll):
		}
	}
}

// RunOnce claims and runs a single job. It returns whether a job was processed.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	job, err := w.store.ClaimJob(ctx, w.id)
	if err != nil || job == nil {
		return false, err
	}
	h, ok := w.handlers[job.Kind]
	if !ok {
		return true, w.store.FailJob(ctx, &postgres.Job{ID: job.ID, Attempts: job.MaxAttempts, MaxAttempts: job.MaxAttempts}, fmt.Errorf("no handler for job kind %q", job.Kind))
	}
	jctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	start := time.Now()
	err = func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return h(jctx, job.Payload)
	}()
	cancel()
	if err != nil {
		w.log.Warn("job failed", "job_id", job.ID, "kind", job.Kind, "attempt", job.Attempts, "error", err)
		return true, w.store.FailJob(context.WithoutCancel(ctx), job, err)
	}
	w.log.Debug("job completed", "job_id", job.ID, "kind", job.Kind, "duration_ms", time.Since(start).Milliseconds())
	return true, w.store.CompleteJob(context.WithoutCancel(ctx), job.ID)
}

// Drain runs jobs until the queue is empty (tests and CLI).
func (w *Worker) Drain(ctx context.Context, max int) (int, error) {
	n := 0
	for n < max {
		worked, err := w.RunOnce(ctx)
		if err != nil {
			return n, err
		}
		if !worked {
			return n, nil
		}
		n++
	}
	return n, nil
}

func (w *Worker) maintenance(ctx context.Context) {
	defer w.wg.Done()
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = w.store.RequeueStaleJobs(ctx, time.Now().Add(-25*time.Minute))
			_, _ = w.store.PurgeExpiredSessions(ctx)
			_, _ = w.store.MarkStaleRuns(ctx, time.Now().Add(-30*time.Minute))
		}
	}
}
