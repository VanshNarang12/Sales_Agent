// Package tenancy carries the tenant (org) identity through every request, so that
// multi-tenant isolation is uniform across all services. The TenantID set here flows
// into context -> gRPC metadata / HTTP headers -> event payloads -> DB RLS session
// and cache keys. See techdocs/coding_standards_techdoc.md §4 and ARCHITECTURE.md §13.
package tenancy

import (
	"context"
	"errors"
)

// HeaderKey is the canonical header / gRPC-metadata key for the tenant id.
const HeaderKey = "x-tenant-id"

// TenantID is the org identifier. It is required on every customer-data operation.
type TenantID string

// ErrNoTenant is returned when a tenant-scoped operation runs without a tenant in context.
var ErrNoTenant = errors.New("tenancy: no tenant id in context")

type ctxKey struct{}

// With returns a copy of ctx carrying the given tenant id.
func With(ctx context.Context, id TenantID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From extracts the tenant id from ctx.
func From(ctx context.Context) (TenantID, bool) {
	id, ok := ctx.Value(ctxKey{}).(TenantID)
	return id, ok && id != ""
}

// MustFrom extracts the tenant id or returns ErrNoTenant. Use at the start of any
// tenant-scoped operation to fail closed rather than leak across tenants.
func MustFrom(ctx context.Context) (TenantID, error) {
	id, ok := From(ctx)
	if !ok {
		return "", ErrNoTenant
	}
	return id, nil
}

// CacheKey namespaces a cache key by tenant so entries can never collide across tenants.
func (t TenantID) CacheKey(parts ...string) string {
	key := "t:" + string(t)
	for _, p := range parts {
		key += ":" + p
	}
	return key
}
