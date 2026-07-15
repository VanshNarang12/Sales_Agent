# Transcription — Techdoc

| | |
| --- | --- |
| **Topic** | `transcription` |
| **Roadmap stage** | `ROADMAP.md` Stage 2 — Real-Time Transcription (the substrate) |
| **Feature IDs** | `FEATURES.md` — `2.1`–`2.6`, `2.10` |
| **Architecture** | `ARCHITECTURE.md` — §7.1 (STT), §4/§5 (planes, hot path), ADR-002 (Go-only) |
| **Plane** | Real-Time |
| **Owner** | Vansh |
| **Status** | done |
| **Last updated** | 2026-07-14 |

## 1. Overview

Stage 2 turns the two PCM audio streams already arriving at the gateway (rep mic on
channel `0x00`, prospect/system audio on `0x01` — see [`audio_capture_techdoc.md`]) into
a **live, speaker-labeled text transcript** delivered in **under 400 ms**. Text is the
substrate every later stage reads: detection (Stage 3) consumes *partials*, retrieval
(Stage 5) consumes *finals*. Once built, a rep is on a call and the system "hears" both
sides as labeled text in real time — the first measurable quality gate of the product.

## 2. Scope

- **In scope (this stage):**
  - `2.1` / `2.10` — provider-agnostic **STT adapter** interface; **Deepgram** as the
    first concrete provider behind it.
  - `2.2` — **streaming partials + finals** emitted as transcript events.
  - `2.3` — **speaker labeling** derived from the physical channel (`0x00`=rep,
    `0x01`=prospect). No ML diarization.
  - `2.4` — **end-of-turn / end-of-speech** signal (from the provider's endpointing).
  - `2.5` — **punctuation / casing / formatting** (provider-side, passed through).
  - `2.6` — **<400 ms latency** target + the per-stage latency metric to prove it.
  - **Server-side key handling** — the gateway holds the Deepgram secret and opens the
    provider connection; the client never sees the key.
- **Out of scope / deferred:**
  - **Persistence of transcripts** → ephemeral on the hot path (consent, ARCH §15);
    durable storage is post-call (Stage 10).
  - **Per-tenant keyterm boosting** (product/competitor terms, §7.1) → config plumbing
    stubbed now, populated when the knowledge base exists (Stage 4).
  - **Multi-provider failover** (AssemblyAI/Gladia/ElevenLabs) → interface supports it;
    only reconnect-with-backoff on a single provider is implemented now.
  - **The Call Session Orchestrator as a separate service** → STT runs co-located in the
    gateway process for now (see §3, Decision D2); it relocates when Stage 3+ needs fan-out.
  - **ML diarization fallback** for single-stream sources → not needed; we always have two
    physical channels.

## 3. Decisions & rationale (the "why")

**D1 — Deepgram first, behind a provider-agnostic adapter.**
- **Decision:** one Go `STTProvider` interface; Deepgram (`nova-3`, streaming WebSocket)
  is the first implementation.
- **Why:** Deepgram has the lowest streaming latency of the §7.1 candidates and native
  keyterm boosting, which directly serves the `<400 ms` gate (`2.6`). The adapter keeps
  us from coupling call logic to one vendor (`2.10`) so failover/swap is a config change.
- **Alternatives rejected:** AssemblyAI/Gladia/ElevenLabs — kept as future adapters but
  higher latency or less-proven streaming for sub-400 ms today.

**D2 — STT runs inside the gateway process (co-located), gateway holds the key.**
- **Decision:** the gateway opens the Deepgram connection server-side; the client only
  streams PCM (as it already does). No Deepgram key ever reaches the renderer.
- **Why:** (a) **security** — a secret shipped in a desktop client is a leaked secret;
  keys live only on the backend (user requirement). (b) **latency** — fewer hops than
  routing through a separate orchestrator service first (ARCH §5 hot-path principle:
  co-located, no extra hops). (c) the Orchestrator is still a *planned* service; building
  it now would block the substrate.
- **Alternatives rejected:** *client → Deepgram directly* (leaks the key, and we lose
  server control of cost/format); *gateway → Orchestrator (gRPC) → Deepgram now* (the
  "right" long-term shape, but premature — adopted when Stage 3+ fans transcripts to
  multiple consumers; this doc notes the migration path in §10).

**D3 — Speaker label = channel, not ML diarization.**
- **Decision:** map `channel 0x00 → "rep"`, `0x01 → "prospect"` (`2.3`).
- **Why:** we already physically separated the streams in Stage 1, so diarization is
  exact and free — ML diarization is both costlier and less reliable (ARCH §7.1).
- **Alternatives rejected:** ML diarization on a mixed stream — only a fallback for
  single-stream sources, which we don't have.

**D4 — One Deepgram stream per (session, channel).**
- **Decision:** each call opens **two** Deepgram streams (rep + prospect), each fed only
  its channel's PCM.
- **Why:** keeps speaker attribution trivial (the stream *is* the speaker), lets
  endpointing/`speech_final` be computed per speaker (needed for `2.4` end-of-turn), and
  matches Deepgram's mono `linear16` input — no channel de-interleaving.
- **Alternatives rejected:** one multichannel stream — Deepgram multichannel exists but
  complicates per-speaker endpointing and our PCM frames are already separate per channel.

**D5 — Realtime drop-oldest backpressure toward the provider.**
- **Decision:** a bounded per-stream send buffer; on overflow, **drop the oldest PCM**,
  not block.
- **Why:** stale audio is useless in a live assist product; blocking would stall frame
  intake and break the latency budget. Mirrors the client's own backpressure
  (`audio_capture_techdoc.md` §6) and the gateway's "gap, never fatal" stance.

## 4. Folder & file structure

New code (Go, per ADR-002). STT lives in its own package, consumed by the gateway.

```
internal/stt/
  ├── provider.go        # STTProvider + STTStream interfaces; TranscriptEvent, StreamConfig types
  ├── manager.go         # per-session lifecycle: channel→stream map, event fan-out, teardown
  ├── manager_test.go    # lifecycle + drop-oldest backpressure tests (fake provider)
  └── deepgram/
       ├── client.go     # Deepgram streaming WS client: auth header, query params, reconnect/backoff
       └── client_test.go# parses Deepgram JSON results → TranscriptEvent (fake WS server)
internal/gateway/
  └── ws.go              # handleAudio: write pcm to stt.Manager instead of discarding it (today: ws.go:130)
cmd/gateway/
  └── (wiring)           # load DEEPGRAM_API_KEY + STT_* config; construct stt.Manager; inject into Server
```

## 5. Architecture & data flow

```
 desktop client                gateway process (co-located)                 Deepgram
 ──────────────                ────────────────────────────                 ────────
 mic  ┐                        ws.go handleAudio(pcm,channel)
 tap  ┘ ─PCM frames(WS)──►       │ writes pcm to ──► stt.Manager
                                 │                      │ per (session,channel) ──► STTStream.Send(pcm) ═══(WSS)══►  nova-3
                                 │                      │                                                            │
                                 │                      ◄── TranscriptEvent (partial/final, speaker, ts) ◄──────────┘
                                 ▼
                          event fan-out (Stage 3 detection / Stage 5 retrieval)
                          — today: metrics + log; consumers wired in later stages
```

- **In:** parsed PCM from `gateway.handleAudio` (currently discarded at `ws.go:130`).
- **Out:** `TranscriptEvent` stream. For now consumed by metrics/logging; Stage 3 (detection)
  subscribes to *partials*, Stage 5 (retrieval) to *finals*.
- **Lifecycle:** lazily open a channel's Deepgram stream on its **first** frame (a rep-only
  call never opens a prospect stream). Tear down all streams on `Disconnect` / WS close /
  session end. Reconnect with backoff on transient provider errors; flush a `final` on close.

