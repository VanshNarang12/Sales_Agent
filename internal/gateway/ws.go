package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
	"github.com/VanshNarang12/sales-agent/internal/stt"
	"github.com/VanshNarang12/sales-agent/internal/suggest"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(_ *http.Request) bool { return true },
}
var (
	framesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_frames_total",
		Help: "Audio frames successfully parsed, labeled by channel (rep/prospect).",
	}, []string{"channel"})

	frameGapTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_frame_gap_total",
		Help: "Sequence-number gaps detected (dropped/reordered frames), by channel.",
	}, []string{"channel"})

	protocolErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_ws_protocol_errors_total",
		Help: "Realtime connections closed on a protocol/format error, by WS close code.",
	}, []string{"code"})

	suggestE2EDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "suggest_e2e_duration_ms",
		Help:    "Suggest click received to suggestion WS write, in ms (feature 6.8, target 2000-4000).",
		Buckets: []float64{250, 500, 1000, 1500, 2000, 3000, 4000, 6000, 10000},
	})

	suggestOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "suggest_outcomes_total",
		Help: "Suggest clicks by outcome: card, refused, gated, no_hits, no_ask, generation_off, extract_error, search_error, generate_error.",
	}, []string{"outcome"})

	postcallDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "postcall_summary_duration_ms",
		Help:    "end_call received to summary ready, in ms (Stage 10; not latency-critical).",
		Buckets: []float64{1000, 2000, 4000, 6000, 10000, 15000, 30000, 60000},
	})

	postcallOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "postcall_outcomes_total",
		Help: "Call-end summaries by outcome: summary, internal_skip, empty_transcript, disabled, transcript_error, llm_error, save_error.",
	}, []string{"outcome"})
)

type helloMsg struct {
	Type         string `json:"type"`
	Role         string `json:"role"`
	SampleRate   int    `json:"sampleRate"`
	Encoding     string `json:"encoding"`
	Channels     int    `json:"channels"`
	FrameSamples int    `json:"frameSamples"`
	// Stage 10: internal calls leave no trace; customer calls get a post-call
	// summary, persisted when a customer name was tagged.
	MeetingType  string `json:"meetingType"`  // "internal" (default) | "customer"
	CustomerName string `json:"customerName"` // optional; empty = summarize but don't persist
}

// suggestionMsg answers a Suggest click. Empty hits means: searched, nothing above
// the confidence threshold — the client shows "no answer in your docs", never a
// made-up card (5.5). Empty ask means the window held no question/objection.
// Card is absent when generation is off, gated, refused, or failed (Stage 6).
type suggestionMsg struct {
	Type      string                   `json:"type"` // always "suggestion"
	Ask       string                   `json:"ask"`
	Hits      []retrieval.SearchResult `json:"hits"`
	Card      *suggest.Card            `json:"card,omitempty"`
	ElapsedMs int64                    `json:"elapsed_ms"` // click received → this write (6.8)
}

type transcriptMsg struct {
	Type        string  `json:"type"` // always "transcript"
	Speaker     string  `json:"speaker"`
	IsFinal     bool    `json:"isFinal"`
	SpeechFinal bool    `json:"speechFinal"`
	Text        string  `json:"text"`
	StartMs     int64   `json:"startMs"`
	EndMs       int64   `json:"endMs"`
	Confidence  float64 `json:"confidence"`
}

type session struct {
	gotHello bool
	lastSeq  [2]uint16
	seqSeen  [2]bool
	stt      *stt.Session
	detect   *detect.Session
	writeMu  sync.Mutex

	// Stage 10 post-call state. postcallStarted is only touched on the WS read
	// goroutine (the session-end defer), so it needs no lock.
	id              string
	tenantID        string
	meetingType     string
	customerName    string
	postcallStarted bool
}

