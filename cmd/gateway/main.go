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

	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/gateway"
	"github.com/VanshNarang12/sales-agent/internal/platform/config"
	"github.com/VanshNarang12/sales-agent/internal/platform/secrets"
	"github.com/VanshNarang12/sales-agent/internal/platform/telemetry"
	"github.com/VanshNarang12/sales-agent/internal/stt"
	"github.com/VanshNarang12/sales-agent/internal/stt/deepgram"
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

	sttMgr := buildSTT(ctx, cfg, log)
	detectEng := buildDetect(log)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           gateway.New(cfg, signingKey, sttMgr, detectEng, log).Handler(),
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
func buildDetect(log *slog.Logger) *detect.Engine {
	return detect.NewEngine(log, nil)
}

func buildSTT(ctx context.Context, cfg *config.Config, log *slog.Logger) *stt.Manager {
	switch cfg.STTProvider {
	case "deepgram":
		key, err := secrets.EnvStore{}.Get(ctx, secrets.KeyDeepgramAPI)
		if err != nil {
			log.Warn("STT disabled: Deepgram key unavailable, running audio-only", "err", err)
			return nil
		}
		prov := deepgram.New(deepgram.Config{
			APIKey:        key,
			Model:         cfg.STTModel,
			EndpointingMs: cfg.STTEndpointingMs,
			Logger:        log,
		})
		log.Info("STT enabled", "provider", cfg.STTProvider, "model", cfg.STTModel)
		return stt.NewManager(prov, log)
	default:
		log.Warn("STT disabled: unknown provider, running audio-only", "provider", cfg.STTProvider)
		return nil
	}
}
