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
			return len(jt.Rows), 6
		},
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			b := widget.NewButton("View", nil)
			b.Hide()
			return container.NewStack(l, b)
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			if id.Row >= len(jt.Rows) {
				return
			}
			stack := o.(*fyne.Container)
			label := stack.Objects[0].(*widget.Label)
			btn := stack.Objects[1].(*widget.Button)

			job := jt.Rows[id.Row]

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
	jt.SetColumnWidth(0, 50)
	jt.SetColumnWidth(1, 200)
	jt.SetColumnWidth(2, 200)
	jt.SetColumnWidth(3, 300)
	jt.SetColumnWidth(4, 150)

	return jt
}

func (jt *JobTable) UpdateData(newData []model.Job) {
	jt.Rows = newData
	jt.Table.Refresh()
}
