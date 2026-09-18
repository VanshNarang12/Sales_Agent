# Suggestion Generation — Techdoc

| | |
| --- | --- |
| **Topic** | `suggestion_generation` |
| **Roadmap stage** | `ROADMAP.md` Stage 6 — Suggestion Generation |
| **Feature IDs** | `FEATURES.md` — `6.1` card, `6.2` talk-track, `6.3` citation, `6.4` confidence, `18.9` confidence gate, `6.12` refusal guardrail, `6.8` latency, `18.6` LLM abstraction (already built in Stage 5) |
| **Architecture** | `ARCHITECTURE.md` §7.4 |
| **Plane** | Real-Time (runs on a Suggest click) |
| **Owner** | Vansh |
| **Status** | done (code + tests; live smoke test pending, §13) |
| **Last updated** | 2026-09-12 |

## 1. Overview

**Where we are:** when the rep clicks Suggest, the backend already finds the right
KB chunks (Stage 5) and sends them raw to the client.

**Problem:** raw chunks are paragraphs from documents. A rep on a live call cannot
read paragraphs.

**What this stage builds:** one LLM call that turns those chunks into a **card**
the rep can read in a glance. A card has:

- 1–3 short bullets (the answer)
- one talk-track line (a sentence the rep can say out loud, word for word)
- citations (which document each point came from)
- a confidence score (how sure we are)

**The rule that makes it safe:** the LLM may only use the chunks we give it.
If the chunks don't answer the question, it must refuse, and the rep sees
"no answer in your docs" — never a made-up answer.

**The speed target:** click → card in 2–4 seconds.

## 1a. Terms used in this doc

- **Chunk** — a small piece (a few paragraphs) of an uploaded document. Documents
  are split into chunks at upload time so search can return just the relevant part.
- **Embedding** — a list of numbers that represents a text's meaning. Texts with
  similar meaning get similar numbers. Made by the Groq embedding model.
- **Cosine similarity** — the score comparing two embeddings. Ranges 0–1 here;
  1 = same meaning. This is the "how good is this match" number from search.
- **`llm.Completer`** — our one Go interface for "send a prompt, get text back".
  Every model provider (Groq, Anthropic, …) is a plug-in behind it, chosen by env vars.
- **querySink** — the gateway function that runs when the rep clicks Suggest.
  It owns the whole click pipeline (steps 1–6 in §5).
- **In-flight guard** — a lock that ignores new Suggest clicks while one is still
  being processed, so LLM calls can't pile up.
- **Talk-track** — one ready-to-say sentence the rep can repeat word for word.
- **Refusal** — the model saying "the chunks don't answer this" instead of guessing.

## 2. Scope

**Build now:**

- `internal/suggest` package with a `Generator` — the one LLM call that makes the card.
- The prompt: "answer only from these chunks, output JSON, refuse if the chunks don't cover it."
- Parsing the LLM's JSON reply into a `Card` struct. Bad JSON = no card.
- Config for the answer model: `LLM_ANSWER_PROVIDER / _MODEL / _BASE_URL / _MAX_TOKENS`.
  (The provider plumbing already exists from Stage 5 — we only add a second role.)
- Confidence gate: if the best retrieval score is too low, don't send a card.
- Gateway wiring: after search, call the Generator, attach the card to the
  `suggestion` WebSocket message.
- Latency metric for the full click → card path.

**Not now (and where it lives):**

- Showing the card on screen → Stage 7 (overlay).
- Do-not-say word filter on cards → Stage 9.
- Streaming the card word-by-word → Stage 15.
- Ranking several alternative cards → Stage 15.
- Matching the rep's tone/brand voice → Stage 25.
- Caching answers to repeated questions → Stage 15.

## 3. Decisions & rationale (the "why")

**D1 — One LLM call here. Two per click in total.**
A Suggest click makes exactly two model calls: extract the question (Stage 5) +
write the card (this stage). Decided 2026-08-30: a third call breaks the 2–4 s
budget. If cards are bad, fix this prompt — never add another call.

**D2 — The LLM sees only the retrieved chunks, never the call transcript.**
The chunks are approved company content. The transcript is not. If we passed the
transcript in, the model could answer from what the prospect said instead of from
the docs — that's the exact hallucination door this stage must keep shut.
The extracted question is passed in as text; that's all the context it gets.

**D3 — The LLM must answer in strict JSON, with a `refuse` field.**
Reply shape: `{"refuse": bool, "bullets": [...], "talk_track": "...", "sources": [0,2]}`.
Why: the gateway decides "show or don't show" by reading a bool in code, not by
guessing from prose. `refuse: true` or unparseable JSON both mean: send no card.
Failure always degrades to "no card", never "wrong card" — same idea as
extraction's NONE contract.

