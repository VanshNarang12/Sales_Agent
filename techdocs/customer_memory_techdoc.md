# Customer Memory — Techdoc

| | |
| --- | --- |
| **Topic** | `customer_memory` |
| **Roadmap stage** | `ROADMAP.md` Stage 10 — Post-Call Output & Customer Memory (second half: 10.7 timeline, 10.8 prep chat) |
| **Feature IDs** | `FEATURES.md` — `9.13` customer context timeline, `9.14` meeting-prep chat (+ reads over `9.11`/`9.12` data) |
| **Architecture** | `ARCHITECTURE.md` §6, §9 |
| **Plane** | Control plane (REST reads + one non-latency-critical LLM call per chat turn) |
| **Owner** | Vansh |
| **Status** | done (code + tests; live smoke test pending, §13) |
| **Last updated** | 2026-09-17 |

## 1. Overview

**Where we are:** every tagged customer call now leaves a digest in Postgres
(`customers` + `call_summaries`, see `post_call_techdoc.md`). Nothing reads that
data yet.

**What this builds:** the read side —

- **Timeline (10.7):** REST endpoints to list customers, list a customer's past
  meetings, and open one meeting's digest. Any teammate can see the history
  before walking into a meeting.
- **Prep chat (10.8):** a chat endpoint where a rep preps for the next meeting
  with a customer. Each answer is grounded in two sources at once: that
  customer's past meeting summaries (loaded whole from Postgres) and the
  company's uploaded docs (existing KB vector search).

**UI note:** these are backend endpoints. The chat/timeline UI belongs to the
future web app (decided 2026-09-16: browser = everything except call-time;
desktop = call-time only). Until the web app exists, test via curl.

## 1a. Terms used in this doc

- **Digest / summary row** — one `call_summaries` row: summary text + action
  items + unanswered questions from one customer call.
- **Timeline** — a customer's digests, newest first.
- **Prep chat** — a conversation with the copilot about one specific customer.
- **Grounding** — the only material the model may answer from: the customer's
  digests + retrieved KB chunks.

## 2. Scope

**Build now:**

- New package `internal/customer`:
  - `store.go` — reads over the existing tables: `ListCustomers`,
    `CreateCustomer`, `ListSummaries` (metadata only), `GetSummary` (full).
  - `chat.go` — `PrepChat`: load digests + search KB + one LLM call per turn.
- Five REST endpoints on the gateway (§7), under the existing auth middleware.
- Config role `LLM_CHAT_*` for the chat model.
- Metrics (§12).

**Not now (and where it lives):**

- Chat/timeline UI → the web app (not yet started).
- Persisting chat conversations server-side → later if users want history; the
  client resends the running conversation each turn (D2).
- Backfill embeddings for digests saved before 0004 / after embed failures →
  small script when needed (search skips them; recency still surfaces them).
- Editing/deleting customers & summaries, renames → with the web app's admin UX.
- CRM-sourced deal context → Stage 19 / `25.2`.

## 3. Decisions & rationale (the "why")

**D1 — Five endpoints, split list vs. detail (2026-09-16, user decision).**
List calls return metadata only; the full summary text is fetched one meeting at
a time. A customer with dozens of meetings must not make the list call heavy.

**D2 — Chat is stateless on the server; the client sends the conversation.**
Each `POST .../chat` carries the running message history (capped, D5). No chat
tables, no session state — the simplest thing that works for a v1, and the
history question (persist or not) is deferred until real usage answers it.

**D3 — Chat context = newest digests (guaranteed) + semantically retrieved
digests + KB chunks, every count env-configurable (REVISED 2026-09-17, user
decision — replaces the original "load all digests" design).**
Every digest is embedded at save time (migration 0004: nullable `vector(768)`
on `call_summaries`, same nomic model/space as KB chunks, partial HNSW index).
Per chat turn: the rep's message is embedded ONCE; that vector searches both
the customer's digests and the KB. The newest `CHAT_RECENT_SUMMARIES` digests
are always included regardless of search (the "prep for next meeting" ask has
little wording overlap with the meeting that matters most — the last one), then
`CHAT_TOPK_SUMMARIES` retrieved ones (deduped), then `CHAT_TOPK_CHUNKS` chunks.
Token cost per turn stays flat no matter how many meetings a customer has —
built for scale from day one. A failed embedding never blocks the summary save;
such rows are invisible to search but still surface via recency.

