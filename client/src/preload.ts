// Preload — the one controlled doorway between the locked-down page and the app.
// Runs before the page loads; exposes ONLY the config values, nothing else.
import { contextBridge, ipcRenderer } from "electron";

// Synchronous on purpose: the page's scripts must not run before config exists.
// One blocking call at startup, never again.
const config = ipcRenderer.sendSync("config:get");

contextBridge.exposeInMainWorld("copilot", {
  config,
  // Prospect (system-audio) capture: the native tap lives in the main process,
  // so the renderer drives it over IPC and receives the PCM chunks here. The
  // renderer feeds those chunks into its existing resample/encode/send pipeline.
  prospect: {
    // Resolves {supported, sampleRate}; supported:false means run rep-only.
    start: (): Promise<{ supported: boolean; sampleRate: number }> =>
      ipcRenderer.invoke("prospect:start"),
    stop: (): Promise<void> => ipcRenderer.invoke("prospect:stop"),
    // Register once; the callback fires for every captured mono PCM chunk.
    onAudio: (cb: (samples: Float32Array) => void): void => {
      ipcRenderer.on("prospect:audio", (_e, samples: Float32Array) => cb(samples));
    },
  },
});
