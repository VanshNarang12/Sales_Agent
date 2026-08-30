# Transcript Store (Redis) — Techdoc

| | |
| --- | --- |
| **Topic** | `transcript_store` |
| **Roadmap stage** | `ROADMAP.md` Stage 3 — Suggestion Trigger (storage substrate; reused by Stage 10 post-call) |
| **Feature IDs** | `FEATURES.md` — `3.2` (last-N-minutes window capture); enabler for `9.1` post-call summary |
| **Architecture** | `ARCHITECTURE.md` §7.2; complements detection_techdoc.md §buffer |
| **Plane** | Real-Time |
| **Owner** | Vansh |
| **Status** | done |
| **Last updated** | 2026-08-26 |

## 1. Overview
Moves the live-call transcript out of gateway process RAM into Redis. Today the
Stage 3 detect session keeps a 400-entry in-process slice (`internal/detect/engine.go`);
a gateway restart or a reconnect to another instance loses all context, and post-call
features have nothing to read. This store keeps the **complete conversation** of an
active call in one Redis sorted set per session (no trimming), lets the Suggest flow
slice out the last-N-ms window at read time, and self-destructs via a **sliding TTL**
after the call goes quiet.

## 2. Scope
- **In scope:** append-per-final-utterance write path; time-window read for Suggest
  (`3.2`); sliding-TTL lifecycle; config knobs; wiring into `detect.Session`.
- **Out of scope / deferred:** durable post-call persistence (Stage 10 reads this key
  before expiry and writes elsewhere); "no-recording mode" bypass (Stage 8 — flagged
  in §13); partial-transcript storage (finals only, same as the RAM buffer).

## 3. Decisions & rationale (the "why")
- **Decision: Redis, not Mongo/Postgres.** Hot-path append every few seconds per call;
  reads must be sub-ms. Redis is already provisioned (`REDIS_URL`, docker-compose).
  Mongo rejected: 60s TTL-monitor granularity, durable-by-default is the wrong
  compliance posture for live transcripts. Postgres rejected: no native expiry;
  row-per-utterance churn for ephemeral data.
- **Decision: one sorted set per session, score = `endMs`.** `ZADD` on write,
  `ZRANGEBYSCORE (latest-lookback, +inf)` on read gives the time-window slice without
  any trimming logic. A Redis LIST was rejected: window reads would need full scans.
- **Decision: store the complete call, no trimming.** Product call (2026-08-26):
  lookback is configurable (90s → 10min), and post-call features need the whole call.
  Memory cost is trivial (~200–300 KB for a 3-hour call).
- **Decision: sliding TTL, refreshed on every write (default 1800s).** While the call
  is live, writes keep the key alive regardless of call length; after the last
  utterance the countdown runs and Redis deletes the key itself — crash-safe cleanup
  with zero sweeper code. 30min is long enough for reconnects and post-call jobs,
  short enough that nothing accumulates.
- **Decision: detect depends on an interface, not on Redis.** `detect` gets a small
  store interface; the Redis implementation lives in `internal/transcript`. Keeps
  detect unit-testable without a Redis and lets tests use a fake.
- **Decision: member encoding `"{startMs}|{speaker}: {text}"`.** ZSET members must be
  unique; two utterances can share an `endMs` score, so `startMs` is prefixed to
  de-collide while keeping the member human-readable and parseable.

## 4. Folder & file structure
```
internal/transcript/
  ├── store.go       # Store: Append (ZADD+EXPIRE pipeline), Window (ZRANGEBYSCORE)
  └── store_test.go  # miniredis-backed unit tests
```
Touched: `internal/detect/engine.go` (RAM slice → store interface),
`internal/platform/config/config.go` (+2 knobs), `cmd/gateway/main.go` (redis client
+ wiring), `internal/gateway/ws.go` (unchanged contract).

