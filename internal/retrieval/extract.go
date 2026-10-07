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

// maxAsks caps how many pending questions one Suggest click answers (multi-card,
// user decision 2026-10-06). Each ask costs one parallel generate call downstream.
const maxAsks = 3

// Prompt tuned 2026-10-06 on a real failing window (KB meta-question + echoed
// lines): the earlier "prospect's most recent ask" phrasing made the model answer
// NONE 2/3 times; naming doc/people questions as valid asks and calling out echo
// duplicates made it 4/4. Multi-ask: one line per pending question, newest first.
const extractSystemPrompt = `You read the tail of a live B2B sales call transcript. Lines are "speaker: text"; speakers are rep (the seller) and prospect (the buyer). Lines may be duplicated across speakers (audio echo) — treat duplicates as one utterance. The rep just pressed a help button.

Identify what the rep most needs help answering right now: the most recent unanswered questions, objections, or concerns raised in the conversation — usually one, at most 3, most recent first. Objections are often statements, not questions (e.g. "that's a lot more than what X quoted us"). Any direct question about the company, product, pricing, integrations, people, or the company's documents counts. Do not repeat the same ask twice.

Reply with one short search query per line naming each ask in plain words, e.g.:
prospect objects that the price is higher than competitor X

No preamble, no quotes, no numbering, no explanation. Only if the window contains no question, objection, or concern at all, reply exactly:
NONE`

const noAsk = "NONE"

type Extractor struct {
	model llm.Completer
}

func NewExtractor(model llm.Completer) *Extractor {
	return &Extractor{model: model}
}

// Extract distills the window into the asks retrieval should search for, newest
// first, capped at maxAsks. (nil, nil) means the window holds no ask — search
// nothing, show nothing.
func (e *Extractor) Extract(ctx context.Context, q detect.BuiltQuery) ([]string, error) {
	out, err := e.model.Complete(ctx, extractSystemPrompt, q.Query)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	var asks []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, noAsk) {
			continue
		}
		asks = append(asks, line)
		if len(asks) == maxAsks {
			break
		}
	}
	return asks, nil
}
