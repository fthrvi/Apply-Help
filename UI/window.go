package ui

import (
	model "32-Adarsha/Model"
	"32-Adarsha/services"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func ShowAddJobPopup(app fyne.App, db *sql.DB, onSave func()) {
	popupWin := app.NewWindow("Add Job")

	linkEntry := widget.NewEntry()
	linkEntry.SetPlaceHolder("Paste job posting URL here...")

	descEntry := widget.NewMultiLineEntry()
	descEntry.SetPlaceHolder("OR paste job description here to skip scraping...")
	descEntry.SetMinRowsVisible(5)

	// Add Model Selection Dropdown
	modelSelect := widget.NewSelect([]string{"Gemini", "Claude", "OpenAI"}, nil)
	modelSelect.SetSelected("Gemini") // Default selection

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

			// Passed selectedModel and manualDesc to the service
			result, err := services.RunAutoApply(rawURL, manualDesc, selectedModel, func(msg string) {
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

			// Apply defaults if not found
			company := result.Company
			if company == "" || company == "Unknown_Company" {
				company = "Unknown Company"
			}
			role := result.Role
			if role == "" || role == "Unknown_Role" {
				role = "Unknown Role"
			}

			resData, _ := json.MarshalIndent(result.ResumeData, "", "  ")
			covData, _ := json.MarshalIndent(result.CoverData, "", "  ")

			newJob := model.Job{
				Company:     company,
				Role:        role,
				Link:        rawURL,
				Status:      "Pending",
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

	cancelBtn := widget.NewButton("Cancel", func() {
		popupWin.Close()
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("Paste a link OR a job description to generate your resume & cover letter.", fyne.TextAlignLeading, fyne.TextStyle{}),
		widget.NewLabel("Job URL (Optional if description provided)"),
		linkEntry,
		widget.NewLabel("Job Description (Optional if URL provided)"),
		descEntry,
		widget.NewLabel("AI Model"),
		modelSelect,
		container.NewHBox(fetchBtn, cancelBtn),
		progress,
		statusLabel,
	)

	popupWin.SetContent(container.NewPadded(content))
	popupWin.Resize(fyne.NewSize(550, 480))
	popupWin.CenterOnScreen()
	popupWin.Show()
}

func BuildSettingsView(db *sql.DB, onBack func()) fyne.CanvasObject {
	// Left Sidebar
	categories := []string{"User Profile", "API Keys", "System Prompts", "Output Schemas", "HTML Templates"}
	list := widget.NewList(
		func() int { return len(categories) },
		func() fyne.CanvasObject { return widget.NewLabel("Template") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(categories[id])
		},
	)

	rightSide := container.NewStack()

	// 1. API Keys View
	geminiKey := widget.NewEntry()
	geminiKey.SetText(services.GetSetting(db, "GEMINI_API_KEY"))
	geminiURL := widget.NewEntry()
	geminiURL.SetText(services.GetSetting(db, "GEMINI_URL"))
	if geminiURL.Text == "" {
		geminiURL.SetText("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent")
	}
	claudeKey := widget.NewEntry()
	claudeKey.SetText(services.GetSetting(db, "CLAUDE_API_KEY"))
	openaiKey := widget.NewEntry()
	openaiKey.SetText(services.GetSetting(db, "OPENAI_API_KEY"))

	keysSaveBtn := widget.NewButton("Save API Keys", func() {
		services.SaveSetting(db, "GEMINI_API_KEY", geminiKey.Text)
		services.SaveSetting(db, "GEMINI_URL", geminiURL.Text)
		services.SaveSetting(db, "CLAUDE_API_KEY", claudeKey.Text)
		services.SaveSetting(db, "OPENAI_API_KEY", openaiKey.Text)
	})
	keysSaveBtn.Importance = widget.HighImportance

	keysForm := container.NewVScroll(container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("API Key Configuration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Gemini API Key"), geminiKey,
		widget.NewLabel("Gemini Endpoint URL"), geminiURL,
		widget.NewLabel("Claude API Key"), claudeKey,
		widget.NewLabel("OpenAI/NVIDIA API Key"), openaiKey,
		container.NewPadded(keysSaveBtn),
	)))

	// 2. Prompts View
	extractPrompt := widget.NewMultiLineEntry()
	extractPrompt.SetText(services.GetSetting(db, "EXTRACTION_PROMPT"))

	combinedPrompt := widget.NewMultiLineEntry()
	combinedPrompt.SetText(services.GetSetting(db, "COMBINED_PROMPT"))

	promptsSaveBtn := widget.NewButton("Save All Prompts", func() {
		services.SaveSetting(db, "EXTRACTION_PROMPT", extractPrompt.Text)
		services.SaveSetting(db, "COMBINED_PROMPT", combinedPrompt.Text)
	})
	promptsSaveBtn.Importance = widget.HighImportance

	promptTabs := container.NewAppTabs(
		container.NewTabItem("Extraction", container.NewPadded(extractPrompt)),
		container.NewTabItem("Combined Document", container.NewPadded(combinedPrompt)),
	)

	promptsForm := container.NewBorder(nil, container.NewPadded(promptsSaveBtn), nil, nil, promptTabs)

	// 3. User Profile View
	ui, _ := services.GetUserInfo(db)

	nameEntry := widget.NewEntry()
	nameEntry.SetText(ui.Name)
	emailEntry := widget.NewEntry()
	emailEntry.SetText(ui.Email)
	phoneEntry := widget.NewEntry()
	phoneEntry.SetText(ui.Phone)
	locationEntry := widget.NewEntry()
	locationEntry.SetText(ui.Location)

	personalInfo := container.NewVBox(
		widget.NewLabelWithStyle("Personal Information", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Full Name"), nameEntry,
		widget.NewLabel("Email"), emailEntry,
		widget.NewLabel("Phone"), phoneEntry,
		widget.NewLabel("Location"), locationEntry,
	)

	// Skills
	languagesEntry := widget.NewEntry()
	languagesEntry.SetText(strings.Join(ui.Skills.Languages, ", "))
	frameworksEntry := widget.NewEntry()
	frameworksEntry.SetText(strings.Join(ui.Skills.Frameworks, ", "))
	devToolsEntry := widget.NewEntry()
	devToolsEntry.SetText(strings.Join(ui.Skills.DevTools, ", "))
	databasesEntry := widget.NewEntry()
	databasesEntry.SetText(strings.Join(ui.Skills.Databases, ", "))

	skillsInfo := container.NewVBox(
		widget.NewLabelWithStyle("Skills & Technologies", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Languages"), languagesEntry,
		widget.NewLabel("Frameworks"), frameworksEntry,
		widget.NewLabel("Dev Tools"), devToolsEntry,
		widget.NewLabel("Databases"), databasesEntry,
	)

	awardsEntry := widget.NewMultiLineEntry()
	awardsEntry.SetText(strings.Join(ui.Awards, "\n"))
	awardsEntry.SetMinRowsVisible(10)
	awardsInfo := container.NewVBox(
		widget.NewLabelWithStyle("Awards & Certifications", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Awards (one per line)"), awardsEntry,
	)

	expAccordion := widget.NewAccordion()
	var renderExperiences func()
	renderExperiences = func() {
		expAccordion.Items = nil
		for i, exp := range ui.Experience {
			idx := i
			company := widget.NewEntry()
			company.SetText(exp.Company)
			title := widget.NewEntry()
			title.SetText(exp.Title)
			bullets := widget.NewMultiLineEntry()
			bullets.SetText(strings.Join(exp.Bullets, "\n"))
			bullets.SetMinRowsVisible(10)
			keywords := widget.NewEntry()
			keywords.SetText(strings.Join(exp.Keywords, ", "))

			removeBtn := widget.NewButtonWithIcon("Remove This Experience", theme.DeleteIcon(), func() {
				ui.Experience = append(ui.Experience[:idx], ui.Experience[idx+1:]...)
				renderExperiences()
			})
			removeBtn.Importance = widget.DangerImportance

			itemContent := container.NewVBox(
				widget.NewLabel("Company"), company,
				widget.NewLabel("Title"), title,
				widget.NewLabel("Keywords (comma separated)"), keywords,
				widget.NewLabel("Bullets (one per line)"), bullets,
				container.NewPadded(removeBtn),
			)

			item := widget.NewAccordionItem(exp.Company+" — "+exp.Title, itemContent)
			expAccordion.Append(item)

			company.OnChanged = func(s string) {
				ui.Experience[idx].Company = s
				item.Title = s + " — " + ui.Experience[idx].Title
				expAccordion.Refresh()
			}
			title.OnChanged = func(s string) {
				ui.Experience[idx].Title = s
				item.Title = ui.Experience[idx].Company + " — " + s
				expAccordion.Refresh()
			}
			keywords.OnChanged = func(s string) { ui.Experience[idx].Keywords = strings.Split(s, ", ") }
			bullets.OnChanged = func(s string) { ui.Experience[idx].Bullets = strings.Split(s, "\n") }
		}
	}
	renderExperiences()

	addExpBtn := widget.NewButtonWithIcon("Add New Experience", theme.ContentAddIcon(), func() {
		ui.Experience = append(ui.Experience, model.Experience{Company: "New Company", Title: "New Title"})
		renderExperiences()
	})

	projAccordion := widget.NewAccordion()
	var renderProjects func()
	renderProjects = func() {
		projAccordion.Items = nil
		for i, proj := range ui.Projects {
			idx := i
			name := widget.NewEntry()
			name.SetText(proj.Name)
			tech := widget.NewEntry()
			tech.SetText(strings.Join(proj.Technologies, ", "))
			bullets := widget.NewMultiLineEntry()
			bullets.SetText(strings.Join(proj.Bullets, "\n"))
			bullets.SetMinRowsVisible(10)

			removeBtn := widget.NewButtonWithIcon("Remove This Project", theme.DeleteIcon(), func() {
				ui.Projects = append(ui.Projects[:idx], ui.Projects[idx+1:]...)
				renderProjects()
			})
			removeBtn.Importance = widget.DangerImportance

			itemContent := container.NewVBox(
				widget.NewLabel("Project Name"), name,
				widget.NewLabel("Technologies (comma separated)"), tech,
				widget.NewLabel("Bullets (one per line)"), bullets,
				container.NewPadded(removeBtn),
			)

			item := widget.NewAccordionItem(proj.Name, itemContent)
			projAccordion.Append(item)

			name.OnChanged = func(s string) {
				ui.Projects[idx].Name = s
				item.Title = s
				projAccordion.Refresh()
			}
			tech.OnChanged = func(s string) { ui.Projects[idx].Technologies = strings.Split(s, ", ") }
			bullets.OnChanged = func(s string) { ui.Projects[idx].Bullets = strings.Split(s, "\n") }
		}
	}
	renderProjects()

	addProjBtn := widget.NewButtonWithIcon("Add New Project", theme.ContentAddIcon(), func() {
		ui.Projects = append(ui.Projects, model.Project{Name: "New Project"})
		renderProjects()
	})

	eduAccordion := widget.NewAccordion()
	var renderEducation func()
	renderEducation = func() {
		eduAccordion.Items = nil
		for i, edu := range ui.Education {
			idx := i
			school := widget.NewEntry()
			school.SetText(edu.Institution)
			degree := widget.NewEntry()
			degree.SetText(edu.Degree)
			coursework := widget.NewMultiLineEntry()
			coursework.SetText(strings.Join(edu.Coursework, "\n"))
			coursework.SetMinRowsVisible(10)

			removeBtn := widget.NewButtonWithIcon("Remove This Education", theme.DeleteIcon(), func() {
				ui.Education = append(ui.Education[:idx], ui.Education[idx+1:]...)
				renderEducation()
			})
			removeBtn.Importance = widget.DangerImportance

			itemContent := container.NewVBox(
				widget.NewLabel("Institution"), school,
				widget.NewLabel("Degree"), degree,
				widget.NewLabel("Coursework (one per line)"), coursework,
				container.NewPadded(removeBtn),
			)

			item := widget.NewAccordionItem(edu.Institution+" — "+edu.Degree, itemContent)
			eduAccordion.Append(item)

			school.OnChanged = func(s string) {
				ui.Education[idx].Institution = s
				item.Title = s + " — " + ui.Education[idx].Degree
				eduAccordion.Refresh()
			}
			degree.OnChanged = func(s string) {
				ui.Education[idx].Degree = s
				item.Title = ui.Education[idx].Institution + " — " + s
				eduAccordion.Refresh()
			}
			coursework.OnChanged = func(s string) { ui.Education[idx].Coursework = strings.Split(s, "\n") }
		}
	}
	renderEducation()

	addEduBtn := widget.NewButtonWithIcon("Add New Education", theme.ContentAddIcon(), func() {
		ui.Education = append(ui.Education, model.Education{Institution: "New Institution", Degree: "New Degree"})
		renderEducation()
	})

	saveProfileBtn := widget.NewButton("Save All Profile Data", func() {
		ui.Name = nameEntry.Text
		ui.Email = emailEntry.Text
		ui.Phone = phoneEntry.Text
		ui.Location = locationEntry.Text
		ui.Skills.Languages = strings.Split(languagesEntry.Text, ", ")
		ui.Skills.Frameworks = strings.Split(frameworksEntry.Text, ", ")
		ui.Skills.DevTools = strings.Split(devToolsEntry.Text, ", ")
		ui.Skills.Databases = strings.Split(databasesEntry.Text, ", ")
		ui.Awards = strings.Split(awardsEntry.Text, "\n")
		services.SaveUserInfo(db, ui)
	})
	saveProfileBtn.Importance = widget.HighImportance

	profileTabs := container.NewAppTabs(
		container.NewTabItem("Personal Info", container.NewVScroll(container.NewPadded(personalInfo))),
		container.NewTabItem("Experience", container.NewVScroll(container.NewPadded(container.NewVBox(expAccordion, addExpBtn)))),
		container.NewTabItem("Project", container.NewVScroll(container.NewPadded(container.NewVBox(projAccordion, addProjBtn)))),
		container.NewTabItem("Education", container.NewVScroll(container.NewPadded(container.NewVBox(eduAccordion, addEduBtn)))),
		container.NewTabItem("Skills & Tech", container.NewVScroll(container.NewPadded(skillsInfo))),
		container.NewTabItem("Awards", container.NewVScroll(container.NewPadded(awardsInfo))),
	)

	profileView := container.NewBorder(nil, container.NewPadded(saveProfileBtn), nil, nil, profileTabs)

	// 4. Output Schemas View
	combinedSchemaEntry := widget.NewMultiLineEntry()
	combinedSchemaEntry.SetText(services.GetSetting(db, "COMBINED_SCHEMA"))

	schemasSaveBtn := widget.NewButton("Save Schemas", func() {
		services.SaveSetting(db, "COMBINED_SCHEMA", combinedSchemaEntry.Text)
	})
	schemasSaveBtn.Importance = widget.HighImportance

	schemaHeader := container.NewVBox(
		widget.NewLabelWithStyle("Combined Output Schema (JSON)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Define the JSON structure for Resume & Cover generation:"),
	)

	schemasView := container.NewBorder(nil, container.NewPadded(schemasSaveBtn), nil, nil,
		container.NewPadded(container.NewBorder(schemaHeader, nil, nil, nil, combinedSchemaEntry)))

	// 5. HTML Templates View
	resTemplateEntry := widget.NewMultiLineEntry()
	resTemplateEntry.SetText(services.GetSetting(db, "RESUME_TEMPLATE"))

	covTemplateEntry := widget.NewMultiLineEntry()
	covTemplateEntry.SetText(services.GetSetting(db, "COVER_TEMPLATE"))

	templateSaveBtn := widget.NewButton("Save Templates", func() {
		services.SaveSetting(db, "RESUME_TEMPLATE", resTemplateEntry.Text)
		services.SaveSetting(db, "COVER_TEMPLATE", covTemplateEntry.Text)
	})
	templateSaveBtn.Importance = widget.HighImportance

	templateTabs := container.NewAppTabs(
		container.NewTabItem("Resume Template", container.NewPadded(resTemplateEntry)),
		container.NewTabItem("Cover Template", container.NewPadded(covTemplateEntry)),
	)

	templatesView := container.NewBorder(nil, container.NewPadded(templateSaveBtn), nil, nil, templateTabs)

	list.OnSelected = func(id widget.ListItemID) {
		rightSide.Objects = nil
		if id == 0 {
			rightSide.Add(profileView)
		} else if id == 1 {
			rightSide.Add(keysForm)
		} else if id == 2 {
			rightSide.Add(promptsForm)
		} else if id == 3 {
			rightSide.Add(schemasView)
		} else if id == 4 {
			rightSide.Add(templatesView)
		}
		rightSide.Refresh()
	}
	list.Select(0)

	backBtn := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), onBack)
	header := container.NewBorder(nil, nil, backBtn, nil, widget.NewLabelWithStyle("App Settings", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}))

	split := container.NewHSplit(list, rightSide)
	split.Offset = 0.2

	return container.NewBorder(header, nil, nil, nil, split)
}


func renderPDFToCanvas(pdfPath string) fyne.CanvasObject {
	if pdfPath == "" {
		return widget.NewLabel("No PDF provided")
	}

	// Use a container that we can update later
	containerObj := container.NewStack(widget.NewProgressBarInfinite())

	go func() {
		fmt.Printf("🔍 Preview: Checking file %s\n", pdfPath)
		stat, err := os.Stat(pdfPath)
		if os.IsNotExist(err) {
			fmt.Printf("❌ Preview: File not found %s\n", pdfPath)
			fyne.Do(func() {
				containerObj.Objects = []fyne.CanvasObject{widget.NewLabel("File not found: " + pdfPath)}
				containerObj.Refresh()
			})
			return
		}
		if stat.Size() == 0 {
			fmt.Printf("❌ Preview: File is empty (0B) %s\n", pdfPath)
			fyne.Do(func() {
				containerObj.Objects = []fyne.CanvasObject{widget.NewLabel("PDF is empty (0B). Check generation.")}
				containerObj.Refresh()
			})
			return
		}

		outDir := "/tmp"
		fmt.Printf("🔨 Preview: Running qlmanage for %s\n", pdfPath)
		cmd := exec.Command("qlmanage", "-t", "-s", "1080", "-o", outDir, pdfPath)
		err = cmd.Run()
		if err != nil {
			fmt.Printf("❌ Preview: qlmanage failed: %v\n", err)
		}

		baseName := filepath.Base(pdfPath)
		pngPath := filepath.Join(outDir, baseName+".png")
		fmt.Printf("🖼️ Preview: Looking for PNG %s\n", pngPath)

		fyne.Do(func() {
			containerObj.Objects = nil
			if _, err := os.Stat(pngPath); os.IsNotExist(err) {
				fmt.Printf("❌ Preview: PNG missing %s\n", pngPath)
				containerObj.Add(widget.NewLabel("Preview generation failed for: " + pdfPath))
			} else {
				fmt.Printf("✅ Preview: Loading PNG %s\n", pngPath)
				img := canvas.NewImageFromFile(pngPath)
				img.FillMode = canvas.ImageFillContain
				containerObj.Add(img)
			}
			containerObj.Refresh()
		})
	}()

	return containerObj
}

func renderQuestions(description string, jsonStr string) fyne.CanvasObject {
	vbox := container.NewVBox()

	if description != "" {
		descLabel := widget.NewLabelWithStyle("Job Description", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		descContent := widget.NewLabel(description)
		descContent.Wrapping = fyne.TextWrapWord
		vbox.Add(descLabel)
		vbox.Add(descContent)
		vbox.Add(widget.NewSeparator())
	}

	if jsonStr == "" {
		vbox.Add(widget.NewLabel("No questions provided"))
		return container.NewVScroll(vbox)
	}

	var qaList []map[string]string
	err := json.Unmarshal([]byte(jsonStr), &qaList)
	if err != nil {
		vbox.Add(widget.NewLabel("Failed to parse questions JSON: " + err.Error() + "\nRaw: " + jsonStr))
		return container.NewVScroll(vbox)
	}

	for _, qa := range qaList {
		qLabel := widget.NewLabelWithStyle(qa["q"], fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		qLabel.Wrapping = fyne.TextWrapWord
		aLabel := widget.NewLabel(qa["a"])
		aLabel.Wrapping = fyne.TextWrapWord
		vbox.Add(qLabel)
		vbox.Add(aLabel)
		vbox.Add(widget.NewSeparator())
	}

	return container.NewVScroll(vbox)
}

func buildJSONEditor(jsonStr string) (fyne.CanvasObject, func() string) {
	// Try to pretty print it first
	var data map[string]any
	_ = json.Unmarshal([]byte(jsonStr), &data)
	if data != nil {
		indent, _ := json.MarshalIndent(data, "", "  ")
		jsonStr = string(indent)
	}

	entry := widget.NewMultiLineEntry()
	entry.SetText(jsonStr)
	entry.Wrapping = fyne.TextWrapWord
	entry.SetMinRowsVisible(20)

	return container.NewVScroll(container.NewPadded(entry)), func() string {
		return entry.Text
	}
}

func BuildEditJobView(db *sql.DB, job model.Job, onSave func(), onCancel func()) fyne.CanvasObject {
	var resumeDataEntry *widget.Entry
	var coverDataEntry *widget.Entry

	company := widget.NewEntry()
	company.SetText(job.Company)

	role := widget.NewEntry()
	role.SetText(job.Role)

	link := widget.NewEntry()
	link.SetText(job.Link)

	openLinkBtn := widget.NewButtonWithIcon("", theme.MailForwardIcon(), func() {
		rawURL := link.Text
		if rawURL != "" {
			if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
				rawURL = "https://" + rawURL
			}
			if parsedUrl, err := url.Parse(rawURL); err == nil {
				fyne.CurrentApp().OpenURL(parsedUrl)
			}
		}
	})

	linkBox := container.NewBorder(nil, nil, nil, openLinkBtn, link)

	status := widget.NewSelectEntry([]string{"Pending", "Applied", "Interview", "Rejected", "Offer"})
	status.SetText(job.Status)
	status.Wrapping = fyne.TextWrapWord

	description := widget.NewMultiLineEntry()
	description.SetText(job.Description)
	description.Wrapping = fyne.TextWrapWord

	resume := widget.NewMultiLineEntry()
	resume.SetText(job.Resume)
	resume.Wrapping = fyne.TextWrapWord
	resume.SetMinRowsVisible(2)

	coverLetter := widget.NewMultiLineEntry()
	coverLetter.SetText(job.Coverletter)
	coverLetter.Wrapping = fyne.TextWrapWord
	coverLetter.SetMinRowsVisible(2)

	question := widget.NewMultiLineEntry()
	question.SetText(job.Question)
	question.Wrapping = fyne.TextWrapWord

	submitBtn := widget.NewButton("Submit", func() {
		job.Company = company.Text
		job.Role = role.Text
		job.Link = link.Text
		job.Status = status.Text
		job.Description = description.Text
		job.Resume = resume.Text
		job.Coverletter = coverLetter.Text
		job.Question = question.Text

		err := services.UpdateJob(db, job)
		if err == nil {
			onSave()
		}
	})
	submitBtn.Importance = widget.HighImportance

	// Add Model Selection Dropdown for Regeneration
	modelSelect := widget.NewSelect([]string{"Gemini", "Claude", "OpenAI"}, nil)
	modelSelect.SetSelected("Gemini")

	regenStatus := widget.NewLabel("")
	regenStatus.Wrapping = fyne.TextWrapWord
	regenProgress := widget.NewProgressBarInfinite()
	regenProgress.Hide()

	var regenBtn *widget.Button
	regenBtn = widget.NewButtonWithIcon("Regenerate Docs", theme.MediaReplayIcon(), func() {
		desc := description.Text
		if desc == "" {
			regenStatus.SetText("⚠ Description is empty — cannot regenerate.")
			return
		}
		comp := company.Text
		if comp == "" {
			comp = "Unknown Company"
		}
		rl := role.Text
		if rl == "" {
			rl = "Unknown Role"
		}

		selectedModel := modelSelect.Selected // Capture chosen model

		regenBtn.Disable()
		regenProgress.Show()
		regenStatus.SetText(fmt.Sprintf("Regenerating docs using %s…", selectedModel))

		go func() {
			// Passed selectedModel to the service
			result, err := services.RunRegenerate(comp, rl, desc, selectedModel, func(msg string) {
				fyne.Do(func() {
					regenStatus.SetText(strings.TrimSpace(msg))
				})
			})
			fyne.Do(func() {
				regenProgress.Hide()
				regenBtn.Enable()
				if err != nil {
					regenStatus.SetText(fmt.Sprintf("Error: %v", err))
					return
				}
				if !result.Success {
					regenStatus.SetText(fmt.Sprintf("Failed: %s", result.Error))
					return
				}
				// Update form fields and save to DB
				resume.SetText(result.ResumePath)
				coverLetter.SetText(result.CoverPath)
				job.Resume = result.ResumePath
				job.Coverletter = result.CoverPath

				resData, _ := json.MarshalIndent(result.ResumeData, "", "  ")
				covData, _ := json.MarshalIndent(result.CoverData, "", "  ")
				job.ResumeData = string(resData)
				job.CoverData = string(covData)

				if resumeDataEntry != nil {
					resumeDataEntry.SetText(job.ResumeData)
				}
				if coverDataEntry != nil {
					coverDataEntry.SetText(job.CoverData)
				}

				_ = services.UpdateJob(db, job)
				regenStatus.SetText(fmt.Sprintf("✓ Documents regenerated using %s!", selectedModel))
			})
		}()
	})
	regenBtn.Importance = widget.MediumImportance

	deleteBtn := widget.NewButton("Delete Job", func() {
		err := services.DeleteJob(db, job.Id)
		if err == nil {
			onSave()
		}
	})
	deleteBtn.Importance = widget.DangerImportance

	backBtn := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		onCancel()
	})

	actionButtons := container.NewHBox(submitBtn, deleteBtn)

	companyRoleBox := container.NewGridWithColumns(2,
		container.NewVBox(widget.NewLabel("Company"), company),
		container.NewVBox(widget.NewLabel("Role"), role),
	)

	row := func(label string, w fyne.CanvasObject) fyne.CanvasObject {
		lbl := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		return container.NewPadded(container.NewVBox(lbl, w))
	}

	form := container.NewVBox(
		container.NewPadded(companyRoleBox),
		row("Link", linkBox),
		row("Status", status),
		row("Description", description),
		row("Resume", resume),
		row("Cover Letter", coverLetter),
		row("Question", question),
		// Group Model Selection and Regen Button
		row("AI Model for Regeneration", modelSelect),
		container.NewPadded(container.NewVBox(
			regenBtn,
			regenProgress,
			regenStatus,
		)),
		container.NewPadded(actionButtons),
	)

	header := container.NewBorder(nil, nil, backBtn, nil, widget.NewLabelWithStyle("Edit Job Entry", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}))

	formContent := container.NewBorder(header, nil, nil, nil, container.NewVScroll(container.NewPadded(form)))

	resumeTab := container.NewTabItem("Resume", renderPDFToCanvas(job.Resume))
	coverTab := container.NewTabItem("Cover Letter", renderPDFToCanvas(job.Coverletter))
	questionTab := container.NewTabItem("Details & Q&A", renderQuestions(job.Description, job.Question))

	resEditor, getResJSON := buildJSONEditor(job.ResumeData)
	covEditor, getCovJSON := buildJSONEditor(job.CoverData)

	updateDataBtn := widget.NewButtonWithIcon("Apply Changes & Overwrite PDFs", theme.DocumentSaveIcon(), func() {
		job.ResumeData = getResJSON()
		job.CoverData = getCovJSON()

		result, err := services.RegenerateFromData(job.Company, job.Role, job.ResumeData, job.CoverData, nil)
		if err == nil {
			job.Resume = result.ResumePath
			job.Coverletter = result.CoverPath
			_ = services.UpdateJob(db, job)
			onSave()
		}
	})
	updateDataBtn.Importance = widget.HighImportance

	contentEditTab := container.NewTabItem("Edit Content", container.NewBorder(nil, container.NewPadded(updateDataBtn), nil, nil, container.NewAppTabs(
		container.NewTabItem("Resume Content", resEditor),
		container.NewTabItem("Cover Content", covEditor),
	)))

	tabs := container.NewAppTabs(resumeTab, coverTab, contentEditTab, questionTab)

	split := container.NewHSplit(formContent, tabs)
	split.Offset = 0.25

	return split
}

func tableResizer(t *widget.Table, container fyne.CanvasObject) {
	width := container.Size().Width
	if width < 800 {
		width = 800
	}
	t.SetColumnWidth(0, 50)
	t.SetColumnWidth(1, width*0.15)
	t.SetColumnWidth(2, width*0.15)
	t.SetColumnWidth(3, width*0.4)
	t.SetColumnWidth(4, width*0.1)
	t.SetColumnWidth(5, 80)
}

func CreateMainWindow(app fyne.App, db *sql.DB) fyne.Window {
	win := app.NewWindow("AutoApply Dashboard")

	allJobs, err := services.GetAllJobs(db)
	if err != nil {
		fmt.Printf("❌ Initial load failed: %v\n", err)
	}

	var searchEntry *widget.Entry
	var mainLayout *fyne.Container
	var jobTable *JobTable

	refreshTable := func() {
		jobs, err := services.GetAllJobs(db)
		if err != nil {
			fmt.Printf("❌ Failed to refresh jobs: %v\n", err)
			return
		}
		allJobs = jobs
		query := strings.ToLower(searchEntry.Text)
		var filtered []model.Job
		for _, j := range allJobs {
			if strings.Contains(strings.ToLower(j.Company), query) ||
				strings.Contains(strings.ToLower(j.Role), query) {
				filtered = append(filtered, j)
			}
		}
		jobTable.UpdateData(filtered)
		win.SetContent(mainLayout)
	}

	searchEntry = widget.NewEntry()
	searchEntry.SetPlaceHolder("Search company or role...")
	searchEntry.OnChanged = func(s string) {
		refreshTable()
	}

	addBtn := widget.NewButtonWithIcon("Add Job", theme.ContentAddIcon(), func() {
		ShowAddJobPopup(app, db, func() {
			refreshTable()
		})
	})
	addBtn.Importance = widget.HighImportance

	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		settingsView := BuildSettingsView(db, func() {
			win.SetContent(mainLayout)
		})
		win.SetContent(settingsView)
	})

	jobTable = NewJobTable(allJobs, func(job model.Job) {
		editView := BuildEditJobView(db, job, func() {
			refreshTable()
		}, func() {
			win.SetContent(mainLayout)
		})
		win.SetContent(editView)
	})

	topRow := container.NewBorder(nil, nil, nil, container.NewHBox(addBtn, settingsBtn), searchEntry)
	mainLayout = container.NewBorder(
		container.NewPadded(topRow),
		nil, nil, nil,
		container.NewPadded(jobTable),
	)

	win.SetContent(mainLayout)
	win.Resize(fyne.NewSize(1200, 800))

	go func() {
		time.Sleep(100 * time.Millisecond)
		tableResizer(jobTable.Table, win.Content())
	}()

	return win
}

