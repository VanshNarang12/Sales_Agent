# Suggestion Trigger — Techdoc

> **File name note:** this file is historically `detection_techdoc.md` (Stage 3 was
> "Detection"). Stage 3 **pivoted from automatic detection to a manual "Suggest"
> button** (see §3 D1 and the `2026-08-16` changelog). The filename/package name
> (`internal/detect`) are kept to avoid a rename storm; the **content** is the source
> of truth: Stage 3 is now the **manual suggestion trigger**, not an auto-detector.

| | |
| --- | --- |
| **Topic** | `suggestion_trigger` (file: `detection_techdoc.md`) |
| **Roadmap stage** | `ROADMAP.md` Stage 3 — Suggestion Trigger (the "Suggest" button) |
| **Feature IDs** | `FEATURES.md` — `3.1` Suggest-button trigger, `3.2` last-N-min window capture, `3.3` light-LLM query builder, `3.4` request/response contract to retrieval, `3.11` button rate-limit / in-flight guard |
| **Architecture** | `ARCHITECTURE.md` — §4 (Suggestion Trigger service), §5.1/§5.2 (hot path + latency budget), §7.2 (trigger design), ADR-001/002/007 |
| **Plane** | Real-Time |
| **Owner** | Vansh |
| **Status** | in-progress (building — repurposing the transcript-window buffer into the button flow) |
| **Last updated** | 2026-08-16 |

## 1. Overview

The suggestion trigger is **how the copilot decides to help — and that decision now
belongs to the rep, not a classifier.** The overlay shows a single **Suggest** button.
When the rep clicks it, the copilot answers whatever the customer is currently asking,
grounded in the company's docs.

We **removed automatic moment detection.** There is no "when did the question end"
heuristic, no objection/competitor/pricing classifier firing cards on its own, no
throttle guessing which partial to act on. The rep is the trigger: they hear the
question, they click, we answer.

On a click, Stage 3 does three things:
1. **Capture** the last **N minutes** of transcript (N configurable) from the rolling
   per-session buffer the gateway already keeps.
2. **Build a query** — a small/cheap LLM reads that noisy window and distills the one
   thing the customer is actually asking into a clean search query.
3. **Hand off** that query to retrieval (Stage 5), which fetches the relevant docs,
   which the answer model (Stage 6) turns into a cited card.

Stage 3 owns steps 1–2 and the request/response contract of step 3. Retrieval (Stage 5)
and the answer LLM (Stage 6) are separate stages — Stage 3 only **triggers and builds
the query**.

## 2. Scope

- **In scope (this stage):**
  - `3.1` **Suggest-button trigger** — a WS control message (`{"type":"suggest",…}`)
    from the overlay button starts one suggestion turn. This is the *only* trigger.
  - `3.2` **Last-N-minutes window capture** — snapshot the rolling per-session
    transcript buffer (both speakers, merged, time-ordered) for a **configurable**
    look-back (`SUGGEST_LOOKBACK_MS`, default e.g. 90 s).
  - `3.3` **Light-LLM query builder** — a small model turns the window into a clean
    retrieval query (`BuiltQuery`), stripping filler/small-talk.
  - `3.4` **Request/response contract** — `SuggestRequest → BuiltQuery`, shaped to be
    the Stage-5 retrieval input and a future gRPC message.
  - `3.11` **Button rate-limit / in-flight guard** — one suggestion turn per session at
    a time (drop or coalesce clicks while a turn is running) + a min-interval floor so a
    double-click doesn't fire two turns.
  - Runs **co-located in the gateway process**, consuming the existing `stt.Manager`
    event fan-out to *fill* the buffer, and the gateway's inbound WS control channel to
    *read* the button click.
  - Dev visibility: print the built query to the gateway console (like the Stage-2
    transcript print).
