package gateway

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// TODO(stage-1): tighten origin checks for the packaged desktop client.
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// Gateway audio metrics. promauto registers them with the default Prometheus registry
// at package init, so they are exposed on /metrics automatically.
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

// helloMsg is the client's opening format-announcement. The client sends it once, as
// JSON text, before any audio. See techdocs/realtime_gateway_techdoc.md §3.
type helloMsg struct {
	Type string `json:"type"`
	Role string `json:"role"`
	SampleRate int `json:"sampleRate"`
	Encoding string `json:"encoding"`
	Channels int `json:"channels"`
	FrameSamples int `json:"frameSamples"`
}

// session is the per-connection, in-memory state. Nothing here is persisted — audio is
// ephemeral by default (ADR-008).
type session struct {
	gotHello bool
	lastSeq  [2]uint16 // last seq seen, indexed by channel (0=rep, 1=prospect)
	seqSeen  [2]bool   // whether any frame has been seen yet on each channel
}

// handleRealtime upgrades the request to a WebSocket and runs the per-connection read
// loop: validate the hello handshake, then parse audio frames. The connection is
// already authenticated and tenant-scoped by authMiddleware (in dev, AUTH_DISABLED
// injects the dev tenant). Stage 2 replaces the per-frame accounting with a gRPC
// forward to the Call Session Orchestrator.
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

// handleHello parses and validates the opening hello message. On success it records
// that the format was accepted and replies {"type":"ready"}. It returns false (ending
// the session) after sending a close frame on any protocol/format error.
func (s *Server) handleHello(conn *websocket.Conn, sess *session, msg []byte) bool {
	var h helloMsg
	if err := json.Unmarshal(msg, &h); err != nil || h.Type != "hello" {
		s.closeWS(conn, wsProtocolError, "expected hello")
		return false
	}
	if h.Encoding != "pcm_s16le" || h.SampleRate != 16000 || h.Channels != 1 {
		s.closeWS(conn, wsUnsupportedFormat, "unsupported audio format")
		return false
	}
	sess.gotHello = true
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ready"}`)); err != nil {
		return false
	}
	return true
}

// handleAudio unpacks one binary audio frame, requires that hello arrived first, and
// records frame/gap metrics. It returns false (ending the session) on malformed input.
// Stage 2: forward (tenant, sessionID, channel, seq, pcm) to the orchestrator.
func (s *Server) handleAudio(conn *websocket.Conn, sess *session, msg []byte) bool {
	if !sess.gotHello {
		s.closeWS(conn, wsProtocolError, "audio before hello")
		return false
	}
	channel, seq, _, err := parseAudioFrame(msg)
	if err != nil {
		s.closeWS(conn, wsProtocolError, "malformed frame")
		return false
	}

	label := channelLabel(channel)
	framesTotal.WithLabelValues(label).Inc()

	// A gap means frames were lost in transit — expected under network backpressure
	// (the client drops stale audio but still advances seq). Record it; never fatal.
	// uint16 addition wraps 65535->0, matching the client's sequence wrap.
	if sess.seqSeen[channel] && seq != sess.lastSeq[channel]+1 {
		frameGapTotal.WithLabelValues(label).Inc()
	}
	sess.lastSeq[channel] = seq
	sess.seqSeen[channel] = true
	return true
}

// closeWS logs the reason, records the protocol-error metric, and sends a WebSocket
// close control message with the given application close code.
func (s *Server) closeWS(conn *websocket.Conn, code int, reason string) {
	s.log.Warn("closing realtime session", "code", code, "reason", reason)
	protocolErrors.WithLabelValues(strconv.Itoa(code)).Inc()
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(2*time.Second),
	)
}
