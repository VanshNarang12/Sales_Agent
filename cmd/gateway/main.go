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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/VanshNarang12/sales-agent/internal/customer"
	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/gateway"
	"github.com/VanshNarang12/sales-agent/internal/kb"
	"github.com/VanshNarang12/sales-agent/internal/llm"
	"github.com/VanshNarang12/sales-agent/internal/platform/config"
	"github.com/VanshNarang12/sales-agent/internal/platform/db"
	"github.com/VanshNarang12/sales-agent/internal/platform/secrets"
	"github.com/VanshNarang12/sales-agent/internal/platform/telemetry"
	"github.com/VanshNarang12/sales-agent/internal/postcall"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
	"github.com/VanshNarang12/sales-agent/internal/stt"
	"github.com/VanshNarang12/sales-agent/internal/stt/deepgram"
	"github.com/VanshNarang12/sales-agent/internal/suggest"
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
	tstore := buildTranscriptStore(ctx, cfg, log)
	detectEng := buildDetect(cfg, tstore, log)
	extractor := buildExtract(ctx, cfg, log)
	pool, embedder := buildKB(ctx, cfg, log)
	ingester := buildIngest(pool, embedder, cfg, log)
	searcher := buildSearch(pool, embedder, cfg, log)
	generator := buildSuggest(ctx, cfg, log)
	summarizer := buildPostcall(ctx, cfg, log)
	var pcStore *postcall.Store
	var custStore *customer.Store
	if pool != nil {
		pcStore = postcall.NewStore(pool)
		custStore = customer.NewStore(pool)
	} else {
		log.Warn("summary persistence disabled: no database")
	}
	chat := buildChat(ctx, cfg, custStore, searcher, embedder, log)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           gateway.New(cfg, signingKey, sttMgr, detectEng, extractor, searcher, generator, ingester, tstore, summarizer, pcStore, embedder, custStore, chat, log).Handler(),
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

// buildTranscriptStore connects the Redis live-transcript store, shared by the
// Suggest trigger (window reads) and the post-call summarizer (full reads).
// Unreachable Redis at boot ⇒ nil ⇒ both degrade, audio still runs.
func buildTranscriptStore(ctx context.Context, cfg *config.Config, log *slog.Logger) *transcript.Store {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Warn("transcript store disabled: bad REDIS_URL", "err", err)
		return nil
	}
	rdb := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Warn("transcript store disabled: redis unreachable", "err", err)
		return nil
	}
	log.Info("transcript store enabled", "ttl_s", cfg.TranscriptTTLSeconds, "lookback_ms", cfg.SuggestLookbackMs)
	return transcript.New(rdb, time.Duration(cfg.TranscriptTTLSeconds)*time.Second)
}

// buildDetect wires the suggestion trigger to the transcript store. No store ⇒
// Suggest disabled (audio still runs).
func buildDetect(cfg *config.Config, store *transcript.Store, log *slog.Logger) *detect.Engine {
	if store == nil {
		log.Warn("suggest disabled: no transcript store")
		return nil
	}
	return detect.NewEngine(log, store, int64(cfg.SuggestLookbackMs))
}

// buildChat wires the prep chat (10.8). Any missing dependency ⇒ nil ⇒ the chat
// endpoint answers 503; timeline reads and calls are unaffected.
func buildChat(ctx context.Context, cfg *config.Config, custStore *customer.Store, searcher *retrieval.Searcher, embedder embed.Embedder, log *slog.Logger) *customer.Chat {
	if custStore == nil || searcher == nil || embedder == nil {
		log.Warn("prep chat disabled: missing DB/search/embedder")
		return nil
	}
	secretKey := map[string]string{
		"anthropic":         secrets.KeyAnthropicAPI,
		"openai_compatible": secrets.KeyOpenAIAPI,
	}[cfg.LLMChatProvider]
	var apiKey string
	if secretKey != "" {
		if k, err := (secrets.EnvStore{}).Get(ctx, secretKey); err == nil {
			apiKey = k
		}
	}
	model, err := llm.New(llm.Config{
		Provider:  cfg.LLMChatProvider,
		Model:     cfg.LLMChatModel,
		APIKey:    apiKey,
		BaseURL:   cfg.LLMChatBaseURL,
		MaxTokens: int64(cfg.LLMChatMaxTokens),
	})
	if err != nil {
		log.Warn("prep chat disabled", "err", err)
		return nil
	}
	log.Info("prep chat enabled", "provider", cfg.LLMChatProvider, "model", cfg.LLMChatModel,
		"recent", cfg.ChatRecentSummaries, "topk_summaries", cfg.ChatTopKSummaries, "topk_chunks", cfg.ChatTopKChunks)
	return customer.NewChat(model, embedder, custStore, searcher, customer.ChatConfig{
		RecentSummaries: cfg.ChatRecentSummaries,
		TopKSummaries:   cfg.ChatTopKSummaries,
		TopKChunks:      cfg.ChatTopKChunks,
		MaxTurns:        cfg.ChatMaxTurns,
	})
}

