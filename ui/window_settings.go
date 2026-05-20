package ui

import (
	model "32-Adarsha/model"
	"32-Adarsha/services"
	"database/sql"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func BuildSettingsView(win fyne.Window, db *sql.DB, onBack func()) fyne.CanvasObject {
	// Left Sidebar
	categories := []string{"User Profile", "API Keys", "System Prompts", "Output Schemas", "HTML Templates", "Error Logs"}
	list := widget.NewList(
		func() int { return len(categories) },
		func() fyne.CanvasObject { return widget.NewLabel("Template") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(categories[id])
		},
	)

	rightSide := container.NewStack()

	// 1. API Keys View
	geminiKey1 := widget.NewPasswordEntry()
	geminiKey1.SetText(services.GetSetting(db, services.KeyGeminiAPI1))
	geminiKey2 := widget.NewPasswordEntry()
	geminiKey2.SetText(services.GetSetting(db, services.KeyGeminiAPI2))

	activeKey := widget.NewRadioGroup([]string{"Key 1", "Key 2"}, nil)
	activeKey.Horizontal = true
	if services.GetSetting(db, services.KeyActiveGemini) == "2" {
		activeKey.SetSelected("Key 2")
	} else {
		activeKey.SetSelected("Key 1")
	}

	geminiURL := widget.NewEntry()
	geminiURL.SetText(services.GetSetting(db, services.KeyGeminiURL))
	if geminiURL.Text == "" {
		geminiURL.SetText("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent")
	}
	claudeKey := widget.NewPasswordEntry()
	claudeKey.SetText(services.GetSetting(db, services.KeyClaudeAPI))
	openaiKey := widget.NewPasswordEntry()
	openaiKey.SetText(services.GetSetting(db, services.KeyOpenAIAPI))

	claudeModel := widget.NewEntry()
	claudeModel.SetPlaceHolder(services.DefaultClaudeModel)
	claudeModel.SetText(services.GetSetting(db, services.KeyClaudeModel))

	openaiModel := widget.NewEntry()
	openaiModel.SetPlaceHolder(services.DefaultOpenAIModel)
	openaiModel.SetText(services.GetSetting(db, services.KeyOpenAIModel))

	keysSaveBtn := widget.NewButton("Save API Keys", func() {
		services.SaveSetting(db, services.KeyGeminiAPI1, geminiKey1.Text)
		services.SaveSetting(db, services.KeyGeminiAPI2, geminiKey2.Text)
		if activeKey.Selected == "Key 2" {
			services.SaveSetting(db, services.KeyActiveGemini, "2")
		} else {
			services.SaveSetting(db, services.KeyActiveGemini, "1")
		}
		services.SaveSetting(db, services.KeyGeminiURL, geminiURL.Text)
		services.SaveSetting(db, services.KeyClaudeAPI, claudeKey.Text)
		services.SaveSetting(db, services.KeyClaudeModel, claudeModel.Text)
		services.SaveSetting(db, services.KeyOpenAIAPI, openaiKey.Text)
		services.SaveSetting(db, services.KeyOpenAIModel, openaiModel.Text)
	})
	keysSaveBtn.Importance = widget.HighImportance

	keysForm := container.NewVScroll(container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("API Key Configuration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Gemini API Key 1"), geminiKey1,
		widget.NewLabel("Gemini API Key 2"), geminiKey2,
		widget.NewLabel("Active Gemini Key"), activeKey,
		widget.NewLabel("Gemini Endpoint URL"), geminiURL,
		widget.NewSeparator(),
		widget.NewLabel("Claude API Key"), claudeKey,
		widget.NewLabel("Claude Model"), claudeModel,
		widget.NewSeparator(),
		widget.NewLabel("OpenAI/NVIDIA API Key"), openaiKey,
		widget.NewLabel("OpenAI/NVIDIA Model"), openaiModel,
		container.NewPadded(keysSaveBtn),
	)))

	// 2. Prompts View
	extractPrompt := widget.NewMultiLineEntry()
	extractPrompt.SetText(services.GetSetting(db, services.KeyExtractPrompt))

	combinedPrompt := widget.NewMultiLineEntry()
	combinedPrompt.SetText(services.GetSetting(db, services.KeyCombinedPrompt))

	promptsSaveBtn := widget.NewButton("Save All Prompts", func() {
		services.SaveSetting(db, services.KeyExtractPrompt, extractPrompt.Text)
		services.SaveSetting(db, services.KeyCombinedPrompt, combinedPrompt.Text)
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

	// applyParsedUserInfo overwrites the current profile (in-memory + UI +
	// persisted) with parsed. Called from the resume-import preview dialog.
	applyParsedUserInfo := func(parsed *model.UserInfo) {
		*ui = *parsed
		nameEntry.SetText(ui.Name)
		emailEntry.SetText(ui.Email)
		phoneEntry.SetText(ui.Phone)
		locationEntry.SetText(ui.Location)
		languagesEntry.SetText(strings.Join(ui.Skills.Languages, ", "))
		frameworksEntry.SetText(strings.Join(ui.Skills.Frameworks, ", "))
		devToolsEntry.SetText(strings.Join(ui.Skills.DevTools, ", "))
		databasesEntry.SetText(strings.Join(ui.Skills.Databases, ", "))
		awardsEntry.SetText(strings.Join(ui.Awards, "\n"))
		renderExperiences()
		renderProjects()
		renderEducation()
		_ = services.SaveUserInfo(db, ui)
	}

	importBtn := widget.NewButtonWithIcon("Import Resume (PDF / DOCX / TXT)", theme.UploadIcon(), func() {
		showImportResumeDialog(win, db, applyParsedUserInfo)
	})
	importBtn.Importance = widget.HighImportance

	profileTabs := container.NewAppTabs(
		container.NewTabItem("Personal Info", container.NewVScroll(container.NewPadded(personalInfo))),
		container.NewTabItem("Experience", container.NewVScroll(container.NewPadded(container.NewVBox(expAccordion, addExpBtn)))),
		container.NewTabItem("Project", container.NewVScroll(container.NewPadded(container.NewVBox(projAccordion, addProjBtn)))),
		container.NewTabItem("Education", container.NewVScroll(container.NewPadded(container.NewVBox(eduAccordion, addEduBtn)))),
		container.NewTabItem("Skills & Tech", container.NewVScroll(container.NewPadded(skillsInfo))),
		container.NewTabItem("Awards", container.NewVScroll(container.NewPadded(awardsInfo))),
	)

	profileView := container.NewBorder(
		container.NewPadded(importBtn),
		container.NewPadded(saveProfileBtn),
		nil, nil,
		profileTabs,
	)

	// 4. Output Schemas View
	combinedSchemaEntry := widget.NewMultiLineEntry()
	combinedSchemaEntry.SetText(services.GetSetting(db, services.KeyCombinedSchema))

	schemasSaveBtn := widget.NewButton("Save Schemas", func() {
		services.SaveSetting(db, services.KeyCombinedSchema, combinedSchemaEntry.Text)
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
	resTemplateEntry.SetText(services.GetSetting(db, services.KeyResumeTemplate))

	covTemplateEntry := widget.NewMultiLineEntry()
	covTemplateEntry.SetText(services.GetSetting(db, services.KeyCoverTemplate))

	templateSaveBtn := widget.NewButton("Save Templates", func() {
		services.SaveSetting(db, services.KeyResumeTemplate, resTemplateEntry.Text)
		services.SaveSetting(db, services.KeyCoverTemplate, covTemplateEntry.Text)
	})
	templateSaveBtn.Importance = widget.HighImportance

	templateTabs := container.NewAppTabs(
		container.NewTabItem("Resume Template", container.NewPadded(resTemplateEntry)),
		container.NewTabItem("Cover Template", container.NewPadded(covTemplateEntry)),
	)

	templatesView := container.NewBorder(nil, container.NewPadded(templateSaveBtn), nil, nil, templateTabs)

	// 6. Error Logs View
	errorLogsContainer := container.NewStack()
	refreshErrorLogs := func() {
		logs := services.GetAllErrors(db)
		vbox := container.NewVBox()
		vbox.Add(widget.NewLabelWithStyle("Recent System Errors", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

		if len(logs) == 0 {
			vbox.Add(widget.NewLabel("No errors logged."))
		}

		for _, l := range logs {
			timeStr := l.Timestamp.Format("2006-01-02 15:04:05")
			msg := fmt.Sprintf("[%s] %s", timeStr, l.Message)
			label := widget.NewLabel(msg)
			label.Wrapping = fyne.TextWrapWord
			vbox.Add(label)
			vbox.Add(widget.NewSeparator())
		}

		clearBtn := widget.NewButtonWithIcon("Clear All Logs", theme.DeleteIcon(), func() {
			services.ClearErrors(db)
			errorLogsContainer.Objects = []fyne.CanvasObject{container.NewCenter(widget.NewLabel("Logs Cleared"))}
			errorLogsContainer.Refresh()
		})
		clearBtn.Importance = widget.DangerImportance

		errorLogsContainer.Objects = []fyne.CanvasObject{container.NewBorder(nil, container.NewPadded(clearBtn), nil, nil, container.NewVScroll(container.NewPadded(vbox)))}
		errorLogsContainer.Refresh()
	}

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
		} else if id == 5 {
			refreshErrorLogs()
			rightSide.Add(errorLogsContainer)
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
