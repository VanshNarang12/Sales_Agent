# Auth (Signup, Login, WhatsApp OTP) — Techdoc

| | |
| --- | --- |
| **Topic** | `auth` |
| **Roadmap stage** | `ROADMAP.md` Stage 0 — item 0.5 |
| **Feature IDs** | `FEATURES.md` — `16.1` (email + Google sign-in), `16.13` (WhatsApp OTP phone verification) |
| **Architecture** | `ARCHITECTURE.md` — §4 (control plane), §13 (tenancy); builds on `project_foundations_techdoc.md` |
| **Plane** | Control |
| **Owner** | Vansh |
| **Status** | in-progress |
| **Last updated** | 2026-09-19 |

## 1. Overview
Real sign-up and login for both clients (Electron overlay + upcoming web app).
Before this, the backend ran with `AUTH_DISABLED=true` and a hardcoded dev tenant.
After this, a user can: sign up with email+password **or** Google, verify their
mobile number once via a WhatsApp OTP (anti free-trial-abuse, `16.13`), and get a
short-lived access token + rotating refresh token that every existing endpoint
already accepts (the bearer middleware was built in Stage 0).

## 2. Scope
- **In scope:** manual signup/login (argon2id), Google sign-in (ID-token verify),
  one-time WhatsApp OTP phone verification (Meta Cloud API), pre-auth vs full token
  scopes, refresh-token rotation with reuse detection, `/v1/me`, logout.
- **Out of scope / deferred:** Microsoft OAuth (`16.1`, later), SSO/SAML (`15.2`,
  Stage 24), per-login 2FA (one-time verification only — user decision 2026-09-19),
  password reset over email (needs an email provider; TODO §13), per-IP rate
  limiting at the edge (TODO §13), team invites (Stage 18).

## 3. Decisions & rationale
- **One-time phone verification, not per-login 2FA.** Purpose is anti-abuse: one
  person must not mint many free-tier accounts. A verified phone that is **globally
  unique across all users** makes that expensive. Per-login OTP would add friction
  and per-login WhatsApp cost for no extra anti-abuse value.
- **WhatsApp via Meta Cloud API directly (no BSP middleman).** Cheapest at scale
  (~₹0.12/auth message in India), and Meta's *authentication templates* are
  pre-built fixed-content — approval is near-instant. Business verification (days)
  only gates scale limits, not development: a test number sends to 5 whitelisted
  numbers immediately; an unverified real number gets ~250 conversations/day.
  *Rejected:* Twilio Verify (fast but ~$0.05/verification forever), MSG91/Gupshup
  (still a middleman). The sender is behind an `identity.OTPSender` interface so
  swapping providers is a one-file change.
- **OTP logic is ours, in Redis.** 6-digit code, SHA-256 hash stored (never the
  code), 5-min TTL, 5 attempts, 60-s resend cooldown, daily send cap. Redis because
  it is ephemeral state with TTL semantics — same reasoning as the transcript store.
- **Two token scopes: `pre` and `full`.** Login before phone verification returns a
  `pre` token that can only call the OTP endpoints (and `/v1/me`). After
  verification the client gets a `full` pair. This keeps "logged in but not
  verified" out of every product endpoint — the middleware enforces it once.
- **Access token stays the existing HMAC compact token** (`internal/platform/auth`)
  with `scp` + `jti` claims added. *Rejected:* switching to JWT libs — no consumer
  needs standard JWT, and the primitive already exists with tests.
- **Refresh token = signed token (carries `org`) + DB row keyed by `jti`.** The DB
  row enables rotation, revocation, and **reuse detection** (a rotated token being
  replayed revokes the whole chain — stolen-token defense). Carrying `org` in the
  token means the refresh endpoint can open a tenant-scoped RLS transaction without
  a cross-tenant lookup.
- **Google sign-in = client obtains the Google ID token, backend verifies it**
  (`POST /v1/auth/google`). Works identically for the web app (Google Identity
  Services) and Electron (system-browser PKCE loopback — Google blocks embedded
  webviews). *Rejected:* backend redirect/callback flow — needs per-client redirect
  allowlists and does not fit Electron cleanly.
- **Login lookup under FORCE RLS via a dedicated policy.** `users` is RLS-forced on
  `app.tenant_id`, but login starts with only an email (no tenant). Migration 0005
  adds policies that also expose a row when `app.login_email` / `app.login_sub`
  (transaction-local GUCs) match. *Rejected:* a separate non-RLS credentials table
  (duplicates users), SECURITY DEFINER functions (hides logic in SQL).
