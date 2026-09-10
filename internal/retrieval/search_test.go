package retrieval

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/kb"
	"github.com/VanshNarang12/sales-agent/internal/platform/db"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)

type fakeEmbedder struct {
	lastPrefix embed.Prefix
	calls      int
	out        [][]float32
	err        error
}

func (f *fakeEmbedder) Embed(_ context.Context, prefix embed.Prefix, texts []string) ([][]float32, error) {
	f.lastPrefix, f.calls = prefix, f.calls+1
	return f.out, f.err
}

func TestSearchEmptyAskSkipsEmbedding(t *testing.T) {
	fe := &fakeEmbedder{}
	hits, err := NewSearcher(fe, nil, 5, 0.5).Search(context.Background(), "   ")
	if err != nil || hits != nil {
		t.Fatalf("want (nil, nil) for empty ask, got (%v, %v)", hits, err)
	}
	if fe.calls != 0 {
		t.Error("embedder called for an empty ask")
	}
}

func TestSearchPropagatesEmbedError(t *testing.T) {
	fe := &fakeEmbedder{err: errors.New("groq down")}
	_, err := NewSearcher(fe, nil, 5, 0.5).Search(context.Background(), "pricing")
	if err == nil {
		t.Fatal("want error when embedding fails")
	}
}

// No tenant in ctx must fail before the pool is touched (nil pool proves it),
// and the ask must have been embedded with the query prefix — never document.
func TestSearchRequiresTenantAndUsesQueryPrefix(t *testing.T) {
	fe := &fakeEmbedder{out: [][]float32{vec(768, 0.1)}}
	_, err := NewSearcher(fe, nil, 5, 0.5).Search(context.Background(), "pricing")
	if err == nil {
		t.Fatal("want error when ctx has no tenant")
	}
	if fe.lastPrefix != embed.PrefixQuery {
		t.Errorf("prefix = %q, want %q", fe.lastPrefix, embed.PrefixQuery)
	}
}

// --- DB tests: need Postgres with migrations applied; skip without TEST_DATABASE_URL ---

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

// vec builds a 768-dim vector with v[0]=x, v[1]=y — orthogonal test vectors so
// cosine similarity is exactly 1 (same axis) or 0 (other axis).
func vec(dims int, x float32, rest ...float32) []float32 {
	v := make([]float32, dims)
	v[0] = x
	for i, r := range rest {
		v[i+1] = r
	}
	return v
}

func ingestDoc(t *testing.T, ctx context.Context, pool *pgxpool.Pool, title string, pieces []kb.Piece, vectors [][]float32) string {
	t.Helper()
	store := kb.NewStore(pool)
	docID, err := store.CreateDocument(ctx, title)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	if err := store.InsertChunks(ctx, docID, pieces, vectors); err != nil {
		t.Fatalf("insert chunks: %v", err)
	}
	return docID
}

func TestSearchRanksFiltersAndCites(t *testing.T) {
	pool := testPool(t)
	ctx := newTestOrg(t, pool)

	docID := ingestDoc(t, ctx, pool, "pricing.md",
		[]kb.Piece{
			{Heading: "Pricing > Discounts", Text: "Annual plans get 20% off."},
			{Heading: "Legal", Text: "Governing law is Delaware."},
		},
		[][]float32{vec(768, 1, 0), vec(768, 0, 1)}) // orthogonal: sim 1 vs 0 for query e1

	fe := &fakeEmbedder{out: [][]float32{vec(768, 1, 0)}}
	hits, err := NewSearcher(fe, pool, 5, 0.5).Search(ctx, "what discount for annual")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1 (orthogonal chunk must fall below threshold)", len(hits))
	}
	h := hits[0]
	if h.DocumentID != docID || h.Title != "pricing.md" || h.Heading != "Pricing > Discounts" {
		t.Errorf("citation fields wrong: %+v", h)
	}
	if h.Text != "Annual plans get 20% off." {
		t.Errorf("text = %q", h.Text)
	}
	if h.Score < 0.99 {
		t.Errorf("score = %f, want ~1 for identical vector", h.Score)
	}
}

func TestSearchExcludesNotReadyDocuments(t *testing.T) {
	pool := testPool(t)
	ctx := newTestOrg(t, pool)

	docID := ingestDoc(t, ctx, pool, "draft.md",
		[]kb.Piece{{Text: "draft content"}}, [][]float32{vec(768, 1)})
	err := db.WithTenantTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE kb_documents SET status = 'processing' WHERE id = $1`, docID)
		return err
	})
	if err != nil {
		t.Fatalf("set processing: %v", err)
	}

	fe := &fakeEmbedder{out: [][]float32{vec(768, 1)}}
	hits, err := NewSearcher(fe, pool, 5, 0.5).Search(ctx, "anything")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %d, want 0 — 'processing' documents must not be searchable", len(hits))
	}
}

func TestSearchTenantIsolation(t *testing.T) {
	pool := testPool(t)
	ctxA := newTestOrg(t, pool)
	ctxB := newTestOrg(t, pool)

	ingestDoc(t, ctxA, pool, "secret-a.md",
		[]kb.Piece{{Text: "tenant A secret"}}, [][]float32{vec(768, 1)})

	fe := &fakeEmbedder{out: [][]float32{vec(768, 1)}}
	hits, err := NewSearcher(fe, pool, 5, 0.5).Search(ctxB, "secret")
	if err != nil {
		t.Fatalf("search as tenant B: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("tenant B sees %d of tenant A's chunks, want 0", len(hits))
	}
}
