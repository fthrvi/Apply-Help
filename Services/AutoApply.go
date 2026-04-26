package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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
	candidates := []string{
		"../AutoApply",
		"../../AutoApply",
		filepath.Join(os.Getenv("HOME"), "Desktop", "code", "Apply", "AutoApply"),
	}
	for _, c := range candidates {
		abs, _ := filepath.Abs(c)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	return "."
}

func sanitizeFilename(name string) string {
	reg, _ := regexp.Compile(`[^a-zA-Z0-9\-_\. ]+`)
	safe := reg.ReplaceAllString(name, "_")
	return strings.TrimSpace(safe)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func StripHTML(html string) string {
	// Remove script and style tags first
	reScript := regexp.MustCompile(`(?s)<script.*?>.*?</script>`)
	reStyle := regexp.MustCompile(`(?s)<style.*?>.*?</style>`)
	html = reScript.ReplaceAllString(html, " ")
	html = reStyle.ReplaceAllString(html, " ")

	// Remove all other tags
	reTags := regexp.MustCompile(`<[^>]*>`)
	text := reTags.ReplaceAllString(html, " ")

	// Collapse whitespace
	reSpace := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(reSpace.ReplaceAllString(text, " "))
}

func FillTemplate(templatePath, outputPath string, data map[string]any) error {
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}
	finalHTML := FillTemplateString(string(content), data)
	return os.WriteFile(outputPath, []byte(finalHTML), 0644)
}

func FillTemplateString(html string, data map[string]any) string {
	pattern := regexp.MustCompile(`\{-\s*(.*?)\s*-\}`)
	return pattern.ReplaceAllStringFunc(html, func(match string) string {
		keyword := strings.TrimSpace(match[2 : len(match)-2])
		if val, ok := data[keyword]; ok {
			return fmt.Sprintf("%v", val)
		}
		return match
	})
}

func HtmlToPdf(htmlPath, pdfPath string) error {
	// Use Chrome headless for better HTML rendering
	chromePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	cmd := exec.Command(chromePath, "--headless", "--disable-gpu", "--print-to-pdf="+pdfPath, htmlPath)
	return cmd.Run()
}

// ═════════════════════════════════════════════════════════════════════════════
// MAIN WORKFLOW
// ═════════════════════════════════════════════════════════════════════════════

// RunAutoApply handles the full sequence: Fetch (if needed) -> Extract -> Generate -> Fill -> PDF
func RunAutoApply(jobURL string, manualDesc string, modelChoice string, logFn func(string)) (*AutoApplyResult, error) {
	if logFn != nil {
		logFn("🚀 Starting Go-native workflow...\n")
	}

	var text string
	var err error

	if manualDesc != "" {
		if logFn != nil { logFn("📝 Using manually provided job description...\n") }
		text = manualDesc
	} else {
		// 1. Fetch HTML
		if logFn != nil { logFn("🔍 STEP 1: Fetching job page (via HTTP)...\n") }
		html, err := FetchHTML(jobURL)
		if err != nil {
			return nil, fmt.Errorf("fetch failed: %v", err)
		}
		text = StripHTML(html)
	}

	if len(text) > 12000 {
		text = text[:12000]
	}

	// 2. Extract Info
	if logFn != nil { logFn("🧠 STEP 2: Extracting role & company via LLM...\n") }
	extractPrompt := GetSetting(GlobalDB, "EXTRACTION_PROMPT")
	prompt := fmt.Sprintf(extractPrompt, text)
	res, err := PromptAI(prompt, modelChoice)
	if err != nil {
		return nil, fmt.Errorf("extraction failed: %v", err)
	}
	
	var info map[string]any
	if err := json.Unmarshal([]byte(res), &info); err != nil {
		// Attempt to extract JSON if model wrapped it in backticks
		reJSON := regexp.MustCompile(`(?s)\{.*\}`)
		if match := reJSON.FindString(res); match != "" {
			json.Unmarshal([]byte(match), &info)
		}
	}

	company := "Unknown Company"
	if v, ok := info["company_name"]; ok { company = fmt.Sprintf("%v", v) }
	role := "Unknown Role"
	if v, ok := info["role"]; ok { role = fmt.Sprintf("%v", v) }
	description := text
	if v, ok := info["job_description"]; ok { description = fmt.Sprintf("%v", v) }

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

	// Combined Generation
	if logFn != nil {
		logFn("  ✨ Generating Resume & Cover Letter in a single LLM call...\n")
	}
	combinedPromptTemplate := GetSetting(GlobalDB, "COMBINED_PROMPT")
	combinedSchema := GetSetting(GlobalDB, "COMBINED_SCHEMA")
	prompt := fmt.Sprintf(combinedPromptTemplate, description, userContext, combinedSchema)

	res, err := PromptAI(prompt, modelChoice)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %v", err)
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
		return nil, fmt.Errorf("failed to extract RESUME or COVER from combined response")
	}

	// Add date
	today := time.Now().Format("January 02, 2006")
	resumeJSON["Date"] = today
	coverJSON["Date"] = today

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

	resTemplate := GetSetting(GlobalDB, "RESUME_TEMPLATE")
	if resTemplate == "" {
		content, _ := os.ReadFile(filepath.Join(baseDir, "placeholder", "resume.html"))
		resTemplate = string(content)
	}

	covTemplate := GetSetting(GlobalDB, "COVER_TEMPLATE")
	if covTemplate == "" {
		content, _ := os.ReadFile(filepath.Join(baseDir, "placeholder", "cover.html"))
		covTemplate = string(content)
	}

	os.WriteFile(resumeHTML, []byte(FillTemplateString(resTemplate, resumeJSON)), 0644)
	os.WriteFile(coverHTML, []byte(FillTemplateString(covTemplate, coverJSON)), 0644)

	// Convert to PDF
	if logFn != nil { logFn("  📄 Converting to PDF (cupsfilter)...\n") }
	HtmlToPdf(resumeHTML, resumePDF)
	HtmlToPdf(coverHTML, coverPDF)

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
	resTemplate := GetSetting(GlobalDB, "RESUME_TEMPLATE")
	if resTemplate == "" {
		content, _ := os.ReadFile(filepath.Join(baseDir, "placeholder", "resume.html"))
		resTemplate = string(content)
	}
	covTemplate := GetSetting(GlobalDB, "COVER_TEMPLATE")
	if covTemplate == "" {
		content, _ := os.ReadFile(filepath.Join(baseDir, "placeholder", "cover.html"))
		covTemplate = string(content)
	}

	os.WriteFile(resumeHTML, []byte(FillTemplateString(resTemplate, resumeJSON)), 0644)
	os.WriteFile(coverHTML, []byte(FillTemplateString(covTemplate, coverJSON)), 0644)

	HtmlToPdf(resumeHTML, resumePDF)
	HtmlToPdf(coverHTML, coverPDF)

	return &AutoApplyResult{
		Success:    true,
		ResumePath: resumePDF,
		CoverPath:  coverPDF,
		ResumeData: resumeJSON,
		CoverData:  coverJSON,
	}, nil
}
