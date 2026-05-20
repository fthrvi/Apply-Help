package services

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
)

// EmailSyncResult summarizes what one Sync call did. Returned to the
// UI so it can render a human-readable report.
type EmailSyncResult struct {
	Scanned       int
	NewClassified int
	Updated       int
	Skipped       int
	Log           []string
	Errors        []string
}

// ClassifiedEmail is the JSON shape we expect the LLM to return for
// each email (see services/defaults/email_classify_prompt.txt).
type ClassifiedEmail struct {
	Company    string `json:"company"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	Confidence string `json:"confidence"`
}

// SyncGmail logs into the user's Gmail via IMAP+app-password, fetches
// every message in the inbox newer than `daysBack` days, classifies
// each previously-unseen one with the configured LLM, and updates the
// matching Job row's status when the classifier is confident enough.
//
// Pre-reqs: KeyGmailAddress + KeyGmailAppPassword settings must be
// filled in. 2FA + an app password from myaccount.google.com is the
// expected setup; the regular Gmail password will NOT work for IMAP
// after Google's less-secure-app deprecation.
func SyncGmail(modelChoice string, daysBack int, logFn func(string)) (*EmailSyncResult, error) {
	if daysBack <= 0 {
		daysBack = 30
	}
	result := &EmailSyncResult{}
	emit := func(s string) {
		result.Log = append(result.Log, s)
		if logFn != nil {
			logFn(s)
		}
	}

	addr := strings.TrimSpace(GetSetting(GlobalDB, KeyGmailAddress))
	pwd := strings.TrimSpace(GetSetting(GlobalDB, KeyGmailAppPassword))
	if addr == "" || pwd == "" {
		return result, fmt.Errorf("Gmail address and app password not set (Settings → API Keys → Gmail Sync)")
	}

	emit(fmt.Sprintf("Connecting to imap.gmail.com as %s…", addr))
	c, err := client.DialTLS("imap.gmail.com:993", &tls.Config{ServerName: "imap.gmail.com"})
	if err != nil {
		return result, fmt.Errorf("IMAP dial: %w", err)
	}
	defer c.Logout()

	if err := c.Login(addr, pwd); err != nil {
		return result, fmt.Errorf("IMAP login failed: %w — double-check the app password", err)
	}

	if _, err := c.Select("INBOX", true); err != nil {
		return result, fmt.Errorf("select INBOX: %w", err)
	}

	since := time.Now().AddDate(0, 0, -daysBack)
	crit := imap.NewSearchCriteria()
	crit.Since = since
	emit(fmt.Sprintf("Searching messages since %s…", since.Format("2006-01-02")))

	uids, err := c.Search(crit)
	if err != nil {
		return result, fmt.Errorf("IMAP search: %w", err)
	}
	if len(uids) == 0 {
		emit("No messages in window.")
		return result, nil
	}
	emit(fmt.Sprintf("Found %d candidate messages.", len(uids)))

	seen, err := loadSeenMessageIDs(GlobalDB, since)
	if err != nil {
		emit(fmt.Sprintf("(warning) couldn't load seen IDs: %v", err))
		seen = map[string]bool{}
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids...)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid, section.FetchItem()}
	messages := make(chan *imap.Message, 32)
	done := make(chan error, 1)

	go func() {
		done <- c.Fetch(seqSet, items, messages)
	}()

	for msg := range messages {
		result.Scanned++
		if msg == nil || msg.Envelope == nil {
			continue
		}
		msgID := msg.Envelope.MessageId
		if msgID == "" || seen[msgID] {
			result.Skipped++
			continue
		}

		var body string
		for _, lit := range msg.Body {
			raw, _ := io.ReadAll(lit)
			body = extractEmailBody(raw)
			break
		}
		if strings.TrimSpace(body) == "" {
			continue
		}

		from := envelopeFrom(msg.Envelope)
		subject := strings.TrimSpace(msg.Envelope.Subject)

		classified, err := classifyEmail(from, subject, body, modelChoice)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", subject, err))
			continue
		}
		result.NewClassified++

		var matchedJobID int64
		if classified.Status != "" && classified.Status != "Other" && classified.Company != "" {
			matchedJobID = matchJobByCompany(GlobalDB, classified.Company)
			if matchedJobID > 0 {
				_, _ = GlobalDB.Exec("UPDATE Job SET status = ? WHERE id = ?", classified.Status, matchedJobID)
				result.Updated++
				emit(fmt.Sprintf("✓ %q → %s (job #%d, %s)", subject, classified.Status, matchedJobID, classified.Company))
			} else {
				emit(fmt.Sprintf("? %q → %s (no matching job for %q)", subject, classified.Status, classified.Company))
			}
		}

		markSeen(GlobalDB, msgID, matchedJobID, classified.Status, classified.Company, subject)
	}

	if err := <-done; err != nil {
		return result, fmt.Errorf("IMAP fetch: %w", err)
	}
	emit(fmt.Sprintf("Done. Scanned=%d, classified=%d, updated=%d, skipped=%d.",
		result.Scanned, result.NewClassified, result.Updated, result.Skipped))
	return result, nil
}

// ── helpers ───────────────────────────────────────────────────────────

func extractEmailBody(raw []byte) string {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		// Not MIME — treat as plain.
		return string(raw)
	}
	defer mr.Close()

	var plain, htmlBody string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if h, ok := p.Header.(*mail.InlineHeader); ok {
			ct, _, _ := h.ContentType()
			data, _ := io.ReadAll(p.Body)
			if strings.HasPrefix(ct, "text/plain") && plain == "" {
				plain = string(data)
			} else if strings.HasPrefix(ct, "text/html") && htmlBody == "" {
				htmlBody = string(data)
			}
		}
	}
	if plain != "" {
		return plain
	}
	if htmlBody != "" {
		return StripHTML(htmlBody)
	}
	return ""
}

func envelopeFrom(env *imap.Envelope) string {
	if len(env.From) == 0 {
		return ""
	}
	a := env.From[0]
	if a.PersonalName != "" {
		return fmt.Sprintf("%s <%s@%s>", a.PersonalName, a.MailboxName, a.HostName)
	}
	return fmt.Sprintf("%s@%s", a.MailboxName, a.HostName)
}

func classifyEmail(from, subject, body, modelChoice string) (*ClassifiedEmail, error) {
	tpl := GetSetting(GlobalDB, KeyEmailClassifyPrompt)
	if tpl == "" {
		tpl = defaultEmailClassifyPrompt
	}
	if len(body) > 4000 {
		body = body[:4000]
	}
	prompt := fmt.Sprintf(tpl, from, subject, body)

	raw, err := PromptAI(prompt, modelChoice)
	if err != nil {
		return nil, err
	}
	jsonStr := strings.TrimSpace(raw)
	if !strings.HasPrefix(jsonStr, "{") {
		re := regexp.MustCompile(`(?s)\{.*\}`)
		if m := re.FindString(jsonStr); m != "" {
			jsonStr = m
		}
	}
	var c ClassifiedEmail
	if err := json.Unmarshal([]byte(jsonStr), &c); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (raw=%q)", err, raw)
	}
	return &c, nil
}

var companySuffixRE = regexp.MustCompile(`(?i)\s+(inc|inc\.|llc|llc\.|corp|corp\.|ltd|ltd\.|co\.?|gmbh|sa|s\.a\.)$`)

func normalizeCompany(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = companySuffixRE.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// matchJobByCompany returns the id of the most-likely Job matching the
// company name from a classified email, or 0 if no good match. Heuristic:
// exact normalized match wins; otherwise the longest substring overlap.
// Already-closed states (Rejected, Offer) are excluded so a follow-up
// email can't reopen them.
func matchJobByCompany(db *sql.DB, company string) int64 {
	target := normalizeCompany(company)
	if target == "" {
		return 0
	}
	rows, err := db.Query("SELECT id, company FROM Job WHERE coalesce(status,'') NOT IN ('Rejected', 'Offer')")
	if err != nil {
		return 0
	}
	defer rows.Close()

	var bestID int64
	var bestLen int
	for rows.Next() {
		var id int64
		var c string
		if err := rows.Scan(&id, &c); err != nil {
			continue
		}
		cn := normalizeCompany(c)
		if cn == "" {
			continue
		}
		if cn == target {
			return id
		}
		if strings.Contains(target, cn) || strings.Contains(cn, target) {
			if len(cn) > bestLen {
				bestID = id
				bestLen = len(cn)
			}
		}
	}
	return bestID
}

func loadSeenMessageIDs(db *sql.DB, since time.Time) (map[string]bool, error) {
	rows, err := db.Query("SELECT message_id FROM EmailScan WHERE seen_at > ?", since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			seen[id] = true
		}
	}
	return seen, nil
}

func markSeen(db *sql.DB, msgID string, jobID int64, status, company, subject string) {
	_, _ = db.Exec(`
        INSERT OR REPLACE INTO EmailScan (message_id, seen_at, matched_job_id, derived_status, company, subject)
        VALUES (?, ?, ?, ?, ?, ?)`,
		msgID, time.Now(), jobID, status, company, subject)
}
