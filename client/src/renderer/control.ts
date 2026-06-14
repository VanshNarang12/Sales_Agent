// Stage 1 capture pipeline (techdocs/audio_capture_techdoc.md §4–§6):
// mic → resample to 16 kHz → Int16 → 2048-sample frames → binary WS to gateway.
// Global script (no imports) — loaded directly by control.html via <script src>.

// ---- 1. Setup: preload bridge + page elements ------------------------------

interface CopilotBridge {
  config: {
    gatewayWsUrl: string;
    frameSize: number;
    version: string;
    os: string;
  };
}
interface Window {
  copilot: CopilotBridge;
}

const cfg = window.copilot.config;

const deviceSelect = document.getElementById("deviceSelect") as HTMLSelectElement;
const connectBtn = document.getElementById("connectBtn") as HTMLButtonElement;
const pauseBtn = document.getElementById("pauseBtn") as HTMLButtonElement;
const statusEl = document.getElementById("status") as HTMLSpanElement;
const framesSentEl = document.getElementById("framesSent") as HTMLSpanElement;
const framesDroppedEl = document.getElementById("framesDropped") as HTMLSpanElement;
const reconnectsEl = document.getElementById("reconnects") as HTMLSpanElement;

// Wire-protocol constants — must match the techdoc §5 and the gateway.
const TARGET_RATE = 16000; // Hz — what STT providers expect
const FRAME_SAMPLES = cfg.frameSize; // 2048 samples = 128 ms @ 16 kHz
const MSG_TYPE_AUDIO = 0x01;
const CH_REP = 0x00; // channel byte: 0 = rep mic (prospect loopback = 0x01 later)
const MAX_BUFFERED = 256 * 1024; // backpressure threshold (§6.1)
const RECONNECT_DELAYS_MS = [500, 1000, 2000, 4000, 8000]; // backoff ladder (§6)
const MAX_RECONNECT_ATTEMPTS = 8;
const LP_ALPHA = 0.45; // one-pole low-pass coefficient (§4.2)

type State = "idle" | "connecting" | "streaming" | "paused" | "reconnecting" | "closed";

let state: State = "idle";
let ws: WebSocket | null = null;
let micStream: MediaStream | null = null;
let audioCtx: AudioContext | null = null;
let sourceNode: MediaStreamAudioSourceNode | null = null;
let processorNode: ScriptProcessorNode | null = null;
let resampleRatio = 3; // recomputed from the real hardware rate on connect (§12)

let seq = 0; // per-channel frame counter; survives reconnects, resets on disconnect
let framesSent = 0;
let framesDropped = 0;
let reconnects = 0;
let reconnectAttempt = 0;
let reconnectTimer: number | null = null;
let userStopped = false;

// Resampler + framer state (carried across audio callbacks).
let lp = 0; // low-pass filter memory
let pickAt = 0; // fractional index of the next sample to keep
const frameBuf = new Int16Array(FRAME_SAMPLES); // reused; encode copies it out
let frameFill = 0;

// ---- 2. Mic dropdown --------------------------------------------------------

async function populateDevices(): Promise<void> {
  const devices = await navigator.mediaDevices.enumerateDevices();
  const mics = devices.filter((d) => d.kind === "audioinput");
  const selected = deviceSelect.value;
  deviceSelect.innerHTML = "";
  mics.forEach((m, i) => {
    const opt = document.createElement("option");
    opt.value = m.deviceId;
    // Labels are blank until mic permission is granted — show a placeholder.
    opt.text = m.label || `Microphone ${i + 1}`;
    deviceSelect.add(opt);
  });
  if (selected) deviceSelect.value = selected;
}

navigator.mediaDevices.addEventListener("devicechange", () => void populateDevices());
void populateDevices();

// ---- 3. Audio pipeline: mic → 16 kHz Int16 samples --------------------------

async function openMic(): Promise<void> {
  micStream = await navigator.mediaDevices.getUserMedia({
    audio: {
      deviceId: deviceSelect.value ? { exact: deviceSelect.value } : undefined,
      channelCount: 1,
      echoCancellation: true,
      noiseSuppression: true,
      autoGainControl: true,
    },
  });
  micStream.getAudioTracks()[0].addEventListener("ended", onMicLost);

  audioCtx = new AudioContext();
  await audioCtx.resume();
  // Use the real hardware rate — not a hard-coded 48000 (§12 edge case).
  resampleRatio = audioCtx.sampleRate / TARGET_RATE;

  sourceNode = audioCtx.createMediaStreamSource(micStream);
  processorNode = audioCtx.createScriptProcessor(4096, 1, 1);
  processorNode.onaudioprocess = (e: AudioProcessingEvent) =>
    onAudioChunk(e.inputBuffer.getChannelData(0));
  sourceNode.connect(processorNode);
  // A ScriptProcessorNode only fires when routed to an output; its output
  // buffer stays zero-filled, so nothing is audible.
  processorNode.connect(audioCtx.destination);
}

// Low-pass every sample (anti-aliasing), keep every `resampleRatio`-th one,
// convert Float32 [−1,1] → Int16 [−32768,32767], emit a frame per 2048 samples.
function onAudioChunk(input: Float32Array): void {
  if (state !== "streaming") return;
  for (let i = 0; i < input.length; i++) {
    lp += LP_ALPHA * (input[i] - lp);
    if (i >= pickAt) {
      pickAt += resampleRatio;
      pushSample(lp);
    }
  }
  pickAt -= input.length; // carry fractional position into the next callback
}

