-- 0001_init.sql — Stage 0 foundations schema.
-- Establishes the tenant (org), users, and the Row-Level Security pattern every
-- tenant-scoped table follows. Enables pgvector for later RAG work (Stage 4/5).
-- See techdocs/project_foundations_techdoc.md and ARCHITECTURE.md §9/§13.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

-- pgvector for embedding columns added by later stages.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

-- The tenant. org.id IS the tenant_id used everywhere.
CREATE TABLE orgs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Users belong to exactly one org (tenant). org_id is the tenant scope.
-- email is stored lowercased by the app; switch to CITEXT later if needed.
CREATE TABLE users (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    email          TEXT NOT NULL,
    oauth_provider TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, email)
);

-- ----------------------------------------------------------------------------
-- Row-Level Security: tenant isolation enforced by the database, not app code.
-- The application sets the current tenant per-transaction via:
--     SELECT set_config('app.tenant_id', '<org-uuid>', true);
-- (see internal/platform/db.WithTenantTx). Policies below read that GUC.
-- ----------------------------------------------------------------------------

ALTER TABLE orgs  ENABLE ROW LEVEL SECURITY;
ALTER TABLE users ENABLE ROW LEVEL SECURITY;

-- orgs: a tenant may only see its own org row.
CREATE POLICY org_isolation ON orgs
    USING (id = current_setting('app.tenant_id', true)::uuid);

-- users: scoped to the current tenant.
CREATE POLICY user_isolation ON users
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);

-- Force RLS even for the table owner so app connections cannot bypass it.
ALTER TABLE orgs  FORCE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;

COMMIT;
