package ui

import (
	model "32-Adarsha/model"
	"32-Adarsha/services"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func tableResizer(t *widget.Table, container fyne.CanvasObject) {
	width := container.Size().Width - 32 // Accounting for the small side padding
	if width < 100 {
		return
	}

	// Perfectly equal columns across 100% of the width
	colWidth := width / 6

	t.SetColumnWidth(0, colWidth)
	t.SetColumnWidth(1, colWidth)
	t.SetColumnWidth(2, colWidth)
	t.SetColumnWidth(3, colWidth)
	t.SetColumnWidth(4, colWidth)
	t.SetColumnWidth(5, colWidth)
}

func CreateMainWindow(app fyne.App, db *sql.DB) fyne.Window {
	win := app.NewWindow("AutoApply Dashboard")

	allJobs, err := services.GetAllJobs(db)
	if err != nil {
		fmt.Printf("❌ Initial load failed: %v\n", err)
	}

	var searchEntry *widget.Entry
	var mainLayout *fyne.Container
	var docsTable *JobTable
	var noDocsTable *JobTable

	refreshTable := func() {
		jobs, err := services.GetAllJobs(db)
		if err != nil {
			fmt.Printf("❌ Failed to refresh jobs: %v\n", err)
			return
		}
		allJobs = jobs
		query := strings.ToLower(searchEntry.Text)

		var docsFiltered []model.Job
		var noDocsFiltered []model.Job

		for _, j := range allJobs {
			if strings.Contains(strings.ToLower(j.Company), query) ||
				strings.Contains(strings.ToLower(j.Role), query) {
				if j.HasDocument == 1 {
					docsFiltered = append(docsFiltered, j)
				} else {
					noDocsFiltered = append(noDocsFiltered, j)
				}
			}
		}
		docsTable.UpdateData(docsFiltered)
		noDocsTable.UpdateData(noDocsFiltered)
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

	syncBtn := widget.NewButtonWithIcon("Sync Email", theme.MailReplyIcon(), func() {
		showGmailSyncDialog(win, db, func() {
			refreshTable()
		})
	})

	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		settingsView := BuildSettingsView(win, db, func() {
			win.SetContent(mainLayout)
		})
		win.SetContent(settingsView)
	})

	onViewJob := func(job model.Job) {
		editView := BuildEditJobView(win, db, job, func() {
			refreshTable()
		}, func() {
			win.SetContent(mainLayout)
		})
		win.SetContent(editView)
	}

	var initialDocs []model.Job
	var initialNoDocs []model.Job
	for _, j := range allJobs {
		if j.HasDocument == 1 {
			initialDocs = append(initialDocs, j)
		} else {
			initialNoDocs = append(initialNoDocs, j)
		}
	}

	docsTable = NewJobTable(initialDocs, onViewJob)
	noDocsTable = NewJobTable(initialNoDocs, onViewJob)

	tabs := container.NewAppTabs(
		container.NewTabItem("With Documents", container.NewPadded(docsTable)),
		container.NewTabItem("Without Documents", container.NewPadded(noDocsTable)),
	)

	topRow := container.NewBorder(nil, nil, nil, container.NewHBox(addBtn, syncBtn, settingsBtn), searchEntry)
	mainLayout = container.NewBorder(
		container.NewPadded(topRow),
		nil, nil, nil,
		tabs,
	)

	win.SetContent(mainLayout)
	win.Resize(fyne.NewSize(1200, 800))

	// The window switches to fullscreen on launch (see main.go); wait for
	// the transition to settle before computing column widths from the final
	// canvas size. A single timer fire replaces the previous polling goroutine
	// that woke up 10 times during startup.
	time.AfterFunc(750*time.Millisecond, func() {
		fyne.Do(func() {
			tableResizer(docsTable.Table, win.Content())
			tableResizer(noDocsTable.Table, win.Content())
		})
	})

	return win
}
