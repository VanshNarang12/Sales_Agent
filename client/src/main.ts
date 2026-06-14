// Electron main process — owns the windows and the app lifecycle.
// Two windows: a normal control window (device picker, connect, pause) and the
// frameless always-on-top overlay that is excluded from screen capture (7.7).
import { app, BrowserWindow, ipcMain } from "electron";
import * as path from "path";

import { config } from "./config";

// Hold references so the windows aren't garbage-collected mid-call.
let controlWindow: BrowserWindow | null = null;
let overlayWindow: BrowserWindow | null = null;

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

app.whenReady().then(() => {
  createControlWindow();
  createOverlayWindow();
});

// Quit fully when the windows are closed — no invisible background app that
// could still be listening to the mic. Closed = silent (1.8 / trust-first).
app.on("window-all-closed", () => {
  app.quit();
});
