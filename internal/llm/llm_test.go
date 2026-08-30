package llm

import (
	"context"
	"testing"
)

type stubCompleter struct{ out string }

func (s stubCompleter) Complete(context.Context, string, string) (string, error) {
	return s.out, nil
}

func TestRegistryResolvesRegisteredProvider(t *testing.T) {
	Register("test_stub", func(cfg Config) (Completer, error) {
		return stubCompleter{out: cfg.Model}, nil
	})

	c, err := New(Config{Provider: "test_stub", Model: "m1"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	out, _ := c.Complete(context.Background(), "", "")
	if out != "m1" {
		t.Errorf("config not passed to factory: %q", out)
	}
}

func TestRegistryUnknownProvider(t *testing.T) {
	if _, err := New(Config{Provider: "nope"}); err == nil {
		t.Fatal("want error for unknown provider")
	}
}

func TestBuiltinProvidersRegistered(t *testing.T) {
	for _, name := range []string{"anthropic", "openai_compatible"} {
		found := false
		for _, p := range Providers() {
			if p == name {
				found = true
			}
		}
		if !found {
			t.Errorf("provider %q not self-registered", name)
		}
	}
}