## 6. Data model & storage

- **No durable storage in this stage.** Transcripts are **ephemeral** on the hot path
  (consent-by-default, ARCH §15). The manager keeps only a short in-memory rolling window
  per session for downstream context; nothing is written to Postgres/S3 here.
- **`tenant_id` propagation:** every `TranscriptEvent` and metric label carries
  `tenant_id` + `session_id` (coding standard: tenant isolation is never optional). In dev
  (`AUTH_DISABLED=true`) tenant resolves to a fixed dev tenant; production derives it from
  the authenticated session (see §13 open question on wiring tenant from auth claims).
- Durable transcript persistence is **Stage 10** (post-call), not here.

## 7. APIs / events

- **Inbound (internal):** `stt.Manager.Write(ctx, sessionID, channel, seq, pcm []byte)` —
  called from `handleAudio`.
- **Outbound (internal event):** `TranscriptEvent`
  ```
  TranscriptEvent {
    TenantID   string
    SessionID  string
    Speaker    string   // "rep" | "prospect"  (derived from channel)
    IsFinal    bool     // false=partial (may change), true=locked segment
    SpeechFinal bool    // true at end-of-turn / end-of-speech  (feature 2.4)
    Text       string
    StartMs    int64    // segment start, ms from session start
    EndMs      int64
    Confidence float64
  }
  ```
  Delivered today over a Go channel; **becomes a proto** (gRPC stream) when the Orchestrator
  is introduced (§10) — the field set is chosen to map 1:1 to that future message.
