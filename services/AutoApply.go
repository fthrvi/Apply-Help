package services

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// AutoApplyResult holds the data returned by the Go workflow.
type AutoApplyResult struct {
	Success     bool           `json:"success"`
	Company     string         `json:"company"`
	Role        string         `json:"role"`
	Description string         `json:"description"`
	ResumePath  string         `json:"resume"`
	CoverPath   string         `json:"cover"`
	ResumeData  map[string]any `json:"resume_data"`
	CoverData   map[string]any `json:"cover_data"`
	Error       string         `json:"error"`
}

// ═════════════════════════════════════════════════════════════════════════════
// HELPERS
// ═════════════════════════════════════════════════════════════════════════════

func getAutoApplyDir() string {
	return AppDir()
}

func sanitizeFilename(name string) string {
	reg, _ := regexp.Compile(`[^a-zA-Z0-9\-_\. ]+`)
	safe := reg.ReplaceAllString(name, "_")
	return strings.TrimSpace(safe)
}

func saveToDownloads(sourcePath string, company string, filename string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	// Organise by Company in Downloads folder
	targetDir := filepath.Join(home, "Downloads", "AutoApply", sanitizeFilename(company))
	os.MkdirAll(targetDir, 0755)

	targetPath := filepath.Join(targetDir, filename)

	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return
	}
	os.WriteFile(targetPath, data, 0644)
}

// ═════════════════════════════════════════════════════════════════════════════
// WORKFLOW STEPS
// ═════════════════════════════════════════════════════════════════════════════

func FetchHTML(url string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Cap response at 5 MB so an adversarial server can't stream forever.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func StripHTML(htmlStr string) string {
	// Remove script and style tags first
	reScript := regexp.MustCompile(`(?s)<script.*?>.*?</script>`)
	reStyle := regexp.MustCompile(`(?s)<style.*?>.*?</style>`)
	htmlStr = reScript.ReplaceAllString(htmlStr, " ")
	htmlStr = reStyle.ReplaceAllString(htmlStr, " ")

	// Remove all other tags
	reTags := regexp.MustCompile(`<[^>]*>`)
	text := reTags.ReplaceAllString(htmlStr, " ")

	// Collapse whitespace
	reSpace := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(reSpace.ReplaceAllString(text, " "))
}

