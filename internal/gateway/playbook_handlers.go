package gateway

// Stage 11 playbook APIs (admin_playbook_techdoc.md §7): reads are org-wide;
// writes are the first real use of users.role — admin only.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/VanshNarang12/sales-agent/internal/platform/auth"
	"github.com/VanshNarang12/sales-agent/internal/playbook"
)

// requireAdmin loads the caller and rejects non-admins. Under the dev bypass
// there is no real user — the check is skipped (dev only, like all of AUTH_DISABLED).
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (userID string, ok bool) {
	if s.cfg.AuthDisabled {
		return "", true
	}
	claims, has := auth.ClaimsFrom(r.Context())
	if !has || s.idsvc == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	u, err := s.idsvc.Me(r.Context(), claims)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	if u.Role != "admin" {
		http.Error(w, "admin role required", http.StatusForbidden)
		return "", false
	}
	return u.ID, true
}

// GET /v1/playbook — guidance + rules (any member).
func (s *Server) handlePlaybookGet(w http.ResponseWriter, r *http.Request) {
	if s.pbStore == nil {
		http.Error(w, "playbook unavailable", http.StatusServiceUnavailable)
		return
	}
	pb, err := s.pbStore.Get(r.Context())
	if err != nil {
		s.log.Warn("playbook get failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, pb)
}

// PUT /v1/playbook {guidance} — admin only.
func (s *Server) handlePlaybookPut(w http.ResponseWriter, r *http.Request) {
	if s.pbStore == nil {
		http.Error(w, "playbook unavailable", http.StatusServiceUnavailable)
		return
	}
	userID, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		Guidance string `json:"guidance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "malformed JSON body", http.StatusBadRequest)
		return
	}
	if err := s.pbStore.SetGuidance(r.Context(), strings.TrimSpace(in.Guidance), userID); err != nil {
		s.log.Warn("playbook save failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/playbook/rules {phrase, reason} — admin only.
func (s *Server) handleRuleAdd(w http.ResponseWriter, r *http.Request) {
	if s.pbStore == nil {
		http.Error(w, "playbook unavailable", http.StatusServiceUnavailable)
		return
	}
	userID, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		Phrase string `json:"phrase"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Phrase) == "" {
		http.Error(w, `"phrase" is required`, http.StatusBadRequest)
		return
	}
	rule, err := s.pbStore.AddRule(r.Context(), in.Phrase, in.Reason, userID)
	if err != nil {
		s.log.Warn("playbook rule add failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

// DELETE /v1/playbook/rules/{id} — admin only.
func (s *Server) handleRuleDelete(w http.ResponseWriter, r *http.Request) {
	if s.pbStore == nil {
		http.Error(w, "playbook unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	if uuid.Validate(id) != nil {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}
	err := s.pbStore.DeleteRule(r.Context(), id)
	if errors.Is(err, playbook.ErrRuleNotFound) {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Warn("playbook rule delete failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
