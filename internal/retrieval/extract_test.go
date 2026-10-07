package retrieval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/VanshNarang12/sales-agent/internal/detect"
)

type fakeCompleter struct {
	lastSystem string
	lastUser   string
	out        string
	err        error
}

func (f *fakeCompleter) Complete(_ context.Context, system, user string) (string, error) {
	f.lastSystem, f.lastUser = system, user
	return f.out, f.err
}

func query(text string) detect.BuiltQuery {
	return detect.BuiltQuery{TenantID: "t1", SessionID: "s1", Query: text}
}

func TestExtractReturnsAsk(t *testing.T) {
	fc := &fakeCompleter{out: "  prospect asks whether the product supports SAML SSO  "}
	asks, err := NewExtractor(fc).Extract(context.Background(), query("prospect: do you support single sign-on"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(asks) != 1 || asks[0] != "prospect asks whether the product supports SAML SSO" {
		t.Errorf("asks = %q, want one trimmed model output", asks)
	}
	if !strings.Contains(fc.lastUser, "single sign-on") {
		t.Errorf("window not passed to model: %q", fc.lastUser)
	}
	if !strings.Contains(fc.lastSystem, "NONE") {
		t.Errorf("system prompt missing NONE contract")
	}
}

func TestExtractMultipleAsks(t *testing.T) {
	fc := &fakeCompleter{out: "prospect asks about pricing plans\n\nprospect asks about Salesforce integration\n"}
	asks, err := NewExtractor(fc).Extract(context.Background(), query("prospect: pricing? and salesforce?"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := []string{"prospect asks about pricing plans", "prospect asks about Salesforce integration"}
	if len(asks) != 2 || asks[0] != want[0] || asks[1] != want[1] {
		t.Errorf("asks = %q, want %q", asks, want)
	}
}

func TestExtractCapsAsks(t *testing.T) {
	fc := &fakeCompleter{out: "a\nb\nc\nd\ne"}
	asks, err := NewExtractor(fc).Extract(context.Background(), query("many questions"))
	if err != nil || len(asks) != maxAsks {
		t.Errorf("want %d asks, got %q (err %v)", maxAsks, asks, err)
	}
}

func TestExtractNoneMeansNoAsk(t *testing.T) {
	for _, out := range []string{"NONE", "none", " None "} {
		fc := &fakeCompleter{out: out}
		asks, err := NewExtractor(fc).Extract(context.Background(), query("rep: how was your weekend"))
		if err != nil || len(asks) != 0 {
			t.Errorf("out %q: want no asks, got (%q, %v)", out, asks, err)
		}
	}
}

func TestExtractPropagatesModelError(t *testing.T) {
	fc := &fakeCompleter{err: errors.New("model down")}
	_, err := NewExtractor(fc).Extract(context.Background(), query("anything"))
	if err == nil {
		t.Fatal("want error when the model fails; a wrong query must never be fabricated")
	}
}
