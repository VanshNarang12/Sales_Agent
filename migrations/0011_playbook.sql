-- 0011_playbook.sql — Stage 11: org guidance + do-not-say rules
-- (admin_playbook_techdoc.md). Both are compiled into a prompt block injected
-- into every Suggest card generation. Writes are admin-only (enforced in the
-- handlers via users.role); RLS scopes rows to the tenant as usual.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

-- One playbook row per org (upserted on first save).
CREATE TABLE playbooks (
    org_id     UUID PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
    guidance   TEXT NOT NULL DEFAULT '',
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- "Never say X (because Y)" — one row per forbidden phrase.
CREATE TABLE playbook_rules (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    phrase     TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX playbook_rules_org_idx ON playbook_rules (org_id, created_at);

ALTER TABLE playbooks      ENABLE ROW LEVEL SECURITY;
ALTER TABLE playbook_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY playbooks_isolation ON playbooks
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);
CREATE POLICY playbook_rules_isolation ON playbook_rules
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);
ALTER TABLE playbooks      FORCE ROW LEVEL SECURITY;
ALTER TABLE playbook_rules FORCE ROW LEVEL SECURITY;

COMMIT;
