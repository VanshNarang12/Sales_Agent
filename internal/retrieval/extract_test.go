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
	ask, err := NewExtractor(fc).Extract(context.Background(), query("prospect: do you support single sign-on"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if ask != "prospect asks whether the product supports SAML SSO" {
		t.Errorf("ask = %q, want trimmed model output", ask)
	}
	if !strings.Contains(fc.lastUser, "single sign-on") {
		t.Errorf("window not passed to model: %q", fc.lastUser)
	}
	if !strings.Contains(fc.lastSystem, "NONE") {
		t.Errorf("system prompt missing NONE contract")
	}
}

func TestExtractNoneMeansNoAsk(t *testing.T) {
	for _, out := range []string{"NONE", "none", " None "} {
		fc := &fakeCompleter{out: out}
		ask, err := NewExtractor(fc).Extract(context.Background(), query("rep: how was your weekend"))
		if err != nil || ask != "" {
			t.Errorf("out %q: want (\"\", nil), got (%q, %v)", out, ask, err)
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
