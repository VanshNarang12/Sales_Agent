package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func anthropicServer(t *testing.T, status int, body map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func newTestAnthropic(t *testing.T, url string) Completer {
	t.Helper()
	c, err := New(Config{Provider: "anthropic", Model: "claude-haiku-4-5", APIKey: "test", BaseURL: url, MaxTokens: 300})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func TestAnthropicCompleteRoundTrip(t *testing.T) {
	srv := anthropicServer(t, 200, map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-haiku-4-5",
		"content":     []map[string]any{{"type": "text", "text": "  the ask  "}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 3},
	})
	defer srv.Close()

	out, err := newTestAnthropic(t, srv.URL).Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if out != "the ask" {
		t.Errorf("out = %q, want trimmed text", out)
	}
}

func TestAnthropicCompleteRefusal(t *testing.T) {
	srv := anthropicServer(t, 200, map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-haiku-4-5",
		"content":     []map[string]any{},
		"stop_reason": "refusal",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 0},
	})
	defer srv.Close()

	if _, err := newTestAnthropic(t, srv.URL).Complete(context.Background(), "sys", "user"); err == nil {
		t.Fatal("want error on refusal")
	}
}

func TestAnthropicCompleteAPIError(t *testing.T) {
	srv := anthropicServer(t, 400, map[string]any{
		"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": "bad"},
	})
	defer srv.Close()

	if _, err := newTestAnthropic(t, srv.URL).Complete(context.Background(), "sys", "user"); err == nil {
		t.Fatal("want error on 400")
	}
}

func TestOpenAICompatibleRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": " the ask "}}},
		})
	}))
	defer srv.Close()

	c, err := New(Config{Provider: "openai_compatible", Model: "deepseek-chat", BaseURL: srv.URL, MaxTokens: 300})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	out, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if out != "the ask" {
		t.Errorf("out = %q", out)
	}
}
