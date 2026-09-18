-- 0004_summary_embeddings.sql — Stage 10.8: semantic search over call summaries.
-- Adds a nullable 768-dim embedding (same nomic model as kb_chunks) to each
-- digest so the prep chat can retrieve relevant past meetings per message.
-- Nullable: an embedding failure must never block saving the summary itself.
-- See techdocs/customer_memory_techdoc.md.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

ALTER TABLE call_summaries ADD COLUMN embedding vector(768);

-- HNSW over IVFFlat for the same reason as kb_chunks (0002): no training data
-- needed, works from row one. Partial: skip rows whose embedding failed.
CREATE INDEX call_summaries_embedding_idx ON call_summaries
    USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;

COMMIT;
