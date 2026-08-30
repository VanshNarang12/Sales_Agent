# Audio Capture — Techdoc


|                   |                                                             |
| ----------------- | ----------------------------------------------------------- |
| **Topic**         | `audio_capture`                                             |
| **Roadmap stage** | `ROADMAP.md` Stage 1 — Audio Capture                        |
| **Feature IDs**   | `1.1`–`1.6`, `1.8`, `1.12`                                  |
| **Architecture**  | `ARCHITECTURE.md` §10 (desktop client), §5 (real-time path) |
| **Plane**         | Client (desktop) → Real-Time Gateway                        |
| **Owner**         | Client                                                      |
| **Status**        | in-progress                                                 |
| **Last updated**  | 2026-08-26                                                  |

> **Repo split (2026-08-26):** the desktop client now lives in its own repository,
> [`Sales_Agent_Frontend`](https://github.com/VanshNarang12/Sales_Agent_Frontend),
> with its git history preserved. Every `client/...` path in this techdoc maps to the
> root of that repo (e.g. `client/src/main.ts` → `src/main.ts`). This backend repo no
> longer contains client code.


> **How to read this doc.** It is both a **spec** (exact formats, types, numbers an
> engineer implements against) and an **explainer** (it defines the audio/networking
> concepts and notation as it uses them, so you don't need prior audio-engineering
> knowledge). Sections 3–8 are the contract; 9–18 are supporting detail.

---

## 1. Overview

This is the desktop application that **captures the call audio and streams it to the
backend**. It is the input end of the entire product: with no clean audio there is
no transcript, no objection detection, and no suggestion card. Stage 1 delivers:
microphone audio flowing from an Electron app to the Realtime Gateway over a
WebSocket, the private overlay window, an instant "stop listening" control, and
**zero audio persistence**.

## 2. Scope

- **In scope now:** Electron desktop shell, macOS + Windows (`1.1`); microphone
capture (`1.2`); resample + encode + frame the audio; stream it to the gateway;
instant pause (`1.8`); ephemeral / no-storage (`1.12`); input-device selection
(`1.6`); the private overlay window (sets up `7.7`).
- **In scope now — Part 2 (this step):** system/loopback capture of the prospect
(`1.3`) and the two-stream split (`1.4`) via a native OS add-on; OS-permission UX
(`1.5`). **Detailed plan in §19.** (Part 1 — rep mic + the gateway-side frame parser
— is done; the parser lives in
`[realtime_gateway_techdoc.md](./realtime_gateway_techdoc.md)`.)
- **Out of scope:** transcription (Stage 2), AI, the full overlay UI (Stage 7).

---

## 3. Concepts primer (read once; the rest of the doc assumes these)

**How a computer represents sound.** A microphone turns air-pressure waves into a
continuously varying voltage. A computer can't store/transmit a "continuous" signal,
so it **samples** it: it measures the signal's amplitude at fixed time intervals and
stores each measurement as a number. This scheme is called **PCM — Pulse-Code
Modulation** — the standard *uncompressed* digital-audio representation. Three
parameters fully describe a PCM stream:

- **Sample rate** — how many measurements per second, in hertz (Hz). We use
**16,000 Hz (16 kHz)** = 16,000 numbers per second per channel. *Why 16 kHz:*
human speech energy sits below ~8 kHz, and the **Nyquist theorem** says you must
sample at ≥2× the highest frequency you want to keep, so 16 kHz preserves all of
speech. It is also the **native input rate of the STT providers** (Deepgram,
AssemblyAI); sending higher just wastes CPU and bandwidth and they downsample it
anyway.
- **Bit depth** — how many bits each sample uses, which sets its numeric precision.
We use **16-bit signed integers (`int16`)**: each sample is a whole number from
**−32768 to +32767**. "Signed" because a sound wave swings both above and below
its resting point (positive and negative). 16-bit is the audio standard (CD
quality) and what STT APIs expect.
- **Channels** — independent audio streams. **Mono = 1** (one microphone). Stereo
would be 2. We use mono per speaker.

**Endianness (`LE`).** A 16-bit number is 2 bytes. *Endianness* is the convention
for which byte comes first in memory/on the wire. **Little-endian (`LE`)** puts the
*least*-significant byte first. So the number `0x1234` is sent as the two bytes
`0x34, 0x12`. We use `**Int16LE`** (16-bit, signed, little-endian) because that is
what the STT APIs and WebAudio's typed arrays use natively — no conversion needed.

**Frame.** Sending one sample at a time = 16,000 network messages/second, which is
absurd. So we group many consecutive samples into a **frame** and send the frame as
one message. We use **2048 samples per frame**. At 16 kHz: `2048 / 16000 = 0.128 s`
= **128 milliseconds of audio per frame**. Small enough to keep latency low, large
enough to be efficient.

**WebSocket (WS).** A normal HTTP request is one-shot (ask → answer → done). A
**WebSocket** is a *persistent, two-way* connection kept open for the whole call:
the client can push audio frames up continuously and the server can push suggestion
cards down — both directions, same connection. WS messages are either **text**
(we use these for JSON control messages) or **binary** (we use these for raw PCM
bytes).

**Byte-layout notation.** Later we describe a message as e.g.
`[1 byte: type][1 byte: channel][2 bytes: seq][payload]`. This means: "the message
is a flat sequence of bytes; byte 0 is the `type` field, byte 1 is `channel`, bytes
2–3 are `seq`, and everything after is the payload." Reading it is just walking the
bytes left to right at the stated offsets.

---

## 4. The capture pipeline (browser/renderer side)

The capture runs in Electron's **renderer** process (a Chromium page) using the
**WebAudio API**. Pipeline, stage by stage:

```
getUserMedia(mic)  →  MediaStreamAudioSourceNode
        →  capture node (ScriptProcessorNode)   // gives us raw Float32 @ 48 kHz
        →  resample 48 kHz → 16 kHz             // decimate by 3 (+ low-pass)
        →  Float32 → Int16LE conversion
        →  accumulate into 2048-sample frames
        →  WebSocket.send(frame)                // binary
```

### 4.1 Acquiring the mic

```ts
const stream = await navigator.mediaDevices.getUserMedia({
  audio: {
    deviceId: selectedDeviceId ? { exact: selectedDeviceId } : undefined, // 1.6 device pick
    channelCount: 1,            // mono
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
  },
});
```

`getUserMedia` triggers the OS mic-permission prompt (`1.5`). `enumerateDevices()`
populates the device picker.

### 4.2 The AudioContext and why we must resample

A browser `AudioContext` runs at the **hardware rate**, almost always **48,000 Hz**,
and hands us samples as **Float32** (32-bit floats in the range **−1.0 … +1.0**).
But we need **16,000 Hz Int16**. So two conversions are required: a **sample-rate
conversion (resample)** 48k→16k, and a **format conversion** Float32→Int16.

`48000 / 16000 = 3` exactly, so resampling is **decimation by a factor of 3**: keep
1 of every 3 samples. *Naïve* decimation (just dropping samples) causes **aliasing**
(high frequencies fold down into the audible band as distortion). The correct method
is **low-pass filter first** (remove everything above 8 kHz = the new Nyquist limit),
*then* drop 2 of every 3 samples. Stage-1 implementation:

```ts
// Simple 1st-order low-pass (one-pole IIR) to tame aliasing, then decimate by 3.
// alpha derived from cutoff ~7 kHz at 48 kHz input. Good enough for speech in v1;
// TODO migrate to a proper FIR / OfflineAudioContext resampler.
let lp = 0;
const alpha = 0.45;
function resample48to16(input: Float32Array): Float32Array {
  const out = new Float32Array(Math.floor(input.length / 3));
  let j = 0;
  for (let i = 0; i < input.length; i++) {
    lp += alpha * (input[i] - lp);     // low-pass
    if (i % 3 === 0) out[j++] = lp;    // decimate: keep every 3rd
  }
  return out;
}
```

### 4.3 Float32 → Int16LE

Each float sample `s ∈ [−1, 1]` becomes an int16 by scaling to the int16 range and
clamping (so a value slightly outside [−1,1] can't overflow/wrap):

```ts
function floatToInt16(s: number): number {
  const v = Math.round(s * 32767);
  return Math.max(-32768, Math.min(32767, v));   // clamp
}
```

We write these into an `ArrayBuffer` using a `DataView`, forcing little-endian:

```ts
view.setInt16(offset, floatToInt16(sample), /* littleEndian = */ true);
```

### 4.4 Framing

`ScriptProcessorNode` delivers buffers sized in powers of two **at the context rate**
(we request 4096 samples @ 48 kHz ≈ 85 ms). After ÷3 resampling that's ~1365 samples
@ 16 kHz — *not* a clean 2048. So we push resampled samples into a **ring buffer**
and emit a WS frame **only when ≥2048 samples have accumulated**, leaving the
remainder for the next frame. This decouples the capture-callback size from our
fixed 2048-sample (128 ms) frame size.

> **Latency note:** `ScriptProcessorNode` is deprecated and runs on the main thread.
> It is fine for Stage 1. **TODO:** migrate to an `AudioWorklet` (runs on a dedicated
> audio thread, lower and more stable latency). Tracked in §13.

---

## 5. Wire protocol (client ↔ gateway)

The client opens **one WebSocket** per call to the gateway endpoint
`ws://<host>/v1/realtime` (dev: `ws://localhost:8080/v1/realtime`; `wss://` = TLS in
prod). Two message kinds flow on it:

### 5.1 `hello` — control message (text/JSON), sent once on open

Before any audio, the client sends a single **text** WS message announcing the
stream's format, so the server (and Stage-2 STT adapter) knows how to interpret the
bytes:

```jsonc
{
  "type": "hello",
  "role": "rep",            // which side: "rep" | "prospect"
  "sampleRate": 16000,      // Hz
  "encoding": "pcm_s16le",  // PCM, signed 16-bit, little-endian
  "channels": 1,            // mono
  "frameSamples": 2048,     // samples per audio frame (=128 ms @ 16 kHz)
  "client": { "app": "sales-copilot", "version": "0.1.0", "os": "darwin" }
}
```

The server may reply with a text `{ "type": "ready" }` before the client starts
sending audio. (Stage 1 just echoes; `ready` is added with the orchestrator.)

### 5.2 Audio frame — data message (binary)

Each audio frame is a **binary** WS message with a 4-byte header + PCM payload:

```
[1 byte: type][1 byte: channel][2 bytes: seq (uint16 LE)][ N bytes: Int16LE PCM ]
└──────────────────── header (4 bytes) ───────────────────┘└───── payload ─────┘
```


| Offset | Bytes | Field     | Type        | Meaning                                                                                                                                                                        |
| ------ | ----- | --------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 0      | 1     | `type`    | `uint8`     | Message type. `0x01` = audio frame. Reserves room for future types (e.g. `0x02` = marker) without ambiguity.                                                                   |
| 1      | 1     | `channel` | `uint8`     | **Who is speaking.** `0x00` = rep (mic), `0x01` = prospect (system audio, added next). This is how we separate speakers **cheaply** instead of using ML diarization (ADR-007). |
| 2      | 2     | `seq`     | `uint16 LE` | Per-channel **sequence number**, increments each frame, wraps 65535→0. Lets the server detect dropped/reordered frames by spotting gaps.                                       |
| 4      | N     | `payload` | `Int16LE[]` | The audio: `frameSamples × 2` bytes. For 2048 samples → **4096 bytes**.                                                                                                        |


So a standard audio frame is `**4 + 4096 = 4100 bytes`**. Encoder:

```ts
function encodeAudioFrame(channel: number, seq: number, pcm: Int16Array): ArrayBuffer {
  const buf = new ArrayBuffer(4 + pcm.byteLength);
  const dv = new DataView(buf);
  dv.setUint8(0, 0x01);              // type = audio
  dv.setUint8(1, channel);           // 0 rep / 1 prospect
  dv.setUint16(2, seq & 0xffff, true); // seq, little-endian
  new Int16Array(buf, 4).set(pcm);   // copy PCM after the header
  return buf;
}
```

### 5.3 Close / error codes

WebSocket closes carry a numeric code. We use standard codes plus an app range
(4000–4999, reserved by the WS spec for application use):


| Code   | Meaning                                                            | Who sends |
| ------ | ------------------------------------------------------------------ | --------- |
| `1000` | Normal closure (call ended, user stopped)                          | either    |
| `1001` | Going away (app quitting)                                          | client    |
| `4001` | Unauthorized (invalid/expired token) — *only when auth is enabled* | server    |
| `4002` | Protocol error (malformed frame, bad length, unknown type)         | server    |
| `4003` | Unsupported format in `hello` (e.g. wrong sampleRate/encoding)     | server    |
| `1011` | Internal server error                                              | server    |


---

## 6. Connection state machine (client)

The client manages the socket as an explicit state machine so reconnects, pausing,
and backpressure are deterministic.

```
        ┌─────────┐  connect()   ┌────────────┐  ws.onopen  ┌───────────────┐
        │  idle   │ ───────────► │ connecting │ ──────────► │ open(handshake)│
        └─────────┘              └────────────┘             └───────┬───────┘
             ▲                         ▲                      send hello
             │ close(1000)             │ backoff timer fires          │
             │                         │                              ▼
        ┌─────────┐                ┌──────────────┐  ws.onclose  ┌──────────┐
        │ closed  │ ◄───────────── │ reconnecting │ ◄─────────── │streaming │
        └─────────┘   give up      └──────────────┘   (error)    └────┬─────┘
                                                            pause ▲    │ resume
                                                                  │    ▼
                                                              ┌────────────┐
                                                              │  paused    │
                                                              └────────────┘
```

- **streaming → paused (`1.8`):** the instant "stop listening" control. We **stop
reading from the audio node immediately** (disconnect the capture node) so no
further frames are produced or sent. Resuming reconnects the node. Pause is local
and instant — it never depends on the network.
- **error → reconnecting:** on unexpected `onclose`/`onerror`, reconnect with
**exponential backoff + jitter**: delays `0.5s, 1s, 2s, 4s, 8s`, capped at `10s`,
with ±20% random jitter to avoid thundering-herd. On success, resend `hello` and
resume. After a max retry window, go to `closed` and surface an error.
- **Sequence numbers** continue across a reconnect per channel so the server can see
exactly how many frames were lost in the gap.

### 6.1 Backpressure (don't buffer stale audio)

For live audio, **old audio is useless** — if the network stalls, we must *drop*
frames, not queue them. We watch `ws.bufferedAmount` (bytes the socket hasn't yet
flushed to the network):

```ts
const MAX_BUFFERED = 256 * 1024; // 256 KB (~64 frames). Tune later.
if (ws.bufferedAmount > MAX_BUFFERED) {
  droppedFrames++;        // metric; skip sending this frame
} else {
  ws.send(frame);
}
```

We still advance `seq` for dropped frames so the gap is visible server-side.

---

## 7. Gateway-side handling (server)

`internal/gateway/ws.go` reads the WS connection (already authenticated +
tenant-scoped by `authMiddleware`; in dev `AUTH_DISABLED` injects the dev tenant).

Parsing each message:

1. **Text message** → JSON-decode; if `type == "hello"`, validate
  `encoding == "pcm_s16le" && sampleRate == 16000 && channels == 1`; else close
   `4003`. Store the stream descriptor on the session.
2. **Binary message** →
  - reject if `len < 4` or `(len-4)` is odd (PCM must be whole int16s) → close `4002`;
  - `type = b[0]`; if `!= 0x01` → close `4002`;
  - `channel = b[1]` (must be 0 or 1);
  - `seq = uint16(b[2]) | uint16(b[3])<<8` (little-endian);
  - track `lastSeq[channel]`; if `seq != lastSeq+1 (mod 65536)` increment a
  `frame_gap` metric;
  - `payload = b[4:]` (the Int16LE PCM).

**Stage 1 behavior:** echo the binary back (proves the round trip). **Stage 2:**
replace the echo with a forward of `(tenant, sessionID, channel, seq, payload)` to
the Call Session Orchestrator over gRPC, which opens an STT provider stream and emits
transcript messages back down this same WebSocket.

Go decode sketch (Stage 2 will move this into a typed reader):

```go
const (
    msgTypeAudio byte = 0x01
)
func parseAudioFrame(b []byte) (channel byte, seq uint16, pcm []byte, err error) {
    if len(b) < 4 || (len(b)-4)%2 != 0 {
        return 0, 0, nil, errBadFrame
    }
    if b[0] != msgTypeAudio {
        return 0, 0, nil, errBadFrame
    }
    channel = b[1]
    seq = uint16(b[2]) | uint16(b[3])<<8 // little-endian
    return channel, seq, b[4:], nil
}
```

---

## 8. Latency analysis (where this sits in the budget)

`ARCHITECTURE.md` §5.2 gives the hot path a 2–4 s end-to-end budget; capture must be
a small, fixed part of it.


| Component                                | Latency         | Note                                                    |
| ---------------------------------------- | --------------- | ------------------------------------------------------- |
| Frame accumulation                       | **128 ms**      | inherent: we wait for 2048 samples before sending       |
| Resample + encode                        | < 1 ms          | trivial arithmetic over 2048 samples                    |
| `ScriptProcessorNode` main-thread jitter | ~5–20 ms        | reduced later by AudioWorklet                           |
| WS send → gateway (LAN/localhost)        | < 5 ms          | persistent socket, no per-message handshake             |
| **Capture contribution**                 | **~135–155 ms** | leaves the bulk of the budget for STT + retrieval + LLM |


The 128 ms frame size is the dominant term and a deliberate latency/efficiency
trade-off; smaller frames cut latency but multiply message overhead.

---

## 9. Process & window architecture (Electron)

Electron has a **main** process (Node.js; owns windows, OS APIs) and **renderer**
processes (Chromium pages; run the UI + WebAudio). We use two windows:

- **Control window** — device picker, Connect/Disconnect, Pause. Runs `capture.ts`.
- **Overlay window** — frameless, transparent, `alwaysOnTop`, and
`**setContentProtection(true)`** so it is excluded from screen capture/share
(Windows `WDA_EXCLUDEFROMCAPTURE`, macOS `NSWindowSharingNone`). This makes the
coaching **private to the rep** while screen-sharing — *not* covert capture of the
prospect; it is paired with an explicit in-call disclosure (`7.7a`). UI is built in
Stage 7; Stage 1 just proves the capability.
- **Security:** renderers run with `contextIsolation: true`, `nodeIntegration: false`; any main↔renderer calls go through a typed `preload` bridge.

## 10. Data model & storage

**None.** Audio is ephemeral (`1.12`): captured → resampled → streamed → discarded.
No files, no database, no recording. Nothing touches disk.

## 11. Configuration

`client/src/config.ts`:

- `gatewayWsUrl` — default `ws://localhost:8080/v1/realtime` (override via
`GATEWAY_WS_URL`).
- `frameSamples` = 2048, `targetSampleRate` = 16000.
No secrets (dev gateway uses `AUTH_DISABLED=true`).

## 12. Error & edge-case handling


| Case                               | Detection                          | Behavior                                                      |
| ---------------------------------- | ---------------------------------- | ------------------------------------------------------------- |
| Mic permission denied              | `getUserMedia` rejects             | show actionable error; stay `idle`                            |
| Selected device unplugged mid-call | `MediaStreamTrack` `ended` event   | auto-switch to default, notify user                           |
| Network drop                       | `ws.onclose`/`onerror`             | enter `reconnecting` (backoff); keep `seq`                    |
| Server rejects format              | close `4003`                       | surface "unsupported audio format"                            |
| Malformed frame                    | server close `4002`                | client logs + reconnects                                      |
| Socket backpressure                | `bufferedAmount > 256 KB`          | drop frames, count `droppedFrames`                            |
| Sample-rate mismatch               | `AudioContext.sampleRate != 48000` | resampler uses actual ratio `ctxRate/16000`, not hard-coded 3 |


## 13. How to extend (next agent)

- **System/prospect audio (`1.3`/`1.4`):** add a native addon exposing an OS loopback
stream (macOS **CoreAudio Tap**, Windows **WASAPI loopback**). Run the same
resample/encode path, send with `channel = 0x01`. Now both voices arrive
separated by the `channel` byte — no ML diarization needed.
- **AudioWorklet:** replace `ScriptProcessorNode` with an `AudioWorklet` processor
(off-main-thread) for lower, steadier latency.
- **Stage 2 (STT):** swap the gateway echo for a gRPC forward to the orchestrator;
it opens a provider STT stream per `channel` and returns transcript text messages.
- **Opus encoding (optional):** if bandwidth matters over WAN, encode PCM→Opus before
send and decode server-side; adds a small CPU/latency cost.

## 14. Testing & verification

- Backend: `make up && AUTH_DISABLED=true make run-gateway`.
- Client: `cd client && npm install && npm start`.
- **Manual:** pick a mic → Connect → gateway logs `realtime session opened`; speaking
produces ~**8 frames/second** (1000 ms ÷ 128 ms); Pause halts frames instantly;
the overlay window does **not** appear in a Zoom/Meet screen-share.
- **Unit-testable pieces:** `resample48to16` (length = ⌊in/3⌋; a 48 kHz sine →
expected 16 kHz output), `floatToInt16` (clamping at ±1.0 → ±32767), `encodeAudioFrame`
(header bytes + length), gateway `parseAudioFrame` (rejects odd/short/bad-type).
- **Exit expectation (ROADMAP Stage 1):** reliable capture, hard stop-listening, no
persistence.

## 15. Observability

- **Gateway:** WS sessions gauge + a span per connection (Stage 0 telemetry); add a
`frame_gap_total` counter from seq-gap detection.
- **Client:** counters for `framesSent`, `droppedFrames`, `reconnects`, logged to
console now; wired to real telemetry with the overlay (Stage 7).

## 16. Decisions & rationale (the "why")

- **Electron + TS** (ARCH §10/standards): one codebase with OS-level audio + an
always-on-top private overlay a browser extension can't provide.
- **Mic first, system audio second:** mic works with zero native code via
`getUserMedia`, so we prove the full WS path immediately; loopback needs a native
addon and is built next, not as a blocker.
- **Channel byte for speaker separation (ADR-007):** cheaper and more reliable than
ML diarization — each physical source carries its own label.
- **Raw Int16 PCM over the wire first:** simplest to validate and exactly what STT
wants; Opus is an optional later optimization.
- **Drop, don't buffer, under backpressure:** stale audio is worthless for live
coaching.
- `**ScriptProcessorNode` now, `AudioWorklet` later:** trivial and reliable for v1;
worklet is a clean latency upgrade.

## 17. Open questions / TODO

- ✅ System/loopback native addon (the prospect's voice) — **done on macOS** and wired
in (§19, 2026-06-19). Windows WASAPI loopback (§19.4) still pending.
- `ScriptProcessorNode` → `AudioWorklet` migration.
- Replace the one-pole low-pass with a proper FIR / `OfflineAudioContext` resampler
if speech quality on noisy lines suffers.
- OS-permission UX (`1.5`) and device hot-swap edge cases (`1.6`).
- Packaging/code-signing for distribution (later).

## 19. System / prospect audio capture (Part 2 — implemented)

> **Status:** implemented on macOS (2026-06-19). The CoreAudio process-tap add-on
> (`client/native/syscapture/src/addon.mm`) is built **and wired into the app**:
> the main process loads it, the renderer drives it, and the prospect's voice now
> streams on channel `0x01` alongside the rep mic. Part 1 (rep mic) was already done.
> **Still pending:** Windows WASAPI loopback (§19.4) and the real-call exit check
> (§19.10) on macOS 14.4+ hardware.
>
> **Wiring (as built) — differs from the original §19.5 sketch.** The add-on runs in
> the **main process** (the sandboxed renderer can't load native modules), so prospect
> audio takes one extra hop the rep mic doesn't:
>
> 1. `main.ts` `require("syscapture")` (macOS only; missing/unsupported → rep-only).
> 2. Renderer calls `prospect.start()` over IPC on connect; main calls the add-on's
>    `start(cb)`, which returns the tap's hardware `sampleRate`.
> 3. Each native mono `Float32` chunk → `webContents.send("prospect:audio", …)` →
>    renderer's `onAudioChunk(prospectPipe, …)`.
> 4. From there it reuses the **exact** Part-1 pipeline (low-pass → decimate to 16 kHz
>    → Int16 → 2048-sample frames → `encodeAudioFrame(0x01, seq, …)` → same socket).
>
> The rep and prospect streams are now separate **`StreamPipe`** objects in
> `control.ts` (independent low-pass memory, fractional sample position, frame buffer,
> and sequence counter). **Pause** silences both because `onAudioChunk` is gated on
> `state === "streaming"`. The tap is independent of the WebSocket, so it survives
> reconnects and is torn down only on Disconnect / give-up (and on app `will-quit`).
> The IPC bridge (`prospect.start/stop/onAudio`) is exposed in `preload.ts`.
>
> The original plan below (§19.1–§19.10) is retained as the design rationale.

### 19.1 The problem this solves

Today the app hears only the **rep** (the local microphone). The **prospect** (the
buyer) is never in the mic — their voice arrives over the network and comes *out of*
the rep's speakers/headphones. To transcribe the buyer we must capture the sound the
**computer itself is playing** — called **system audio** (a.k.a. **loopback** audio,
because we "loop" the output back and read it as an input). That captured stream is
tagged `channel = 0x01` (prospect); the mic stays `channel = 0x00` (rep). Two
separately-labeled streams over one connection = the **two-stream split** (`1.4`),
which is how Stage 2 will know who said what **without** AI speaker-guessing
("diarization" = software that tries to infer which speaker is which) — ADR-007.

### 19.2 Why a native add-on is required

The rep-mic capture we already have uses `getUserMedia`, a **web API** available in
Electron's renderer (the Chromium page). But the renderer **cannot** grab arbitrary
system audio on macOS — the browser sandbox doesn't expose it. Capturing the system
output needs OS-specific APIs that only native code can call. So Part 2 adds a
**native add-on**: a small piece of compiled, OS-specific code (C/C++ / Objective-C on
macOS) that the Electron main process loads and calls like an ordinary function.

- **N-API / `node-addon-api`** — the stable interface Node/Electron provides for
writing native add-ons. "Stable" = it keeps working across Node/Electron versions
without rewriting for each bump. We build the add-on against this.

### 19.3 macOS — CoreAudio Tap (no driver, user-space)

- **CoreAudio** = macOS's built-in audio system. A **process tap**
(`AudioHardwareCreateProcessTap` / `CATapDescription`, **macOS 14.4+**) lets an app
create a *read-only listener* on audio output and receive copies of the samples.
- It runs in **user space** (a normal app — not the "kernel," the protected core of
the OS), installs **nothing**, and disappears when the app quits — no kernel driver,
fully reversible.
- It triggers one **permission prompt** the first time (macOS **TCC** = the
Transparency/Consent/Control system — the same machinery behind the mic and
screen-recording prompts). The user allows it; it's revocable in System Settings.
- Output: **Float32 PCM** (32-bit floating-point samples, range −1.0…+1.0) at the
hardware rate (typically 48 kHz) — the *exact same shape* the mic path already
produces, so it feeds the existing pipeline unchanged.

### 19.4 Windows — WASAPI loopback (no driver)

- **WASAPI** = Windows Audio Session API, the built-in Windows audio interface.
**Loopback** mode captures whatever is playing on an output device — also no driver,
no install. Built later; macOS is first (the dev machine is macOS).

### 19.5 How it plugs into the existing pipeline

The add-on hands PCM chunks to JavaScript through a **callback** (a function the add-on
calls each time a new buffer of audio is ready). From there it reuses the Part-1
pipeline in `client/src/renderer/control.ts` **verbatim**, only with a different
channel tag and its own counter:

```
native tap (macOS CoreAudio / Windows WASAPI)
      → Float32 PCM @ 48 kHz   (same format the mic produces)
      → resample48to16   (existing)        // 48 kHz → 16 kHz
      → floatToInt16     (existing)        // Float32 → Int16
      → accumulate 2048-sample frames      (existing)
      → encodeAudioFrame(channel = 0x01, seqProspect++, pcm)   // prospect tag
      → WebSocket.send(frame)              // same socket as the rep stream
```

- **Per-channel sequence counters.** The rep stream keeps its counter (channel `0x00`);
the prospect stream gets its **own** counter (channel `0x01`). The gateway is already
ready for this — Part 1's `session.lastSeq[2]`/`seqSeen[2]` track each channel
independently and `parseAudioFrame` already accepts `channel == 0x01`.

### 19.6 Lifecycle, pause, and consent

- The tap **starts on Connect** and **stops on Disconnect**.
- **Pause (`1.8`) must stop the prospect tap too** — the instant "stop listening"
control has to silence *both* streams, not just the mic.
- Nothing is stored; both streams stay ephemeral (`1.12`, ADR-008).

### 19.7 Build tooling

- `**binding.gyp`** + `**node-gyp**` — the standard build config + compiler that turn
the native source into a loadable `.node` binary.
- `**electron-rebuild**` (or `prebuildify`) — native add-ons must be compiled against
**Electron's** Node ABI, not the system Node's. ("ABI" = Application Binary Interface,
the binary-compatibility version; a mismatch makes the add-on fail to load.)

### 19.8 Files this will add / change

```
client/native/syscapture/        # NEW — the native add-on
  ├── binding.gyp                 # build config (node-gyp)
  ├── src/mac_tap.mm              # macOS CoreAudio Tap (Objective-C++)
  ├── src/win_loopback.cpp        # Windows WASAPI loopback (added later)
  └── src/addon.cc                # N-API glue: expose start(cb)/stop() to JS
client/src/renderer/control.ts    # CHANGE — wire the second (prospect) stream in
client/src/native.d.ts            # NEW — TypeScript types for the add-on's start/stop
```

### 19.9 Risks & edge cases

- **macOS < 14.4** — the process-tap API doesn't exist there. Plan: require 14.4+ for
the prospect stream initially (the rep stream still works on older macOS); a
virtual-audio-device fallback is explicitly avoided (it would install a system
component).
- **Echo** — the rep mic could faintly pick up the prospect's voice from the speakers.
Mitigations already present: the mic uses `echoCancellation`, and the two streams are
captured separately; headphones remove it entirely.
- **Sample-rate differences** — if the tap delivers a rate other than 48 kHz, reuse the
existing `resampleRatio = ctxRate / 16000` logic (§12), not a hard-coded 3.
- **Device hot-swap** — if the output device changes mid-call, restart the tap on the
new default device.

### 19.10 What "done" looks like (exit check)

On a real Zoom/Meet call with the rep on headphones: the gateway logs frames on
**both** channels; `gateway_frames_total{channel="rep"}` and `{channel="prospect"}`
both climb at ~8/sec; Pause halts both instantly; nothing is written to disk. This
completes Stage 1's two-sided capture and hands Stage 2 a clean, speaker-separated
stream.

## 20. Changelog

- `2026-06-07` — Techdoc created (Stage 1 start).
- `2026-06-07` — Rewritten as a full technical spec: concepts primer, WebAudio
resample/encode pipeline with formulas, WS wire protocol (hello JSON + binary frame
byte layout + close codes), connection state machine + backpressure, gateway-side
parsing, latency budget, error matrix. — setup
- `2026-06-13` — Added the Part 2 plan (§19): native prospect/system-audio capture via
macOS CoreAudio Tap / Windows WASAPI loopback, the two-stream split (channel `0x01`),
how it reuses the existing resample/encode/frame pipeline, build tooling,
lifecycle/pause/consent, files to add, and risks. Plan only — no code yet. — setup
- `2026-06-19` — **Wired Part 2 in (macOS).** The previously-built `syscapture` add-on
is now loaded by `main.ts` and driven end-to-end: new IPC `prospect:start`/`prospect:stop`
+ `prospect:audio` (main→renderer), exposed via `preload.ts`. Refactored `control.ts`
to a per-stream `StreamPipe` (rep + prospect) so each channel has its own resampler/
framer/seq; prospect frames go out on channel `0x01`; Pause gates both via `state`; the
tap is torn down on Disconnect/give-up and app `will-quit`; unsupported OS/denied
permission degrades to rep-only. `tsc` + `npm run build` green. Two-sided capture is
code-complete on macOS — pending: Windows WASAPI source and the real-call exit check. — build
- `2026-06-19` — **Verified live on the real macOS GUI client; fixed two blockers the
synthetic test had bypassed.**
  1. **Electron mic permission (rep stream).** `getUserMedia` failed with
  `NotAllowedError` and **no macOS prompt ever appeared**. Root cause: Electron's
  `session` denies a renderer `media` request by default, so Chromium rejects it
  *before* macOS TCC is ever consulted — hence no prompt. Fix in `main.ts`:
  `session.defaultSession.setPermissionRequestHandler` (and `setPermissionCheckHandler`)
  now return `true` for `permission === "media"`, and on macOS we call
  `systemPreferences.askForMediaAccess("microphone")` up front to trigger the native
  TCC prompt. Added `[copilot/main]` diagnostic logging of `getMediaAccessStatus`
  before/after the ask. Dev-mode caveat documented: TCC attributes the request to the
  *launching* app (Terminal/iTerm/VS Code), not "Electron"; a prior silent `denied`
  requires `tccutil reset Microphone`. In the packaged `.app` this is normal (own
  bundle identity + `NSMicrophoneUsageDescription`, already present in Electron 33).
  2. **Native add-on ABI mismatch (prospect stream).** `build/Release/syscapture.node`
  had been compiled against **system Node (ABI 108)**, but the app runs on
  **Electron 33 (ABI 130)** → Electron silently failed to load it, so the tap fell back
  to `unsupported — rep-only`. `electron-rebuild` skipped the module (it's a symlinked
  `file:` dependency it doesn't traverse). Fix: rebuilt explicitly against Electron 33's
  headers (`USING_ELECTRON_CONFIG_GYPI`), producing the ABI-130 binary at
  `native/syscapture/bin/darwin-arm64-130/syscapture.node`. End users never hit this —
  the correct binary ships inside the packaged app.
  **Result:** real client console showed `prospect tap: on @ 48000Hz`, `ws open —
  sending hello`, `ws message: {"type":"ready"}`; status reached **streaming** and both
  `gateway_frames_total{channel="rep"}` and `{channel="prospect"}` climbed. Live macOS
  capture confirmed working. Status stays **in-progress** — still pending: Windows WASAPI
  source and the §19.10 exit check on a real Zoom/Meet call with the rep on headphones. — build

