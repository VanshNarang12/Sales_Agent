package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// One adapter covers every vendor speaking the OpenAI embeddings API:
// Groq (our default, serving nomic-embed-text-v1.5), OpenAI, Gemini, Ollama, vLLM.
func init() {
	Register("openai_compatible", newOpenAICompatible)
}

const (
	defaultEmbedBaseURL   = "https://api.openai.com/v1"
	defaultEmbedBatchSize = 64
)

type openAIEmbedder struct {
	http    *http.Client
	baseURL string
	apiKey  string
	model   string
	dims    int
	batch   int
}

func newOpenAICompatible(cfg Config) (Embedder, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("embed openai_compatible: missing model")
	}
	if cfg.Dims <= 0 {
		return nil, fmt.Errorf("embed openai_compatible: missing dims")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultEmbedBaseURL
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = defaultEmbedBatchSize
	}
	return &openAIEmbedder{
		http:    &http.Client{Timeout: 60 * time.Second},
		baseURL: strings.TrimRight(base, "/"),
		apiKey:  cfg.APIKey, // may be empty for local servers (Ollama/vLLM)
		model:   cfg.Model,
		dims:    cfg.Dims,
		batch:   batch,
	}, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (e *openAIEmbedder) Embed(ctx context.Context, prefix Prefix, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = string(prefix) + t
	}

	out := make([][]float32, 0, len(prefixed))
	for start := 0; start < len(prefixed); start += e.batch {
		end := min(start+e.batch, len(prefixed))
		vecs, err := e.embedBatch(ctx, prefixed[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (e *openAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("embed: read body: %w", err)
	}

	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("embed: status %d: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(raw)
		if parsed.Error != nil {
			msg = parsed.Error.Message
		}
		return nil, fmt.Errorf("embed: status %d: %s", resp.StatusCode, msg)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embed: sent %d texts, got %d vectors", len(texts), len(parsed.Data))
	}

	// The API may return items out of order; place by index.
	vecs := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return nil, fmt.Errorf("embed: vector index %d out of range", d.Index)
		}
		if len(d.Embedding) != e.dims {
			return nil, fmt.Errorf("embed: got %d dims, want %d — wrong EMBED_MODEL/EMBED_DIMS config?", len(d.Embedding), e.dims)
		}
		vecs[d.Index] = d.Embedding
	}
	for i, v := range vecs {
		if v == nil {
			return nil, fmt.Errorf("embed: missing vector for text %d", i)
		}
	}
	return vecs, nil
}
