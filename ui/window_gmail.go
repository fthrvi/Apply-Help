package ui

import (
	"32-Adarsha/services"
	"database/sql"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// showGmailSyncDialog opens a modal that runs services.SyncGmail and
// streams its log lines into a scrollable text area. onComplete fires
// after the sync (success or failure) so the caller can refresh any
// affected views (e.g. the dashboard job table).
func showGmailSyncDialog(win fyne.Window, db *sql.DB, onComplete func()) {
	_ = db // currently uses services.GlobalDB; reserved for future per-DB sync

	if strings.TrimSpace(services.GetSetting(services.GlobalDB, services.KeyGmailAddress)) == "" ||
		strings.TrimSpace(services.GetSetting(services.GlobalDB, services.KeyGmailAppPassword)) == "" {
		dialog.ShowInformation("Gmail not configured",
			"Go to Settings → API Keys → Gmail Sync and fill in your address + app password first.\n\nYou'll need 2-step verification on your Google account and an app password from myaccount.google.com/apppasswords.",
			win)
		return
	}

	modelRadio := widget.NewRadioGroup([]string{"Gemini", "Claude", "OpenAI"}, nil)
	modelRadio.Horizontal = true
	modelRadio.SetSelected("Gemini")

	daysSelect := widget.NewSelect([]string{"7", "14", "30", "60", "90"}, nil)
	daysSelect.SetSelected("30")

	logArea := widget.NewMultiLineEntry()
	logArea.Wrapping = fyne.TextWrapWord
	logArea.SetMinRowsVisible(18)

	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	var syncBtn *widget.Button
	syncBtn = widget.NewButton("Sync Now", func() {
		syncBtn.Disable()
		progress.Show()
		logArea.SetText("")

		days := 30
		switch daysSelect.Selected {
		case "7":
			days = 7
		case "14":
			days = 14
		case "60":
			days = 60
		case "90":
			days = 90
		}

		var lines []string
		appendLine := func(s string) {
			lines = append(lines, s)
			fyne.Do(func() {
				logArea.SetText(strings.Join(lines, "\n"))
				logArea.CursorRow = len(lines)
			})
		}

		go func() {
			result, err := services.SyncGmail(modelRadio.Selected, days, appendLine)
			fyne.Do(func() {
				progress.Hide()
				syncBtn.Enable()
				if err != nil {
					appendLine(fmt.Sprintf("\n❌ %v", err))
				} else if result != nil {
					appendLine(fmt.Sprintf("\n✅ Scanned %d, classified %d, updated %d, skipped %d.",
						result.Scanned, result.NewClassified, result.Updated, result.Skipped))
				}
				if onComplete != nil {
					onComplete()
				}
			})
		}()
	})
	syncBtn.Importance = widget.HighImportance

	header := container.NewVBox(
		widget.NewLabelWithStyle("Sync Gmail → Job Status", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Reads recent inbox messages, classifies each one with the selected LLM, and updates the matching Job row's status. Already-rejected and already-offer jobs are skipped."),
		widget.NewLabel("LLM"),
		modelRadio,
		widget.NewLabel("Look back (days)"),
		daysSelect,
		container.NewGridWithColumns(1, syncBtn),
		progress,
		widget.NewSeparator(),
		widget.NewLabel("Log"),
	)
	content := container.NewBorder(header, nil, nil, nil, container.NewVScroll(logArea))

	d := dialog.NewCustom("Gmail Sync", "Close", content, win)
	d.Resize(fyne.NewSize(900, 800))
	d.Show()
}
