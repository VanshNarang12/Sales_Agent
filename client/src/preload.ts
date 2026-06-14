// Preload — the one controlled doorway between the locked-down page and the app.
// Runs before the page loads; exposes ONLY the config values, nothing else.
import { contextBridge, ipcRenderer } from "electron";

// Synchronous on purpose: the page's scripts must not run before config exists.
// One blocking call at startup, never again.
const config = ipcRenderer.sendSync("config:get");

contextBridge.exposeInMainWorld("copilot", { config });
