// Package config loads and validates service configuration from the environment.
// All configuration is read once at boot and validated; services never read os.Getenv
// directly elsewhere (see techdocs/coding_standards_techdoc.md §8).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the typed configuration shared by all services. Service-specific fields
// can be added as needed; cross-cutting fields live here.
type Config struct {
	// Env is one of: dev, staging, prod.
	Env string
	// ServiceName identifies the running service in telemetry.
	ServiceName string
	// HTTPAddr is the listen address for the HTTP/WS server (e.g. ":8080").
	HTTPAddr string

	// DatabaseURL is the Postgres DSN (pgvector-enabled).
	DatabaseURL string
	// RedisURL is the Redis connection string.
	RedisURL string
	// NATSURL is the event-bus connection string.
	NATSURL string

	// OTelEndpoint is the OTLP/HTTP traces endpoint (e.g. "localhost:4318").
	OTelEndpoint string

	// AuthSigningKey is the HMAC key used to sign session tokens (from secrets mgr).
	AuthSigningKey string

	// AuthDisabled bypasses login on the realtime endpoint and injects DevTenantID.
	// DEV ONLY — ignored unless Env == "dev". Lets us build/test the core pipeline
	// before the OAuth/login flow is built (deferred to Stage 0.5).
	AuthDisabled bool
	// DevTenantID is the org id used when AuthDisabled is on.
	DevTenantID string

	// STTProvider selects the speech-to-text adapter (e.g. "deepgram"). The provider's
	// API key is a secret fetched from the vault, not stored here (transcription D2).
	STTProvider string
	// STTModel is the provider model id (e.g. "nova-3").
	STTModel string
	// STTEndpointingMs is the silence threshold (ms) that marks end-of-turn (feature 2.4).
	STTEndpointingMs int

	// SuggestLookbackMs
	SuggestLookbackMs    int
	TranscriptTTLSeconds int

	// LLM extraction role (retrieval step 1) — provider/model are registry lookups,
	// so swapping vendors is an env edit (query_extraction_techdoc.md §3).
	LLMExtractProvider  string
	LLMExtractModel     string
	LLMExtractBaseURL   string
	LLMExtractMaxTokens int

	// Knowledge-base ingest (Stage 4) — knowledge_base_techdoc.md §9.
	EmbedProvider      string
	EmbedModel         string
	EmbedBaseURL       string
	EmbedDims          int // must match the migration's vector(N)
	EmbedBatchSize     int
	ChunkTargetTokens  int // approximate tokens (chars ÷ 4)
	ChunkOverlapTokens int
	MaxUploadMB        int

	// Retrieval search (Stage 5) — rag_retrieval_techdoc.md §9.
	RetrievalTopK     int
	RetrievalMinScore float64 // cosine-similarity gate; below it a hit is dropped (5.5)

	// LLM answer role (Stage 6 card generation) — suggestion_generation_techdoc.md §9.
	LLMAnswerProvider  string
	LLMAnswerModel     string
	LLMAnswerBaseURL   string
	LLMAnswerMaxTokens int
	// SuggestMinConfidence gates cards: below it the card is dropped, hits still sent (18.9).
	SuggestMinConfidence float64

	// LLM summary role (Stage 10 post-call digest) — post_call_techdoc.md §9.
	LLMSummaryProvider  string
	LLMSummaryModel     string
	LLMSummaryBaseURL   string
	LLMSummaryMaxTokens int

	// Prep chat (Stage 10.8) — customer_memory_techdoc.md §9. Every context knob
	// is configurable by design (user decision 2026-09-17).
	LLMChatProvider     string
	LLMChatModel        string
	LLMChatBaseURL      string
	LLMChatMaxTokens    int
	ChatRecentSummaries int // newest digests always included
	ChatTopKSummaries   int // semantically retrieved digests
	ChatTopKChunks      int // retrieved KB chunks
	ChatMaxTurns        int // chat-history cap per turn
}

