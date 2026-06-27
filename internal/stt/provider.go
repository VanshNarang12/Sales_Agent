// Package stt is the speech-to-text substrate for the realtime plane. It turns the
// per-channel PCM audio that arrives at the gateway into a stream of speaker-labeled
// transcript events (partials + finals) for downstream detection and retrieval.
//
// The package is provider-agnostic: callers depend only on the Provider/Stream
// interfaces defined here, never on a concrete vendor. Deepgram is the first
// implementation (internal/stt/deepgram); others (AssemblyAI, Gladia, ElevenLabs)
// can be added without touching the gateway. See techdocs/transcription_techdoc.md
// (Stage 2, decisions D1/D4).
package stt

import (
	"context"
	"errors"
)

// Speaker identifies who is talking. It is derived from the physical audio channel
// (decision D3) rather than ML diarization: rep = channel 0x00, prospect = 0x01.
type Speaker string

const (
	SpeakerRep      Speaker = "rep"      // the salesperson (microphone, channel 0x00)
	SpeakerProspect Speaker = "prospect" // the other party (system audio, channel 0x01)
)

// ErrProviderUnavailable is returned by Provider.OpenStream when the backing STT
// service cannot be reached or is not configured (e.g. missing API key). Callers
// degrade to audio-only — a transcription failure never blocks the call.
var ErrProviderUnavailable = errors.New("stt: provider unavailable")

// TranscriptEvent is one transcription result for a single speaker. Its field set
// maps 1:1 to the future gRPC message used when transcription moves behind the Call
// Session Orchestrator (techdoc §10), so adopting that path is a transport change only.
type TranscriptEvent struct {
	TenantID  string  // tenant that owns the call (tenant isolation, coding-standard §4)
	SessionID string  // realtime session this transcript belongs to
	Speaker   Speaker // who spoke (rep | prospect), from the channel

	// IsFinal distinguishes a partial (interim, may still change as more audio
	// arrives — drives Stage 3 detection) from a final (locked segment — drives
	// Stage 5 retrieval). Feature 2.2.
	IsFinal bool
	// SpeechFinal marks end-of-turn / end-of-speech: the speaker paused long enough
	// that their turn is considered complete. Feature 2.4.
	SpeechFinal bool
	Text        string  // the transcribed text for this segment (punctuated/cased, feature 2.5)
	StartMs     int64   // segment start, milliseconds from session start
	EndMs       int64   // segment end, milliseconds from session start
	Confidence  float64 // provider confidence in [0,1]; 0 if the provider omits it
}

// StreamConfig describes one audio stream to be transcribed. One stream is opened
// per (session, channel), so exactly one Speaker per stream (decision D4). The audio
// format mirrors the capture pipeline (16 kHz, signed 16-bit little-endian PCM, mono),
// so no resampling is needed before the provider.
type StreamConfig struct {
	TenantID   string
	SessionID  string
	Speaker    Speaker
	SampleRate int    // samples per second; 16000 for our pipeline
	Encoding   string // PCM encoding; "pcm_s16le" (Deepgram "linear16")
	Channels   int    // always 1 — each stream carries a single speaker

	// Keyterms are per-tenant product/competitor names boosted for recognition
	// accuracy (feature 2.6 / ARCH §7.1). Empty until the knowledge base (Stage 4)
	// populates them; providers that do not support boosting ignore this.
	Keyterms []string
}

// Provider opens transcription streams. A Provider is safe for concurrent use and is
// typically a long-lived singleton holding vendor credentials (server-side only —
// the API key never leaves the backend, decision D2).
type Provider interface {
	// OpenStream starts a new transcription stream for cfg. The returned Stream is
	// fed PCM via Send and emits results on Events until Close is called or ctx is
	// cancelled. Returns ErrProviderUnavailable (wrapped) if the provider cannot start.
	OpenStream(ctx context.Context, cfg StreamConfig) (Stream, error)
}

// Stream is one open transcription session for a single speaker. It is NOT safe for
// concurrent Send calls — a single goroutine owns Send for a given stream (the
// gateway's per-channel write path). Events may be consumed from a different goroutine.
type Stream interface {
	// Send submits one frame of raw PCM (signed 16-bit little-endian, matching
	// StreamConfig). Send is non-blocking: under provider backpressure the oldest
	// buffered audio is dropped rather than blocking the caller (decision D5).
	Send(pcm []byte) error

	// Events returns the channel of transcript results for this stream. The channel
	// is closed when the stream ends (after Close, ctx cancellation, or a terminal
	// provider error). Range over it to consume; a closed channel signals teardown.
	Events() <-chan TranscriptEvent

	// Close stops the stream and releases provider resources. It flushes any pending
	// final result, then closes the Events channel. Close is idempotent.
	Close() error
}
