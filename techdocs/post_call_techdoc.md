# Post-Call Output — Techdoc

| | |
| --- | --- |
| **Topic** | `post_call` |
| **Roadmap stage** | `ROADMAP.md` Stage 10 — Post-Call Output & Customer Memory (first half: 10.1–10.6) |
| **Feature IDs** | `FEATURES.md` — `9.10` meeting-type marker, `9.11` customer tagging, `9.12` persisted summaries, `9.1` summary, `9.2` action items, `9.9` unanswered questions |
| **Architecture** | `ARCHITECTURE.md` §6 |
| **Plane** | Real-Time gateway (trigger) + one non-latency-critical LLM call |
| **Owner** | Vansh |
| **Status** | done (code + tests; live smoke test pending, §13) |
| **Last updated** | 2026-09-14 |

## 1. Overview

**Where we were:** when a call ended, everything disappeared. The transcript
expired from Redis and nothing was kept.

**What this built:** when a **customer** call ends (the WebSocket closes), the
backend reads the full transcript from Redis and makes one LLM call that produces:

- a short summary of the call
- action items / next steps
- questions the rep couldn't answer (content-gap signal)

If the rep tagged a customer name at call start, the digest is saved to Postgres
against that customer — this is the team's memory of the relationship, feeding
the timeline (`10.7`) and the meeting-prep chat (`10.8`,
`customer_memory_techdoc.md`).

**Nothing is shown in the app.** The summary is generated and stored only.
Delivery to humans (e.g. a post-call email) is a future feature.

**Internal meetings produce nothing.** The client marks every call as `internal`
or `customer` at start. Internal = no summary, no storage, transcript expires as
before.

## 1a. Terms used in this doc

- **Meeting-type marker** — a field the client sends when the call starts:
  `internal` (own company) or `customer` (a prospect/client).
- **Transcript store** — the Redis list of everything said this call
  (`internal/transcript`, sliding TTL; see `transcript_store_techdoc.md`).
- **`llm.Completer`** — our one Go interface for "send a prompt, get text back"
  (see `query_extraction_techdoc.md`).

## 2. Scope

**Built:**

- `hello` WS message extended: `meetingType` (`internal` | `customer`, default
  `internal`) and optional `customerName`.
- `internal/postcall` package:
  - `Summarizer` — read full transcript, one LLM call, strict JSON out.
  - `Store` — find-or-create the customer, insert the summary row
    (`migrations/0003_customer_memory.sql`).
- Gateway: when the WS session ends (any reason), customer calls run
  summarize → save. One shot per session, guarded by a flag.
- Transcript store gained `Full()` (whole call; `Window()` only reads a slice).
- Config role: `LLM_SUMMARY_PROVIDER / _MODEL / _BASE_URL / _MAX_TOKENS`.
- Metrics (§12).
- Desktop client: meeting-type dropdown + customer-name field on the connect
  panel, sent on `hello`. Nothing else — no summary UI.

**Not here (and where it lives):**

- Showing/delivering the summary to a human → future (post-call email idea,
  not yet on the roadmap).
- Timeline read API + meeting-prep chat → `customer_memory_techdoc.md`
  (Stage 10 second half: `10.7`, `10.8`).
- Follow-up email draft → Stage 19 (`9.3`). CRM write-back → Stage 19.
- Per-rep scoring/coaching → Stage 21.

## 3. Decisions & rationale (the "why")

**D1 — The trigger is the socket closing. There is no in-call "end call" message.**
Every session end — deliberate stop or crash — goes through the same one path:
WS close → summarize → save. An explicit `end_call` message + a `call_summary`
WS reply were built first, then **removed 2026-09-14 (user decision)**: their only
purpose was showing the summary in the app, which we don't do — summaries are
stored, not displayed. One real path = one path in the code.

**D2 — One LLM call produces summary + action items + unanswered questions.**
Same philosophy as Stage 6 D1: fewest calls that do the job. Not latency-critical
(the call is over). Strict JSON reply, same contract style as the card generator:
unparseable JSON = no summary stored, log a warning — never a malformed summary.

