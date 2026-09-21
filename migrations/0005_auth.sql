-- 0005_auth.sql — Stage 0.5 auth: credentials, phone verification, refresh tokens.
-- Adds login credentials to users, the login-lookup RLS policies (login starts with
-- only an email/google_sub — no tenant yet), global uniqueness for email/phone/
-- google_sub (phone uniqueness is the anti free-trial-abuse guarantee, 16.13), and
-- the refresh_tokens lifecycle table. See techdocs/auth_techdoc.md.

-- Forward-only migration. Do not edit after it has shipped; add a new file instead.

BEGIN;

ALTER TABLE users
    ADD COLUMN password_hash     TEXT,
    ADD COLUMN google_sub        TEXT,
    ADD COLUMN phone             TEXT,
    ADD COLUMN phone_verified_at TIMESTAMPTZ;

-- Emails are unique per org (0001's UNIQUE(org_id, email)), NOT globally: the same
-- email may hold separate accounts in different orgs (Slack model — user decision
-- 2026-09-20). Login therefore checks the password against every account with the
-- email and asks the client to pick an org when several match.
-- One verified phone = one account. Unique indexes enforce across tenants even
-- under RLS, which is exactly the anti-abuse property we want (16.13).
CREATE UNIQUE INDEX users_phone_unique ON users (phone) WHERE phone IS NOT NULL;
CREATE UNIQUE INDEX users_google_sub_unique ON users (google_sub) WHERE google_sub IS NOT NULL;

-- Login-lookup policies: before a tenant is known, a transaction may expose exactly
-- one user row by setting a transaction-local GUC (never both set with tenant ctx).
--     SELECT set_config('app.login_email', lower('<email>'), true);
--     SELECT set_config('app.login_sub',   '<google_sub>',   true);
-- Policies are OR'd with user_isolation, so tenant-scoped queries are unaffected.
CREATE POLICY user_login_by_email ON users
    USING (lower(email) = current_setting('app.login_email', true));
CREATE POLICY user_login_by_sub ON users
    USING (google_sub IS NOT NULL
           AND google_sub = current_setting('app.login_sub', true));

-- Org names for the login org-picker: during an email login the orgs that hold an
-- account for that email are readable (name only matters; still gated on knowing
-- the email). The users subquery is itself filtered by the policies above.
CREATE POLICY org_login_lookup ON orgs
    USING (id IN (SELECT org_id FROM users
                  WHERE lower(email) = current_setting('app.login_email', true)));

-- Refresh-token lifecycle. The token itself is a signed compact token carrying
-- org/user/jti; this row only tracks rotation/revocation (reuse detection).
CREATE TABLE refresh_tokens (
    id          UUID PRIMARY KEY,                        -- the token's jti
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    rotated_at  TIMESTAMPTZ,                             -- set when exchanged
    revoked_at  TIMESTAMPTZ,                             -- set on logout / reuse
    replaced_by UUID,                                    -- jti of the successor
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);

ALTER TABLE refresh_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY refresh_token_isolation ON refresh_tokens
    USING (org_id = current_setting('app.tenant_id', true)::uuid)
    WITH CHECK (org_id = current_setting('app.tenant_id', true)::uuid);
ALTER TABLE refresh_tokens FORCE ROW LEVEL SECURITY;

COMMIT;