- **Emails are unique per org, not globally** (Slack model — user decision
  2026-09-20): the same email may hold separate accounts in different orgs, which
  is what Stage 18 invites need. Consequence: login checks the password against
  every account with the email; several matches → HTTP 409 with the candidate org
  list (`OrgSelectionError`) and the client re-submits with `org_id`. Google
  sign-in auto-links by email only when exactly one account holds it, otherwise
  asks for password login (`ErrEmailAmbiguous`). Phone and `google_sub` stay
  globally unique (phone uniqueness IS the anti-abuse guarantee; unique indexes
  work across tenants regardless of RLS).
- **Signup works inside RLS as-is:** the org UUID is generated in the app, the
  transaction sets `app.tenant_id` to it, then inserts org + first user.
- **Every signup creates a new org** (one signup = one tenant). No auto-grouping by
  email domain — a public-domain email (gmail.com) must never join a stranger's
  org. Joining an existing org arrives with Stage 18 invites. Default org name when
  none is given: `<email local part>'s workspace` (user decision 2026-09-20) — a
  label only, isolation is by UUID.
- **Failed OTP delivery keeps the code + cooldown** (no wipe-and-retry). A
  timed-out Meta call may still have delivered the message; wiping would invalidate
  a code the user is about to type (user decision 2026-09-20). Recovery from a real
  failure is the normal resend after the 60-s cooldown.

## 4. Folder & file structure
```
internal/identity/            # auth service (control plane)
  ├── service.go              # flows: signup, login, google, otp send/verify, refresh, logout
  ├── store.go                # orgs/users/refresh_tokens queries (RLS-aware)
  ├── password.go             # argon2id hash + constant-time verify
  ├── otp.go                  # Redis OTP manager (hash, TTL, attempts, cooldown, cap)
  ├── whatsapp.go             # OTPSender interface + Meta Cloud API impl + dev log sender
  └── google.go               # Google ID-token verifier (go-oidc)
internal/gateway/auth_handlers.go  # HTTP handlers for /v1/auth/* and /v1/me
internal/platform/auth/auth.go     # claims gain scp/jti; scope constants
migrations/0005_auth.sql           # users auth columns, login policies, refresh_tokens
```

## 5. Architecture & data flow
1. **Signup (manual):** `POST /v1/auth/signup` → create org+user (argon2id hash) →
   return `pre` token (phone not verified yet).
2. **Login:** `POST /v1/auth/login` → lookup by email (login-lookup policy) →
   verify password → `phone_verified_at` set? **full pair** : **pre token**.
3. **Google:** client gets Google ID token → `POST /v1/auth/google` → verify
   signature/audience → find by `google_sub`, else link by email, else create
   org+user → same pre/full decision.
4. **OTP:** `POST /v1/auth/otp/send` (pre or full token) → normalize E.164 → checks
   (cooldown, cap, phone not taken) → store hash in Redis → WhatsApp template send.
   `POST /v1/auth/otp/verify` → compare hash, bump attempts → stamp
   `users.phone_verified_at` → return **full pair**.
5. **Refresh:** `POST /v1/auth/refresh` → verify signature → tenant tx from `org`
   claim → row by `jti`: active → rotate (new pair, old row marked, `replaced_by`
   set); already rotated/revoked → **revoke chain**, 401.
6. **Logout:** `POST /v1/auth/logout` → revoke the presented refresh token's row.

## 6. Data model & storage
- `users` (+`password_hash`, `google_sub`, `phone`, `phone_verified_at`) — RLS
  tenant policy + login-lookup policies (plus `org_login_lookup` on `orgs` so the
  org-picker can show names). Unique: email per org (0001), `phone` and
  `google_sub` globally.
- `refresh_tokens` (`id` = jti, `org_id`, `user_id`, `expires_at`, `rotated_at`,
  `revoked_at`, `replaced_by`) — RLS on `org_id`. No token material stored — the
  signature authenticates; the row only tracks lifecycle.
- Redis: `otp:code:<user>` (SHA-256 of code, TTL 5 m), `otp:attempts:<user>`,
  `otp:cooldown:<user>` (60 s), `otp:sends:<user>:<day>` (daily cap). No PII beyond
  the phone number in transit to Meta; codes never logged in prod.

## 7. APIs / events
All REST/JSON, unauthenticated unless noted:
- `POST /v1/auth/signup` `{email, password, org_name}` → `{token(pre)}`
- `POST /v1/auth/login` `{email, password, org_id?}` → `{token(pre)}` or full pair;
  409 `{error, orgs:[{org_id,org_name}]}` when the email spans several orgs and no
  `org_id` was given
- `POST /v1/auth/google` `{id_token, org_name?}` → same as login
- `POST /v1/auth/otp/send` `{phone}` (bearer: pre|full) → `{sent, cooldown_s}`
- `POST /v1/auth/otp/verify` `{code}` (bearer: pre|full) → full pair
- `POST /v1/auth/refresh` `{refresh_token}` → full pair (rotated)
- `POST /v1/auth/logout` `{refresh_token}` → 204
- `GET /v1/me` (bearer) → `{user_id, org_id, email, phone_verified, scope}`
Full pair = `{access_token, refresh_token, expires_in}`.

