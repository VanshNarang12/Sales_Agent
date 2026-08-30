// Package llm is the pluggable model layer: callers depend on Completer only;
// providers self-register by name. See techdocs/query_extraction_techdoc.md §3.
package llm

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Completer is one text-in/text-out model call (Strategy).
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Config is everything a provider factory needs to build a Completer.
type Config struct {
	Provider  string
	Model     string
	APIKey    string
	BaseURL   string // optional endpoint override (self-hosted / OpenAI-compatible vendors)
	MaxTokens int64
}

// Factory builds a provider's Completer from config.
type Factory func(cfg Config) (Completer, error)

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register makes a provider available by name; the one-line integration point for a new provider.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = f
}

// New resolves cfg.Provider from the registry and builds the Completer.
func New(cfg Config) (Completer, error) {
	mu.RLock()
	f, ok := registry[cfg.Provider]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("llm: unknown provider %q (registered: %v)", cfg.Provider, Providers())
	}
	return f(cfg)
}

// Providers lists registered provider names (sorted, for error messages/logs).
func Providers() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
