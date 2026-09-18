package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/VanshNarang12/sales-agent/internal/customer"
)

var (
	prepchatDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "prepchat_turn_duration_ms",
		Help:    "Chat message received to answer written, in ms (Stage 10.8).",
		Buckets: []float64{500, 1000, 2000, 4000, 6000, 10000, 20000},
	})
	prepchatOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "prepchat_outcomes_total",
		Help: "Chat turns by outcome: answer, bad_request, no_customer, chat_error.",
	}, []string{"outcome"})
)

// GET /v1/customers
func (s *Server) handleCustomerList(w http.ResponseWriter, r *http.Request) {
	if s.custStore == nil {
		http.Error(w, "customer memory unavailable (DB not configured)", http.StatusServiceUnavailable)
		return
	}
	list, err := s.custStore.ListCustomers(r.Context())
	if err != nil {
		s.log.Warn("customer list failed", "err", err)
		http.Error(w, "listing customers failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /v1/customers — {"name": "..."} → find-or-create.
func (s *Server) handleCustomerCreate(w http.ResponseWriter, r *http.Request) {
	if s.custStore == nil {
		http.Error(w, "customer memory unavailable (DB not configured)", http.StatusServiceUnavailable)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "malformed JSON body", http.StatusBadRequest)
		return
	}
	c, err := s.custStore.CreateCustomer(r.Context(), in.Name)
	if err != nil {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// GET /v1/customers/{id}/summaries — timeline metadata, newest first.
func (s *Server) handleSummaryList(w http.ResponseWriter, r *http.Request) {
	if s.custStore == nil {
		http.Error(w, "customer memory unavailable (DB not configured)", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if uuid.Validate(id) != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	metas, err := s.custStore.ListSummaries(r.Context(), id)
	if errors.Is(err, customer.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Warn("summary list failed", "err", err)
		http.Error(w, "listing summaries failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, metas)
}

// GET /v1/summaries/{id} — one full meeting digest.
func (s *Server) handleSummaryGet(w http.ResponseWriter, r *http.Request) {
	if s.custStore == nil {
		http.Error(w, "customer memory unavailable (DB not configured)", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if uuid.Validate(id) != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	d, err := s.custStore.GetSummary(r.Context(), id)
	if errors.Is(err, customer.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Warn("summary get failed", "err", err)
		http.Error(w, "reading summary failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// POST /v1/customers/{id}/chat — {"message": "...", "history": [{role,text}]}.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.chat == nil || s.custStore == nil {
		http.Error(w, "prep chat unavailable (LLM/DB not configured)", http.StatusServiceUnavailable)
		return
	}
	start := time.Now()
	id := r.PathValue("id")
	if uuid.Validate(id) != nil {
		prepchatOutcomes.WithLabelValues("no_customer").Inc()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var in struct {
		Message string             `json:"message"`
		History []customer.Message `json:"history"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Message == "" {
		prepchatOutcomes.WithLabelValues("bad_request").Inc()
		http.Error(w, `"message" is required`, http.StatusBadRequest)
		return
	}
	if _, err := s.custStore.ListSummaries(r.Context(), id); errors.Is(err, customer.ErrNotFound) {
		prepchatOutcomes.WithLabelValues("no_customer").Inc()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	reply, err := s.chat.Answer(r.Context(), id, in.Message, in.History)
	if err != nil {
		s.log.Warn("prep chat failed", "err", err, "customer", id)
		prepchatOutcomes.WithLabelValues("chat_error").Inc()
		http.Error(w, "chat failed", http.StatusBadGateway)
		return
	}
	elapsed := time.Since(start).Milliseconds()
	prepchatDuration.Observe(float64(elapsed))
	prepchatOutcomes.WithLabelValues("answer").Inc()
	writeJSON(w, http.StatusOK, map[string]any{
		"answer": reply.Answer, "sources": reply.Sources, "elapsed_ms": elapsed,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
