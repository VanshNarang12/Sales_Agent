-- 0007_meeting_stats.sql — per-call stats for the meetings APIs (web app).
-- Adds what the org-wide meeting list needs but Stage 10 never captured: when the
-- call started, how long it ran, how many answer cards were served, and which
-- documents were cited. Old rows keep NULL/0/[] defaults — the API falls back to
-- created_at for started_at. See techdocs/meetings_api_techdoc.md.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

ALTER TABLE call_summaries
    ADD COLUMN started_at        TIMESTAMPTZ,
    ADD COLUMN duration_seconds  INT   NOT NULL DEFAULT 0,
    ADD COLUMN suggestions_count INT   NOT NULL DEFAULT 0,
    ADD COLUMN sources           JSONB NOT NULL DEFAULT '[]';  -- ["pricing.pdf", ...]

-- Org-wide list read: "all meetings in my org, newest first, cursor-paginated".
CREATE INDEX call_summaries_org_time_idx ON call_summaries (org_id, created_at DESC, id DESC);

COMMIT;