**D4 — KB grounding reuses the existing retrieval path via a new `SearchVec`.**
`retrieval.Searcher` gained `SearchVec(ctx, vec, topK)` (the old `Search`
delegates to it) so the chat embeds the message once and reuses the vector for
both searches. Same min-score gate as Suggest.

**D5 — One LLM call per chat turn, own `LLM_CHAT_*` role.**
Same single-call philosophy as Stages 5/6/10. A separate config role because
chat wants a bigger token budget (long context in, longer answers out) and may
move to a stronger model without touching the in-call roles. History cap:
last `CHAT_MAX_TURNS` messages (default 12) — the customer digests carry the
long-term memory, so old chat turns add little. Prompt layout is cache-friendly:
static grounding (digests, chunks) first, history + new message last.

**D6 — Grounded but conversational: no refusal contract, sources named inline.**
Prep chat is planning help, not a live-call card: the model is told to answer
only from the digests + chunks and to say plainly when they don't cover
something — but replies are free text, not the strict JSON/refusal contract of
Stage 6. The stakes differ: a rep reading prep notes at their desk can push
back; a rep mid-call cannot.

**D7 — New package `internal/customer`, not inside `postcall`.**
One job per package: `postcall` writes the digest at call end; `customer` reads
the memory and talks about it. Both touch the same tables; neither imports the
other.

## 4. Folder & file structure (as built)

```
internal/customer/
  ├── store.go        # ListCustomers/CreateCustomer/ListSummaries/GetSummary +
  │                   # RecentSummaries/SearchSummaries (cosine, per customer) — RLS via WithTenantTx
  ├── store_test.go   # DB tests (skip without TEST_DATABASE_URL; isolation tests fail on dev Neon by design)
  ├── chat.go         # Chat.Answer: embed once → recent+searched digests + chunks + history → one LLM call
  └── chat_test.go    # 6 tests: grounding, dedup, history cap, empty msg, no meetings, LLM error
internal/gateway/customer_handlers.go  # the five REST handlers + prepchat metrics
internal/gateway/server.go     # custStore/chat fields; five routes under authMiddleware
internal/gateway/ws.go         # runPostcall embeds the digest before SaveSummary
internal/postcall/store.go     # SaveSummary(..., embedding) — nil embedding never blocks
internal/retrieval/search.go   # SearchVec(ctx, vec, topK); Search delegates to it
cmd/gateway/main.go            # buildChat wiring; customer.NewStore next to postcall.NewStore
internal/platform/config/config.go  # LLM_CHAT_*, CHAT_RECENT_SUMMARIES, CHAT_TOPK_SUMMARIES, CHAT_TOPK_CHUNKS, CHAT_MAX_TURNS
migrations/0004_summary_embeddings.sql  # nullable vector(768) + partial HNSW; applied to dev 2026-09-17
.env.example
```

## 5. Architecture & data flow

```
Timeline:  GET endpoints → SQL over customers / call_summaries → JSON

Prep chat: POST /v1/customers/{id}/chat {message, history[]}
  1. 404 if the customer isn't this tenant's
  2. embed the message ONCE (query prefix)
  3. newest CHAT_RECENT_SUMMARIES digests (always) +
     top CHAT_TOPK_SUMMARIES by cosine over the customer's digests, deduped (D3)
  4. top CHAT_TOPK_CHUNKS KB chunks via SearchVec with the same vector (D4)
  5. one LLM call: system prompt + digests + chunks + capped history + message
  6. return {answer, sources: doc titles, elapsed_ms}               (D6)
```

## 6. Data model & storage

No new tables. Reads over `customers` and `call_summaries` (migration 0003),
every query inside `db.WithTenantTx` so RLS scopes it. Chat conversations are
not stored (D2). Chat answers must not hit the logs — log ids, counts, latency.

## 7. APIs / events

