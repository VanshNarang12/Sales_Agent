-- 0009_untagged_summaries.sql — keep summaries from untagged customer calls.
-- Decision 2026-09-29 (post_call_techdoc.md §13b): a call with no typed customer
-- name still saves its summary, with customer_id NULL. It shows on the meetings
-- list as "No customer tagged" and can be attached to a customer later via
-- PATCH /v1/meetings/{id}/customer. FK + RLS unchanged.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

ALTER TABLE call_summaries ALTER COLUMN customer_id DROP NOT NULL;

COMMIT;
