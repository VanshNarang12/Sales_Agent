# Detection — Techdoc

| | |
| --- | --- |
| **Topic** | `detection` |
| **Roadmap stage** | `ROADMAP.md` Stage 3 — Intent & Objection Detection |
| **Feature IDs** | `FEATURES.md` — `3.1` objection, `3.2` buyer-question, `3.3` competitor-mention, `3.4` pricing/discount, `3.11` throttling/relevance gating |
| **Architecture** | `ARCHITECTURE.md` — §4 (Detection Service), §5.1/§5.2 (hot path + latency budget), §7.2 (detection design), §9.3 (playbook entities), ADR-001/002/007 |
| **Plane** | Real-Time |
| **Owner** | Vansh |
| **Status** | in-progress (scope/plan — no code yet) |
| **Last updated** | 2026-07-14 |

## 1. Overview

Detection is the **trigger**: it reads the live transcript arriving from Stage 2 and
decides **when** the copilot should help. It watches both speakers' text as it streams
and flags the four core "moments" — an **objection**, a **buyer question**, a
**competitor mention**, or a **pricing/discount request** — each with a confidence
score and the exact text span that fired it. A relevance/throttling gate then
suppresses noise so the rep isn't flooded.

It is the **first consumer of the transcript stream** and the thing every later stage
waits on: a detected moment is what Stage 5 (retrieval) turns into a query and Stage 6
(generation) turns into a card. In this stage detection only **detects and emits**
moments (logged + surfaced for visibility); wiring a moment through to a retrieved,
generated card is Stage 5/6.

## 2. Scope

- **In scope (this stage):**
  - `3.1` **objection detection**, `3.2` **buyer-question detection**,
    `3.3` **competitor-mention detection**, `3.4` **pricing/discount-request detection**
    — as typed `DetectedMoment` events with confidence + span.
  - `3.11` **throttling / relevance gating** — per-session, per-type debounce + de-dup +
    a max-firings-per-minute ceiling so the same objection doesn't re-fire on every partial.
  - The `DetectedMoment` event contract (shaped to become the Stage-5 retrieval query
    input and a future gRPC message).
  - Runs **co-located in the gateway process**, consuming the existing `stt.Manager`
    event fan-out (the same callback that today feeds `transcriptSink`).
  - Dev visibility: print detected moments to the gateway console (like the Stage-2
    transcript print).
- **Out of scope / deferred:**
  - **Retrieval + cards** (`5.x` Stage 5, `6.x` Stage 6) — detection only emits the trigger.
  - **Risk / red-flag detection** (`3.6`, powers do-not-say) → Stage 9; **buying-signal**
    (`3.5`) and **discovery-gap** (`3.7`) → Stage 14. The `MomentType` enum is designed
    to extend to these without a contract change.
  - **Self-hosted fine-tuned classifier** (DistilBERT-class, `ARCH §7.2`) → a Python
    async-plane sidecar; deferred (Decision D1). MVP is rules + an optional Haiku call.
  - **Configurable trigger phrases / trackers** (`3.10`) and **per-tenant competitor
    lists** → sourced from the Playbook/KB (Stage 4/11); config plumbing is stubbed now.
  - **Detection as a separate service** (`ARCH §4`) → relocated out of the gateway when
    Stage 3+ needs fan-out (Decision D2, migration path in §10).
  - **Persisting** detected moments (the objections-encountered log, `9.5`) → post-call, later.

## 3. Decisions & rationale (the "why")

**D1 — Rules/keyword-trackers first, an optional small Claude Haiku call as the fuzzy
fallback; NOT a self-hosted DistilBERT classifier.**
- **Decision:** high-precision **rules/regex + keyword trackers** (Go, sub-millisecond)
  catch the explicit triggers (competitor names, "too expensive", "what's the price",
  "can you do a discount"); a **small Claude Haiku call** (behind the LLM gateway) handles
  fuzzy/implicit objections only when rules are inconclusive. The `ARCH §7.2`
  DistilBERT-class model is **not** built now.
- **Why:** the backend is **Go-only (ADR-002)** and Python is barred from the hot path
  (`coding_standards §1`) — a self-hosted transformer would need a Python async sidecar and
  serving infra we don't need for MVP precision. Rules give instant, explainable,
  high-precision triggers inside the **50–150 ms** detection budget (`ARCH §5.2`); Haiku
  covers nuance without us hosting a model.