**D3 — "Unanswered questions" come from the same LLM call, not from Suggest outcomes.**
The transcript itself shows where the rep deferred ("I'll get back to you").
Zero new plumbing. The sharper signal — Suggest clicks that returned no card —
is a Stage 13 upgrade when the feedback pipeline lands.

**D4 — Meeting type rides on `hello`, defaulting to `internal`.**
The client already sends `hello` once at call start. Default-internal is the safe
direction: forgetting to tag a call can never cause accidental persistence.

**D5 — Internal calls skip the LLM entirely.**
No summary is generated at all (not generated-then-discarded). Zero cost, zero
data; "internal meetings leave no trace" is enforced in code.

**D6 — New package `internal/postcall`, not inside `suggest` or `detect`.**
One job per package (Stage 6 D6): `suggest` writes in-call cards, `postcall`
writes the after-call digest. Summarizer imports `llm.Completer` + the transcript
`Entry`; Store imports the db helper.

**D7 — Storage is Postgres (same DB as the KB), two tables.**
`customers` (one row per tagged customer, unique per org on `lower(name)` so
"Acme"/"acme" accumulate on one record) + `call_summaries` (summary text,
action items + unanswered as JSONB, FK → customers with cascade delete).
Rejected: MongoDB — a second database to run for relational data that joins,
cascades, and later shares pgvector with the KB. Rejected: customer name as a
plain column on each summary — name drift/typos would split one customer's
history. Same RLS tenant isolation as every other table.

**D8 — Default model: Groq via the existing registry, own `LLM_SUMMARY_*` role.**
Summary quality/cost can be tuned (e.g. a bigger model) without touching the
latency-critical in-call roles.

## 4. Folder & file structure (as built)

```
internal/postcall/
  ├── summarize.go       # Summarizer: full-transcript prompt, LLM call, JSON parse
  ├── summarize_test.go  # fake-LLM tests: good reply, empty call, bad JSON, errors (8 tests)
  └── store.go           # Store: find-or-create customer + insert summary (one tx)
internal/transcript/store.go   # added Full() (whole call, oldest first)
internal/gateway/ws.go         # hello gains meetingType/customerName; session-end defer → runPostcall
internal/gateway/server.go     # Server gained tstore/summarizer/pcStore; New() takes them
cmd/gateway/main.go            # buildTranscriptStore (shared w/ detect), buildPostcall, pcStore
internal/platform/config/config.go  # LLM_SUMMARY_*
migrations/0003_customer_memory.sql # customers + call_summaries (+RLS); applied to dev 2026-09-14
.env.example                   # new vars documented

Sales_Agent_Frontend (separate repo):
src/renderer/control.html      # meeting-type dropdown + customer-name input
src/renderer/control.ts        # sends meetingType/customerName on hello; no summary UI
```

## 5. Architecture & data flow

```
call start: hello {meetingType, customerName?}   → stored on the session struct
call end:   WS closes (stop click, crash, drop — all the same path)
  1. meetingType == internal → do nothing, return
  2. read FULL transcript from Redis (transcript store)
  3. empty transcript → nothing to summarize, return
  4. LLM call: transcript → {summary, action_items, unanswered}   (strict JSON)
  5. customer name tagged → save to Postgres (customers ⭢ call_summaries)
```

Runs once per session (flag-guarded) with its own 60 s context — the WS request
context is already dead when the close fires.

## 6. Data model & storage

`migrations/0003_customer_memory.sql` — see D7. Both tables carry `org_id` with
forced RLS (`app.tenant_id` GUC, `db.WithTenantTx`). Only the digest is stored:
audio and transcripts are never persisted (the Stage-8 promise holds).
Summary text must not appear in logs — log only session id, saved yes/no, latency.

## 7. APIs / events

- **Inbound (WS):** `hello` gained two fields:

```json
{"type": "hello", "...": "...", "meetingType": "customer", "customerName": "Acme Corp"}
```

