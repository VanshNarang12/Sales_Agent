package embed

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type Prefix string

const (
	PrefixDocument Prefix = "search_document: "
	PrefixQuery    Prefix = "search_query: "
)

type Embedder interface {
	Embed(ctx context.Context, prefix Prefix, texts []string) ([][]float32, error)
}

type Config struct {
	Provider  string
	Model     string
	APIKey    string
	BaseURL   string
	Dims      int
	BatchSize int
}

type Factory func(cfg Config) (Embedder, error)

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register makes a provider available by name; the one-line integration point.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = f
}

// New resolves cfg.Provider from the registry and builds the Embedder.
func New(cfg Config) (Embedder, error) {
	mu.RLock()
	f, ok := registry[cfg.Provider]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("embed: unknown provider %q (registered: %v)", cfg.Provider, Providers())
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
