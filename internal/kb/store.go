package kb

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/VanshNarang12/sales-agent/internal/platform/db"
)

// Store persists documents and chunks. Every method runs inside db.WithTenantTx,
// so the tenant comes from ctx and RLS filters every read/write.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreateDocument inserts the document row in status 'processing' and returns its id.
func (s *Store) CreateDocument(ctx context.Context, title string) (string, error) {
	var id string
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`INSERT INTO kb_documents (org_id, title, status)
			 VALUES (current_setting('app.tenant_id')::uuid, $1, 'processing')
			 RETURNING id`, title).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("kb create document: %w", err)
	}
	return id, nil
}

// InsertChunks bulk-writes all chunk rows and flips the document to 'ready' in ONE
// transaction — all-or-nothing. Bulk = one pgx.Batch (single network round trip);
// CopyFrom was not used because its binary protocol needs registered codecs for the
// vector type, while the $N::vector text cast works with the plain pool.
func (s *Store) InsertChunks(ctx context.Context, documentID string, pieces []Piece, vectors [][]float32) error {
	if len(pieces) != len(vectors) {
		return fmt.Errorf("kb insert chunks: %d pieces but %d vectors", len(pieces), len(vectors))
	}
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for i, p := range pieces {
			heading := any(p.Heading)
			if p.Heading == "" {
				heading = nil
			}
			batch.Queue(
				`INSERT INTO kb_chunks (org_id, document_id, position, heading, chunk_text, embedding)
				 VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4, $5::vector)`,
				documentID, i, heading, p.Text, vectorLiteral(vectors[i]))
		}
		batch.Queue(`UPDATE kb_documents SET status = 'ready' WHERE id = $1`, documentID)
		return tx.SendBatch(ctx, batch).Close()
	})
	if err != nil {
		return fmt.Errorf("kb insert chunks: %w", err)
	}
	return nil
}

// MarkFailed records that ingestion died after the document row was created.
func (s *Store) MarkFailed(ctx context.Context, documentID string) error {
	err := db.WithTenantTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE kb_documents SET status = 'failed' WHERE id = $1`, documentID)
		return err
	})
	if err != nil {
		return fmt.Errorf("kb mark failed: %w", err)
	}
	return nil
}

// vectorLiteral renders a vector in pgvector's text form: [0.1,0.2,...].
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
