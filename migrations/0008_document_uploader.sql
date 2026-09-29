-- 0008_document_uploader.sql — who uploaded each KB document (Documents page).
-- Nullable: pre-0008 rows have no known uploader (UI shows "—"), and a deleted
-- user leaves their documents behind — the knowledge belongs to the org.
-- See techdocs/knowledge_base_techdoc.md §7.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

ALTER TABLE kb_documents
    ADD COLUMN uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL;

COMMIT;
