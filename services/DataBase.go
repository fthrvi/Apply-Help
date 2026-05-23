package services

import (
	model "32-Adarsha/model"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type ErrorLog struct {
	Id        int
	Message   string
	Timestamp time.Time
}

var GlobalDB *sql.DB

func InitDb() *sql.DB {
	dbPath := filepath.Join(AppDir(), "jobs.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}

	if err := db.Ping(); err != nil {
		log.Fatal("Database unreachable:", err)
	}

	// API keys live in this file; restrict to owner-read so a multi-user
	// machine doesn't expose them via default 0644 perms.
	_ = os.Chmod(dbPath, 0600)

	createTable(db)
	seedDefaults(db)
	GlobalDB = db
	return db
}

func seedDefaults(db *sql.DB) {
	// Only seed if Settings is empty
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM Settings").Scan(&count)
	if err != nil || count > 0 {
		return
	}

	// API Keys (Placeholders)
	SaveSetting(db, KeyGeminiURL, "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent")

	// Local LLM defaults — point at the user's home server on Tailscale
	// and use a 7B model that fits comfortably in a 16GB VRAM GPU.
	// Editable in Settings → API Keys → Local LLM.
	SaveSetting(db, KeyLocalLLMEndpoint, "http://prithvi-system-product-name:11434")
	SaveSetting(db, KeyLocalLLMModel, "qwen2.5:7b-instruct")
	SaveSetting(db, KeyLocalLLMEmbedModel, "nomic-embed-text")

	// Prompts, schemas, and HTML templates come from embedded defaults so a
	// fresh install works without any sibling files on disk.
	SaveSetting(db, KeyExtractPrompt, defaultExtractionPrompt)
	SaveSetting(db, KeyCombinedPrompt, defaultCombinedPrompt)
	SaveSetting(db, KeyCombinedSchema, defaultCombinedSchema)
	SaveSetting(db, KeyResumeParsePrompt, defaultResumeParsePrompt)
	SaveSetting(db, KeyTranscriptParsePrompt, defaultTranscriptParsePrompt)
	SaveSetting(db, KeyEmailClassifyPrompt, defaultEmailClassifyPrompt)
	SaveSetting(db, KeyResumeTemplate, defaultResumeTemplate)
	SaveSetting(db, KeyCoverTemplate, defaultCoverTemplate)

	// Initial User Info Structure
	emptyUserInfo := `{"name":"","email":"","phone":"","location":"","linkedin":"","github":"","education":[],"experience":[],"projects":[],"skills":{"languages":[],"frameworks":[],"dev_tools":[],"databases":[]},"awards":[]}`
	SaveSetting(db, KeyUserInfo, emptyUserInfo)
}

func createTable(db *sql.DB) {
	query := `
    CREATE TABLE IF NOT EXISTS Job (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        company TEXT,
        role TEXT,
        link TEXT,
        status TEXT,
        created_at DATETIME,
        description TEXT,
        resume TEXT,
        coverLetter TEXT,
        question TEXT,
        resume_data TEXT,
        cover_data TEXT,
        source TEXT,
        has_document INTEGER DEFAULT 0
    );`

	_, err := db.Exec(query)
	if err != nil {
		log.Fatal("Failed to create table:", err)
	}

	// Settings table
	querySettings := `
    CREATE TABLE IF NOT EXISTS Settings (
        key TEXT PRIMARY KEY,
        value TEXT
    );`
	_, err = db.Exec(querySettings)
	if err != nil {
		log.Fatal("Failed to create Settings table:", err)
	}

	// Migrations: Add new columns to existing Job table if missing
	_, _ = db.Exec("ALTER TABLE Job ADD COLUMN resume_data TEXT;")
	_, _ = db.Exec("ALTER TABLE Job ADD COLUMN cover_data TEXT;")
	_, _ = db.Exec("ALTER TABLE Job ADD COLUMN source TEXT;")
	_, _ = db.Exec("ALTER TABLE Job ADD COLUMN has_document INTEGER DEFAULT 0;")

	// Backfill has_document for existing jobs
	_, _ = db.Exec("UPDATE Job SET has_document = 1 WHERE coalesce(resume, '') != '' AND coalesce(coverLetter, '') != '';")

	// ErrorLogs table
	queryLogs := `
    CREATE TABLE IF NOT EXISTS ErrorLogs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        message TEXT,
        timestamp DATETIME
    );`
	_, _ = db.Exec(queryLogs)

	// EmailScan table — tracks which inbox messages we've already
	// classified so a re-sync doesn't re-spend LLM tokens on the same
	// email. message_id is the RFC 2822 Message-ID header value.
	queryScan := `
    CREATE TABLE IF NOT EXISTS EmailScan (
        message_id TEXT PRIMARY KEY,
        seen_at DATETIME,
        matched_job_id INTEGER,
        derived_status TEXT,
        company TEXT,
        subject TEXT
    );`
	_, _ = db.Exec(queryScan)

	// GitHubBulletCache — LLM-polished résumé bullets per repo, keyed by
	// URL with a content hash so the cache invalidates when description
	// or README content changes. See PolishProjectBulletsWithLLM.
	queryBullets := `
    CREATE TABLE IF NOT EXISTS GitHubBulletCache (
        url TEXT PRIMARY KEY,
        content_hash TEXT,
        bullets TEXT,
        updated_at DATETIME
    );`
	_, _ = db.Exec(queryBullets)

	// JobDescriptionCache — cleaned job-page text keyed by SHA-256 of the
	// URL. Lets re-Generate (or re-Fetch) the same job avoid both the HTTP
	// round-trip and the StripHTML/chromedp work. 24h TTL enforced by the
	// caller, not the schema.
	queryDescCache := `
    CREATE TABLE IF NOT EXISTS JobDescriptionCache (
        url_hash TEXT PRIMARY KEY,
        description TEXT,
        fetched_via TEXT,
        fetched_at DATETIME
    );`
	_, _ = db.Exec(queryDescCache)

	// LLMResponseCache — full LLM response keyed by a hash of (model +
	// final prompt). Re-clicking Generate with identical inputs returns
	// instantly with zero tokens spent. Any input change (description,
	// profile, schema, prompt template) yields a different hash.
	queryLLMCache := `
    CREATE TABLE IF NOT EXISTS LLMResponseCache (
        input_hash TEXT PRIMARY KEY,
        response TEXT,
        cached_at DATETIME
    );`
	_, _ = db.Exec(queryLLMCache)

	// JobEvent — timeline of status transitions per job. Used by the
	// EditView timeline tab and the dashboard stale-application banner.
	queryEvents := `
    CREATE TABLE IF NOT EXISTS JobEvent (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        job_id INTEGER,
        event_type TEXT,
        from_status TEXT,
        to_status TEXT,
        note TEXT,
        created_at DATETIME
    );`
	_, _ = db.Exec(queryEvents)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_jobevent_job_id ON JobEvent(job_id);`)

	// Template / schema / prompt migration. The historical defaults had
	// hardcoded "[YOUR FULL NAME]" / "[STREET ADDRESS]" / hardcoded
	// company-and-school entries baked into the template, plus a schema
	// that used fixed UNM1/MO1/PROJ1 keys. The new defaults use raw HTML
	// section placeholders ({!- ExperienceSection -!}) populated from
	// UserInfo arrays + LLM-tailored bullets. When we detect any of the
	// old markers, overwrite the stored value — the user couldn't have
	// reasonably "customized" a broken template that doesn't substitute.
	storedResume := GetSetting(db, KeyResumeTemplate)
	// Sentinel "font-size: 0.76rem" marks the one-page-compact CSS.
	// Earlier sentinels stay as legacy guards for older installs.
	if strings.Contains(storedResume, "[YOUR FULL NAME]") ||
		strings.Contains(storedResume, "[STREET ADDRESS]") ||
		strings.Contains(storedResume, "UNM Mentoring Institute") ||
		strings.Contains(storedResume, "{- UNM1 -}") ||
		!strings.Contains(storedResume, "font-size: 0.76rem") {
		_ = SaveSetting(db, KeyResumeTemplate, defaultResumeTemplate)
	}

	storedCover := GetSetting(db, KeyCoverTemplate)
	// Sentinel: new cover drops City_State_Zip; old one still has it.
	if strings.Contains(storedCover, "[FIRST NAME]") ||
		strings.Contains(storedCover, "[STREET ADDRESS]") ||
		strings.Contains(storedCover, "[YOUR FULL NAME]") ||
		strings.Contains(storedCover, "{- City_State_Zip -}") ||
		!strings.Contains(storedCover, "font-size: 1.45rem") {
		_ = SaveSetting(db, KeyCoverTemplate, defaultCoverTemplate)
	}

	storedSchema := GetSetting(db, KeyCombinedSchema)
	schemaNeedsUpdate := strings.Contains(storedSchema, "UNM1") ||
		strings.Contains(storedSchema, "Tec_List") ||
		strings.Contains(storedSchema, "City_State_Zip") ||
		!strings.Contains(storedSchema, "ExperienceBullets") ||
		!strings.Contains(storedSchema, "RelevantProjectIndices")
	if schemaNeedsUpdate {
		_ = SaveSetting(db, KeyCombinedSchema, defaultCombinedSchema)
	}

	storedPrompt := GetSetting(db, KeyCombinedPrompt)
	promptNeedsUpdate := !strings.Contains(storedPrompt, "ExperienceBullets") ||
		!strings.Contains(storedPrompt, "RelevantProjectIndices") ||
		!strings.Contains(storedPrompt, "filler — strip them out") ||
		!strings.Contains(storedPrompt, "MUST FIT ON ONE US LETTER PAGE")
	if promptNeedsUpdate {
		_ = SaveSetting(db, KeyCombinedPrompt, defaultCombinedPrompt)
	}

	// Bust the LLM response cache when we replace prompt/schema — the
	// stored input_hash values are tied to the old prompt format and
	// would serve stale (old-schema) responses on cache hit.
	if schemaNeedsUpdate || promptNeedsUpdate {
		_, _ = db.Exec("DELETE FROM LLMResponseCache")
	}

	// Backfill Local-LLM defaults for users who upgraded from before
	// the feature existed (seedDefaults only fires on empty Settings).
	if GetSetting(db, KeyLocalLLMEndpoint) == "" {
		_ = SaveSetting(db, KeyLocalLLMEndpoint, "http://prithvi-system-product-name:11434")
	}
	if GetSetting(db, KeyLocalLLMModel) == "" {
		_ = SaveSetting(db, KeyLocalLLMModel, "qwen2.5:7b-instruct")
	}
	if GetSetting(db, KeyLocalLLMEmbedModel) == "" {
		_ = SaveSetting(db, KeyLocalLLMEmbedModel, "nomic-embed-text")
	}
	if GetSetting(db, KeyRemoteDebugURL) == "" {
		_ = SaveSetting(db, KeyRemoteDebugURL, "http://localhost:9222")
	}
}

func CreateJob(db *sql.DB, j model.Job) (int64, error) {
	hasDoc := 0
	if j.Resume != "" && j.Coverletter != "" {
		hasDoc = 1
	}

	// Dedup at insert time: skip rows whose non-empty link already exists.
	// Empty-link rows (manual entries) are always allowed through. Returning
	// id=0 with no error signals "deduplicated, not an error" to callers.
	query := `
INSERT INTO Job (company, role, link, status, created_at, description, resume, coverLetter, question, resume_data, cover_data, source, has_document)
SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
WHERE ? = '' OR NOT EXISTS (SELECT 1 FROM Job WHERE link = ?)`

	result, err := db.Exec(query,
		j.Company, j.Role, j.Link, j.Status, time.Now(), j.Description,
		j.Resume, j.Coverletter, j.Question, j.ResumeData, j.CoverData, j.Source, hasDoc,
		j.Link, j.Link,
	)
	if err != nil {
		return 0, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return 0, nil
	}
	id, err := result.LastInsertId()
	if err == nil && id > 0 {
		// Creation event — gives every job a deterministic "start" entry
		// in its timeline.
		_ = RecordJobEvent(db, int(id), "system", "", j.Status, "Job added")
	}
	return id, err
}

func UpdateJob(db *sql.DB, j model.Job) error {
	hasDoc := 0
	if j.Resume != "" && j.Coverletter != "" {
		hasDoc = 1
	}

	// Capture pre-update status so we can detect transitions and write
	// a JobEvent row. One extra SELECT per update is acceptable — Job
	// table is small and writes aren't hot.
	var oldStatus string
	_ = db.QueryRow("SELECT status FROM Job WHERE id = ?", j.Id).Scan(&oldStatus)

	query := `UPDATE Job SET company=?, role=?, link=?, status=?, description=?, resume=?, coverLetter=?, question=?, resume_data=?, cover_data=?, source=?, has_document=?
	          WHERE id=?`

	_, err := db.Exec(query, j.Company, j.Role, j.Link, j.Status, j.Description, j.Resume, j.Coverletter, j.Question, j.ResumeData, j.CoverData, j.Source, hasDoc, j.Id)
	if err != nil {
		return err
	}

	// Record a transition event when status changed. The "system"
	// initial event written by CreateJob means oldStatus will already
	// be set for any job created through the app.
	if oldStatus != j.Status {
		_ = RecordJobEvent(db, j.Id, "status_change", oldStatus, j.Status, "")
	}
	return nil
}

func DeleteJob(db *sql.DB, id int) error {
	query := `DELETE FROM Job WHERE id = ?`
	_, err := db.Exec(query, id)
	return err
}

func GetAllJobs(db *sql.DB) ([]model.Job, error) {
	query := `SELECT id, company, role, link, status, created_at, description, resume, coverLetter, question, 
	          COALESCE(resume_data, ''), COALESCE(cover_data, ''), COALESCE(source, ''), COALESCE(has_document, 0) 
              FROM Job 
              ORDER BY id DESC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []model.Job

	for rows.Next() {
		var j model.Job
		err := rows.Scan(
			&j.Id,
			&j.Company,
			&j.Role,
			&j.Link,
			&j.Status,
			&j.Created,
			&j.Description,
			&j.Resume,
			&j.Coverletter,
			&j.Question,
			&j.ResumeData,
			&j.CoverData,
			&j.Source,
			&j.HasDocument,
		)

		if err != nil {
			log.Printf("❌ Error scanning job row: %v", err)
			return nil, err
		}

		jobs = append(jobs, j)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return jobs, nil
}

func GetSetting(db *sql.DB, key string) string {
	var value string
	err := db.QueryRow("SELECT value FROM Settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		return ""
	}
	return value
}

func SaveSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO Settings (key, value) VALUES (?, ?)", key, value)
	return err
}

func GetUserInfo(db *sql.DB) (*model.UserInfo, error) {
	val := GetSetting(db, KeyUserInfo)
	if val == "" {
		return &model.UserInfo{}, nil
	}

	var ui model.UserInfo
	err := json.Unmarshal([]byte(val), &ui)
	if err != nil {
		return nil, err
	}
	return &ui, nil
}

func SaveUserInfo(db *sql.DB, ui *model.UserInfo) error {
	data, err := json.Marshal(ui)
	if err != nil {
		return err
	}
	return SaveSetting(db, KeyUserInfo, string(data))
}

func LogError(db *sql.DB, message string) {
	if db == nil {
		db = GlobalDB
	}
	if db == nil {
		return
	}
	_, _ = db.Exec("INSERT INTO ErrorLogs (message, timestamp) VALUES (?, ?)", message, time.Now())
}

func GetAllErrors(db *sql.DB) []ErrorLog {
	if db == nil {
		db = GlobalDB
	}
	rows, err := db.Query("SELECT id, message, timestamp FROM ErrorLogs ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var logs []ErrorLog
	for rows.Next() {
		var l ErrorLog
		_ = rows.Scan(&l.Id, &l.Message, &l.Timestamp)
		logs = append(logs, l)
	}
	return logs
}

func ClearErrors(db *sql.DB) {
	if db == nil {
		db = GlobalDB
	}
	_, _ = db.Exec("DELETE FROM ErrorLogs")
}
