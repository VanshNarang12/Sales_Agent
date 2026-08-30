# Query Extraction (LLM) — Techdoc

| | |
| --- | --- |
| **Topic** | `query_extraction` |
| **Roadmap stage** | `ROADMAP.md` Stage 5 — Retrieval & Grounding (step `5.2`, built early); also delivers the `6.1` provider abstraction |
| **Feature IDs** | `FEATURES.md` — `5.3` (query refinement, elevated to mandatory extraction), `18.6`/`6.1` (pluggable LLM provider) |
| **Architecture** | `ARCHITECTURE.md` §7.3 (retrieval, first step) |
| **Plane** | Real-Time |
| **Owner** | Vansh |
| **Status** | in-progress |
| **Last updated** | 2026-08-27 |

## 1. Overview
The mandatory first step of retrieval: every Suggest click's raw transcript window is
passed through one small LLM call that extracts *what the prospect is asking or
objecting to*, and only that extracted ask is embedded and searched (Stage 5). This is
the anti-bad-experience gate decided 2026-08-27: heuristics (question detection,
recency weighting) were rejected because the highest-value moments — objections — are
not grammatical questions, and STT output is disfluent. One extraction call handles
questions, objections, and noise uniformly.

## 2. Scope
- **In scope:** the `llm.Completer` provider interface + Anthropic implementation
  (official Go SDK); the `retrieval.Extractor` (system prompt + NONE handling); config
  (`LLM_MODEL`, `LLM_MAX_TOKENS`); `ANTHROPIC_API_KEY` via the secrets store; gateway
  wiring (querySink → extract → log, until search exists).
- **Out of scope / deferred:** embedding + vector search (Stage 4/5), the Stage 6
  answer model (reuses `llm.Completer`), confidence gating (5.5), caching of common
  extractions (15.4).

## 3. Decisions & rationale (the "why")
- **Decision: extraction is mandatory, not a fallback.** Product call (2026-08-27):
  a single irrelevant card in front of a prospect is unacceptable, so the robust path
  ships first; no heuristic stack. The trigger (Stage 3) stays LLM-free — extraction
  lives in retrieval, where query quality is owned and measured.
- **Decision: exactly ONE query-side LLM call (2026-08-30).** Extraction is the only
  model call between click and search. The earlier layered design (3.3 "build" +
  5.2 "refine/expand" as separate calls) is void — two query-side round-trips don't
  fit the 2–4 s budget. Per suggestion: extraction (this step) + Stage-6 answer
  generation = two LLM calls total, one job each. Query-quality problems are fixed in
  this step's prompt or in search itself — never by adding another query LLM call.
- **Decision: extraction returns the ask or NONE.** Small talk / no-ask windows return
  NONE → nothing is searched, nothing shown. Failure degrades to "no card", never
  "wrong card" (with 5.4 citations + 5.5 confidence gate as the other two layers).
