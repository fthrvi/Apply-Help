package services

import _ "embed"

//go:embed defaults/extraction_prompt.txt
var defaultExtractionPrompt string

//go:embed defaults/combined_prompt.txt
var defaultCombinedPrompt string

//go:embed defaults/combined_schema.json
var defaultCombinedSchema string

//go:embed defaults/resume_parse_prompt.txt
var defaultResumeParsePrompt string

//go:embed defaults/transcript_parse_prompt.txt
var defaultTranscriptParsePrompt string

//go:embed defaults/email_classify_prompt.txt
var defaultEmailClassifyPrompt string

//go:embed placeholder/resume.html
var defaultResumeTemplate string

//go:embed placeholder/cover.html
var defaultCoverTemplate string

// Alternative template styles. Selectable via the Settings → HTML
// Templates picker; choosing one writes its contents into
// KeyResumeTemplate + KeyCoverTemplate.
//
//go:embed placeholder/resume_alt1.html
var resumeAlt1 string

//go:embed placeholder/cover_alt1.html
var coverAlt1 string

//go:embed placeholder/resume_alt2.html
var resumeAlt2 string

//go:embed placeholder/cover_alt2.html
var coverAlt2 string

//go:embed placeholder/resume_alt3.html
var resumeAlt3 string

//go:embed placeholder/cover_alt3.html
var coverAlt3 string

// TemplateStyle describes one of the bundled template pairs.
type TemplateStyle struct {
	ID          string
	Label       string
	Description string
	Resume      string
	Cover       string
}

// TemplateStyles returns the bundled template options in display order.
// The Settings UI iterates this list to build the picker.
func TemplateStyles() []TemplateStyle {
	return []TemplateStyle{
		{
			ID:          "default",
			Label:       "Magazine Pink",
			Description: "EB Garamond + Oswald, pink accent, horizontal masthead.",
			Resume:      defaultResumeTemplate,
			Cover:       defaultCoverTemplate,
		},
		{
			ID:          "alt1",
			Label:       "Modern Minimal",
			Description: "Inter, monochrome with subtle slate accent, generous whitespace.",
			Resume:      resumeAlt1,
			Cover:       coverAlt1,
		},
		{
			ID:          "alt2",
			Label:       "Editorial Classic",
			Description: "Cormorant Garamond + Source Serif Pro, deep oxblood accent, centered masthead.",
			Resume:      resumeAlt2,
			Cover:       coverAlt2,
		},
		{
			ID:          "alt3",
			Label:       "Architectural Technical",
			Description: "JetBrains Mono headings + Inter body, sharp teal accent, terminal-inspired markers.",
			Resume:      resumeAlt3,
			Cover:       coverAlt3,
		},
	}
}
