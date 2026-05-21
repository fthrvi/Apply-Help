package ui

import (
	model "32-Adarsha/model"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// JobAction is a discrete row-level operation a user can trigger from
// the per-row menu (P0 quick-actions). Each value maps to a status
// change or destructive op; the caller's onQuickAction handler
// performs the actual DB update + refresh.
type JobAction string

const (
	ActionMarkApplied   JobAction = "applied"
	ActionMarkInterview JobAction = "interview"
	ActionMarkOffer     JobAction = "offer"
	ActionMarkRejected  JobAction = "rejected"
	ActionDelete        JobAction = "delete"
)

type JobTable struct {
	*widget.Table
	Rows []model.Job
	// Selected is keyed by job ID. Tracked for P2 bulk-action support.
	// nil means "selection disabled for this table"; callers that want
	// bulk-select pass a non-nil map and observe size changes via
	// OnSelectionChange.
	Selected         map[int]bool
	OnSelectionChange func()
}

// statusBgColor returns the badge background color for a job status.
// Light pastel fills so the default label text (dark) stays readable.
func statusBgColor(s string) color.NRGBA {
	switch s {
	case "Applied":
		return color.NRGBA{R: 209, G: 250, B: 229, A: 255} // emerald-100
	case "Interview":
		return color.NRGBA{R: 237, G: 233, B: 254, A: 255} // violet-100
	case "Offer":
		return color.NRGBA{R: 110, G: 231, B: 183, A: 255} // emerald-300
	case "Rejected":
		return color.NRGBA{R: 254, G: 226, B: 226, A: 255} // red-100
	case "Processed":
		return color.NRGBA{R: 207, G: 250, B: 254, A: 255} // cyan-100
	case "Pending":
		return color.NRGBA{R: 254, G: 243, B: 199, A: 255} // amber-100
	case "New", "":
		return color.NRGBA{R: 219, G: 234, B: 254, A: 255} // blue-100
	default:
		return color.NRGBA{R: 229, G: 231, B: 235, A: 255} // gray-200
	}
}

// NewJobTable constructs the table widget.
//
//   - onView is called when a row is clicked (or the View button tapped)
//   - onQuickAction is called when one of the row's ⋮-menu items is
//     selected. When nil, the ⋮ button is hidden.
//   - selected (optional, P2) — a map of job-id → bool the table mutates
//     when checkboxes toggle. When nil, the checkbox column is hidden.
func NewJobTable(win fyne.Window, data []model.Job, onView func(model.Job), onQuickAction func(model.Job, JobAction)) *JobTable {
	jt := &JobTable{Rows: data, Selected: map[int]bool{}}

	cols := 7 // checkbox, ID, Company, Role, Link, Status, Action
	t := widget.NewTable(
		func() (int, int) {
			return len(jt.Rows) + 1, cols
		},
		func() fyne.CanvasObject {
			// Each cell can render as one of four shapes. We build all
			// four into a single Stack and toggle visibility per cell:
			//   - check (col 0)
			//   - label  (cols 1-5, also the header row)
			//   - actions (col 6 data rows: View + ⋮)
			//   - bg     (status pill background, only col 5 data rows)
			label := widget.NewLabel("")
			label.Truncation = fyne.TextTruncateEllipsis
			check := widget.NewCheck("", nil)
			check.Hide()
			viewBtn := widget.NewButton("View", nil)
			moreBtn := widget.NewButtonWithIcon("", theme.MoreVerticalIcon(), nil)
			moreBtn.Importance = widget.LowImportance
			actions := container.NewHBox(viewBtn, moreBtn)
			actions.Hide()
			bg := canvas.NewRectangle(color.Transparent)
			bg.CornerRadius = 6
			return container.NewStack(bg, label, check, actions)
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			stack := o.(*fyne.Container)
			bg := stack.Objects[0].(*canvas.Rectangle)
			label := stack.Objects[1].(*widget.Label)
			check := stack.Objects[2].(*widget.Check)
			actions := stack.Objects[3].(*fyne.Container)
			viewBtn := actions.Objects[0].(*widget.Button)
			moreBtn := actions.Objects[1].(*widget.Button)

			// Reset transient state per render. Fyne recycles cell
			// widgets across rows/columns, so previous bindings would
			// leak if we didn't.
			bg.FillColor = color.Transparent
			bg.Refresh()
			check.Hide()
			actions.Hide()
			label.Show()
			label.TextStyle = fyne.TextStyle{}

			// HEADER ROW
			if id.Row == 0 {
				headers := []string{"", "ID", "Company", "Role", "Link", "Status", "Action"}
				label.TextStyle = fyne.TextStyle{Bold: true}
				if id.Col < len(headers) {
					label.SetText(headers[id.Col])
				}
				return
			}

			// DATA ROWS
			dataRowIdx := id.Row - 1
			if dataRowIdx >= len(jt.Rows) {
				return
			}
			job := jt.Rows[dataRowIdx]

			// CHECKBOX COLUMN (col 0)
			if id.Col == 0 {
				if jt.Selected == nil {
					return
				}
				label.Hide()
				check.Show()
				check.OnChanged = nil
				check.SetChecked(jt.Selected[job.Id])
				check.OnChanged = func(v bool) {
					if v {
						jt.Selected[job.Id] = true
					} else {
						delete(jt.Selected, job.Id)
					}
					if jt.OnSelectionChange != nil {
						jt.OnSelectionChange()
					}
				}
				return
			}

			// ACTION COLUMN (col 6)
			if id.Col == 6 {
				label.Hide()
				actions.Show()
				viewBtn.OnTapped = func() {
					if onView != nil {
						onView(job)
					}
				}
				if onQuickAction == nil {
					moreBtn.Hide()
				} else {
					moreBtn.Show()
					moreBtn.OnTapped = func() {
						menu := fyne.NewMenu("",
							fyne.NewMenuItem("Mark as Applied", func() { onQuickAction(job, ActionMarkApplied) }),
							fyne.NewMenuItem("Mark as Interview", func() { onQuickAction(job, ActionMarkInterview) }),
							fyne.NewMenuItem("Mark as Offer", func() { onQuickAction(job, ActionMarkOffer) }),
							fyne.NewMenuItem("Mark as Rejected", func() { onQuickAction(job, ActionMarkRejected) }),
							fyne.NewMenuItemSeparator(),
							fyne.NewMenuItem("Delete", func() { onQuickAction(job, ActionDelete) }),
						)
						if win != nil {
							pop := widget.NewPopUpMenu(menu, win.Canvas())
							pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(moreBtn)
							pop.ShowAtPosition(pos.Add(fyne.NewPos(0, moreBtn.Size().Height)))
						}
					}
				}
				return
			}

			// REGULAR DATA CELLS (cols 1-5)
			rowValues := job.ToStringSlice() // 5 elements: id, company, role, link, status
			dataCol := id.Col - 1            // shift past checkbox
			if dataCol >= 0 && dataCol < len(rowValues) {
				label.SetText(rowValues[dataCol])
			}

			// STATUS BADGE (col 5)
			if id.Col == 5 {
				status := job.Status
				if status == "" {
					status = "New"
				}
				label.SetText(status)
				bg.FillColor = statusBgColor(status)
				bg.Refresh()
			}
		},
	)

	// Row-click → onView (the View button is still there for users
	// who target it directly; either path works). Skip clicks on the
	// checkbox or action columns to avoid double-firing.
	t.OnSelected = func(id widget.TableCellID) {
		defer t.UnselectAll()
		if id.Row == 0 {
			return
		}
		if id.Col == 0 || id.Col == 6 {
			return
		}
		dataIdx := id.Row - 1
		if dataIdx < 0 || dataIdx >= len(jt.Rows) {
			return
		}
		if onView != nil {
			onView(jt.Rows[dataIdx])
		}
	}

	jt.Table = t
	return jt
}

func (jt *JobTable) UpdateData(newData []model.Job) {
	jt.Rows = newData
	// Garbage-collect selections for rows that no longer exist (the
	// data refreshed and dropped some).
	if jt.Selected != nil {
		living := map[int]bool{}
		for _, j := range newData {
			living[j.Id] = true
		}
		for id := range jt.Selected {
			if !living[id] {
				delete(jt.Selected, id)
			}
		}
	}
	jt.Table.Refresh()
}

// ClearSelection drops all checked rows. Used after a bulk action so
// the selection set doesn't linger across operations.
func (jt *JobTable) ClearSelection() {
	if jt.Selected == nil {
		return
	}
	for k := range jt.Selected {
		delete(jt.Selected, k)
	}
	if jt.OnSelectionChange != nil {
		jt.OnSelectionChange()
	}
	jt.Table.Refresh()
}