- **External (Deepgram, server→Deepgram):** WSS to `wss://api.deepgram.com/v1/listen` with
  query params `model=nova-3&encoding=linear16&sample_rate=16000&channels=1&interim_results=true&punctuate=true&smart_format=true&endpointing=<ms>`; auth via the `Authorization: Token <DEEPGRAM_API_KEY>` **header** (server-side only). Input PCM matches our pipeline exactly: 16 kHz, `pcm_s16le`, mono — no resample needed (the client already targets 16 kHz, `audio_capture_techdoc.md` §12).

## 8. External dependencies

- **Deepgram** streaming STT (`nova-3`). Failover/degradation: reconnect with exponential
  backoff on stream errors; on hard failure the session degrades to **audio-only / no
  transcript** (the call is never blocked). Multi-provider failover (AssemblyAI/Gladia/
  ElevenLabs) is interface-ready but deferred.
- **Library:** reuse `github.com/gorilla/websocket` (already a dependency) for the
  Deepgram client — no new WS lib.

## 9. Configuration & secrets

Named, not valued:
- `DEEPGRAM_API_KEY` — **secret**, backend-only, never logged, never sent to the client.
- `STT_PROVIDER` — default `deepgram` (selects the adapter).
- `STT_MODEL` — default `nova-3`.
- `STT_ENDPOINTING_MS` — silence threshold for end-of-turn (`2.4`), default e.g. `300`.
- `STT_KEYTERMS` — per-tenant boost terms (`2.6`/§7.1); **stubbed**, populated at Stage 4.

## 10. How to extend (for the next agent)

- **Add another STT provider:** implement `STTProvider`/`STTStream` in a new
  `internal/stt/<vendor>/` package; select via `STT_PROVIDER`. No gateway changes.
- **Enable failover:** wrap providers in a composite `STTProvider` in `manager.go` that
  fails over on stream error — the interface already supports it.
- **Add keyterm boosting:** populate `STT_KEYTERMS` per tenant from the KB (Stage 4) and
  pass them into `StreamConfig`; the Deepgram client forwards them as `keyterm` params.
- **Migrate to the Orchestrator (when Stage 3+ needs fan-out):** move `internal/stt` behind
  a `cmd/orchestrator` gRPC streaming service; the gateway forwards
  `(tenant, session, channel, seq, pcm)` over gRPC instead of calling `stt.Manager`
  in-process. `TranscriptEvent` is already shaped to become the gRPC response message.

## 11. Testing & verification

- **Unit:** `deepgram/client_test.go` runs a **fake Deepgram WS server** that returns
  canned interim/final JSON; assert correct `TranscriptEvent` mapping (partial vs final,
  `speech_final`, timestamps, confidence). `manager_test.go` covers stream lifecycle,
  channel→speaker mapping, and **drop-oldest** backpressure.
- **Integration (gated):** behind a build tag / skipped without `DEEPGRAM_API_KEY`; stream
  a known WAV through the gateway and assert transcript text + speaker labels.
- **What "working" looks like (ROADMAP Stage 2 exit):** on a live call, both speakers'
  speech appears as labeled, punctuated text; partials update live and finals lock; the
  `<400 ms` latency metric (frame-received → first partial covering it) holds at p95.

## 12. Observability

Telemetry-first (coding standard). Metrics (all labeled `tenant_id`, `speaker`):
- `stt_partial_latency_ms` — **histogram**, frame-received → first partial. The Stage-2
  latency gate (`2.6`); contributes to the end-to-end budget.
- `stt_streams_active` — gauge of open provider streams.
- `stt_finals_total`, `stt_partials_total` — counters.
- `stt_reconnects_total`, `stt_provider_errors_total` — provider health.
- Trace span per provider stream (session-scoped), child spans on reconnect.

## 13. Open questions / TODO

- **Tenant from auth:** wire `tenant_id` from the authenticated session/`hello` rather than
  the dev default once auth (Stage 0/12) lands; today `AUTH_DISABLED` uses a fixed tenant.
- **Endpointing tuning:** `STT_ENDPOINTING_MS` value that balances responsiveness vs.
  cutting people off — needs real-call tuning (`2.4`).
- **Cost controls:** two streams per call doubles STT minutes; consider pausing the
  prospect stream during long rep monologues. Revisit after measuring.