- **Out of scope / deferred:**
  - **Retrieval + docs fetch** (`5.x` Stage 5) and **answer generation** (`6.x`
    Stage 6) — Stage 3 emits the query, not the answer.
  - **The overlay Suggest button UI + hotkey** (`7.x` Stage 7) — Stage 3 consumes the
    click; it doesn't render the button.
  - **Automatic detection of any moment** (old `3.1`–`3.4` classifiers, `3.5`–`3.10`) —
    **removed, permanently.** The Suggest button is the only live trigger; there is no
    proactive/auto layer and none is planned (D1). Signals that once implied live
    detection (buying-signal, discovery-gap, sentiment, talk-ratio) move to **post-call
    analysis / coaching** (Stage 10/21), not a live listener.
  - **Do-not-say guardrail** (`3.6`, killer #3) — kept, but as a **check on the
    copilot's own suggestion output** (the Guardrail Service vets each card before
    display), **not** live listening for the rep about to misspeak. That distinction is
    what keeps it inside "no auto-detect" (§13).
  - **Auto-appearing competitor battlecard** — dropped as an auto-behavior; the
    battlecard is **pulled via the Suggest button** like any other answer.
  - **Persisting** transcript windows or queries → post-call, later (ephemeral, ADR-008).

## 3. Decisions & rationale (the "why")

**D1 — The trigger is a manual "Suggest" button. Automatic moment detection is removed.**
- **Decision:** the rep clicks **Suggest**; nothing surfaces otherwise. We deleted the
  "detect when a question ends / detect an objection → auto-fire a card" architecture.
- **Why:** knowing *when* a question has ended (or whether a phrase is really an
  objection) is a hard, error-prone real-time problem — end-of-utterance heuristics
  flap, and a wrong auto-fire either interrupts the rep or shows a card for a question
  that was never asked. A human already knows the exact moment they want help; letting
  the rep decide removes an entire class of false positives/negatives and makes the
  product's behavior predictable ("it answers when I ask"). It also sidesteps the
  throttling/over-firing problem — there is nothing to throttle but the button itself.
- **Cost accepted:** the rep must click (a small action, mapped to a hotkey in Stage 7);
  we trade "magically proactive" for "reliably correct and controllable" in the MVP.
- **Alternatives rejected:** *auto end-of-question detection + auto-answer* (the old
  approach — flappy, interrupts, over-fires); *auto-classify every moment type* (four
  classifiers' worth of precision/recall tuning for a trigger the rep can do perfectly
  with one click).

**D2 — Two-model RAG pipeline: a LIGHT LLM builds the query, then RETRIEVAL, then a BIG
LLM answers. Not one big call over the whole transcript, not raw-transcript embedding.**
- **Decision:** click → **light LLM** (`window → search query`) → **retrieval** fetches
  top-K docs for that query → **big LLM** (`query + docs → cited answer`).
- **Why:**
  - **Docs don't fit** — a tenant's full KB can't go in one prompt; we must retrieve the
    relevant few first (this is textbook RAG). The big LLM reads 3–5 chunks, not the KB.
  - **The transcript window is noisy** — filler, half-sentences, cross-talk. A cheap
    model cleaning it into a real question first makes retrieval hit the right chunks;
    embedding the raw window directly retrieves junk.
  - **Cost/latency** — the expensive model runs once, over a small grounded context, not
    over the whole KB or on every turn.
- **Why a light LLM and not just embed the window:** direct embedding of a messy sales
  transcript retrieves poorly (small talk dominates the vector). The light model extracts
  intent first. It is a small, fast connector (see D5), well inside the latency budget.
