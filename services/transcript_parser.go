package services

import (
	model "32-Adarsha/model"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ParseTranscriptToEducationMap runs the configured LLM over arbitrary
// transcript text (PDF extract / OCR / paste) plus the candidate's
// Education entries, and returns a map of zero-based Education index →
// list of courses that belong to that entry. The LLM groups by
// matching institution + degree against each course's source.
//
// modelChoice mirrors PromptAI values ("gemini" / "claude" / "openai").
func ParseTranscriptToEducationMap(transcriptText string, educations []model.Education, modelChoice string) (map[int][]string, error) {
	if strings.TrimSpace(transcriptText) == "" {
		return nil, fmt.Errorf("transcript text is empty — extraction may have failed")
	}
	if len(educations) == 0 {
		return nil, fmt.Errorf("no education entries on file — add one in Settings → User Profile → Education first")
	}

	tpl := GetSetting(GlobalDB, KeyTranscriptParsePrompt)
	if tpl == "" {
		tpl = defaultTranscriptParsePrompt
	}

	// Compact education context: just index, institution, degree.
	type eduLite struct {
		Index       int    `json:"index"`
		Institution string `json:"institution"`
		Degree      string `json:"degree"`
	}
	list := make([]eduLite, 0, len(educations))
	for i, e := range educations {
		list = append(list, eduLite{Index: i, Institution: e.Institution, Degree: e.Degree})
	}
	eduJSON, _ := json.Marshal(list)

	if len(transcriptText) > 40000 {
		transcriptText = transcriptText[:40000]
	}
	prompt := fmt.Sprintf(tpl, string(eduJSON), transcriptText)

	raw, err := PromptAI(prompt, modelChoice)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	jsonStr := strings.TrimSpace(raw)
	if !strings.HasPrefix(jsonStr, "{") {
		re := regexp.MustCompile(`(?s)\{.*\}`)
		if m := re.FindString(jsonStr); m != "" {
			jsonStr = m
		}
	}

	var parsed struct {
		ByEducation map[string][]string `json:"by_education"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		preview := raw
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return nil, fmt.Errorf("parse JSON: %w (raw: %s)", err, preview)
	}

	out := map[int][]string{}
	for k, v := range parsed.ByEducation {
		var idx int
		if _, err := fmt.Sscanf(k, "%d", &idx); err != nil {
			continue
		}
		if idx < 0 || idx >= len(educations) {
			continue
		}
		seen := map[string]bool{}
		cleaned := make([]string, 0, len(v))
		for _, c := range v {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			k := strings.ToLower(c)
			if seen[k] {
				continue
			}
			seen[k] = true
			cleaned = append(cleaned, c)
		}
		if len(cleaned) > 0 {
			out[idx] = cleaned
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("LLM returned no usable courses for any education entry")
	}
	return out, nil
}
