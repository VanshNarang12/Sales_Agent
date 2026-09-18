// Package suggest turns retrieved KB chunks into the rep-facing answer card
// (Stage 6). See techdocs/suggestion_generation_techdoc.md.
package suggest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/VanshNarang12/sales-agent/internal/llm"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
)

const maxBullets = 3

// Citation is the source of a card's content, display-ready (D5: mapped from
// model-returned chunk indexes, never model-written text).
type Citation struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
	Heading    string `json:"heading,omitempty"`
}

// Card is what the rep sees for one Suggest click.
type Card struct {
	Bullets    []string   `json:"bullets"`
	TalkTrack  string     `json:"talk_track"`
	Citations  []Citation `json:"citations"`
	Confidence float64    `json:"confidence"` // top retrieval hit's similarity (D4)
}

const answerSystemPrompt = `You write a glanceable answer card for a sales rep on a live call. You are given the prospect's ask and numbered source chunks from the rep's approved company documents.

Rules:
- Use ONLY the source chunks. Never use outside knowledge.
- If the chunks do not answer the ask, refuse.

Reply with ONLY this JSON, nothing else:
{"refuse": false, "bullets": ["..."], "talk_track": "...", "sources": [0]}

- refuse: true if the chunks do not answer the ask (then leave the other fields empty).
- bullets: 1-3 short points answering the ask, each under 15 words.
- talk_track: one natural sentence the rep can say out loud, word for word.
- sources: the numbers of every chunk you used.`

// modelReply is the strict JSON contract the model must return (D3).
type modelReply struct {
	Refuse    bool     `json:"refuse"`
	Bullets   []string `json:"bullets"`
	TalkTrack string   `json:"talk_track"`
	Sources   []int    `json:"sources"`
}

type Generator struct {
	model llm.Completer
}

func NewGenerator(model llm.Completer) *Generator {
	return &Generator{model: model}
}

// Generate makes the one answer LLM call: ask + chunks → card.
// (nil, nil) means refused or nothing to answer from — send no card, never a
// wrong card. The transcript window is deliberately not an input (D2).
func (g *Generator) Generate(ctx context.Context, ask string, hits []retrieval.SearchResult) (*Card, error) {
	if strings.TrimSpace(ask) == "" || len(hits) == 0 {
		return nil, nil
	}
	out, err := g.model.Complete(ctx, answerSystemPrompt, userPrompt(ask, hits))
	if err != nil {
		return nil, fmt.Errorf("generate: %w", err)
	}
	var reply modelReply
	if err := json.Unmarshal([]byte(stripToJSON(out)), &reply); err != nil {
		return nil, fmt.Errorf("generate: bad model JSON: %w", err)
	}
	if reply.Refuse || len(reply.Bullets) == 0 || strings.TrimSpace(reply.TalkTrack) == "" {
		return nil, nil
	}
	citations := mapCitations(reply.Sources, hits)
	if len(citations) == 0 {
		// A card we can't cite is a card we don't show (6.3 is mandatory).
		return nil, nil
	}
	if len(reply.Bullets) > maxBullets {
		reply.Bullets = reply.Bullets[:maxBullets]
	}
	return &Card{
		Bullets:    reply.Bullets,
		TalkTrack:  strings.TrimSpace(reply.TalkTrack),
		Citations:  citations,
		Confidence: hits[0].Score,
	}, nil
}

// userPrompt renders the ask plus the numbered chunks the model may cite.
func userPrompt(ask string, hits []retrieval.SearchResult) string {
	var b strings.Builder
	b.WriteString("Ask: ")
	b.WriteString(ask)
	b.WriteString("\n\nSource chunks:\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] %s", i, h.Title)
		if h.Heading != "" {
			fmt.Fprintf(&b, " > %s", h.Heading)
		}
		b.WriteString("\n")
		b.WriteString(h.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

// mapCitations resolves chunk indexes to citations, dropping out-of-range
// indexes and duplicate documents.
func mapCitations(sources []int, hits []retrieval.SearchResult) []Citation {
	var out []Citation
	seen := map[string]bool{}
	for _, i := range sources {
		if i < 0 || i >= len(hits) {
			continue
		}
		key := hits[i].DocumentID + "\x00" + hits[i].Heading
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Citation{DocumentID: hits[i].DocumentID, Title: hits[i].Title, Heading: hits[i].Heading})
	}
	return out
}

// stripToJSON tolerates models wrapping the JSON in code fences or prose:
// it returns the outermost {...} slice of the reply.
func stripToJSON(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return strings.TrimSpace(s)
}
