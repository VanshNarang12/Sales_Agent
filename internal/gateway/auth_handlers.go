package gateway

// Auth endpoints (Stage 0.5): signup, login, Google sign-in, WhatsApp OTP phone
// verification, refresh rotation, logout, and /v1/me. See techdocs/auth_techdoc.md.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/VanshNarang12/sales-agent/internal/identity"
	"github.com/VanshNarang12/sales-agent/internal/platform/auth"
)

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		OrgName  string `json:"org_name"`
	}
	if !s.decodeAuthReq(w, r, &req) {
		return
	}
	sess, err := s.idsvc.Signup(r.Context(), req.Email, req.Password, req.OrgName)
	if err != nil {
		authError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		OrgID    string `json:"org_id"` // required only when the email spans several orgs
	}
	if !s.decodeAuthReq(w, r, &req) {
		return
	}
	sess, err := s.idsvc.Login(r.Context(), req.Email, req.Password, req.OrgID)
	if err != nil {
		authError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleGoogleAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDToken string `json:"id_token"`
		OrgName string `json:"org_name"`
	}
	if !s.decodeAuthReq(w, r, &req) {
		return
	}
	sess, err := s.idsvc.GoogleAuth(r.Context(), req.IDToken, req.OrgName)
	if err != nil {
		authError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleOTPSend(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Phone string `json:"phone"`
	}
	if !s.decodeAuthReq(w, r, &req) {
		return
	}
	if err := s.idsvc.SendOTP(r.Context(), claims, req.Phone); err != nil {
		authError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

func (s *Server) handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !s.decodeAuthReq(w, r, &req) {
		return
	}
	sess, err := s.idsvc.VerifyOTP(r.Context(), claims, req.Code)
	if err != nil {
		authError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !s.decodeAuthReq(w, r, &req) {
		return
	}
	sess, err := s.idsvc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		authError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !s.decodeAuthReq(w, r, &req) {
		return
	}
	if err := s.idsvc.Logout(r.Context(), req.RefreshToken); err != nil {
		authError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.idsvc == nil {
		http.Error(w, "auth service unavailable", http.StatusServiceUnavailable)
		return
	}
	u, err := s.idsvc.Me(r.Context(), claims)
	if err != nil {
		authError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        u.ID,
		"org_id":         u.OrgID,
		"email":          u.Email,
		"phone_verified": u.PhoneVerifiedAt != nil,
		"scope":          claims.Scope,
	})
}

// decodeAuthReq parses the JSON body (1 MB cap) and 503s if auth isn't wired.
func (s *Server) decodeAuthReq(w http.ResponseWriter, r *http.Request, v any) bool {
	if s.idsvc == nil {
		http.Error(w, "auth service unavailable", http.StatusServiceUnavailable)
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

// authError maps identity errors to HTTP statuses; unknown errors become 500
// without leaking internals.
func authError(w http.ResponseWriter, err error) {
	// Multi-workspace email: tell the client which orgs to offer (re-submit with org_id).
	var sel *identity.OrgSelectionError
	if errors.As(err, &sel) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": sel.Error(), "orgs": sel.Options})
		return
	}
	status := http.StatusInternalServerError
	msg := "internal error"
	switch {
	case errors.Is(err, identity.ErrInvalidEmail),
		errors.Is(err, identity.ErrWeakPassword),
		errors.Is(err, identity.ErrInvalidPhone),
		errors.Is(err, identity.ErrOTPExpired),
		errors.Is(err, identity.ErrOTPMismatch):
		status, msg = http.StatusBadRequest, err.Error()
	case errors.Is(err, identity.ErrInvalidCredentials),
		errors.Is(err, identity.ErrGoogleToken),
		errors.Is(err, identity.ErrTokenReuse),
		errors.Is(err, auth.ErrInvalidToken):
		status, msg = http.StatusUnauthorized, err.Error()
	case errors.Is(err, identity.ErrUserNotFound):
		status, msg = http.StatusNotFound, err.Error()
	case errors.Is(err, identity.ErrEmailTaken),
		errors.Is(err, identity.ErrPhoneTaken),
		errors.Is(err, identity.ErrEmailAmbiguous),
		errors.Is(err, identity.ErrAlreadyVerified):
		status, msg = http.StatusConflict, err.Error()
	case errors.Is(err, identity.ErrOTPCooldown),
		errors.Is(err, identity.ErrOTPDailyCap),
		errors.Is(err, identity.ErrOTPMaxAttempts):
		status, msg = http.StatusTooManyRequests, err.Error()
	case errors.Is(err, identity.ErrGoogleDisabled),
		errors.Is(err, identity.ErrOTPUnavailable):
		status, msg = http.StatusServiceUnavailable, err.Error()
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
