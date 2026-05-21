package ui

import (
	model "32-Adarsha/model"
	"32-Adarsha/services"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func ShowAddJobPopup(app fyne.App, db *sql.DB, onSave func()) {
	popupWin := app.NewWindow("Add Job")

	linkEntry := widget.NewEntry()
	linkEntry.SetPlaceHolder("Paste job posting URL here...")

	descEntry := widget.NewMultiLineEntry()
	descEntry.SetPlaceHolder("OR paste job description here to skip scraping...")
	descEntry.SetMinRowsVisible(5)

	modelSelect := widget.NewRadioGroup([]string{"Gemini", "Claude", "OpenAI"}, nil)
	modelSelect.Horizontal = true
	modelSelect.SetSelected("Gemini") // Default selection

	companyEntry := widget.NewEntry()
	companyEntry.SetPlaceHolder("Optional: Company Name")
	roleEntry := widget.NewEntry()
	roleEntry.SetPlaceHolder("Optional: Role Name")

	statusLabel := widget.NewLabel("")
	statusLabel.Wrapping = fyne.TextWrapWord

	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	var mu sync.Mutex
	fetching := false

	fetchBtn := widget.NewButton("Fetch & Create", nil)
	fetchBtn.Importance = widget.HighImportance

	fetchBtn.OnTapped = func() {
		rawURL := strings.TrimSpace(linkEntry.Text)
		manualDesc := strings.TrimSpace(descEntry.Text)

		if rawURL == "" && manualDesc == "" {
			statusLabel.SetText("Please enter a URL or a Job Description.")
			return
		}

		if rawURL != "" && !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			rawURL = "https://" + rawURL
		}

		selectedModel := modelSelect.Selected // Capture chosen model

		mu.Lock()
		if fetching {
			mu.Unlock()
			return
		}
		fetching = true
		mu.Unlock()

		fetchBtn.Disable()
		progress.Show()
		statusLabel.SetText(fmt.Sprintf("Processing using %s…\nThis may take a few minutes.", selectedModel))

		go func() {
			defer func() {
				mu.Lock()
				fetching = false
				mu.Unlock()
				fyne.Do(func() {
					fetchBtn.Enable()
					progress.Hide()
				})
			}()

			// Add Job has no pre-known company/role — let RunAutoApply
			// extract them from the description.
			result, err := services.RunAutoApply(rawURL, manualDesc, "", "", selectedModel, func(msg string) {
				fyne.Do(func() {
					statusLabel.SetText(strings.TrimSpace(msg))
				})
			})

			if err != nil {
				fyne.Do(func() { statusLabel.SetText(fmt.Sprintf("Error: %v", err)) })
				return
			}
			if !result.Success {
				fyne.Do(func() { statusLabel.SetText(fmt.Sprintf("Script error: %s", result.Error)) })
				return
			}

			// Apply defaults if not found, prioritize manual input
			company := strings.TrimSpace(companyEntry.Text)
			if company == "" {
				company = result.Company
			}
			if company == "" || company == "Unknown_Company" {
				company = "Unknown Company"
			}

			role := strings.TrimSpace(roleEntry.Text)
			if role == "" {
				role = result.Role
			}
			if role == "" || role == "Unknown_Role" {
				role = "Unknown Role"
			}

			resData, _ := json.MarshalIndent(result.ResumeData, "", "  ")
			covData, _ := json.MarshalIndent(result.CoverData, "", "  ")

			newJob := model.Job{
				Company:     company,
				Role:        role,
				Link:        rawURL,
				Status:      "Processed",
				Description: result.Description,
				Resume:      result.ResumePath,
				Coverletter: result.CoverPath,
				ResumeData:  string(resData),
				CoverData:   string(covData),
			}

			_, dbErr := services.CreateJob(db, newJob)
			if dbErr != nil {
				fyne.Do(func() { statusLabel.SetText(fmt.Sprintf("DB error: %v", dbErr)) })
				return
			}

			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("✓ Created: %s — %s", company, role))
				onSave()
				popupWin.Close()
			})
		}()
	}

	addOnlyBtn := widget.NewButton("Add to List Only", func() {
		rawURL := strings.TrimSpace(linkEntry.Text)
		manualDesc := strings.TrimSpace(descEntry.Text)

		if rawURL == "" && manualDesc == "" {
			statusLabel.SetText("Please enter a URL or a Job Description.")
			return
		}

		if rawURL != "" && !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			rawURL = "https://" + rawURL
		}

		company := strings.TrimSpace(companyEntry.Text)
		if company == "" {
			company = "Pending Generation"
		}
		role := strings.TrimSpace(roleEntry.Text)
		if role == "" {
			role = "Manual Entry"
		}

		newJob := model.Job{
			Company:     company,
			Role:        role,
			Link:        rawURL,
			Status:      "Pending",
			Description: manualDesc,
		}

		_, dbErr := services.CreateJob(db, newJob)
		if dbErr != nil {
			statusLabel.SetText(fmt.Sprintf("DB error: %v", dbErr))
			return
		}

		onSave()
		popupWin.Close()
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		popupWin.Close()
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("Paste a job description to generate documents, or provide a link to fetch it automatically.", fyne.TextAlignLeading, fyne.TextStyle{}),
		widget.NewLabel("Job Description"),
		descEntry,
		container.NewGridWithColumns(2,
			container.NewVBox(widget.NewLabel("Company (Optional)"), companyEntry),
			container.NewVBox(widget.NewLabel("Role (Optional)"), roleEntry),
		),
		widget.NewLabel("Job URL (Optional)"),
		linkEntry,
		widget.NewLabel("AI Model"),
		modelSelect,
		container.NewGridWithColumns(3, fetchBtn, addOnlyBtn, cancelBtn),
		progress,
		statusLabel,
	)

	popupWin.SetContent(container.NewPadded(container.NewVScroll(content)))
	popupWin.Resize(fyne.NewSize(550, 600))
	popupWin.CenterOnScreen()
	popupWin.Show()
}