- **Alternatives rejected:** *single big-LLM call with transcript + all docs stuffed in*
  (won't fit, expensive, worse grounding); *embed the raw transcript window → search*
  (noisy → wrong chunks); *rules/regex to extract the query* (misses paraphrase, the
  same brittleness D1 removed).

**D3 — The look-back window is a configurable last-N-minutes snapshot of the rolling
per-session buffer.**
- **Decision:** the gateway keeps a rolling, time-ordered, per-speaker transcript buffer
  (already built for detection). On a click we snapshot the last `SUGGEST_LOOKBACK_MS`
  (configurable, default e.g. 90 s), merge both speakers in time order, and pass that to
  the query builder.
- **Why:** the customer's question usually spans the last few utterances plus a little
  rep context; a fixed, tunable window captures it without unbounded prompt growth. Made
  configurable because the right look-back differs by call style (fast Q&A vs. long
  monologue) and is a cheap knob to tune against real calls.
- **Alternatives rejected:** *whole-call transcript* (prompt bloat, cost, dilutes the
  actual question); *only the last final utterance* (misses multi-sentence questions and
  the rep's setup); *fixed hard-coded window* (can't tune per deployment).

**D4 — The trigger runs co-located in the gateway, driven by the inbound WS control
channel — not a separate service.**
- **Decision:** the button click arrives as a WS text message on the **same session
  socket** that carries audio; the gateway invokes the in-process trigger. The transcript
  buffer is filled by the same `EventFunc` the `stt.Manager` already calls.
- **Why:** the transcript is already in-process (STT is co-located, transcription D2);
  the click is already on the socket. No network hop, no new service for MVP
  (`ARCH §5` hot-path: co-located, no extra hops). The Orchestrator/Detection split
  (`ARCH §4`) stays a *planned* service; migration path in §10.
- **Alternatives rejected:** *stand up the Orchestrator + a Trigger service now* — right
  long-term shape, premature; adopt when we fan the built query to multiple consumers.

**D5 — Both models are PLUGGABLE connectors selected by config in MongoDB — a common
interface + a provider registry, mirroring `buildSTT`.**
- **Decision:** a `model.Connector` interface (`Complete`, context + a typed request →
  response) has one implementation per provider (`anthropic` first). Two global
  `model_configs` MongoDB docs select the models by **purpose**: `purpose:"query_builder"`
  (the light model, e.g. `claude-haiku-4-5`) and `purpose:"answer"` (the big model, used
  in Stage 6). A **`Registry`** maps `provider → constructor`, exactly like the `buildSTT`
  switch in `cmd/gateway/main.go`. Swapping either model is a **DB edit**; adding a
  provider is *register a constructor + insert a doc*, no change to `engine.go`.
- **Why:** decouples *which* model runs from the trigger logic; lets us downgrade/swap the
  query builder or answer model by editing one global doc; keeps provider API keys
  server-side (secrets vault, keyed by provider — never in Mongo, never on the client).
  Global (no `tenant_id`), loaded at boot.
- **Note:** this is the surviving half of the old D8 (pluggable model connectors). The old
  rules/`detection_rules` collection (old D7) is **removed** — there are no auto-detection
  rules anymore.
- **Alternatives rejected:** *hard-wire the Anthropic SDK* (can't swap without a code
  change); *model id via env only* (swappable model but not provider, not hot-editable).

**D6 — `BuiltQuery` is shaped to become the retrieval input + a gRPC message.**
- **Decision:** the field set (query, source window, span, session/tenant) maps 1:1 to
  what Stage 5 retrieval needs and to the future Orchestrator→Retrieval proto — same
  approach `TranscriptEvent`/the old `DetectedMoment` took (transcription §7).

**D7 — One suggestion turn per session at a time (in-flight guard + min-interval).**
- **Decision:** while a suggestion turn is running for a session, further clicks are
  **dropped** (not queued); a small min-interval floor also debounces double-clicks. This
  is the only "throttle" left — it guards the button, not an auto-firer.
- **Why:** a turn spans a light-LLM call + retrieval + a big-LLM call (~1–3 s); firing a
  second turn on an impatient double-click wastes tokens and races two answers onto the
  overlay. Drop-not-queue keeps the answer tied to *now*, not a stale click.

## 4. Folder & file structure

Go package `internal/detect/` (kept name; per `coding_standards §2`), consumed by the
gateway. Built by **repurposing** the existing transcript-window buffer (previously the
detection engine) into the button-triggered flow.

Build order: reuse the buffer → add the click entrypoint + contract → light-LLM query
builder connector → in-flight guard.

```
internal/detect/
  ├── moment.go            # contract: SuggestRequest, BuiltQuery, Speaker alias, EmitFunc (repurposed from DetectedMoment)
  ├── engine.go            # Engine + per-session rolling transcript buffer (OnTranscript fills it); Suggest() reads it
  ├── engine_test.go       # synthetic-TranscriptEvent tests: buffer fill + window snapshot + Suggest() → BuiltQuery
  ├── window.go            # last-N-min snapshot: merge both speakers in time order, trim to SUGGEST_LOOKBACK_MS (D3)
  ├── window_test.go       # window trimming / speaker-merge / ordering tests with an injectable clock
  ├── query.go             # QueryBuilder: window → search query via the light-model connector (D2)
  ├── query_test.go        # query-builder parse tests with a fake model response
  ├── guard.go             # per-session in-flight guard + min-interval (D7)
  ├── model/
  │   ├── connector.go     # Connector interface (Complete) + Registry (provider → constructor) (D5)
  │   └── anthropic.go     # Anthropic/Claude connector (first provider); key from secrets vault
  └── store/
      ├── mongo.go         # ModelConfigStore (model_configs); global reads, loaded once at boot (D5)
      └── mongo_test.go    # load/decode test against model_config docs
internal/platform/config/config.go # add MONGO_URI, SUGGEST_LOOKBACK_MS, SUGGEST_MIN_INTERVAL_MS
internal/gateway/ws.go              # fill the buffer from the transcript EventFunc; route the inbound {"type":"suggest"} click → Engine.Suggest
cmd/gateway/main.go                 # build ModelConfigStore + Engine at boot; inject into the Server
```

> **Repurposed, not rebuilt:** `engine.go`'s per-speaker rolling buffer and
> `OnTranscript` fill path are kept verbatim — they are exactly the last-N-min buffer the
> button needs. What's **removed** is the auto-emit path (the `Evaluator` loop that fired
> a moment on every transcript event) and the `rules/`, `throttle` machinery. What's
> **added** is `Suggest()` (snapshot → query builder → emit `BuiltQuery`), `window.go`,
> `query.go`, and `guard.go`.

