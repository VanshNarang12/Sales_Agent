package detect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/VanshNarang12/sales-agent/internal/stt"
	"github.com/VanshNarang12/sales-agent/internal/transcript"
)

// fakeStore is an in-memory TranscriptStore with the same window semantics as Redis.
type fakeStore struct {
	entries   []transcript.Entry
	appendErr error
	windowErr error
}

func (f *fakeStore) Append(_ context.Context, _, _ string, e transcript.Entry) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeStore) Window(_ context.Context, _, _ string, lookbackMs int64) ([]transcript.Entry, error) {
	if f.windowErr != nil {
		return nil, f.windowErr
	}
	if len(f.entries) == 0 {
		return nil, nil
	}
	cutoff := f.entries[len(f.entries)-1].EndMs - lookbackMs
	var out []transcript.Entry
	for _, e := range f.entries {
		if e.EndMs >= cutoff {
			out = append(out, e)
		}
	}
	return out, nil
}

func finalEv(sp stt.Speaker, text string, startMs, endMs int64) stt.TranscriptEvent {
	return stt.TranscriptEvent{Speaker: sp, IsFinal: true, Text: text, StartMs: startMs, EndMs: endMs}
}

func TestSuggestEmitsWindowAsQuery(t *testing.T) {
	eng := NewEngine(nil, &fakeStore{}, 0)

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
	if !strings.Contains(q.Query, "prospect: do you support single sign-on") {
		t.Errorf("query = %q, want the raw transcript window", q.Query)
	}
	if q.StartMs != 0 || q.EndMs != 3000 {
		t.Errorf("span = [%d,%d], want [0,3000]", q.StartMs, q.EndMs)
	}
}

func TestSuggestEmptyStoreEmitsNothing(t *testing.T) {
	eng := NewEngine(nil, &fakeStore{}, 0)
	var n int
	sess := eng.StartSession(context.Background(), "t", "s", func(BuiltQuery) { n++ })

	sess.Suggest(context.Background(), 0)

	if n != 0 {
		t.Fatalf("empty store should emit nothing, got %d", n)
	}
}

func TestSuggestWindowErrorEmitsNothing(t *testing.T) {
	eng := NewEngine(nil, &fakeStore{windowErr: errors.New("redis down")}, 0)
	var n int
	sess := eng.StartSession(context.Background(), "t", "s", func(BuiltQuery) { n++ })

	sess.Suggest(context.Background(), 0)

	if n != 0 {
		t.Fatalf("store error should emit nothing, got %d", n)
	}
}

func TestOnTranscriptIgnoresPartialsAndEmpty(t *testing.T) {
	eng := NewEngine(nil, &fakeStore{}, 0)
	var got []BuiltQuery
	sess := eng.StartSession(context.Background(), "t", "s", func(q BuiltQuery) { got = append(got, q) })

	sess.OnTranscript(stt.TranscriptEvent{Speaker: stt.SpeakerProspect, IsFinal: false, Text: "half sentence"})
	sess.OnTranscript(finalEv(stt.SpeakerProspect, "", 0, 100))
	sess.OnTranscript(finalEv(stt.SpeakerProspect, "kept", 100, 200))

	sess.Suggest(context.Background(), 0)

	if len(got) != 1 || got[0].Query != "prospect: kept" {
		t.Errorf("window = %+v, want only the non-empty final", got)
	}
}

func TestJoinWindow(t *testing.T) {
	text, startMs, endMs := joinWindow([]transcript.Entry{
		{Speaker: "prospect", Text: "old", StartMs: 0, EndMs: 1000},
		{Speaker: "rep", Text: "recent", StartMs: 9000, EndMs: 10000},
	})
	if text != "prospect: old\nrep: recent" {
		t.Errorf("text = %q", text)
	}
	if startMs != 0 || endMs != 10000 {
		t.Errorf("span = [%d,%d], want [0,10000]", startMs, endMs)
	}
}
