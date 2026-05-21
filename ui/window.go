package ui

import (
	model "32-Adarsha/model"
	pull "32-Adarsha/joppull"
	"32-Adarsha/services"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func tableResizer(t *widget.Table, container fyne.CanvasObject) {
	width := container.Size().Width - 32
	if width < 100 {
		return
	}

	// 7 columns: checkbox (narrow), ID (narrow), Company, Role, Link,
	// Status, Action (medium). The middle four share the remaining
	// width; the edges are fixed.
	const checkW = 40
	const idW = 60
	const statusW = 110
	const actionW = 110
	remaining := width - checkW - idW - statusW - actionW
	mid := remaining / 3 // Company / Role / Link

	t.SetColumnWidth(0, checkW)
	t.SetColumnWidth(1, idW)
	t.SetColumnWidth(2, mid)
	t.SetColumnWidth(3, mid)
	t.SetColumnWidth(4, mid)
	t.SetColumnWidth(5, statusW)
	t.SetColumnWidth(6, actionW)
}

func CreateMainWindow(app fyne.App, db *sql.DB) fyne.Window {
	win := app.NewWindow("AutoApply Dashboard")

	allJobs, err := services.GetAllJobs(db)
	if err != nil {
		fmt.Printf("❌ Initial load failed: %v\n", err)
	}

	var searchEntry *widget.Entry
	var mainLayout *fyne.Container
	// Three mutually-exclusive buckets:
	//   Inbox    — has_document=0 AND status not in pipeline (new from scrape, nothing done)
	//   Drafts   — has_document=1 AND status not in pipeline (PDFs generated, not applied yet)
	//   Pipeline — status in {Applied, Interview, Offer, Rejected} (in-flight, regardless of docs)
	var inboxTable, draftsTable, pipelineTable *JobTable
	var tabs *container.AppTabs

	// Chip filter state. activeChips is keyed by tag label; absent or
	// false means "this chip is off". rebuildChipBar rewrites chipBarHost
	// from the saved JobPreferences plus current session toggles, then
	// triggers a refreshTable so the visible jobs reflect the new filter.
	activeChips := map[string]bool{}
	chipBarHost := container.NewVBox()
	var rebuildChipBar func()
	// Forward-declared so the bulk-action bar closures (defined just
	// below) can call it; the assignment is further down where the
	// rest of the dashboard state is in scope.
	var refreshTable func()

	// Inline stats label that lives in the top bar. Updated on every
	// refreshTable call so it reflects the *unfiltered* totals — gives
	// the user a steady "where do I stand" signal independent of the
	// current search / chip filter.
	statsLabel := widget.NewLabel("")
	statsLabel.TextStyle = fyne.TextStyle{Italic: true}
	statsLabel.Alignment = fyne.TextAlignTrailing

	// Stale-applications banner — shown only when at least one
	// application has been in "Applied" with no further event for 14+
	// days. Clicking jumps to Pipeline and surfaces a warning-tone
	// rectangle so it's visible without being aggressive.
	staleBannerBtn := widget.NewButton("", func() {
		if tabs != nil {
			tabs.SelectIndex(2) // Pipeline
		}
	})
	staleBannerBtn.Importance = widget.WarningImportance
	staleBannerBtn.Hide()

	// P2 bulk-action bar. Visible only when at least one job is
	// selected across any of the three tables. Buttons apply to the
	// union of selections, so the user can multi-select across tabs
	// and bulk-update in one click.
	bulkLabel := widget.NewLabel("")
	bulkLabel.TextStyle = fyne.TextStyle{Bold: true}

	bulkForEachSelected := func(fn func(j model.Job)) {
		for _, table := range []*JobTable{inboxTable, draftsTable, pipelineTable} {
			for id := range table.Selected {
				for _, j := range table.Rows {
					if j.Id == id {
						fn(j)
						break
					}
				}
			}
		}
	}
	bulkClear := func() {
		inboxTable.ClearSelection()
		draftsTable.ClearSelection()
		pipelineTable.ClearSelection()
	}
	bulkApply := func(newStatus string) {
		bulkForEachSelected(func(j model.Job) {
			j.Status = newStatus
			_ = services.UpdateJob(db, j)
		})
		bulkClear()
		refreshTable()
	}
	bulkDelete := func() {
		total := len(inboxTable.Selected) + len(draftsTable.Selected) + len(pipelineTable.Selected)
		dialog.ShowConfirm(
			fmt.Sprintf("Delete %d job(s)?", total),
			"This removes the selected jobs and all their events. Cannot be undone.",
			func(ok bool) {
				if !ok {
					return
				}
				bulkForEachSelected(func(j model.Job) {
					_ = services.DeleteJob(db, j.Id)
				})
				bulkClear()
				refreshTable()
			}, win)
	}

	bulkAppliedBtn := widget.NewButton("Mark Applied", func() { bulkApply("Applied") })
	bulkRejectedBtn := widget.NewButton("Mark Rejected", func() { bulkApply("Rejected") })
	bulkDeleteBtn := widget.NewButton("Delete", bulkDelete)
	bulkDeleteBtn.Importance = widget.DangerImportance
	bulkClearBtn := widget.NewButton("Clear Selection", bulkClear)
	bulkClearBtn.Importance = widget.LowImportance

	bulkBar := container.NewHBox(bulkLabel, bulkAppliedBtn, bulkRejectedBtn, bulkDeleteBtn, bulkClearBtn)
	bulkBar.Hide()

	// Empty-state labels overlaid on each tab when the filtered list is
	// empty — explains *why* the table is blank instead of leaving the
	// user wondering whether the app is broken.
	mkEmpty := func() *widget.Label {
		l := widget.NewLabel("")
		l.Alignment = fyne.TextAlignCenter
		l.TextStyle = fyne.TextStyle{Italic: true}
		l.Hide()
		return l
	}
	inboxEmpty := mkEmpty()
	draftsEmpty := mkEmpty()
	pipelineEmpty := mkEmpty()

	refreshTable = func() {
		jobs, err := services.GetAllJobs(db)
		if err != nil {
			fmt.Printf("❌ Failed to refresh jobs: %v\n", err)
			return
		}
		allJobs = jobs
		query := strings.ToLower(searchEntry.Text)

		// Build the union of keywords from every currently-active chip.
		// Empty = no chip filter; JobMatchesKeywords treats that as
		// "match everything", preserving the firehose.
		var activeKeywords []string
		if len(activeChips) > 0 {
			prefs, _ := services.GetJobPreferences(db)
			for _, t := range prefs.Tags {
				if !t.Enabled {
					continue
				}
				if activeChips[t.Label] {
					activeKeywords = append(activeKeywords, t.Keywords...)
				}
			}
		}

		var inboxFiltered, draftsFiltered, pipelineFiltered []model.Job

		isPipelineStatus := func(s string) bool {
			switch s {
			case "Applied", "Interview", "Offer", "Rejected":
				return true
			}
			return false
		}

		for _, j := range allJobs {
			if query != "" {
				if !strings.Contains(strings.ToLower(j.Company), query) &&
					!strings.Contains(strings.ToLower(j.Role), query) {
					continue
				}
			}
			if !services.JobMatchesKeywords(j.Role, j.Company, activeKeywords) {
				continue
			}
			// Pipeline beats Drafts beats Inbox — a job that's been
			// marked Applied shows in Pipeline even if it also has docs
			// (which it usually does).
			switch {
			case isPipelineStatus(j.Status):
				pipelineFiltered = append(pipelineFiltered, j)
			case j.HasDocument == 1:
				draftsFiltered = append(draftsFiltered, j)
			default:
				inboxFiltered = append(inboxFiltered, j)
			}
		}
		inboxTable.UpdateData(inboxFiltered)
		draftsTable.UpdateData(draftsFiltered)
		pipelineTable.UpdateData(pipelineFiltered)

		// Stats strip — count from the unfiltered universe so it stays
		// stable across search/chip toggles and gives the user a sense
		// of overall pipeline.
		applied, interview, offer, rejected := 0, 0, 0, 0
		for _, j := range allJobs {
			switch j.Status {
			case "Applied":
				applied++
			case "Interview":
				interview++
			case "Offer":
				offer++
			case "Rejected":
				rejected++
			}
		}
		parts := []string{fmt.Sprintf("%d total", len(allJobs))}
		if applied > 0 {
			parts = append(parts, fmt.Sprintf("%d applied", applied))
		}
		if interview > 0 {
			parts = append(parts, fmt.Sprintf("%d interview", interview))
		}
		if offer > 0 {
			parts = append(parts, fmt.Sprintf("%d offer", offer))
		}
		if rejected > 0 {
			parts = append(parts, fmt.Sprintf("%d rejected", rejected))
		}
		statsLabel.SetText(strings.Join(parts, "  ·  "))

		// Stale-applications surface — 14-day default threshold.
		stale := services.GetStaleApplications(db, 14)
		if len(stale) == 0 {
			staleBannerBtn.Hide()
		} else {
			staleBannerBtn.SetText(fmt.Sprintf("%d application(s) stale (no response in 14+ days) — review in Pipeline", len(stale)))
			staleBannerBtn.Show()
		}

		// Empty-state messaging: distinguishes "no jobs yet" from
		// "filter matched nothing" so the user knows what to act on.
		setEmpty := func(lbl *widget.Label, n int, defaultMsg string) {
			if n > 0 {
				lbl.Hide()
				return
			}
			if query != "" || len(activeChips) > 0 {
				lbl.SetText("No jobs match the current filter. Clear search / chips to see more.")
			} else {
				lbl.SetText(defaultMsg)
			}
			lbl.Show()
		}
		setEmpty(inboxEmpty, len(inboxFiltered),
			"Inbox is empty. SimplifyJobs scrape runs every 60s on launch — new postings land here.")
		setEmpty(draftsEmpty, len(draftsFiltered),
			"No drafts yet. Open a job from Inbox and click Generate to produce a tailored resume.")
		setEmpty(pipelineEmpty, len(pipelineFiltered),
			"Pipeline is empty. Set a job's status to Applied / Interview / Offer / Rejected to track it here.")

		win.SetContent(mainLayout)
	}

	rebuildChipBar = func() {
		prefs, _ := services.GetJobPreferences(db)

		// Collect enabled tags; ignore disabled ones (the user toggled
		// them off in Settings → keep saved but hide from dashboard).
		var enabledTags []model.JobTag
		for _, t := range prefs.Tags {
			if t.Enabled && len(t.Keywords) > 0 {
				enabledTags = append(enabledTags, t)
			}
		}

		if len(enabledTags) == 0 {
			chipBarHost.Objects = nil
			chipBarHost.Refresh()
			return
		}

		// Garbage-collect session state for tags that no longer exist;
		// default new tags to active=true so the user sees their full
		// preference set after first generation.
		living := map[string]bool{}
		for _, t := range enabledTags {
			living[t.Label] = true
			if _, ok := activeChips[t.Label]; !ok {
				activeChips[t.Label] = true
			}
		}
		for k := range activeChips {
			if !living[k] {
				delete(activeChips, k)
			}
		}

		bar := container.NewHBox(widget.NewLabel("Filter:"))
		for _, t := range enabledTags {
			tag := t
			btn := widget.NewButton(tag.Label, nil)
			if activeChips[tag.Label] {
				btn.Importance = widget.HighImportance
			} else {
				btn.Importance = widget.LowImportance
			}
			btn.OnTapped = func() {
				activeChips[tag.Label] = !activeChips[tag.Label]
				rebuildChipBar()
				refreshTable()
			}
			bar.Add(btn)
		}
		clearBtn := widget.NewButton("Clear", func() {
			for k := range activeChips {
				activeChips[k] = false
			}
			rebuildChipBar()
			refreshTable()
		})
		clearBtn.Importance = widget.LowImportance
		bar.Add(clearBtn)

		chipBarHost.Objects = []fyne.CanvasObject{container.NewPadded(bar)}
		chipBarHost.Refresh()
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

	syncBtn := widget.NewButtonWithIcon("Sync Email", theme.MailReplyIcon(), func() {
		showGmailSyncDialog(win, db, func() {
			refreshTable()
		})
	})

	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		settingsView := BuildSettingsView(win, db, func() {
			// Job Preferences may have changed; rebuild the chip bar
			// before showing the dashboard again.
			rebuildChipBar()
			refreshTable()
			win.SetContent(mainLayout)
		})
		win.SetContent(settingsView)
	})

	// Per-row quick action handler — wired up in P0. Status changes
	// go through services.UpdateJob (which records JobEvents — P1).
	// Delete confirms before destroying.
	onQuickAction := func(job model.Job, action JobAction) {
		switch action {
		case ActionMarkApplied:
			job.Status = "Applied"
			_ = services.UpdateJob(db, job)
		case ActionMarkInterview:
			job.Status = "Interview"
			_ = services.UpdateJob(db, job)
		case ActionMarkOffer:
			job.Status = "Offer"
			_ = services.UpdateJob(db, job)
		case ActionMarkRejected:
			job.Status = "Rejected"
			_ = services.UpdateJob(db, job)
		case ActionDelete:
			dialog.ShowConfirm(
				"Delete "+job.Company+" — "+job.Role+"?",
				"This removes the job row and all its events. Cannot be undone.",
				func(ok bool) {
					if !ok {
						return
					}
					_ = services.DeleteJob(db, job.Id)
					refreshTable()
				}, win)
			return
		}
		refreshTable()
	}

	onViewJob := func(job model.Job) {
		editView := BuildEditJobView(win, db, job, func() {
			// Save / Submit / Delete: refresh tables and navigate back.
			// The dashboard's three-tab routing puts the updated job in
			// the right bucket automatically; we don't force-switch the
			// active tab so the user stays oriented to whatever they
			// were viewing before.
			refreshTable()
			win.SetContent(mainLayout)
		}, func() {
			// Plain back-arrow: refresh in case has_document or status
			// changed (the regen handler updates them on the EditView).
			refreshTable()
			win.SetContent(mainLayout)
		})
		win.SetContent(editView)
	}

	var initialInbox, initialDrafts, initialPipeline []model.Job
	for _, j := range allJobs {
		switch j.Status {
		case "Applied", "Interview", "Offer", "Rejected":
			initialPipeline = append(initialPipeline, j)
			continue
		}
		if j.HasDocument == 1 {
			initialDrafts = append(initialDrafts, j)
		} else {
			initialInbox = append(initialInbox, j)
		}
	}

	inboxTable = NewJobTable(win, initialInbox, onViewJob, onQuickAction)
	draftsTable = NewJobTable(win, initialDrafts, onViewJob, onQuickAction)
	pipelineTable = NewJobTable(win, initialPipeline, onViewJob, onQuickAction)

	// Wire bulk-bar visibility: any time selection across any table
	// changes, recompute the union count and toggle the bar.
	onBulkSelChange := func() {
		total := len(inboxTable.Selected) + len(draftsTable.Selected) + len(pipelineTable.Selected)
		if total == 0 {
			bulkBar.Hide()
			return
		}
		bulkLabel.SetText(fmt.Sprintf("%d selected:", total))
		bulkBar.Show()
	}
	inboxTable.OnSelectionChange = onBulkSelChange
	draftsTable.OnSelectionChange = onBulkSelChange
	pipelineTable.OnSelectionChange = onBulkSelChange

	tabs = container.NewAppTabs(
		container.NewTabItem("Inbox", container.NewPadded(
			container.NewStack(inboxTable, container.NewCenter(inboxEmpty)),
		)),
		container.NewTabItem("Drafts", container.NewPadded(
			container.NewStack(draftsTable, container.NewCenter(draftsEmpty)),
		)),
		container.NewTabItem("Pipeline", container.NewPadded(
			container.NewStack(pipelineTable, container.NewCenter(pipelineEmpty)),
		)),
	)

	// Stats label sits between the search field and the action buttons,
	// padded for a bit of breathing room.
	topRow := container.NewBorder(
		nil, nil,
		nil,
		container.NewHBox(container.NewPadded(statsLabel), addBtn, syncBtn, settingsBtn),
		searchEntry,
	)
	mainLayout = container.NewBorder(
		container.NewVBox(
			container.NewPadded(topRow),
			chipBarHost,
			container.NewPadded(staleBannerBtn),
			container.NewPadded(bulkBar),
		),
		nil, nil, nil,
		tabs,
	)

	// First chip-bar render. Reads saved JobPreferences from the DB; if
	// none exist (user hasn't generated yet), the host renders empty
	// (zero height) and the dashboard looks identical to before.
	rebuildChipBar()
	// Re-apply tag filter to the initial snapshot so the dashboard isn't
	// briefly unfiltered before the first user interaction.
	refreshTable()

	win.SetContent(mainLayout)
	win.Resize(fyne.NewSize(1200, 800))

	// The window switches to fullscreen on launch (see main.go); wait for
	// the transition to settle before computing column widths from the final
	// canvas size. A single timer fire replaces the previous polling goroutine
	// that woke up 10 times during startup.
	time.AfterFunc(750*time.Millisecond, func() {
		fyne.Do(func() {
			tableResizer(inboxTable.Table, win.Content())
			tableResizer(draftsTable.Table, win.Content())
			tableResizer(pipelineTable.Table, win.Content())
		})
	})

	// Background SimplifyJobs poll. Fires once immediately to catch up on
	// anything new since the last launch, then ticks every pollInterval.
	// PullLatestJobs uses an ETag-conditional GET, so the steady-state
	// "nothing new" tick costs zero rate-limit tokens. New rows trigger a
	// silent table refresh via fyne.Do(refreshTable).
	const pollInterval = 60 * time.Second
	pollDone := make(chan struct{})
	win.SetOnClosed(func() {
		select {
		case <-pollDone:
			// already closed
		default:
			close(pollDone)
		}
	})
	go func() {
		doPull := func() {
			n, err := pull.PullLatestJobs(db)
			if err == nil && n > 0 {
				fyne.Do(refreshTable)
			}
		}
		doPull()

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				doPull()
			case <-pollDone:
				return
			}
		}
	}()

	return win
}
