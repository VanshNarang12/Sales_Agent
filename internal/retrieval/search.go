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
	all, err := s.SearchMulti(ctx, []string{ask})
	if err != nil {
		return nil, err
	}
	return all[0], nil
}

// SearchMulti answers several asks at the wire cost of one (multi-card Suggest,
// 2026-10-06): one embedding call for all asks, then every vector search queued
// in a single db.WithTenantBatch round trip. Result i belongs to asks[i].
func (s *Searcher) SearchMulti(ctx context.Context, asks []string) ([][]SearchResult, error) {
	if len(asks) == 0 {
		return nil, nil
	}
	vecs, err := s.embedder.Embed(ctx, embed.PrefixQuery, asks)
	if err != nil {
		return nil, fmt.Errorf("search embed: %w", err)
	}
	if len(vecs) != len(asks) {
		return nil, fmt.Errorf("search embed: %d vectors for %d asks", len(vecs), len(asks))
	}
	results := make([][]SearchResult, len(asks))
	err = db.WithTenantBatch(ctx, s.pool,
		func(b *pgx.Batch) {
			for _, v := range vecs {
				b.Queue(searchSQL, vectorLiteral(v), s.topK)
			}
		},
		func(br pgx.BatchResults) error {
			for i := range vecs {
				hits, err := scanHits(br, s.minScore)
				if err != nil {
					return err
				}
				results[i] = hits
			}
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	return results, nil
}

// scanHits reads one queued search's rows off the batch, applying the score gate.
func scanHits(br pgx.BatchResults, minScore float64) ([]SearchResult, error) {
	rows, err := br.Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.DocumentID, &r.Title, &r.Heading, &r.Text, &r.Score); err != nil {
			return nil, err
		}
		if r.Score >= minScore {
			hits = append(hits, r)
		}
	}
	return hits, rows.Err()
}

// SearchVec searches with an already-embedded query — for callers (prep chat)
// that embed once and search several tables with the same vector.
func (s *Searcher) SearchVec(ctx context.Context, vec []float32, topK int) ([]SearchResult, error) {
	if topK <= 0 {
		topK = s.topK
	}
	// Batched (one round trip): the Suggest hot path pays ~270 ms × 5 for a plain
	// tenant tx against a remote DB. RLS scoping is unchanged (db.WithTenantBatch).
	var hits []SearchResult
	err := db.WithTenantBatch(ctx, s.pool,
		func(b *pgx.Batch) { b.Queue(searchSQL, vectorLiteral(vec), topK) },
		func(br pgx.BatchResults) error {
			var err error
			hits, err = scanHits(br, s.minScore)
			return err
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
