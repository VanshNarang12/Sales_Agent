// Package gateway is the Realtime Gateway: it terminates the client connection,
// authenticates the session, sets tenant context, and (from Stage 1) streams audio
// to the Call Session Orchestrator. In Stage 0 it proves the streaming path end to
// end with an authenticated, tenant-scoped WebSocket echo. See ARCHITECTURE.md §4/§5.
package gateway

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/VanshNarang12/sales-agent/internal/customer"
	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/identity"
	"github.com/VanshNarang12/sales-agent/internal/jobs"
	"github.com/VanshNarang12/sales-agent/internal/platform/auth"
	"github.com/VanshNarang12/sales-agent/internal/platform/config"
	"github.com/VanshNarang12/sales-agent/internal/platform/telemetry"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
	"github.com/VanshNarang12/sales-agent/internal/playbook"
	"github.com/VanshNarang12/sales-agent/internal/postcall"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
	"github.com/VanshNarang12/sales-agent/internal/stt"
	"github.com/VanshNarang12/sales-agent/internal/suggest"
	"github.com/VanshNarang12/sales-agent/internal/transcript"
)

// Server holds gateway dependencies.
type Server struct {
	cfg        *config.Config
	signingKey string
	stt        *stt.Manager
	detect     *detect.Engine
	extract    *retrieval.Extractor
	search     *retrieval.Searcher
	suggest    *suggest.Generator
	ingest     DocumentIngester
	docList    DocumentLister
	tstore     *transcript.Store
	summarizer *postcall.Summarizer
	pcStore    *postcall.Store
	embedder   embed.Embedder
	custStore  *customer.Store
	chat       *customer.Chat
	idsvc      *identity.Service
	jobq       *jobs.Queue
	pbStore    *playbook.Store
	log        *slog.Logger
}

func New(cfg *config.Config, signingKey string, sttMgr *stt.Manager, detectEng *detect.Engine, extractor *retrieval.Extractor, searcher *retrieval.Searcher, generator *suggest.Generator, ingester DocumentIngester, docList DocumentLister, tstore *transcript.Store, summarizer *postcall.Summarizer, pcStore *postcall.Store, embedder embed.Embedder, custStore *customer.Store, chat *customer.Chat, idsvc *identity.Service, jobq *jobs.Queue, pbStore *playbook.Store, log *slog.Logger) *Server {
	return &Server{cfg: cfg, signingKey: signingKey, stt: sttMgr, detect: detectEng, extract: extractor, search: searcher, suggest: generator, ingest: ingester, docList: docList, tstore: tstore, summarizer: summarizer, pcStore: pcStore, embedder: embedder, custStore: custStore, chat: chat, idsvc: idsvc, jobq: jobq, pbStore: pbStore, log: log}
}

