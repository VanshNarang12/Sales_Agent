package gateway

import "testing"

// makeFrame builds a valid-shaped audio frame: 4-byte header + pcmBytes of payload.
func makeFrame(typ, channel byte, seq uint16, pcmBytes int) []byte {
	b := make([]byte, frameHeaderLen+pcmBytes)
	b[0] = typ
	b[1] = channel
	b[2] = byte(seq)      // low byte first (little-endian)
	b[3] = byte(seq >> 8) // high byte
	return b
}

func TestParseAudioFrameOK(t *testing.T) {
	b := makeFrame(msgTypeAudio, chProspect, 0x0102, 4096)
	channel, seq, pcm, err := parseAudioFrame(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channel != chProspect {
		t.Errorf("channel = %d, want %d", channel, chProspect)
	}
	if seq != 0x0102 {
		t.Errorf("seq = %#x, want 0x0102", seq)
	}
	if len(pcm) != 4096 {
		t.Errorf("pcm length = %d, want 4096", len(pcm))
	}
}

func TestParseAudioFrameSeqLittleEndian(t *testing.T) {
	// seq bytes 0x34, 0x12 little-endian => 0x1234.
	b := []byte{msgTypeAudio, chRep, 0x34, 0x12, 0x00, 0x00}
	_, seq, _, err := parseAudioFrame(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seq != 0x1234 {
		t.Errorf("seq = %#x, want 0x1234", seq)
	}
}

func TestParseAudioFrameRejects(t *testing.T) {
	cases := map[string][]byte{
		"too short":   {msgTypeAudio, chRep},               // fewer than 4 header bytes
		"odd payload": {msgTypeAudio, chRep, 0, 0, 0x7f},   // (len-4) is odd
		"wrong type":  makeFrame(0x02, chRep, 1, 4),        // type byte != 0x01
		"bad channel": makeFrame(msgTypeAudio, 0x05, 1, 4), // channel not 0 or 1
	}
	for name, b := range cases {
		if _, _, _, err := parseAudioFrame(b); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}
