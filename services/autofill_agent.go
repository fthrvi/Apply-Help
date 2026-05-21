package services

import (
	model "32-Adarsha/model"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// FieldSignature is the per-field summary we extract from the form DOM
// before classification. The chromedp-injected scanner JS populates
// this; the autofill agent consumes it.
type FieldSignature struct {
	ID           string   `json:"id"`            // element id (preferred selector)
	Name         string   `json:"name"`          // name attribute
	Type         string   `json:"type"`          // text|email|tel|select|textarea|checkbox|radio|...
	Tag          string   `json:"tag"`           // input|select|textarea
	Label        string   `json:"label"`         // resolved <label for=...> text or aria-label
	Placeholder  string   `json:"placeholder"`   // placeholder attribute
	Required     bool     `json:"required"`      // marked required
	Options      []string `json:"options"`       // for <select>, the visible option texts
	CurrentValue string   `json:"current_value"` // existing value (skip if non-empty)
}

// FieldFill is the classifier's decision for one field.
type FieldFill struct {
	Value      string  // the value to set on the element (or "skip" sentinel)
	Confidence float32 // 0-1; below threshold we either escalate or skip
	Source     string  // "regex" | "embedding" | "local-llm" | "skip"
}

// AutofillProfile is the user's profile distilled into a flat
// (key, value, embedding) list. Built once per autofill run; the
// embeddings are pre-computed so each per-field lookup is just a
// cosine-similarity scan in memory.
type AutofillProfile struct {
	Keys   []string    // semantic descriptions ("first name", "email address", ...)
	Values []string    // matching profile values
	Embeds [][]float32 // matching embeddings of Keys (may be empty if embed model unavailable)
}

// BuildAutofillProfile expands UserInfo into a flat list of fillable
// fields, with embeddings if a local embedder is configured. When
// embeddings fail (no local LLM), Embeds stays nil and the agent
// falls back to regex-only tier — same behavior as the legacy
// OpenAndPreFill but smarter when local LLM is up.
func BuildAutofillProfile(ui *model.UserInfo) *AutofillProfile {
	p := &AutofillProfile{}
	add := func(key, val string) {
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		p.Keys = append(p.Keys, key)
		p.Values = append(p.Values, val)
	}

	if ui == nil {
		return p
	}
	add("first name", firstName(ui.Name))
	add("last name", lastName(ui.Name))
	add("full name", ui.Name)
	add("preferred name", firstName(ui.Name))
	add("email address", ui.Email)
	add("phone number", ui.Phone)
	add("mobile phone", ui.Phone)
	add("linkedin profile url", ui.LinkedIn)
	add("github profile url", ui.GitHub)
	add("portfolio website url", ui.GitHub) // best-effort fallback
	add("current city or location", ui.Location)
	add("address", ui.Location)
	add("city", ui.Location)

	if len(ui.Experience) > 0 {
		add("current company employer", ui.Experience[0].Company)
		add("current job title role", ui.Experience[0].Title)
	}
	if len(ui.Education) > 0 {
		add("school university institution", ui.Education[0].Institution)
		add("degree", ui.Education[0].Degree)
		add("major field of study", ui.Education[0].Degree)
	}

	// Skills aggregated — useful for "skills" / "technologies" fields.
	skills := []string{}
	skills = append(skills, ui.Skills.Languages...)
	skills = append(skills, ui.Skills.Frameworks...)
	skills = append(skills, ui.Skills.DevTools...)
	skills = append(skills, ui.Skills.Databases...)
	if len(skills) > 0 {
		add("skills technologies stack", strings.Join(skills, ", "))
	}

	// Embeddings — best-effort. If the embed call fails we keep going
	// with regex-only matching. The endpoint check inside LocalLLMEmbed
	// returns a fast error when not configured.
	if strings.TrimSpace(GetSetting(GlobalDB, KeyLocalLLMEndpoint)) != "" {
		embeds := make([][]float32, len(p.Keys))
		for i, k := range p.Keys {
			if v, err := LocalLLMEmbed(k); err == nil && len(v) > 0 {
				embeds[i] = v
			}
		}
		// Only commit if at least one embedding succeeded.
		for _, e := range embeds {
			if len(e) > 0 {
				p.Embeds = embeds
				break
			}
		}
	}
	return p
}

// fieldSignatureText concatenates the field's text-bearing attributes
// into a single string for regex / embedding matching.
func fieldSignatureText(f FieldSignature) string {
	return strings.ToLower(strings.Join([]string{f.Label, f.Placeholder, f.Name, f.ID}, " "))
}

// ClassifyField runs the three tiers and returns a fill decision.
//   - Tier 1: regex on common field names (instant, free, covers ~60% of fields)
//   - Tier 2: embedding similarity vs profile keys (~5ms when local LLM is up,
//     handles semantic variations the regex misses)
//   - Tier 3: local LLM with profile-as-system-prompt (~200ms, handles
//     dropdowns + free-text questions)
//
// jobContext is the job description; supplied so the LLM can answer
// "Why are you interested in this role?" style questions in-context.
func ClassifyField(f FieldSignature, profile *AutofillProfile, jobContext string) FieldFill {
	if strings.TrimSpace(f.CurrentValue) != "" {
		return FieldFill{Source: "skip"}
	}

	// --- TIER 1: regex ---
	sig := fieldSignatureText(f)
	if val, conf := regexMatch(sig, profile); conf >= 0.9 {
		return FieldFill{Value: val, Confidence: conf, Source: "regex"}
	}

	// --- TIER 2: embedding ---
	if len(profile.Embeds) > 0 {
		fieldDesc := f.Label
		if fieldDesc == "" {
			fieldDesc = f.Placeholder
		}
		if fieldDesc == "" {
			fieldDesc = f.Name
		}
		if fieldDesc != "" {
			if v, err := LocalLLMEmbed(fieldDesc); err == nil && len(v) > 0 {
				bestIdx, bestSim := -1, float32(0)
				for i, ke := range profile.Embeds {
					if len(ke) == 0 {
						continue
					}
					s := CosineSim(v, ke)
					if s > bestSim {
						bestSim = s
						bestIdx = i
					}
				}
				if bestIdx >= 0 && bestSim >= 0.7 {
					return FieldFill{Value: profile.Values[bestIdx], Confidence: bestSim, Source: "embedding"}
				}
			}
		}
	}

	// --- TIER 3: local LLM ---
	// Only invoke for fields where it could plausibly help: dropdowns,
	// long textareas, or required fields we couldn't match elsewhere.
	// Skipping every unrelated checkbox keeps per-form cost low.
	shouldEscalate := f.Tag == "select" || f.Tag == "textarea" || f.Required
	if !shouldEscalate {
		return FieldFill{Source: "skip"}
	}

	if strings.TrimSpace(GetSetting(GlobalDB, KeyLocalLLMEndpoint)) == "" {
		return FieldFill{Source: "skip"}
	}

	val, conf := localLLMClassify(f, profile, jobContext)
	if val == "" {
		return FieldFill{Source: "skip"}
	}
	return FieldFill{Value: val, Confidence: conf, Source: "local-llm"}
}

// regexMatch is the legacy field-name pattern matcher. Returns a
// (value, confidence) tuple; confidence is 1.0 on a hit, 0 otherwise.
func regexMatch(sig string, p *AutofillProfile) (string, float32) {
	rules := []struct {
		re  *regexp.Regexp
		key string
	}{
		{regexp.MustCompile(`first.?name|fname|given`), "first name"},
		{regexp.MustCompile(`last.?name|lname|family|surname`), "last name"},
		{regexp.MustCompile(`full.?name|^name$|your.?name|candidate.?name|legal.?name`), "full name"},
		{regexp.MustCompile(`e[-_ ]?mail`), "email address"},
		{regexp.MustCompile(`phone|mobile|telephone|tel\b`), "phone number"},
		{regexp.MustCompile(`linkedin`), "linkedin profile url"},
		{regexp.MustCompile(`github`), "github profile url"},
		{regexp.MustCompile(`portfolio|website`), "portfolio website url"},
		{regexp.MustCompile(`location|city|address`), "current city or location"},
		{regexp.MustCompile(`current.?company|current.?employer`), "current company employer"},
		{regexp.MustCompile(`current.?title|current.?role|current.?position`), "current job title role"},
		{regexp.MustCompile(`school|university|institution|college`), "school university institution"},
	}
	for _, r := range rules {
		if r.re.MatchString(sig) {
			for i, k := range p.Keys {
				if k == r.key {
					return p.Values[i], 1.0
				}
			}
		}
	}
	return "", 0
}

// localLLMClassify asks the small local model what value belongs in
// this field. Returns "" + 0 confidence if the model can't decide.
func localLLMClassify(f FieldSignature, profile *AutofillProfile, jobContext string) (string, float32) {
	// Build a compact profile-as-system-prompt — small enough to load
	// quickly into the KV cache, large enough to answer most questions.
	type profileEntry struct {
		Key   string `json:"k"`
		Value string `json:"v"`
	}
	entries := make([]profileEntry, 0, len(profile.Keys))
	for i, k := range profile.Keys {
		entries = append(entries, profileEntry{Key: k, Value: profile.Values[i]})
	}
	profileJSON, _ := json.Marshal(entries)

	systemPrompt := "You are an autofill assistant. The candidate's profile is below. " +
		"You will receive form fields one at a time. For each, output ONLY the value to fill in " +
		"(no quotes, no commentary). If a field has multiple-choice options, output one option exactly. " +
		"For free-text questions (\"Why are you interested?\", etc.), produce a short answer in the candidate's voice using their profile + the job context. " +
		"If you can't confidently determine a value, output exactly: SKIP\n\n" +
		"Candidate profile: " + string(profileJSON)
	if strings.TrimSpace(jobContext) != "" {
		// Cap jobContext to keep prompts small / responses fast.
		if len(jobContext) > 1500 {
			jobContext = jobContext[:1500]
		}
		systemPrompt += "\n\nTarget job description: " + jobContext
	}

	// User prompt = the field signature in a structured form.
	type fieldRequest struct {
		Label       string   `json:"label"`
		Placeholder string   `json:"placeholder,omitempty"`
		Type        string   `json:"type"`
		Tag         string   `json:"tag"`
		Options     []string `json:"options,omitempty"`
	}
	req := fieldRequest{
		Label:       f.Label,
		Placeholder: f.Placeholder,
		Type:        f.Type,
		Tag:         f.Tag,
		Options:     f.Options,
	}
	reqJSON, _ := json.Marshal(req)

	out, err := LocalLLMChat(systemPrompt, "Field: "+string(reqJSON)+"\n\nValue:", false)
	if err != nil {
		LogError(GlobalDB, fmt.Sprintf("autofill LLM classify: %v", err))
		return "", 0
	}
	out = strings.TrimSpace(out)
	if out == "" || strings.EqualFold(out, "SKIP") {
		return "", 0
	}
	// Strip leading "Value:" / quotes if the model added them.
	out = strings.TrimPrefix(out, "Value:")
	out = strings.TrimPrefix(out, "value:")
	out = strings.TrimSpace(out)
	out = strings.Trim(out, `"'`)
	return out, 0.8 // a successful LLM call gets a baseline confidence; we don't have logits
}
