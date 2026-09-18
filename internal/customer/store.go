package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/db"
)

var ErrNotFound = errors.New("not found")

type Customer struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Meetings      int        `json:"meetings"`
	LastMeetingAt *time.Time `json:"last_meeting_at,omitempty"`
}

type SummaryMeta struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
}

type SummaryDetail struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customer_id"`
	SessionID   string    `json:"session_id"`
	CreatedAt   time.Time `json:"created_at"`
	Summary     string    `json:"summary"`
	ActionItems []string  `json:"action_items"`
	Unanswered  []string  `json:"unanswered"`
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListCustomers(ctx context.Context) ([]Customer, error) {
	var out []Customer
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT c.id, c.name, count(cs.id), max(cs.created_at)
			 FROM customers c
			 LEFT JOIN call_summaries cs ON cs.customer_id = c.id
			 GROUP BY c.id, c.name
			 ORDER BY max(cs.created_at) DESC NULLS LAST, c.created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Customer
			if err := rows.Scan(&c.ID, &c.Name, &c.Meetings, &c.LastMeetingAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("customer list: %w", err)
	}
	if out == nil {
		out = []Customer{}
	}
	return out, nil
}

func (s *Store) CreateCustomer(ctx context.Context, name string) (Customer, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Customer{}, fmt.Errorf("customer create: empty name")
	}
	var c Customer
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`INSERT INTO customers (org_id, name)
			 VALUES (current_setting('app.tenant_id')::uuid, $1)
			 ON CONFLICT (org_id, lower(name)) DO UPDATE SET name = customers.name
			 RETURNING id, name`, name).Scan(&c.ID, &c.Name)
	})
	if err != nil {
		return Customer{}, fmt.Errorf("customer create: %w", err)
	}
	return c, nil
}

func (s *Store) ListSummaries(ctx context.Context, customerID string) ([]SummaryMeta, error) {
	var out []SummaryMeta
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM customers WHERE id = $1)`, customerID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx,
			`SELECT id, session_id, created_at FROM call_summaries
			 WHERE customer_id = $1 ORDER BY created_at DESC`, customerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m SummaryMeta
			if err := rows.Scan(&m.ID, &m.SessionID, &m.CreatedAt); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("summary list: %w", err)
	}
	if out == nil {
		out = []SummaryMeta{}
	}
	return out, nil
}

// RecentSummaries: the newest n digests with full text — the guaranteed-recency
// part of the chat context (CHAT_RECENT_SUMMARIES).
func (s *Store) RecentSummaries(ctx context.Context, customerID string, n int) ([]SummaryDetail, error) {
	return s.querySummaries(ctx,
		`SELECT id, customer_id, session_id, created_at, summary, action_items, unanswered
		 FROM call_summaries WHERE customer_id = $1
		 ORDER BY created_at DESC LIMIT $2`, customerID, n)
}

// SearchSummaries: cosine search over this customer's embedded digests
// (CHAT_TOPK_SUMMARIES). Rows without an embedding are invisible here — the
// recency read still surfaces them.
func (s *Store) SearchSummaries(ctx context.Context, customerID string, queryVec []float32, topK int) ([]SummaryDetail, error) {
	return s.querySummaries(ctx,
		`SELECT id, customer_id, session_id, created_at, summary, action_items, unanswered
		 FROM call_summaries WHERE customer_id = $1 AND embedding IS NOT NULL
		 ORDER BY embedding <=> $2::vector LIMIT $3`, customerID, vectorLiteral(queryVec), topK)
}

func (s *Store) querySummaries(ctx context.Context, sql string, args ...any) ([]SummaryDetail, error) {
	var out []SummaryDetail
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d SummaryDetail
			var items, unanswered []byte
			if err := rows.Scan(&d.ID, &d.CustomerID, &d.SessionID, &d.CreatedAt, &d.Summary, &items, &unanswered); err != nil {
				return err
			}
			if err := json.Unmarshal(items, &d.ActionItems); err != nil {
				return err
			}
			if err := json.Unmarshal(unanswered, &d.Unanswered); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("summary query: %w", err)
	}
	return out, nil
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

func (s *Store) GetSummary(ctx context.Context, id string) (SummaryDetail, error) {
	var d SummaryDetail
	var items, unanswered []byte
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id, customer_id, session_id, created_at, summary, action_items, unanswered
			 FROM call_summaries WHERE id = $1`, id).
			Scan(&d.ID, &d.CustomerID, &d.SessionID, &d.CreatedAt, &d.Summary, &items, &unanswered)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SummaryDetail{}, ErrNotFound
	}
	if err != nil {
		return SummaryDetail{}, fmt.Errorf("summary get: %w", err)
	}
	if err := json.Unmarshal(items, &d.ActionItems); err != nil {
		return SummaryDetail{}, fmt.Errorf("summary get: bad action_items: %w", err)
	}
	if err := json.Unmarshal(unanswered, &d.Unanswered); err != nil {
		return SummaryDetail{}, fmt.Errorf("summary get: bad unanswered: %w", err)
	}
	return d, nil
}
