package kb

import (
	"context"
	"fmt"

	"github.com/VanshNarang12/sales-agent/internal/embed"
)

// documentStore is what the Ingester needs from storage (*Store satisfies it; tests use a fake).
type documentStore interface {
	CreateDocument(ctx context.Context, title string) (string, error)
	InsertChunks(ctx context.Context, documentID string, pieces []Piece, vectors [][]float32) error
	MarkFailed(ctx context.Context, documentID string) error
}

// Ingester orchestrates one document's ingestion: chunk → embed → store.
// Dependencies are built once at boot (main.go) and shared across uploads.
type Ingester struct {
	embedder      embed.Embedder
	store         documentStore
	targetTokens  int
	overlapTokens int
}

func NewIngester(embedder embed.Embedder, store documentStore, targetTokens, overlapTokens int) *Ingester {
	return &Ingester{
		embedder:      embedder,
		store:         store,
		targetTokens:  targetTokens,
		overlapTokens: overlapTokens,
	}
}

// IngestDocument takes plain text (extraction from PDF/DOCX happens in the handler,
// before this) and makes it searchable. On embed/store failure the document is
// marked 'failed' and zero chunks exist — never half-indexed.
func (in *Ingester) IngestDocument(ctx context.Context, title, text string) (documentID string, chunkCount int, err error) {
	pieces := Chunk(title, text, in.targetTokens, in.overlapTokens)
	if len(pieces) == 0 {
		return "", 0, fmt.Errorf("ingest %q: document has no extractable text", title)
	}

	documentID, err = in.store.CreateDocument(ctx, title)
	if err != nil {
		return "", 0, fmt.Errorf("ingest %q: %w", title, err)
	}

	texts := make([]string, len(pieces))
	for i, p := range pieces {
		texts[i] = p.Text
	}
	vectors, err := in.embedder.Embed(ctx, embed.PrefixDocument, texts)
	if err != nil {
		_ = in.store.MarkFailed(ctx, documentID) // best effort; the real error is the embed one
		return documentID, 0, fmt.Errorf("ingest %q: %w", title, err)
	}

	if err := in.store.InsertChunks(ctx, documentID, pieces, vectors); err != nil {
		_ = in.store.MarkFailed(ctx, documentID)
		return documentID, 0, fmt.Errorf("ingest %q: %w", title, err)
	}
	return documentID, len(pieces), nil
}