**D4 — Confidence = the top retrieval score. The LLM does not score itself.**
Search already gives every chunk a cosine similarity score (§1a). The best
chunk's score becomes the card's confidence — free, already computed, and it
measures the right thing: how well the docs match the question. LLM self-ratings
are unreliable and cost tokens. Rejected: a separate judge call (a third LLM
call — banned by D1).

**D5 — The LLM picks citations by number, it never writes them.**
Chunks are numbered 0..N in the prompt. The model returns `sources: [0,2]` —
just indexes. The gateway maps each index back to the real document title and
heading from the search results. A model asked to *write* a citation can invent
one; a model that can only *point at a list we control* cannot.

**D6 — New package `internal/suggest`, not inside `retrieval`.**
`kb` writes documents. `retrieval` finds chunks. `suggest` writes the answer.
One job per package. It imports only two things: `llm.Completer` (to call the
model) and `retrieval.SearchResult` (the chunk + citation struct search returns).

**D7 — Default model: Groq `llama-3.3-70b-versatile` (free tier).**
Same choice and reasons as extraction (see `query_extraction_techdoc.md` §3:
no free Anthropic/OpenAI tier; Gemini free tier trains on your data; Groq is
fast and free). Switching later = edit env vars, no code change.

**D8 — The card rides on the existing `suggestion` WS message as a new optional field.**
One click = one reply. The client doesn't have to match up two separate messages.
Rejected: a separate `card` message type — only worth it once we stream (Stage 15).

## 4. Folder & file structure (as built)

```
internal/suggest/
  ├── generate.go       # Generator: prompt, LLM call, JSON parsing, index→citation mapping
  └── generate_test.go  # fake-LLM tests: good card, refusal, bad JSON, bad index, empty hits (9 tests)
internal/gateway/ws.go     # querySink: search → generateCard → card + elapsed_ms on the suggestion message; metrics
internal/gateway/server.go # Server gained the `suggest` field; New() takes the Generator
cmd/gateway/main.go        # buildSuggest next to buildExtract (nil = feature off, raw hits still sent)
internal/platform/config/config.go  # LLM_ANSWER_*, SUGGEST_MIN_CONFIDENCE
.env.example               # new vars documented

Sales_Agent_Frontend (separate repo):
src/renderer/control.ts    # renders the card + the timer (live count-up → server elapsed_ms)
src/renderer/control.html  # card + timer markup and styles in the dev suggest panel
```

## 5. Architecture & data flow

```
Suggest click (WS)
  1. read transcript window from Redis          (Stage 3)
  2. LLM call #1: window → question             (Stage 5)   no question → stop
  3. vector search: question → chunks           (Stage 5)   no chunks  → "no answer"
  4. LLM call #2: question + chunks → card      (THIS STAGE)
       refuse / bad JSON → no card
  5. confidence gate: top chunk score < SUGGEST_MIN_CONFIDENCE → no card
  6. send WS "suggestion" message: {ask, hits, card?}
```

All of this runs on the Suggest goroutine. The existing in-flight guard holds
until step 6, so a second click can't start while one is running.
If step 4 fails, the message still goes out without a `card` — the client shows
its normal "no answer" state.

## 6. Data model & storage

Nothing stored. The call is stateless. Card text must not appear in prod logs
(standards §7) — log only: session id, hit count, refused yes/no, latency.
Saving cards for feedback is Stage 13 (`18.2`).

## 7. APIs / events

- **Inbound (in-process):** `Generate(ctx, ask string, hits []retrieval.SearchResult) (*Card, error)`.
  Returns `nil` card when the model refuses.
- **Outbound (WS, gateway → client):** the existing `suggestion` message gains a `card` field:

```json
{"type": "suggestion", "ask": "...", "hits": [...], "elapsed_ms": 2310,
 "card": {
   "bullets": ["Annual plan is 20% cheaper", "Price-match on 3yr contracts"],
   "talk_track": "If budget is the concern, our annual plan brings that down about 20%.",
   "citations": [{"document_id": "…", "title": "pricing.md", "heading": "Discounts"}],
   "confidence": 0.78}}
```

No `card` field = refused, gated, or generation disabled. Client shows "no answer".
`elapsed_ms` = click received → this write; the client shows it as the answer timer
(live count-up while waiting, server number on arrival).

## 8. External dependencies

- Groq API (default) through the existing `openai_compatible` adapter. Any other
  registered provider selectable via `LLM_ANSWER_PROVIDER`.
