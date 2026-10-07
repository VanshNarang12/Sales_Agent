package postcall

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

// Store persists customers and call summaries (migrations/0003). Every method
// runs inside db.WithTenantTx, so the tenant comes from ctx and RLS filters
// every read/write.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Stats is what the live session measured about one call (migration 0007): when
// it started, how long it ran, cards served, and the documents those cards cited.
type Stats struct {
	StartedAt        time.Time
	DurationSeconds  int
	SuggestionsCount int
	Sources          []string
}

// SaveSummary inserts the summary row; with a customer name it finds-or-creates
// the customer (case-insensitive, so "Acme" and "acme" accumulate on one record),
// with an empty name it saves untagged (customer_id NULL, migration 0009) for
// later tagging via TagMeeting. One transaction, all-or-nothing. embedding may be
// nil (embed failure never blocks the save); prep chat searches embedded rows only.
func (s *Store) SaveSummary(ctx context.Context, customerName, sessionID string, sum *Summary, embedding []float32, stats Stats) error {
	name := strings.TrimSpace(customerName)
	items, err := json.Marshal(sum.ActionItems)
	if err != nil {
		return fmt.Errorf("postcall save: %w", err)
	}
	unanswered, err := json.Marshal(sum.Unanswered)
	if err != nil {
		return fmt.Errorf("postcall save: %w", err)
	}
	if stats.Sources == nil {
		stats.Sources = []string{}
	}
	sources, err := json.Marshal(stats.Sources)
	if err != nil {
		return fmt.Errorf("postcall save: %w", err)
	}
	err = db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		var customerID *string // NULL when the call was untagged
		if name != "" {
			id, err := upsertCustomer(ctx, tx, name)
			if err != nil {
				return err
			}
			customerID = &id
		}
		var vec *string
		if len(embedding) > 0 {
			v := vectorLiteral(embedding)
			vec = &v
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO call_summaries (org_id, customer_id, session_id, summary, action_items, unanswered, embedding,
			                             started_at, duration_seconds, suggestions_count, sources)
			 VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4, $5, $6::vector, $7, $8, $9, $10)`,
			customerID, sessionID, sum.Summary, items, unanswered, vec,
			nullTime(stats.StartedAt), stats.DurationSeconds, stats.SuggestionsCount, sources)
		return err
	})
	if err != nil {
		return fmt.Errorf("postcall save: %w", err)
	}
	return nil
}

// Meeting is one summarized customer call as the meetings APIs expose it.
// StartedAt falls back to the row's created_at for pre-0007 rows.
type Meeting struct {
	ID               string    `json:"meeting_id"`
	ClientName       string    `json:"client_name"`
	StartedAt        time.Time `json:"started_at"`
	DurationMinutes  int       `json:"duration_minutes"`
	Summary          string    `json:"summary"`
	ActionItems      []string  `json:"action_items,omitempty"`
	Unanswered       []string  `json:"unanswered,omitempty"`
	SuggestionsCount int       `json:"suggestions_count"`
	Sources          []string  `json:"sources"`
}

var ErrMeetingNotFound = errors.New("postcall: meeting not found")

// COALESCE + LEFT JOIN: untagged summaries (customer_id NULL) list with an empty
// client name — the UI renders them as "No customer tagged".
const meetingCols = `cs.id, COALESCE(c.name, ''), COALESCE(cs.started_at, cs.created_at), cs.duration_seconds,
       cs.summary, cs.action_items, cs.unanswered, cs.suggestions_count, cs.sources, cs.created_at`

// upsertCustomer finds-or-creates a customer by name within the tenant. The no-op
// DO UPDATE keeps the first-typed casing and makes RETURNING yield the id on both
// the insert and the already-exists path.
func upsertCustomer(ctx context.Context, tx pgx.Tx, name string) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO customers (org_id, name)
		 VALUES (current_setting('app.tenant_id')::uuid, $1)
		 ON CONFLICT (org_id, lower(name)) DO UPDATE SET name = customers.name
		 RETURNING id`, name).Scan(&id)
	return id, err
}

