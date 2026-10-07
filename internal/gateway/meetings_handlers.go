package gateway

// Meetings APIs: the org-wide call history the web app shows (Meetings page,
// dashboard tiles). Data = call_summaries + per-call stats (migration 0007).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"github.com/google/uuid"
	"github.com/VanshNarang12/sales-agent/internal/postcall"
)

// handleMeetingList serves GET /v1/meetings?limit=&cursor= — newest first.
// List rows stay light: action items and unanswered questions ride the detail
// endpoint only.
func (s *Server) handleMeetingList(w http.ResponseWriter, r *http.Request) {
	if s.pcStore == nil {
		http.Error(w, "meetings unavailable", http.StatusServiceUnavailable)
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			http.Error(w, "limit must be 1-200", http.StatusBadRequest)
			return
		}
		limit = n
	}
	list, next, err := s.pcStore.ListMeetings(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.log.Warn("meeting list failed", "err", err)
		http.Error(w, "bad cursor or internal error", http.StatusBadRequest)
		return
	}
	if list == nil {
		list = []postcall.Meeting{}
	}
	for i := range list {
		list[i].ActionItems, list[i].Unanswered = nil, nil
	}
	resp := map[string]any{"meetings": list}
	if next != "" {
		resp["next_cursor"] = next
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleMeetingTag serves PATCH /v1/meetings/{id}/customer — attach an untagged
// summary to a customer by name (find-or-create). Returns the updated meeting.
func (s *Server) handleMeetingTag(w http.ResponseWriter, r *http.Request) {
	if s.pcStore == nil {
		http.Error(w, "meetings unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "meeting not found", http.StatusNotFound)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Name) == "" {
		http.Error(w, `"name" is required`, http.StatusBadRequest)
		return
	}
	err := s.pcStore.TagMeeting(r.Context(), id, in.Name)
	if errors.Is(err, postcall.ErrMeetingNotFound) {
		http.Error(w, "meeting not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Warn("meeting tag failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	m, err := s.pcStore.GetMeeting(r.Context(), id)
	if err != nil {
		s.log.Warn("meeting reload after tag failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// handleMeetingGet serves GET /v1/meetings/{id} — one call in full.
func (s *Server) handleMeetingGet(w http.ResponseWriter, r *http.Request) {
	if s.pcStore == nil {
		http.Error(w, "meetings unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "meeting not found", http.StatusNotFound)
		return
	}
	m, err := s.pcStore.GetMeeting(r.Context(), id)
	if errors.Is(err, postcall.ErrMeetingNotFound) {
		http.Error(w, "meeting not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Warn("meeting get failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, m)
}
