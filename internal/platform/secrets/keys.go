package secrets

const (
	KeyAuthSigning  = "AUTH_SIGNING_KEY"
	KeyDeepgramAPI  = "DEEPGRAM_API_KEY"
	KeyAnthropicAPI = "ANTHROPIC_API_KEY"
	KeyOpenAIAPI    = "OPENAI_API_KEY"
	// KeyEmbedAPI is the embedding role's own key (e.g. a Gemini key), so the
	// embed vendor can differ from the chat-LLM vendor. Falls back to KeyOpenAIAPI.
	KeyEmbedAPI = "EMBED_API_KEY"
)

var AllKeys = []string{
	KeyAuthSigning,
	KeyDeepgramAPI,
	KeyAnthropicAPI,
	KeyOpenAIAPI,
	KeyEmbedAPI,
}
