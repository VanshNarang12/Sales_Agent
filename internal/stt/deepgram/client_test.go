package deepgram

import (
	"testing"

	"github.com/VanshNarang12/sales-agent/internal/stt"
)

func testCfg() stt.StreamConfig {
	return stt.StreamConfig{
		TenantID:   "tenant-1",
		SessionID:  "sess-1",
		Speaker:    stt.SpeakerProspect,
		SampleRate: 16000,
		Encoding:   "pcm_s16le",
		Channels:   1,
	}
}

func TestParseResult(t *testing.T) {
	tests := []struct {
		name    string
		msg     string
		wantOK  bool
		wantTxt string
		final   bool
		speech  bool
		startMs int64
		endMs   int64
		conf    float64
	}{
		{
			name:    "interim partial",
			msg:     `{"type":"Results","is_final":false,"speech_final":false,"start":1.0,"duration":0.5,"channel":{"alternatives":[{"transcript":"hello there","confidence":0.91}]}}`,
			wantOK:  true,
			wantTxt: "hello there",
			final:   false,
			speech:  false,
			startMs: 1000,
			endMs:   1500,
			conf:    0.91,
		},
		{
			name:    "final with end of turn",
			msg:     `{"type":"Results","is_final":true,"speech_final":true,"start":2.0,"duration":1.25,"channel":{"alternatives":[{"transcript":"what's the price","confidence":0.99}]}}`,
			wantOK:  true,
			wantTxt: "what's the price",
			final:   true,
			speech:  true,
			startMs: 2000,
			endMs:   3250,
			conf:    0.99,
		},
		{
			name:   "empty transcript during silence is skipped",
			msg:    `{"type":"Results","is_final":false,"channel":{"alternatives":[{"transcript":"   ","confidence":0}]}}`,
			wantOK: false,
		},
		{
			name:   "no alternatives is skipped",
			msg:    `{"type":"Results","channel":{"alternatives":[]}}`,
			wantOK: false,
		},
		{
			name:   "metadata message is skipped",
			msg:    `{"type":"Metadata","duration":12.3}`,
			wantOK: false,
		},
		{
			name:   "utterance end message is skipped",
			msg:    `{"type":"UtteranceEnd","last_word_end":3.1}`,
			wantOK: false,
		},
		{
			name:   "malformed json is skipped, not fatal",
			msg:    `{not json`,
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := parseResult([]byte(tc.msg), testCfg())
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if ev.Text != tc.wantTxt {
				t.Errorf("Text = %q, want %q", ev.Text, tc.wantTxt)
			}
			if ev.IsFinal != tc.final {
				t.Errorf("IsFinal = %v, want %v", ev.IsFinal, tc.final)
			}
			if ev.SpeechFinal != tc.speech {
				t.Errorf("SpeechFinal = %v, want %v", ev.SpeechFinal, tc.speech)
			}
			if ev.StartMs != tc.startMs {
				t.Errorf("StartMs = %d, want %d", ev.StartMs, tc.startMs)
			}
			if ev.EndMs != tc.endMs {
				t.Errorf("EndMs = %d, want %d", ev.EndMs, tc.endMs)
			}
			if ev.Confidence != tc.conf {
				t.Errorf("Confidence = %v, want %v", ev.Confidence, tc.conf)
			}
			// Speaker/tenant/session are passed through from the stream config.
			if ev.Speaker != stt.SpeakerProspect || ev.TenantID != "tenant-1" || ev.SessionID != "sess-1" {
				t.Errorf("context not propagated: %+v", ev)
			}
		})
	}
}

// TestProviderDefaults verifies New fills in model/endpointing defaults.
func TestProviderDefaults(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.model != "nova-3" {
		t.Errorf("model = %q, want nova-3", p.model)
	}
	if p.endpointingMs != 300 {
		t.Errorf("endpointingMs = %d, want 300", p.endpointingMs)
	}
}

// TestBuildURL checks the Deepgram query string carries our fixed audio format and the
// streaming flags the later stages depend on (interim results, endpointing, keyterms).
func TestBuildURL(t *testing.T) {
	p := New(Config{APIKey: "k", Model: "nova-3", EndpointingMs: 250})
	cfg := testCfg()
	cfg.Keyterms = []string{"Shipsy", "Acme"}
	u := p.buildURL(cfg)

	for _, want := range []string{
		"model=nova-3",
		"encoding=linear16",
		"sample_rate=16000",
		"channels=1",
		"interim_results=true",
		"endpointing=250",
		"keyterm=Shipsy",
		"keyterm=Acme",
	} {
		if !contains(u, want) {
			t.Errorf("buildURL missing %q in %q", want, u)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
