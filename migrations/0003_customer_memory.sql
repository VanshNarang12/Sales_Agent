-- 0003_customer_memory.sql — Stage 10: customer records + persisted call summaries.
-- One row per tagged customer in customers; one row per summarized customer call in
-- call_summaries (summaries only — transcripts/audio are never persisted). Tenant
-- isolation via the same RLS pattern as 0001/0002.
-- See techdocs/post_call_techdoc.md and techdocs/customer_memory_techdoc.md.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

-- A customer the org has calls with. Created on first tag; reused on later calls
-- so context accumulates across reps (9.11).
CREATE TABLE customers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,   -- as typed by the rep; matched case-insensitively
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- "Acme", "acme" and "ACME" are the same customer within an org.
CREATE UNIQUE INDEX customers_org_name_idx ON customers (org_id, lower(name));

-- The digest of one customer call (9.12). The only call data that survives the
-- call: summary + action items + unanswered questions. Internal meetings never
-- produce a row.
CREATE TABLE call_summaries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    customer_id  UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    session_id   TEXT NOT NULL,   -- gateway call-session id, for log correlation
    summary      TEXT NOT NULL,
    action_items JSONB NOT NULL DEFAULT '[]',  -- ["Send security whitepaper", ...]
    unanswered   JSONB NOT NULL DEFAULT '[]',  -- ["Does it support Okta SSO?", ...]
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The timeline read (9.13) and the prep-chat context read (9.14):
-- "all summaries for customer X, newest first".
CREATE INDEX call_summaries_customer_idx ON call_summaries (customer_id, created_at DESC);

-- ----------------------------------------------------------------------------
-- Row-Level Security: same pattern as 0001 — the app sets app.tenant_id per
-- transaction (db.WithTenantTx); the DB filters every read/write by it.
-- ----------------------------------------------------------------------------

ALTER TABLE customers      ENABLE ROW LEVEL SECURITY;
ALTER TABLE call_summaries ENABLE ROW LEVEL SECURITY;

CREATE POLICY customers_isolation ON customers
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);

CREATE POLICY call_summaries_isolation ON call_summaries
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);

ALTER TABLE customers      FORCE ROW LEVEL SECURITY;
ALTER TABLE call_summaries FORCE ROW LEVEL SECURITY;

COMMIT;
