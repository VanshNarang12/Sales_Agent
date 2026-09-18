package postcall

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/db"
)

// Store persists customers and call summaries (migrations/0003). Every method
// runs inside db.WithTenantTx, so the tenant comes from ctx and RLS filters
// every read/write.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// SaveSummary finds-or-creates the customer (case-insensitive name match within
// the org, so "Acme" and "acme" accumulate on one record) and inserts the
// summary row — one transaction, all-or-nothing. embedding may be nil (embed
// failure never blocks the save); the prep chat searches only embedded rows.
func (s *Store) SaveSummary(ctx context.Context, customerName, sessionID string, sum *Summary, embedding []float32) error {
	name := strings.TrimSpace(customerName)
	if name == "" {
		return fmt.Errorf("postcall save: empty customer name")
	}
	items, err := json.Marshal(sum.ActionItems)
	if err != nil {
		return fmt.Errorf("postcall save: %w", err)
	}
	unanswered, err := json.Marshal(sum.Unanswered)
	if err != nil {
		return fmt.Errorf("postcall save: %w", err)
	}
	err = db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		// The no-op DO UPDATE keeps the first-typed casing and makes RETURNING
		// yield the id on both the insert and the already-exists path.
		var customerID string
		if err := tx.QueryRow(ctx,
			`INSERT INTO customers (org_id, name)
			 VALUES (current_setting('app.tenant_id')::uuid, $1)
			 ON CONFLICT (org_id, lower(name)) DO UPDATE SET name = customers.name
			 RETURNING id`, name).Scan(&customerID); err != nil {
			return err
		}
		var vec *string
		if len(embedding) > 0 {
			v := vectorLiteral(embedding)
			vec = &v
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO call_summaries (org_id, customer_id, session_id, summary, action_items, unanswered, embedding)
			 VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4, $5, $6::vector)`,
			customerID, sessionID, sum.Summary, items, unanswered, vec)
		return err
	})
	if err != nil {
		return fmt.Errorf("postcall save: %w", err)
	}
	return nil
}

// vectorLiteral renders a vector in pgvector's text form: [0.1,0.2,...].
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", f)
	}
	b.WriteByte(']')
	return b.String()
}
