package services

import (
	model "32-Adarsha/model"
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// ExtractResumeText opens path and returns its plaintext content. It
// dispatches on file extension: .pdf via ledongthuc/pdf, .docx via a
// pure-Go zip+XML walk, and .txt via os.ReadFile. Any other extension is
// rejected so an LLM doesn't get fed binary garbage.
func ExtractResumeText(path string) (string, error) {
	return ExtractResumeTextWithPassword(path, "")
}

// ExtractResumeTextWithPassword is the encryption-aware variant. When
// the PDF is encrypted, pdfcpu decrypts it to a temp file using the
// supplied password and the temp file is what gets text-extracted. An
// empty password is fine for unencrypted PDFs. The temp file is
// removed before return.
//
// Errors from this path bubble up with the underlying message so the
// UI can detect "encrypted PDF" / "invalid password" and prompt the
// user to enter a password.
func ExtractResumeTextWithPassword(path, password string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pdf":
		// pdfcpu validates + decrypts. If the file is already
		// unencrypted, decrypt is a no-op and we fall through to
		// extractPDFText on the original.
		conf := pdfmodel.NewDefaultConfiguration()
		if password != "" {
			conf.UserPW = password
			conf.OwnerPW = password
		}

		encrypted, encErr := IsPDFEncrypted(path)
		if encErr != nil {
			// Not fatal — proceed and let extractPDFText raise the
			// real error.
			return extractPDFText(path)
		}
		if !encrypted {
			return extractPDFText(path)
		}

		tmp, err := os.CreateTemp("", "applyhelp-decrypt-*.pdf")
		if err != nil {
			return "", err
		}
		tmpPath := tmp.Name()
		tmp.Close()
		defer os.Remove(tmpPath)

		if err := pdfapi.DecryptFile(path, tmpPath, conf); err != nil {
			return "", fmt.Errorf("decrypt pdf: %w", err)
		}
		return extractPDFText(tmpPath)
	case ".docx":
		return extractDocxText(path)
	case ".txt", ".md":
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("unsupported file type %q (expected .pdf, .docx, .txt, or .md)", ext)
	}
}

// IsPDFEncrypted returns true when the PDF requires a password to read.
// Used by the UI to surface a password prompt instead of failing
// blindly. pdfcpu's Validate is the authoritative check; we treat any
// "encrypted" string in its error as positive identification.
func IsPDFEncrypted(path string) (bool, error) {
	conf := pdfmodel.NewDefaultConfiguration()
	err := pdfapi.ValidateFile(path, conf)
	if err == nil {
		return false, nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "encrypted") || strings.Contains(msg, "password") {
		return true, nil
	}
	// Other validation errors aren't our concern here.
	return false, err
}

func extractPDFText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	var buf strings.Builder
	totalPage := r.NumPage()
	for i := 1; i <= totalPage; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("read page %d: %w", i, err)
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}
	return buf.String(), nil
}

// extractDocxText pulls the body text out of a .docx file. A docx is a zip
// archive whose word/document.xml holds the content; the actual visible
// text lives in <w:t> elements. We walk the XML and concatenate them,
// inserting newlines on paragraph (<w:p>) boundaries.
func extractDocxText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer zr.Close()

	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("docx missing word/document.xml")
	}

	rc, err := doc.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	var buf strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse docx xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				var s string
				if err := dec.DecodeElement(&s, &t); err == nil {
					buf.WriteString(s)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				buf.WriteString("\n")
			}
		}
	}
	return buf.String(), nil
}

// ParseResumeToUserInfo sends the extracted resume text to the configured
// LLM with the resume-parse prompt and unmarshals the response into a
// UserInfo. modelChoice mirrors the existing PromptAI dropdown values
// ("gemini" / "claude" / "openai").
func ParseResumeToUserInfo(resumeText, modelChoice string) (*model.UserInfo, error) {
	if strings.TrimSpace(resumeText) == "" {
		return nil, fmt.Errorf("resume text is empty — extraction may have failed")
	}

	tpl := GetSetting(GlobalDB, KeyResumeParsePrompt)
	if tpl == "" {
		tpl = defaultResumeParsePrompt
	}
	// Cap to keep the prompt within reasonable bounds for cheap models.
	if len(resumeText) > 30000 {
		resumeText = resumeText[:30000]
	}
	prompt := fmt.Sprintf(tpl, resumeText)

	raw, err := PromptAI(prompt, modelChoice)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	// The LLM may wrap the JSON in markdown fences or prose despite the
	// prompt; extract the first {...} block as a fallback.
	jsonStr := strings.TrimSpace(raw)
	if !strings.HasPrefix(jsonStr, "{") {
		re := regexp.MustCompile(`(?s)\{.*\}`)
		if m := re.FindString(jsonStr); m != "" {
			jsonStr = m
		}
	}

	var ui model.UserInfo
	if err := json.Unmarshal([]byte(jsonStr), &ui); err != nil {
		return nil, fmt.Errorf("parse JSON: %w\nRaw response:\n%s", err, raw)
	}
	return &ui, nil
}
