package secrets

const (
	KeyAuthSigning  = "AUTH_SIGNING_KEY"
	KeyDeepgramAPI  = "DEEPGRAM_API_KEY"
	KeyAnthropicAPI = "ANTHROPIC_API_KEY"
	KeyOpenAIAPI    = "OPENAI_API_KEY"
	// KeyEmbedAPI is the embedding role's own key (e.g. a Gemini key), so the
	// embed vendor can differ from the chat-LLM vendor. Falls back to KeyOpenAIAPI.
	KeyEmbedAPI = "EMBED_API_KEY"
	// KeyWhatsAppToken is the Meta Cloud API access token for OTP sends (16.13).
	KeyWhatsAppToken = "WHATSAPP_ACCESS_TOKEN"
)

var AllKeys = []string{
	KeyAuthSigning,
	KeyDeepgramAPI,
	KeyAnthropicAPI,
	KeyOpenAIAPI,
	KeyEmbedAPI,
	KeyWhatsAppToken,
}
