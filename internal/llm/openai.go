package llm

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

// One adapter covers every vendor speaking the OpenAI chat-completions API:
// OpenAI, DeepSeek, Groq, Together, and self-hosted Ollama/vLLM — pick via BaseURL.
func init() {
	Register("openai_compatible", newOpenAICompatible)
}

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

type openAIClient struct {
	http    *http.Client
	baseURL string
	apiKey  string
	model   string
	maxTok  int64
}

func newOpenAICompatible(cfg Config) (Completer, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("openai_compatible: missing model")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	return &openAIClient{
		http:    &http.Client{Timeout: 60 * time.Second},
		baseURL: strings.TrimRight(base, "/"),
		apiKey:  cfg.APIKey, // may be empty for local servers (Ollama/vLLM)
		model:   cfg.Model,
		maxTok:  cfg.MaxTokens,
	}, nil
}

type oaiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int64        `json:"max_tokens,omitempty"`
	Messages  []oaiMessage `json:"messages"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiResponse struct {
	Choices []struct {
		Message oaiMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *openAIClient) Complete(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(oaiRequest{
		Model:     c.model,
		MaxTokens: c.maxTok,
		Messages: []oaiMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", fmt.Errorf("openai complete: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("openai complete: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai complete: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("openai complete: read body: %w", err)
	}

	var out oaiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("openai complete: status %d: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(raw)
		if out.Error != nil {
			msg = out.Error.Message
		}
		return "", fmt.Errorf("openai complete: status %d: %s", resp.StatusCode, msg)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai complete: empty choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