// Load reads configuration from the environment and validates it.
func Load(serviceName string) (*Config, error) {
	c := &Config{
		Env:            getenv("ENV", "dev"),
		ServiceName:    serviceName,
		HTTPAddr:       getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		RedisURL:       getenv("REDIS_URL", "redis://localhost:6379"),
		NATSURL:        getenv("NATS_URL", "nats://localhost:4222"),
		OTelEndpoint:   getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318"),
		AuthSigningKey: os.Getenv("AUTH_SIGNING_KEY"),
		AuthDisabled:   os.Getenv("AUTH_DISABLED") == "true",
		DevTenantID:    getenv("DEV_TENANT_ID", "00000000-0000-0000-0000-000000000001"),

		STTProvider:      getenv("STT_PROVIDER", "deepgram"),
		STTModel:         getenv("STT_MODEL", "nova-3"),
		STTEndpointingMs: getenvInt("STT_ENDPOINTING_MS", 300),

		SuggestLookbackMs:    getenvInt("SUGGEST_LOOKBACK_MS", 90_000),
		TranscriptTTLSeconds: getenvInt("TRANSCRIPT_TTL_SECONDS", 1800),

		LLMExtractProvider:  getenv("LLM_EXTRACT_PROVIDER", "openai_compatible"),
		LLMExtractModel:     getenv("LLM_EXTRACT_MODEL", "openai/gpt-oss-20b"),
		LLMExtractBaseURL:   getenv("LLM_EXTRACT_BASE_URL", "https://api.groq.com/openai/v1"),
		LLMExtractMaxTokens: getenvInt("LLM_EXTRACT_MAX_TOKENS", 300),

		EmbedProvider:      getenv("EMBED_PROVIDER", "openai_compatible"),
		EmbedModel:         getenv("EMBED_MODEL", "nomic-embed-text-v1.5"),
		EmbedBaseURL:       getenv("EMBED_BASE_URL", "https://api.groq.com/openai/v1"),
		EmbedDims:          getenvInt("EMBED_DIMS", 768),
		EmbedBatchSize:     getenvInt("EMBED_BATCH_SIZE", 64),
		ChunkTargetTokens:  getenvInt("CHUNK_TARGET_TOKENS", 450),
		ChunkOverlapTokens: getenvInt("CHUNK_OVERLAP_TOKENS", 60),
		MaxUploadMB:        getenvInt("MAX_UPLOAD_MB", 15),

		RetrievalTopK:     getenvInt("RETRIEVAL_TOP_K", 5),
		RetrievalMinScore: getenvFloat("RETRIEVAL_MIN_SCORE", 0.5),

		LLMAnswerProvider:    getenv("LLM_ANSWER_PROVIDER", "openai_compatible"),
		LLMAnswerModel:       getenv("LLM_ANSWER_MODEL", "openai/gpt-oss-120b"),
		LLMAnswerBaseURL:     getenv("LLM_ANSWER_BASE_URL", "https://api.groq.com/openai/v1"),
		LLMAnswerMaxTokens:   getenvInt("LLM_ANSWER_MAX_TOKENS", 2000),
		SuggestMinConfidence: getenvFloat("SUGGEST_MIN_CONFIDENCE", 0.5),

		LLMSummaryProvider:  getenv("LLM_SUMMARY_PROVIDER", "openai_compatible"),
		LLMSummaryModel:     getenv("LLM_SUMMARY_MODEL", "openai/gpt-oss-120b"),
		LLMSummaryBaseURL:   getenv("LLM_SUMMARY_BASE_URL", "https://api.groq.com/openai/v1"),
		LLMSummaryMaxTokens: getenvInt("LLM_SUMMARY_MAX_TOKENS", 1500),

		LLMChatProvider:     getenv("LLM_CHAT_PROVIDER", "openai_compatible"),
		LLMChatModel:        getenv("LLM_CHAT_MODEL", "openai/gpt-oss-120b"),
		LLMChatBaseURL:      getenv("LLM_CHAT_BASE_URL", "https://api.groq.com/openai/v1"),
		LLMChatMaxTokens:    getenvInt("LLM_CHAT_MAX_TOKENS", 2500),
		ChatRecentSummaries: getenvInt("CHAT_RECENT_SUMMARIES", 2),
		ChatTopKSummaries:   getenvInt("CHAT_TOPK_SUMMARIES", 5),
		ChatTopKChunks:      getenvInt("CHAT_TOPK_CHUNKS", 5),
		ChatMaxTurns:        getenvInt("CHAT_MAX_TURNS", 12),
	}
	// AUTH_DISABLED is honored only in dev — never bypass auth in staging/prod.
	if c.Env != "dev" {
		c.AuthDisabled = false
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return c, nil
}

func (c *Config) validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.AuthSigningKey == "" && !c.AuthDisabled {
		missing = append(missing, "AUTH_SIGNING_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	switch c.Env {
	case "dev", "staging", "prod":
	default:
		return fmt.Errorf("invalid ENV %q (want dev|staging|prod)", c.Env)
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getenvInt reads an integer env var, returning fallback when unset or unparseable.
func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// getenvFloat reads a float env var, returning fallback when unset or unparseable.
func getenvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
