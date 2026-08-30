package detect

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/VanshNarang12/sales-agent/internal/stt"
	"github.com/VanshNarang12/sales-agent/internal/transcript"
)

// TranscriptStore is the live-transcript backend (Redis in prod, a fake in tests).
type TranscriptStore interface {
	Append(ctx context.Context, tenantID, sessionID string, e transcript.Entry) error
	Window(ctx context.Context, tenantID, sessionID string, lookbackMs int64) ([]transcript.Entry, error)
}

type Engine struct {
	log        *slog.Logger
	store      TranscriptStore
	lookbackMs int64
}

func NewEngine(log *slog.Logger, store TranscriptStore, defaultLookbackMs int64) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if defaultLookbackMs <= 0 {
		defaultLookbackMs = 90_000
	}
	return &Engine{log: log, store: store, lookbackMs: defaultLookbackMs}
}

func (e *Engine) StartSession(ctx context.Context, tenantID, sessionID string, emit EmitFunc) *Session {
	return &Session{
		eng:       e,
		ctx:       ctx,
		tenantID:  tenantID,
		sessionID: sessionID,
		emit:      emit,
	}
}

type Session struct {
	eng       *Engine
	ctx       context.Context
	tenantID  string
	sessionID string
	emit      EmitFunc

	mu       sync.Mutex
	inFlight bool
}

// OnTranscript write-throughs each final utterance; a failed append is dropped, not fatal.
func (s *Session) OnTranscript(ev stt.TranscriptEvent) {
	if !ev.IsFinal || ev.Text == "" {
		return
	}
	e := transcript.Entry{Speaker: string(ev.Speaker), Text: ev.Text, StartMs: ev.StartMs, EndMs: ev.EndMs}
	if err := s.eng.store.Append(s.ctx, s.tenantID, s.sessionID, e); err != nil {
		s.eng.log.Warn("transcript append failed", "err", err, "session", s.sessionID)
	}
}

func (s *Session) Suggest(ctx context.Context, lookbackMs int64) {
	if lookbackMs <= 0 {
		lookbackMs = s.eng.lookbackMs
	}

	s.mu.Lock()
	if s.inFlight {
		s.mu.Unlock()
		s.eng.log.Info("suggest dropped", "reason", "in_flight", "session", s.sessionID)
		return
	}
	s.inFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.inFlight = false
		s.mu.Unlock()
	}()

	entries, err := s.eng.store.Window(ctx, s.tenantID, s.sessionID, lookbackMs)
	if err != nil {
		s.eng.log.Warn("transcript window failed", "err", err, "session", s.sessionID)
		return
	}
	win, startMs, endMs := joinWindow(entries)
	if win == "" {
		return
	}

	s.emit(BuiltQuery{
		TenantID:  s.tenantID,
		SessionID: s.sessionID,
		Query:     win,
		StartMs:   startMs,
		EndMs:     endMs,
	})
}

// joinWindow renders store entries as "speaker: text" lines and returns the span.
func joinWindow(entries []transcript.Entry) (text string, startMs, endMs int64) {
	if len(entries) == 0 {
		return "", 0, 0
	}
	var b strings.Builder
	startMs = entries[0].StartMs
	for _, e := range entries {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(e.Speaker)
		b.WriteString(": ")
		b.WriteString(e.Text)
		if e.StartMs < startMs {
			startMs = e.StartMs
		}
		if e.EndMs > endMs {
			endMs = e.EndMs
		}
	}
	return strings.TrimSpace(b.String()), startMs, endMs
}
