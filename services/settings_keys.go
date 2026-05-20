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
	KeyResumeParsePrompt  = "RESUME_PARSE_PROMPT"
	KeyResumeTemplate = "RESUME_TEMPLATE"
	KeyCoverTemplate  = "COVER_TEMPLATE"
	KeyUserInfo       = "USER_INFO"
	KeyLastCommitSHA  = "LAST_COMMIT_SHA"
)

// Default model identifiers used when the corresponding setting is empty.
// These can be overridden at runtime via the Settings UI.
const (
	DefaultOpenAIModel = "google/gemma-3-27b-it"
	DefaultClaudeModel = "claude-sonnet-4-6"
)