- If the provider is down or times out: warn log, message goes out without a card.
  Audio and transcription are untouched.

## 9. Configuration & secrets

- `LLM_ANSWER_PROVIDER` — default `openai_compatible`
- `LLM_ANSWER_MODEL` — default `llama-3.3-70b-versatile`
- `LLM_ANSWER_BASE_URL` — default Groq endpoint
- `LLM_ANSWER_MAX_TOKENS` — default 400
- `SUGGEST_MIN_CONFIDENCE` — default 0.5 (same as `RETRIEVAL_MIN_SCORE`; raise it
  to be stricter about cards than about raw hits)
- Secrets: reuses `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`. Server-side only.

## 10. How to extend (for the next agent)

- **Stage 7 (overlay):** render the `card` field. No card = show the no-answer state.
  Citation fields are already display-ready strings.
- **Stage 9 (do-not-say filter):** insert a check between `Generate` and
  `sendSuggestion` in querySink. The card is plain structured text — easy to vet.
- **Streaming (`6.9`, Stage 15):** add a streaming method to `llm.Completer`,
  send partial card updates as extra WS messages. The card shape can stay.
- **Prompt changes:** edit the system prompt in `generate.go`. Add a test case to
  `generate_test.go` for every behavior you change. From Stage 13 on, run the
  groundedness harness before loosening anything.

## 11. Testing & verification

- **Unit (fake LLM, no network):** good reply → correct Card; `refuse: true` →
  nil card; malformed JSON → nil card + error; source index out of range →
  that index dropped; empty hits → return nil without calling the LLM.
- **Manual (= the stage's exit test):** upload a doc, start a call, say an
  objection the doc answers, click Suggest. Pass = card with bullets +
  talk-track + citation within 2–4 s. Say something off-topic, click Suggest.
  Pass = "no answer", not an invented card.

## 12. Observability (as built)

- `suggest_e2e_duration_ms` — histogram, click received → suggestion WS write.
  This is the `6.8` number. Buckets up to 10 s; target band 2000–4000.
- `suggest_outcomes_total{outcome=…}` — one counter for every click's result:
  `card`, `refused`, `gated`, `no_hits`, `no_ask`, `generation_off`,
  `extract_error`, `search_error`, `generate_error`. (Planned separate
  cards/refusals/gated counters merged into this one labeled counter.)
- Generation duration is logged per click (`suggest: card generated … ms`);
  a dedicated histogram can be added when Stage 13 needs it.
- The same elapsed number is sent to the client (`elapsed_ms`) so the rep-visible
  timer and the metric can't drift apart.
- Latency budget inside the 4 s cap: extract ≤ 0.8 s, search ≤ 0.5 s, generate ≤ 1.5 s.

## 13. Open questions / TODO

- **Live smoke test not yet run** (the §11 manual steps): gateway + desktop
  client, upload a doc, real call, click Suggest, see the card + timer. Unit
  tests and type checks are green; this is the last check before the stage's
  exit expectation ("magic moment" in 2–4 s) is confirmed. ROADMAP `6.8` stays
  `[~]` until p95 is verified on a real call.
- One prompt for both objections and plain questions, or two variants?
  Decide from real-call output; don't pre-build variants.
- Talk-track length: one sentence or two? Tune on real calls; cap it in the prompt.
- `SUGGEST_MIN_CONFIDENCE` starts at 0.5 (a guess). Calibrate in Stage 13 evals.

## 14. Changelog

- `2026-09-12` — **Built and wired.** `internal/suggest` Generator + 9 unit
  tests; `LLM_ANSWER_*` + `SUGGEST_MIN_CONFIDENCE` config; `buildSuggest` in
  main.go; querySink runs generate after search with the confidence gate
  skipping the LLM call; `suggestion` message gained `card` + `elapsed_ms`;
  metrics `suggest_e2e_duration_ms` + `suggest_outcomes_total`; desktop client
  renders the card and an answer timer (live count-up → server elapsed).
  ROADMAP 6.1–6.7 ticked; 6.8 `[~]` until the live smoke test (§13). — Vansh + Claude
- `2026-09-12` — Rewritten in plain language after review: terms primer added
  (§1a), scope split into "build now / not now" one-liners, decisions numbered
  D1–D8. Same decisions, clearer wording. — Vansh + Claude
- `2026-09-10` — Created at Stage 6 start. Decisions D1–D8: one answer call
  (two per click total), chunks-only context, strict JSON with refusal,
  retrieval-score confidence, index-based citations, new `internal/suggest`
  package, `LLM_ANSWER_*` role, card as a field on the existing `suggestion`
  message. — Vansh + Claude