function pushSample(s: number): void {
  const v = Math.round(s * 32767);
  frameBuf[frameFill++] = v > 32767 ? 32767 : v < -32768 ? -32768 : v; // clamp
  if (frameFill === FRAME_SAMPLES) {
    frameFill = 0;
    sendFrame(frameBuf);
  }
}

// ---- 4. Frame encoding + send (wire format, techdoc §5.2) -------------------

// [1B type][1B channel][2B seq LE][N bytes Int16LE PCM]
function encodeAudioFrame(channel: number, seqNum: number, pcm: Int16Array): ArrayBuffer {
  const buf = new ArrayBuffer(4 + pcm.byteLength);
  const dv = new DataView(buf);
  dv.setUint8(0, MSG_TYPE_AUDIO);
  dv.setUint8(1, channel);
  dv.setUint16(2, seqNum & 0xffff, true); // little-endian
  new Int16Array(buf, 4).set(pcm);
  return buf;
}

function sendFrame(pcm: Int16Array): void {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  // Backpressure: live audio must never queue — drop, but still advance seq
  // so the server sees the gap (§6.1).
  if (ws.bufferedAmount > MAX_BUFFERED) {
    framesDropped++;
    seq = (seq + 1) & 0xffff;
    updateCounters();
    return;
  }
  ws.send(encodeAudioFrame(CH_REP, seq, pcm));
  seq = (seq + 1) & 0xffff;
  framesSent++;
  updateCounters();
}

function sendHello(): void {
  if (!ws) return;
  ws.send(
    JSON.stringify({
      type: "hello",
      role: "rep",
      sampleRate: TARGET_RATE,
      encoding: "pcm_s16le",
      channels: 1,
      frameSamples: FRAME_SAMPLES,
      client: { app: "sales-copilot", version: cfg.version, os: cfg.os },
    })
  );
}

// ---- 5. Connection state machine (techdoc §6) -------------------------------

async function connect(): Promise<void> {
  userStopped = false;
  setState("connecting");
  try {
    await openMic();
  } catch {
    showError("mic unavailable or permission denied");
    setState("idle");
    return;
  }
  void populateDevices(); // real labels become available after permission
  openSocket();
}

function openSocket(): void {
  ws = new WebSocket(cfg.gatewayWsUrl);
  ws.binaryType = "arraybuffer";
  ws.onopen = () => {
    reconnectAttempt = 0;
    sendHello();
    setState("streaming");
  };
  // Stage 0/1 gateway echoes our frames back — nothing to do with them yet.
  ws.onmessage = () => {};
  ws.onclose = () => {
    ws = null;
    if (!userStopped) scheduleReconnect();
  };
  ws.onerror = () => {}; // onclose always follows
}

function scheduleReconnect(): void {
  if (reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
    showError("connection lost — gave up after retries");
    teardown();
    setState("closed");
    return;
  }
  const base =
    RECONNECT_DELAYS_MS[Math.min(reconnectAttempt, RECONNECT_DELAYS_MS.length - 1)];
  const jitter = base * 0.2 * (Math.random() * 2 - 1); // ±20%, avoids thundering herd
  reconnectAttempt++;
  reconnects++;
  setState("reconnecting");
  updateCounters();
  reconnectTimer = window.setTimeout(openSocket, base + jitter);
}

function disconnect(): void {
  userStopped = true;
  teardown();
  seq = 0; // fresh session next connect (seq persists only across reconnects)
  setState("idle");
}

function teardown(): void {
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  if (ws) {
    ws.onclose = null; // suppress the reconnect path on a deliberate close
    ws.close(1000);
    ws = null;
  }
  processorNode?.disconnect();
  sourceNode?.disconnect();
  micStream?.getTracks().forEach((t) => t.stop()); // releases the mic (OS light off)
  void audioCtx?.close();
  processorNode = null;
  sourceNode = null;
  micStream = null;
  audioCtx = null;
  frameFill = 0;
  lp = 0;
  pickAt = 0;
  reconnectAttempt = 0;
}

function onMicLost(): void {
  if (state === "idle" || state === "closed") return;
  showError("microphone disconnected");
  disconnect();
}

// ---- 6. Pause + UI ----------------------------------------------------------

// Instant, local "stop listening" (1.8): physically detach the mic from the
// pipeline — no samples are produced, nothing depends on the network.
function pause(): void {
  if (!sourceNode || !processorNode) return;
  sourceNode.disconnect(processorNode);
  setState("paused");
}

function resume(): void {
  if (!sourceNode || !processorNode) return;
  sourceNode.connect(processorNode);
  setState("streaming");
}

function setState(s: State): void {
  state = s;
  statusEl.textContent = s;
  statusEl.className = "st-" + s;
  updateUI();
}

function showError(msg: string): void {
  statusEl.textContent = msg;
  statusEl.className = "st-reconnecting";
}

function updateUI(): void {
  const active = state !== "idle" && state !== "closed";
  connectBtn.textContent = active ? "Disconnect" : "Connect";
  pauseBtn.disabled = state !== "streaming" && state !== "paused";
  pauseBtn.textContent = state === "paused" ? "Resume" : "Pause";
  deviceSelect.disabled = active;
}

function updateCounters(): void {
  framesSentEl.textContent = String(framesSent);
  framesDroppedEl.textContent = String(framesDropped);
  reconnectsEl.textContent = String(reconnects);
}

connectBtn.addEventListener("click", () => {
  if (state === "idle" || state === "closed") void connect();
  else disconnect();
});

pauseBtn.addEventListener("click", () => {
  if (state === "streaming") pause();
  else if (state === "paused") resume();
});

updateUI();