func (s *Server) handleRealtime(w http.ResponseWriter, r *http.Request) {
	tid, err := tenancy.MustFrom(r.Context())
	if err != nil {
		http.Error(w, "no tenant", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Error("ws upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	s.log.Info("realtime session opened", "tenant", string(tid))
	sess := &session{id: newSessionID(), tenantID: string(tid), meetingType: "internal"}

	// Stage 10: every session end — deliberate stop or crash/drop — lands here;
	// customer calls get summarized + persisted, internal calls leave no trace.
	defer func() {
		if !sess.postcallStarted {
			sess.postcallStarted = true
			s.runPostcall(sess)
		}
	}()

	if s.stt != nil {
		sink := s.transcriptSink(conn, sess)
		if s.detect != nil {
			sess.detect = s.detect.StartSession(r.Context(), string(tid), sess.id, s.querySink(conn, sess))
			sink = composeSinks(sink, sess.detect.OnTranscript)
		}
		sess.stt = s.stt.StartSession(r.Context(), string(tid), sess.id, sink)
		defer sess.stt.Close() // runs before conn.Close() (LIFO): supervisors stop writing first
		s.log.Info("transcription session started", "tenant", string(tid), "session", sess.id)
	}

	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			s.log.Info("realtime session closed", "tenant", string(tid), "err", err)
			return
		}
		switch mt {
		case websocket.TextMessage:
			if !s.handleText(r.Context(), conn, sess, msg) {
				return
			}
		case websocket.BinaryMessage:
			if !s.handleAudio(conn, sess, msg) {
				return
			}
		}
	}
}

type controlMsg struct {
	Type string `json:"type"`
}

type suggestMsg struct {
	Type       string `json:"type"` // "suggest"
	LookbackMs int64  `json:"lookbackMs"`
}

// handleText dispatches an inbound WS text message: the Suggest click (Stage 3) or the hello handshake.
func (s *Server) handleText(ctx context.Context, conn *websocket.Conn, sess *session, msg []byte) bool {
	var c controlMsg
	if err := json.Unmarshal(msg, &c); err != nil {
		s.closeWS(conn, sess, wsProtocolError, "malformed control message")
		return false
	}
	if c.Type == "suggest" {
		if sess.detect != nil {
			var m suggestMsg
			_ = json.Unmarshal(msg, &m)
			go sess.detect.Suggest(ctx, m.LookbackMs) // runs off the read loop: it calls the query-builder LLM
		}
		return true
	}
	return s.handleHello(conn, sess, msg)
}

func (s *Server) handleHello(conn *websocket.Conn, sess *session, msg []byte) bool {
	var h helloMsg
	if err := json.Unmarshal(msg, &h); err != nil || h.Type != "hello" {
		s.closeWS(conn, sess, wsProtocolError, "expected hello")
		return false
	}
	if h.Encoding != "pcm_s16le" || h.SampleRate != 16000 || h.Channels != 1 {
		s.closeWS(conn, sess, wsUnsupportedFormat, "unsupported audio format")
		return false
	}
	if h.MeetingType == "customer" {
		sess.meetingType = "customer"
		sess.customerName = strings.TrimSpace(h.CustomerName)
	} // anything else stays "internal" — the safe default: nothing persisted (D4)
	s.log.Info("packet: hello", "role", h.Role, "sampleRate", h.SampleRate, "encoding", h.Encoding, "channels", h.Channels, "meeting", sess.meetingType)
	sess.gotHello = true
	sess.writeMu.Lock()
	err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ready"}`))
	sess.writeMu.Unlock()
	if err != nil {
		return false
	}
	return true
}

func (s *Server) handleAudio(conn *websocket.Conn, sess *session, msg []byte) bool {
	if !sess.gotHello {
		s.closeWS(conn, sess, wsProtocolError, "audio before hello")
		return false
	}
	channel, seq, pcm, err := parseAudioFrame(msg)
	if err != nil {
		s.closeWS(conn, sess, wsProtocolError, "malformed frame")
		return false
	}

	label := channelLabel(channel)
	framesTotal.WithLabelValues(label).Inc()
	if sess.seqSeen[channel] && seq != sess.lastSeq[channel]+1 {
		frameGapTotal.WithLabelValues(label).Inc()
	}
	sess.lastSeq[channel] = seq
	sess.seqSeen[channel] = true
	if sess.stt != nil {
		sess.stt.Write(channel, pcm)
	}
	return true
}

func composeSinks(sinks ...stt.EventFunc) stt.EventFunc {
	return func(ev stt.TranscriptEvent) {
		for _, s := range sinks {
			s(ev)
		}
	}
}

// querySink runs on the Suggest goroutine (off the WS read loop); the in-flight
// guard stays held through extraction + search + generation, so clicks can't
// stack LLM calls.
// Flow: window → extract ask → embed+search KB → card → "suggestion" WS message.
func (s *Server) querySink(conn *websocket.Conn, sess *session) detect.EmitFunc {
	return func(q detect.BuiltQuery) {
		start := time.Now()
		if s.extract == nil {
			fmt.Printf("[suggest] session=%s query=%q\n", q.SessionID, q.Query)
			return
		}
		// 20 s ceiling: covers a cold Neon wake-up in dev. The 2-4 s product target
		// (6.8) is measured by suggest_e2e_duration_ms, not enforced here.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ask, err := s.extract.Extract(ctx, q)
		if err != nil {
			s.log.Warn("extraction failed; no suggestion", "err", err, "session", q.SessionID)
			suggestOutcomes.WithLabelValues("extract_error").Inc()
			return
		}
		if ask == "" {
			s.log.Info("suggest: no ask in window", "session", q.SessionID)
			suggestOutcomes.WithLabelValues("no_ask").Inc()
			s.sendSuggestion(conn, sess, q.SessionID, "", nil, nil, start)
			return
		}
		if s.search == nil {
			fmt.Printf("[suggest] session=%s ask=%q\n", q.SessionID, ask)
			return
		}
		// Search needs the tenant in ctx: RLS scopes the SQL to this org's chunks.
		hits, err := s.search.Search(tenancy.With(ctx, tenancy.TenantID(q.TenantID)), ask)
		if err != nil {
			s.log.Warn("search failed; no suggestion", "err", err, "session", q.SessionID)
			suggestOutcomes.WithLabelValues("search_error").Inc()
			return
		}
		s.log.Info("suggest: searched", "session", q.SessionID, "hits", len(hits))
		card, outcome := s.generateCard(ctx, q.SessionID, ask, hits)
		suggestOutcomes.WithLabelValues(outcome).Inc()
		s.sendSuggestion(conn, sess, q.SessionID, ask, hits, card, start)
	}
}

// generateCard runs the Stage-6 answer call. A nil card means no card — the
// suggestion message still goes out with the raw hits. The outcome string feeds
// suggest_outcomes_total. Logs never carry card text.
func (s *Server) generateCard(ctx context.Context, sessionID, ask string, hits []retrieval.SearchResult) (*suggest.Card, string) {
	switch {
	case len(hits) == 0:
		return nil, "no_hits"
	case s.suggest == nil:
		return nil, "generation_off"
	case hits[0].Score < s.cfg.SuggestMinConfidence:
		s.log.Info("suggest: card gated by confidence", "session", sessionID, "top_score", hits[0].Score)
		return nil, "gated"
	}
	start := time.Now()
	card, err := s.suggest.Generate(ctx, ask, hits)
	if err != nil {
		s.log.Warn("card generation failed; sending hits only", "err", err, "session", sessionID)
		return nil, "generate_error"
	}
	s.log.Info("suggest: card generated", "session", sessionID, "refused", card == nil, "ms", time.Since(start).Milliseconds())
	if card == nil {
		return nil, "refused"
	}
	return card, "card"
}

func (s *Server) sendSuggestion(conn *websocket.Conn, sess *session, sessionID, ask string, hits []retrieval.SearchResult, card *suggest.Card, start time.Time) {
	if hits == nil {
		hits = []retrieval.SearchResult{} // marshal as [], not null
	}
	elapsed := time.Since(start).Milliseconds()
	suggestE2EDuration.Observe(float64(elapsed))
	b, err := json.Marshal(suggestionMsg{Type: "suggestion", Ask: ask, Hits: hits, Card: card, ElapsedMs: elapsed})
	if err != nil {
		return
	}
	sess.writeMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, b)
	sess.writeMu.Unlock()
	if err != nil {
		s.log.Warn("suggestion write failed", "err", err, "session", sessionID)
	}
}

