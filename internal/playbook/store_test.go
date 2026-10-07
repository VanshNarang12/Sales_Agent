package playbook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/VanshNarang12/sales-agent/internal/platform/tenancy"
)

func TestPromptBlockEmpty(t *testing.T) {
	if got := (Playbook{}).PromptBlock(); got != "" {
		t.Errorf("empty playbook block = %q, want empty", got)
	}
}

func TestPromptBlockFormat(t *testing.T) {
	p := Playbook{
		Guidance: "Always mention the 30-day pilot when pricing comes up.",
		Rules: []Rule{
			{Phrase: "guaranteed uptime", Reason: "legal hasn't approved SLA language"},
			{Phrase: "free onboarding"},
		},
	}
	got := p.PromptBlock()
	for _, want := range []string{
		"Org playbook — follow strictly:",
		"30-day pilot",
		`"guaranteed uptime" (legal hasn't approved SLA language)`,
		`"free onboarding"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q:\n%s", want, got)
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("block should not end with a newline")
	}
}

func TestPromptBlockRulesOnly(t *testing.T) {
	p := Playbook{Rules: []Rule{{Phrase: "x"}}}
	if got := p.PromptBlock(); !strings.Contains(got, "NEVER") {
		t.Errorf("rules-only block = %q", got)
	}
}

// fakeCache is an in-memory Cache. SetNX always refuses the lock so no
// background refresh goroutine runs (there is no DB in these tests).
type fakeCache struct {
	m    map[string]string
	nxOK bool
}

func newFakeCache() *fakeCache { return &fakeCache{m: map[string]string{}} }

func (c *fakeCache) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := c.m[key]
	return v, ok, nil
}
func (c *fakeCache) Set(_ context.Context, key, val string, _ time.Duration) error {
	c.m[key] = val
	return nil
}
func (c *fakeCache) SetNX(_ context.Context, key, val string, _ time.Duration) (bool, error) {
	if !c.nxOK {
		return false, nil
	}
	c.m[key] = val
	return true, nil
}
func (c *fakeCache) Del(_ context.Context, key string) error {
	delete(c.m, key)
	return nil
}

func seed(t *testing.T, c *fakeCache, tid string, pb Playbook, fetchedAt time.Time) {
	t.Helper()
	raw, err := json.Marshal(envelope{FetchedAt: fetchedAt, Playbook: pb})
	if err != nil {
		t.Fatal(err)
	}
	c.m[cacheKey(tid)] = string(raw)
}

func TestGetServesFreshCacheWithoutDB(t *testing.T) {
	c := newFakeCache()
	seed(t, c, "t1", Playbook{Guidance: "g"}, time.Now())
	s := NewStore(nil, c) // nil pool: any DB touch would panic — proves cache-only
	ctx := tenancy.With(context.Background(), tenancy.TenantID("t1"))
	got, err := s.Get(ctx)
	if err != nil || got.Guidance != "g" {
		t.Fatalf("Get = %+v, %v; want cached playbook", got, err)
	}
}

func TestGetServesStaleCacheWithoutBlocking(t *testing.T) {
	c := newFakeCache() // nxOK=false: refresh goroutine never acquires the lock
	seed(t, c, "t1", Playbook{Guidance: "old"}, time.Now().Add(-2*softTTL))
	s := NewStore(nil, c)
	ctx := tenancy.With(context.Background(), tenancy.TenantID("t1"))
	got, err := s.Get(ctx)
	if err != nil || got.Guidance != "old" {
		t.Fatalf("Get = %+v, %v; want stale entry served instantly", got, err)
	}
}

func TestGetTenantIsolation(t *testing.T) {
	c := newFakeCache()
	seed(t, c, "t1", Playbook{Guidance: "g"}, time.Now())
	if _, ok := c.m[cacheKey("t2")]; ok {
		t.Fatal("t2 must have no entry")
	}
	if cacheKey("t1") == cacheKey("t2") {
		t.Fatal("cache keys must differ per tenant")
	}
}

func TestPutRoundTrips(t *testing.T) {
	c := newFakeCache()
	s := NewStore(nil, c)
	pb := Playbook{Guidance: "g", Rules: []Rule{{Phrase: "x"}}}
	s.put(context.Background(), "t1", pb)
	raw, ok := c.m[cacheKey("t1")]
	if !ok {
		t.Fatal("put stored nothing")
	}
	var e envelope
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	if e.Playbook.Guidance != "g" || len(e.Playbook.Rules) != 1 || e.FetchedAt.IsZero() {
		t.Errorf("round trip = %+v", e)
	}
}
