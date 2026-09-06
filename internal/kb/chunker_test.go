package kb

import (
	"fmt"
	"strings"
	"testing"
)

// para builds one paragraph of n numbered sentences.
func para(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s sentence number %d with some filler words in it. ", prefix, i)
	}
	return strings.TrimSpace(b.String())
}

func TestChunkWallOfTextNoFormatting(t *testing.T) {
	text := para("Plain", 80) // one giant paragraph, no headings, no blank lines
	pieces := Chunk("Pricing Doc", text, 100, 15)

	if len(pieces) < 2 {
		t.Fatalf("want multiple pieces for long text, got %d", len(pieces))
	}
	limit := 100 * charsPerToken
	for i, p := range pieces {
		if p.Heading != "" {
			t.Errorf("piece %d: heading = %q, want empty (no structure)", i, p.Heading)
		}
		if !strings.HasPrefix(p.Text, "Pricing Doc — ") {
			t.Errorf("piece %d: breadcrumb fallback to title missing: %q", i, p.Text[:40])
		}
		// limit + overlap + breadcrumb is the acceptable ceiling
		if len(p.Text) > limit+15*charsPerToken+40 {
			t.Errorf("piece %d: %d chars, exceeds limit+overlap", i, len(p.Text))
		}
		if !strings.HasSuffix(strings.TrimSpace(p.Text), ".") {
			t.Errorf("piece %d ends mid-sentence: %q", i, p.Text[len(p.Text)-30:])
		}
	}
}

func TestChunkHeadingsBecomeBreadcrumbs(t *testing.T) {
	text := "intro before any heading.\n\n# Pricing\nBase plan costs money.\n\n## Enterprise\nCustom quotes above fifty seats."
	pieces := Chunk("Guide", text, 450, 60)

	if len(pieces) != 3 {
		t.Fatalf("want 3 pieces (intro, Pricing, Pricing>Enterprise), got %d: %+v", len(pieces), pieces)
	}
	if pieces[0].Heading != "" || !strings.HasPrefix(pieces[0].Text, "Guide — intro") {
		t.Errorf("intro piece wrong: %+v", pieces[0])
	}
	if pieces[1].Heading != "Pricing" || !strings.HasPrefix(pieces[1].Text, "Guide > Pricing — ") {
		t.Errorf("Pricing piece wrong: %+v", pieces[1])
	}
	if pieces[2].Heading != "Pricing > Enterprise" || !strings.HasPrefix(pieces[2].Text, "Guide > Pricing > Enterprise — ") {
		t.Errorf("Enterprise piece wrong: %+v", pieces[2])
	}
}

func TestChunkOverlapRepeatsPreviousTail(t *testing.T) {
	text := para("Overlap", 80)
	pieces := Chunk("Doc", text, 100, 20)
	if len(pieces) < 2 {
		t.Fatalf("need ≥2 pieces, got %d", len(pieces))
	}
	prevBody := strings.TrimPrefix(pieces[0].Text, "Doc — ")
	prevSentences := sentences(prevBody)
	lastSentence := prevSentences[len(prevSentences)-1]
	if !strings.Contains(pieces[1].Text, lastSentence) {
		t.Errorf("piece 1 does not repeat the tail of piece 0.\ntail: %q", lastSentence)
	}
}

func TestChunkTableStaysWhole(t *testing.T) {
	var rows []string
	rows = append(rows, "| plan | price |", "| --- | --- |")
	for i := 0; i < 60; i++ {
		rows = append(rows, fmt.Sprintf("| plan %d | %d dollars per seat every month |", i, i*10))
	}
	table := strings.Join(rows, "\n") // way over a 100-token limit
	text := "Intro paragraph about plans.\n\n" + table + "\n\nOutro paragraph."

	pieces := Chunk("Doc", text, 100, 15)
	found := 0
	for _, p := range pieces {
		if strings.Contains(p.Text, "| plan 0 |") {
			found++
			if !strings.Contains(p.Text, "| plan 59 |") {
				t.Error("table was split — first and last row not in the same piece")
			}
		}
	}
	if found != 1 {
		t.Errorf("table should appear whole in exactly 1 piece, found in %d", found)
	}
}

func TestChunkShortDocSinglePiece(t *testing.T) {
	pieces := Chunk("Doc", "One short line about pricing.", 450, 60)
	if len(pieces) != 1 {
		t.Fatalf("want 1 piece, got %d", len(pieces))
	}
	if pieces[0].Text != "Doc — One short line about pricing." {
		t.Errorf("text = %q", pieces[0].Text)
	}
}

func TestChunkEmptyInput(t *testing.T) {
	if pieces := Chunk("Doc", "   \n\n  ", 450, 60); pieces != nil {
		t.Fatalf("want nil for empty text, got %+v", pieces)
	}
}

func TestChunkMonsterSentenceHardSplit(t *testing.T) {
	monster := strings.Repeat("word ", 1000) // 5000 chars, no sentence punctuation
	pieces := Chunk("Doc", monster, 100, 15)
	if len(pieces) < 2 {
		t.Fatalf("monster sentence should hard-split, got %d piece(s)", len(pieces))
	}
	for i, p := range pieces {
		if len(p.Text) > 100*charsPerToken+15*charsPerToken+40 {
			t.Errorf("piece %d exceeds limit: %d chars", i, len(p.Text))
		}
	}
}

func TestDecimalNumbersNotSplit(t *testing.T) {
	ss := sentences("The price is 3.5 dollars per seat. Second sentence here.")
	if len(ss) != 2 {
		t.Fatalf("want 2 sentences (decimal must not split), got %d: %v", len(ss), ss)
	}
}