// looksBlocked decides whether a plain-HTTP fetch returned a real job
// page or a bot-challenge / empty SPA shell. Drives the chromedp
// fallback in FetchAndCleanDescription.
func looksBlocked(rawHTML, cleaned string) bool {
	if len(cleaned) < 300 {
		return true
	}
	lower := strings.ToLower(rawHTML)
	for _, marker := range []string{
		"cf-browser-verification",
		"just a moment",
		"cf-mitigated",
		"datadome",
		"px-captcha",
		"checking your browser",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// FetchHTMLViaBrowser drives chromedp to render the page (executing JS,
// waiting for content to appear) and returns the visible body text.
// Used as a fallback when plain HTTP returns blocked / empty bodies.
// Zero LLM cost — pure rendering work.
func FetchHTMLViaBrowser(jobURL string) (string, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.DisableGPU,
		chromedp.Flag("hide-scrollbars", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	runCtx, cancelTimeout := context.WithTimeout(browserCtx, 45*time.Second)
	defer cancelTimeout()

	var bodyText string
	err := chromedp.Run(runCtx,
		chromedp.Navigate(jobURL),
		// Give the SPA time to populate before reading text. Many ATS
		// pages (Greenhouse, Lever, Ashby) take 1-3s to render the job
		// description after initial load.
		chromedp.Sleep(2500*time.Millisecond),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Text("body", &bodyText, chromedp.NodeVisible, chromedp.ByQuery),
	)
	if err != nil {
		return "", fmt.Errorf("chromedp fetch: %w", err)
	}

	// chromedp.Text returns visible text but may carry double spaces from
	// CSS-driven layout. Collapse to match StripHTML's output shape.
	reSpace := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(reSpace.ReplaceAllString(bodyText, " ")), nil
}

// FetchAndCleanDescription returns the cleaned job-description text for
// a URL, with a 24h SQLite-backed cache. On a miss it tries plain HTTP
// first (cheap, fast); if the response looks like a bot challenge / SPA
// shell, it falls back to chromedp. Cache hits skip the network and
// extraction work entirely.
func FetchAndCleanDescription(jobURL string) (string, error) {
	if cached := GetDescriptionCache(GlobalDB, jobURL, 24*time.Hour); cached != "" {
		return cached, nil
	}

	var lastErr error
	if html, err := FetchHTML(jobURL); err == nil {
		cleaned := StripHTML(html)
		if !looksBlocked(html, cleaned) {
			_ = SaveDescriptionCache(GlobalDB, jobURL, cleaned, "http")
			return cleaned, nil
		}
		lastErr = fmt.Errorf("plain HTTP returned blocked / empty page (%d chars)", len(cleaned))
	} else {
		lastErr = err
	}

	cleaned, err := FetchHTMLViaBrowser(jobURL)
	if err != nil {
		if lastErr != nil {
			return "", fmt.Errorf("http: %v; browser: %w", lastErr, err)
		}
		return "", err
	}
	_ = SaveDescriptionCache(GlobalDB, jobURL, cleaned, "browser")
	return cleaned, nil
}

// FillTemplateString substitutes {- KEY -} placeholders in htmlStr with values
// from data. Values are HTML-escaped before insertion: the substituted content
// comes from an LLM response, and unescaped insertion would let a malicious or
// glitched response inject scripts, images, or iframes that Chrome would
// execute or fetch during PDF rendering.
func FillTemplateString(htmlStr string, data map[string]any) string {
	pattern := regexp.MustCompile(`\{-\s*(.*?)\s*-\}`)
	return pattern.ReplaceAllStringFunc(htmlStr, func(match string) string {
		keyword := strings.TrimSpace(match[2 : len(match)-2])
		if val, ok := data[keyword]; ok {
			return html.EscapeString(fmt.Sprintf("%v", val))
		}
		return match
	})
}

// HtmlToPdf renders htmlPath to pdfPath using a headless Chrome/Chromium
// driven over the Chrome DevTools Protocol via chromedp. The previous
// implementation shelled out to /Applications/Google Chrome.app/... which
// was macOS-only; chromedp auto-detects the browser's location on macOS,
// Linux, and Windows.
//
// Defense in depth: even though FillTemplateString HTML-escapes substituted
// values, we also disable JavaScript in the rendering context so a malicious
// or glitched LLM response that somehow encodes raw markup can't fetch
// resources or execute scripts during rendering.
func HtmlToPdf(htmlPath, pdfPath string) error {
	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return err
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.DisableGPU,
		chromedp.Flag("disable-javascript", true),
		chromedp.Flag("hide-scrollbars", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	runCtx, cancelTimeout := context.WithTimeout(browserCtx, 30*time.Second)
	defer cancelTimeout()

	var pdfBytes []byte
	err = chromedp.Run(runCtx,
		chromedp.Navigate("file://"+absHTML),
		chromedp.ActionFunc(func(ctx context.Context) error {
			buf, _, err := page.PrintToPDF().
				WithPrintBackground(true).
				Do(ctx)
			if err != nil {
				return err
			}
			pdfBytes = buf
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("chromedp PrintToPDF: %w", err)
	}

	return os.WriteFile(pdfPath, pdfBytes, 0644)
}

// ═════════════════════════════════════════════════════════════════════════════
// MAIN WORKFLOW
// ═════════════════════════════════════════════════════════════════════════════

// RunAutoApply handles the full sequence: Fetch (if needed) -> Extract
// (if needed) -> Generate -> Fill -> PDF.
//
// Inputs:
//   - jobURL: fetched via FetchAndCleanDescription (HTTP-first with
//     chromedp fallback + 24h cache) when manualDesc is empty.
//   - manualDesc: pre-supplied description; bypasses the network entirely.
//   - knownCompany / knownRole: when both non-empty, the extraction LLM
//     call is skipped — saves one full call per Generate, the cheapest
//     credit optimization in the pipeline.
func RunAutoApply(jobURL, manualDesc, knownCompany, knownRole, modelChoice string, logFn func(string)) (*AutoApplyResult, error) {
	if logFn != nil {
		logFn("🚀 Starting Go-native workflow...\n")
	}

	var text string

	if strings.TrimSpace(manualDesc) != "" {
		if logFn != nil {
			logFn("📝 Using manually provided job description...\n")
		}
		text = manualDesc
	} else {
		if logFn != nil {
			logFn("🔍 STEP 1: Fetching job page (HTTP, chromedp fallback)...\n")
		}
		fetched, err := FetchAndCleanDescription(jobURL)
		if err != nil {
			LogError(GlobalDB, fmt.Sprintf("Fetch failed: %v", err))
			return nil, fmt.Errorf("fetch failed: %v", err)
		}
		text = fetched
	}

	if len(text) > 12000 {
		text = text[:12000]
	}

	// Skip the extraction LLM call when caller already knows company +
	// role (e.g. the EditJobView form has them filled). One LLM call
	// saved per Generate.
	if strings.TrimSpace(knownCompany) != "" && strings.TrimSpace(knownRole) != "" {
		if logFn != nil {
			logFn("⚡ Skipping extraction (company + role already known)\n")
		}
		return runStep3(knownCompany, knownRole, text, modelChoice, logFn)
	}

	if logFn != nil {
		logFn("🧠 STEP 2: Extracting role & company via LLM...\n")
	}
	extractPrompt := GetSetting(GlobalDB, KeyExtractPrompt)
	prompt := fmt.Sprintf(extractPrompt, text)
	res, err := PromptAI(prompt, modelChoice)
	if err != nil {
		LogError(GlobalDB, fmt.Sprintf("Extraction failed: %v", err))
		return nil, fmt.Errorf("extraction failed: %v", err)
	}

	var info map[string]any
	if err := json.Unmarshal([]byte(res), &info); err != nil {
		reJSON := regexp.MustCompile(`(?s)\{.*\}`)
		if match := reJSON.FindString(res); match != "" {
			json.Unmarshal([]byte(match), &info)
		}
	}

	company := "Unknown Company"
	if v, ok := info["company_name"]; ok {
		company = fmt.Sprintf("%v", v)
	}
	role := "Unknown Role"
	if v, ok := info["role"]; ok {
		role = fmt.Sprintf("%v", v)
	}
	description := text
	if v, ok := info["job_description"]; ok {
		description = fmt.Sprintf("%v", v)
	}

	return runStep3(company, role, description, modelChoice, logFn)
}

// RunRegenerate skips the fetch/extract phase and goes straight to generation
func RunRegenerate(company, role, description string, modelChoice string, logFn func(string)) (*AutoApplyResult, error) {
	if logFn != nil {
		logFn("🔄 Starting Go-native Regeneration...\n")
	}
	return runStep3(company, role, description, modelChoice, logFn)
}

func runStep3(company, role, description, modelChoice string, logFn func(string)) (*AutoApplyResult, error) {
	baseDir := getAutoApplyDir()

	if logFn != nil {
		logFn(fmt.Sprintf("📝 STEP 3: Generating tailored documents for %s at %s...\n", role, company))
	}

	// Load user context
	var userContext string
	dbUserInfo, _ := GetUserInfo(GlobalDB)
	if dbUserInfo.Name != "" || len(dbUserInfo.Experience) > 0 {
		// Use DB data
		data, _ := json.Marshal(dbUserInfo)
		userContext = string(data)
	} else {
		// Fallback to files
		resumeDataRaw, _ := os.ReadFile(filepath.Join(baseDir, "context", "resume.json"))
		coverDataRaw, _ := os.ReadFile(filepath.Join(baseDir, "context", "cover.json"))
		userContext = fmt.Sprintf("RESUME_DATA: %s\nCOVER_DATA: %s", string(resumeDataRaw), string(coverDataRaw))
	}

	// Append GitHub context if the user has configured a github username.
	// Failures here are non-fatal — the LLM can still work off the
	// in-app résumé data, we just log so the user can see it in Error Logs.
	if ghCtx, ghErr := GitHubContextForCurrentUser(); ghErr == nil && ghCtx != nil {
		ghBytes, _ := json.Marshal(ghCtx)
		userContext += "\n\nGitHub Profile and Repositories (additional context):\n" + string(ghBytes)
		if logFn != nil {
			logFn(fmt.Sprintf("  🐙 Including GitHub context (%d repos) for @%s\n", len(ghCtx.Repos), ghCtx.Username))
		}
	} else if ghErr != nil {
		LogError(GlobalDB, fmt.Sprintf("GitHub context fetch failed: %v", ghErr))
	}

	// Combined Generation — cached by (model, full prompt). Re-Generates
	// with identical inputs cost zero tokens. Any change to description,
	// profile, schema, or prompt template alters the hash → cache miss.
	combinedPromptTemplate := GetSetting(GlobalDB, KeyCombinedPrompt)
	combinedSchema := GetSetting(GlobalDB, KeyCombinedSchema)
	prompt := fmt.Sprintf(combinedPromptTemplate, description, userContext, combinedSchema)

	var res string
	if cached := GetLLMResponseCache(GlobalDB, modelChoice, prompt); cached != "" {
		if logFn != nil {
			logFn("  ⚡ Reusing cached LLM response (zero tokens spent)\n")
		}
		res = cached
	} else {
		if logFn != nil {
			logFn("  ✨ Generating Resume & Cover Letter in a single LLM call...\n")
		}
		var err error
		res, err = PromptAI(prompt, modelChoice)
		if err != nil {
			LogError(GlobalDB, fmt.Sprintf("Combined generation failed: %v", err))
			return nil, fmt.Errorf("generation failed: %v", err)
		}
		_ = SaveLLMResponseCache(GlobalDB, modelChoice, prompt, res)
	}

	var combinedJSON map[string]map[string]any
	if err := json.Unmarshal([]byte(res), &combinedJSON); err != nil {
		// Attempt to extract JSON if model wrapped it in backticks
		reJSON := regexp.MustCompile(`(?s)\{.*\}`)
		if match := reJSON.FindString(res); match != "" {
			json.Unmarshal([]byte(match), &combinedJSON)
		}
	}

	resumeJSON := combinedJSON["RESUME"]
	coverJSON := combinedJSON["COVER"]

	if resumeJSON == nil || coverJSON == nil {
		err := fmt.Errorf("failed to extract RESUME or COVER from combined response")
		LogError(GlobalDB, err.Error())
		return nil, err
	}

	// Date + personal info injection. Personal-info keys are always set
	// (even to "") so the {- KEY -} placeholders substitute cleanly
	// instead of leaking through as literal text. The contact line is
	// pre-rendered as raw HTML so empty fields drop without leaving
	// orphan separators.
	today := time.Now().Format("January 02, 2006")
	resumeJSON["Date"] = today
	coverJSON["Date"] = today

	if dbUserInfo != nil {
		setPersonal := func(m map[string]any, k, v string) {
			if existing, ok := m[k]; ok && existing != nil {
				if s, isStr := existing.(string); isStr && s != "" {
					return // LLM (or earlier pass) supplied a real value; respect it
				}
			}
			m[k] = v
		}
		setPersonal(resumeJSON, "FullName", dbUserInfo.Name)
		setPersonal(resumeJSON, "Email", dbUserInfo.Email)
		setPersonal(resumeJSON, "Phone", dbUserInfo.Phone)
		setPersonal(resumeJSON, "Location", dbUserInfo.Location)
		setPersonal(resumeJSON, "LinkedIn", dbUserInfo.LinkedIn)
		setPersonal(resumeJSON, "GitHub", dbUserInfo.GitHub)
		setPersonal(coverJSON, "FullName", dbUserInfo.Name)
		setPersonal(coverJSON, "Email", dbUserInfo.Email)
		setPersonal(coverJSON, "Phone", dbUserInfo.Phone)
	}

	// ── Section rendering ──
	// Pull the LLM's structured arrays — ExperienceBullets (matrix),
	// EducationCoursework (vector), ProjectDescriptions (vector). Each
	// position aligns with the same-index entry in UserInfo. Missing /
	// short arrays fall back to the saved Experience.Bullets etc., so a
	// partial LLM response still produces a usable resume.
	expBullets := coerceStringMatrix(resumeJSON["ExperienceBullets"])
	eduCoursework := coerceStringArray(resumeJSON["EducationCoursework"])
	projDescs := coerceStringArray(resumeJSON["ProjectDescriptions"])
	projIndices := coerceIntArray(resumeJSON["RelevantProjectIndices"])

	var experienceHTML, educationHTML, projectsHTML, contactHTML string
	if dbUserInfo != nil {
		experienceHTML = RenderExperienceSection(dbUserInfo.Experience, expBullets)
		educationHTML = RenderEducationSection(dbUserInfo.Education, eduCoursework)
		projectsHTML = RenderProjectsSection(dbUserInfo.Projects, projIndices, projDescs)
		contactHTML = renderContactLine(dbUserInfo)
	}

	rawSections := map[string]string{
		"ExperienceSection": experienceHTML,
		"EducationSection":  educationHTML,
		"ProjectsSection":   projectsHTML,
		"ContactLine":       contactHTML,
	}

	// Output paths
	safeCompany := sanitizeFilename(company)
	outDir := filepath.Join(baseDir, "Company", safeCompany)
	os.MkdirAll(outDir, 0755)

	resumeHTML := filepath.Join(outDir, "resume.html")
	resumePDF := filepath.Join(outDir, "resume.pdf")
	coverHTML := filepath.Join(outDir, "cover.html")
	coverPDF := filepath.Join(outDir, "cover.pdf")

	// Fill Templates
	if logFn != nil {
		logFn("  🖋️  Filling HTML templates...\n")
	}

	resTemplate := GetSetting(GlobalDB, KeyResumeTemplate)
	if resTemplate == "" {
		resTemplate = defaultResumeTemplate
	}

	covTemplate := GetSetting(GlobalDB, KeyCoverTemplate)
	if covTemplate == "" {
		covTemplate = defaultCoverTemplate
	}

	// Two-pass fill: raw HTML sections (pre-rendered, not escaped) first,
	// then the regular {- KEY -} substitutions which are HTML-escaped.
	resumeFilled := FillRawHTML(resTemplate, rawSections)
	resumeFilled = FillTemplateString(resumeFilled, resumeJSON)
	coverFilled := FillRawHTML(covTemplate, rawSections)
	coverFilled = FillTemplateString(coverFilled, coverJSON)

	os.WriteFile(resumeHTML, []byte(resumeFilled), 0644)
	os.WriteFile(coverHTML, []byte(coverFilled), 0644)

	// Convert to PDF
	if logFn != nil { logFn("  📄 Converting to PDF (cupsfilter)...\n") }
	if err := HtmlToPdf(resumeHTML, resumePDF); err != nil {
		LogError(GlobalDB, fmt.Sprintf("resume PDF failed: %v", err))
		return nil, fmt.Errorf("resume PDF generation failed: %v", err)
	}
	if err := HtmlToPdf(coverHTML, coverPDF); err != nil {
		LogError(GlobalDB, fmt.Sprintf("cover PDF failed: %v", err))
		return nil, fmt.Errorf("cover PDF generation failed: %v", err)
	}

	// Also save to Downloads
	saveToDownloads(resumePDF, company, "resume.pdf")
	saveToDownloads(coverPDF, company, "cover.pdf")

	if logFn != nil { logFn("✅ DONE! Files saved in Company/" + safeCompany + "\n") }

	return &AutoApplyResult{
		Success:     true,
		Company:     company,
		Role:        role,
		Description: description,
		ResumePath:  resumePDF,
		CoverPath:   coverPDF,
		ResumeData:  resumeJSON,
		CoverData:   coverJSON,
	}, nil
}


func RegenerateFromData(company, role, resumeDataJSON, coverDataJSON string, logFn func(string)) (*AutoApplyResult, error) {
	var resumeJSON map[string]any
	var coverJSON map[string]any

	if err := json.Unmarshal([]byte(resumeDataJSON), &resumeJSON); err != nil {
		return nil, fmt.Errorf("invalid resume JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(coverDataJSON), &coverJSON); err != nil {
		return nil, fmt.Errorf("invalid cover JSON: %v", err)
	}

	// Re-run step 3 part 2 (Filling and PDF)
	baseDir := getAutoApplyDir()
	safeCompany := sanitizeFilename(company)
	outDir := filepath.Join(baseDir, "Company", safeCompany)
	os.MkdirAll(outDir, 0755)

	resumeHTML := filepath.Join(outDir, "resume.html")
	resumePDF := filepath.Join(outDir, "resume.pdf")
	coverHTML := filepath.Join(outDir, "cover.html")
	coverPDF := filepath.Join(outDir, "cover.pdf")

	// Fill Templates
	resTemplate := GetSetting(GlobalDB, KeyResumeTemplate)
	if resTemplate == "" {
		resTemplate = defaultResumeTemplate
	}
	covTemplate := GetSetting(GlobalDB, KeyCoverTemplate)
	if covTemplate == "" {
		covTemplate = defaultCoverTemplate
	}

	// Same two-pass fill as runStep3 — raw HTML sections rebuilt from
	// the current UserInfo + LLM-edited arrays in resumeJSON, then the
	// escaped {- KEY -} pass. This means the user can hand-edit the
	// JSON content and the structural entries still come from UserInfo.
	dbUserInfo, _ := GetUserInfo(GlobalDB)
	expBullets := coerceStringMatrix(resumeJSON["ExperienceBullets"])
	eduCoursework := coerceStringArray(resumeJSON["EducationCoursework"])
	projDescs := coerceStringArray(resumeJSON["ProjectDescriptions"])
	projIndices := coerceIntArray(resumeJSON["RelevantProjectIndices"])
	var experienceHTML, educationHTML, projectsHTML, contactHTML string
	if dbUserInfo != nil {
		experienceHTML = RenderExperienceSection(dbUserInfo.Experience, expBullets)
		educationHTML = RenderEducationSection(dbUserInfo.Education, eduCoursework)
		projectsHTML = RenderProjectsSection(dbUserInfo.Projects, projIndices, projDescs)
		contactHTML = renderContactLine(dbUserInfo)

		setPersonal := func(m map[string]any, k, v string) {
			if existing, ok := m[k]; ok && existing != nil {
				if s, isStr := existing.(string); isStr && s != "" {
					return
				}
			}
			m[k] = v
		}
		setPersonal(resumeJSON, "FullName", dbUserInfo.Name)
		setPersonal(resumeJSON, "Email", dbUserInfo.Email)
		setPersonal(resumeJSON, "Phone", dbUserInfo.Phone)
		setPersonal(resumeJSON, "Location", dbUserInfo.Location)
		setPersonal(resumeJSON, "LinkedIn", dbUserInfo.LinkedIn)
		setPersonal(resumeJSON, "GitHub", dbUserInfo.GitHub)
		setPersonal(coverJSON, "FullName", dbUserInfo.Name)
		setPersonal(coverJSON, "Email", dbUserInfo.Email)
		setPersonal(coverJSON, "Phone", dbUserInfo.Phone)
	}
	rawSections := map[string]string{
		"ExperienceSection": experienceHTML,
		"EducationSection":  educationHTML,
		"ProjectsSection":   projectsHTML,
		"ContactLine":       contactHTML,
	}

	resumeFilled := FillRawHTML(resTemplate, rawSections)
	resumeFilled = FillTemplateString(resumeFilled, resumeJSON)
	coverFilled := FillRawHTML(covTemplate, rawSections)
	coverFilled = FillTemplateString(coverFilled, coverJSON)

	os.WriteFile(resumeHTML, []byte(resumeFilled), 0644)
	os.WriteFile(coverHTML, []byte(coverFilled), 0644)

	if err := HtmlToPdf(resumeHTML, resumePDF); err != nil {
		LogError(GlobalDB, fmt.Sprintf("resume PDF failed: %v", err))
		return nil, fmt.Errorf("resume PDF generation failed: %v", err)
	}
	if err := HtmlToPdf(coverHTML, coverPDF); err != nil {
		LogError(GlobalDB, fmt.Sprintf("cover PDF failed: %v", err))
		return nil, fmt.Errorf("cover PDF generation failed: %v", err)
	}

	saveToDownloads(resumePDF, company, "resume.pdf")
	saveToDownloads(coverPDF, company, "cover.pdf")

	return &AutoApplyResult{
		Success:    true,
		ResumePath: resumePDF,
		CoverPath:  coverPDF,
		ResumeData: resumeJSON,
		CoverData:  coverJSON,
	}, nil
}
