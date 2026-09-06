-- 0002_knowledge_base.sql — Stage 4: documents-only knowledge base.
-- One row per uploaded document in kb_documents; its searchable pieces in kb_chunks
-- with a 768-dim embedding (nomic-embed-text-v1.5). Tenant isolation via the same
-- RLS pattern as 0001. See techdocs/knowledge_base_techdoc.md.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

CREATE TABLE kb_documents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,   -- filename; shown in citations
    status      TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('processing','ready','failed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE kb_chunks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
    position    INT  NOT NULL,   -- chunk order inside the document
    heading     TEXT,            -- breadcrumb path; NULL if the doc had no structure
    chunk_text  TEXT NOT NULL,   -- breadcrumb-prefixed text that was embedded
    embedding   vector(768) NOT NULL,   -- nomic-embed-text-v1.5 output size
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, position)
);

-- Nearest-neighbor search index. HNSW over IVFFlat: needs no training data, so it
-- works from row one and keeps good recall as the table grows.
CREATE INDEX kb_chunks_embedding_idx ON kb_chunks USING hnsw (embedding vector_cosine_ops);

-- Lookups by document (re-ingest wipes a document's chunks).
CREATE INDEX kb_chunks_document_idx ON kb_chunks (document_id);

-- ----------------------------------------------------------------------------
-- Row-Level Security: same pattern as 0001 — the app sets app.tenant_id per
-- transaction (db.WithTenantTx); the DB filters every read/write by it.
-- ----------------------------------------------------------------------------

ALTER TABLE kb_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE kb_chunks    ENABLE ROW LEVEL SECURITY;

CREATE POLICY kb_documents_isolation ON kb_documents
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);

CREATE POLICY kb_chunks_isolation ON kb_chunks
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);

ALTER TABLE kb_documents FORCE ROW LEVEL SECURITY;
ALTER TABLE kb_chunks    FORCE ROW LEVEL SECURITY;

COMMIT;
