package secrets

const (
	KeyAuthSigning  = "AUTH_SIGNING_KEY"
	KeyDeepgramAPI  = "DEEPGRAM_API_KEY"
	KeyAnthropicAPI = "ANTHROPIC_API_KEY"
	KeyOpenAIAPI    = "OPENAI_API_KEY"
)
var AllKeys = []string{
	KeyAuthSigning,
	KeyDeepgramAPI,
	KeyAnthropicAPI,
	KeyOpenAIAPI,
}
