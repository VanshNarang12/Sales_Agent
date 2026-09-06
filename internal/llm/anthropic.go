package llm

import (
	"context"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"strings"
)

func init() {
	Register("anthropic", newAnthropic)
}

// anthropicClient adapts the official Anthropic SDK to Completer.
type anthropicClient struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
}

func newAnthropic(cfg Config) (Completer, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropic: missing API key")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("anthropic: missing model")
	}
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &anthropicClient{
		client:    anthropic.NewClient(opts...),
		model:     anthropic.Model(cfg.Model),
		maxTokens: cfg.MaxTokens,
	}, nil
}

func (a *anthropicClient) Complete(ctx context.Context, system, user string) (string, error) {
	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: a.maxTokens,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic complete: %w", err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", fmt.Errorf("anthropic complete: model refused")
	}
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			return strings.TrimSpace(t.Text), nil
		}
	}
	return "", fmt.Errorf("anthropic complete: no text block in response")
}
