# Coding Standards — Techdoc

| | |
| --- | --- |
| **Topic** | `coding_standards` |
| **Applies to** | all code in the project |
| **Architecture** | `ARCHITECTURE.md` §19, ADR-002 |
| **Status** | done (living document — update as conventions evolve) |
| **Last updated** | 2026-06-07 |

> The code-writing methods every agent and developer follows on this project. Read
> this before writing any code. Deviations must be justified in the relevant
> feature techdoc.

## 1. Languages & runtimes
- **Backend: Go only**, across all three planes (ADR-002). The AI work is API
  orchestration (STT/LLM/embeddings are bought), which Go does best.
- **Desktop client: TypeScript + React on Electron**; native audio via Node addons
  (CoreAudio Tap on macOS, WASAPI on Windows).
- **Python is not used** unless/until we self-host a model — and then only as an
  isolated sidecar on the **async plane**, never on the hot path.
- **IaC: Terraform. Orchestration: Kubernetes manifests/Helm.**

## 2. Repository layout
Monorepo. Standard Go project layout:
```
/cmd/<service>/         # main entrypoint per service (thin)
/internal/<service>/    # service-private packages (not importable across services)
/internal/platform/     # shared: telemetry, tenancy, auth-context, config, errors
/pkg/                   # genuinely reusable libs (use sparingly)
/api/proto/             # protobuf definitions (gRPC contracts)
/api/openapi/           # REST/OpenAPI specs (control plane)
/migrations/            # SQL migrations (versioned, forward-only)
/deploy/                # Terraform + k8s/Helm
# (desktop client moved 2026-08-26 to its own repo: github.com/VanshNarang12/Sales_Agent_Frontend)
/techdocs/              # one <topic>_techdoc.md per section (see INDEX.md)
```
- A service never imports another service's `/internal`. Cross-service contact is
  gRPC, REST, or events only.

## 3. Naming
- **Go packages:** short, lowercase, no underscores (`retrieval`, not `retrieval_svc`).
- **Files:** `snake_case.go`; tests `_test.go`.
- **Exported identifiers:** `PascalCase` with doc comments; unexported `camelCase`.
- **Protos:** `PascalCase` messages/services, `snake_case` fields.
- **DB:** `snake_case` tables/columns; tables plural (`calls`, `coaching_scores`).
- **Events:** dotted `noun.verb` (`call.ended`, `consent.recorded`).
- **Techdocs:** `topic_name_techdoc.md` (see the `techdoc` skill).

## 4. Multi-tenancy (non-negotiable)
- A `TenantID` rides in the request context end-to-end (set at the gateway, carried
  in gRPC metadata, stamped on every event).
- Every customer-data table has `tenant_id` and a **Postgres Row-Level Security**
  policy keyed on it. Queries never filter tenancy in app code alone.
- Cache keys, vector namespaces, and object-store prefixes are tenant-scoped.
- Write a cross-tenant access test for every data-touching feature; it must fail to
  read another tenant's data.

## 5. The hot path (latency-critical) rules
- gRPC (protobuf) between services; **no REST/JSON on the live suggestion path**.
- Stream, don't batch; emit partial results; render first token early.
- Co-locate RT services (same cluster/AZ). Don't add a network hop without a budget
  line for it (`ARCHITECTURE.md` §5.2).
- Every hot-path component records its **stage latency metric** and an OpenTelemetry
  span. If it's not measured, it's not done.
- Degrade gracefully: if a downstream (LLM) is slow/down, fall back (e.g. show the
  cited snippet) rather than block the call.

## 6. AI / grounding rules
- Retrieval returns **cited** snippets or **nothing** — never an uncited answer
  (`FEATURES.md` 5.4/5.5).
- The **guardrail gate** (groundedness + do-not-say + confidence + PII filter) runs
  before any card is displayed.
- LLM/STT/embeddings go through the **provider gateway** abstraction — no direct SDK
  calls scattered in services. The gateway enforces no-train flags, region pinning,
  and cost/latency telemetry.
- Tier models by path: Haiku (live) / Sonnet (post-call) / Opus (deep analysis).
- Prompt/model changes must pass the **eval suite** in CI (groundedness + latency).

## 7. Error handling & logging
- Wrap errors with context (`fmt.Errorf("...: %w", err)`); never swallow.
- Return typed/sentinel errors across gRPC; map to problem+json at the REST edge.
- **Structured logging** (slog/zerolog), tenant-tagged, **PII-scrubbed**. No secrets
  or raw transcript content in logs.
- Panics never cross a request boundary; recover at handlers.

## 8. Configuration & secrets
- Config via env + typed config struct (validated at boot). No magic literals.
- Secrets only from the secrets manager (KMS-backed); never in code, env files, or
  logs. Reference secrets by name in techdocs, never by value.
- Behavior changes behind **feature flags** for staged rollout.

## 9. Data & migrations
- All schema changes via forward-only versioned migrations in `/migrations`.
- Respect data classification & lifecycle (`ARCHITECTURE.md` §9.2): audio ephemeral
  by default; retention/delete driven by `consent.*` events.

## 10. Testing
- **Unit tests** for logic; **integration tests** for service boundaries (gRPC,
  DB, events) using ephemeral containers.
- **Eval tests** for AI behavior (groundedness, wrong-answer rate, latency) on a
  labeled corpus — gate deploys on them.
- Tenant-isolation test per data feature (see §4).
- Target: meaningful coverage of logic + every public contract; tests run in CI.

## 11. API & contracts
- gRPC for internal; REST/OpenAPI (or GraphQL for dashboards) at the client edge.
- Versioned (`/v1`); idempotency keys on writes; cursor pagination.
- Proto/OpenAPI specs live in `/api` and are the source of truth (generate, don't
  hand-write, clients/stubs).

## 12. Git & reviews
- Trunk-based; short-lived branches; small PRs.
- Commit style: `type(scope): summary` (e.g. `feat(retrieval): hybrid search`).
  Types: feat, fix, refactor, test, docs, chore, perf.
- Every feature PR updates the relevant `*_techdoc.md` and `techdocs/INDEX.md`.
- CI must be green (build, lint `golangci-lint`/`gofmt`, tests, evals where relevant)
  before merge.

## 13. Definition of Done (code)
A change is done when: it meets acceptance criteria; has tests (incl. tenant
isolation if it touches data); is instrumented (traces/metrics); degrades
gracefully; respects consent/privacy settings; its techdoc + the index are updated;
and CI (incl. eval gates on the live path) is green.

## 14. Changelog
- `2026-06-07` — Initial standards established (Go-only backend per ADR-002). — setup
