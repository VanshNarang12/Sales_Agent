package kb

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/VanshNarang12/sales-agent/internal/embed"
)

type fakeStore struct {
	created     []string
	inserted    [][]Piece
	failed      []string
	insertErr   error
	createErr   error
	lastVectors [][]float32
	nextDocID   string
}

func (f *fakeStore) CreateDocument(_ context.Context, title string) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.created = append(f.created, title)
	if f.nextDocID == "" {
		f.nextDocID = "doc-1"
	}
	return f.nextDocID, nil
}

func (f *fakeStore) InsertChunks(_ context.Context, _ string, pieces []Piece, vectors [][]float32) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, pieces)
	f.lastVectors = vectors
	return nil
}

func (f *fakeStore) MarkFailed(_ context.Context, id string) error {
	f.failed = append(f.failed, id)
	return nil
}

type fakeIngestEmbedder struct {
	err   error
	calls int
	seen  []string
}

func (f *fakeIngestEmbedder) Embed(_ context.Context, prefix embed.Prefix, texts []string) ([][]float32, error) {
	f.calls++
	f.seen = texts
	if prefix != embed.PrefixDocument {
		return nil, errors.New("wrong prefix for ingestion")
	}
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 2, 3}
	}
	return out, nil
}

func TestIngestHappyPath(t *testing.T) {
	st := &fakeStore{}
	em := &fakeIngestEmbedder{}
	ing := NewIngester(em, st, 450, 60)

	docID, n, err := ing.IngestDocument(context.Background(), "pricing.md", "# Plans\nBase plan costs money.")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if docID != "doc-1" || n != 1 {
		t.Errorf("got (%s, %d), want (doc-1, 1)", docID, n)
	}
	if len(st.inserted) != 1 || len(st.lastVectors) != 1 {
		t.Errorf("chunks/vectors not stored: %+v", st)
	}
	if len(st.failed) != 0 {
		t.Errorf("MarkFailed called on happy path")
	}
	if !strings.Contains(em.seen[0], "pricing.md > Plans — ") {
		t.Errorf("embedded text missing breadcrumb: %q", em.seen[0])
	}
}

func TestIngestEmbedFailureMarksFailed(t *testing.T) {
	st := &fakeStore{}
	em := &fakeIngestEmbedder{err: errors.New("groq down")}
	ing := NewIngester(em, st, 450, 60)

	docID, _, err := ing.IngestDocument(context.Background(), "doc.md", "Some text here.")
	if err == nil {
		t.Fatal("want error when embedding fails")
	}
	if len(st.inserted) != 0 {
		t.Error("chunks must not be inserted after embed failure")
	}
	if len(st.failed) != 1 || st.failed[0] != docID {
		t.Errorf("document not marked failed: %+v", st.failed)
	}
}

func TestIngestInsertFailureMarksFailed(t *testing.T) {
	st := &fakeStore{insertErr: errors.New("db down")}
	em := &fakeIngestEmbedder{}
	ing := NewIngester(em, st, 450, 60)

	docID, _, err := ing.IngestDocument(context.Background(), "doc.md", "Some text here.")
	if err == nil {
		t.Fatal("want error when insert fails")
	}
	if len(st.failed) != 1 || st.failed[0] != docID {
		t.Errorf("document not marked failed: %+v", st.failed)
	}
}

func TestIngestEmptyDocumentRejected(t *testing.T) {
	st := &fakeStore{}
	ing := NewIngester(&fakeIngestEmbedder{}, st, 450, 60)

	_, _, err := ing.IngestDocument(context.Background(), "empty.md", "   \n\n ")
	if err == nil {
		t.Fatal("want error for empty document")
	}
	if len(st.created) != 0 {
		t.Error("no document row should be created for empty text")
	}
}
