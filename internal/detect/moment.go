// Package detect is the Stage 3 suggestion trigger: it buffers the live transcript and,
// on a manual "Suggest" click, builds a retrieval query from the last N minutes.
// Automatic detection was removed. See techdocs/detection_techdoc.md.
package detect

import "github.com/VanshNarang12/sales-agent/internal/stt"

type Speaker = stt.Speaker

// SuggestRequest is one Suggest-button click for a session (D1).
type SuggestRequest struct {
	TenantID   string
	SessionID  string
	LookbackMs int64 // window size; 0 ⇒ engine default (D3)
}

// BuiltQuery is the single outbound contract of Stage 3 (shaped to become the Stage-5 retrieval input + a gRPC message, D6).
type BuiltQuery struct {
	TenantID  string
	SessionID string

	Query   string // the raw last-N-min transcript window
	StartMs int64
	EndMs   int64
}

// EmitFunc hands a built query to its consumer; invoked from the engine goroutine and must not block.
type EmitFunc func(BuiltQuery)
