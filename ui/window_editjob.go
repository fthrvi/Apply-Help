package ui

import (
	model "32-Adarsha/model"
	"32-Adarsha/services"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

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
		cmd := exec.Command("qlmanage", "-t", "-s", "2048", "-o", outDir, pdfPath)
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

func BuildEditJobView(win fyne.Window, db *sql.DB, job model.Job, onSave func(), onCancel func()) fyne.CanvasObject {
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

	currentStatus := job.Status
	if currentStatus == "" {
		currentStatus = "New"
	}
	status := widget.NewButton(currentStatus, nil)
	status.OnTapped = func() {
		var items []*fyne.MenuItem
		for _, stat := range []string{"New", "Pending", "Processed", "Applied", "Interview", "Rejected", "Offer"} {
			s := stat
			items = append(items, fyne.NewMenuItem(s, func() {
				currentStatus = s
				status.SetText(s)
			}))
		}
		menu := fyne.NewMenu("", items...)
		pop := widget.NewPopUpMenu(menu, win.Canvas())
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(status)
		pop.ShowAtPosition(pos.Add(fyne.NewPos(0, status.Size().Height)))
	}

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
		job.Status = currentStatus
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

	modelSelect := widget.NewRadioGroup([]string{"Gemini", "Claude", "OpenAI"}, nil)
	modelSelect.Horizontal = true
	modelSelect.SetSelected("Gemini")

	regenStatus := widget.NewLabel("")
	regenStatus.Wrapping = fyne.TextWrapWord
	regenProgress := widget.NewProgressBarInfinite()
	regenProgress.Hide()

	var regenBtn *widget.Button
	regenBtn = widget.NewButtonWithIcon("Generate/Regenerate Docs", theme.MediaReplayIcon(), func() {
		rawURL := link.Text
		desc := description.Text
		selectedModel := modelSelect.Selected

		regenBtn.Disable()
		regenProgress.Show()

		go func() {
			defer func() {
				fyne.Do(func() {
					regenProgress.Hide()
					regenBtn.Enable()
				})
			}()

			var result *services.AutoApplyResult
			var err error

			if job.Status == "Pending" {
				// Full flow: Fetch + Extract + Generate
				fyne.Do(func() { regenStatus.SetText(fmt.Sprintf("Processing full workflow using %s…", selectedModel)) })
				result, err = services.RunAutoApply(rawURL, desc, selectedModel, func(msg string) {
					fyne.Do(func() { regenStatus.SetText(strings.TrimSpace(msg)) })
				})
			} else {
				// Just regenerate from existing description
				comp := company.Text
				if comp == "" {
					comp = "Unknown Company"
				}
				rl := role.Text
				if rl == "" {
					rl = "Unknown Role"
				}
				fyne.Do(func() { regenStatus.SetText(fmt.Sprintf("Regenerating docs using %s…", selectedModel)) })
				result, err = services.RunRegenerate(comp, rl, desc, selectedModel, func(msg string) {
					fyne.Do(func() { regenStatus.SetText(strings.TrimSpace(msg)) })
				})
			}

			if err != nil {
				fyne.Do(func() { regenStatus.SetText(fmt.Sprintf("Error: %v", err)) })
				return
			}
			if !result.Success {
				fyne.Do(func() { regenStatus.SetText(fmt.Sprintf("Failed: %s", result.Error)) })
				return
			}

			fyne.Do(func() {
				// Update form fields
				company.SetText(result.Company)
				role.SetText(result.Role)
				description.SetText(result.Description)
				currentStatus = "Processed"
				status.SetText("Processed")
				resume.SetText(result.ResumePath)
				coverLetter.SetText(result.CoverPath)

				// Update internal job object
				job.Company = result.Company
				job.Role = result.Role
				job.Description = result.Description
				job.Status = "Processed"
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
				regenStatus.SetText(fmt.Sprintf("✓ Successfully processed using %s!", selectedModel))
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

	actionButtons := container.NewGridWithColumns(2, submitBtn, deleteBtn)

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
	)

	header := container.NewBorder(nil, nil, backBtn, nil, widget.NewLabelWithStyle("Edit Job Entry", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}))

	formContent := container.NewBorder(header, container.NewPadded(actionButtons), nil, nil, container.NewVScroll(container.NewPadded(form)))

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
