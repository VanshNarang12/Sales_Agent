package customer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
)

type fakeChatLLM struct {
	lastSystem string
	lastUser   string
	out        string
	err        error
}

func (f *fakeChatLLM) Complete(_ context.Context, system, user string) (string, error) {
	f.lastSystem, f.lastUser = system, user
	return f.out, f.err
}

type fakeEmbedder struct {
	calls int
	err   error
}

func (f *fakeEmbedder) Embed(_ context.Context, _ embed.Prefix, texts []string) ([][]float32, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

type fakeReader struct {
	recent   []SummaryDetail
	searched []SummaryDetail
}

func (f *fakeReader) RecentSummaries(_ context.Context, _ string, _ int) ([]SummaryDetail, error) {
	return f.recent, nil
}
func (f *fakeReader) SearchSummaries(_ context.Context, _ string, _ []float32, _ int) ([]SummaryDetail, error) {
	return f.searched, nil
}

type fakeKB struct {
	hits []retrieval.SearchResult
}

func (f *fakeKB) SearchVec(_ context.Context, _ []float32, _ int) ([]retrieval.SearchResult, error) {
	return f.hits, nil
}

func digest(id, text string) SummaryDetail {
	return SummaryDetail{ID: id, Summary: text, CreatedAt: time.Now(),
		ActionItems: []string{"follow up"}, Unanswered: []string{}}
}

func testChat(llm *fakeChatLLM, r *fakeReader, kb *fakeKB) *Chat {
	return NewChat(llm, &fakeEmbedder{}, r, kb,
		ChatConfig{RecentSummaries: 2, TopKSummaries: 5, TopKChunks: 5, MaxTurns: 4})
}

func TestChatGroundsInDigestsAndChunks(t *testing.T) {
	fl := &fakeChatLLM{out: "They pushed back on pricing last time; open with the annual discount."}
	r := &fakeReader{
		recent:   []SummaryDetail{digest("a", "Latest: pricing objection raised")},
		searched: []SummaryDetail{digest("b", "Older: asked about Okta SSO")},
	}
	kb := &fakeKB{hits: []retrieval.SearchResult{
		{DocumentID: "d1", Title: "pricing.md", Heading: "Discounts", Text: "Annual is 20% off", Score: 0.8},
		{DocumentID: "d1", Title: "pricing.md", Heading: "Terms", Text: "Net-30", Score: 0.6},
	}}
	reply, err := testChat(fl, r, kb).Answer(context.Background(), "cust", "how do I prep?", nil)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	for _, want := range []string{"pricing objection raised", "asked about Okta SSO", "Annual is 20% off", "how do I prep?"} {
		if !strings.Contains(fl.lastUser, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if len(reply.Sources) != 1 || reply.Sources[0] != "pricing.md" {
		t.Errorf("sources = %v, want deduped [pricing.md]", reply.Sources)
	}
}

func TestChatDedupesRecentAndSearched(t *testing.T) {
	fl := &fakeChatLLM{out: "ok"}
	same := digest("a", "Shared digest")
	r := &fakeReader{recent: []SummaryDetail{same}, searched: []SummaryDetail{same, digest("b", "Unique digest")}}
	if _, err := testChat(fl, r, &fakeKB{}).Answer(context.Background(), "cust", "hi", nil); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if strings.Count(fl.lastUser, "Shared digest") != 1 {
		t.Errorf("duplicate digest sent twice:\n%s", fl.lastUser)
	}
	if !strings.Contains(fl.lastUser, "Unique digest") {
		t.Error("searched-only digest missing")
	}
}

func TestChatCapsHistory(t *testing.T) {
	fl := &fakeChatLLM{out: "ok"}
	history := []Message{
		{Role: "user", Text: "turn-1"}, {Role: "assistant", Text: "turn-2"},
		{Role: "user", Text: "turn-3"}, {Role: "assistant", Text: "turn-4"},
		{Role: "user", Text: "turn-5"}, {Role: "assistant", Text: "turn-6"},
	}
	if _, err := testChat(fl, &fakeReader{}, &fakeKB{}).Answer(context.Background(), "cust", "next?", history); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if strings.Contains(fl.lastUser, "turn-1") || strings.Contains(fl.lastUser, "turn-2") {
		t.Error("history not capped to MaxTurns=4")
	}
	if !strings.Contains(fl.lastUser, "turn-6") {
		t.Error("newest history turn missing")
	}
}

func TestChatEmptyMessageRejected(t *testing.T) {
	if _, err := testChat(&fakeChatLLM{out: "ok"}, &fakeReader{}, &fakeKB{}).Answer(context.Background(), "cust", "  ", nil); err == nil {
		t.Fatal("want error on empty message")
	}
}

func TestChatNoMeetingsStillAnswers(t *testing.T) {
	fl := &fakeChatLLM{out: "No meetings on record; here's what the docs say."}
	reply, err := testChat(fl, &fakeReader{}, &fakeKB{}).Answer(context.Background(), "cust", "prep me", nil)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if !strings.Contains(fl.lastUser, "(none recorded yet)") {
		t.Error("prompt should state no meetings recorded")
	}
	if reply.Answer == "" {
		t.Error("empty answer")
	}
}

func TestChatLLMErrorPropagates(t *testing.T) {
	fl := &fakeChatLLM{err: errors.New("provider down")}
	if _, err := testChat(fl, &fakeReader{}, &fakeKB{}).Answer(context.Background(), "cust", "hi", nil); err == nil {
		t.Fatal("want error")
	}
}