- **Decision: pattern stack for ALL model calls (product call 2026-08-29 — "one line
  to add any other model").** *Strategy:* callers depend only on `llm.Completer`.
  *Adapter:* one file per provider adapting its SDK to the interface. *Registry +
  Factory:* providers self-register by name in `init()`; `llm.New(Config)` resolves
  from the registry — adding a provider is one new file whose only integration point
  is a single `llm.Register(...)` line; switching provider/model is an env edit.
  *Role-based config:* call sites are roles (`LLM_EXTRACT_*` now, `LLM_ANSWER_*` at
  Stage 6), each independently pointing at any provider/model. *Decorator:*
  telemetry/timeout wrap the interface once, provider-agnostic.
- **Decision: official Anthropic Go SDK inside the adapter, not hand-rolled HTTP.**
  Retries, typed errors, streaming for later stages come free; mirrors Deepgram.
- **Decision: default = Groq free tier via the `openai_compatible` adapter**
  (`llama-3.3-70b-versatile`; product call 2026-08-29). Why: no free Anthropic/OpenAI
  API tier exists; Gemini's free tier permits Google to train on submitted prompts —
  unacceptable for call transcripts (contradicts roadmap `8.5`/`14.12`); Groq is
  inference-only, fastest (~200–400 ms), most generous free limits. One
  `openai_compatible` adapter covers Groq/DeepSeek/OpenAI/Gemini/Ollama via
  `LLM_EXTRACT_BASE_URL`. The `anthropic` adapter is kept (unused until credits).
  Earlier Haiku-4.5 default superseded — swap back is env-only.
- **Alternatives rejected:** heuristic question-detection + multi-query fusion +
  recency rerank (misses statement-objections, many tunable parts); raw-window
  embedding (multi-topic windows dilute the vector — the original 3.3 concern).

## 4. Folder & file structure
```
internal/llm/
  ├── llm.go             # Completer interface (Strategy), Config, Register/New (Registry+Factory)
  ├── anthropic.go       # Anthropic adapter (official SDK); self-registers in init()
  ├── openai.go          # OpenAI-compatible adapter (Groq/DeepSeek/OpenAI/Gemini/Ollama via BaseURL)
  ├── anthropic_test.go  # httptest round-trips (both adapters) + refusal/error paths
  └── llm_test.go        # registry: resolve, unknown provider, one-line registration
internal/retrieval/
  ├── extract.go       # Extractor: system prompt, BuiltQuery → ask | "" (NONE)
  └── extract_test.go  # fake-Completer tests: ask, NONE, error propagation
```
Touched: `internal/platform/config/config.go` (+`LLM_MODEL`, `LLM_MAX_TOKENS`),
`internal/platform/secrets/keys.go` (+`ANTHROPIC_API_KEY`), `cmd/gateway/main.go`
(buildExtract; nil = disabled, calls unaffected), `internal/gateway/ws.go`
(querySink → Extract → log), `.env.example`.

## 5. Architecture & data flow
```
Suggest click ─► detect.Session.Suggest ─► BuiltQuery (raw window)
                                              │  querySink (gateway)
                                              ▼
                              retrieval.Extractor.Extract
                                 one Messages call: system prompt + window
                                              │
                     ask ("prospect objects that…") ──► [Stage 5 search — pending; logged today]
                     NONE ─► stop (nothing searched, nothing shown)
```
Runs on the Suggest goroutine (off the WS read loop); the in-flight guard stays held
during extraction, so concurrent clicks can't stack LLM calls.

## 6. Data model & storage
None — stateless call. No transcript text persisted by this component; prod logs must
not contain window/ask text (standards §7; dev console print is temporary).

## 7. APIs / events
- **Inbound:** `Extract(ctx, detect.BuiltQuery) (ask string, err error)`.
- **Outbound:** Anthropic Messages API (one call per click). Downstream consumer is
  Stage 5 search (pending); today the ask is logged.

## 8. External dependencies
- `github.com/anthropics/anthropic-sdk-go` (official SDK).
- Degradation: no `ANTHROPIC_API_KEY` at boot → extraction disabled (warn), Suggest
  falls back to logging the raw window; call/audio unaffected. Model error/timeout at
  click time → error logged, no card — never a fabricated query.

## 9. Configuration & secrets
- `LLM_EXTRACT_PROVIDER` (default `openai_compatible`) — registry name for the role.
- `LLM_EXTRACT_MODEL` (default `llama-3.3-70b-versatile`) — model id for the role.
- `LLM_EXTRACT_BASE_URL` (default Groq) — vendor endpoint for openai_compatible.
- `LLM_EXTRACT_MAX_TOKENS` (default 300) — extraction output cap.
- Secrets: `OPENAI_API_KEY` (openai_compatible vendors incl. Groq), `ANTHROPIC_API_KEY`.
- `ANTHROPIC_API_KEY` — secret via `secrets.EnvStore` (vault later); server-side only,
  never shipped to the desktop client (same rule as Deepgram, transcription D2).

## 10. How to extend (for the next agent)
- Stage 5 search: consume the ask where the gateway currently logs it — do not bypass
  extraction; NONE means stop.
- Stage 6 answer model: implement against `llm.Completer` (or extend with a streaming
  method); register construction in `main.go` beside `buildExtract`.
- New provider: add `internal/llm/<provider>.go` implementing `Completer`, with
  `func init() { Register("<name>", factory) }` — that Register line is the only
  integration point; select it via `LLM_<ROLE>_PROVIDER` env.
- Prompt changes: `extract.go`'s system prompt; add eval cases to `extract_test.go`
  and (Stage 13) the groundedness harness before loosening it.

## 11. Testing & verification
- Unit: extractor with a fake Completer (ask / NONE / error); llm client against an
  `httptest` server (parses text block, surfaces refusal/no-text as errors).
- Local: run gateway with `ANTHROPIC_API_KEY` set, connect client, speak an objection
  ("that's way more than your competitor"), click Suggest → gateway logs the extracted
  ask, not the raw window.

## 12. Observability
- Planned with the Stage 3/5 metric set: `extract_latency_ms` histogram,
  `extract_errors_total`, `extract_none_total` (how often clicks find no ask — a
  button-UX signal). Latency budget: extraction is inside the 2–4 s end-to-end target;
  alert if p95 > 800 ms.

## 13. Open questions / TODO
- Should extraction also emit a normalized *topic* (pricing/competitor/security) to
  pre-filter search? Revisit at Stage 5.
- Prompt-injection posture: the window is prospect-controlled speech; the system
  prompt constrains output to a query, and output is never executed — re-review when
  the ask starts driving retrieval filters.

## 14. Changelog
- `2026-08-29` — **Implemented and tested.** Registry/Strategy/Adapter pattern stack in
  `internal/llm` (anthropic + openai_compatible adapters), `retrieval.Extractor` with
  NONE contract, role-based config, gateway querySink wired (extract → log until Stage
  5 search). Default flipped Anthropic→**Groq free tier** via openai_compatible
  (rationale in §3); Gemini rejected for free-tier data-training terms. — Vansh + Claude
- `2026-08-27` — Created at feature start. Decisions: mandatory LLM extraction in
  retrieval (not the trigger), NONE contract, `llm.Completer` provider layer via the
  official Go SDK, config-selectable model. Supersedes the heuristic
  question-detection design discussed the same day. — Vansh + Claude
