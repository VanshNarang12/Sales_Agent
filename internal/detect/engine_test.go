package detect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/VanshNarang12/sales-agent/internal/stt"
)

// fakeBuilder records the window it saw and returns a canned query (or an error).
type fakeBuilder struct {
	last string
	out  string
	err  error
}

func (f *fakeBuilder) Build(_ context.Context, window string) (string, error) {
	f.last = window
	return f.out, f.err
}

func finalEv(sp stt.Speaker, text string, startMs, endMs int64) stt.TranscriptEvent {
	return stt.TranscriptEvent{Speaker: sp, IsFinal: true, Text: text, StartMs: startMs, EndMs: endMs}
}

func TestSuggestBuildsQueryFromWindow(t *testing.T) {
	fb := &fakeBuilder{out: "does the product support SAML SSO?"}
	eng := NewEngine(nil, fb)

	var got []BuiltQuery
	sess := eng.StartSession(context.Background(), "tenant-1", "sess-1", func(q BuiltQuery) {
		got = append(got, q)
	})

	sess.OnTranscript(finalEv(stt.SpeakerRep, "sure, what did you want to know", 0, 1000))
	sess.OnTranscript(finalEv(stt.SpeakerProspect, "do you support single sign-on", 1000, 3000))

	sess.Suggest(context.Background(), 0)

	if len(got) != 1 {
		t.Fatalf("want 1 built query, got %d", len(got))
	}
	q := got[0]
	if q.TenantID != "tenant-1" || q.SessionID != "sess-1" {
		t.Errorf("session context not stamped: %+v", q)
	}
	if q.Query != "does the product support SAML SSO?" {
		t.Errorf("query = %q, want the builder output", q.Query)
	}
	if !strings.Contains(fb.last, "single sign-on") {
		t.Errorf("builder window = %q, want the recent transcript", fb.last)
	}
	if q.Source != SourceModel {
		t.Errorf("source = %q, want model", q.Source)
	}
}

func TestSuggestPassesThroughWhenNoBuilder(t *testing.T) {
	eng := NewEngine(nil, nil) // skeleton mode: window is the query

	var got []BuiltQuery
	sess := eng.StartSession(context.Background(), "t", "s", func(q BuiltQuery) { got = append(got, q) })

	sess.OnTranscript(finalEv(stt.SpeakerProspect, "what is the price", 0, 1000))
	sess.Suggest(context.Background(), 0)

	if len(got) != 1 || !strings.Contains(got[0].Query, "what is the price") {
		t.Fatalf("want the window passed through, got %+v", got)
	}
}

func TestSuggestEmptyBufferEmitsNothing(t *testing.T) {
	eng := NewEngine(nil, &fakeBuilder{out: "x"})
	var n int
	sess := eng.StartSession(context.Background(), "t", "s", func(BuiltQuery) { n++ })

	sess.Suggest(context.Background(), 0)

	if n != 0 {
		t.Fatalf("empty buffer should emit nothing, got %d", n)
	}
}

func TestSuggestBuilderErrorEmitsNothing(t *testing.T) {
	eng := NewEngine(nil, &fakeBuilder{err: errors.New("model down")})
	var n int
	sess := eng.StartSession(context.Background(), "t", "s", func(BuiltQuery) { n++ })

	sess.OnTranscript(finalEv(stt.SpeakerProspect, "anything", 0, 1000))
	sess.Suggest(context.Background(), 0)

	if n != 0 {
		t.Fatalf("builder error should emit nothing, got %d", n)
	}
}

func TestOnTranscriptIgnoresPartialsAndEmpty(t *testing.T) {
	fb := &fakeBuilder{out: "q"}
	eng := NewEngine(nil, fb)
	sess := eng.StartSession(context.Background(), "t", "s", func(BuiltQuery) {})

	sess.OnTranscript(stt.TranscriptEvent{Speaker: stt.SpeakerProspect, IsFinal: false, Text: "half sentence"})
	sess.OnTranscript(finalEv(stt.SpeakerProspect, "", 0, 100))
	sess.OnTranscript(finalEv(stt.SpeakerProspect, "kept", 100, 200))

	sess.Suggest(context.Background(), 0)

	if fb.last != "prospect: kept" {
		t.Errorf("buffer = %q, want only the non-empty final", fb.last)
	}
}

func TestSnapshotTrimsToLookback(t *testing.T) {
	buf := []entry{
		{speaker: stt.SpeakerProspect, text: "old", startMs: 0, endMs: 1000},
		{speaker: stt.SpeakerRep, text: "recent", startMs: 9000, endMs: 10000},
	}
	// lookback 2000ms from latest end (10000) → cutoff 8000 → drops the "old" entry.
	text, startMs, endMs := snapshot(buf, 2000)
	if strings.Contains(text, "old") || !strings.Contains(text, "recent") {
		t.Errorf("window = %q, want only recent within lookback", text)
	}
	if startMs != 9000 || endMs != 10000 {
		t.Errorf("span = [%d,%d], want [9000,10000]", startMs, endMs)
	}
}
