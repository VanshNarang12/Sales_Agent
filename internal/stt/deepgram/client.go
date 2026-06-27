// Package deepgram implements the stt.Provider interface against Deepgram's
// streaming speech-to-text API. The gateway opens one stream per (session, channel)
// and feeds it raw PCM; this package forwards that PCM to Deepgram over a WebSocket
// and parses the JSON results back into stt.TranscriptEvents.
//
// The Deepgram API key is held server-side only and never reaches the client
// (transcription_techdoc.md decision D2). See §5/§7 of that doc for the data flow
// and wire details.
package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/VanshNarang12/sales-agent/internal/stt"
)

const (
	// dialEndpoint is Deepgram's realtime streaming WebSocket base URL.
	dialEndpoint = "wss://api.deepgram.com/v1/listen"

	// sendBuffer bounds the per-stream outbound PCM queue. On overflow the oldest
	// frame is dropped (decision D5) — stale audio is useless in a live product.
	// Each frame is ~2048 samples (128 ms at 16 kHz), so 64 ≈ 8 s of slack.
	sendBuffer = 64

	// keepAliveInterval keeps the Deepgram socket open during silence; Deepgram
	// closes idle connections after ~10 s without audio or a KeepAlive.
	keepAliveInterval = 5 * time.Second
)

// Provider is a Deepgram-backed stt.Provider. It is safe for concurrent use and
// holds the API key + model selection for every stream it opens.
type Provider struct {
	apiKey        string
	model         string
	endpointingMs int
	dialer        *websocket.Dialer
	log           *slog.Logger
}

// Config configures a Provider. APIKey is required (fetched from the secrets vault by
// the caller); Model and EndpointingMs fall back to sensible defaults when zero.
type Config struct {
	APIKey        string
	Model         string // default "nova-3"
	EndpointingMs int    // default 300
	Logger        *slog.Logger
}

// New constructs a Deepgram Provider. It does not dial; connections open per stream.
func New(cfg Config) *Provider {
	if cfg.Model == "" {
		cfg.Model = "nova-3"
	}
	if cfg.EndpointingMs == 0 {
		cfg.EndpointingMs = 300
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Provider{
		apiKey:        cfg.APIKey,
		model:         cfg.Model,
		endpointingMs: cfg.EndpointingMs,
		dialer:        websocket.DefaultDialer,
		log:           cfg.Logger,
	}
}

// OpenStream dials Deepgram for one speaker stream and starts the read/write pumps.
// It returns ErrProviderUnavailable (wrapped) when the key is missing or the dial fails.
func (p *Provider) OpenStream(ctx context.Context, cfg stt.StreamConfig) (stt.Stream, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("%w: missing API key", stt.ErrProviderUnavailable)
	}

	dialURL := p.buildURL(cfg)
	header := http.Header{"Authorization": []string{"Token " + p.apiKey}}

	conn, _, err := p.dialer.DialContext(ctx, dialURL, header)
	if err != nil {
		return nil, fmt.Errorf("%w: dial: %v", stt.ErrProviderUnavailable, err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	s := &stream{
		cfg:    cfg,
		conn:   conn,
		sendCh: make(chan []byte, sendBuffer),
		events: make(chan stt.TranscriptEvent, sendBuffer),
		cancel: cancel,
		done:   make(chan struct{}),
		log:    p.log.With("speaker", string(cfg.Speaker), "session", cfg.SessionID),
	}
	go s.writePump(streamCtx)
	go s.readPump(streamCtx)
	return s, nil
}

// buildURL assembles the Deepgram streaming URL with our fixed audio format plus the
// per-stream model/endpointing/keyterm parameters. Our PCM is 16 kHz signed-16-bit
// little-endian mono, which Deepgram calls encoding=linear16 (no resample needed).
func (p *Provider) buildURL(cfg stt.StreamConfig) string {
	q := url.Values{}
	q.Set("model", p.model)
	q.Set("encoding", "linear16")
	q.Set("sample_rate", strconv.Itoa(cfg.SampleRate))
	q.Set("channels", strconv.Itoa(cfg.Channels))
	q.Set("interim_results", "true")                    // partials (feature 2.2)
	q.Set("punctuate", "true")                          // feature 2.5
	q.Set("smart_format", "true")                       // casing/formatting (feature 2.5)
	q.Set("endpointing", strconv.Itoa(p.endpointingMs)) // end-of-turn (feature 2.4)
	for _, kt := range cfg.Keyterms {                   // per-tenant boosting (feature 2.6)
		q.Add("keyterm", kt)
	}
	return dialEndpoint + "?" + q.Encode()
}

// stream is one open Deepgram session for a single speaker. Send is owned by a single
// goroutine (the gateway's per-channel write path); Events may be read elsewhere.
type stream struct {
	cfg    stt.StreamConfig
	conn   *websocket.Conn
	sendCh chan []byte
	events chan stt.TranscriptEvent
	cancel context.CancelFunc
	done   chan struct{} // closed once both pumps have exited
	log    *slog.Logger
}

// Send enqueues one PCM frame without blocking. If the buffer is full it drops the
// oldest queued frame and retries, so a slow provider never stalls audio intake (D5).
func (s *stream) Send(pcm []byte) error {
	for {
		select {
		case s.sendCh <- pcm:
			return nil
		default:
			select {
			case <-s.sendCh: // drop oldest, then retry the enqueue
			default:
			}
		}
	}
}

// Events returns the transcript channel. It is closed when the stream tears down.
func (s *stream) Events() <-chan stt.TranscriptEvent { return s.events }

// Close stops both pumps and releases the socket. It is idempotent: a second call is a
// no-op because cancel and the pump exit are guarded by streamCtx / done.
func (s *stream) Close() error {
	s.cancel()
	return nil
}

// writePump drains sendCh to the socket as binary frames and sends periodic KeepAlives.
// On ctx cancellation it sends Deepgram's CloseStream (to flush a trailing final), then
// closes the connection.
func (s *stream) writePump(ctx context.Context) {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Ask Deepgram to finalize, then close. Best-effort — errors are terminal anyway.
			_ = s.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"CloseStream"}`))
			_ = s.conn.Close()
			return
		case pcm := <-s.sendCh:
			if err := s.conn.WriteMessage(websocket.BinaryMessage, pcm); err != nil {
				s.log.Warn("deepgram write failed", "err", err)
				s.cancel()
				return
			}
		case <-ticker.C:
			if err := s.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"KeepAlive"}`)); err != nil {
				s.cancel()
				return
			}
		}
	}
}

