package services

import (
	model "32-Adarsha/model"
	"database/sql"
	"time"
)

// RecordJobEvent writes a row to JobEvent. eventType is "status_change"
// when fromStatus != toStatus, "note" for free-form notes (toStatus
// can equal fromStatus), or "system" for app-generated events.
func RecordJobEvent(db *sql.DB, jobID int, eventType, fromStatus, toStatus, note string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(
		`INSERT INTO JobEvent (job_id, event_type, from_status, to_status, note, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, eventType, fromStatus, toStatus, note, time.Now(),
	)
	return err
}

// GetJobEvents returns the events for a job, newest first.
func GetJobEvents(db *sql.DB, jobID int) []model.JobEvent {
	if db == nil {
		return nil
	}
	rows, err := db.Query(
		`SELECT id, job_id, event_type, COALESCE(from_status, ''), COALESCE(to_status, ''), COALESCE(note, ''), created_at
		 FROM JobEvent WHERE job_id = ? ORDER BY created_at DESC`,
		jobID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []model.JobEvent
	for rows.Next() {
		var e model.JobEvent
		if err := rows.Scan(&e.Id, &e.JobId, &e.EventType, &e.FromStatus, &e.ToStatus, &e.Note, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// StaleApplication is a job that has been in the Applied status for
// daysThreshold or more days with no further status change. Returned
// rows are minimum-sufficient for the dashboard banner; callers can
// follow up with a full GetAllJobs/lookup when they need the rest.
type StaleApplication struct {
	JobID         int
	Company       string
	Role          string
	AppliedAt     time.Time
	DaysSince     int
}

// GetStaleApplications returns Applied-status jobs whose most-recent
// JobEvent is older than daysThreshold days. Used for the dashboard
// stale-applications banner.
func GetStaleApplications(db *sql.DB, daysThreshold int) []StaleApplication {
	if db == nil {
		return nil
	}
	// SQLite date math: `datetime('now', '-N days')`. We compare to the
	// MAX(created_at) of events for each Applied job.
	query := `
		SELECT j.id, j.company, j.role,
		       (SELECT MAX(created_at) FROM JobEvent WHERE job_id = j.id) AS last_event
		FROM Job j
		WHERE j.status = 'Applied'
		  AND (
		    SELECT MAX(created_at) FROM JobEvent WHERE job_id = j.id
		  ) < datetime('now', ?)
		ORDER BY last_event ASC`
	rows, err := db.Query(query, "-"+itoa(daysThreshold)+" days")
	if err != nil {
		return nil
	}
	defer rows.Close()

	now := time.Now()
	var out []StaleApplication
	for rows.Next() {
		var s StaleApplication
		var lastEvent sql.NullTime
		if err := rows.Scan(&s.JobID, &s.Company, &s.Role, &lastEvent); err != nil {
			continue
		}
		if lastEvent.Valid {
			s.AppliedAt = lastEvent.Time
			s.DaysSince = int(now.Sub(lastEvent.Time).Hours() / 24)
		}
		out = append(out, s)
	}
	return out
}

// itoa avoids fmt.Sprintf for one micro-allocation in the hot path of
// the stale-applications query (fired on every dashboard refresh).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
