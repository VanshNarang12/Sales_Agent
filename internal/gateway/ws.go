package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/jobs"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
	"github.com/VanshNarang12/sales-agent/internal/postcall"
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
	// Multi-card (2026-10-06): one click can answer up to 3 pending asks; each
	// arrives as its own message as it finishes. Index orders them, count tells
	// the client how many to expect. Single-ask clicks send 0/1 as before.
	AskIndex int `json:"ask_index"`
	AskCount int `json:"ask_count"`
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

	// Per-call stats for the meetings APIs (migration 0007). statsMu guards them:
	// they're written on the Suggest goroutine and read on the session-end defer.
	statsMu     sync.Mutex
	startedAt   time.Time
	cardsServed int
	citedDocs   map[string]struct{}
}

// recordCard counts a served answer card and the documents it cited.
func (sess *session) recordCard(card *suggest.Card) {
	if card == nil {
		return
	}
	sess.statsMu.Lock()
	defer sess.statsMu.Unlock()
	sess.cardsServed++
	if sess.citedDocs == nil {
		sess.citedDocs = map[string]struct{}{}
	}
	for _, c := range card.Citations {
		if c.Title != "" {
			sess.citedDocs[c.Title] = struct{}{}
		}
	}
}

// stats snapshots the call's stats at session end.
func (sess *session) stats() postcall.Stats {
	sess.statsMu.Lock()
	defer sess.statsMu.Unlock()
	docs := make([]string, 0, len(sess.citedDocs))
	for d := range sess.citedDocs {
		docs = append(docs, d)
	}
	sort.Strings(docs)
	return postcall.Stats{
		StartedAt:        sess.startedAt,
		DurationSeconds:  int(time.Since(sess.startedAt) / time.Second),
		SuggestionsCount: sess.cardsServed,
		Sources:          docs,
	}
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
	sess := &session{id: newSessionID(), tenantID: string(tid), meetingType: "internal", startedAt: time.Now()}

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
		asks, err := s.extract.Extract(ctx, q)
		extractMs := time.Since(start).Milliseconds()
		if err != nil {
			s.log.Warn("extraction failed; no suggestion", "err", err, "session", q.SessionID)
			suggestOutcomes.WithLabelValues("extract_error").Inc()
			return
		}
		if len(asks) == 0 {
			s.log.Info("suggest: no ask in window", "session", q.SessionID)
			suggestOutcomes.WithLabelValues("no_ask").Inc()
			s.sendSuggestion(conn, sess, q.SessionID, "", nil, nil, start, 0, 1)
			return
		}
		if s.search == nil {
			fmt.Printf("[suggest] session=%s asks=%d\n", q.SessionID, len(asks))
			return
		}
		// Search needs the tenant in ctx: RLS scopes the SQL to this org's chunks.
		// All asks share one embedding call and one DB round trip (SearchMulti).
		searchStart := time.Now()
		hitsPerAsk, err := s.search.SearchMulti(tenancy.With(ctx, tenancy.TenantID(q.TenantID)), asks)
		if err != nil {
			s.log.Warn("search failed; no suggestion", "err", err, "session", q.SessionID)
			suggestOutcomes.WithLabelValues("search_error").Inc()
			return
		}
		totalHits := 0
		for _, h := range hitsPerAsk {
			totalHits += len(h)
		}
		s.log.Info("suggest: searched", "session", q.SessionID, "asks", len(asks), "hits", totalHits,
			"extract_ms", extractMs, "search_ms", time.Since(searchStart).Milliseconds())
		// One generate call per ask, in parallel; each card ships the moment it is
		// ready (writeMu serializes the socket). Waiting here keeps the detect
		// engine's in-flight guard held until the last card is out.
		var wg sync.WaitGroup
		for i := range asks {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				card, outcome := s.generateCard(ctx, q.TenantID, q.SessionID, asks[i], hitsPerAsk[i])
				suggestOutcomes.WithLabelValues(outcome).Inc()
				s.sendSuggestion(conn, sess, q.SessionID, asks[i], hitsPerAsk[i], card, start, i, len(asks))
			}(i)
		}
		wg.Wait()
	}
}

