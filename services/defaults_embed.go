package services

import _ "embed"

//go:embed defaults/extraction_prompt.txt
var defaultExtractionPrompt string

//go:embed defaults/combined_prompt.txt
var defaultCombinedPrompt string

//go:embed defaults/combined_schema.json
var defaultCombinedSchema string

//go:embed placeholder/resume.html
var defaultResumeTemplate string

//go:embed placeholder/cover.html
var defaultCoverTemplate string