// readPump reads Deepgram JSON messages, converts Results into TranscriptEvents, and
// emits them on s.events. It exits on read error or ctx cancellation, closing events.
func (s *stream) readPump(ctx context.Context) {
	defer close(s.events)
	defer close(s.done)
	for {
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			if ctx.Err() == nil {
				s.log.Warn("deepgram read failed", "err", err)
			}
			s.cancel()
			return
		}
		ev, ok := parseResult(msg, s.cfg)
		if !ok {
			continue // non-Results message, or an empty transcript — skip
		}
		select {
		case s.events <- ev:
		case <-ctx.Done():
			return
		}
	}
}

// dgMessage is the subset of Deepgram's streaming JSON we consume. Deepgram also emits
// "Metadata" and "UtteranceEnd" messages, which carry no transcript and are ignored.
type dgMessage struct {
	Type        string  `json:"type"`
	IsFinal     bool    `json:"is_final"`
	SpeechFinal bool    `json:"speech_final"`
	Start       float64 `json:"start"`    // segment start, seconds
	Duration    float64 `json:"duration"` // segment length, seconds
	Channel     struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
}

// parseResult converts one Deepgram message into a TranscriptEvent. It returns ok=false
// for non-transcript messages and for empty/whitespace transcripts (Deepgram emits empty
// interims during silence). Pure function — unit-tested directly without a socket.
func parseResult(msg []byte, cfg stt.StreamConfig) (stt.TranscriptEvent, bool) {
	var m dgMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		return stt.TranscriptEvent{}, false
	}
	if m.Type != "" && m.Type != "Results" {
		return stt.TranscriptEvent{}, false
	}
	if len(m.Channel.Alternatives) == 0 {
		return stt.TranscriptEvent{}, false
	}
	alt := m.Channel.Alternatives[0]
	if strings.TrimSpace(alt.Transcript) == "" {
		return stt.TranscriptEvent{}, false
	}
	return stt.TranscriptEvent{
		TenantID:    cfg.TenantID,
		SessionID:   cfg.SessionID,
		Speaker:     cfg.Speaker,
		IsFinal:     m.IsFinal,
		SpeechFinal: m.SpeechFinal,
		Text:        alt.Transcript,
		StartMs:     int64(m.Start * 1000),
		EndMs:       int64((m.Start + m.Duration) * 1000),
		Confidence:  alt.Confidence,
	}, true
}
