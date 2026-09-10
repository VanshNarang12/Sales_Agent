// Package gateway is the Realtime Gateway: it terminates the client connection,
// authenticates the session, sets tenant context, and (from Stage 1) streams audio
// to the Call Session Orchestrator. In Stage 0 it proves the streaming path end to
// end with an authenticated, tenant-scoped WebSocket echo. See ARCHITECTURE.md §4/§5.
package gateway

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/platform/auth"
	"github.com/VanshNarang12/sales-agent/internal/platform/config"
	"github.com/VanshNarang12/sales-agent/internal/platform/telemetry"
	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
	"github.com/VanshNarang12/sales-agent/internal/stt"
)

// Server holds gateway dependencies.
type Server struct {
	cfg        *config.Config
	signingKey string
	stt        *stt.Manager
	detect     *detect.Engine
	extract    *retrieval.Extractor
	search     *retrieval.Searcher
	ingest     DocumentIngester
	log        *slog.Logger
}

func New(cfg *config.Config, signingKey string, sttMgr *stt.Manager, detectEng *detect.Engine, extractor *retrieval.Extractor, searcher *retrieval.Searcher, ingester DocumentIngester, log *slog.Logger) *Server {
	return &Server{cfg: cfg, signingKey: signingKey, stt: sttMgr, detect: detectEng, extract: extractor, search: searcher, ingest: ingester, log: log}
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

	// Authenticated, tenant-scoped realtime endpoint.
	mux.Handle("GET /v1/realtime", s.authMiddleware(http.HandlerFunc(s.handleRealtime)))
	mux.Handle("POST /v1/documents", s.authMiddleware(http.HandlerFunc(s.handleDocumentUpload)))

	return telemetry.HTTPMiddleware(s.cfg.ServiceName)(mux)
}

// authMiddleware verifies the bearer token and injects the tenant (org) into context.
// It fails closed: no valid token -> 401, never an unscoped request.
//
// DEV ONLY: when cfg.AuthDisabled is set (dev env), it skips token verification and
// injects cfg.DevTenantID, so the core pipeline can be built/tested before the
// login/OAuth flow exists (deferred to Stage 0.5). Tenancy still flows end to end.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthDisabled {
			ctx := tenancy.With(r.Context(), tenancy.TenantID(s.cfg.DevTenantID))
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims, err := auth.Verify(s.signingKey, token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := tenancy.With(r.Context(), tenancy.TenantID(claims.OrgID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
