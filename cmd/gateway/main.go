// Command gateway is the Realtime Gateway entrypoint. It loads config, initializes
// telemetry, builds the gateway HTTP/WS server, and serves with graceful shutdown.
// Kept thin: all logic lives in internal/gateway and internal/platform.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/VanshNarang12/sales-agent/internal/gateway"
	"github.com/VanshNarang12/sales-agent/internal/platform/config"
	"github.com/VanshNarang12/sales-agent/internal/platform/secrets"
	"github.com/VanshNarang12/sales-agent/internal/platform/telemetry"
)

const serviceName = "gateway"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(serviceName)
	if err != nil {
		return err
	}

	// Secrets: signing key for session tokens (env-backed locally). Not needed when
	// auth is disabled for dev (login/OAuth deferred to Stage 0.5).
	var signingKey string
	if !cfg.AuthDisabled {
		signingKey, err = secrets.EnvStore{}.Get(ctx, "AUTH_SIGNING_KEY")
		if err != nil {
			return err
		}
	} else {
		log.Warn("AUTH_DISABLED: realtime endpoint is unauthenticated (dev only)")
	}

	shutdownTelemetry, err := telemetry.Init(ctx, serviceName, cfg.OTelEndpoint)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTelemetry(ctx)
	}()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           gateway.New(cfg, signingKey, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("gateway listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
