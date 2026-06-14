# Project Foundations — Techdoc

| | |
| --- | --- |
| **Topic** | `project_foundations` |
| **Roadmap stage** | `ROADMAP.md` Stage 0 — Project Foundations |
| **Feature IDs** | `0.1`–`0.9` → `20.8`, `20.9`, `20.1`, `16.1`, `15.6`, `15.1`, `15.9`, `18.1` |
| **Architecture** | `ARCHITECTURE.md` §9 (data), §13 (multi-tenancy), §16 (observability), §17 (infra), ADR-001/002/006 |
| **Plane** | Cross-cutting (sets up all three planes) |
| **Owner** | Platform |
| **Status** | in-progress |
| **Last updated** | 2026-06-07 |

## 1. Overview
The skeleton every feature plugs into. No product feature ships here — Stage 0
delivers a deployable, multi-tenant Go monorepo with the streaming backbone, the
data stores, auth, tenant isolation, secrets handling, and telemetry. The goal is
that the three architectural invariants are **true from day one**: tenant
isolation, a streaming (WS+gRPC) backbone, and per-stage observability ("instrument
before you optimize").

## 2. Scope
- **In scope:** repo + CI/CD + envs (`0.1`); multi-tenant Postgres with RLS
  (`0.2`/`20.8`); the three stores — Postgres(+pgvector)/Redis/S3 (`0.3`/`20.9`);
  realtime streaming backbone skeleton — WS gateway + gRPC service template
  (`0.4`/`20.1`); auth/sign-up + OAuth (`0.5`/`16.1`); tenant isolation (`0.6`/`15.6`);
  encryption in transit + at rest (`0.7`/`15.1`); secrets manager + rotation
  (`0.8`/`15.9`); telemetry scaffolding — OTel traces + Prometheus metrics
  (`0.9`/`18.1`).
- **Out of scope / deferred:** actual audio capture (Stage 1), STT (Stage 2), any
  product logic. SSO/SAML is V2 (Stage 24); only basic auth/OAuth here. Qdrant is
  deferred — pgvector for now (ADR-006).

## 3. Decisions & rationale (the "why")
- **Modular monolith per plane to start (ADR-001).** Stage 0 stands up *one* Go
  module with a clean package layout, not a fleet of microservices. We peel out
  services only when the §2.3 triggers fire. *Rejected:* full microservices on day
  one — premature ops cost and latency hops with no scale need yet.
- **Go-only backend (ADR-002).** One module, one toolchain. *Rejected:* Python AI
  services — no in-process inference here, so no second language.
- **Tenant isolation via Postgres Row-Level Security, not app-only filters.** RLS is
  enforced by the DB, so a missed `WHERE tenant_id=` in app code cannot leak data.
  The app sets the tenant per transaction via a session GUC. *Rejected:* app-layer
  filtering alone (too easy to forget); schema-per-tenant (operationally heavy at
  our tenant count).
- **`tenant_id` propagated through context → gRPC metadata → event headers → cache
  keys.** A single `platform/tenancy` package owns this so every service does it the
  same way.
- **pgvector in the primary Postgres (ADR-006).** One database to run for the MVP;
  migrate to Qdrant when index size/QPS demand. *Rejected:* standing up a dedicated
  vector engine now.
- **Local dev parity via docker-compose** (Postgres+pgvector, Redis, MinIO for S3,
  NATS). Cloud is AWS/EKS/Terraform; local uses the same interfaces so code is
  portable. *Rejected:* dev-against-cloud (slow, costly, flaky).
- **Telemetry before features.** OTel + Prometheus wired into the service template
  now, so the hot-path latency budget (§5.2) is measurable the moment Stage 1 lands.

## 4. Folder & file structure
What Stage 0 adds (the monorepo root):
```
go.mod / go.sum            # single Go module
Makefile                   # build, test, lint, migrate, run, compose targets
.golangci.yml              # linter config
docker-compose.yml         # local stores: postgres+pgvector, redis, minio, nats
.github/workflows/ci.yml   # CI: build + lint + test

cmd/
  gateway/main.go          # Realtime Gateway entrypoint (WS + health), thin

internal/
  gateway/                 # gateway service-private code (server, ws handler)
  platform/                # shared, cross-service building blocks
    config/                # typed config, env-loaded + validated at boot
    tenancy/               # TenantID context + propagation helpers
    telemetry/             # OTel tracing + Prometheus metrics bootstrap
    auth/                  # session token + OAuth scaffolding (basic)
    db/                    # Postgres pool, RLS session setter
    secrets/               # secrets-manager interface (+ env impl for local)

api/
  proto/                   # gRPC contracts (placeholder for Stage 1+ services)

migrations/
  0001_init.sql            # orgs, users, RLS policies, pgvector extension

deploy/
  terraform/               # IaC skeleton (providers, modules placeholders)
  k8s/                     # manifests/Helm placeholders (hot vs async node pools)
```

## 5. Architecture & data flow
Stage 0 wires the **edges**, not the product flow:
- Client → **Realtime Gateway** (WebSocket, health) → (Stage 1+ will fan to the
  orchestrator over gRPC). Today the gateway authenticates, sets `TenantID` in
  context, and echoes — proving the streaming path + tenancy + telemetry end to end.
- Every request: gateway resolves tenant → sets context → (DB calls set the RLS GUC)
  → OTel span emitted → Prometheus metric recorded.

## 6. Data model & storage
`migrations/0001_init.sql` creates:
- `orgs (id, name, created_at)` — the tenant.
- `users (id, org_id, email, oauth_provider, created_at)` — `org_id` = `tenant_id`.
- **RLS** enabled on tenant-scoped tables with a policy keyed on the
  `app.tenant_id` session setting.
- `CREATE EXTENSION vector;` (pgvector) so Stage 4/5 can add embedding columns.
Stores stood up (no product data yet): Postgres(+pgvector), Redis (cache/session),
S3/MinIO (objects), NATS (events). Retention/consent machinery is deferred to the
consent techdoc; no customer/personal data is stored in Stage 0.

## 7. APIs / events
- **Inbound:** `GET /healthz`, `GET /readyz` (REST); `WS /v1/realtime` (auth +
  tenancy + echo skeleton); `/metrics` (Prometheus).
- **Outbound:** none yet (events begin when the orchestrator emits `call.*`).
- gRPC service template provided in `internal/platform` for Stage 1+ services.

## 8. External dependencies
Local: docker-compose images. Cloud (later): AWS RDS Postgres(+pgvector),
ElastiCache Redis, S3, MSK/NATS, EKS. Libraries: a Go HTTP/WS framework, pgx
(Postgres), OTel SDK, Prometheus client, an OAuth lib.

## 9. Configuration & secrets
- Typed config in `internal/platform/config`, loaded from env, validated at boot.
- Secrets via `internal/platform/secrets` interface — env-backed locally,
  KMS-backed in cloud. Named, never valued, in code/docs. Rotation supported by the
  interface.

## 10. How to extend (for the next agent)
- **New service?** `cp -r` the service template under `cmd/<svc>` + `internal/<svc>`;
  it already wires config, tenancy, telemetry, db. Add its gRPC proto in
  `api/proto/`.
- **New table?** Add a forward-only migration in `migrations/`; if tenant-scoped,
  include `tenant_id` + an RLS policy (copy from `0001_init.sql`).
- **New hot-path step?** Add a stage latency metric + OTel span (see
  `platform/telemetry`) — required by the standards.

## 11. Testing & verification
- Unit tests for config/tenancy/telemetry helpers.
- Integration: bring up docker-compose, run migrations, hit `/healthz` and the WS
  echo, assert an OTel trace + metric are produced.
- **Tenant-isolation test:** two orgs, confirm one cannot read the other's rows with
  RLS on. (Foundational guard for every later data feature.)
- Exit expectation (per `ROADMAP.md` Stage 0): deployable multi-tenant shell with
  auth + streaming backbone; a trace shows end-to-end across services.

## 12. Observability
Service template emits: OTel traces (auto-instrumented HTTP/WS + manual spans),
Prometheus metrics (request count/latency, WS sessions, build info). Grafana
dashboards + the hot-path latency SLO panels are scaffolded for Stage 1 to populate.

## 13. Open questions / TODO
- ✅ **Build/test gate green** — Go 1.26.4; `go mod tidy`, `go build ./...`,
  `go vet ./...`, `go test ./...`, and `gofmt -l` all pass (2026-06-07). `go.sum`
  committed; deps resolved as pinned in `go.mod`.
- **Login/OAuth is intentionally DEFERRED** (product-first decision, 2026-06-07).
  The realtime endpoint runs with `AUTH_DISABLED=true` (dev-only) which injects
  `DEV_TENANT_ID`, so the core pipeline (Stages 1–7) can be built and tested without
  login. Tenancy still flows end to end. Build the real OAuth/login flow at Stage 0.5
  before any non-dev deployment. HMAC session tokens (`platform/auth`) already exist
  as the primitive.
- Decide managed-auth (Ory/Auth0) vs. in-house for SSO later (Stage 24).
- Terraform/k8s are skeletons; full infra hardens through Stages 22/24.
- Integration + tenant-isolation tests (§11) are specified but not yet implemented;
  add once the toolchain is up and docker-compose is running.

## 14. Changelog
- `2026-06-07` — Techdoc created; Stage 0 scaffold started. — setup
- `2026-06-07` — Scaffolded Go monorepo: module `github.com/VanshNarang12/sales-agent`;
  `platform/{config,tenancy,telemetry,db,auth,secrets}`; realtime gateway
  (WS echo + auth + tenancy + metrics); `migrations/0001_init.sql` (orgs/users + RLS);
  docker-compose (postgres+pgvector/redis/minio/nats/otel); Makefile; golangci;
  CI workflow; `.env.example`; tenancy unit tests. — setup
- `2026-06-07` — Verified on Go 1.26.4: `go mod tidy` + `build` + `vet` + `test` +
  `gofmt` all green; `go.sum` generated. Scaffold compiles and runs. — setup
