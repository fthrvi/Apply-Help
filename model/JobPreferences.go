package model

// JobPreferences captures the user's target-role tags for filtering the
// dashboard. Generated initially by an LLM that reads UserInfo +
// GitHubContext, then editable in Settings.
//
// Each Tag has a short label and a keyword list. A job is considered a
// match for a tag when its role or company (case-folded) contains any of
// the tag's keywords. Tags with Enabled=false are hidden from the
// dashboard chip bar but kept in the saved list so the user can toggle
// them back on later.
type JobPreferences struct {
	Tags []JobTag `json:"tags"`
}

type JobTag struct {
	Label    string   `json:"label"`
	Keywords []string `json:"keywords"`
	Enabled  bool     `json:"enabled"`
}
