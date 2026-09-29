# Web App (Marketing Site + Account Console) — Techdoc

| | |
| --- | --- |
| **Topic** | `web_app` |
| **Roadmap stage** | Stage 0.5 (auth UI) + pre-Stage 12 shell (profile/billing placeholders) |
| **Feature IDs** | `16.1`, `16.13` (UI side); placeholders for `16.7`/`16.9` (usage/billing) |
| **Architecture** | `ARCHITECTURE.md` §4 (control plane, REST client) |
| **Plane** | Control (browser client) |
| **Owner** | Vansh |
| **Status** | in-progress |
| **Last updated** | 2026-09-22 |

## 1. Overview
Browser app in the sibling repo folder **`Sales_Agent_Web`** (separate repo; git
wiring by the user). Three jobs: (1) public marketing site (Cluely-style, copy from
`COMPANY.md`), (2) the auth flows against `/v1/auth/*`, (3) the logged-in console —
download the desktop app, dashboard, profile (usage/balance placeholders until
Stage 12).

## 2. Scope
- **In scope:** landing page, signup/login (incl. multi-org workspace picker),
  WhatsApp OTP verify screen, token storage + auto-refresh, route guards, download/
  dashboard/profile pages; meetings page (list + detail, backed by /v1/meetings);
  **Documents page** (plan 2026-09-27, below).
- **Out / next:** Google sign-in button (needs `GOOGLE_CLIENT_IDS`), real download
  links, billing (Stage 12), team invites (Stage 18).

### Customers + prep chat plan (2026-09-28)
- **Route `/customers`, new "Customers" nav tab** — `GET /v1/customers`: name,
  meetings count, last-meeting date; row → `/customers/{id}`.
- **Route `/customers/{id}` — the customer workspace, two panes:**
  - Timeline (left): digests newest-first from `GET /v1/customers/{id}/summaries`
    (`{id, session_id, created_at}`); selecting one fetches
    `GET /v1/summaries/{id}` → summary + action_items + unanswered.
  - Prep chat (right): `POST /v1/customers/{id}/chat`
    `{message, history:[{role,text}]}` → `{answer, sources[], elapsed_ms}`.
    History kept client-side (backend is stateless per turn, caps turns via
    `CHAT_MAX_TURNS`); sources render as doc chips under each answer.
- API client: `customers.list/create`, `summaries.list/get`, `chat.send`; shared
  `useCustomers` query (same pattern as useMeetings/useDocuments).
- Entry points: nav tab; Dashboard/Meetings rows keep linking to /meetings (a
  meeting→customer cross-link can come later — meetings API has no customer_id yet).

### Documents page plan (2026-09-27)
- **Route `/documents`, new "Documents" tab in the AppShell nav** (docs are a core
  object like Meetings — the knowledge every answer is grounded in).
- Page = upload zone (button + drag-and-drop, PDF/TXT/MD, 15 MB cap mirroring the
  backend) with per-file progress, above the document list: title, chunk count
  ("indexed"), **uploaded by** (email; "—" for pre-0008 rows), date, status.
- Backend contract: `POST /v1/documents` (existing, multipart) +
  `GET /v1/documents` (new — see knowledge_base_techdoc.md §7).
- Entry points: Home setup checklist step 01 "Upload your knowledge" links here and
  flips to done when ≥1 document exists; Dashboard "Documents uploaded" tile shows
  the real count and links here. One shared TanStack Query (same pattern as
  useMeetings) feeds all three.

## 3. Decisions & rationale
- **Stack: Vite + React + TS + Tailwind v4 + react-router + TanStack Query.** App
  behind a login → no SSR/Next needed; matches the Electron client's TS+React.
  Pinned `vite@^6` + `@vitejs/plugin-react@^4`: the vite 8 scaffold ships
  rolldown whose native darwin binding failed to install. Node 22 (`.nvmrc`).
- **Tokens in localStorage** (`sc_access`/`sc_refresh`/`sc_scope`), transparent
  401 → refresh → retry in `src/lib/api.ts` with a single-flight refresh promise.
  Rotation reuse-detection lives server-side.
- **Roles are backend-only** (user decision 2026-09-21): no admin/user UI switch;
  `role` is displayed, never branched on yet.
- **Placeholders are honest:** tiles that need metering/billing say so ("live
  after your first call") instead of showing fake numbers.

## 4. Folder & file structure
```
Sales_Agent_Web/src/
  lib/api.ts            # API client, token store, refresh-retry, auth endpoints
  lib/auth.tsx          # AuthProvider (/v1/me) + useAuth
  components/
    Protected.tsx       # guard: no token → /login; pre scope → /verify
    AppShell.tsx        # logged-in top bar + Outlet
    AuthCard.tsx        # shared auth page shell + form primitives
  pages/
    Landing.tsx         # marketing site (hero, steps, features, trust, FAQ, CTA)
    Signup.tsx Login.tsx Verify.tsx      # auth flows
    Download.tsx Dashboard.tsx Profile.tsx
```

## 5. Flow
signup → pre token → `/verify` (phone + OTP, 60 s resend timer) → full pair →
`/download`. Login: 409 with `orgs` ⇒ workspace picker ⇒ retry with `org_id`.
Guarded routes redirect by token scope. Logout revokes the refresh token
best-effort then clears storage.

## 6. Backend touchpoints
`/v1/auth/{signup,login,otp/send,otp/verify,refresh,logout}`, `/v1/me` (now
returns `role`; migration 0006). CORS: gateway `corsMiddleware`, exact-origin
allowlist via `CORS_ALLOWED_ORIGINS` (default `http://localhost:5173`).

## 7. Running locally
```
# backend:  AUTH_DISABLED=false go run ./cmd/gateway   (needs migration 0006)
# web:      cd ../Sales_Agent_Web && nvm use && npm run dev   # http://localhost:5173
```
Dev OTP codes print in the gateway log (log-only sender) until the WABA exists.

## 8. Open questions / TODO
- Google sign-in button once `GOOGLE_CLIENT_IDS` exists.
- Real download URLs when desktop builds are published.
- KB upload / timeline / prep chat screens (next build).
- E2E tests (Playwright) once flows settle.

## 9. Changelog
- `2026-09-22` — Initial build: landing, auth flows (incl. workspace picker + OTP),
  download/dashboard/profile; backend gained `role` (0006) + CORS. — Claude
