// Package retrieval turns a Suggest window into grounded, cited content (Stage 5).
// Extraction is its mandatory first step. See techdocs/query_extraction_techdoc.md.
package retrieval

import (
	"context"
	"fmt"
	"strings"

	"github.com/VanshNarang12/sales-agent/internal/detect"
	"github.com/VanshNarang12/sales-agent/internal/llm"
)

const extractSystemPrompt = `You read the tail of a live B2B sales call transcript. Lines are "speaker: text"; speakers are rep (the seller) and prospect (the buyer). The rep just pressed a help button.

Identify the one thing the rep most needs help answering right now: the prospect's most recent question, objection, or concern. Objections are often statements, not questions (e.g. "that's a lot more than what X quoted us").

Reply with a single short search query naming that ask in plain words, e.g.:
prospect objects that the price is higher than competitor X

No preamble, no quotes, no explanation. If the window contains no question, objection, or concern, reply exactly:
NONE`

const noAsk = "NONE"

type Extractor struct {
	model llm.Completer
}

func NewExtractor(model llm.Completer) *Extractor {
	return &Extractor{model: model}
}

// Extract distills the window into the ask retrieval should search for.
// ("", nil) means the window holds no ask — search nothing, show nothing.
func (e *Extractor) Extract(ctx context.Context, q detect.BuiltQuery) (string, error) {
	out, err := e.model.Complete(ctx, extractSystemPrompt, q.Query)
	if err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" || strings.EqualFold(out, noAsk) {
		return "", nil
	}
	return out, nil
}
