package ui

import (
	model "32-Adarsha/model"
	"32-Adarsha/services"
	"database/sql"
	"encoding/json"
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

// showImportResumeDialog opens a modal that lets the user pick a resume
// file, parse it via the configured LLM, preview the extracted UserInfo
// as pretty-printed JSON, and apply it to the profile via the supplied
// apply callback (which is responsible for refreshing the form widgets
// and persisting to the database).
func showImportResumeDialog(win fyne.Window, db *sql.DB, apply func(*model.UserInfo)) {
	_ = db // currently unused but kept for future "remember last model" persistence

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

	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	previewEntry := widget.NewMultiLineEntry()
	previewEntry.Wrapping = fyne.TextWrapWord
	previewEntry.SetMinRowsVisible(15)
	previewEntry.SetPlaceHolder("Parsed resume preview will appear here…")

	var parsedUI *model.UserInfo

	applyBtn := widget.NewButton("Apply to Profile", nil)
	applyBtn.Importance = widget.HighImportance
	applyBtn.Disable()

	parseBtn := widget.NewButton("Parse Resume", nil)
	parseBtn.Importance = widget.MediumImportance

	var customDialog dialog.Dialog

	parseBtn.OnTapped = func() {
		if chosenPath == "" {
			statusLabel.SetText("Pick a resume file first.")
			return
		}
		parseBtn.Disable()
		applyBtn.Disable()
		progress.Show()
		statusLabel.SetText(fmt.Sprintf("Extracting text from %s…", filepath.Base(chosenPath)))

		go func() {
			text, err := services.ExtractResumeText(chosenPath)
			if err != nil {
				fyne.Do(func() {
					progress.Hide()
					parseBtn.Enable()
					statusLabel.SetText(fmt.Sprintf("Extraction failed: %v", err))
				})
				return
			}
			if strings.TrimSpace(text) == "" {
				fyne.Do(func() {
					progress.Hide()
					parseBtn.Enable()
					statusLabel.SetText("File parsed but no text was found. Is this an image-only PDF?")
				})
				return
			}

			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("Sending %d chars to %s…", len(text), modelRadio.Selected))
			})

			ui, err := services.ParseResumeToUserInfo(text, modelRadio.Selected)
			if err != nil {
				fyne.Do(func() {
					progress.Hide()
					parseBtn.Enable()
					statusLabel.SetText(fmt.Sprintf("LLM parse failed: %v", err))
				})
				return
			}

			pretty, _ := json.MarshalIndent(ui, "", "  ")
			fyne.Do(func() {
				progress.Hide()
				parseBtn.Enable()
				parsedUI = ui
				previewEntry.SetText(string(pretty))
				applyBtn.Enable()
				statusLabel.SetText("Preview below. Review, then click Apply to overwrite your profile.")
			})
		}()
	}

	applyBtn.OnTapped = func() {
		if parsedUI == nil {
			return
		}
		// Re-parse from the edited preview text so the user can fix any
		// LLM mistakes in-line before applying.
		var edited model.UserInfo
		if err := json.Unmarshal([]byte(previewEntry.Text), &edited); err != nil {
			statusLabel.SetText(fmt.Sprintf("Edited JSON is invalid: %v", err))
			return
		}
		apply(&edited)
		if customDialog != nil {
			customDialog.Hide()
		}
	}

	// Border layout so the JSON preview (the only thing in the center) gets
	// every pixel not claimed by the controls above. A plain VBox would
	// collapse the VScroll to its minimum height and leave the bottom of
	// the dialog empty.
	header := container.NewVBox(
		widget.NewLabelWithStyle("Import Resume", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Pick a resume (.pdf, .docx, .txt, or .md). The selected LLM will read it and fill in your profile fields. You can edit the JSON preview before applying."),
		container.NewBorder(nil, nil, nil, browseBtn, filePathLabel),
		widget.NewLabel("LLM"),
		modelRadio,
		container.NewGridWithColumns(2, parseBtn, applyBtn),
		progress,
		statusLabel,
		widget.NewSeparator(),
		widget.NewLabel("Parsed JSON (editable)"),
	)
	content := container.NewBorder(header, nil, nil, nil, container.NewVScroll(previewEntry))

	customDialog = dialog.NewCustom("Import Resume", "Close", content, win)
	customDialog.Resize(fyne.NewSize(900, 800))
	customDialog.Show()
}
