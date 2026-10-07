# Admin Playbook (Stage 11) — Techdoc

| | |
| --- | --- |
| **Topic** | `admin_playbook` |
| **Roadmap stage** | Stage 11 — Admin & Playbook Authoring |
| **Feature IDs** | `12.1` playbook builder, `12.4` do-not-say editor, `6.5`/`8.3`/`3.6` guardrail (killer #3) |
| **Architecture** | `ARCHITECTURE.md` §4 (control plane), §7.4 (generation) |
| **Plane** | Control (authoring); the block rides the hot path as prompt text |
| **Owner** | Vansh |
| **Status** | in-progress |
| **Last updated** | 2026-10-03 |

## 1. Overview
A sales leader governs what reps see live: an org-wide **guidance** text ("always
mention the 30-day pilot when pricing comes up") and a **do-not-say** rule list
("guaranteed uptime" — legal hasn't approved SLA language). Both are compiled
into one prompt block injected into every Suggest card generation, so the model
writes cards already inside the org's boundaries.

## 2. Scope
- **In:** playbooks + rules storage (0011), CRUD APIs (admin-only writes — the
  first real use of the role column), prompt-block injection into
  `suggest.Generate`, web app Playbook page (admin edit, member read-only).
- **Out:** battlecard/objection editors (dropped 2026-08-30); post-generation
  checking (see §3); desktop changes (none needed — cards ship unchanged).

## 3. Decisions & rationale
- **Prompt-only enforcement (USER DECISION 2026-10-03).** Guidance + do-not-say
  rules are sent to the card LLM as instructions; there is NO post-generation
  matcher, regeneration pass, or "card withheld" state. Rationale: the model
  respecting instructions covers the overwhelming case with zero added latency
  or machinery. Recorded trade-off: prevention without verification — a slip is
  not caught before display. Revisit via Stage-13 evals; a literal or
  LLM-judge check can be layered on without schema changes.
  *Rejected (user):* post-generation literal matching + regenerate-once + withhold.
- **No similarity search.** The playbook is small (one guidance block + dozens of
  rules) and applies to every card — it's injected wholesale. Retrieval is for
  the KB, where only relevant chunks may enter the prompt.
- **Admin-only writes.** PUT/POST/DELETE require `users.role = 'admin'` (org
  creator). Reads are org-wide: members see the playbook, can't edit it. Under
  `AUTH_DISABLED` (dev bypass) the check is skipped.
- **Shared Redis cache, stale-while-revalidate (USER DECISION 2026-10-06).**
  Originally fetched from Postgres per Suggest click ("one indexed read ~ms");
  measured at ~1.9 s on dev — the read is a tenant tx (BEGIN, set_config,
  2 SELECTs, COMMIT = 5 round trips × ~270 ms RTT to Neon us-west-2). The cache
  lives in Redis (already a hot-path dependency via the transcript store) so
  every gateway instance reads the same entry — an in-process map was rejected
  because a write only invalidates the handling instance's memory. Key
  `playbook:<org_id>`, value = playbook + fetched_at. Reads: Redis GET (~ms);
  entries soft-stale after 10 min are served as-is while a single-flight
  (Redis SETNX lock) background re-fetch catches them up — only a tenant's
  first-ever read blocks on the DB. Writes: re-read the DB and overwrite the
  entry (the DB read stays the only code path that builds a playbook, so cache
  and DB can't diverge); re-read failure ⇒ DEL so the next reader fetches.
  24 h hard TTL self-heals a lost write-side refresh. Redis down at boot ⇒ nil
  cache ⇒ every read hits the DB (correct, slower).

## 4. Folder & file structure
```
migrations/0011_playbook.sql        # playbooks (1 row/org) + playbook_rules + RLS
internal/playbook/
  ├── store.go                      # Get/SetGuidance/AddRule/DeleteRule + PromptBlock + Redis cache logic
  ├── cache.go                      # RedisCache adapter (go-redis → Cache interface)
  └── store_test.go
internal/gateway/playbook_handlers.go  # GET/PUT /v1/playbook, POST/DELETE rules
internal/gateway/ws.go              # querySink fetches the block, passes to Generate
internal/suggest/generate.go        # Generate(ctx, ask, hits, playbookBlock)
Sales_Agent_Web/src/pages/Playbook.tsx  # guidance textarea + rules list
```

## 5. Data flow
Suggest click → extract ask → KB search → **fetch playbook (per-tenant cache; tenant tx on miss) → compile
prompt block** → card LLM (system prompt + block) → card → WS send. Authoring:
web Playbook page → CRUD APIs → tables.

## 6. Data model
`playbooks(org_id PK → orgs, guidance TEXT, updated_by → users, updated_at)` —
one row per org, upserted. `playbook_rules(id, org_id, phrase, reason,
created_by, created_at)` — delete = hard delete. RLS: standard tenant policies
on both, FORCEd.

## 7. APIs
- `GET /v1/playbook` → `{guidance, rules: [{id, phrase, reason, created_at}]}` (any member)
- `PUT /v1/playbook` `{guidance}` (admin) → 204
- `POST /v1/playbook/rules` `{phrase, reason}` (admin) → the rule
- `DELETE /v1/playbook/rules/{id}` (admin) → 204
- Non-admin write → 403 `"admin role required"`.

## 8. Prompt block format (compiled by PromptBlock)
```
Org playbook — follow strictly:
<guidance text>
You must NEVER say, promise, or imply any of the following:
- "guaranteed uptime" (legal hasn't approved SLA language)
- "free onboarding" (no longer offered)
```
Empty playbook ⇒ empty block ⇒ prompt unchanged from today.

## 9. Testing
Store CRUD + PromptBlock formatting (unit); suggest prompt-injection test (fake
completer asserts the block lands in the system prompt); handler role gate.

## 10. Changelog
- `2026-10-03` — Created at Stage 11 start; prompt-only enforcement per user
  decision (no post-generation guardrail check). — Claude
