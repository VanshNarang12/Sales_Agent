package stt

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProvider hands out fakeStreams and counts how many times each speaker opened.
type fakeProvider struct {
	mu      sync.Mutex
	opens   map[Speaker]int
	streams []*fakeStream
	failN   int32 // fail the first N OpenStream calls (atomic)
}

func newFakeProvider() *fakeProvider { return &fakeProvider{opens: map[Speaker]int{}} }

func (p *fakeProvider) OpenStream(_ context.Context, cfg StreamConfig) (Stream, error) {
	if atomic.LoadInt32(&p.failN) > 0 {
		atomic.AddInt32(&p.failN, -1)
		return nil, ErrProviderUnavailable
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opens[cfg.Speaker]++
	st := &fakeStream{events: make(chan TranscriptEvent, 8), speaker: cfg.Speaker}
	p.streams = append(p.streams, st)
	return st, nil
}

func (p *fakeProvider) openCount(sp Speaker) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.opens[sp]
}

type fakeStream struct {
	events  chan TranscriptEvent
	speaker Speaker
	sent    atomic.Int32
	closed  atomic.Bool
}

func (s *fakeStream) Send(_ []byte) error            { s.sent.Add(1); return nil }
func (s *fakeStream) Events() <-chan TranscriptEvent { return s.events }
func (s *fakeStream) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		close(s.events)
	}
	return nil
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

// TestLazyOpen: a stream opens only after the first frame for that channel, and a
// rep-only call never opens a prospect stream.
func TestLazyOpen(t *testing.T) {
	p := newFakeProvider()
	m := NewManager(p, nil)
	sess := m.StartSession(context.Background(), "t1", "s1", func(TranscriptEvent) {})
	defer sess.Close()

	if p.openCount(SpeakerRep) != 0 {
		t.Fatal("rep stream opened before any frame")
	}
	sess.Write(0, []byte{1, 2})
	waitFor(t, func() bool { return p.openCount(SpeakerRep) == 1 }, "rep stream open")

	if got := p.openCount(SpeakerProspect); got != 0 {
		t.Fatalf("prospect stream opened on a rep-only call: %d", got)
	}
}

// TestEventForwarding: events from both streams reach the callback, tagged by speaker.
func TestEventForwarding(t *testing.T) {
	p := newFakeProvider()
	m := NewManager(p, nil)

	var mu sync.Mutex
	got := map[Speaker]int{}
	sess := m.StartSession(context.Background(), "t1", "s1", func(ev TranscriptEvent) {
		mu.Lock()
		got[ev.Speaker]++
		mu.Unlock()
	})
	defer sess.Close()

	sess.Write(0, []byte{1})
	sess.Write(1, []byte{2})
	waitFor(t, func() bool { return p.openCount(SpeakerRep) == 1 && p.openCount(SpeakerProspect) == 1 }, "both streams open")

	p.mu.Lock()
	streams := append([]*fakeStream{}, p.streams...)
	p.mu.Unlock()
	for _, st := range streams {
		st.events <- TranscriptEvent{Speaker: st.speaker, Text: "hi", IsFinal: true}
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return got[SpeakerRep] == 1 && got[SpeakerProspect] == 1
	}, "one event per speaker delivered")
}

// TestReconnect: when a stream dies mid-call, the supervisor reopens it.
func TestReconnect(t *testing.T) {
	p := newFakeProvider()
	m := NewManager(p, nil)
	sess := m.StartSession(context.Background(), "t1", "s1", func(TranscriptEvent) {})
	defer sess.Close()

	sess.Write(0, []byte{1})
	waitFor(t, func() bool { return p.openCount(SpeakerRep) == 1 }, "first open")

	// Kill the first stream — the supervisor should open a second one (after backoff).
	p.mu.Lock()
	first := p.streams[0]
	p.mu.Unlock()
	first.Close()

	waitFor(t, func() bool { return p.openCount(SpeakerRep) == 2 }, "reconnect after stream death")
}

// TestOpenFailureRetries: OpenStream failures are retried, not fatal.
func TestOpenFailureRetries(t *testing.T) {
	p := newFakeProvider()
	atomic.StoreInt32(&p.failN, 2) // first two opens fail, third succeeds
	m := NewManager(p, nil)
	sess := m.StartSession(context.Background(), "t1", "s1", func(TranscriptEvent) {})
	defer sess.Close()

	sess.Write(0, []byte{1})
	waitFor(t, func() bool { return p.openCount(SpeakerRep) == 1 }, "open succeeds after retries")
}

// TestCloseStopsSupervisors: Close cancels ctx and all supervisors exit.
func TestCloseStopsSupervisors(t *testing.T) {
	p := newFakeProvider()
	m := NewManager(p, nil)
	sess := m.StartSession(context.Background(), "t1", "s1", func(TranscriptEvent) {})

	sess.Write(0, []byte{1})
	sess.Write(1, []byte{2})
	waitFor(t, func() bool { return p.openCount(SpeakerRep) == 1 && p.openCount(SpeakerProspect) == 1 }, "streams open")

	done := make(chan struct{})
	go func() { sess.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return — supervisors leaked")
	}
}
