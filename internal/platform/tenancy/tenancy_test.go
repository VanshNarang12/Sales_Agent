package tenancy

import (
	"context"
	"errors"
	"testing"
)

func TestWithAndFrom(t *testing.T) {
	ctx := With(context.Background(), "org-123")
	got, ok := From(ctx)
	if !ok || got != "org-123" {
		t.Fatalf("From() = %q, %v; want org-123, true", got, ok)
	}
}

func TestFromEmpty(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Fatal("From() on empty context should report not-ok")
	}
}

func TestMustFromFailsClosed(t *testing.T) {
	if _, err := MustFrom(context.Background()); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("MustFrom() err = %v; want ErrNoTenant", err)
	}
}

func TestCacheKeyIsTenantScoped(t *testing.T) {
	a := TenantID("org-a").CacheKey("card", "obj-1")
	b := TenantID("org-b").CacheKey("card", "obj-1")
	if a == b {
		t.Fatalf("cache keys must differ across tenants: %q == %q", a, b)
	}
}
