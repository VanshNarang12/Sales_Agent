// Electron main process — owns the windows and the app lifecycle.
// Two windows: a normal control window (device picker, connect, pause) and the
// frameless always-on-top overlay that is excluded from screen capture (7.7).
import { app, BrowserWindow, ipcMain, session, systemPreferences } from "electron";
import * as path from "path";

import { config } from "./config";

// Hold references so the windows aren't garbage-collected mid-call.
let controlWindow: BrowserWindow | null = null;
let overlayWindow: BrowserWindow | null = null;

// Native system-audio (the prospect's voice) capture. It MUST live in the main
// process — the renderer runs with nodeIntegration disabled and cannot load
// native modules. It is OPTIONAL: macOS-only, and only when built against the
// 14.4+ CoreAudio tap SDK. If it isn't available the app still runs rep-only.
// See techdocs/audio_capture_techdoc.md §19.
let syscapture: typeof import("syscapture") | null = null;
if (process.platform === "darwin") {
  try {
    syscapture = require("syscapture");
  } catch {
    syscapture = null; // not built / unsupported — degrade to rep-only
  }
}
let prospectRunning = false;

function createControlWindow(): void {
  controlWindow = new BrowserWindow({
    width: 420,
    height: 380,
    title: "Sales Copilot",
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  controlWindow.loadFile(path.join(__dirname, "renderer", "control.html"));
  controlWindow.setContentProtection(true);
  controlWindow.on("closed", () => {
    controlWindow = null;
    // The control window is the app's off-switch: closing it must never leave
    // the overlay floating (it has no close button of its own).
    app.quit();
  });
}

function createOverlayWindow(): void {
  overlayWindow = new BrowserWindow({
    width: 340,
    height: 170,
    frame: false, // no title bar — looks like a floating card
    transparent: true, // background can be see-through
    alwaysOnTop: true, // floats above Zoom/Meet
    skipTaskbar: true, // don't clutter the taskbar/dock
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  // Exclude from screen recording/sharing: the rep sees the card, a shared
  // screen does not (Windows WDA_EXCLUDEFROMCAPTURE, macOS NSWindowSharingNone).
  overlayWindow.setContentProtection(true);
  // Meeting apps float high too — "screen-saver" level stays above them.
  overlayWindow.setAlwaysOnTop(true, "screen-saver");
  // macOS: follow the user onto full-screen Spaces (e.g. full-screen Zoom).
  overlayWindow.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true });
  overlayWindow.loadFile(path.join(__dirname, "renderer", "overlay.html"));
  overlayWindow.on("closed", () => {
    overlayWindow = null;
  });
}

// The sandboxed page can't read config.ts itself — preload asks for it over IPC.
ipcMain.on("config:get", (event) => {
  event.returnValue = {
    gatewayWsUrl: config.gatewayWsUrl,
    frameSize: config.frameSize,
    version: app.getVersion(),
    os: process.platform,
  };
});

// ---- Prospect (system-audio) capture, driven by the renderer ----------------
// The renderer starts the tap when a call connects. We forward each mono PCM
// chunk (Float32) to that same renderer over IPC; it runs the existing
// resample → Int16 → frame → WebSocket pipeline, tagged as the prospect channel.
// This handler NEVER throws into the renderer: an unsupported OS, a denied
// permission, or a missing build all resolve to {supported:false} so the call
// continues rep-only. The native start() returns the tap's hardware sample rate
// so the renderer can resample it to 16 kHz.
ipcMain.handle("prospect:start", (event) => {
  if (!syscapture) return { supported: false, sampleRate: 0 };
  if (prospectRunning) return { supported: true, sampleRate: 0 };
  const sender = event.sender;
  try {
    const { sampleRate } = syscapture.start((samples) => {
      // Runs on the main thread (marshalled off the CoreAudio thread by the
      // add-on). Drop the chunk if the window is gone — never write to a dead
      // renderer.
      if (!sender.isDestroyed()) sender.send("prospect:audio", samples);
    });
    prospectRunning = true;
    return { supported: true, sampleRate };
  } catch {
    return { supported: false, sampleRate: 0 }; // macOS <14.4 / denied / not built
  }
});

ipcMain.handle("prospect:stop", () => {
  if (syscapture && prospectRunning) {
    syscapture.stop();
    prospectRunning = false;
  }
});

app.whenReady().then(async () => {
  // Electron denies the renderer's microphone request by default — and because
  // Chromium denies it first, macOS never even shows its own prompt (the cause
  // of getUserMedia's NotAllowedError). Explicitly grant the mic ("media").
  session.defaultSession.setPermissionRequestHandler((_wc, permission, callback) => {
    callback(permission === "media");
  });
  session.defaultSession.setPermissionCheckHandler((_wc, permission) => permission === "media");

  // macOS: trigger the native TCC microphone prompt up front so the user grants
  // it before the first call (no-op on other platforms). The system-audio tap
  // asks for its own, separate permission the first time it starts.
  if (process.platform === "darwin") {
    const status = systemPreferences.getMediaAccessStatus("microphone");
    console.log("[copilot/main] mic access status before ask:", status);
    try {
      const granted = await systemPreferences.askForMediaAccess("microphone");
      console.log("[copilot/main] askForMediaAccess granted:", granted);
    } catch (e) {
      console.error("[copilot/main] askForMediaAccess error:", e);
    }
  }

  createControlWindow();
  createOverlayWindow();
});

// Quit fully when the windows are closed — no invisible background app that
// could still be listening to the mic. Closed = silent (1.8 / trust-first).
app.on("window-all-closed", () => {
  app.quit();
});

// Belt-and-braces: never leave the system-audio tap running past the app's life.
app.on("will-quit", () => {
  if (syscapture && prospectRunning) {
    try {
      syscapture.stop();
    } catch {
      // already torn down — nothing to do
    }
    prospectRunning = false;
  }
});
