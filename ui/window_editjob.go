package ui

import (
	model "32-Adarsha/model"
	"32-Adarsha/services"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/chromedp/chromedp"
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

		// Cache previews under the user's cache dir, keyed by hash of the
		// source path so two jobs whose PDFs share a basename
		// (e.g. resume.pdf in different per-company subdirs) don't collide.
		cacheRoot, err := os.UserCacheDir()
		if err != nil {
			cacheRoot = os.TempDir()
		}
		previewDir := filepath.Join(cacheRoot, "applyhelp", "previews")
		_ = os.MkdirAll(previewDir, 0700)

		sum := sha1.Sum([]byte(pdfPath))
		pngPath := filepath.Join(previewDir, hex.EncodeToString(sum[:8])+".png")

		// Prefer screenshotting the same-basename HTML that AutoApply wrote
		// alongside the PDF — chromedp renders HTML reliably across
		// platforms, while screenshotting a PDF in headless Chrome is
		// quirky. Fall back to the PDF if the HTML is gone for any reason.
		source := strings.TrimSuffix(pdfPath, ".pdf") + ".html"
		if _, herr := os.Stat(source); herr != nil {
			source = pdfPath
		}
		absSource, err := filepath.Abs(source)
		if err != nil {
			fmt.Printf("❌ Preview: abs path failed: %v\n", err)
		}

		fmt.Printf("🔨 Preview: chromedp full-page screenshot of %s\n", absSource)
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

		// FullScreenshot captures the entire scrollable document height,
		// not just the visible viewport. Multi-page resumes render as one
		// tall PNG that the Fyne scroll container can pan through.
		var imgBytes []byte
		err = chromedp.Run(runCtx,
			chromedp.EmulateViewport(900, 1200),
			chromedp.Navigate("file://"+absSource),
			chromedp.Sleep(500*time.Millisecond),
			chromedp.FullScreenshot(&imgBytes, 90),
		)
		if err != nil {
			fmt.Printf("❌ Preview: chromedp screenshot failed: %v\n", err)
		} else {
			_ = os.WriteFile(pngPath, imgBytes, 0600)
		}

		// Read PNG dimensions so the canvas.Image takes its natural
		// size inside the scroll container instead of collapsing to
		// Fyne's default minimum.
		var imgW, imgH int
		if f, ferr := os.Open(pngPath); ferr == nil {
			cfg, _, derr := image.DecodeConfig(f)
			f.Close()
			if derr == nil {
				imgW = cfg.Width
				imgH = cfg.Height
			}
		}

		fyne.Do(func() {
			containerObj.Objects = nil
			if _, err := os.Stat(pngPath); os.IsNotExist(err) {
				fmt.Printf("❌ Preview: PNG missing %s\n", pngPath)
				containerObj.Add(widget.NewLabel("Preview generation failed for: " + pdfPath))
			} else {
				fmt.Printf("✅ Preview: Loading PNG %s (%dx%d)\n", pngPath, imgW, imgH)
				img := canvas.NewImageFromFile(pngPath)
				img.FillMode = canvas.ImageFillOriginal
				if imgW > 0 && imgH > 0 {
					img.SetMinSize(fyne.NewSize(float32(imgW), float32(imgH)))
				}
				// Bi-directional scroll so multi-page (tall) and oversized
				// (wide) previews are both navigable.
				containerObj.Add(container.NewScroll(img))
			}
			containerObj.Refresh()
		})
	}()

	return containerObj
}

