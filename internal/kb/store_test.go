package kb

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/db"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)

// Store tests need a real Postgres with migrations 0001+0002 applied (RLS can't be
// faked). Set TEST_DATABASE_URL to run them; they skip otherwise.
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

// newTestOrg creates a throwaway org and returns a ctx carrying its tenant id.
// Deleting the org on cleanup cascades to its documents and chunks.
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

func vecOf(dims int, fill float32) []float32 {
	v := make([]float32, dims)
	for i := range v {
		v[i] = fill
	}
	return v
}

func TestStoreIngestRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := newTestOrg(t, pool)
	store := NewStore(pool)

	docID, err := store.CreateDocument(ctx, "pricing.md")
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	pieces := []Piece{
		{Heading: "Pricing", Text: "Doc > Pricing — base plan costs money."},
		{Heading: "", Text: "Doc — intro text."},
	}
	vectors := [][]float32{vecOf(768, 0.1), vecOf(768, 0.2)}
	if err := store.InsertChunks(ctx, docID, pieces, vectors); err != nil {
		t.Fatalf("insert chunks: %v", err)
	}

	// A connection with NO tenant set must see nothing (RLS).
	var leaked int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM kb_chunks`).Scan(&leaked); err == nil && leaked != 0 {
		t.Errorf("RLS leak: tenant-less connection sees %d chunks, want 0", leaked)
	}

	var count int
	var status string
	err = db.WithTenantTx(ctx, pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_chunks WHERE document_id = $1`, docID).Scan(&count); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT status FROM kb_documents WHERE id = $1`, docID).Scan(&status)
	})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if count != 2 {
		t.Errorf("chunks = %d, want 2", count)
	}
	if status != "ready" {
		t.Errorf("status = %q, want ready", status)
	}
}

func TestStoreTenantIsolation(t *testing.T) {
	pool := testPool(t)
	ctxA := newTestOrg(t, pool)
	ctxB := newTestOrg(t, pool)
	store := NewStore(pool)

	docID, err := store.CreateDocument(ctxA, "secret-a.md")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.InsertChunks(ctxA, docID, []Piece{{Text: "secret A"}}, [][]float32{vecOf(768, 1)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var countB int
	err = db.WithTenantTx(ctxB, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctxB, `SELECT count(*) FROM kb_chunks`).Scan(&countB)
	})
	if err != nil {
		t.Fatalf("tenant B read: %v", err)
	}
	if countB != 0 {
		t.Fatalf("tenant B sees %d of tenant A's chunks, want 0", countB)
	}
}

func TestStoreMarkFailed(t *testing.T) {
	pool := testPool(t)
	ctx := newTestOrg(t, pool)
	store := NewStore(pool)

	docID, err := store.CreateDocument(ctx, "doomed.md")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.MarkFailed(ctx, docID); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	var status string
	err = db.WithTenantTx(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM kb_documents WHERE id = $1`, docID).Scan(&status)
	})
	if err != nil || status != "failed" {
		t.Fatalf("status = %q (err %v), want failed", status, err)
	}
}

func TestStorePieceVectorMismatch(t *testing.T) {
	store := NewStore(nil) // fails before touching the DB
	if err := store.InsertChunks(context.Background(), "doc", []Piece{{Text: "a"}}, nil); err == nil {
		t.Fatal("want error on pieces/vectors length mismatch")
	}
}
