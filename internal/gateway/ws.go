package gateway

import (
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

	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
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
		sess.stt = s.stt.StartSession(r.Context(), string(tid), sessionID, s.transcriptSink(conn, sess))
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
			if !s.handleHello(conn, sess, msg) {
				return
			}
		case websocket.BinaryMessage:
			if !s.handleAudio(conn, sess, msg) {
				return
			}
		}
	}
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