## 8. External dependencies
- **Meta WhatsApp Cloud API** (`graph.facebook.com`) — authentication template
  send. Degrades: sender unavailable ⇒ OTP send returns 503; login/signup still
  work (user stays on `pre` scope). Dev fallback: log-only sender when no token.
- **Google OIDC** (`accounts.google.com` JWKS) via `github.com/coreos/go-oidc/v3`.
  Degrades: verifier init fails ⇒ `/v1/auth/google` 503; manual login unaffected.
- `golang.org/x/crypto/argon2` (already an indirect dep; now direct).

## 9. Configuration & secrets
Config (env → `config.Config`): `ACCESS_TOKEN_TTL_MIN` (15), `PREAUTH_TOKEN_TTL_MIN`
(10), `REFRESH_TOKEN_TTL_DAYS` (30), `OTP_TTL_SECONDS` (300), `OTP_MAX_ATTEMPTS`
(5), `OTP_RESEND_COOLDOWN_SECONDS` (60), `OTP_DAILY_CAP` (10),
`WHATSAPP_PHONE_NUMBER_ID`, `WHATSAPP_TEMPLATE` (`otp_login`), `WHATSAPP_LANG`
(`en`), `WHATSAPP_API_BASE` (Graph v20.0), `GOOGLE_CLIENT_IDS` (comma-separated:
web + desktop). Secrets (names only): `AUTH_SIGNING_KEY` (existing),
`WHATSAPP_ACCESS_TOKEN`.

## 10. How to extend (for the next agent)
- **New OTP channel (SMS fallback):** implement `identity.OTPSender`, select by
  config in `cmd/gateway/main.go` (pattern: `buildIdentity`).
- **Microsoft login:** copy `google.go` shape — verify the ID token, add a
  `microsoft_sub` column + unique index + login-lookup GUC policy.
- **Per-login 2FA later:** the OTP manager is user-scoped, not signup-scoped — call
  `otp.Send/Verify` from the login flow and gate the full pair on it.
- **New protected endpoint:** wrap with the existing `authMiddleware` — it now also
  injects claims; use `auth.ClaimsFrom(ctx)` for the user id.

## 11. Testing & verification
- Unit: password hash/verify, token scope issue/verify, OTP manager on miniredis
  (expiry, attempts, cooldown, cap), refresh rotation + reuse-revocation with a
  fake store, flows with a fake sender.
- Tenant isolation: refresh_tokens RLS test (known Neon dev caveat: neondb_owner
  has BYPASSRLS — see memory/neon-rls-bypass-deferred).
- Manual: signup → OTP (log sender in dev) → verify → call `/v1/documents` with the
  full token and `AUTH_DISABLED=false`.

## 12. Observability
Auth endpoints ride the existing `telemetry.HTTPMiddleware`. Structured logs:
signup/login/otp events tenant-tagged, **never** the password, code, or full phone
(last 4 digits only). Not on the hot path — no stage-latency budget line.

## 13. Open questions / TODO
- Password reset (needs email provider — pick with Stage 12 onboarding).
- Per-IP rate limiting on auth endpoints (edge/middleware) before public exposure.
- WABA business verification + production template registration (ops task).
- Electron + web client implementations of the flows (frontend repos).
- Turn `AUTH_DISABLED` off as the dev default once clients have login UIs.

## 14. Changelog
- `2026-09-19` — Techdoc created; design agreed (manual+Google, one-time WhatsApp
  OTP via Meta Cloud API, pre/full scopes, rotating refresh tokens). — Claude
- `2026-09-20` — Implemented end to end: migration 0005 (not yet applied to the
  dev DB), `internal/identity` (password/otp/whatsapp/google/store/service),
  gateway `/v1/auth/*` + `/v1/me`, pre/full scope middleware, `cmd/gateway`
  wiring, unit tests green. Pending: apply migration, register WhatsApp template
  (WABA), set `GOOGLE_CLIENT_IDS`, live smoke test, client login UIs. — Claude
- `2026-09-21` — Dropped global email uniqueness for the Slack model (same email,
  many orgs): multi-match login returns the org picker (409 + `org_id` retry);
  Google auto-link only on an unambiguous email. — Claude
- `2026-09-21` — Migration 0005 applied to the dev DB; live smoke test passed:
  signup → pre scope blocked from product APIs (403) → OTP send/verify (dev log
  sender) → full pair → /v1/me → refresh rotation → replay rejected (401) →
  re-login straight to full. Backend done; WABA/Google env + client UIs remain. — Claude