// Handler builds the HTTP/WS routes with telemetry and auth wired in.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Liveness/readiness — unauthenticated.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	// Prometheus metrics.
	mux.Handle("GET /metrics", telemetry.MetricsHandler())

	// Auth flows (Stage 0.5). Signup/login/google/refresh/logout are public;
	// the OTP pair and /v1/me accept any authenticated scope (pre or full).
	mux.HandleFunc("POST /v1/auth/signup", s.handleSignup)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/google", s.handleGoogleAuth)
	mux.HandleFunc("POST /v1/auth/refresh", s.handleRefresh)
	mux.HandleFunc("POST /v1/auth/logout", s.handleLogout)
	mux.Handle("POST /v1/auth/otp/send", s.anyScopeMiddleware(http.HandlerFunc(s.handleOTPSend)))
	mux.Handle("POST /v1/auth/otp/verify", s.anyScopeMiddleware(http.HandlerFunc(s.handleOTPVerify)))
	mux.Handle("GET /v1/me", s.anyScopeMiddleware(http.HandlerFunc(s.handleMe)))

	// Authenticated, tenant-scoped realtime endpoint.
	mux.Handle("GET /v1/realtime", s.authMiddleware(http.HandlerFunc(s.handleRealtime)))
	mux.Handle("POST /v1/documents", s.authMiddleware(http.HandlerFunc(s.handleDocumentUpload)))
	mux.Handle("GET /v1/documents", s.authMiddleware(http.HandlerFunc(s.handleDocumentList)))

	// Meetings: org-wide call history for the web app (migration 0007).
	mux.Handle("GET /v1/meetings", s.authMiddleware(http.HandlerFunc(s.handleMeetingList)))
	mux.Handle("GET /v1/meetings/{id}", s.authMiddleware(http.HandlerFunc(s.handleMeetingGet)))
	mux.Handle("PATCH /v1/meetings/{id}/customer", s.authMiddleware(http.HandlerFunc(s.handleMeetingTag)))

	// Playbook: org guidance + do-not-say rules (Stage 11). Writes are admin-only.
	mux.Handle("GET /v1/playbook", s.authMiddleware(http.HandlerFunc(s.handlePlaybookGet)))
	mux.Handle("PUT /v1/playbook", s.authMiddleware(http.HandlerFunc(s.handlePlaybookPut)))
	mux.Handle("POST /v1/playbook/rules", s.authMiddleware(http.HandlerFunc(s.handleRuleAdd)))
	mux.Handle("DELETE /v1/playbook/rules/{id}", s.authMiddleware(http.HandlerFunc(s.handleRuleDelete)))

	// Customer memory: timeline + prep chat (Stage 10.7/10.8).
	mux.Handle("GET /v1/customers", s.authMiddleware(http.HandlerFunc(s.handleCustomerList)))
	mux.Handle("POST /v1/customers", s.authMiddleware(http.HandlerFunc(s.handleCustomerCreate)))
	mux.Handle("GET /v1/customers/{id}/summaries", s.authMiddleware(http.HandlerFunc(s.handleSummaryList)))
	mux.Handle("GET /v1/summaries/{id}", s.authMiddleware(http.HandlerFunc(s.handleSummaryGet)))
	mux.Handle("POST /v1/customers/{id}/chat", s.authMiddleware(http.HandlerFunc(s.handleChat)))

	return telemetry.HTTPMiddleware(s.cfg.ServiceName)(s.corsMiddleware(mux))
}

// corsMiddleware admits the web app's browser origin (exact allowlist, no
// wildcard). Non-browser clients (Electron WS, curl) send no Origin and pass
// through untouched.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(s.cfg.CORSAllowedOrigins))
	for _, o := range s.cfg.CORSAllowedOrigins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && allowed[origin] {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware verifies the bearer token and injects the tenant (org) into context.
// It fails closed: no valid token -> 401, never an unscoped request.
//
// DEV ONLY: when cfg.AuthDisabled is set (dev env), it skips token verification and
// injects cfg.DevTenantID, so the core pipeline can be built/tested before the
// login/OAuth flow exists (deferred to Stage 0.5). Tenancy still flows end to end.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return s.verifyToken(next, true)
}

// anyScopeMiddleware admits pre-scope tokens too — only the OTP-verification pair
// and /v1/me may run before the phone is verified (auth_techdoc.md §3).
func (s *Server) anyScopeMiddleware(next http.Handler) http.Handler {
	return s.verifyToken(next, false)
}

func (s *Server) verifyToken(next http.Handler, requireFull bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthDisabled {
			ctx := tenancy.With(r.Context(), tenancy.TenantID(s.cfg.DevTenantID))
			ctx = auth.WithClaims(ctx, auth.Claims{OrgID: s.cfg.DevTenantID, Scope: auth.ScopeFull})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims, err := auth.Verify(s.signingKey, token)
		if err != nil || claims.Scope == auth.ScopeRefresh {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if requireFull && !claims.Full() {
			http.Error(w, "phone verification required", http.StatusForbidden)
			return
		}
		ctx := tenancy.With(r.Context(), tenancy.TenantID(claims.OrgID))
		ctx = auth.WithClaims(ctx, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
