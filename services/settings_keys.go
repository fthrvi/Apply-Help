package services

// Settings key constants. The Settings table stores everything as string
// key/value pairs; centralizing the keys here keeps typos out and makes
// adding new ones obvious. Use the bare name inside this package
// (KeyGeminiAPI1) or services.KeyGeminiAPI1 from other packages.
const (
	KeyGeminiAPI1     = "GEMINI_API_KEY"
	KeyGeminiAPI2     = "GEMINI_API_KEY_2"
	KeyActiveGemini   = "ACTIVE_GEMINI_KEY"
	KeyGeminiURL      = "GEMINI_URL"
	KeyClaudeAPI      = "CLAUDE_API_KEY"
	KeyClaudeModel    = "CLAUDE_MODEL"
	KeyOpenAIAPI      = "OPENAI_API_KEY"
	KeyOpenAIModel    = "OPENAI_MODEL"
	KeyExtractPrompt      = "EXTRACTION_PROMPT"
	KeyCombinedPrompt     = "COMBINED_PROMPT"
	KeyCombinedSchema     = "COMBINED_SCHEMA"
	KeyResumeParsePrompt     = "RESUME_PARSE_PROMPT"
	KeyTranscriptParsePrompt = "TRANSCRIPT_PARSE_PROMPT"
	KeyResumeTemplate = "RESUME_TEMPLATE"
	KeyCoverTemplate  = "COVER_TEMPLATE"
	KeyUserInfo         = "USER_INFO"
	KeyJobPreferences   = "JOB_PREFERENCES"
	KeyLastCommitSHA    = "LAST_COMMIT_SHA"
	KeyLastSimplifyETag = "LAST_SIMPLIFY_ETAG"

	KeyGithubUsername = "GITHUB_USERNAME"
	KeyGithubToken    = "GITHUB_TOKEN"

	KeyGmailAddress     = "GMAIL_ADDRESS"
	KeyGmailAppPassword = "GMAIL_APP_PASSWORD"
	KeyEmailClassifyPrompt = "EMAIL_CLASSIFY_PROMPT"

	// Local LLM (Ollama-compatible) endpoint + models. Used by the
	// autofill agent for low-latency, profile-aware form filling.
	KeyLocalLLMEndpoint   = "LOCAL_LLM_ENDPOINT"
	KeyLocalLLMModel      = "LOCAL_LLM_MODEL"
	KeyLocalLLMEmbedModel = "LOCAL_LLM_EMBED_MODEL"
)

// Default model identifiers used when the corresponding setting is empty.
// These can be overridden at runtime via the Settings UI.
const (
	DefaultOpenAIModel = "google/gemma-3-27b-it"
	DefaultClaudeModel = "claude-sonnet-4-6"
)
