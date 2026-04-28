package ui

import (
	model "32-Adarsha/Model"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type JobTable struct {
	*widget.Table
	Rows []model.Job
}

func NewJobTable(data []model.Job, onView func(model.Job)) *JobTable {
	jt := &JobTable{Rows: data}

	t := widget.NewTable(
		func() (int, int) {
			return len(jt.Rows) + 1, 6
		},
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			b := widget.NewButton("View", nil)
			b.Hide()
			// Remove NewCenter so the button spans the whole cell width
			return container.NewStack(l, b)
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			stack := o.(*fyne.Container)
			label := stack.Objects[0].(*widget.Label)
			btn := stack.Objects[1].(*widget.Button)

			// 1. HEADER ROW
			if id.Row == 0 {
				btn.Hide()
				label.Show()
				label.TextStyle = fyne.TextStyle{Bold: true}
				headers := []string{"ID", "Company", "Role", "Link", "Status", "Action"}
				if id.Col < len(headers) {
					label.SetText(headers[id.Col])
				}
				return
			}

			// 2. DATA ROWS (Offset by 1)
			dataRowIdx := id.Row - 1
			if dataRowIdx >= len(jt.Rows) {
				return
			}

			label.TextStyle = fyne.TextStyle{} // Reset style for data
			job := jt.Rows[dataRowIdx]

			if id.Col == 5 {
				label.Hide()
				btn.Show()
				btn.OnTapped = func() {
					if onView != nil {
						onView(job)
					}
				}
			} else {
				btn.Hide()
				label.Show()
				rowValues := job.ToStringSlice()
				if id.Col < len(rowValues) {
					label.SetText(rowValues[id.Col])
				}
			}
		},
	)

	jt.Table = t
	return jt
}

func (jt *JobTable) UpdateData(newData []model.Job) {
	jt.Rows = newData
	jt.Table.Refresh()
}