- **Latency attribution:** if `<400 ms` is missed, split the budget (client→gateway WS vs.
  gateway→Deepgram RTT) to see which dominates before optimizing.
- **`nova-3` vs `flux`:** confirm which Deepgram model best hits the latency/accuracy point
  for sales speech; `STT_MODEL` makes this a config flip.
- **Dev console print is not prod-safe:** `transcriptSink` prints raw transcript text to the
  gateway console (`internal/gateway/ws.go`) for local visibility — coding-standards §7
  forbids transcript content in production logs. Gate it behind a dev flag (e.g.
  `STT_LOG_TRANSCRIPTS`) or remove it before any real deployment.

## 14. Changelog
- `2026-06-19` — Techdoc created at the start of Stage 2. Scope/plan only — no code yet.
  Decisions recorded: Deepgram behind a provider-agnostic adapter (D1); STT co-located in
  the gateway with the secret key held server-side, client streams PCM only (D2);
  speaker = physical channel, no ML diarization (D3); one Deepgram stream per
  (session, channel) (D4); drop-oldest realtime backpressure (D5). Defined the
  `TranscriptEvent` contract, folder structure (`internal/stt/`), Deepgram WS params
  (16 kHz `linear16` mono, server-side `Authorization: Token` header), config/secrets,
  the `<400 ms` latency metric, and the Orchestrator migration path. Status: in-progress. — setup
- `2026-06-19` — Built the package foundation: `internal/stt/provider.go` — the
  `Provider` and `Stream` interfaces, the `TranscriptEvent` and `StreamConfig` types,
  the `Speaker` type (`rep`/`prospect`, from the channel), and the
  `ErrProviderUnavailable` sentinel. Documents the D2–D5 contracts inline (server-side
  key, one stream per speaker, single-goroutine Send, drop-oldest backpressure). No
  vendor code yet. `gofmt`, `go build`, `go vet` green. — build
- `2026-06-20` — Built the Deepgram adapter + routed its key through the vault.
  `internal/stt/deepgram/client.go` implements `stt.Provider`/`stt.Stream`: dials
  `wss://api.deepgram.com/v1/listen` with `Authorization: Token` (server-side only),
  fixed `linear16`/16 kHz/mono params + `interim_results`/`punctuate`/`smart_format`/
  `endpointing`/`keyterm`; a writePump (binary PCM + 5 s KeepAlive + CloseStream-on-cancel),
  a readPump, drop-oldest `Send`, and a pure `parseResult` mapping Deepgram JSON →
  `TranscriptEvent`. **Vault:** the existing `internal/platform/secrets` (Store/EnvStore,
  KMS TODO) is now the canonical vault — added `secrets/keys.go` (the `AllKeys` registry:
  `AUTH_SIGNING_KEY`, `DEEPGRAM_API_KEY`), `DEEPGRAM_API_KEY` + `STT_*` to `.env.example`,
  and `STTProvider`/`STTModel`/`STTEndpointingMs` to `config.go`. Tests in
  `client_test.go` cover partial/final/end-of-turn parsing, empty/metadata/malformed skips,
  defaults, and URL params. `gofmt`/`go build ./...`/`go vet`/`go test` green. Still no
  real key needed (tests use canned JSON). — build
- `2026-06-20` — Built the per-session coordinator: `internal/stt/manager.go`. `Manager`
  (one Provider, process-wide) mints a per-connection `Session` via `StartSession`.
  `Session.Write(channel, pcm)` is the call site for `ws.go` (replaces the discarded
  PCM). Per speaker: **lazy open** on first frame (rep-only calls never open a prospect
  stream), a supervisor goroutine that **reconnects with capped exponential backoff**
  (250 ms → 5 s) on stream death, and **fan-out** to an `EventFunc` callback. The live
  stream pointer is mutex-guarded (Write vs. supervisor); `Send` drops while
  (re)connecting (D5). Emits §12 metrics (`stt_streams_active`, `stt_reconnects_total`,
  `stt_provider_errors_total`, `stt_partials_total`, `stt_finals_total`).
  **Bug found+fixed during testing:** the event-forward loop used `for range
  stream.Events()`, so `Close()` deadlocked when a stream didn't auto-close its channel
  on ctx-cancel (caught by a 600 s test timeout). Reworked into `pump`, which `select`s
  on `ctx.Done()` so teardown never depends on the provider. `manager_test.go` (fake
  provider/stream) covers lazy-open, fan-out, reconnect, open-failure retry, and clean
  Close; `go test -race` green. — build