## 5. Architecture & data flow

```
 stt.Manager ── TranscriptEvent (EventFunc, per speaker) ──► detect.Session.OnTranscript(ev)
   (partial/final, speaker, text, span)                          │
                                                                  └─ appends to rolling per-session buffer (both speakers, time-ordered)

 overlay Suggest button ── WS {"type":"suggest"} ──► gateway ──► detect.Session.Suggest(ctx, lookbackMs)
                                                                  │
                                                                  ├─ in-flight guard / min-interval (3.11, D7)
                                                                  ├─ snapshot last-N-min window (D3)
                                                                  ├─ light-LLM query builder      (window → query, D2/D5)
                                                                  └─ EmitFunc(BuiltQuery)
                                                                        │
                                                        today: console log + metrics + WS {"type":"query",…}
                                                        Stage 5: → Retrieval(query) → Stage 6: answer card
```

- **In (two sources):** (1) `TranscriptEvent` from the STT manager fills the buffer — the
  **same in-process fan-out** that feeds `transcriptSink` in `ws.go`; (2) the **Suggest
  click** arrives as an inbound WS control message on the session socket.
- **Out:** `BuiltQuery` via an `EmitFunc` callback. Today it's logged + metriced and
  (optionally) pushed to the client as `{"type":"query",…}` for visibility. In Stage 5 the
  emit becomes the retrieval call.
- **Speaker context:** the window merges **both** channels (customer question + the rep's
  setup) so the query builder has the full exchange. Speaker labels come free from the
  channel (ADR-007) — no diarization.
- **Lifecycle:** the engine is process-wide (like `stt.Manager`); per-session state (the
  rolling buffer + in-flight flag) is created on first transcript and torn down with the
  session.

## 6. Data model & storage

