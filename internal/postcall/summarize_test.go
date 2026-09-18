package postcall

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/VanshNarang12/sales-agent/internal/transcript"
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

func testEntries() []transcript.Entry {
	return []transcript.Entry{
		{Speaker: "rep", Text: "Thanks for joining, what are you evaluating?", StartMs: 0, EndMs: 2000},
		{Speaker: "prospect", Text: "We need SSO. Does it support Okta?", StartMs: 2100, EndMs: 5000},
		{Speaker: "rep", Text: "Let me get back to you on Okta specifically.", StartMs: 5100, EndMs: 8000},
	}
}

const goodReply = `{"summary": "Prospect is evaluating the product and needs SSO; the rep will follow up on Okta support.", "action_items": ["Follow up on Okta SSO support"], "unanswered": ["Does it support Okta?"]}`

func TestSummarizeReturnsSummary(t *testing.T) {
	fc := &fakeCompleter{out: goodReply}
	sum, err := NewSummarizer(fc).Summarize(context.Background(), testEntries())
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if sum == nil {
		t.Fatal("want a summary")
	}
	if !strings.Contains(sum.Summary, "Okta") {
		t.Errorf("summary = %q", sum.Summary)
	}
	if len(sum.ActionItems) != 1 || sum.ActionItems[0] != "Follow up on Okta SSO support" {
		t.Errorf("action_items = %v", sum.ActionItems)
	}
	if len(sum.Unanswered) != 1 {
		t.Errorf("unanswered = %v", sum.Unanswered)
	}
}

func TestSummarizeSendsSpeakerLabeledTranscript(t *testing.T) {
	fc := &fakeCompleter{out: goodReply}
	if _, err := NewSummarizer(fc).Summarize(context.Background(), testEntries()); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if !strings.Contains(fc.lastUser, "prospect: We need SSO. Does it support Okta?") {
		t.Errorf("user prompt missing labeled utterance:\n%s", fc.lastUser)
	}
}

func TestSummarizeEmptyTranscriptSkipsLLM(t *testing.T) {
	fc := &fakeCompleter{out: goodReply}
	sum, err := NewSummarizer(fc).Summarize(context.Background(), nil)
	if err != nil || sum != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", sum, err)
	}
	if fc.calls != 0 {
		t.Errorf("LLM called %d times on empty transcript", fc.calls)
	}
}

func TestSummarizeBadJSONIsError(t *testing.T) {
	fc := &fakeCompleter{out: "I could not summarize this call."}
	sum, err := NewSummarizer(fc).Summarize(context.Background(), testEntries())
	if err == nil || sum != nil {
		t.Fatalf("want error on bad JSON, got (%v, %v)", sum, err)
	}
}

func TestSummarizeEmptySummaryFieldIsError(t *testing.T) {
	fc := &fakeCompleter{out: `{"summary": "  ", "action_items": [], "unanswered": []}`}
	sum, err := NewSummarizer(fc).Summarize(context.Background(), testEntries())
	if err == nil || sum != nil {
		t.Fatalf("want error on empty summary, got (%v, %v)", sum, err)
	}
}

func TestSummarizeLLMErrorPropagates(t *testing.T) {
	fc := &fakeCompleter{err: errors.New("provider down")}
	sum, err := NewSummarizer(fc).Summarize(context.Background(), testEntries())
	if err == nil || sum != nil {
		t.Fatalf("want error, got (%v, %v)", sum, err)
	}
}

func TestSummarizeNilListsBecomeEmpty(t *testing.T) {
	fc := &fakeCompleter{out: `{"summary": "Short call, no next steps."}`}
	sum, err := NewSummarizer(fc).Summarize(context.Background(), testEntries())
	if err != nil || sum == nil {
		t.Fatalf("summarize: (%v, %v)", sum, err)
	}
	if sum.ActionItems == nil || sum.Unanswered == nil {
		t.Errorf("lists must be empty, not nil: %v %v", sum.ActionItems, sum.Unanswered)
	}
}

func TestSummarizeToleratesCodeFences(t *testing.T) {
	fc := &fakeCompleter{out: "```json\n" + goodReply + "\n```"}
	sum, err := NewSummarizer(fc).Summarize(context.Background(), testEntries())
	if err != nil || sum == nil {
		t.Fatalf("summarize: (%v, %v)", sum, err)
	}
}
