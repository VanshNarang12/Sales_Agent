# Realtime Gateway — Techdoc

| | |
| --- | --- |
| **Topic** | `realtime_gateway` |
| **Roadmap stage** | `ROADMAP.md` Stage 1 — Audio Capture (server side); sets up Stage 2 |
| **Feature IDs** | `1.4` (two-stream separation, server-side routing), `2.3` (speaker labeling source), `20.1` (real-time streaming backend) |
| **Architecture** | `ARCHITECTURE.md` §4 (Realtime Gateway), §5 (real-time path), ADR-001/002/007/008 |
| **Plane** | Real-Time |
| **Owner** | Platform / Realtime |
| **Status** | in-progress |
| **Last updated** | 2026-06-13 |

> **How to read this doc.** It is both a **spec** (the exact rules the gateway
> implements) and an **explainer** (it defines every term, library, and message as it
> uses them, so you don't need prior WebSocket/Go knowledge). §3 is the concepts
> primer — read it once and the rest follows. The client side of the same protocol is
> in [`audio_capture_techdoc.md`](./audio_capture_techdoc.md); this doc is the
> **server** side.

---

## 1. Overview
The **Realtime Gateway** is the **single front door of the real-time plane** — the
one server the desktop app talks to during a live call. By Stage 0 it already does
three things: accepts the connection, checks the caller is allowed in
(authentication), and stamps the request with which company/tenant it belongs to.

What this section adds: until now the gateway just **echoed** — it sent every
message straight back to prove the connection worked. That test has served its
purpose. Stage 1 replaces the echo with **real understanding of the audio
protocol**: the gateway now reads the opening `hello` message (defined in §3),
checks the audio format is one it supports, unpacks each chunk of audio to learn
**who is speaking and in what order**, rejects anything malformed, and counts any
audio that went missing in transit. After this the server actually comprehends the
incoming stream — the precondition for Stage 2, where the audio is handed to the
transcription engine.

## 2. Scope
- **In scope (this section):**
  - Read the opening **`hello`** message and validate the audio format; reject with
    close code `4003` if unsupported.
  - Unpack each binary **audio frame** into `(channel, seq, pcm)`; reject malformed
    frames with close code `4002`.
  - Reject an audio frame that arrives **before** a valid `hello` (close `4002`).
  - Track a **sequence number** per channel and count gaps (lost/reordered audio) as
    a metric.
  - Reply `{"type":"ready"}` once the `hello` is accepted.
  - **Remove the echo.**
  - Unit-test the unpacking logic; expose counters for observability.
- **Out of scope / deferred:**
  - Handing frames to the **Call Session Orchestrator** and streaming transcripts/
    cards back → **Stage 2** (§11).
  - The native **prospect/system-audio** capture that produces `channel = 0x01`
    (the buyer's voice) — that's client-side work, see
    [`audio_capture_techdoc.md`](./audio_capture_techdoc.md) §13.
  - Real **login/OAuth** — the `4001` (unauthorized) path exists but only runs when
    auth is enabled; login itself is deferred (ROADMAP Stage 0.5).

---

## 3. Concepts primer (read once; the rest of the doc assumes these)

**WebSocket (WS) — the kind of connection we use.** A normal web request (HTTP) is
*one-shot*: the client asks once, the server answers once, and the connection is
done. That's useless for a live call, where audio flows continuously for minutes and
suggestion cards need to come back at any moment. A **WebSocket** is a *persistent,
two-way* connection: it opens once and stays open, and **both sides can send messages
whenever they want** over that single connection — the client pushes audio up, the
server pushes messages down, simultaneously. That two-way-stay-open property is
exactly what a live call needs.

**"Upgrade."** A WebSocket actually *starts* as an ordinary HTTP request and is then
"upgraded" into a WebSocket via a special handshake (a defined exchange of headers).
After the upgrade, the same TCP connection carries WebSocket messages instead of HTTP.
In our code the line `upgrader.Upgrade(w, r, nil)` performs that switch.

**Why a library — `github.com/gorilla/websocket`.** Go's standard library can serve
HTTP but does **not** implement the WebSocket protocol (the handshake, the message
framing, the ping/pong keep-alives, etc.). `gorilla/websocket` is the widely-used,
well-tested Go package that implements all of that for us, so we don't hand-roll a
protocol. We use it to (a) perform the upgrade and (b) read/write whole messages with
`conn.ReadMessage()` / `conn.WriteMessage()`.

**Text vs. binary messages.** The WebSocket protocol labels every message as one of
two kinds:
- **Text message** — UTF-8 text, meant for human-readable strings. We use it for
  **JSON control messages** (like `hello` below). JSON = a simple text format of
  `{"key": value}` pairs.
- **Binary message** — raw bytes, no text encoding. We use it for the **raw audio**
  (PCM, defined below), because audio is numbers, not text.

`conn.ReadMessage()` returns a small integer telling us which kind arrived
(`websocket.TextMessage` or `websocket.BinaryMessage`). The gateway branches on it:
text → parse JSON; binary → unpack an audio frame.

**The `hello` message — what it is and why it exists.** `hello` is the **first
message the client sends, once, right after the socket opens, before any audio.** Its
job is to **announce the format of the audio that's about to follow.** This matters
because the audio arrives as raw bytes with no labels — the server has no way to know
whether those bytes are 16,000 samples/sec or 8,000, 16-bit or 8-bit, one channel or
two, unless the client states it up front. `hello` is that "here is how to read what
I'm about to send you" handshake. Its fields:

| Field | Example | What it means |
| --- | --- | --- |
| `type` | `"hello"` | Which control message this is. |
| `role` | `"rep"` | Which side of the call this stream is — the salesperson (`rep`) or the buyer (`prospect`). |
| `sampleRate` | `16000` | **Samples per second** — how many audio measurements per second. 16,000 (16 kHz) is what speech needs and what the transcription providers expect. |
| `encoding` | `"pcm_s16le"` | How each sample is stored. **PCM** = raw, uncompressed audio numbers. **s16le** = signed 16-bit, little-endian (each sample is a whole number −32768…32767; "little-endian" = the low byte comes first on the wire). |
| `channels` | `1` | Number of audio streams in this message. `1` = mono (one voice). |
| `frameSamples` | `2048` | How many samples are bundled into one chunk (= 128 ms of audio at 16 kHz). |
| `client` | `{app, version, os}` | Which app/version/OS is connecting (for debugging). |

The gateway accepts the stream only if `encoding == "pcm_s16le"`,
`sampleRate == 16000`, and `channels == 1` — the one format the rest of the pipeline
is built for. Anything else → close `4003` (defined below).

**The audio frame — the binary message layout.** Sending one sample at a time would
mean 16,000 tiny messages per second. Instead the client groups 2048 samples into a
**frame** (one chunk = 128 ms of audio) and sends it as one binary message. Each
frame is a flat run of bytes: a small **header** describing the chunk, then the audio
itself.

```
[1 byte: type][1 byte: channel][2 bytes: seq][ N bytes: PCM audio ]
└──────────────── header (4 bytes) ───────────────┘└──── payload ────┘
```

Reading that notation: the message is just bytes laid end to end; byte 0 is `type`,
byte 1 is `channel`, bytes 2–3 are `seq`, and everything after byte 3 is the audio.

| Field | Bytes | What it is / why it's there |
| --- | --- | --- |
| `type` | 1 | Message kind. `0x01` = "this is an audio frame." Reserving a type byte lets us add other binary message kinds later (e.g. a marker) without ambiguity. |
| `channel` | 1 | **Who is speaking.** `0x00` = rep (mic), `0x01` = prospect (system audio). This is how we know the speaker **for free** — each physical source carries its own label — instead of using costly AI "diarization" to guess (ADR-007). |
| `seq` | 2 | **Sequence number** — a counter that increases by 1 for every frame on that channel, wrapping 65535→0. It lets the server spot missing or out-of-order frames (see below). Little-endian (low byte first). |
| `payload` | N | The audio: `frameSamples × 2` bytes (2 bytes per 16-bit sample). For 2048 samples → 4096 bytes. |

So a normal audio frame is `4 + 4096 = 4100` bytes.

**Sequence number (`seq`) and "gaps."** Because each frame's `seq` is one more than
the last, the server can tell if audio went missing: if it sees `seq = 5` then
`seq = 8`, frames 6 and 7 were lost. We call that a **gap**. Gaps are *expected and
fine* on live audio — the client deliberately **drops** frames when the network is
congested (stale audio is worthless for live coaching), still bumping `seq` so the
loss is visible. So the gateway treats a gap as a **metric to record**, never an
error to kill the call over.

**Close codes — why a connection ended.** When a WebSocket closes, it carries a
number explaining why. We use the standard ones plus an application-specific range
(4000–4999, reserved by the WebSocket spec for app use):

| Code | Meaning | Who sends it |
| --- | --- | --- |
| `1000` | Normal close (call ended / user stopped) | either side |
| `1001` | "Going away" (app quitting) | client |
| `4001` | Unauthorized (bad/expired token) — only when auth is enabled | server |
| `4002` | Protocol error (malformed frame, audio before `hello`, unknown type) | server |
| `4003` | Unsupported format (wrong sampleRate/encoding/channels in `hello`) | server |
| `1011` | Internal server error | server |

---

## 4. Decisions & rationale (the "why")

- **Unpack + validate at the gateway, not downstream.**
  - *Why:* the gateway is the protocol boundary — the first place that understands the
    wire format. Rejecting bad frames here means they never reach (and waste) the
    orchestrator or a paid transcription stream. Failing closed mirrors how tenancy
    already works (`MustFrom` refuses to proceed without a tenant).
  - *Rejected:* passing raw bytes to the orchestrator and validating there — adds a
    network hop on the latency budget and spreads wire-format knowledge into a service
    that should only ever see clean `(channel, seq, pcm)`.

- **Delete the echo.**
  - *Why:* echo was a Stage-0 "is the pipe connected?" check. It's now replaced by
    real parsing, and bouncing audio back would waste downstream bandwidth. The client
    already ignores anything the server sends today, so removing it breaks nothing.

- **`hello` must come first; an early audio frame closes `4002`.**
  - *Why:* the server literally cannot interpret PCM bytes before it knows their
    format. "Handshake first" keeps the protocol deterministic.

- **A sequence gap is a metric, not a fatal error.**
  - *Why:* live audio tolerates loss by design (the client drops under congestion,
    `audio_capture_techdoc.md` §6.1). Killing a call on a normal Wi-Fi blip would be
    far worse than a short gap in coaching.

- **Session state is in memory only.**
  - *Why:* audio is ephemeral by default (ADR-008). The per-connection state is just
    the declared format + the last `seq` per channel + counters — nothing is written
    to disk or any database.

- **The unpacking logic is a pure function in its own file (`frame.go`).**
  - *Why:* a function that takes bytes and returns `(channel, seq, pcm, err)` can be
    unit-tested without opening a real socket — the fastest, most reliable test
    surface.

- **Forward-looking: this is real-time-plane code.**
  - When the backend is split into services, the gateway moves under `cmd/realtime`
    next to the orchestrator, still importing the shared `internal/platform`
    packages. The parsing/validation here doesn't change — it's exactly the clean seam
    ADR-001/010 plan for.

## 5. Folder & file structure
```
cmd/gateway/
  └── main.go          # entrypoint (unchanged): load config, start telemetry, serve

internal/gateway/
  ├── server.go        # routes + auth middleware + tenancy (Stage 0, unchanged)
  ├── ws.go            # WS upgrade + per-connection read loop: hello → frames (REWRITTEN: no echo)
  ├── frame.go         # NEW — pure unpacker: parseAudioFrame([]byte) → (channel, seq, pcm, err); message-type + close-code constants
  └── frame_test.go    # NEW — table-driven tests (good frame, too short, odd length, wrong type, seq decoding)
```

## 6. Architecture & data flow
```
Desktop client ──WS /v1/realtime──► Realtime Gateway
  1) text  "hello"  ───────────►  validate format → save descriptor → reply {"type":"ready"}
  2) binary audio frame ───────►  parseAudioFrame() → (channel, seq, pcm)
                                   update lastSeq[channel] → frame_gap_total on a gap
                                   [Stage 2: forward (tenant, sessionID, channel, seq, pcm) → Orchestrator over gRPC]
  3) close (1000/1001) ────────►  end session, free resources
```
- **Called by:** the desktop client — one WebSocket per call. The connection is
  already authenticated and tenant-scoped by `authMiddleware` in `server.go`; in dev,
  `AUTH_DISABLED` injects the dev tenant so the pipeline can run before login exists.
- **Calls (Stage 2+):** the Call Session Orchestrator over **gRPC** (gRPC = a fast,
  typed service-to-service call mechanism used on the hot path; not yet wired).
- **Per-connection server state machine:** `awaiting-hello → streaming → closed`. An
  audio frame while still `awaiting-hello` → close `4002`. A valid `hello` →
  `streaming` and send `ready`. Any read error or close frame → `closed`.

## 7. Data model & storage
**None.** No database tables, no Redis, no object store, no files. Frames are unpacked
in memory and (Stage 1) discarded after accounting; (Stage 2) forwarded to the
orchestrator stream. This is the ephemeral-audio guarantee (`14.3`/`1.12`, ADR-008).
The `tenant_id` lives only in the in-memory request context.

## 8. APIs / events
- **Inbound — WebSocket endpoint `GET /v1/realtime`:**
  - **text** `hello` — the format-announcement message (fields defined in §3).
  - **binary** audio frame — `[1B type=0x01][1B channel][2B seq][N B PCM]` (layout in §3).
- **Outbound — over the same WebSocket (this section):**
  - text `{"type":"ready"}` after a valid `hello` (tells the client "format accepted,
    you may stream").
  - **close codes** as in §3 (`1000`/`1001`/`4001`/`4002`/`4003`/`1011`).
- **Outbound (Stage 2):** a gRPC stream to the orchestrator; transcript and card
  messages flow back down this same WebSocket. The contract (a `.proto` file) lands in
  `api/proto/` — `.proto` = the schema file that defines gRPC messages/methods.
- **Events:** none here. Call lifecycle events (`call.started` / `call.ended`) begin at
  the orchestrator in Stage 2 (`ARCHITECTURE.md` §12).

## 9. External dependencies
- **`github.com/gorilla/websocket`** — the Go package that implements the WebSocket
  protocol (handshake/upgrade, message framing, read/write). *Why:* Go's standard
  library has no WebSocket support; this is the de-facto standard package. Already a
  dependency (`go.mod`).
- No transcription/LLM/CRM dependencies in this section — those enter at the
  orchestrator (Stage 2).
- **Degradation:** if a read errors or the client disappears, the session ends
  cleanly; there is no downstream service to fail yet.

## 10. Configuration & secrets
- Reuses Stage-0 config (`internal/platform/config`): `HTTP_ADDR` (listen address),
  `ENV`, `AUTH_DISABLED` (dev bypass of login), `DEV_TENANT_ID`, and
  `AUTH_SIGNING_KEY` (only when auth is enabled).
- No new configuration in this section. No secrets are read here — the signing key is
  handled earlier, in `main.go`/`server.go`.

## 11. How to extend (for the next agent)
- **Stage 2 (the main next step) — forward to the orchestrator:** in `ws.go`, replace
  the "update counters then discard" step (after `parseAudioFrame`) with a gRPC client
  stream to the Call Session Orchestrator, sending `(tenant, sessionID, channel, seq,
  pcm)`. Open the stream on the first valid `hello`; close it when the WebSocket
  closes. Read transcript/card messages back from the orchestrator and write them to
  the client with `conn.WriteMessage(...)`. Define the messages in a new
  `api/proto/realtime.proto`.
- **A new control message** (e.g. `pause`, `marker`): add a `case` in the text branch
  of the read loop. Keep `frame.go` for binary audio only.
- **A new binary message kind** (reserve a `type` byte other than `0x01`): extend the
  `type` switch in `parseAudioFrame` and add a test row.

## 12. Testing & verification
- **Unit (`frame_test.go`), table-driven:** `parseAudioFrame` accepts a well-formed
  4100-byte frame (4-byte header + 4096-byte PCM) and returns the right
  `channel`/`seq`/`len(pcm)`; it rejects `len < 4`, an odd `(len − 4)` (PCM must be
  whole 16-bit samples), and a `type` byte other than `0x01`; `seq` decodes
  little-endian (`b[2] | b[3]<<8`).
- **Manual / integration:** start the stores and gateway
  (`make up && AUTH_DISABLED=true make run-gateway`), then the client
  (`cd client && npm start`) → Connect. Expected: the gateway logs the session,
  accepts the `hello`, the client receives `ready`, roughly **8 frames/second** are
  parsed (1000 ms ÷ 128 ms per frame), Pause stops the frames, and **no audio is
  echoed back**. Send a deliberately malformed frame → server closes with `4002`; send
  a `hello` with `sampleRate: 44100` → close `4003`.
- **Exit expectation (ROADMAP Stage 1, server side):** the gateway reliably unpacks a
  two-channel-capable audio stream and accounts for any loss — ready for Stage 2 to
  attach transcription.

## 13. Observability
- **Metrics (Prometheus — a metrics system that scrapes counters/gauges from
  `/metrics`):**
  - `ws_sessions` (gauge — a value that goes up and down) — currently open realtime
    connections.
  - `frames_total{channel}` (counter — a value that only increases) — audio frames
    successfully parsed, per channel.
  - `frame_gap_total{channel}` (counter) — detected sequence gaps (dropped/reordered
    audio), per channel.
  - `ws_protocol_errors_total{code}` (counter) — connections closed by app code
    (`4002`/`4003`).
- **Traces (OpenTelemetry — a system that records the timeline of one request across
  services):** a span (one timed unit of work) per connection, tagged with the tenant
  (Stage 0). Stage 2 extends the trace into the orchestrator and transcription so we
  can see where each stage spends its latency budget (§5.2).
- **Logs:** structured and tenant-tagged, with **no audio bytes or transcript content
  ever logged** (coding standards §7).

## 14. Open questions / TODO
- Enforce `channel ∈ {0,1}` strictly at parse time (close `4002` on others) vs. accept
  and let routing ignore unknown channels — currently leaning strict.
- `frameSamples` and `role` from `hello` are informational today; decide whether the
  server should pin the frame size (reject anything ≠ 2048) once Stage-2 transcription
  framing constraints are known.
- The WebSocket origin check in `ws.go` is still permissive (`return true`); tighten it
  for the packaged desktop client.
- Server-side pacing/backpressure toward transcription — design alongside the
  orchestrator in Stage 2.
- Service split: when the real-time plane moves to `cmd/realtime`, relocate
  `internal/gateway` with it (no logic change expected).

## 15. Changelog
- `2026-06-13` — Techdoc created at the start of Stage 1 gateway work: parse the
  `hello` handshake + binary audio frames, validate format, track sequence gaps, and
  remove the echo; sets up the Stage 2 forward to the orchestrator. Added a concepts
  primer (§3) defining WebSocket, the `gorilla/websocket` library, text vs. binary
  messages, the `hello` message, the audio-frame byte layout, sequence numbers, and
  close codes. Status: in-progress. — setup
- `2026-06-13` — Implemented the gateway parsing: `frame.go` (pure `parseAudioFrame`
  + wire/close-code constants), `frame_test.go` (table-driven parser tests), and
  rewrote `ws.go` (validate `hello` + reply `ready`, parse audio frames, per-channel
  sequence-gap accounting, `gateway_frames_total`/`gateway_frame_gap_total`/
  `gateway_ws_protocol_errors_total` metrics; **echo removed**). `gofmt`,
  `go build ./...`, `go vet`, and `go test ./internal/gateway/...` all green. Part 1
  (gateway) done; Part 2 (native prospect/system-audio capture) and the Stage 2
  orchestrator forward remain — status stays in-progress. — build
- `2026-06-19` — **Bugfix: WebSocket upgrades were failing with "response does not
  implement http.Hijacker".** Root cause: `telemetry.HTTPMiddleware` wraps every
  response in a `statusWriter` (to record the status code) that embedded
  `http.ResponseWriter` but did not expose `Hijack` — so gorilla's `Upgrade` could not
  hijack the raw TCP connection and returned HTTP 500. This blocked **all** realtime
  connections. Fix: added a `Hijack()` method to `statusWriter`
  (`internal/platform/telemetry/telemetry.go`) that delegates to the underlying
  ResponseWriter's `http.Hijacker`. Verified end to end: a synthetic client sent 25
  rep (`0x00`) + 25 prospect (`0x01`) frames; the gateway replied `{"type":"ready"}`
  and `/metrics` showed `gateway_frames_total{channel="rep"}=25` and
  `{channel="prospect"}=25`. — build
- `2026-06-19` — **Confirmed end-to-end against the real macOS GUI client** (previous
  proof was a synthetic client; this validates the actual desktop app's `hello` format
  and frame encoding match the parser). Sequence observed: `ws open — sending hello` →
  gateway accepted the `hello` and replied `{"type":"ready"}` → client reached
  **streaming** → both `gateway_frames_total{channel="rep"}` and `{channel="prospect"}`
  climbed (prospect via the ABI-130 native tap @ 48 kHz). No `4001/4002/4003` close
  codes and no `gateway_ws_protocol_errors_total` increments — hello and both frame
  channels parsed clean on the first real-client run. The earlier `ERR_CONNECTION_REFUSED`
  during testing was simply the gateway not running (`go run ./cmd/gateway` with
  `AUTH_DISABLED=true`), not a protocol fault. Stage-1 gateway path verified live;
  status stays **in-progress** pending the Stage-2 orchestrator forward. — build
