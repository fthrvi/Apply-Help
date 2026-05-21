package services

import (
	model "32-Adarsha/model"
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Per-entry HTML templates use a __TOKEN__ syntax so they don't collide
// with FillTemplateString's {- KEY -} markers. The static markup
// references the same CSS classes the main resume.html stylesheet
// defines (.entry, .entry-header, .entry-title, .entry-date, .entry-body).
const experienceEntryHTML = `<div class="entry">
  <div class="entry-header">
    <div class="entry-title">__HEADING__ &mdash; <span>__TITLE__</span></div>
    <div class="entry-date">__DATES__</div>
  </div>
  <div class="entry-body"><ul>__BULLETS__</ul></div>
</div>
`

const educationEntryHTML = `<div class="entry">
  <div class="entry-header">
    <div class="entry-title">__HEADING__ &mdash; <span>__DEGREE__</span></div>
    <div class="entry-date">__DATES__</div>
  </div>
  <div class="entry-body"><p>__COURSEWORK__</p></div>
</div>
`

const projectEntryHTML = `<div class="entry">
  <div class="entry-header">
    <div class="entry-title">__NAME__ __URL_LINK__ <span>__TECH__</span></div>
  </div>
  <div class="entry-body"><p>__DESCRIPTION__</p></div>
</div>
`

// rawHTMLFillRe matches the alternate-syntax {!- KEY -!} placeholder
// used for sections whose value is pre-rendered HTML that must NOT be
// escaped. FillTemplateString's escaping is correct for LLM output but
// would mangle our rendered entries.
var rawHTMLFillRe = regexp.MustCompile(`\{!-\s*(.*?)\s*-!\}`)

// FillRawHTML substitutes {!- KEY -!} placeholders with values from
// rawSections WITHOUT HTML escaping. Missing keys leave the placeholder
// unchanged so substitution failures are visible during development.
func FillRawHTML(htmlStr string, rawSections map[string]string) string {
	return rawHTMLFillRe.ReplaceAllStringFunc(htmlStr, func(match string) string {
		// {!- and -!} are 3 chars each.
		keyword := strings.TrimSpace(match[3 : len(match)-3])
		if val, ok := rawSections[keyword]; ok {
			return val
		}
		return match
	})
}

// renderContactLine builds a horizontal contact strip from UserInfo,
// skipping fields the user hasn't filled. Output is a pre-escaped HTML
// fragment ready to drop into the {!- ContactLine -!} slot in both the
// resume and cover templates.
//
// When UserInfo.GitHub is empty but the user has KeyGithubUsername set
// (used by the GitHub repo-fetch integration), we synthesize the URL
// as https://github.com/<username>. Saves the user from entering the
// same handle twice.
func renderContactLine(ui *model.UserInfo) string {
	if ui == nil {
		return ""
	}
	var parts []string
	if s := strings.TrimSpace(ui.Location); s != "" {
		parts = append(parts, html.EscapeString(s))
	}
	if s := strings.TrimSpace(ui.Phone); s != "" {
		esc := html.EscapeString(s)
		parts = append(parts, fmt.Sprintf(`<a href="tel:%s">%s</a>`, esc, esc))
	}
	if s := strings.TrimSpace(ui.Email); s != "" {
		esc := html.EscapeString(s)
		parts = append(parts, fmt.Sprintf(`<a href="mailto:%s">%s</a>`, esc, esc))
	}
	if s := strings.TrimSpace(ui.LinkedIn); s != "" {
		parts = append(parts, fmt.Sprintf(`<a href="%s">LinkedIn</a>`, html.EscapeString(s)))
	}
	githubURL := strings.TrimSpace(ui.GitHub)
	if githubURL == "" {
		if user := strings.TrimSpace(GetSetting(GlobalDB, KeyGithubUsername)); user != "" {
			githubURL = "https://github.com/" + user
		}
	}
	if githubURL != "" {
		parts = append(parts, fmt.Sprintf(`<a href="%s">GitHub</a>`, html.EscapeString(githubURL)))
	}
	return strings.Join(parts, " &middot; ")
}

// experienceHeading composes the left-side title of an experience entry
// from the structured fields: "Company, Location" when Location is set,
// else just Company.
func experienceHeading(exp model.Experience) string {
	loc := strings.TrimSpace(exp.Location)
	comp := strings.TrimSpace(exp.Company)
	if comp == "" {
		return ""
	}
	if loc == "" {
		return comp
	}
	return comp + ", " + loc
}

func educationHeading(edu model.Education) string {
	loc := strings.TrimSpace(edu.Location)
	inst := strings.TrimSpace(edu.Institution)
	if inst == "" {
		return ""
	}
	if loc == "" {
		return inst
	}
	return inst + ", " + loc
}

// dateRange formats a "Start - End" string, substituting "Present" when
// End is empty and Start is not. An empty Start with non-empty End just
// returns End. Empty/empty returns "".
func dateRange(start, end string) string {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	switch {
	case start == "" && end == "":
		return ""
	case start == "":
		return end
	case end == "":
		return start + " - Present"
	default:
		return start + " - " + end
	}
}

// renderExperienceSection concatenates all experience entries into HTML
// for the {!- ExperienceSection -!} slot. LLM-tailored bullets are taken
// from bulletsByIndex when present; otherwise the saved Experience.Bullets
// are used as a fallback so a partial LLM response still produces output.
func RenderExperienceSection(exps []model.Experience, bulletsByIndex [][]string) string {
	if len(exps) == 0 {
		return `<p class="entry-body" style="color:#999;font-style:italic">No experience on file. Add entries in Settings → User Profile → Experience.</p>`
	}
	var out strings.Builder
	for i, exp := range exps {
		bullets := exp.Bullets
		if i < len(bulletsByIndex) && len(bulletsByIndex[i]) > 0 {
			bullets = bulletsByIndex[i]
		}
		var bulletsHTML strings.Builder
		for _, b := range bullets {
			b = strings.TrimSpace(b)
			if b == "" {
				continue
			}
			bulletsHTML.WriteString("<li>")
			bulletsHTML.WriteString(html.EscapeString(b))
			bulletsHTML.WriteString("</li>")
		}

		entry := experienceEntryHTML
		entry = strings.ReplaceAll(entry, "__HEADING__", html.EscapeString(experienceHeading(exp)))
		entry = strings.ReplaceAll(entry, "__TITLE__", html.EscapeString(exp.Title))
		entry = strings.ReplaceAll(entry, "__DATES__", html.EscapeString(dateRange(exp.StartDate, exp.EndDate)))
		entry = strings.ReplaceAll(entry, "__BULLETS__", bulletsHTML.String())
		out.WriteString(entry)
	}
	return out.String()
}

func RenderEducationSection(edus []model.Education, courseworkByIndex []string) string {
	if len(edus) == 0 {
		return ""
	}
	var out strings.Builder
	for i, edu := range edus {
		coursework := ""
		if i < len(courseworkByIndex) {
			coursework = strings.TrimSpace(courseworkByIndex[i])
		}
		if coursework == "" && len(edu.Coursework) > 0 {
			coursework = strings.Join(edu.Coursework, "; ")
		}

		entry := educationEntryHTML
		entry = strings.ReplaceAll(entry, "__HEADING__", html.EscapeString(educationHeading(edu)))
		entry = strings.ReplaceAll(entry, "__DEGREE__", html.EscapeString(edu.Degree))
		entry = strings.ReplaceAll(entry, "__DATES__", html.EscapeString(dateRange(edu.StartDate, edu.EndDate)))
		entry = strings.ReplaceAll(entry, "__COURSEWORK__", html.EscapeString(coursework))
		out.WriteString(entry)
	}
	return out.String()
}

// RenderProjectsSection renders only the projects selected by the LLM
// via RelevantProjectIndices. selectedIndices[i] is the zero-based
// index into projs that should appear in slot i; descByIndex[i] is the
// matching tailored description. When selectedIndices is empty (LLM
// didn't return any) it falls back to the first 3 projects so a partial
// LLM response still produces output. Each rendered entry includes a
// small accent-colored URL link when project.URL is set.
func RenderProjectsSection(projs []model.Project, selectedIndices []int, descByIndex []string) string {
	if len(projs) == 0 {
		return ""
	}
	indices := selectedIndices
	if len(indices) == 0 {
		n := len(projs)
		if n > 3 {
			n = 3
		}
		indices = make([]int, n)
		for i := range indices {
			indices[i] = i
		}
	}

	var out strings.Builder
	for slot, projIdx := range indices {
		if projIdx < 0 || projIdx >= len(projs) {
			continue
		}
		p := projs[projIdx]

		desc := ""
		if slot < len(descByIndex) {
			desc = strings.TrimSpace(descByIndex[slot])
		}
		if desc == "" && len(p.Bullets) > 0 {
			desc = strings.Join(p.Bullets, " ")
		}

		techDisplay := ""
		if len(p.Technologies) > 0 {
			techDisplay = "| " + strings.Join(p.Technologies, " | ") + " |"
		}

		urlLink := ""
		if u := strings.TrimSpace(p.URL); u != "" {
			urlLink = fmt.Sprintf(`<a class="repo-link" href="%s">%s</a>`,
				html.EscapeString(u), html.EscapeString(displayURL(u)))
		}

		entry := projectEntryHTML
		entry = strings.ReplaceAll(entry, "__NAME__", html.EscapeString(p.Name))
		entry = strings.ReplaceAll(entry, "__URL_LINK__", urlLink)
		entry = strings.ReplaceAll(entry, "__TECH__", html.EscapeString(techDisplay))
		entry = strings.ReplaceAll(entry, "__DESCRIPTION__", html.EscapeString(desc))
		out.WriteString(entry)
	}
	return out.String()
}

// displayURL strips the scheme + trailing slash for compact in-resume
// display: "https://github.com/foo/bar" → "github.com/foo/bar".
func displayURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimSuffix(u, "/")
	return u
}

// coerceIntArray pulls a []int out of an `any` value that may be
// []int, []float64 (JSON numbers come back as float64), or []any.
func coerceIntArray(v any) []int {
	switch t := v.(type) {
	case []int:
		return t
	case []float64:
		out := make([]int, 0, len(t))
		for _, x := range t {
			out = append(out, int(x))
		}
		return out
	case []any:
		out := make([]int, 0, len(t))
		for _, x := range t {
			switch n := x.(type) {
			case float64:
				out = append(out, int(n))
			case int:
				out = append(out, n)
			}
		}
		return out
	default:
		return nil
	}
}

// coerceStringArray pulls a []string out of an `any` value that may be
// a []string, []any (of strings), or nil. Robust against the LLM
// returning slightly different JSON shapes.
func coerceStringArray(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	default:
		return nil
	}
}

// coerceStringMatrix pulls a [][]string out of an `any` value that's
// expected to be an array of arrays (the ExperienceBullets shape).
func coerceStringMatrix(v any) [][]string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([][]string, 0, len(arr))
	for _, row := range arr {
		out = append(out, coerceStringArray(row))
	}
	return out
}
