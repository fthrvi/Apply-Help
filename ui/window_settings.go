package ui

import (
	model "32-Adarsha/model"
	"32-Adarsha/services"
	"database/sql"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func BuildSettingsView(win fyne.Window, db *sql.DB, onBack func()) fyne.CanvasObject {
	// Left Sidebar — icon + label per entry. Icons give the user a
	// visual anchor when scanning; the list still routes via index so
	// nothing else needs to change.
	type sidebarItem struct {
		label string
		icon  fyne.Resource
	}
	items := []sidebarItem{
		{"User Profile", theme.AccountIcon()},
		{"API Keys", theme.VisibilityOffIcon()},
		{"System Prompts", theme.DocumentIcon()},
		{"Output Schemas", theme.ListIcon()},
		{"HTML Templates", theme.ColorPaletteIcon()},
		{"Error Logs", theme.WarningIcon()},
	}
	list := widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject {
			icon := widget.NewIcon(theme.AccountIcon())
			label := widget.NewLabel("Template")
			return container.NewHBox(icon, label)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			hbox := o.(*fyne.Container)
			hbox.Objects[0].(*widget.Icon).SetResource(items[id].icon)
			hbox.Objects[1].(*widget.Label).SetText(items[id].label)
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

	githubUsername := widget.NewEntry()
	githubUsername.SetPlaceHolder("e.g. octocat")
	githubUsername.SetText(services.GetSetting(db, services.KeyGithubUsername))

	githubToken := widget.NewPasswordEntry()
	githubToken.SetPlaceHolder("Personal Access Token (optional, higher rate limit)")
	githubToken.SetText(services.GetSetting(db, services.KeyGithubToken))

	gmailAddress := widget.NewEntry()
	gmailAddress.SetPlaceHolder("you@gmail.com")
	gmailAddress.SetText(services.GetSetting(db, services.KeyGmailAddress))

	gmailAppPassword := widget.NewPasswordEntry()
	gmailAppPassword.SetPlaceHolder("16-char app password (myaccount.google.com/apppasswords)")
	gmailAppPassword.SetText(services.GetSetting(db, services.KeyGmailAppPassword))

	localLLMEndpoint := widget.NewEntry()
	localLLMEndpoint.SetPlaceHolder("http://prithvi-system-product-name:11434")
	localLLMEndpoint.SetText(services.GetSetting(db, services.KeyLocalLLMEndpoint))

	localLLMModel := widget.NewEntry()
	localLLMModel.SetPlaceHolder("qwen2.5:7b-instruct")
	localLLMModel.SetText(services.GetSetting(db, services.KeyLocalLLMModel))

	localLLMEmbedModel := widget.NewEntry()
	localLLMEmbedModel.SetPlaceHolder("nomic-embed-text")
	localLLMEmbedModel.SetText(services.GetSetting(db, services.KeyLocalLLMEmbedModel))

	localLLMStatusLabel := widget.NewLabel("(not tested)")
	localLLMTestBtn := widget.NewButton("Test Connection", func() {
		// Save current entries first so the ping uses what the user
		// just typed, not the previously-saved values.
		services.SaveSetting(db, services.KeyLocalLLMEndpoint, localLLMEndpoint.Text)
		services.SaveSetting(db, services.KeyLocalLLMModel, localLLMModel.Text)
		services.SaveSetting(db, services.KeyLocalLLMEmbedModel, localLLMEmbedModel.Text)
		localLLMStatusLabel.SetText("Pinging…")
		go func() {
			err := services.LocalLLMPing()
			fyne.Do(func() {
				if err != nil {
					localLLMStatusLabel.SetText("Failed: " + err.Error())
				} else {
					localLLMStatusLabel.SetText("✓ Connected. Model loaded and responsive.")
				}
			})
		}()
	})

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
		services.SaveSetting(db, services.KeyGithubUsername, githubUsername.Text)
		services.SaveSetting(db, services.KeyGithubToken, githubToken.Text)
		services.SaveSetting(db, services.KeyGmailAddress, gmailAddress.Text)
		services.SaveSetting(db, services.KeyGmailAppPassword, gmailAppPassword.Text)
		services.SaveSetting(db, services.KeyLocalLLMEndpoint, localLLMEndpoint.Text)
		services.SaveSetting(db, services.KeyLocalLLMModel, localLLMModel.Text)
		services.SaveSetting(db, services.KeyLocalLLMEmbedModel, localLLMEmbedModel.Text)
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
		widget.NewSeparator(),
		widget.NewLabelWithStyle("GitHub Integration", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Profile + top repos are injected into the LLM prompt during resume/cover generation. Username alone works (60 req/hr); add a token for 5000/hr."),
		widget.NewLabel("GitHub Username"), githubUsername,
		widget.NewLabel("GitHub Personal Access Token"), githubToken,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Gmail Sync", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Auto-update job statuses from your inbox. Requires 2-step verification on your Google account, then create an app password at myaccount.google.com/apppasswords."),
		widget.NewLabel("Gmail Address"), gmailAddress,
		widget.NewLabel("Gmail App Password"), gmailAppPassword,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Local LLM (Ollama-compatible)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Powers the autofill agent. Point at any Ollama endpoint on your Tailnet. Profile loaded as system prompt; per-field calls are ~100-300ms with KV-cache keepalive."),
		widget.NewLabel("Endpoint URL"), localLLMEndpoint,
		widget.NewLabel("Model (chat)"), localLLMModel,
		widget.NewLabel("Model (embeddings)"), localLLMEmbedModel,
		container.NewGridWithColumns(2, localLLMTestBtn, localLLMStatusLabel),
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
			urlEntry := widget.NewEntry()
			urlEntry.SetText(proj.URL)
			urlEntry.SetPlaceHolder("https://github.com/user/repo (optional)")
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
				widget.NewLabel("URL / GitHub Link"), urlEntry,
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
			urlEntry.OnChanged = func(s string) { ui.Projects[idx].URL = strings.TrimSpace(s) }
			tech.OnChanged = func(s string) { ui.Projects[idx].Technologies = strings.Split(s, ", ") }
			bullets.OnChanged = func(s string) { ui.Projects[idx].Bullets = strings.Split(s, "\n") }
		}
	}
	renderProjects()

	addProjBtn := widget.NewButtonWithIcon("Add New Project", theme.ContentAddIcon(), func() {
		ui.Projects = append(ui.Projects, model.Project{Name: "New Project"})
		renderProjects()
	})

	importProjBtn := widget.NewButtonWithIcon("Import from GitHub", theme.DownloadIcon(), func() {
		if strings.TrimSpace(services.GetSetting(db, services.KeyGithubUsername)) == "" {
			dialog.ShowInformation("GitHub not configured",
				"Set your GitHub username in Settings → API Keys → GitHub Integration first.", win)
			return
		}

		// applyImport fetches repos, optionally runs an LLM bullet polish
		// pass, and merges results into ui.Projects. modelChoice mirrors
		// PromptAI values ("gemini" / "claude" / "openai"); empty string
		// means heuristic-only.
		applyImport := func(replace bool, modelChoice string) {
			progress := dialog.NewCustomWithoutButtons("Fetching repos from GitHub…", widget.NewProgressBarInfinite(), win)
			progress.Show()
			go func() {
				ghCtx, err := services.GitHubContextForCurrentUser()
				if err != nil {
					fyne.Do(func() {
						progress.Hide()
						dialog.ShowError(err, win)
					})
					return
				}
				if ghCtx == nil || len(ghCtx.Repos) == 0 {
					fyne.Do(func() {
						progress.Hide()
						dialog.ShowInformation("No repos", "GitHub returned no repos for that username.", win)
					})
					return
				}

				newProjects := make([]model.Project, 0, len(ghCtx.Repos))
				for _, r := range ghCtx.Repos {
					newProjects = append(newProjects, services.RepoToProject(r))
				}

				// Optional LLM polish. Failures here fall through to the
				// heuristic bullets we already produced; we surface the
				// error in the success dialog so the user knows.
				var polishNote string
				if modelChoice != "" {
					fyne.Do(func() {
						progress.Hide()
						progress = dialog.NewCustomWithoutButtons(
							fmt.Sprintf("Polishing bullets with %s…", strings.Title(modelChoice)),
							widget.NewProgressBarInfinite(), win)
						progress.Show()
					})
					polished, perr := services.PolishProjectBulletsWithLLM(ghCtx, ui, modelChoice)
					if perr != nil {
						services.LogError(db, fmt.Sprintf("Bullet polish failed: %v", perr))
						polishNote = "\n\nLLM polish failed; kept heuristic bullets. See Error Logs."
					}
					applied := 0
					for i := range newProjects {
						if b, ok := polished[newProjects[i].Name]; ok && len(b) > 0 {
							newProjects[i].Bullets = b
							applied++
						}
					}
					if perr == nil && applied < len(newProjects) {
						polishNote = fmt.Sprintf("\n\nLLM polished %d of %d projects.", applied, len(newProjects))
					}
				}

				fyne.Do(func() {
					progress.Hide()
					if replace {
						ui.Projects = newProjects
					} else {
						ui.Projects = append(ui.Projects, newProjects...)
					}
					renderProjects()
					_ = services.SaveUserInfo(db, ui)
					dialog.ShowInformation("Imported",
						fmt.Sprintf("Imported %d project(s) from GitHub.%s", len(newProjects), polishNote), win)
				})
			}()
		}

		// Step 1 — ask which model (if any) should polish the bullets.
		// "Skip" produces heuristic bullets only (no API key required).
		const polishSkip = "Skip (heuristic only)"
		polishRadio := widget.NewRadioGroup(
			[]string{polishSkip, "Gemini", "Claude", "OpenAI"},
			nil,
		)
		polishRadio.SetSelected(polishSkip)

		// chooseReplace returns true=replace, false=append, after asking
		// the user. Called only when ui.Projects already has entries.
		chooseReplace := func(after func(replace bool)) {
			if len(ui.Projects) == 0 {
				after(true)
				return
			}
			var chooseDialog dialog.Dialog
			replaceBtn := widget.NewButton("Replace All", func() {
				chooseDialog.Hide()
				after(true)
			})
			replaceBtn.Importance = widget.HighImportance
			appendBtn := widget.NewButton("Append", func() {
				chooseDialog.Hide()
				after(false)
			})
			cancelBtn := widget.NewButton("Cancel", func() {
				chooseDialog.Hide()
			})
			body := container.NewVBox(
				widget.NewLabel(fmt.Sprintf("You have %d existing project(s). Replace them with your GitHub repos, or append the imports?", len(ui.Projects))),
				container.NewGridWithColumns(3, replaceBtn, appendBtn, cancelBtn),
			)
			chooseDialog = dialog.NewCustomWithoutButtons("Existing projects", body, win)
			chooseDialog.Show()
		}

		var polishDialog dialog.Dialog
		continueBtn := widget.NewButton("Continue", func() {
			polishDialog.Hide()
			choice := ""
			if polishRadio.Selected != polishSkip {
				choice = strings.ToLower(polishRadio.Selected)
			}
			chooseReplace(func(replace bool) { applyImport(replace, choice) })
		})
		continueBtn.Importance = widget.HighImportance
		cancelPolishBtn := widget.NewButton("Cancel", func() { polishDialog.Hide() })
		polishBody := container.NewVBox(
			widget.NewLabel("Polish project bullets with an LLM?"),
			widget.NewLabel("Heuristic bullets are repo description + first README line."),
			widget.NewLabel("LLM polish rewrites them as résumé-grade XYZ statements."),
			polishRadio,
			container.NewGridWithColumns(2, continueBtn, cancelPolishBtn),
		)
		polishDialog = dialog.NewCustomWithoutButtons("Import from GitHub", polishBody, win)
		polishDialog.Show()
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
			coursework.SetMinRowsVisible(6)
			transcript := widget.NewMultiLineEntry()
			transcript.SetText(strings.Join(edu.Transcript, "\n"))
			transcript.SetMinRowsVisible(8)
			transcript.SetPlaceHolder("Paste your transcript here, one course per line (any format).\nLLM picks the 2-4 most relevant to each job.")

			removeBtn := widget.NewButtonWithIcon("Remove This Education", theme.DeleteIcon(), func() {
				ui.Education = append(ui.Education[:idx], ui.Education[idx+1:]...)
				renderEducation()
			})
			removeBtn.Importance = widget.DangerImportance

			itemContent := container.NewVBox(
				widget.NewLabel("Institution"), school,
				widget.NewLabel("Degree"), degree,
				widget.NewLabel("Coursework (one per line — used as fallback when no transcript)"), coursework,
				widget.NewLabel("Full Transcript (one course per line, optional)"), transcript,
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
			transcript.OnChanged = func(s string) {
				parts := strings.Split(s, "\n")
				out := make([]string, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						out = append(out, p)
					}
				}
				ui.Education[idx].Transcript = out
			}
		}
	}
	renderEducation()

	addEduBtn := widget.NewButtonWithIcon("Add New Education", theme.ContentAddIcon(), func() {
		ui.Education = append(ui.Education, model.Education{Institution: "New Institution", Degree: "New Degree"})
		renderEducation()
	})

	// Single transcript-import button that handles all Education entries
	// at once. The LLM matches each course to the right entry by
	// institution + degree.
	importTranscriptBtn := widget.NewButtonWithIcon("Import Transcript (PDF / DOCX / TXT)", theme.UploadIcon(), func() {
		if len(ui.Education) == 0 {
			dialog.ShowInformation("Add Education first",
				"Create at least one Education entry before importing a transcript — the LLM needs to know which degree each course belongs to.", win)
			return
		}
		showImportTranscriptDialog(win, db, ui.Education, func(byIdx map[int][]string) {
			for idx, courses := range byIdx {
				if idx >= 0 && idx < len(ui.Education) {
					ui.Education[idx].Transcript = courses
				}
			}
			_ = services.SaveUserInfo(db, ui)
			renderEducation()
		})
	})
	importTranscriptBtn.Importance = widget.MediumImportance

	// Job Preferences — LLM-derived filter tags. Each tag has a label,
	// a keyword list (substring-matched against role+company on the
	// dashboard), and an Enabled flag (false hides the chip without
	// deleting the tag).
	jobPrefs, _ := services.GetJobPreferences(db)

	prefsAccordion := widget.NewAccordion()
	var renderJobPrefs func()
	renderJobPrefs = func() {
		prefsAccordion.Items = nil
		for i, tag := range jobPrefs.Tags {
			idx := i
			labelEntry := widget.NewEntry()
			labelEntry.SetText(tag.Label)
			keywordsEntry := widget.NewEntry()
			keywordsEntry.SetText(strings.Join(tag.Keywords, ", "))
			enabledCheck := widget.NewCheck("Enabled (shown as chip on dashboard)", nil)
			enabledCheck.SetChecked(tag.Enabled)

			removeBtn := widget.NewButtonWithIcon("Remove This Tag", theme.DeleteIcon(), func() {
				jobPrefs.Tags = append(jobPrefs.Tags[:idx], jobPrefs.Tags[idx+1:]...)
				renderJobPrefs()
			})
			removeBtn.Importance = widget.DangerImportance

			itemContent := container.NewVBox(
				widget.NewLabel("Label"), labelEntry,
				widget.NewLabel("Keywords (comma separated, case-insensitive substring match against job role + company)"), keywordsEntry,
				enabledCheck,
				container.NewPadded(removeBtn),
			)
			item := widget.NewAccordionItem(tag.Label, itemContent)
			prefsAccordion.Append(item)

			labelEntry.OnChanged = func(s string) {
				jobPrefs.Tags[idx].Label = strings.TrimSpace(strings.ToLower(s))
				item.Title = jobPrefs.Tags[idx].Label
				prefsAccordion.Refresh()
			}
			keywordsEntry.OnChanged = func(s string) {
				parts := strings.Split(s, ",")
				cleaned := make([]string, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(strings.ToLower(p))
					if p != "" {
						cleaned = append(cleaned, p)
					}
				}
				jobPrefs.Tags[idx].Keywords = cleaned
			}
			enabledCheck.OnChanged = func(b bool) {
				jobPrefs.Tags[idx].Enabled = b
			}
		}
	}
	renderJobPrefs()

	addTagBtn := widget.NewButtonWithIcon("Add New Tag", theme.ContentAddIcon(), func() {
		jobPrefs.Tags = append(jobPrefs.Tags, model.JobTag{Label: "new-tag", Enabled: true})
		renderJobPrefs()
	})

	generatePrefsBtn := widget.NewButtonWithIcon("Generate from Profile + GitHub", theme.ViewRefreshIcon(), func() {
		modelRadio := widget.NewRadioGroup([]string{"Gemini", "Claude", "OpenAI"}, nil)
		modelRadio.Horizontal = true
		modelRadio.SetSelected("Gemini")

		var dlg dialog.Dialog
		generateBtn := widget.NewButton("Generate", func() {
			modelChoice := strings.ToLower(modelRadio.Selected)
			dlg.Hide()
			progress := dialog.NewCustomWithoutButtons(
				fmt.Sprintf("Generating job preferences with %s…", strings.Title(modelChoice)),
				widget.NewProgressBarInfinite(), win)
			progress.Show()
			go func() {
				ghCtx, ghErr := services.GitHubContextForCurrentUser()
				if ghErr != nil {
					services.LogError(db, fmt.Sprintf("GitHub context fetch for job prefs failed: %v", ghErr))
				}
				newPrefs, err := services.GenerateJobPreferencesWithLLM(ui, ghCtx, modelChoice)
				fyne.Do(func() { progress.Hide() })
				if err != nil {
					services.LogError(db, fmt.Sprintf("Job prefs generation failed: %v", err))
					fyne.Do(func() { dialog.ShowError(err, win) })
					return
				}
				fyne.Do(func() {
					*jobPrefs = *newPrefs
					renderJobPrefs()
					_ = services.SaveJobPreferences(db, jobPrefs)
					dialog.ShowInformation("Generated",
						fmt.Sprintf("Created %d tag(s). Edit or disable any you don't want.", len(jobPrefs.Tags)), win)
				})
			}()
		})
		generateBtn.Importance = widget.HighImportance
		cancelGenBtn := widget.NewButton("Cancel", func() { dlg.Hide() })
		body := container.NewVBox(
			widget.NewLabel("LLM reads your profile (skills, experience, projects) and GitHub repos"),
			widget.NewLabel("to suggest 5-10 job filter tags. Replaces existing tags."),
			modelRadio,
			container.NewGridWithColumns(2, generateBtn, cancelGenBtn),
		)
		dlg = dialog.NewCustomWithoutButtons("Generate Job Preferences", body, win)
		dlg.Show()
	})
	generatePrefsBtn.Importance = widget.HighImportance

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
		_ = services.SaveJobPreferences(db, jobPrefs)
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

	jobPrefsView := container.NewVBox(
		widget.NewLabelWithStyle("Job Preferences", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Tags filter the dashboard. A job appears when its role or company contains any keyword from at least one enabled tag's chip on the dashboard."),
		container.NewGridWithColumns(2, generatePrefsBtn, addTagBtn),
		prefsAccordion,
	)

	profileTabs := container.NewAppTabs(
		container.NewTabItem("Personal Info", container.NewVScroll(container.NewPadded(personalInfo))),
		container.NewTabItem("Experience", container.NewVScroll(container.NewPadded(container.NewVBox(expAccordion, addExpBtn)))),
		container.NewTabItem("Project", container.NewVScroll(container.NewPadded(container.NewVBox(
			projAccordion,
			container.NewGridWithColumns(2, addProjBtn, importProjBtn),
		)))),
		container.NewTabItem("Education", container.NewVScroll(container.NewPadded(container.NewVBox(
			eduAccordion,
			container.NewGridWithColumns(2, addEduBtn, importTranscriptBtn),
		)))),
		container.NewTabItem("Skills & Tech", container.NewVScroll(container.NewPadded(skillsInfo))),
		container.NewTabItem("Job Preferences", container.NewVScroll(container.NewPadded(jobPrefsView))),
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

	// Bundled template-style picker. Selecting a style overwrites the
	// raw template entries below; the user can still hand-tune from
	// there. Bypasses the migration sentinel by setting both stored
	// templates directly.
	styles := services.TemplateStyles()
	styleLabels := make([]string, 0, len(styles))
	for _, s := range styles {
		styleLabels = append(styleLabels, s.Label)
	}
	styleRadio := widget.NewRadioGroup(styleLabels, nil)
	styleRadio.Horizontal = false
	styleDesc := widget.NewLabel("")
	styleDesc.Wrapping = fyne.TextWrapWord
	styleRadio.OnChanged = func(label string) {
		for _, s := range styles {
			if s.Label == label {
				styleDesc.SetText(s.Description)
				return
			}
		}
	}

	applyStyleBtn := widget.NewButtonWithIcon("Apply This Style", theme.ConfirmIcon(), func() {
		var picked *services.TemplateStyle
		for i := range styles {
			if styles[i].Label == styleRadio.Selected {
				picked = &styles[i]
				break
			}
		}
		if picked == nil {
			dialog.ShowInformation("Pick a style", "Select a template style above before applying.", win)
			return
		}
		dialog.ShowConfirm(
			"Apply "+picked.Label+"?",
			"Replaces your current Resume + Cover templates with the "+picked.Label+" preset. Any hand-edits in the raw template entries below will be lost.",
			func(ok bool) {
				if !ok {
					return
				}
				_ = services.SaveSetting(db, services.KeyResumeTemplate, picked.Resume)
				_ = services.SaveSetting(db, services.KeyCoverTemplate, picked.Cover)
				resTemplateEntry.SetText(picked.Resume)
				covTemplateEntry.SetText(picked.Cover)
				dialog.ShowInformation("Applied", picked.Label+" is now your active template.", win)
			}, win)
	})
	applyStyleBtn.Importance = widget.HighImportance

	stylePicker := container.NewVBox(
		widget.NewLabelWithStyle("Template Style", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Pick a bundled aesthetic. Generate a job afterward to see it rendered."),
		styleRadio,
		styleDesc,
		container.NewPadded(applyStyleBtn),
		widget.NewSeparator(),
	)

	templateTabs := container.NewAppTabs(
		container.NewTabItem("Style Picker", container.NewVScroll(container.NewPadded(stylePicker))),
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
