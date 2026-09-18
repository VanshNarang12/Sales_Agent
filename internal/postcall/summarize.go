// Package postcall turns a finished call's transcript into the after-call digest:
// summary, action items, unanswered questions (Stage 10).
// See techdocs/post_call_techdoc.md.
package postcall

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/VanshNarang12/sales-agent/internal/llm"
	"github.com/VanshNarang12/sales-agent/internal/transcript"
)

// Summary is the digest of one customer call — the only call data that outlives
// the transcript's Redis TTL.
type Summary struct {
	Summary     string   `json:"summary"`
	ActionItems []string `json:"action_items"`
	Unanswered  []string `json:"unanswered"`
}

const summarySystemPrompt = `You are a senior Vice President in my company and now I am sharing you a transcript of a sales Pitch summarize the finished sales call for the rep's team. transcript is labeled by speaker (rep = our salesperson, prospect = the customer).

Reply with ONLY this JSON, nothing else:
{"summary": "...", "action_items": ["..."], "unanswered": ["..."]}

- summary: Make it a to the point summary but use the language that is easier to understand What the call was about, the prospect's situation and concerns, objections raised, and where things stand and what were the customers insights about their requirement and our product.
- action_items: concrete next steps that were agreed or clearly implied. Empty list if none.
- unanswered: questions the prospect asked that the rep could not answer or promised to follow up on. Empty list if none.
- Use only what is in the transcript. Never invent names, numbers, or commitments.`

type Summarizer struct {
	model llm.Completer
}

func NewSummarizer(model llm.Completer) *Summarizer {
	return &Summarizer{model: model}
}

func (s *Summarizer) Summarize(ctx context.Context, entries []transcript.Entry) (*Summary, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out, err := s.model.Complete(ctx, summarySystemPrompt, renderTranscript(entries))
	if err != nil {
		return nil, fmt.Errorf("summarize: %w", err)
	}
	var sum Summary
	if err := json.Unmarshal([]byte(stripToJSON(out)), &sum); err != nil {
		return nil, fmt.Errorf("summarize: bad model JSON: %w", err)
	}
	if strings.TrimSpace(sum.Summary) == "" {
		return nil, fmt.Errorf("summarize: model returned an empty summary")
	}
	if sum.ActionItems == nil {
		sum.ActionItems = []string{}
	}
	if sum.Unanswered == nil {
		sum.Unanswered = []string{}
	}
	return &sum, nil
}

func renderTranscript(entries []transcript.Entry) string {
	var b strings.Builder
	b.WriteString("Transcript:\n")
	for _, e := range entries {
		b.WriteString(e.Speaker)
		b.WriteString(": ")
		b.WriteString(e.Text)
		b.WriteString("\n")
	}
	return b.String()
}


func stripToJSON(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return strings.TrimSpace(s)
}
