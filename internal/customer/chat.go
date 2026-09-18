package customer

import (
	"context"
	"fmt"
	"strings"

	"github.com/VanshNarang12/sales-agent/internal/embed"
	"github.com/VanshNarang12/sales-agent/internal/llm"
	"github.com/VanshNarang12/sales-agent/internal/retrieval"
)

type Message struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

type Reply struct {
	Answer  string   `json:"answer"`
	Sources []string `json:"sources"` // doc titles the KB contributed
}

// ChatConfig: every context knob is env-driven (see customer_memory_techdoc §9).
type ChatConfig struct {
	RecentSummaries int // newest digests always included
	TopKSummaries   int // semantically retrieved digests
	TopKChunks      int // retrieved KB chunks
	MaxTurns        int // history cap
}

type summaryReader interface {
	RecentSummaries(ctx context.Context, customerID string, n int) ([]SummaryDetail, error)
	SearchSummaries(ctx context.Context, customerID string, queryVec []float32, topK int) ([]SummaryDetail, error)
}

type chunkSearcher interface {
	SearchVec(ctx context.Context, vec []float32, topK int) ([]retrieval.SearchResult, error)
}

type Chat struct {
	model    llm.Completer
	embedder embed.Embedder
	store    summaryReader
	kb       chunkSearcher
	cfg      ChatConfig
}

func NewChat(model llm.Completer, embedder embed.Embedder, store summaryReader, kb chunkSearcher, cfg ChatConfig) *Chat {
	return &Chat{model: model, embedder: embedder, store: store, kb: kb, cfg: cfg}
}

const chatSystemPrompt = `You are a sales copilot helping a rep prepare for their next meeting with a specific customer. You are given notes from past meetings with this customer and excerpts from the company's own documents.

Rules:
- Answer only from the meeting notes and document excerpts. If they don't cover something, say so plainly.
- Conversational style: natural prose, like a sharp colleague talking the rep through it. No rigid bullet-card format unless the rep asks for a list.
- Be specific: reference what actually happened in past meetings and what the docs actually say.`

// Answer runs one chat turn: embed the message once, gather grounding
// (recent digests + searched digests + KB chunks), one LLM call.
func (c *Chat) Answer(ctx context.Context, customerID, message string, history []Message) (Reply, error) {
	if strings.TrimSpace(message) == "" {
		return Reply{}, fmt.Errorf("chat: empty message")
	}
	vecs, err := c.embedder.Embed(ctx, embed.PrefixQuery, []string{message})
	if err != nil {
		return Reply{}, fmt.Errorf("chat embed: %w", err)
	}
	vec := vecs[0]

	recent, err := c.store.RecentSummaries(ctx, customerID, c.cfg.RecentSummaries)
	if err != nil {
		return Reply{}, err
	}
	searched, err := c.store.SearchSummaries(ctx, customerID, vec, c.cfg.TopKSummaries)
	if err != nil {
		return Reply{}, err
	}
	chunks, err := c.kb.SearchVec(ctx, vec, c.cfg.TopKChunks)
	if err != nil {
		return Reply{}, fmt.Errorf("chat kb search: %w", err)
	}

	digests := dedupeSummaries(recent, searched)
	out, err := c.model.Complete(ctx, chatSystemPrompt, chatUserPrompt(digests, chunks, capTurns(history, c.cfg.MaxTurns), message))
	if err != nil {
		return Reply{}, fmt.Errorf("chat llm: %w", err)
	}

	sources := make([]string, 0, len(chunks))
	seen := map[string]bool{}
	for _, ch := range chunks {
		if !seen[ch.Title] {
			seen[ch.Title] = true
			sources = append(sources, ch.Title)
		}
	}
	return Reply{Answer: strings.TrimSpace(out), Sources: sources}, nil
}

// dedupeSummaries: recent first (guaranteed), then searched ones not already there.
func dedupeSummaries(recent, searched []SummaryDetail) []SummaryDetail {
	seen := map[string]bool{}
	out := make([]SummaryDetail, 0, len(recent)+len(searched))
	for _, s := range recent {
		seen[s.ID] = true
		out = append(out, s)
	}
	for _, s := range searched {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	return out
}

func capTurns(history []Message, max int) []Message {
	if max > 0 && len(history) > max {
		return history[len(history)-max:]
	}
	return history
}

// chatUserPrompt: static grounding first (cache-friendly), history and the new
// message last.
func chatUserPrompt(digests []SummaryDetail, chunks []retrieval.SearchResult, history []Message, message string) string {
	var b strings.Builder
	b.WriteString("Past meetings with this customer (newest first):\n")
	if len(digests) == 0 {
		b.WriteString("(none recorded yet)\n")
	}
	for _, d := range digests {
		fmt.Fprintf(&b, "\n[%s]\n%s\n", d.CreatedAt.Format("2006-01-02"), d.Summary)
		if len(d.ActionItems) > 0 {
			fmt.Fprintf(&b, "Action items: %s\n", strings.Join(d.ActionItems, "; "))
		}
		if len(d.Unanswered) > 0 {
			fmt.Fprintf(&b, "Unanswered: %s\n", strings.Join(d.Unanswered, "; "))
		}
	}
	b.WriteString("\nCompany document excerpts:\n")
	if len(chunks) == 0 {
		b.WriteString("(nothing relevant found)\n")
	}
	for _, ch := range chunks {
		fmt.Fprintf(&b, "\n[%s", ch.Title)
		if ch.Heading != "" {
			fmt.Fprintf(&b, " > %s", ch.Heading)
		}
		b.WriteString("]\n")
		b.WriteString(ch.Text)
		b.WriteString("\n")
	}
	b.WriteString("\nConversation so far:\n")
	for _, m := range history {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Text)
	}
	fmt.Fprintf(&b, "\nRep's message: %s", message)
	return b.String()
}
