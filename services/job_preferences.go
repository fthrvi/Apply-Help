package services

import (
	model "32-Adarsha/model"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// GetJobPreferences loads the saved JobPreferences, or returns an empty
// struct if none have been generated yet. JSON decode errors are
// swallowed (treated as "no prefs"); the caller can regenerate via the
// Settings UI.
func GetJobPreferences(db *sql.DB) (*model.JobPreferences, error) {
	val := GetSetting(db, KeyJobPreferences)
	if val == "" {
		return &model.JobPreferences{}, nil
	}
	var p model.JobPreferences
	if err := json.Unmarshal([]byte(val), &p); err != nil {
		return &model.JobPreferences{}, nil
	}
	return &p, nil
}

func SaveJobPreferences(db *sql.DB, p *model.JobPreferences) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return SaveSetting(db, KeyJobPreferences, string(data))
}

// GenerateJobPreferencesWithLLM asks the configured LLM to derive job
// tags from the user's profile and GitHub activity. Each tag carries a
// keyword list that drives dashboard substring matching. Tags are
// returned with Enabled=true; the user prunes/edits in Settings.
//
// modelChoice mirrors PromptAI ("gemini" / "claude" / "openai").
func GenerateJobPreferencesWithLLM(ui *model.UserInfo, ghCtx *GitHubContext, modelChoice string) (*model.JobPreferences, error) {
	modelChoice = strings.ToLower(strings.TrimSpace(modelChoice))
	if modelChoice == "" {
		return nil, fmt.Errorf("model choice required")
	}
	prompt := buildJobPreferencesPrompt(ui, ghCtx)
	raw, err := PromptAI(prompt, modelChoice)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}
	return parseJobPreferencesResponse(raw)
}

func buildJobPreferencesPrompt(ui *model.UserInfo, ghCtx *GitHubContext) string {
	uiJSON := "{}"
	if ui != nil {
		b, _ := json.Marshal(ui)
		uiJSON = string(b)
	}
	ghJSON := "{}"
	if ghCtx != nil {
		b, _ := json.Marshal(ghCtx)
		ghJSON = string(b)
	}

	return fmt.Sprintf(`You generate job-search filter tags for a candidate.

Given the candidate's profile and GitHub activity below, output 5-10 tags that describe the kinds of new-grad / early-career roles they'd be a strong fit for. For each tag:
- "label": a short lowercase category name (e.g. "backend", "ml-infra", "distributed-systems", "ios", "data")
- "keywords": 4-8 lowercase strings that commonly appear in job titles for that tag. Include synonyms and abbreviations. Example for "backend": ["backend", "server", "platform", "infrastructure", "api"]. Example for "ml-infra": ["ml", "machine learning", "ai", "mlops", "model serving", "inference"]
- "enabled": always true

Tags should reflect the candidate's actual evidence (languages, frameworks, project domains, repo topics), not generic roles. If they ship distributed Go services, include "distributed-systems". If their projects are all React frontends, do NOT include "ml". Prefer specific tags over generic ones — "graphics" over "software-engineer".

Return a single JSON object with this shape — no markdown, no prose, no code fences:
{"tags": [{"label": "backend", "keywords": ["backend", "server", "platform"], "enabled": true}, ...]}

Candidate profile:
%s

GitHub context:
%s`, uiJSON, ghJSON)
}

var jobPrefsJSONRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseJobPreferencesResponse(raw string) (*model.JobPreferences, error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") {
		if m := jobPrefsJSONRe.FindString(s); m != "" {
			s = m
		}
	}
	var p model.JobPreferences
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		preview := raw
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return nil, fmt.Errorf("parse JSON: %w (raw: %s)", err, preview)
	}

	out := model.JobPreferences{}
	seenLabels := map[string]bool{}
	for _, t := range p.Tags {
		t.Label = strings.TrimSpace(strings.ToLower(t.Label))
		if t.Label == "" || seenLabels[t.Label] {
			continue
		}
		seenLabels[t.Label] = true

		cleaned := make([]string, 0, len(t.Keywords))
		seenKW := map[string]bool{}
		for _, k := range t.Keywords {
			k = strings.TrimSpace(strings.ToLower(k))
			if k == "" || seenKW[k] {
				continue
			}
			seenKW[k] = true
			cleaned = append(cleaned, k)
		}
		t.Keywords = cleaned
		if len(t.Keywords) == 0 {
			continue
		}
		t.Enabled = true
		out.Tags = append(out.Tags, t)
	}
	if len(out.Tags) == 0 {
		return nil, fmt.Errorf("LLM returned no usable tags")
	}
	return &out, nil
}

// JobMatchesKeywords reports whether the role/company contains any of
// the supplied keywords (case-folded substring match). An empty keyword
// list always matches — caller treats "no filter active" as show-all.
func JobMatchesKeywords(role, company string, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	haystack := strings.ToLower(role) + " " + strings.ToLower(company)
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(haystack, kw) {
			return true
		}
	}
	return false
}
