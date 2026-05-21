package ui

import (
	model "32-Adarsha/model"
	"32-Adarsha/services"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showImportTranscriptDialog opens a modal that:
//  1. Lets the user pick a transcript file (.pdf/.docx/.txt/.md).
//  2. Extracts text — when the PDF is encrypted, reveals a password
//     field and re-extracts via pdfcpu decryption.
//  3. Runs ParseTranscriptToEducationMap with the candidate's Education
//     entries so the LLM groups courses by institution/degree.
//  4. Shows the grouped preview (one section per Education entry, each
//     editable) and applies the courses to each entry's Transcript via
//     the apply callback.
//
// The apply callback receives a map of zero-based Education index →
// course list. The caller is responsible for updating the corresponding
// model.Education[idx].Transcript and refreshing the form.
func showImportTranscriptDialog(win fyne.Window, db *sql.DB, educations []model.Education, apply func(map[int][]string)) {
	_ = db

	filePathLabel := widget.NewLabel("(no file selected)")
	filePathLabel.Wrapping = fyne.TextWrapWord
	var chosenPath string

	browseBtn := widget.NewButtonWithIcon("Browse…", theme.FolderOpenIcon(), func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			defer reader.Close()
			chosenPath = reader.URI().Path()
			filePathLabel.SetText(filepath.Base(chosenPath))
		}, win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".pdf", ".docx", ".txt", ".md"}))
		fd.Show()
	})

	modelRadio := widget.NewRadioGroup([]string{"Gemini", "Claude", "OpenAI"}, nil)
	modelRadio.Horizontal = true
	modelRadio.SetSelected("Gemini")

	// Password field, hidden until extraction reports encryption.
	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("PDF password (leave empty if not encrypted)")
	passwordRow := container.NewVBox(
		widget.NewLabel("Password (for encrypted PDFs):"),
		passwordEntry,
	)
	passwordRow.Hide()

	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	// Per-Education preview area — populated after a successful parse.
	previewByIdx := map[int]*widget.Entry{}
	previewContainer := container.NewVBox()

	var customDialog dialog.Dialog
	applyBtn := widget.NewButton("Apply to Education Entries", nil)
	applyBtn.Importance = widget.HighImportance
	applyBtn.Disable()

	parseBtn := widget.NewButton("Parse Transcript", nil)
	parseBtn.Importance = widget.MediumImportance

	runParse := func() {
		if chosenPath == "" {
			statusLabel.SetText("Pick a transcript file first.")
			return
		}
		if len(educations) == 0 {
			statusLabel.SetText("Add at least one Education entry first.")
			return
		}
		parseBtn.Disable()
		applyBtn.Disable()
		progress.Show()
		password := passwordEntry.Text
		modelChoice := strings.ToLower(modelRadio.Selected)

		statusLabel.SetText(fmt.Sprintf("Extracting text from %s…", filepath.Base(chosenPath)))

		go func() {
			text, err := services.ExtractResumeTextWithPassword(chosenPath, password)
			if err != nil {
				msg := strings.ToLower(err.Error())
				isEnc := strings.Contains(msg, "encrypted") || strings.Contains(msg, "password")
				fyne.Do(func() {
					progress.Hide()
					parseBtn.Enable()
					if isEnc {
						passwordRow.Show()
						if password == "" {
							statusLabel.SetText("This PDF is encrypted. Enter the password and click Parse again.")
						} else {
							statusLabel.SetText("Decryption failed: wrong password. Try again.")
						}
					} else {
						statusLabel.SetText(fmt.Sprintf("Extraction failed: %v", err))
					}
				})
				return
			}
			if strings.TrimSpace(text) == "" {
				fyne.Do(func() {
					progress.Hide()
					parseBtn.Enable()
					statusLabel.SetText("File parsed but no text was found. Image-only PDF?")
				})
				return
			}

			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("Parsing %d chars with %s…", len(text), modelRadio.Selected))
			})

			byIdx, perr := services.ParseTranscriptToEducationMap(text, educations, modelChoice)
			if perr != nil {
				fyne.Do(func() {
					progress.Hide()
					parseBtn.Enable()
					statusLabel.SetText(fmt.Sprintf("LLM parse failed: %v", perr))
				})
				return
			}

			fyne.Do(func() {
				progress.Hide()
				parseBtn.Enable()
				previewContainer.RemoveAll()
				previewByIdx = map[int]*widget.Entry{}

				total := 0
				for i, edu := range educations {
					courses := byIdx[i]
					total += len(courses)

					entry := widget.NewMultiLineEntry()
					entry.SetText(strings.Join(courses, "\n"))
					entry.SetMinRowsVisible(6)
					entry.Wrapping = fyne.TextWrapWord
					if len(courses) == 0 {
						entry.SetPlaceHolder("(no courses matched to this entry)")
					}
					previewByIdx[i] = entry

					title := edu.Institution + " — " + edu.Degree
					previewContainer.Add(widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
					previewContainer.Add(entry)
					previewContainer.Add(widget.NewSeparator())
				}
				statusLabel.SetText(fmt.Sprintf("Parsed %d course(s) across %d entry(ies). Edit if needed, then Apply.", total, len(educations)))
				if total > 0 {
					applyBtn.Enable()
				}
			})
		}()
	}

	parseBtn.OnTapped = runParse

	applyBtn.OnTapped = func() {
		out := map[int][]string{}
		for idx, entry := range previewByIdx {
			lines := strings.Split(entry.Text, "\n")
			cleaned := make([]string, 0, len(lines))
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" {
					cleaned = append(cleaned, l)
				}
			}
			out[idx] = cleaned
		}
		if apply != nil {
			apply(out)
		}
		customDialog.Hide()
	}

	cancelBtn := widget.NewButton("Cancel", func() { customDialog.Hide() })

	body := container.NewVBox(
		widget.NewLabel("Import courses from a transcript file. The LLM matches each course to one of your Education entries by institution and degree."),
		container.NewBorder(nil, nil, nil, browseBtn, filePathLabel),
		widget.NewLabel("Model:"), modelRadio,
		passwordRow,
		container.NewGridWithColumns(2, parseBtn, applyBtn),
		progress, statusLabel,
		widget.NewSeparator(),
		widget.NewLabel("Preview (grouped by Education entry, editable):"),
		previewContainer,
		container.NewPadded(cancelBtn),
	)

	customDialog = dialog.NewCustomWithoutButtons("Import Transcript", container.NewVScroll(container.NewPadded(body)), win)
	customDialog.Resize(fyne.NewSize(720, 640))
	customDialog.Show()
}