// generateCard runs the Stage-6 answer call. A nil card means no card — the
// suggestion message still goes out with the raw hits. The outcome string feeds
// suggest_outcomes_total. Logs never carry card text.
func (s *Server) generateCard(ctx context.Context, tenantID, sessionID, ask string, hits []retrieval.SearchResult) (*suggest.Card, string) {
	switch {
	case len(hits) == 0:
		return nil, "no_hits"
	case s.suggest == nil:
		return nil, "generation_off"
	case hits[0].Score < s.cfg.SuggestMinConfidence:
		s.log.Info("suggest: card gated by confidence", "session", sessionID, "top_score", hits[0].Score)
		return nil, "gated"
	}
	// Stage 11: the org playbook rides the system prompt. A read failure means a
	// card without the playbook, never no card.
	block := ""
	if s.pbStore != nil {
		if pb, err := s.pbStore.Get(tenancy.With(ctx, tenancy.TenantID(tenantID))); err != nil {
			s.log.Warn("suggest: playbook read failed; generating without it", "err", err, "session", sessionID)
		} else {
			block = pb.PromptBlock()
		}
	}
	start := time.Now()
	card, err := s.suggest.Generate(ctx, ask, hits, block)
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

func (s *Server) sendSuggestion(conn *websocket.Conn, sess *session, sessionID, ask string, hits []retrieval.SearchResult, card *suggest.Card, start time.Time, askIndex, askCount int) {
	if hits == nil {
		hits = []retrieval.SearchResult{} // marshal as [], not null
	}
	sess.recordCard(card)
	elapsed := time.Since(start).Milliseconds()
	suggestE2EDuration.Observe(float64(elapsed))
	b, err := json.Marshal(suggestionMsg{Type: "suggestion", Ask: ask, Hits: hits, Card: card, ElapsedMs: elapsed,
		AskIndex: askIndex, AskCount: askCount})
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
// postcallPayload is the durable job body (jobs table, migration 0010): the
// pointer to the transcript (session id) + call metadata — never the transcript
// itself, which stays in Redis so the expiry promise holds.
type postcallPayload struct {
	TenantID     string         `json:"tenant_id"`
	SessionID    string         `json:"session_id"`
	CustomerName string         `json:"customer_name"`
	Stats        postcall.Stats `json:"stats"`
}

// runPostcall fires on every session end. Customer calls become a durable job —
// one INSERT — so a gateway restart can't lose the summary. If the queue isn't
// available the old inline path runs as the fallback (never worse than before).
func (s *Server) runPostcall(sess *session) {
	if sess.meetingType != "customer" {
		postcallOutcomes.WithLabelValues("internal_skip").Inc()
		return
	}
	if s.summarizer == nil || s.tstore == nil {
		postcallOutcomes.WithLabelValues("disabled").Inc()
		return
	}
	p := postcallPayload{
		TenantID:     sess.tenantID,
		SessionID:    sess.id,
		CustomerName: sess.customerName,
		Stats:        sess.stats(),
	}
	if s.jobq != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.jobq.Enqueue(ctx, sess.tenantID, "postcall_summary", p); err == nil {
			s.log.Info("postcall: job enqueued", "session", sess.id)
			return
		} else {
			s.log.Warn("postcall: enqueue failed; processing inline", "err", err, "session", sess.id)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = s.processPostcall(ctx, p)
}

// ProcessPostcallJob is the jobs-worker handler for kind "postcall_summary".
// A returned error = the worker retries (backoff), then parks the job as failed.
func (s *Server) ProcessPostcallJob(ctx context.Context, job *jobs.Job) error {
	var p postcallPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("postcall job payload: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return s.processPostcall(cctx, p)
}

// processPostcall is the pipeline: transcript → summary LLM (with quick retries)
// → embed → persist. Shared by the worker and the inline fallback.
func (s *Server) processPostcall(ctx context.Context, p postcallPayload) error {
	start := time.Now()
	entries, err := s.tstore.Full(ctx, p.TenantID, p.SessionID)
	if err != nil {
		postcallOutcomes.WithLabelValues("transcript_error").Inc()
		return fmt.Errorf("transcript read: %w", err)
	}
	if len(entries) == 0 {
		// Nothing to summarize — includes "transcript expired before the last
		// retry". Terminal and correct under the expiry promise, not an error.
		postcallOutcomes.WithLabelValues("empty_transcript").Inc()
		return nil
	}
	// Up to 3 attempts (waits 2 s then 5 s) inside the 60 s budget: the call is
	// already over, so waiting costs nothing — a transient LLM error must not
	// cost the summary (post_call_techdoc.md §13b).
	var sum *postcall.Summary
	for attempt, wait := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
			}
		}
		if ctx.Err() != nil {
			break
		}
		sum, err = s.summarizer.Summarize(ctx, entries)
		if err == nil && sum != nil {
			break
		}
		s.log.Warn("postcall: summary attempt failed", "attempt", attempt+1, "err", err, "session", p.SessionID)
	}
	if err != nil || sum == nil {
		postcallOutcomes.WithLabelValues("llm_error").Inc()
		return fmt.Errorf("summary llm: %w", err)
	}
	elapsed := time.Since(start).Milliseconds()
	postcallDuration.Observe(float64(elapsed))

	// Untagged calls save too (customer_id NULL) — taggable later from the web
	// app's Meetings page. Only a missing store skips persistence now.
	if s.pcStore == nil {
		postcallOutcomes.WithLabelValues("summary").Inc()
		return nil
	}
	// Embed the digest for the prep chat's semantic search. Same model+space
	// as KB chunks. Failure = save without embedding, never lose the summary.
	var vec []float32
	if s.embedder != nil {
		digest := sum.Summary + "\n" + strings.Join(sum.ActionItems, "\n") + "\n" + strings.Join(sum.Unanswered, "\n")
		if vecs, err := s.embedder.Embed(ctx, embed.PrefixDocument, []string{digest}); err != nil {
			s.log.Warn("postcall: embed failed; saving without embedding", "err", err, "session", p.SessionID)
		} else if len(vecs) == 1 {
			vec = vecs[0]
		}
	}
	// Untagged calls save too (customer_id NULL, migration 0009) — taggable later.
	sctx := tenancy.With(ctx, tenancy.TenantID(p.TenantID))
	if err := s.pcStore.SaveSummary(sctx, p.CustomerName, p.SessionID, sum, vec, p.Stats); err != nil {
		postcallOutcomes.WithLabelValues("save_error").Inc()
		return fmt.Errorf("summary save: %w", err)
	}
	postcallOutcomes.WithLabelValues("summary").Inc()
	s.log.Info("postcall: summary ready", "session", p.SessionID, "saved", true, "ms", elapsed)
	return nil
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
