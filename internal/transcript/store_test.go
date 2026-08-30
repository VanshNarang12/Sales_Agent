package transcript

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return New(rdb, 30*time.Minute), mr
}

func TestAppendAndWindow(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	entries := []Entry{
		{Speaker: "rep", Text: "hello there", StartMs: 0, EndMs: 1_000},
		{Speaker: "prospect", Text: "what is the pricing", StartMs: 5_000, EndMs: 6_000},
		{Speaker: "rep", Text: "let me check", StartMs: 100_000, EndMs: 101_000},
	}
	for _, e := range entries {
		if err := s.Append(ctx, "t1", "s1", e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	// Wide window: everything, oldest first, fields round-tripped.
	all, err := s.Window(ctx, "t1", "s1", 500_000)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 entries, got %d", len(all))
	}
	for i, e := range all {
		if e != entries[i] {
			t.Errorf("entry %d: want %+v, got %+v", i, entries[i], e)
		}
	}

	// 90s window from latest (101000): cutoff 11000 excludes the first two.
	recent, err := s.Window(ctx, "t1", "s1", 90_000)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if len(recent) != 1 || recent[0].Text != "let me check" {
		t.Fatalf("want only the last utterance, got %+v", recent)
	}
}

func TestWindowEmptySession(t *testing.T) {
	s, _ := newTestStore(t)
	got, err := s.Window(context.Background(), "t1", "missing", 90_000)
	if err != nil || got != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", got, err)
	}
}

func TestSlidingTTLRefreshedOnAppend(t *testing.T) {
	s, mr := newTestStore(t)
	ctx := context.Background()
	k := key("t1", "s1")

	if err := s.Append(ctx, "t1", "s1", Entry{Speaker: "rep", Text: "a", EndMs: 1}); err != nil {
		t.Fatal(err)
	}
	if ttl := mr.TTL(k); ttl != 30*time.Minute {
		t.Fatalf("want 30m TTL, got %v", ttl)
	}

	mr.FastForward(10 * time.Minute)
	if err := s.Append(ctx, "t1", "s1", Entry{Speaker: "rep", Text: "b", StartMs: 2, EndMs: 3}); err != nil {
		t.Fatal(err)
	}
	if ttl := mr.TTL(k); ttl != 30*time.Minute {
		t.Fatalf("TTL not refreshed by append: got %v", ttl)
	}

	// No writes → the countdown finally runs out and Redis deletes the key.
	mr.FastForward(31 * time.Minute)
	if mr.Exists(k) {
		t.Fatal("key should have expired after 30m of silence")
	}
}

func TestTenantKeySeparation(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	_ = s.Append(ctx, "tenantA", "s1", Entry{Speaker: "rep", Text: "secret A", EndMs: 1_000})
	_ = s.Append(ctx, "tenantB", "s1", Entry{Speaker: "rep", Text: "secret B", EndMs: 1_000})

	got, err := s.Window(ctx, "tenantA", "s1", 90_000)
	if err != nil || len(got) != 1 || got[0].Text != "secret A" {
		t.Fatalf("tenantA window leaked or missing: (%+v, %v)", got, err)
	}
}
