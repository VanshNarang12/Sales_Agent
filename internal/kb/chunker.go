// Package kb ingests documents into the knowledge base: chunking, embedding, storage.
// Chunking strategy and its benchmarks: techdocs/knowledge_base_techdoc.md §3.
package kb

import (
	"strings"
)

// Piece is one chunk ready for embedding.
type Piece struct {
	Heading string // breadcrumb path inside the doc; "" when the doc had no headings
	Text    string // breadcrumb-prefixed text — exactly what gets embedded and stored
}

const (
	charsPerToken        = 4 // approximation decided in the techdoc; no tokenizer dependency
	defaultTargetTokens  = 450
	defaultOverlapTokens = 60
)

func Chunk(docTitle, text string, targetTokens, overlapTokens int) []Piece {
	if targetTokens <= 0 {
		targetTokens = defaultTargetTokens
	}
	if overlapTokens <= 0 {
		overlapTokens = defaultOverlapTokens
	}
	limit := targetTokens * charsPerToken
	overlap := overlapTokens * charsPerToken

	var pieces []Piece
	for _, sec := range splitSections(text) {
		bodies := packSection(sec.body, limit)
		crumb := docTitle
		if sec.path != "" {
			crumb = docTitle + " > " + sec.path
		}
		for i, body := range bodies {
			if i > 0 && !isTable(body) {
				if tail := sentenceTail(bodies[i-1], overlap); tail != "" {
					body = tail + " " + body
				}
			}
			pieces = append(pieces, Piece{Heading: sec.path, Text: crumb + " — " + body})
		}
	}
	return pieces
}

type section struct {
	path string // "H1 > H2" breadcrumb; "" for content before/without headings
	body string
}

// splitSections cuts at markdown headings, tracking the heading path per section.
// No headings ⇒ one section with an empty path. Empty bodies are dropped.
func splitSections(text string) []section {
	var out []section
	var stack []string // heading text by level
	var buf []string

	flush := func() {
		body := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = buf[:0]
		if body != "" {
			out = append(out, section{path: strings.Join(stack, " > "), body: body})
		}
	}

	for _, line := range strings.Split(text, "\n") {
		level, title := headingOf(line)
		if level == 0 {
			buf = append(buf, line)
			continue
		}
		flush()
		if level-1 < len(stack) {
			stack = stack[:level-1]
		}
		stack = append(stack, title)
	}
	flush()
	return out
}

// headingOf returns the markdown heading level (1–6) and title, or (0, "").
func headingOf(line string) (int, string) {
	trimmed := strings.TrimLeft(line, "#")
	level := len(line) - len(trimmed)
	if level < 1 || level > 6 || !strings.HasPrefix(trimmed, " ") {
		return 0, ""
	}
	return level, strings.TrimSpace(trimmed)
}

// packSection greedily packs a section's paragraphs (and, when a paragraph is too
// big, its sentences) into bodies of ≤ limit chars. Tables are atomic: emitted as
// their own body even when oversized.
func packSection(body string, limit int) []string {
	type unit struct {
		text  string
		sep   string // separator before this unit when appended to a non-empty buffer
		table bool
	}
	var units []unit
	for _, par := range splitParagraphs(body) {
		switch {
		case isTable(par):
			units = append(units, unit{text: par, sep: "\n\n", table: true})
		case len(par) > limit:
			for _, s := range sentences(par) {
				for _, part := range hardSplit(s, limit) {
					units = append(units, unit{text: part, sep: " "})
				}
			}
		default:
			units = append(units, unit{text: par, sep: "\n\n"})
		}
	}

	var bodies []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			bodies = append(bodies, buf.String())
			buf.Reset()
		}
	}
	for _, u := range units {
		if u.table {
			flush()
			bodies = append(bodies, u.text) // whole, even if oversized
			continue
		}
		if buf.Len() > 0 && buf.Len()+len(u.sep)+len(u.text) > limit {
			flush()
		}
		if buf.Len() > 0 {
			buf.WriteString(u.sep)
		}
		buf.WriteString(u.text)
	}
	flush()
	return bodies
}

func splitParagraphs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\n\n") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// isTable: a markdown table block — at least 2 lines, all starting with "|".
func isTable(s string) bool {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) < 2 {
		return false
	}
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "|") {
			return false
		}
	}
	return true
}

// sentences splits on ". ! ?" followed by whitespace/end. "3.5" stays whole.
func sentences(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '.' && c != '!' && c != '?' {
			continue
		}
		if i+1 < len(s) && s[i+1] != ' ' && s[i+1] != '\n' && s[i+1] != '\t' {
			continue
		}
		if t := strings.TrimSpace(s[start : i+1]); t != "" {
			out = append(out, t)
		}
		start = i + 1
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		out = append(out, t)
	}
	return out
}

// hardSplit is the last resort for a single "sentence" longer than the limit:
// cut at the last space before the limit, or at the limit if there is none.
func hardSplit(s string, limit int) []string {
	var out []string
	for len(s) > limit {
		cut := strings.LastIndexByte(s[:limit], ' ')
		if cut <= 0 {
			cut = limit
		}
		out = append(out, strings.TrimSpace(s[:cut]))
		s = strings.TrimSpace(s[cut:])
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}

// sentenceTail returns whole sentences from the end of prev totalling ≤ maxChars —
// the overlap prepended to the next chunk. "" when prev fits entirely (no point
// duplicating a whole chunk) or has no sentence within the budget.
func sentenceTail(prev string, maxChars int) string {
	ss := sentences(prev)
	if len(ss) == 0 {
		return ""
	}
	total := 0
	start := len(ss)
	for i := len(ss) - 1; i >= 0; i-- {
		if total+len(ss[i])+1 > maxChars {
			break
		}
		total += len(ss[i]) + 1
		start = i
	}
	if start == 0 || start == len(ss) {
		return "" // whole chunk would repeat, or nothing fits
	}
	return strings.Join(ss[start:], " ")
}
