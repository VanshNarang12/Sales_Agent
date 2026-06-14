package gateway

import "errors"

// Wire-protocol constants for the realtime audio stream.
// Full byte layout: techdocs/realtime_gateway_techdoc.md §3.
const (
	msgTypeAudio byte = 0x01 // binary message type byte: "this is an audio frame"

	chRep      byte = 0x00 // channel byte: rep (microphone)
	chProspect byte = 0x01 // channel byte: prospect (system audio)

	frameHeaderLen = 4 // [1B type][1B channel][2B seq little-endian]
)

// WebSocket application close codes (4000–4999 is the spec's app-reserved range).
const (
	wsProtocolError     = 4002 // malformed frame, audio before hello, or unknown type
	wsUnsupportedFormat = 4003 // unsupported sampleRate/encoding/channels in hello
)

// errBadFrame is returned when a binary message is not a well-formed audio frame.
var errBadFrame = errors.New("gateway: malformed audio frame")

// parseAudioFrame unpacks one binary audio frame into the speaking channel, the
// per-channel sequence number, and the raw PCM payload. It is a pure function (no
// socket, no state) so it can be unit-tested directly.
//
// Layout: [1B type=0x01][1B channel][2B seq little-endian][N bytes Int16LE PCM].
func parseAudioFrame(b []byte) (channel byte, seq uint16, pcm []byte, err error) {
	// Need the 4-byte header; PCM is 16-bit samples, so the payload length must be even.
	if len(b) < frameHeaderLen || (len(b)-frameHeaderLen)%2 != 0 {
		return 0, 0, nil, errBadFrame
	}
	if b[0] != msgTypeAudio {
		return 0, 0, nil, errBadFrame
	}
	channel = b[1]
	if channel != chRep && channel != chProspect {
		return 0, 0, nil, errBadFrame
	}
	seq = uint16(b[2]) | uint16(b[3])<<8 // little-endian: low byte first
	pcm = b[frameHeaderLen:]
	return channel, seq, pcm, nil
}

// channelLabel maps a channel byte to a human/metric label.
func channelLabel(channel byte) string {
	if channel == chProspect {
		return "prospect"
	}
	return "rep"
}
