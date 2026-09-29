-- 0006_user_roles.sql — org roles (Stage 0.5 follow-up, feeds Stage 18 invites).
-- The org creator is its admin; users added later (invite flow, Stage 18) are
-- members. Role is backend data only — the UI never switches on it yet.
-- DEFAULT 'admin' backfills existing rows correctly: every existing user created
-- their own org. Invite inserts must set role explicitly.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

ALTER TABLE users
    ADD COLUMN role TEXT NOT NULL DEFAULT 'admin'
    CHECK (role IN ('admin', 'member'));

COMMIT;