// renderJobTimeline shows the job's event history — newest at the top.
// Each row: bold date · status transition (or note text) · optional
// "X days ago" suffix. Empty timeline gets a friendly placeholder.
func renderJobTimeline(db *sql.DB, jobID int) fyne.CanvasObject {
	events := services.GetJobEvents(db, jobID)
	if len(events) == 0 {
		lbl := widget.NewLabel("No events recorded yet.\n\nEvents are created automatically when this job's status changes.")
		lbl.Wrapping = fyne.TextWrapWord
		return container.NewPadded(lbl)
	}

	vbox := container.NewVBox()
	now := time.Now()
	for _, e := range events {
		dateStr := e.CreatedAt.Format("Mon, Jan 2, 2006 · 3:04 PM")
		days := int(now.Sub(e.CreatedAt).Hours() / 24)
		var ago string
		switch {
		case days == 0:
			ago = "today"
		case days == 1:
			ago = "yesterday"
		default:
			ago = fmt.Sprintf("%d days ago", days)
		}

		var body string
		switch e.EventType {
		case "status_change":
			from := e.FromStatus
			if from == "" {
				from = "(unset)"
			}
			body = fmt.Sprintf("Status: %s → %s", from, e.ToStatus)
		case "system":
			body = e.Note
			if body == "" {
				body = "System event"
			}
		default:
			body = e.Note
		}

		dateLine := widget.NewLabelWithStyle(dateStr+"   ·   "+ago, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		bodyLine := widget.NewLabel(body)
		bodyLine.Wrapping = fyne.TextWrapWord
		vbox.Add(dateLine)
		vbox.Add(bodyLine)
		vbox.Add(widget.NewSeparator())
	}
	return container.NewVScroll(container.NewPadded(vbox))
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

	// Forward-declared so the Generate / Apply-Changes handlers can
	// refresh the preview tabs in place after producing new PDFs,
	// instead of navigating away. The user stays on the EditView and
	// sees the freshly-generated documents.
	var resumeTab, coverTab, questionTab *container.TabItem
	var tabs *container.AppTabs
	// description is also forward-declared so the Auto-Fill button
	// closure (defined above) can read it as job context — Go closures
	// capture by name, but the name must exist in scope at closure
	// creation time. Assigned further below where the rest of the
	// form fields are wired up.
	var description *widget.Entry

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

	prefillBtn := widget.NewButtonWithIcon("Auto-Fill (Agent)", theme.ComputerIcon(), func() {
		userInfo, err := services.GetUserInfo(db)
		if err != nil || userInfo == nil {
			dialog.ShowError(fmt.Errorf("could not load user info: %v", err), win)
			return
		}
		if userInfo.Name == "" && userInfo.Email == "" {
			dialog.ShowInformation("Profile is empty",
				"Fill in your name and email in Settings → User Profile before using Auto-Fill.", win)
			return
		}

		// Live progress dialog — uses a multi-line entry rather than a
		// Label so the user can select + Cmd+C the agent's reasoning
		// log (much easier to share than screenshotting).
		progressLog := widget.NewMultiLineEntry()
		progressLog.SetText("Starting agent…")
		progressLog.Wrapping = fyne.TextWrapWord
		progressLog.SetMinRowsVisible(18)

		copyBtn := widget.NewButtonWithIcon("Copy Log", theme.ContentCopyIcon(), func() {
			win.Clipboard().SetContent(progressLog.Text)
		})
		copyBtn.Importance = widget.MediumImportance

		dlgBody := container.NewBorder(
			nil,
			container.NewPadded(copyBtn),
			nil, nil,
			progressLog,
		)
		progressDlg := dialog.NewCustom("Auto-Fill Agent", "Close", container.NewPadded(dlgBody), win)
		progressDlg.Resize(fyne.NewSize(640, 460))
		progressDlg.Show()

		progress := func(msg string) {
			fyne.Do(func() {
				current := progressLog.Text
				if current == "Starting agent…" {
					progressLog.SetText(msg)
				} else {
					progressLog.SetText(current + "\n" + msg)
				}
				// Move cursor to end so the latest line is visible.
				progressLog.CursorRow = strings.Count(progressLog.Text, "\n")
				progressLog.Refresh()
			})
		}
		if err := services.RunAutofillAgent(link.Text, userInfo, description.Text, role.Text, company.Text, job.Resume, job.Coverletter, progress); err != nil {
			dialog.ShowError(err, win)
		}
	})

	linkActions := container.NewHBox(openLinkBtn, prefillBtn)
	linkBox := container.NewBorder(nil, nil, nil, linkActions, link)

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

	description = widget.NewMultiLineEntry()
	description.SetText(job.Description)
	description.Wrapping = fyne.TextWrapWord

	// Fetch Description: pulls the job posting via FetchAndCleanDescription
	// (plain HTTP with 24h cache → chromedp fallback on blocked / SPA pages)
	// and pastes the cleaned text into the Description field for review.
	// Decouples fetching from Generate so the user can edit before
	// spending tokens, and re-fetches the same URL are free.
	fetchDescBtn := widget.NewButtonWithIcon("Fetch from URL", theme.SearchIcon(), nil)
	fetchDescBtn.OnTapped = func() {
		rawURL := strings.TrimSpace(link.Text)
		if rawURL == "" {
			dialog.ShowInformation("No URL", "Add a job link first.", win)
			return
		}
		fetchDescBtn.Disable()
		go func() {
			defer fyne.Do(func() { fetchDescBtn.Enable() })
			cleaned, err := services.FetchAndCleanDescription(rawURL)
			if err != nil {
				services.LogError(db, fmt.Sprintf("Fetch description failed: %v", err))
				fyne.Do(func() { dialog.ShowError(err, win) })
				return
			}
			fyne.Do(func() {
				description.SetText(cleaned)
				// Persist immediately so a re-open of this job doesn't
				// need another Fetch click — the next FetchAndClean call
				// would hit the URL cache anyway, but skipping the
				// network round-trip and the extra click is cleaner UX.
				job.Description = cleaned
				_ = services.UpdateJob(db, job)
			})
		}()
	}

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
		rawURL := strings.TrimSpace(link.Text)
		desc := strings.TrimSpace(description.Text)
		// Pass the form's company / role through — when both are
		// populated, RunAutoApply skips the extraction LLM call. One
		// fewer round-trip per Generate.
		knownComp := strings.TrimSpace(company.Text)
		knownRole := strings.TrimSpace(role.Text)
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

			fyne.Do(func() { regenStatus.SetText(fmt.Sprintf("Processing with %s…", selectedModel)) })
			result, err := services.RunAutoApply(rawURL, desc, knownComp, knownRole, selectedModel, func(msg string) {
				fyne.Do(func() { regenStatus.SetText(strings.TrimSpace(msg)) })
			})

			if err != nil {
				fyne.Do(func() { regenStatus.SetText(fmt.Sprintf("Error: %v", err)) })
				return
			}
			if !result.Success {
				fyne.Do(func() { regenStatus.SetText(fmt.Sprintf("Failed: %s", result.Error)) })
				return
			}

			fyne.Do(func() {
				company.SetText(result.Company)
				role.SetText(result.Role)
				description.SetText(result.Description)
				currentStatus = "Processed"
				status.SetText("Processed")
				resume.SetText(result.ResumePath)
				coverLetter.SetText(result.CoverPath)

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

				// Stay on the EditView and refresh the Resume / Cover /
				// Q&A preview tabs in place so the user sees the new
				// PDFs immediately. The dashboard's has_document state
				// will be refreshed by the back-button when the user
				// chooses to leave.
				if resumeTab != nil {
					resumeTab.Content = renderPDFToCanvas(job.Resume)
				}
				if coverTab != nil {
					coverTab.Content = renderPDFToCanvas(job.Coverletter)
				}
				if questionTab != nil {
					questionTab.Content = renderQuestions(job.Description, job.Question)
				}
				if tabs != nil {
					tabs.Refresh()
					// Auto-switch to the Resume tab so the user lands
					// on the visible preview rather than whatever they
					// last had open.
					tabs.SelectIndex(0)
				}
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

	// Section header: caps + slightly muted, mirrors the resume template's
	// uppercase section dividers. Provides visual rhythm without taking
	// much vertical space.
	section := func(title string) fyne.CanvasObject {
		lbl := widget.NewLabelWithStyle(strings.ToUpper(title), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		return container.NewPadded(container.NewVBox(lbl, widget.NewSeparator()))
	}

	descLabel := widget.NewLabelWithStyle("Description", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	descHeader := container.NewBorder(nil, nil, descLabel, fetchDescBtn, nil)
	descRow := container.NewPadded(container.NewBorder(descHeader, nil, nil, nil, description))

	form := container.NewVBox(
		section("Job Details"),
		container.NewPadded(companyRoleBox),
		row("Link", linkBox),
		row("Status", status),

		section("Posting"),
		descRow,

		section("Generation"),
		row("AI Model", modelSelect),
		container.NewPadded(container.NewVBox(
			regenBtn,
			regenProgress,
			regenStatus,
		)),

		section("Output"),
		row("Resume", resume),
		row("Cover Letter", coverLetter),
		row("Q&A Notes", question),
	)

	header := container.NewBorder(nil, nil, backBtn, nil, widget.NewLabelWithStyle("Edit Job Entry", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}))

	formContent := container.NewBorder(header, container.NewPadded(actionButtons), nil, nil, container.NewVScroll(container.NewPadded(form)))

	resumeTab = container.NewTabItem("Resume", renderPDFToCanvas(job.Resume))
	coverTab = container.NewTabItem("Cover Letter", renderPDFToCanvas(job.Coverletter))
	questionTab = container.NewTabItem("Details & Q&A", renderQuestions(job.Description, job.Question))
	timelineTab := container.NewTabItem("Timeline", renderJobTimeline(db, job.Id))

	resEditor, getResJSON := buildJSONEditor(job.ResumeData)
	covEditor, getCovJSON := buildJSONEditor(job.CoverData)

	updateDataBtn := widget.NewButtonWithIcon("Apply Changes & Overwrite PDFs", theme.DocumentSaveIcon(), func() {
		job.ResumeData = getResJSON()
		job.CoverData = getCovJSON()

		result, err := services.RegenerateFromData(job.Company, job.Role, job.ResumeData, job.CoverData, nil)
		if err != nil {
			dialog.ShowError(err, win)
			return
		}
		job.Resume = result.ResumePath
		job.Coverletter = result.CoverPath
		_ = services.UpdateJob(db, job)
		// Same stay-on-page behavior as Generate: refresh the previews
		// in place so the user can see what their edits produced.
		if resumeTab != nil {
			resumeTab.Content = renderPDFToCanvas(job.Resume)
		}
		if coverTab != nil {
			coverTab.Content = renderPDFToCanvas(job.Coverletter)
		}
		if tabs != nil {
			tabs.Refresh()
			tabs.SelectIndex(0)
		}
	})
	updateDataBtn.Importance = widget.HighImportance

	contentEditTab := container.NewTabItem("Edit Content", container.NewBorder(nil, container.NewPadded(updateDataBtn), nil, nil, container.NewAppTabs(
		container.NewTabItem("Resume Content", resEditor),
		container.NewTabItem("Cover Content", covEditor),
	)))

	tabs = container.NewAppTabs(resumeTab, coverTab, contentEditTab, questionTab, timelineTab)

	split := container.NewHSplit(formContent, tabs)
	split.Offset = 0.25

	return split
}