- **Outbound:** none. No WS reply, no event. The only output is the DB write.
  Reading summaries back (timeline, prep chat) is `customer_memory_techdoc.md`.

## 8. External dependencies

Groq API (default) through the existing `openai_compatible` adapter, selectable
via `LLM_SUMMARY_PROVIDER`. Provider down = warn log + `llm_error` outcome, no
row written; nothing else affected.

## 9. Configuration & secrets

- `LLM_SUMMARY_PROVIDER` — default `openai_compatible`
- `LLM_SUMMARY_MODEL` — default `openai/gpt-oss-120b`
- `LLM_SUMMARY_BASE_URL` — default Groq endpoint
- `LLM_SUMMARY_MAX_TOKENS` — default 1500 (reasoning tokens count against it)
- Secrets: reuses `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`. Server-side only.

## 10. How to extend (for the next agent)

- **Timeline / prep chat (`10.7`/`10.8`):** read path over the same tables —
  create `customer_memory_techdoc.md` first; queries live on `postcall.Store`.
- **Post-call email delivery (future, user-requested):** a worker that reads new
  `call_summaries` rows and emails them — do NOT re-add a WS reply for this;
  the DB row is the hand-off point.
- **Better unanswered-question signal (Stage 13):** track asks whose Suggest
  outcome was `no_hits`/`refused`, merge into the summary payload.
- **Prompt changes:** edit `summarySystemPrompt` in `summarize.go`; add a test
  per behavior change.

## 11. Testing & verification

- **Unit (fake LLM, no network):** 8 tests in `summarize_test.go` — good reply,
  speaker-labeled prompt, empty transcript skips the LLM, bad JSON, empty
  summary field, provider error, nil lists become `[]`, code fences tolerated.
- **Manual (stage exit test, pending):** run a call tagged customer + name, talk
  for a minute, click Stop → within ~10 s a row appears in `call_summaries`
  joined to the right `customers` row. Same call tagged internal → no rows.

## 12. Observability

- `postcall_summary_duration_ms` — histogram, session end → summary ready.
- `postcall_outcomes_total{outcome=…}` — `summary`, `internal_skip`,
  `empty_transcript`, `disabled`, `transcript_error`, `llm_error`, `save_error`.
- Not on the hot path: no latency budget; duration logged per call.

## 13. Open questions / TODO

- **Planned future feature — summary delivery by email (user idea, 2026-09-14):**
  instead of any in-app display, a worker emails the meeting summary insights
  (summary + action items + unanswered questions) after each customer call.
  Sketch: worker picks up new `call_summaries` rows → renders an email → sends to
  the rep (later: configurable recipients per team). Needs an email provider
  (e.g. SES/Resend), a `users`↔session link (auth, Stage 0.5) to know who to
  send to, and a sent/failed marker on the row so nothing is double-sent. Not on
  the roadmap yet — add a feature ID when it's scheduled.
- **Live smoke test not yet run** (§11 manual steps) — last check before ticking
  the stage's exit expectation.
- Very short accidental connects produce tiny summaries — worth a minimum-
  utterance threshold? Decide from real usage.
- The summary prompt was hand-tuned by Vansh (VP persona); revisit wording after
  the first real outputs.

## 14. Changelog

- `2026-09-14` — **Built and wired.** `internal/postcall` (Summarizer + Store,
  8 tests); migration 0003 (customers + call_summaries, RLS) applied to dev;
  `Full()` on the transcript store; hello carries meetingType/customerName;
  session-end defer runs summarize→save (one path); `LLM_SUMMARY_*` config;
  metrics; client sends the two hello fields. **Removed same day (user
  decision):** the explicit `end_call` message, the `call_summary` WS reply, and
  the client summary panel — summaries are stored, not displayed; delivery will
  be a future email feature (D1). — Vansh + Claude
- `2026-09-14` — Created at Stage 10 start with the original two-path design
  (end_call + WS reply). Superseded by the entry above. — Vansh + Claude