// TagMeeting attaches an untagged (or re-tags a tagged) summary to a customer by
// name — find-or-create, then point the row at them. From then on the summary
// joins that customer's timeline and prep-chat context.
func (s *Store) TagMeeting(ctx context.Context, meetingID, customerName string) error {
	name := strings.TrimSpace(customerName)
	if name == "" {
		return fmt.Errorf("postcall tag: empty customer name")
	}
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		customerID, err := upsertCustomer(ctx, tx, name)
		if err != nil {
			return err
		}
		res, err := tx.Exec(ctx,
			`UPDATE call_summaries SET customer_id = $1 WHERE id = $2`, customerID, meetingID)
		if err != nil {
			return err
		}
		if res.RowsAffected() == 0 {
			return ErrMeetingNotFound
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrMeetingNotFound) {
		return fmt.Errorf("postcall tag: %w", err)
	}
	return err
}

// ListMeetings returns the org's summarized calls, newest first. The cursor is
// "<created_at RFC3339Nano>|<id>" from the previous page's last row; empty means
// first page. Second return is the next cursor ("" on the last page).
func (s *Store) ListMeetings(ctx context.Context, limit int, cursor string) ([]Meeting, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where, args := "", []any{limit + 1}
	if cursor != "" {
		ts, id, ok := strings.Cut(cursor, "|")
		at, err := time.Parse(time.RFC3339Nano, ts)
		if !ok || err != nil {
			return nil, "", fmt.Errorf("postcall list: bad cursor")
		}
		where = "WHERE (cs.created_at, cs.id) < ($2, $3::uuid)"
		args = append(args, at, id)
	}
	var out []Meeting
	var createdAts []time.Time
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, fmt.Sprintf(`
			SELECT %s FROM call_summaries cs
			LEFT JOIN customers c ON c.id = cs.customer_id
			%s
			ORDER BY cs.created_at DESC, cs.id DESC
			LIMIT $1`, meetingCols, where), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, created, err := scanMeeting(rows)
			if err != nil {
				return err
			}
			out = append(out, m)
			createdAts = append(createdAts, created)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", fmt.Errorf("postcall list: %w", err)
	}
	// We fetched limit+1 rows; a full overflow means there is a next page and the
	// cursor points at the last row we actually return.
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = createdAts[limit-1].Format(time.RFC3339Nano) + "|" + out[limit-1].ID
	}
	return out, next, nil
}

// GetMeeting returns one summarized call in full (detail dialog).
func (s *Store) GetMeeting(ctx context.Context, id string) (Meeting, error) {
	var m Meeting
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, fmt.Sprintf(`
			SELECT %s FROM call_summaries cs
			LEFT JOIN customers c ON c.id = cs.customer_id
			WHERE cs.id = $1`, meetingCols), id)
		var err error
		m, _, err = scanMeeting(row)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Meeting{}, ErrMeetingNotFound
	}
	if err != nil {
		return Meeting{}, fmt.Errorf("postcall get meeting: %w", err)
	}
	return m, nil
}

// scanMeeting maps one joined row; also returns created_at for cursor building.
func scanMeeting(row pgx.Row) (Meeting, time.Time, error) {
	var m Meeting
	var durationSec int
	var items, unanswered, sources []byte
	var createdAt time.Time
	if err := row.Scan(&m.ID, &m.ClientName, &m.StartedAt, &durationSec,
		&m.Summary, &items, &unanswered, &m.SuggestionsCount, &sources, &createdAt); err != nil {
		return Meeting{}, time.Time{}, err
	}
	m.DurationMinutes = (durationSec + 30) / 60
	_ = json.Unmarshal(items, &m.ActionItems)
	_ = json.Unmarshal(unanswered, &m.Unanswered)
	if err := json.Unmarshal(sources, &m.Sources); err != nil || m.Sources == nil {
		m.Sources = []string{}
	}
	return m, createdAt, nil
}

// nullTime maps the zero time to NULL (pre-0007 fallback handled at read time).
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
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