All under the auth middleware, tenant from context.

- `GET  /v1/customers` — `[{id, name, meetings, last_meeting_at}]`
- `POST /v1/customers` — `{name}` → find-or-create (same casing rule as call
  tagging: unique per org on `lower(name)`) → `{id, name}`
- `GET  /v1/customers/{id}/summaries` — metadata only:
  `[{id, session_id, created_at}]`, newest first
- `GET  /v1/summaries/{id}` — `{id, customer_id, session_id, created_at,
  summary, action_items, unanswered}`
- `POST /v1/customers/{id}/chat` —
  in: `{message, history: [{role: "user"|"assistant", text}]}`
  out: `{answer, sources: [titles], elapsed_ms}`

## 8. External dependencies

The chat LLM via the existing provider registry (`LLM_CHAT_*`, Groq default) and
the existing embedder for KB search. Either down ⇒ chat answers 503; timeline
endpoints are pure SQL and unaffected.

## 9. Configuration & secrets

- `LLM_CHAT_PROVIDER` — default `openai_compatible`
- `LLM_CHAT_MODEL` — default `openai/gpt-oss-120b`
- `LLM_CHAT_BASE_URL` — default Groq endpoint
- `LLM_CHAT_MAX_TOKENS` — default 2500
- `CHAT_RECENT_SUMMARIES` — default 2 (D3, guaranteed recency)
- `CHAT_TOPK_SUMMARIES` — default 5 (D3, semantic retrieval)
- `CHAT_TOPK_CHUNKS` — default 5 (D4)
- `CHAT_MAX_TURNS` — default 12 (D5)
- Secrets: reuses `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`.

## 10. How to extend (for the next agent)

- **Web app UI:** consume these endpoints as-is; nothing server-side should
  need to change for rendering.
- **Chat history persistence:** add a `chat_messages` table + load-instead-of-
  resend; the handler contract can stay identical.
- **Summary search at scale:** if a customer outgrows `CHAT_MAX_SUMMARIES`,
  embed digests into a pgvector column and search them like KB chunks.
- **Prompt changes:** `chat.go` system prompt; add a test per behavior change.

## 11. Testing & verification

- **Unit:** store methods against the tables (pattern of `kb/store_test.go`);
  chat with a fake LLM + fake searcher — grounding text contains digests and
  chunks, history capped, empty-customer case answers without digests.
- **Manual (stage exit test):** after a few tagged test calls: list customers →
  list that customer's meetings → open one → chat "how should I prep for the
  next meeting with them?" and get an answer that cites both a past discussion
  and an uploaded doc.

## 12. Observability

- `prepchat_turn_duration_ms` — histogram per chat turn.
- `prepchat_outcomes_total{outcome=answer|no_customer|llm_error|search_error}`.
- Timeline reads ride the existing HTTP telemetry middleware.

## 13. Open questions / TODO

- Rate limit on the chat endpoint (LLM cost) — revisit with usage metering (16.7).

Resolved (2026-09-16, user decisions):
- `GET /v1/customers` returns **no summary preview** — metadata only.
- Chat tone: **conversation style** — natural back-and-forth prose, not
  bullet-card format (baked into the `chat.go` system prompt).

## 14. Changelog

- `2026-09-17` — **Built and wired.** D3/D4 revised to the embedding design
  (user decision: build for scale, everything configurable): migration 0004
  (vector(768) + partial HNSW) applied to dev; digests embedded at save time in
  runPostcall; `internal/customer` store (6 reads incl. Recent/SearchSummaries)
  + Chat (embed-once, recency + retrieval + chunks, conversation style, 6 unit
  tests); `retrieval.SearchVec`; five REST endpoints + prepchat metrics;
  `LLM_CHAT_*` + `CHAT_*` config. Full repo build/vet/test green. — Vansh + Claude
- `2026-09-16` — Created at 10.7/10.8 start. D1–D7: five-endpoint API (user-
  specified), stateless chat, digests-loaded-whole + KB search grounding, one
  call per turn under a new `LLM_CHAT_*` role, conversational (non-card)
  grounding contract, new `internal/customer` package. — Vansh + Claude
