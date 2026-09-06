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

	"github.com/redis/go-redis/v9"

	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/gateway"
	"github.com/VanshNarang12/sales-agent/internal/kb"
	"github.com/VanshNarang12/sales-agent/internal/llm"
	"github.com/VanshNarang12/sales-agent/internal/platform/config"
	"github.com/VanshNarang12/sales-agent/internal/platform/db"
	"github.com/VanshNarang12/sales-agent/internal/platform/secrets"
	"github.com/VanshNarang12/sales-agent/internal/platform/telemetry"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
	"github.com/VanshNarang12/sales-agent/internal/stt"
	"github.com/VanshNarang12/sales-agent/internal/stt/deepgram"
	"github.com/VanshNarang12/sales-agent/internal/transcript"
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
	detectEng := buildDetect(ctx, cfg, log)
	extractor := buildExtract(ctx, cfg, log)
	ingester := buildIngest(ctx, cfg, log)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           gateway.New(cfg, signingKey, sttMgr, detectEng, extractor, ingester, log).Handler(),
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
// buildDetect wires the suggestion trigger to the Redis transcript store. Suggest
// needs the store: an unreachable Redis at boot disables the trigger (audio still runs).
func buildDetect(ctx context.Context, cfg *config.Config, log *slog.Logger) *detect.Engine {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Warn("suggest disabled: bad REDIS_URL", "err", err)
		return nil
	}
	rdb := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Warn("suggest disabled: redis unreachable", "err", err)
		return nil
	}
	store := transcript.New(rdb, time.Duration(cfg.TranscriptTTLSeconds)*time.Second)
	log.Info("transcript store enabled", "ttl_s", cfg.TranscriptTTLSeconds, "lookback_ms", cfg.SuggestLookbackMs)
	return detect.NewEngine(log, store, int64(cfg.SuggestLookbackMs))
}

// buildExtract wires the LLM ask-extraction (retrieval step 1). No key / unknown
// provider ⇒ extraction disabled: Suggest logs the raw window, calls unaffected.
func buildExtract(ctx context.Context, cfg *config.Config, log *slog.Logger) *retrieval.Extractor {
	secretKey := map[string]string{
		"anthropic":         secrets.KeyAnthropicAPI,
		"openai_compatible": secrets.KeyOpenAIAPI,
	}[cfg.LLMExtractProvider]
	var apiKey string
	if secretKey != "" {
		if k, err := (secrets.EnvStore{}).Get(ctx, secretKey); err == nil {
			apiKey = k
		}
	}
	model, err := llm.New(llm.Config{
		Provider:  cfg.LLMExtractProvider,
		Model:     cfg.LLMExtractModel,
		APIKey:    apiKey,
		BaseURL:   cfg.LLMExtractBaseURL,
		MaxTokens: int64(cfg.LLMExtractMaxTokens),
	})
	if err != nil {
		log.Warn("extraction disabled; Suggest will log the raw window", "err", err)
		return nil
	}
	log.Info("extraction enabled", "provider", cfg.LLMExtractProvider, "model", cfg.LLMExtractModel)
	return retrieval.NewExtractor(model)
}

// buildIngest wires Stage-4 upload: Neon pool + embedder + Ingester. Any missing
// piece ⇒ nil ⇒ POST /v1/documents answers 503; live calls unaffected.
func buildIngest(ctx context.Context, cfg *config.Config, log *slog.Logger) gateway.DocumentIngester {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Warn("ingest disabled: database unreachable", "err", err)
		return nil
	}
	var apiKey string
	if k, err := (secrets.EnvStore{}).Get(ctx, secrets.KeyOpenAIAPI); err == nil {
		apiKey = k
	}
	embedder, err := embed.New(embed.Config{
		Provider:  cfg.EmbedProvider,
		Model:     cfg.EmbedModel,
		APIKey:    apiKey,
		BaseURL:   cfg.EmbedBaseURL,
		Dims:      cfg.EmbedDims,
		BatchSize: cfg.EmbedBatchSize,
	})
	if err != nil {
		log.Warn("ingest disabled: embedder", "err", err)
		return nil
	}
	log.Info("ingest enabled", "embed_provider", cfg.EmbedProvider, "embed_model", cfg.EmbedModel)
	return kb.NewIngester(embedder, kb.NewStore(pool), cfg.ChunkTargetTokens, cfg.ChunkOverlapTokens)
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
