package retrieval

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/platform/db"
)

// SearchResult is one KB chunk that answers the ask, with its citation fields.
type SearchResult struct {
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	Heading    string  `json:"heading,omitempty"`
	Text       string  `json:"text"`
	Score      float64 `json:"score"` // cosine similarity, 1 = identical
}

// Searcher turns an extracted ask into scored, cited chunks (retrieval step 2).
// Tenant scoping comes from ctx via db.WithTenantTx; RLS filters every row.
type Searcher struct {
	embedder embed.Embedder
	pool     *pgxpool.Pool
	topK     int
	minScore float64
}

func NewSearcher(embedder embed.Embedder, pool *pgxpool.Pool, topK int, minScore float64) *Searcher {
	return &Searcher{embedder: embedder, pool: pool, topK: topK, minScore: minScore}
}

// searchSQL: cosine distance over the HNSW index, title joined for the citation,
// only 'ready' documents. Distance is converted to similarity in the SELECT so
// the threshold filter below reads the same number the client will see.
const searchSQL = `
SELECT c.document_id, d.title, COALESCE(c.heading, ''), c.chunk_text,
       1 - (c.embedding <=> $1::vector) AS score
FROM kb_chunks c
JOIN kb_documents d ON d.id = c.document_id
WHERE d.status = 'ready'
ORDER BY c.embedding <=> $1::vector
LIMIT $2`

// Search embeds the ask (query prefix — chunks were embedded with the document
// prefix) and returns the top-K chunks at or above minScore. An empty result
// means: searched, nothing good enough — the caller shows "no answer", not a card.
func (s *Searcher) Search(ctx context.Context, ask string) ([]SearchResult, error) {
	if strings.TrimSpace(ask) == "" {
		return nil, nil
	}
	vecs, err := s.embedder.Embed(ctx, embed.PrefixQuery, []string{ask})
	if err != nil {
		return nil, fmt.Errorf("search embed: %w", err)
	}

	var hits []SearchResult
	err = db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, searchSQL, vectorLiteral(vecs[0]), s.topK)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r SearchResult
			if err := rows.Scan(&r.DocumentID, &r.Title, &r.Heading, &r.Text, &r.Score); err != nil {
				return err
			}
			if r.Score >= s.minScore {
				hits = append(hits, r)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	return hits, nil
}

// vectorLiteral renders a vector in pgvector's text form: [0.1,0.2,...].
// Same helper as kb's; duplicated so retrieval doesn't import ingest internals.
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v)*10 + 2)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
