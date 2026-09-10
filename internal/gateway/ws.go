package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
	"github.com/VanshNarang12/sales-agent/internal/stt"
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
)

type helloMsg struct {
	Type         string `json:"type"`
	Role         string `json:"role"`
	SampleRate   int    `json:"sampleRate"`
	Encoding     string `json:"encoding"`
	Channels     int    `json:"channels"`
	FrameSamples int    `json:"frameSamples"`
}

// suggestionMsg answers a Suggest click. Empty hits means: searched, nothing above
// the confidence threshold — the client shows "no answer in your docs", never a
// made-up card (5.5). Empty ask means the window held no question/objection.
type suggestionMsg struct {
	Type string                   `json:"type"` // always "suggestion"
	Ask  string                   `json:"ask"`
	Hits []retrieval.SearchResult `json:"hits"`
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
	sess := &session{}

	if s.stt != nil {
		sessionID := newSessionID()
		sink := s.transcriptSink(conn, sess)
		if s.detect != nil {
			sess.detect = s.detect.StartSession(r.Context(), string(tid), sessionID, s.querySink(conn, sess))
			sink = composeSinks(sink, sess.detect.OnTranscript)
		}
		sess.stt = s.stt.StartSession(r.Context(), string(tid), sessionID, sink)
		defer sess.stt.Close() // runs before conn.Close() (LIFO): supervisors stop writing first
		s.log.Info("transcription session started", "tenant", string(tid), "session", sessionID)
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
	s.log.Info("packet: hello", "role", h.Role, "sampleRate", h.SampleRate, "encoding", h.Encoding, "channels", h.Channels)
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
// guard stays held through extraction + search, so clicks can't stack LLM calls.
// Flow: window → extract ask → embed+search KB → "suggestion" WS message.
func (s *Server) querySink(conn *websocket.Conn, sess *session) detect.EmitFunc {
	return func(q detect.BuiltQuery) {
		if s.extract == nil {
			fmt.Printf("[suggest] session=%s query=%q\n", q.SessionID, q.Query)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ask, err := s.extract.Extract(ctx, q)
		if err != nil {
			s.log.Warn("extraction failed; no suggestion", "err", err, "session", q.SessionID)
			return
		}
		if ask == "" {
			s.log.Info("suggest: no ask in window", "session", q.SessionID)
			s.sendSuggestion(conn, sess, q.SessionID, "", nil)
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
			return
		}
		s.log.Info("suggest: searched", "session", q.SessionID, "hits", len(hits))
		s.sendSuggestion(conn, sess, q.SessionID, ask, hits)
	}
}

func (s *Server) sendSuggestion(conn *websocket.Conn, sess *session, sessionID, ask string, hits []retrieval.SearchResult) {
	if hits == nil {
		hits = []retrieval.SearchResult{} // marshal as [], not null
	}
	b, err := json.Marshal(suggestionMsg{Type: "suggestion", Ask: ask, Hits: hits})
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
