-- 0010_jobs.sql — durable job queue (post_call_techdoc.md §13b).
-- Postgres-as-queue: one row per owed unit of work, claimed with FOR UPDATE
-- SKIP LOCKED. First kind: 'postcall_summary' — enqueued at call end so a
-- gateway restart can no longer lose a summary. Payload is metadata only
-- (session id, customer, stats); the transcript stays in Redis with its TTL.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

CREATE TABLE jobs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,               -- 'postcall_summary'
    payload     JSONB NOT NULL,
    status      TEXT NOT NULL DEFAULT 'queued'
                CHECK (status IN ('queued', 'running', 'done', 'failed')),
    attempts    INT  NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    run_at      TIMESTAMPTZ NOT NULL DEFAULT now(),  -- not before this (backoff)
    last_error  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The claim query's path: due queued work, oldest first.
CREATE INDEX jobs_due_idx ON jobs (status, run_at) WHERE status IN ('queued', 'running');

-- RLS: tenant-scoped like everything else, PLUS a worker policy — the in-process
-- worker sets the transaction-local GUC app.worker='1' to scan all orgs' jobs
-- (same escape-hatch pattern as the 0005 login-lookup policies).
ALTER TABLE jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY jobs_isolation ON jobs
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);
CREATE POLICY jobs_worker ON jobs
    USING (current_setting('app.worker', true) = '1')
    WITH CHECK (current_setting('app.worker', true) = '1');
ALTER TABLE jobs FORCE ROW LEVEL SECURITY;

COMMIT;
