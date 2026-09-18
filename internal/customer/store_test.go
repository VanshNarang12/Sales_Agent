package customer

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/db"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
	"github.com/VanshNarang12/sales-agent/internal/postcall"
)

// Needs a real Postgres with migrations 0001+0003 applied (RLS can't be faked).
// Set TEST_DATABASE_URL to run; skips otherwise.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newTestOrg(t *testing.T, pool *pgxpool.Pool) context.Context {
	t.Helper()
	orgID := uuid.NewString()
	ctx := tenancy.With(context.Background(), tenancy.TenantID(orgID))
	err := db.WithTenantTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO orgs (id, name) VALUES ($1, 'test-org')", orgID)
		return err
	})
	if err != nil {
		t.Fatalf("create test org: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenantTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "DELETE FROM orgs WHERE id = $1", orgID)
			return err
		})
	})
	return ctx
}

// seedSummary writes a digest through the postcall store — the same path prod uses.
func seedSummary(t *testing.T, pool *pgxpool.Pool, ctx context.Context, customerName, sessionID string) {
	t.Helper()
	err := postcall.NewStore(pool).SaveSummary(ctx, customerName, sessionID, &postcall.Summary{
		Summary:     "Discussed pricing for " + customerName,
		ActionItems: []string{"Send proposal"},
		Unanswered:  []string{"Okta SSO?"},
	}, nil)
	if err != nil {
		t.Fatalf("seed summary: %v", err)
	}
}

func TestCreateCustomerFindOrCreate(t *testing.T) {
	pool := testPool(t)
	ctx := newTestOrg(t, pool)
	store := NewStore(pool)

	a, err := store.CreateCustomer(ctx, "Acme Corp")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	b, err := store.CreateCustomer(ctx, "  acme corp ")
	if err != nil {
		t.Fatalf("re-create: %v", err)
	}
	if a.ID != b.ID {
		t.Errorf("same customer split into two ids: %s / %s", a.ID, b.ID)
	}
	if b.Name != "Acme Corp" {
		t.Errorf("name = %q, want original casing kept", b.Name)
	}
	if _, err := store.CreateCustomer(ctx, "   "); err == nil {
		t.Error("want error on empty name")
	}
}

func TestListCustomersOrderAndCounts(t *testing.T) {
	pool := testPool(t)
	ctx := newTestOrg(t, pool)
	store := NewStore(pool)

	if _, err := store.CreateCustomer(ctx, "NeverMet Inc"); err != nil {
		t.Fatalf("create: %v", err)
	}
	seedSummary(t, pool, ctx, "Acme Corp", "s1")
	seedSummary(t, pool, ctx, "Acme Corp", "s2")

	list, err := store.ListCustomers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("customers = %d, want 2", len(list))
	}
	if list[0].Name != "Acme Corp" || list[0].Meetings != 2 || list[0].LastMeetingAt == nil {
		t.Errorf("first = %+v, want Acme Corp with 2 meetings and a last-meeting time", list[0])
	}
	if list[1].Name != "NeverMet Inc" || list[1].Meetings != 0 || list[1].LastMeetingAt != nil {
		t.Errorf("second = %+v, want NeverMet Inc with 0 meetings", list[1])
	}
}

func TestListAndGetSummaries(t *testing.T) {
	pool := testPool(t)
	ctx := newTestOrg(t, pool)
	store := NewStore(pool)

	seedSummary(t, pool, ctx, "Acme Corp", "s1")
	seedSummary(t, pool, ctx, "Acme Corp", "s2")
	cust, err := store.CreateCustomer(ctx, "Acme Corp")
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}

	metas, err := store.ListSummaries(ctx, cust.ID)
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("summaries = %d, want 2", len(metas))
	}
	if metas[0].CreatedAt.Before(metas[1].CreatedAt) {
		t.Error("summaries not newest-first")
	}

	d, err := store.GetSummary(ctx, metas[0].ID)
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	if d.CustomerID != cust.ID || d.Summary == "" || len(d.ActionItems) != 1 || len(d.Unanswered) != 1 {
		t.Errorf("detail = %+v, want full digest", d)
	}

	if _, err := store.ListSummaries(ctx, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown customer: err = %v, want ErrNotFound", err)
	}
	if _, err := store.GetSummary(ctx, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown summary: err = %v, want ErrNotFound", err)
	}
}

func TestTenantIsolation(t *testing.T) {
	pool := testPool(t)
	ctxA := newTestOrg(t, pool)
	ctxB := newTestOrg(t, pool)
	store := NewStore(pool)

	seedSummary(t, pool, ctxA, "Secret Corp", "s1")
	custA, err := store.CreateCustomer(ctxA, "Secret Corp")
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}
	metasA, err := store.ListSummaries(ctxA, custA.ID)
	if err != nil || len(metasA) != 1 {
		t.Fatalf("tenant A summaries: %v / %d", err, len(metasA))
	}

	listB, err := store.ListCustomers(ctxB)
	if err != nil || len(listB) != 0 {
		t.Errorf("tenant B customer list = %d (err %v), want 0", len(listB), err)
	}
	if _, err := store.ListSummaries(ctxB, custA.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("tenant B sees A's customer: err = %v, want ErrNotFound", err)
	}
	if _, err := store.GetSummary(ctxB, metasA[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("tenant B sees A's summary: err = %v, want ErrNotFound", err)
	}
}
