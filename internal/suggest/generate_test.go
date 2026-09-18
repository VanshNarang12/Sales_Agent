package suggest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/VanshNarang12/sales-agent/internal/retrieval"
)

type fakeCompleter struct {
	lastSystem string
	lastUser   string
	out        string
	err        error
	calls      int
}

func (f *fakeCompleter) Complete(_ context.Context, system, user string) (string, error) {
	f.calls++
	f.lastSystem, f.lastUser = system, user
	return f.out, f.err
}

func testHits() []retrieval.SearchResult {
	return []retrieval.SearchResult{
		{DocumentID: "d1", Title: "pricing.md", Heading: "Discounts", Text: "Annual plans are 20% cheaper.", Score: 0.82},
		{DocumentID: "d2", Title: "security.md", Heading: "SSO", Text: "SAML SSO is on the Pro plan.", Score: 0.61},
	}
}

const goodReply = `{"refuse": false, "bullets": ["Annual plan is 20% cheaper"], "talk_track": "Our annual plan brings that down about 20%.", "sources": [0]}`

func TestGenerateReturnsCard(t *testing.T) {
	fc := &fakeCompleter{out: goodReply}
	card, err := NewGenerator(fc).Generate(context.Background(), "price too high", testHits())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if card == nil {
		t.Fatal("want a card")
	}
	if len(card.Bullets) != 1 || card.Bullets[0] != "Annual plan is 20% cheaper" {
		t.Errorf("bullets = %v", card.Bullets)
	}
	if card.TalkTrack != "Our annual plan brings that down about 20%." {
		t.Errorf("talk_track = %q", card.TalkTrack)
	}
	if len(card.Citations) != 1 || card.Citations[0] != (Citation{DocumentID: "d1", Title: "pricing.md", Heading: "Discounts"}) {
		t.Errorf("citations = %v, want index 0 mapped to pricing.md", card.Citations)
	}
	if card.Confidence != 0.82 {
		t.Errorf("confidence = %v, want top hit score", card.Confidence)
	}
	if !strings.Contains(fc.lastUser, "[0] pricing.md > Discounts") || !strings.Contains(fc.lastUser, "price too high") {
		t.Errorf("user prompt missing numbered chunks or ask: %q", fc.lastUser)
	}
	if !strings.Contains(fc.lastSystem, "refuse") {
		t.Errorf("system prompt missing refusal contract")
	}
}

func TestGenerateRefusalMeansNoCard(t *testing.T) {
	fc := &fakeCompleter{out: `{"refuse": true, "bullets": [], "talk_track": "", "sources": []}`}
	card, err := NewGenerator(fc).Generate(context.Background(), "unrelated ask", testHits())
	if err != nil || card != nil {
		t.Errorf("want (nil, nil) on refusal, got (%v, %v)", card, err)
	}
}

func TestGenerateBadJSONIsErrorNoCard(t *testing.T) {
	fc := &fakeCompleter{out: "Sure! Here are some bullets: - one - two"}
	card, err := NewGenerator(fc).Generate(context.Background(), "ask", testHits())
	if err == nil {
		t.Fatal("want error on unparseable model output; a wrong card must never be shown")
	}
	if card != nil {
		t.Errorf("card = %v, want nil", card)
	}
}

func TestGenerateToleratesCodeFences(t *testing.T) {
	fc := &fakeCompleter{out: "```json\n" + goodReply + "\n```"}
	card, err := NewGenerator(fc).Generate(context.Background(), "ask", testHits())
	if err != nil || card == nil {
		t.Fatalf("want card despite code fences, got (%v, %v)", card, err)
	}
}

func TestGenerateDropsOutOfRangeSources(t *testing.T) {
	fc := &fakeCompleter{out: `{"refuse": false, "bullets": ["b"], "talk_track": "t", "sources": [7, -1, 1, 1]}`}
	card, err := NewGenerator(fc).Generate(context.Background(), "ask", testHits())
	if err != nil || card == nil {
		t.Fatalf("generate: (%v, %v)", card, err)
	}
	if len(card.Citations) != 1 || card.Citations[0].DocumentID != "d2" {
		t.Errorf("citations = %v, want only the valid, deduped index 1", card.Citations)
	}
}

func TestGenerateNoValidCitationsMeansNoCard(t *testing.T) {
	fc := &fakeCompleter{out: `{"refuse": false, "bullets": ["b"], "talk_track": "t", "sources": [9]}`}
	card, err := NewGenerator(fc).Generate(context.Background(), "ask", testHits())
	if err != nil || card != nil {
		t.Errorf("want (nil, nil) when no source survives, got (%v, %v)", card, err)
	}
}

func TestGenerateEmptyInputsSkipModelCall(t *testing.T) {
	fc := &fakeCompleter{out: goodReply}
	g := NewGenerator(fc)
	if card, err := g.Generate(context.Background(), "", testHits()); card != nil || err != nil {
		t.Errorf("empty ask: want (nil, nil), got (%v, %v)", card, err)
	}
	if card, err := g.Generate(context.Background(), "ask", nil); card != nil || err != nil {
		t.Errorf("no hits: want (nil, nil), got (%v, %v)", card, err)
	}
	if fc.calls != 0 {
		t.Errorf("model called %d times, want 0", fc.calls)
	}
}

func TestGenerateCapsBullets(t *testing.T) {
	fc := &fakeCompleter{out: `{"refuse": false, "bullets": ["1","2","3","4","5"], "talk_track": "t", "sources": [0]}`}
	card, err := NewGenerator(fc).Generate(context.Background(), "ask", testHits())
	if err != nil || card == nil {
		t.Fatalf("generate: (%v, %v)", card, err)
	}
	if len(card.Bullets) != maxBullets {
		t.Errorf("bullets = %d, want capped at %d", len(card.Bullets), maxBullets)
	}
}

func TestGeneratePropagatesModelError(t *testing.T) {
	fc := &fakeCompleter{err: errors.New("model down")}
	card, err := NewGenerator(fc).Generate(context.Background(), "ask", testHits())
	if err == nil {
		t.Fatal("want error when the model fails")
	}
	if card != nil {
		t.Errorf("card = %v, want nil on error", card)
	}
}