- **No durable storage of call content.** Hot-path and ephemeral (ADR-008). Per session
  the engine keeps a **rolling in-memory transcript buffer** (bounded to a few minutes /
  N chars) plus a single in-flight flag + last-fired timestamp. No transcript or query is
  persisted — nothing hits Postgres/S3 (that's post-call, `9.5`).
- **Config plane — MongoDB (GLOBAL, no `tenant_id`):** the trigger reads only its **model
  selection** from Mongo, loaded **once at service start**:
  - **`model_configs`** — one doc per purpose: `{ _id, purpose:"query_builder"|"answer",
    provider, model, temperature, max_tokens, timeout_ms, enabled }`. Global.
  - Read once at boot into an in-memory connector; a change takes effect on the next
    restart. This is *config*, not customer data.
  - The old **`detection_rules`** collection is **removed** — no auto-detection rules.
- **`tenant_id` propagation:** every `BuiltQuery` and metric label carries `tenant_id` +
  `session_id` (from the call's session). The Mongo config reads are **not** tenant-scoped
  — model config is global system config, so there is no cross-tenant concern.

## 7. APIs / events

- **Inbound (internal):**
  - `detect.Session.OnTranscript(ctx, TranscriptEvent)` — fills the buffer (called from
    the gateway's transcript sink).
  - `detect.Session.Suggest(ctx, lookbackMs int64)` — called when the gateway receives the
    Suggest click; runs the guard + window snapshot + query builder.
- **Outbound (internal event):** `BuiltQuery`, delivered via an `EmitFunc` (Go callback
  today; a gRPC stream message when the Orchestrator is split out).
  ```
  SuggestRequest {
    TenantID   string
    SessionID  string
    LookbackMs int64      // window size; 0 ⇒ SUGGEST_LOOKBACK_MS default (D3)
  }
  BuiltQuery {
    TenantID   string
    SessionID  string
    Query      string     // the clean search query the light LLM produced (D2)
    WindowText string     // the transcript window it was built from (observability / future citation)
    StartMs    int64      // window span, ms from session start
    EndMs      int64
    Source     string     // "model" — which path built it (fixed; only the light-LLM path exists)
  }
  ```
- **External:** the query builder calls the configured **light model** (Anthropic first)
  through the `model.Connector` registry — server-side key, never on the client. The
  answer model call is Stage 6.

## 8. External dependencies

- **MongoDB** (`go.mongodb.org/mongo-driver`) — config plane (D5: `model_configs`).
  **Degradation:** if Mongo is unreachable at boot the engine starts with **no configured
  model**; a click then returns a "suggestions unavailable" event instead of a query — the
  call/audio path is never blocked (same "never fatal" stance as STT's audio-only
  degradation, transcription §8).
- **A configured model provider** (Anthropic first, via the `model.Connector` registry,
  D5) for the query builder. Provider API key comes from the **secrets vault**, keyed by
  provider — never from Mongo, never on the client. **Degradation:** query-builder
  unavailable/timed-out → the click returns an error event; no auto-fallback query (a
  wrong query wastes a retrieval + answer round-trip).

## 9. Configuration & secrets

Named, not valued:
- `SUGGEST_ENABLED` — master switch (default `true`).
- `MONGO_URI` — MongoDB connection string for the config plane (D5). Empty ⇒ no model
  configured, clicks return "unavailable" (never fatal).
- `DETECT_RULES_DB` / `SUGGEST_DB` — Mongo database name (default e.g. `sales_copilot`).
- `SUGGEST_LOOKBACK_MS` — default look-back window for a click (`3.2`, D3), default e.g.
  `90000`. Overridable per-request via `SuggestRequest.LookbackMs`.
- `SUGGEST_MIN_INTERVAL_MS` — min gap between two accepted clicks per session (`3.11`, D7),
  default e.g. `1500`.
- **Model selection is NOT env** — it lives in `model_configs` in Mongo so models are
  swappable without a redeploy (D5).
- **Secret:** the configured model provider's API key comes from the **secrets vault**
  (keyed by provider name, e.g. `ANTHROPIC_API_KEY`) — fetched by the connector, never in
  Mongo, never on the client.

## 10. How to extend (for the next agent)

- **Tune the look-back:** change `SUGGEST_LOOKBACK_MS` (or send `LookbackMs` per click)
  and measure query quality on recorded calls; this is the main knob.
- **Swap/downgrade a model:** edit the `model_configs` doc for `query_builder` (or
  `answer`); no code change (D5). Add a provider = register a constructor + insert a doc.
- **Wire retrieval (Stage 5):** replace the console-log `EmitFunc` with the retrieval call
  — `BuiltQuery` is already the retrieval input (D6).
- **Do NOT add automatic surfacing.** No proactive/auto-detect layer — this is a
  deliberate product decision (D1), not a gap to fill. New live help = new ways to
  trigger the button (e.g. hotkeys, a typed Ask box), never a listener that fires on its
  own.
- **Migrate to the Orchestrator/Trigger service (when fan-out is needed):** move
  `internal/detect` behind a gRPC service; the gateway forwards transcript events + the
  click over gRPC. `BuiltQuery` is already shaped to be that gRPC message (mirrors the STT
  migration, transcription §10).

## 11. Testing & verification

- **Unit:** `window_test.go` — the last-N-min snapshot merges both speakers in time order
  and trims to the look-back, using an **injectable clock** (no real sleeps).
  `query_test.go` — the query builder maps a fake light-model response to a `BuiltQuery`
  and handles a model error. `guard_test.go` — a second click during an in-flight turn is
  dropped; a click inside the min-interval is dropped.
- **Integration (no Deepgram needed):** feed **synthetic `TranscriptEvent`s** into
  `OnTranscript` to fill the buffer, then call `Suggest()` and assert the emitted
  `BuiltQuery` — the same fake-driven pattern the STT tests use, so Stage 3 is fully
  testable before any live call.
- **What "working" looks like (ROADMAP Stage 3 exit):** a Suggest click reliably produces
  a clean, relevant query from the recent transcript; clicks are guarded against
  double-fire; query quality measured on **recorded calls**. Dev visibility:
  `[suggest] session=… query="does the product support SAML SSO?"` on the gateway console.

## 12. Observability

Telemetry-first (`coding_standards §5`). Metrics (labeled `tenant_id`, `source`):
- `suggest_clicks_total` — counter of accepted Suggest clicks.
- `suggest_dropped_total{reason}` — counter (`reason` = `in_flight`|`min_interval`|
  `disabled`|`no_model`) so the guard is measurable, not a black box.
- `suggest_query_latency_ms` — **histogram**, click-received → `BuiltQuery`-emitted (the
  query-builder round-trip); part of the §5.2 end-to-end budget.
- `suggest_query_builder_errors_total` — light-model failures.
- Trace span per suggestion turn, child of the session span (extends the Stage-2 trace so
  per-stage latency is visible end to end through retrieval + generation).
- **No raw transcript/query text in prod logs** (`coding_standards §7`) — the console
  print is a dev-only aid; gate/remove before deploy.

## 13. Open questions / TODO

- **Look-back tuning** (`SUGGEST_LOOKBACK_MS`) — the main quality knob; tune against a
  labeled corpus of recorded calls (too short misses multi-sentence questions, too long
  dilutes the query).
- **Query-builder prompt** — how much rep-context vs. customer-question to weight; whether
  to have it emit a *structured* query (intent + entities) instead of a plain string for
  better retrieval.
- **Do-not-say guardrail** (`3.6`, killer #3) — RESOLVED as a guardrail on the copilot's
  **own suggestion output** (Stage 9 Guardrail Service vets each card before display), not
  a live listener. Open only in *how strict* that output check is, not whether we
  auto-detect (we don't). The **auto-appearing competitor battlecard** is dropped —
  pulled via the Suggest button instead.
- **"Unavailable" UX** — what the overlay shows when Mongo/model is down and a click can't
  build a query.
- **Orchestrator/Trigger service split** (D4) — relocate when fan-out demands it.

## 14. Changelog
- `2026-08-16` — **PIVOT: automatic detection removed; Stage 3 is now the manual
  "Suggest" button.** The old "detect when a question ends / classify objection /
  competitor / pricing → auto-fire a card" architecture is deleted (D1). New flow: rep
  clicks Suggest → snapshot the last-N-min transcript window (configurable, D3) → a
  **light LLM builds a clean search query** (D2) → hand off to retrieval (Stage 5) → big
  LLM answers (Stage 6). Contract changed `DetectedMoment` → `SuggestRequest`/`BuiltQuery`
  (D6). Kept the pluggable model connector (old D8, now D5 — two purposes:
  `query_builder`, `answer`) and the rolling transcript buffer (repurposed from the
  detection engine). **Removed** the `detection_rules` collection, the rules `Evaluator`,
  the auto-emit path, and the throttle (replaced by a simple in-flight guard, D7). Do-not-say
  and auto-battlecard resolved: no proactive/auto layer at all (permanent); do-not-say
  survives as a guardrail on our own suggestion output, competitor battlecard is pulled
  via the button (§13). Config changed
  (`DETECT_*` → `SUGGEST_LOOKBACK_MS`, `SUGGEST_MIN_INTERVAL_MS`). Status: in-progress.
- `2026-07-15` — *(superseded by the 2026-08-16 pivot)* Rules moved from hard-coded Go
  tables to global MongoDB `detection_rules` loaded at boot (old D7); the fuzzy classifier
  became a pluggable `model.Connector` + registry selected from `model_configs` (old D8).
  Build order was skeleton → rules → throttle → model connector.
- `2026-07-14` — *(superseded)* Techdoc created at the start of Stage 3 for the original
  **automatic detection** design: rules/keyword-trackers first + optional Claude Haiku
  fuzzy fallback, no self-hosted DistilBERT (old D1); co-located in the gateway (old D2);
  detect on partials, confirm on final (old D3); throttling as a first-class component
  (old D4); rules gate the classifier (old D5); `DetectedMoment` contract (old D6).