- **Alternatives rejected:** *self-hosted DistilBERT now* (infra + Python on the critical
  path, premature); *LLM-on-every-turn* (latency + per-turn cost; unnecessary when rules
  handle most triggers); *regex-only* (misses paraphrased objections like "I need to run
  this by my team" = stall).

**D2 — Detection runs co-located in the gateway, consuming the existing STT event
fan-out — not a separate Detection Service yet.**
- **Decision:** a `detect.Engine` subscribes to the same in-process `EventFunc` the
  `stt.Manager` already calls per `TranscriptEvent`; no new network hop.
- **Why:** the transcript events are **already in-process** (transcription D2 co-located
  STT in the gateway). Adding a gRPC hop to a standalone Detection Service now spends
  latency budget with no scale need (`ARCH §5` hot-path: co-located, no extra hops), and
  the Orchestrator/Detection split (`ARCH §4`) is still a *planned* service.
- **Alternatives rejected:** *stand up the Detection Service + Orchestrator now* — the
  "right" long-term shape but premature; adopted when Stage 3+ fans transcripts/moments to
  multiple consumers (retrieval, analytics). Migration path in §10, mirroring the STT one.

**D3 — Detect on partials, confirm on settle/final.**
- **Decision:** run the cheap rule fast-path on **every partial** (so Stage 5 retrieval can
  pre-warm before the buyer finishes the sentence, `ARCH §5.2`), but only **emit** a moment
  once the triggering span is stable (the partial stops changing under it) or the segment
  goes final — and never re-emit the same span.
- **Why:** partials are volatile (they rewrite as more audio arrives); firing a card on a
  half-formed partial and retracting it destroys trust. Partials buy latency; finals buy
  stability — we take both.
- **Alternatives rejected:** *finals-only* (loses the pre-warm head start); *every-partial
  emit* (flapping, over-firing).

**D4 — Throttling / relevance gating is a first-class component (`3.11`), not an
afterthought.**
- **Decision:** a `throttle` layer applies, per session and per moment-type: a **debounce
  window** (collapse repeats of the same type/span), **de-duplication** (same normalized
  span already fired → drop), a **min-confidence gate**, and a **max-firings-per-minute
  ceiling**.
- **Why:** over-firing is the fastest way to make the overlay ignorable. A live call
  repeats phrases; without gating, one "it's expensive" streamed across 8 partials/sec
  would fire ~8 cards. Trust depends on the system speaking only when it adds value.
- **Alternatives rejected:** *no gating* (noise); *global (not per-type) cooldown* (a
  competitor mention shouldn't suppress a simultaneous pricing objection).

**D5 — Rules fast-path gates the paid classifier call.**
- **Decision:** the Haiku call (when enabled) runs **only** on windows the rules mark
  ambiguous, and only after passing the throttle — never on every partial.
- **Why:** controls both latency and STT-scale LLM cost; most real triggers are
  rule-catchable, so the LLM is reserved for genuine nuance.

**D6 — `DetectedMoment` is shaped to become the retrieval query + a gRPC message.**
- **Decision:** the field set (type, subtype, span, confidence, speaker, source) maps 1:1
  to what Stage 5 retrieval needs and to the future Orchestrator→Detection proto — same
  approach `TranscriptEvent` took (transcription §7).

## 4. Folder & file structure

New Go package `internal/detect/` (per `coding_standards §2`), consumed by the gateway.

```
internal/detect/
  ├── detect.go        # Detector/Engine interfaces; MomentType, DetectedMoment, EmitFunc, Config
  ├── rules.go         # rule + keyword-tracker fast path: objection/question/pricing cues (Go, sub-ms)
  ├── competitors.go   # per-tenant competitor-name tracker (list stubbed; KB populates it in Stage 4)
  ├── classifier.go    # fuzzy fallback via Claude Haiku behind an interface; DISABLED until the LLM gateway (Stage 6)
  ├── throttle.go      # per-session, per-type debounce + de-dup + min-confidence + rate ceiling (3.11)
  ├── engine.go        # OnTranscript(ev): window → rules → (maybe) classifier → throttle → emit
  ├── rules_test.go    # table-driven detector tests (cues, competitor match, pricing/question)
  └── throttle_test.go # debounce / de-dup / rate-ceiling tests with an injectable clock
internal/gateway/ws.go # wire detect.Engine as a second consumer of the transcript EventFunc (beside transcriptSink)
cmd/gateway/main.go    # build the detect.Engine at boot from DETECT_* config; inject into the Server
```

> The classifier lives behind an interface so Stage 3 ships **rules-only** (no LLM
> dependency). The LLM/STT provider gateway (`ARCH §7.5`, feature `18.6`) formally lands in
> Stage 6; until then `classifier.go` is a no-op implementation selected by config.

## 5. Architecture & data flow

```
 stt.Manager ── TranscriptEvent (EventFunc, per speaker) ──► detect.Engine.OnTranscript(ev)
   (partial/final, speaker, text, span)                          │
                                                                  ├─ rolling transcript window (per session/speaker)
                                                                  ├─ rules fast-path            (Go, sub-ms)
                                                                  ├─ competitor tracker         (per-tenant list)
                                                                  ├─ [ambiguous] Haiku classifier   (Stage 6+, gated by D5)
                                                                  ├─ throttle / relevance gate  (3.11)
                                                                  └─ EmitFunc(DetectedMoment)
                                                                        │
                                                        today: console log + metrics + WS {"type":"moment",…}
                                                        Stage 5: → Retrieval query(context, moment)
```

- **In:** `TranscriptEvent` from the STT manager — the **same in-process fan-out** that
  today feeds `transcriptSink` in `ws.go`. The gateway will call *both* sinks per event.
- **Out:** `DetectedMoment` via an `EmitFunc` callback. Today it's logged + metriced and
  (optionally) pushed to the client as a `{"type":"moment",…}` WS text message for
  visibility, mirroring the Stage-2 transcript print. In Stage 5 the emit becomes the
  retrieval trigger.
- **Speaker routing:** objections/questions/pricing/competitor mentions come from the
  **prospect** channel primarily; the rep channel is watched too (it feeds do-not-say in
  Stage 9). Speaker comes free from the channel label (ADR-007) — no diarization.
- **Lifecycle:** the engine is process-wide (like `stt.Manager`); per-session state (the
  rolling window + throttle memory) is created on first transcript and torn down with the
  session.

## 6. Data model & storage

- **No durable storage.** Detection is hot-path and ephemeral (ADR-008). Per session the
  engine keeps a **short in-memory rolling transcript window** (a few seconds / N tokens)
  plus throttle bookkeeping (last-fired timestamps per type). Nothing hits Postgres/S3.
- **`tenant_id` propagation:** every `DetectedMoment` and metric label carries
  `tenant_id` + `session_id` (tenant isolation is never optional, `coding_standards §4`).
- **Per-tenant trigger data** (competitor names, tracker phrases, objection→type hints)
  will come from the **Playbook/KB** entities (`ARCH §9.3`: `Trigger`, `Battlecard`,
  `ObjectionRule`) once Stage 4/11 exist; **stubbed** from config now.
- Persisting detected moments (objections-encountered log, `9.5`) is post-call, not here.

## 7. APIs / events

- **Inbound (internal):** `detect.Engine.OnTranscript(ctx, TranscriptEvent)` — called from
  the gateway's transcript sink.
- **Outbound (internal event):** `DetectedMoment`, delivered via an `EmitFunc` (Go
  callback today; a gRPC stream message when the Detection Service is split out).
  ```
  MomentType = "objection" | "question" | "competitor" | "pricing"   // extends: risk, buying_signal, discovery_gap
  DetectedMoment {
    TenantID   string
    SessionID  string
    Speaker    Speaker     // from the channel (usually prospect)
    Type       MomentType
    Subtype    string      // e.g. objection→"price"/"timing"/"authority"; competitor→"<name>"
    Text       string      // the triggering span (verbatim transcript slice)
    StartMs    int64       // span start, ms from session start
    EndMs      int64
    Confidence float64     // [0,1]; rule hits are high-confidence, classifier hits vary
    Source     string      // "rule" | "classifier" — which path fired (observability + eval)
  }
  ```
- **External:** none for rules-only. The fuzzy classifier, when enabled, calls **Claude
  Haiku** through the LLM gateway (Stage 6) — server-side key, never on the client.

## 8. External dependencies

- **None** for the rules-only MVP (pure Go).
- **Claude Haiku** (via the LLM gateway, Stage 6) for the fuzzy fallback when enabled.
  **Degradation:** if the classifier is unavailable or times out, detection falls back to
  **rules-only** and the call is never blocked — same "never fatal" stance as STT's
  audio-only degradation (transcription §8).

## 9. Configuration & secrets

Named, not valued:
- `DETECT_ENABLED` — master switch (default `true`).
- `DETECT_CLASSIFIER` — `off` | `haiku` (default `off` until the LLM gateway lands).
- `DETECT_COOLDOWN_MS` — per-type debounce window (`3.11`), default e.g. `4000`.
- `DETECT_MAX_PER_MIN` — max moments emitted per session per minute (`3.11`), default e.g. `12`.
- `DETECT_MIN_CONFIDENCE` — suppress below this (`3.11`), default e.g. `0.5`.
- Per-tenant competitor/trigger lists — from the Playbook/KB (Stage 4/11); **stubbed** now.
- **No new secret** until the Haiku classifier is enabled (it reuses the server-side LLM
  provider key via the gateway).

## 10. How to extend (for the next agent)

- **Add a moment type** (e.g. `risk`, `buying_signal`): add it to `MomentType`, add its cues
  to `rules.go`, add a test row in `rules_test.go`, and (if fuzzy) a branch in the
  classifier prompt. No contract change — `DetectedMoment` already carries `Type`/`Subtype`.
- **Populate real triggers:** wire per-tenant competitor names + tracker phrases from the KB
  (Stage 4) / Playbook admin (Stage 11) into `Config`; `competitors.go` already reads a list.
- **Enable the fuzzy classifier:** implement `classifier.go` against the LLM gateway
  (Stage 6) and set `DETECT_CLASSIFIER=haiku`; it's gated by D5 so it only runs on
  ambiguous, throttle-passed windows.
- **Migrate to the Detection Service (when fan-out is needed):** move `internal/detect`
  behind a `cmd/orchestrator`/`cmd/detection` gRPC service; the gateway forwards
  transcript events over gRPC instead of an in-process callback. `DetectedMoment` is already
  shaped to be that gRPC message (mirrors the STT orchestrator migration, transcription §10).

## 11. Testing & verification

- **Unit:** `rules_test.go` — table-driven cues per type (objection paraphrases, explicit
  pricing/discount asks, question forms, competitor-name matching incl. casing/word
  boundaries). `throttle_test.go` — debounce collapses repeats, de-dup drops a re-fired
  span, the per-minute ceiling caps output, min-confidence filters; uses an **injectable
  clock** (no real sleeps). `classifier` parse tests use a fake LLM response.
- **Integration (no Deepgram needed):** feed **synthetic `TranscriptEvent`s** (partials +
  finals) through `Engine.OnTranscript` and assert the emitted `DetectedMoment`s — the same
  fake-driven pattern the STT tests use, so Stage 3 is fully testable before any live call.
- **What "working" looks like (ROADMAP Stage 3 exit):** the four moments are correctly
  flagged and noise is suppressed; precision/recall measured on **recorded calls**. Dev
  visibility: `[moment] prospect objection(price) 0.91 "it's way too expensive"` on the
  gateway console.

## 12. Observability

Telemetry-first (`coding_standards §5`). Metrics (labeled `tenant_id`, `type`, `source`,
`speaker`):
- `detect_moments_total{type,source}` — counter of emitted moments.
- `detect_latency_ms` — **histogram**, transcript-event-received → moment-emitted; the
  §5.2 **50–150 ms** detection budget line. Buckets straddling 150 ms.
- `detect_suppressed_total{reason}` — counter (`reason` = `cooldown`|`dedup`|`low_confidence`|
  `rate_ceiling`) so throttling is measurable, not a black box.
- `detect_classifier_calls_total`, `detect_classifier_errors_total` — when the Haiku path is on.
- Trace span per detection, child of the session span (extends the Stage-2 trace so per-stage
  latency is visible end to end).
- **No raw transcript text in prod logs** (`coding_standards §7`) — the moment console print
  is the same dev-only aid as the Stage-2 transcript print; gate/remove before deploy.

## 13. Open questions / TODO

- **Precision/recall tuning** needs a **labeled corpus of recorded calls** — the real
  quality gate. Rules will start over-precise (miss paraphrases); tune cues + decide when to
  spend a Haiku call.
- **Partial-vs-final firing policy** (D3): how stable must a partial be before we emit? Tune
  against real transcripts to balance pre-warm latency vs. flapping.
- **Objection subtypes** (price / timing / authority / need) — how granular for Stage 3 vs.
  deferring subtype refinement to Stage 9 guidance.
- **Trigger source:** competitor/tracker lists are stubbed until the KB (Stage 4) and admin
  Playbook (Stage 11) exist.
- **Classifier stays disabled** until the LLM gateway (Stage 6, `18.6`) — Stage 3 ships
  rules-only; revisit once Haiku is reachable.
- **Detection Service split** (D2) — relocate when fan-out demands it.

## 14. Changelog
- `2026-07-14` — Techdoc created at the start of Stage 3. Scope/plan only — no code yet.
  Decisions recorded: rules/keyword-trackers first with an optional Claude Haiku fuzzy
  fallback, **no** self-hosted DistilBERT (D1, Go-only/hot-path); detection **co-located in
  the gateway** consuming the existing `stt.Manager` event fan-out, Detection Service split
  deferred (D2); detect on partials, confirm on settle/final (D3); throttling/relevance
  gating as a first-class component — debounce + de-dup + min-confidence + rate ceiling
  (D4, feature `3.11`); rules fast-path gates the paid classifier call (D5); `DetectedMoment`
  shaped to become the Stage-5 retrieval query + a future gRPC message (D6). Defined the
  `DetectedMoment`/`MomentType` contract, the `internal/detect/` package layout, config
  (`DETECT_*`), the `detect_latency_ms`/`detect_moments_total`/`detect_suppressed_total`
  metrics, and the synthetic-transcript test approach (no Deepgram needed). Status:
  in-progress. — setup