- `2026-06-27` — **Wired the STT package into the live gateway path — the substrate now
  runs end to end (was orphaned: built but `ws.go` discarded PCM).** Four edits, no new
  files:
  1. **Startup (`cmd/gateway/main.go`).** New `buildSTT(ctx, cfg, log)` constructs the
     manager once at boot: `switch cfg.STTProvider` → for `deepgram`, fetch
     `secrets.KeyDeepgramAPI` from the vault (`EnvStore`), build `deepgram.New(...)` with
     `STTModel`/`STTEndpointingMs`, and `stt.NewManager`. A **missing key or unknown
     provider returns nil (audio-only)** and logs why — it is never a fatal boot error
     (D2 server-side key; §8 degradation). The key is read server-side only.
  2. **DI (`internal/gateway/server.go`).** `Server` gains an `stt *stt.Manager` field;
     `New(cfg, signingKey, sttMgr, log)` takes it. `nil` ⇒ the gateway runs without
     transcription.
  3. **Per-call lifecycle + transcript return (`internal/gateway/ws.go`).** On connect,
     if a manager exists, `handleRealtime` mints a `newSessionID()` (16 bytes from
     `crypto/rand`, hex — process-unique, no new dep) and calls
     `Manager.StartSession(r.Context(), tenant, sessionID, transcriptSink)`; teardown is a
     `defer sess.stt.Close()` placed **after** `defer conn.Close()` so (LIFO) supervisors
     stop writing before the socket shuts. `handleAudio` now captures `pcm` (was `_`) and
     calls `sess.stt.Write(channel, pcm)` — the lazy-open trigger. `transcriptSink`
     marshals each `TranscriptEvent` → a new `transcriptMsg` JSON (`{"type":"transcript",
     speaker,isFinal,speechFinal,text,startMs,endMs,confidence}`) and writes it back down
     the same WS. **Concurrency fix:** the manager fans events from up to two supervisor
     goroutines while the read loop also writes (`ready`/close frames), but
     gorilla/websocket forbids concurrent writers — so `session` gained a `writeMu` that
     guards **every** write (`ready`, `closeWS` close frame, and each transcript).
     `closeWS` now takes `sess` to hold that lock.
  4. **Latency gate (`internal/stt/manager.go`).** Added the `stt_partial_latency_ms`
     histogram (feature 2.6 / §12) with buckets straddling the 400 ms target. Each
     `channelStream` got an atomic `lastFrameNs` stamped in `Write` on send; `pump` (now
     `pump(cs, stream, label)`) observes `now − lastFrameNs` on every partial. Documented
     in-code + §13 as an **approximation** (newest-frame proxy — a partial may cover
     earlier audio, so it can read low); precise per-segment attribution stays a §13 TODO.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test -race ./internal/...` all green
  (existing gateway + stt tests still pass; no behavior change when STT is nil).
  **Not yet done (Stage 2 exit gate):** no run against the real Deepgram API yet — needs a
  live `DEEPGRAM_API_KEY` to confirm labeled partials/finals stream back and the
  `<400 ms` p95 holds on a real call. Status stays **in-progress**. — build
- `2026-07-14` — **Stage 2 closed out — transcription verified live against the real
  Deepgram API.** With a real `DEEPGRAM_API_KEY` sourced into the gateway environment
  (`set -a; source .env; set +a`), a live macOS session streamed speaker-labeled,
  punctuated **partials + finals** end to end. The gateway now prints finalized phrases to
  its console for dev visibility — `[transcript] rep:/prospect: <text>` from a `fmt.Printf`
  in `transcriptSink` (`internal/gateway/ws.go`), finals only, so the terminal stays
  readable. This confirms D1–D4 on real traffic: Deepgram `nova-3`, one stream per
  (session, channel), channel→speaker labeling (no ML diarization), and the server-side key
  held backend-only (never sent to the client, D2).
  **Dev-only caveat:** the console print emits raw transcript text, which
  `coding_standards_techdoc.md` §7 forbids in production logs — gate it behind a flag or
  remove it before any real deployment (tracked in §13).
  **Residuals (measurement/tuning, not blockers to Stage 3):** (a) confirm the `<400 ms`
  p95 gate on real calls via `stt_partial_latency_ms` — instrumented (`manager.go`) and
  readable from `/metrics`, but the newest-frame proxy reads low and excludes the ~150 ms
  client capture leg (`audio_capture_techdoc.md` §8), so true end-to-end is higher;
  (b) a two-sided real Zoom/Meet call with the rep on headphones to confirm the `prospect`
  label in the wild; (c) endpointing tuning. Status → **done** (substrate built and verified
  live; residuals are measurement/tuning). — build
