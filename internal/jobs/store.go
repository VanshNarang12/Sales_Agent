// Package jobs is the durable Postgres-backed job queue (migration 0010,
// post_call_techdoc.md §13b): INSERT = enqueue, FOR UPDATE SKIP LOCKED = claim,
// run_at + attempts = retry policy. No broker — see the techdoc for the
// trade-off table against BullMQ/RabbitMQ/Redis.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Job is one claimed unit of work.
type Job struct {
	ID          string
	OrgID       string
	Kind        string
	Payload     []byte
	Attempts    int
	MaxAttempts int
}

// Queue enqueues and claims jobs. The wake channel lets a same-process enqueuer
// nudge the worker instantly; the worker's ticker covers everything else.
type Queue struct {
	pool *pgxpool.Pool
	wake chan struct{}
	log  *slog.Logger
}

func NewQueue(pool *pgxpool.Pool, log *slog.Logger) *Queue {
	return &Queue{pool: pool, wake: make(chan struct{}, 1), log: log}
}

// Wake is the worker's instant-wakeup signal (buffered, never blocks enqueue).
func (q *Queue) Wake() <-chan struct{} { return q.wake }

// workerTx runs fn with the app.worker RLS escape hatch set (internal plumbing —
// product reads of jobs would go through db.WithTenantTx instead).
func (q *Queue) workerTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("jobs tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.worker', '1', true)"); err != nil {
		return fmt.Errorf("jobs guc: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Enqueue writes the job (durable from this moment) and nudges the worker.
func (q *Queue) Enqueue(ctx context.Context, orgID, kind string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jobs enqueue: %w", err)
	}
	err = q.workerTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO jobs (org_id, kind, payload) VALUES ($1, $2, $3)`, orgID, kind, body)
		return err
	})
	if err != nil {
		return fmt.Errorf("jobs enqueue: %w", err)
	}
	select {
	case q.wake <- struct{}{}:
	default: // worker already nudged — the drain loop will see this job too
	}
	return nil
}

// claimOne picks the oldest due job, marks it running, and returns it — or nil
// when nothing is due. SKIP LOCKED makes concurrent claimers never collide.
func (q *Queue) claimOne(ctx context.Context) (*Job, error) {
	var j Job
	err := q.workerTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			UPDATE jobs SET status = 'running', attempts = attempts + 1, updated_at = now()
			WHERE id = (
				SELECT id FROM jobs
				WHERE status = 'queued' AND run_at <= now()
				ORDER BY created_at
				FOR UPDATE SKIP LOCKED
				LIMIT 1
			)
			RETURNING id, org_id, kind, payload, attempts, max_attempts`)
		return row.Scan(&j.ID, &j.OrgID, &j.Kind, &j.Payload, &j.Attempts, &j.MaxAttempts)
	})
	if err == nil {
		return &j, nil
	}
	if isNoRows(err) {
		return nil, nil
	}
	return nil, fmt.Errorf("jobs claim: %w", err)
}

// complete marks a job done.
func (q *Queue) complete(ctx context.Context, id string) error {
	return q.workerTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'done', last_error = NULL, updated_at = now() WHERE id = $1`, id)
		return err
	})
}

// fail records the error and either schedules a retry (run_at backoff grows with
// attempts) or, out of attempts, parks the job as terminally failed.
func (q *Queue) fail(ctx context.Context, j *Job, jobErr error) error {
	return q.workerTx(ctx, func(tx pgx.Tx) error {
		if j.Attempts >= j.MaxAttempts {
			_, err := tx.Exec(ctx,
				`UPDATE jobs SET status = 'failed', last_error = $2, updated_at = now() WHERE id = $1`,
				j.ID, jobErr.Error())
			return err
		}
		backoff := time.Duration(j.Attempts) * 30 * time.Second
		_, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'queued', last_error = $2, run_at = now() + $3::interval, updated_at = now()
			 WHERE id = $1`,
			j.ID, jobErr.Error(), backoff.String())
		return err
	})
}

// resetStale is the janitor: a crashed worker leaves jobs stuck at 'running';
// anything running longer than staleAfter goes back to 'queued'.
func (q *Queue) resetStale(ctx context.Context, staleAfter time.Duration) (int64, error) {
	var n int64
	err := q.workerTx(ctx, func(tx pgx.Tx) error {
		res, err := tx.Exec(ctx,
			`UPDATE jobs SET status = 'queued', updated_at = now()
			 WHERE status = 'running' AND updated_at < now() - $1::interval`,
			staleAfter.String())
		n = res.RowsAffected()
		return err
	})
	return n, err
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
