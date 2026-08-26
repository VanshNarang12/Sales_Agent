package detect

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/VanshNarang12/sales-agent/internal/stt"
)
const defaultLookbackMs = 90_000
const maxBufEntries = 400
type QueryBuilder interface {
	Build(ctx context.Context, window string) (string, error)
}
type Engine struct {
	log     *slog.Logger
	builder QueryBuilder
}

func NewEngine(log *slog.Logger, builder QueryBuilder) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{log: log, builder: builder}
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

type entry struct {
	speaker Speaker
	text    string
	startMs int64
	endMs   int64
}

type Session struct {
	eng       *Engine
	ctx       context.Context
	tenantID  string
	sessionID string
	emit      EmitFunc

	mu       sync.Mutex
	buf      []entry
	inFlight bool
}

func (s *Session) OnTranscript(ev stt.TranscriptEvent) {
	if !ev.IsFinal || ev.Text == "" {
		return
	}
	s.mu.Lock()
	s.buf = append(s.buf, entry{speaker: ev.Speaker, text: ev.Text, startMs: ev.StartMs, endMs: ev.EndMs})
	if len(s.buf) > maxBufEntries {
		s.buf = s.buf[len(s.buf)-maxBufEntries:]
	}
	s.mu.Unlock()
}

func (s *Session) Suggest(ctx context.Context, lookbackMs int64) {
	if lookbackMs <= 0 {
		lookbackMs = defaultLookbackMs
	}

	s.mu.Lock()
	if s.inFlight {
		s.mu.Unlock()
		s.eng.log.Info("suggest dropped", "reason", "in_flight", "session", s.sessionID)
		return
	}
	win, startMs, endMs := snapshot(s.buf, lookbackMs)
	s.inFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.inFlight = false
		s.mu.Unlock()
	}()

	if win == "" {
		return
	}

	query := win
	if s.eng.builder != nil {
		q, err := s.eng.builder.Build(ctx, win)
		if err != nil {
			s.eng.log.Warn("query builder failed", "err", err, "session", s.sessionID)
			return
		}
		query = q
	}

	s.emit(BuiltQuery{
		TenantID:   s.tenantID,
		SessionID:  s.sessionID,
		Query:      query,
		WindowText: win,
		StartMs:    startMs,
		EndMs:      endMs,
		Source:     SourceModel,
	})
}
func snapshot(buf []entry, lookbackMs int64) (text string, startMs, endMs int64) {
	if len(buf) == 0 {
		return "", 0, 0
	}
	latest := buf[len(buf)-1].endMs
	cutoff := latest - lookbackMs

	var b strings.Builder
	startMs = latest
	for _, e := range buf {
		if e.endMs < cutoff {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(e.speaker))
		b.WriteString(": ")
		b.WriteString(e.text)
		if e.startMs < startMs {
			startMs = e.startMs
		}
	}
	return strings.TrimSpace(b.String()), startMs, latest
}
