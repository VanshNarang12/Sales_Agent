package jobs

import (
	"context"
	"fmt"
	"time"
)

// HandlerFunc processes one claimed job. A nil return marks the job done; an
// error schedules a retry (or terminal failure on the last attempt).
type HandlerFunc func(ctx context.Context, job *Job) error

// Worker drains the queue. Wake-ups: the Queue's nudge channel (instant, for
// jobs enqueued in this process) and a ticker (user decision 2026-10-02: every
// 5 minutes) that catches retries, crash leftovers, and other instances' jobs.
type Worker struct {
	queue    *Queue
	handlers map[string]HandlerFunc
	interval time.Duration
	stale    time.Duration
}

func NewWorker(q *Queue) *Worker {
	return &Worker{
		queue:    q,
		handlers: map[string]HandlerFunc{},
		interval: 5 * time.Minute,
		stale:    5 * time.Minute,
	}
}

// Register wires a handler for one job kind. Call before Run.
func (w *Worker) Register(kind string, h HandlerFunc) { w.handlers[kind] = h }

// Run blocks until ctx is cancelled. Start it in its own goroutine.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.janitorAndDrain(ctx) // boot: pick up anything a previous process left behind
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.queue.Wake():
			w.drain(ctx)
		case <-ticker.C:
			w.janitorAndDrain(ctx)
		}
	}
}

func (w *Worker) janitorAndDrain(ctx context.Context) {
	if n, err := w.queue.resetStale(ctx, w.stale); err != nil {
		w.queue.log.Warn("jobs: janitor failed", "err", err)
	} else if n > 0 {
		w.queue.log.Info("jobs: requeued stale running jobs", "count", n)
	}
	w.drain(ctx)
}

// drain claims and runs jobs until nothing is due.
func (w *Worker) drain(ctx context.Context) {
	for {
		job, err := w.queue.claimOne(ctx)
		if err != nil {
			w.queue.log.Warn("jobs: claim failed", "err", err)
			return
		}
		if job == nil {
			return
		}
		w.runOne(ctx, job)
	}
}

func (w *Worker) runOne(ctx context.Context, job *Job) {
	h, ok := w.handlers[job.Kind]
	if !ok {
		_ = w.queue.fail(ctx, job, fmt.Errorf("no handler registered for kind %q", job.Kind))
		return
	}
	err := safeCall(ctx, h, job)
	if err != nil {
		w.queue.log.Warn("jobs: attempt failed", "kind", job.Kind, "id", job.ID,
			"attempt", job.Attempts, "err", err)
		if ferr := w.queue.fail(ctx, job, err); ferr != nil {
			w.queue.log.Warn("jobs: recording failure failed", "id", job.ID, "err", ferr)
		}
		return
	}
	if cerr := w.queue.complete(ctx, job.ID); cerr != nil {
		w.queue.log.Warn("jobs: marking done failed", "id", job.ID, "err", cerr)
	}
}

// safeCall keeps a panicking handler from killing the worker goroutine.
func safeCall(ctx context.Context, h HandlerFunc, job *Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return h(ctx, job)
}