// runPostcall is the Stage-10 call-end path, triggered by the socket closing:
// full transcript → summary LLM → persist (when a customer is tagged).
// Internal meetings do nothing at all (D5). Summary text never hits the logs.
// Delivery to humans (e.g. a post-call email) is a future feature — nothing is
// sent to the client.
func (s *Server) runPostcall(sess *session) {
	if sess.meetingType != "customer" {
		postcallOutcomes.WithLabelValues("internal_skip").Inc()
		return
	}
	if s.summarizer == nil || s.tstore == nil {
		postcallOutcomes.WithLabelValues("disabled").Inc()
		return
	}
	start := time.Now()
	// Own context: the WS request context dies with the connection, and the
	// fallback path runs exactly then. 60 s covers a long transcript.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	entries, err := s.tstore.Full(ctx, sess.tenantID, sess.id)
	if err != nil {
		s.log.Warn("postcall: transcript read failed", "err", err, "session", sess.id)
		postcallOutcomes.WithLabelValues("transcript_error").Inc()
		return
	}
	if len(entries) == 0 {
		postcallOutcomes.WithLabelValues("empty_transcript").Inc()
		return
	}
	sum, err := s.summarizer.Summarize(ctx, entries)
	if err != nil || sum == nil {
		if err != nil {
			s.log.Warn("postcall: summary failed", "err", err, "session", sess.id)
		}
		postcallOutcomes.WithLabelValues("llm_error").Inc()
		return
	}
	elapsed := time.Since(start).Milliseconds()
	postcallDuration.Observe(float64(elapsed))

	outcome, saved := "summary", false
	if s.pcStore != nil && sess.customerName != "" {
		// Embed the digest for the prep chat's semantic search. Same model+space
		// as KB chunks. Failure = save without embedding, never lose the summary.
		var vec []float32
		if s.embedder != nil {
			digest := sum.Summary + "\n" + strings.Join(sum.ActionItems, "\n") + "\n" + strings.Join(sum.Unanswered, "\n")
			if vecs, err := s.embedder.Embed(ctx, embed.PrefixDocument, []string{digest}); err != nil {
				s.log.Warn("postcall: embed failed; saving without embedding", "err", err, "session", sess.id)
			} else if len(vecs) == 1 {
				vec = vecs[0]
			}
		}
		sctx := tenancy.With(ctx, tenancy.TenantID(sess.tenantID))
		if err := s.pcStore.SaveSummary(sctx, sess.customerName, sess.id, sum, vec); err != nil {
			s.log.Warn("postcall: save failed", "err", err, "session", sess.id)
			outcome = "save_error" // rep still sees the summary; it just isn't stored
		} else {
			saved = true
		}
	}
	postcallOutcomes.WithLabelValues(outcome).Inc()
	s.log.Info("postcall: summary ready", "session", sess.id, "saved", saved, "ms", elapsed)
}

