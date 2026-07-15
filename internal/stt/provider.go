package stt

import (
	"context"
	"errors"
)

type Speaker string

const (
	SpeakerRep      Speaker = "rep"
	SpeakerProspect Speaker = "prospect"
)

var ErrProviderUnavailable = errors.New("stt: provider unavailable")

type TranscriptEvent struct {
	TenantID    string
	SessionID   string
	Speaker     Speaker
	IsFinal     bool
	SpeechFinal bool
	Text        string
	StartMs     int64
	EndMs       int64
	Confidence  float64
}

type StreamConfig struct {
	TenantID   string
	SessionID  string
	Speaker    Speaker
	SampleRate int
	Encoding   string
	Channels   int

	Keyterms []string
}

type Provider interface {
	OpenStream(ctx context.Context, cfg StreamConfig) (Stream, error)
}
type Stream interface {
	Send(pcm []byte) error
	Events() <-chan TranscriptEvent
	Close() error
}
