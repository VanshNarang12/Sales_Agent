// Package config loads and validates service configuration from the environment.
// All configuration is read once at boot and validated; services never read os.Getenv
// directly elsewhere (see techdocs/coding_standards_techdoc.md §8).
package config

import (
	"fmt"
	"os"
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