func (s *Server) transcriptSink(conn *websocket.Conn, sess *session) stt.EventFunc {
	return func(ev stt.TranscriptEvent) {
		if ev.IsFinal && ev.Text != "" {
			fmt.Printf("[transcript] %-9s %s\n", string(ev.Speaker)+":", ev.Text)
		}

		b, err := json.Marshal(transcriptMsg{
			Type:        "transcript",
			Speaker:     string(ev.Speaker),
			IsFinal:     ev.IsFinal,
			SpeechFinal: ev.SpeechFinal,
			Text:        ev.Text,
			StartMs:     ev.StartMs,
			EndMs:       ev.EndMs,
			Confidence:  ev.Confidence,
		})
		if err != nil {
			return
		}
		sess.writeMu.Lock()
		err = conn.WriteMessage(websocket.TextMessage, b)
		sess.writeMu.Unlock()
		if err != nil {
			s.log.Warn("transcript write failed", "err", err)
		}
	}
}
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
func (s *Server) closeWS(conn *websocket.Conn, sess *session, code int, reason string) {
	s.log.Warn("closing realtime session", "code", code, "reason", reason)
	protocolErrors.WithLabelValues(strconv.Itoa(code)).Inc()
	sess.writeMu.Lock()
	defer sess.writeMu.Unlock()
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(2*time.Second),
	)
}
