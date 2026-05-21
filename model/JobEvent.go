package model

import "time"

// JobEvent records a single point in a job's lifecycle. Written
// automatically when the Job.Status field changes (and could be
// written manually for free-form notes / system events in future).
//
// Used for the EditView timeline and dashboard stale-alerts.
type JobEvent struct {
	Id         int
	JobId      int
	EventType  string // "status_change" | "note" | "system"
	FromStatus string
	ToStatus   string
	Note       string
	CreatedAt  time.Time
}
