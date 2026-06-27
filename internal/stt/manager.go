package stt

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Manager coordinates transcription for the whole process. It holds a single Provider
// and mints a per-call Session on demand. It is safe for concurrent use and is created
// once at startup. See transcription_techdoc.md §5.
type Manager struct {
	provider Provider
	log      *slog.Logger

	// Fixed audio format for every stream — mirrors the capture pipeline (decision D4),
	// so no resampling happens between the gateway and the provider.
	sampleRate int
	encoding   string
}

// NewManager builds a Manager around an STT Provider (e.g. the Deepgram adapter).
func NewManager(p Provider, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		provider:   p,
		log:        log,
		sampleRate: 16000,
		encoding:   "pcm_s16le",
	}
}

// EventFunc consumes one transcript event. It is invoked from the manager's supervisor
// goroutines (one per speaker), so it MUST be safe for concurrent calls and must not
// block — slow consumers stall transcript delivery. Today the gateway passes a func that
// logs + records metrics; Stage 3 detection becomes the real consumer.
type EventFunc func(TranscriptEvent)

// Channel indices mirror the wire protocol: 0x00 = rep, 0x01 = prospect.
const (
	chanRep      = 0
	chanProspect = 1
	numChannels  = 2
)

// reconnect backoff schedule (per channel), capped so a persistently-down provider is
// retried at a steady, low rate without giving up — the call keeps running audio-only
// until transcription recovers.
const (
	backoffBase = 250 * time.Millisecond
	backoffMax  = 5 * time.Second
)

var (
	streamsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "stt_streams_active",
		Help: "Open STT provider streams, by speaker.",
	}, []string{"speaker"})

	reconnectsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "stt_reconnects_total",
		Help: "STT stream reconnect attempts after a dropped stream, by speaker.",
	}, []string{"speaker"})

	providerErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "stt_provider_errors_total",
		Help: "STT OpenStream failures, by speaker.",
	}, []string{"speaker"})

	partialsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "stt_partials_total",
		Help: "Interim (partial) transcript events emitted, by speaker.",
	}, []string{"speaker"})

	finalsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "stt_finals_total",
		Help: "Final transcript events emitted, by speaker.",
	}, []string{"speaker"})
)

// Session is the per-call transcription state: up to one live Stream per speaker, each
// supervised by its own goroutine that opens it lazily and reconnects it on failure.
// One Session per realtime WebSocket connection.
type Session struct {
	mgr       *Manager
	ctx       context.Context
	cancel    context.CancelFunc
	tenantID  string
	sessionID string
	onEvent   EventFunc

	channels [numChannels]*channelStream
	wg       sync.WaitGroup
}

// channelStream tracks one speaker's stream. `once` ensures the supervisor starts on the
// first frame (lazy open); `cur` is the live stream (nil while (re)connecting), guarded
// by `mu` because Write (gateway goroutine) and the supervisor race on it.
type channelStream struct {
	once    sync.Once
	speaker Speaker
	mu      sync.Mutex
	cur     Stream
}

// StartSession creates a Session bound to ctx. When the connection ends the caller must
// call Session.Close (or cancel ctx) to tear down the streams.
func (m *Manager) StartSession(ctx context.Context, tenantID, sessionID string, onEvent EventFunc) *Session {
	sctx, cancel := context.WithCancel(ctx)
	s := &Session{
		mgr:       m,
		ctx:       sctx,
		cancel:    cancel,
		tenantID:  tenantID,
		sessionID: sessionID,
		onEvent:   onEvent,
	}
	s.channels[chanRep] = &channelStream{speaker: SpeakerRep}
	s.channels[chanProspect] = &channelStream{speaker: SpeakerProspect}
	return s
}

// Write routes one PCM frame to its speaker's stream, lazily starting that speaker's
// supervisor on the first frame. Frames that arrive while the stream is (re)connecting
// are dropped — consistent with the realtime drop-oldest stance (decision D5). Unknown
// channels are ignored (the gateway already rejects them at parse time).
func (s *Session) Write(channel byte, pcm []byte) {
	if int(channel) >= numChannels {
		return
	}
	cs := s.channels[channel]
	cs.once.Do(func() {
		s.wg.Add(1)
		go s.supervise(cs)
	})

	cs.mu.Lock()
	cur := cs.cur
	cs.mu.Unlock()
	if cur != nil {
		_ = cur.Send(pcm)
	}
}

// supervise owns one speaker's stream for the life of the Session: open it, forward its
// events, and on failure reconnect with capped exponential backoff until ctx is done.
func (s *Session) supervise(cs *channelStream) {
	defer s.wg.Done()
	label := string(cs.speaker)
	cfg := StreamConfig{
		TenantID:   s.tenantID,
		SessionID:  s.sessionID,
		Speaker:    cs.speaker,
		SampleRate: s.mgr.sampleRate,
		Encoding:   s.mgr.encoding,
		Channels:   1,
		// Keyterms: populated from the knowledge base in Stage 4.
	}

	attempt := 0
	for {
		stream, err := s.mgr.provider.OpenStream(s.ctx, cfg)
		if err != nil {
			providerErrorsTotal.WithLabelValues(label).Inc()
			s.mgr.log.Warn("stt open failed", "speaker", label, "session", s.sessionID, "err", err)
			if !sleepBackoff(s.ctx, attempt) {
				return // ctx cancelled — session closing
			}
			attempt++
			continue
		}

		cs.mu.Lock()
		cs.cur = stream
		cs.mu.Unlock()
		streamsActive.WithLabelValues(label).Inc()
		attempt = 0 // a successful open resets the backoff

		// Forward events until the stream ends (provider error) or the session is
		// torn down. We must select on ctx here rather than only ranging over
		// Events(): teardown cannot depend on the provider closing its channel.
		s.pump(stream, label)

		cs.mu.Lock()
		cs.cur = nil
		cs.mu.Unlock()
		streamsActive.WithLabelValues(label).Dec()
		_ = stream.Close() // idempotent; also unblocks a stream waiting on us to read

		if s.ctx.Err() != nil {
			return // deliberate teardown, do not reconnect
		}
		// Stream died mid-call — reconnect.
		reconnectsTotal.WithLabelValues(label).Inc()
		s.mgr.log.Info("stt stream dropped, reconnecting", "speaker", label, "session", s.sessionID)
		if !sleepBackoff(s.ctx, attempt) {
			return
		}
		attempt++
	}
}

// pump forwards a stream's events to the consumer, recording per-speaker metrics. It
// returns when the stream's Events channel closes OR the session ctx is cancelled —
// the latter ensures Close never blocks on a stream that hasn't closed its channel.
func (s *Session) pump(stream Stream, label string) {
	events := stream.Events()
	for {
		select {
		case <-s.ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return // stream ended
			}
			if ev.IsFinal {
				finalsTotal.WithLabelValues(label).Inc()
			} else {
				partialsTotal.WithLabelValues(label).Inc()
			}
			s.onEvent(ev)
		}
	}
}

// Close tears down every stream and waits for the supervisors to exit. Idempotent.
func (s *Session) Close() {
	s.cancel()
	s.wg.Wait()
}

// sleepBackoff waits backoffBase·2^attempt (capped at backoffMax), returning false if ctx
// is cancelled during the wait so the caller stops retrying.
func sleepBackoff(ctx context.Context, attempt int) bool {
	if attempt > 5 {
		attempt = 5 // cap the shift; backoffMax bounds the result anyway
	}
	d := backoffBase << attempt
	if d > backoffMax {
		d = backoffMax
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
