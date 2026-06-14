// Package db provides the Postgres connection pool and the tenant-isolation helper.
// Tenant isolation is enforced by Postgres Row-Level Security; the app sets the
// current tenant on the transaction via a session GUC (app.tenant_id) so a missed
// WHERE clause in app code cannot leak across tenants.
// See migrations/0001_init.sql, ARCHITECTURE.md §13, coding_standards_techdoc.md §4.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)

// Connect opens a validated connection pool.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return pool, nil
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
