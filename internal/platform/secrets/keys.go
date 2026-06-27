package secrets

// keys.go is the vault registry: the single place that names every secret this
// system uses. Code refers to secrets by these constants (never raw string
// literals) so there is one authoritative list of "all the keys", and so renaming
// or auditing a secret touches exactly one line. Values live only in the backing
// Store (env locally, a KMS-backed manager in cloud) — never here.

const (
	// KeyAuthSigning is the HMAC key used to sign/verify session tokens.
	KeyAuthSigning = "AUTH_SIGNING_KEY"

	// KeyDeepgramAPI is the Deepgram streaming-STT API key. Held server-side only —
	// it is never sent to the desktop client (transcription_techdoc.md decision D2).
	KeyDeepgramAPI = "DEEPGRAM_API_KEY"
)

// AllKeys lists every secret name in the registry. Used by tooling/tests to document
// and validate the environment (e.g. assert .env.example covers every key). Add a
// new key's constant here when you add it above.
var AllKeys = []string{
	KeyAuthSigning,
	KeyDeepgramAPI,
}