// buildPostcall wires the Stage-10 summarizer. No key / unknown provider ⇒ nil ⇒
// calls end without a summary, everything else unaffected.
func buildPostcall(ctx context.Context, cfg *config.Config, log *slog.Logger) *postcall.Summarizer {
	secretKey := map[string]string{
		"anthropic":         secrets.KeyAnthropicAPI,
		"openai_compatible": secrets.KeyOpenAIAPI,
	}[cfg.LLMSummaryProvider]
	var apiKey string
	if secretKey != "" {
		if k, err := (secrets.EnvStore{}).Get(ctx, secretKey); err == nil {
			apiKey = k
		}
	}
	model, err := llm.New(llm.Config{
		Provider:  cfg.LLMSummaryProvider,
		Model:     cfg.LLMSummaryModel,
		APIKey:    apiKey,
		BaseURL:   cfg.LLMSummaryBaseURL,
		MaxTokens: int64(cfg.LLMSummaryMaxTokens),
	})
	if err != nil {
		log.Warn("post-call summaries disabled", "err", err)
		return nil
	}
	log.Info("post-call summaries enabled", "provider", cfg.LLMSummaryProvider, "model", cfg.LLMSummaryModel)
	return postcall.NewSummarizer(model)
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

// buildKB connects the KB dependencies shared by ingest (Stage 4) and search
// (Stage 5): the Neon pool and the embedder. Either may come back nil; each
// consumer degrades on its own, live calls unaffected.
func buildKB(ctx context.Context, cfg *config.Config, log *slog.Logger) (*pgxpool.Pool, embed.Embedder) {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Warn("KB disabled: database unreachable", "err", err)
		return nil, nil
	}
	// EMBED_API_KEY first (embed vendor ≠ chat vendor, e.g. Gemini), else OPENAI_API_KEY.
	var apiKey string
	if k, err := (secrets.EnvStore{}).Get(ctx, secrets.KeyEmbedAPI); err == nil {
		apiKey = k
	} else if k, err := (secrets.EnvStore{}).Get(ctx, secrets.KeyOpenAIAPI); err == nil {
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
		log.Warn("KB disabled: embedder", "err", err)
		return pool, nil
	}
	return pool, embedder
}

// buildIngest wires Stage-4 upload. Missing deps ⇒ nil ⇒ POST /v1/documents answers 503.
func buildIngest(pool *pgxpool.Pool, embedder embed.Embedder, cfg *config.Config, log *slog.Logger) gateway.DocumentIngester {
	if pool == nil || embedder == nil {
		log.Warn("ingest disabled: missing KB dependencies")
		return nil
	}
	log.Info("ingest enabled", "embed_provider", cfg.EmbedProvider, "embed_model", cfg.EmbedModel)
	return kb.NewIngester(embedder, kb.NewStore(pool), cfg.ChunkTargetTokens, cfg.ChunkOverlapTokens)
}

// buildSearch wires Stage-5 retrieval. Missing deps ⇒ nil ⇒ Suggest logs the ask only.
func buildSearch(pool *pgxpool.Pool, embedder embed.Embedder, cfg *config.Config, log *slog.Logger) *retrieval.Searcher {
	if pool == nil || embedder == nil {
		log.Warn("search disabled: Suggest will log the ask only")
		return nil
	}
	log.Info("retrieval search enabled", "top_k", cfg.RetrievalTopK, "min_score", cfg.RetrievalMinScore)
	return retrieval.NewSearcher(embedder, pool, cfg.RetrievalTopK, cfg.RetrievalMinScore)
}

// buildSuggest wires Stage-6 card generation. No key / unknown provider ⇒ nil ⇒
// Suggest still sends raw hits, just without a card (same degradation as extract).
func buildSuggest(ctx context.Context, cfg *config.Config, log *slog.Logger) *suggest.Generator {
	secretKey := map[string]string{
		"anthropic":         secrets.KeyAnthropicAPI,
		"openai_compatible": secrets.KeyOpenAIAPI,
	}[cfg.LLMAnswerProvider]
	var apiKey string
	if secretKey != "" {
		if k, err := (secrets.EnvStore{}).Get(ctx, secretKey); err == nil {
			apiKey = k
		}
	}
	model, err := llm.New(llm.Config{
		Provider:  cfg.LLMAnswerProvider,
		Model:     cfg.LLMAnswerModel,
		APIKey:    apiKey,
		BaseURL:   cfg.LLMAnswerBaseURL,
		MaxTokens: int64(cfg.LLMAnswerMaxTokens),
	})
	if err != nil {
		log.Warn("card generation disabled; Suggest will send raw hits only", "err", err)
		return nil
	}
	log.Info("card generation enabled", "provider", cfg.LLMAnswerProvider, "model", cfg.LLMAnswerModel, "min_confidence", cfg.SuggestMinConfidence)
	return suggest.NewGenerator(model)
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