## 5. Architecture & data flow
```
STT final event ─► detect.Session.OnTranscript ─► transcript.Store.Append
                                                    ZADD transcript:{tenant}:{session} endMs member
                                                    EXPIRE key ttl        (pipelined, 1 RTT)
Suggest click ──► detect.Session.Suggest ─► Store.Window(lookbackMs)
                                                    ZRANGEBYSCORE key (latest-lookback) +inf
                 ─► BuiltQuery emit (the window is the query)
```

## 6. Data model & storage
- Key: `transcript:{tenant_id}:{session_id}` — tenant id is **in the key**, preserving
  isolation in a shared Redis (coding_standards §4).
- Value: ZSET; score = utterance `endMs` (call-relative ms); member = `"{startMs}|{speaker}: {text}"`.
- Retention: sliding TTL `TRANSCRIPT_TTL_SECONDS` (default 1800). No explicit delete
  path needed; Stage 8 no-recording mode must skip Append entirely (§13).

## 7. APIs / events
- **Inbound:** `Append(ctx, tenantID, sessionID, ev)` from detect; `Window(ctx, tenantID, sessionID, lookbackMs)` from Suggest.
- **Outbound:** none — `BuiltQuery` contract (detection_techdoc D6) is unchanged.

## 8. External dependencies
- `github.com/redis/go-redis/v9` (client), `github.com/alicebob/miniredis/v2` (test-only).
- Degradation: Redis down ⇒ Append logs + drops (call continues, audio unaffected);
  Suggest returns empty window ⇒ no query emitted (same as empty buffer today).

## 9. Configuration & secrets
- `REDIS_URL` (existing) — connection string.
- `SUGGEST_LOOKBACK_MS` (new, default 90000) — server-side default window; WS
  `lookbackMs` still overrides per click.
- `TRANSCRIPT_TTL_SECONDS` (new, default 1800) — sliding key TTL.
- No secrets beyond the URL; key material stays in env/vault per standards §8.

## 10. How to extend (for the next agent)
- Post-call (Stage 10): read the full set with `ZRANGEBYSCORE key -inf +inf` at
  call-end, persist durably, then let TTL clean up — do not `DEL` (a late Suggest may
  still need it).
- New consumer of the window: depend on the same interface detect uses; never import
  go-redis outside `internal/transcript`.
- No-recording mode (Stage 8): gate `Append` at the detect wiring in `ws.go`.

## 11. Testing & verification
- Unit: miniredis — append/window semantics, TTL refresh on write, empty-window,
  tenant-key separation. Detect tests swap in a fake store (no Redis needed).
- Local: `make up` (redis), `make run-gateway`, connect client, talk, click Suggest;
  verify `[suggest]` log shows the window and `redis-cli TTL transcript:...` ≈ 1800
  and falling between utterances.

## 12. Observability
- `transcript_store_append_errors_total`, `transcript_store_window_latency_ms`
  (planned with the Stage 3 metric set, detection_techdoc §12).
- Append/Window errors logged with tenant+session, never with transcript text
  (standards §7).

## 13. Open questions / TODO
- Stage 8 no-recording mode must bypass this store — the "ephemeral by default"
  story now depends on the TTL, which is weaker than "never stored"; needs an
  explicit off switch.
- Redis HA story (single instance today; window loss on failover is acceptable pre-MVP).
- Whether Stage 10 snapshots at call-end via a gateway hook or an async job.

## 14. Changelog
- `2026-08-26` — Implemented and tested: `internal/transcript` (Store, miniredis
  tests), detect engine swapped to the `TranscriptStore` interface (RAM buffer,
  `maxBufEntries` cap and `snapshot()` removed; `joinWindow()` renders the read),
  config knobs added, boot wiring pings Redis and disables Suggest if unreachable.
  All tests pass. — Vansh + Claude
- `2026-08-26` — Created at feature start: Redis ZSET store replacing the in-RAM
  detect buffer; complete-call storage, sliding 30-min TTL, window read at Suggest
  time. — Vansh + Claude
