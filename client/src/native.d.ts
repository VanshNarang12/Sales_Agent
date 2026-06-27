// TypeScript types for the native system-audio capture add-on (client/native/syscapture).
// The add-on is loaded in the Electron main process. See techdocs/audio_capture_techdoc.md §19.
declare module "syscapture" {
  /** A chunk of mono Float32 PCM samples (range -1..1) tapped from system output. */
  export type AudioChunkCallback = (samples: Float32Array) => void;

  /**
   * Start capturing system (prospect) audio. On first use macOS shows a one-time
   * audio-capture permission prompt. Returns the tap's native sample rate in Hz so the
   * caller can resample to 16 kHz.
   */
  export function start(onAudio: AudioChunkCallback): { sampleRate: number };

  /** Stop capturing and release the tap, aggregate device, and IO callback. */
  export function stop(): void;
}
