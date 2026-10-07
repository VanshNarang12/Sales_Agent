// Package db provides the Postgres connection pool and the tenant-isolation helper.
// Tenant isolation is enforced by Postgres Row-Level Security; the app sets the
// current tenant on the transaction via a session GUC (app.tenant_id) so a missed
// WHERE clause in app code cannot leak across tenants.
// See migrations/0001_init.sql, ARCHITECTURE.md §13, coding_standards_techdoc.md §4.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db config: %w", err)
	}
	cfg.MinConns = 1
	cfg.HealthCheckPeriod = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return pool, nil
}

// WithTenantBatch pipelines fn's queued queries — with the RLS tenant GUC set
// first — into ONE wire round trip (vs WithTenantTx's 5: BEGIN/set/query/.../COMMIT),
// for latency-critical reads against a remote DB. All statements in a pgx batch
// run in a single implicit transaction (verified against the Neon pooler
// 2026-10-06: the transaction-local app.tenant_id is visible to later batch
// statements and gone afterwards), so RLS scoping is identical to WithTenantTx.
// fn must queue plain queries only — no BEGIN/COMMIT, which would break the
// single-transaction property. handle reads results in queue order; the
// set_config result is consumed here. If set_config fails, Postgres aborts the
// batch and no queued query runs (fail-closed).
func WithTenantBatch(ctx context.Context, pool *pgxpool.Pool, fn func(b *pgx.Batch), handle func(br pgx.BatchResults) error) error {
	tid, err := tenancy.MustFrom(ctx)
	if err != nil {
		return err
	}
	b := &pgx.Batch{}
	b.Queue("SELECT set_config('app.tenant_id', $1, true)", string(tid))
	fn(b)
	br := pool.SendBatch(ctx, b)
	defer br.Close()
	if _, err := br.Exec(); err != nil {
		return fmt.Errorf("set tenant: %w", err)
	}
	return handle(br)
}

// WithTenantTx runs fn inside a transaction whose RLS tenant context is set from ctx.
// Every customer-data query must go through a tenant-scoped transaction.
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tid, err := tenancy.MustFrom(ctx)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// SET LOCAL scopes the GUC to this transaction; RLS policies read it.
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", string(tid)); err != nil {
		return fmt.Errorf("set tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
