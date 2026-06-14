// Package secrets abstracts secret retrieval. Locally it is env-backed; in cloud it
// is backed by a KMS-backed secrets manager. Secrets are referenced by name only —
// never hardcoded, logged, or committed (coding_standards_techdoc.md §8).
package secrets

import (
	"context"
	"fmt"
	"os"
)

// Store retrieves named secrets. Implementations must support rotation (callers
// fetch on use rather than caching indefinitely).
type Store interface {
	Get(ctx context.Context, name string) (string, error)
}

// EnvStore is the local/dev implementation backed by environment variables.
type EnvStore struct{}

// Get returns the secret value for name from the environment.
func (EnvStore) Get(_ context.Context, name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("secrets: %q not set", name)
	}
	return v, nil
}

// TODO(stage-24): add an AWS Secrets Manager / KMS implementation for cloud envs.
