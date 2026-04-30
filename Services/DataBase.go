package services

import (
	model "32-Adarsha/Model"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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
	db, err := sql.Open("sqlite", "jobs.db")
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}

	if err := db.Ping(); err != nil {
		log.Fatal("Database unreachable:", err)
	}

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
	SaveSetting(db, "GEMINI_URL", "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent")

	// Helper to read default files
	readDef := func(name string) string {
		data, _ := os.ReadFile(filepath.Join("defaults", name))
		return string(data)
	}

	// Prompts from files
	SaveSetting(db, "EXTRACTION_PROMPT", readDef("extraction_prompt.txt"))
	SaveSetting(db, "COMBINED_PROMPT", readDef("combined_prompt.txt"))

	// Schemas from files
	SaveSetting(db, "COMBINED_SCHEMA", readDef("combined_schema.json"))

	// Helper to read placeholder files
	readPlace := func(name string) string {
		data, _ := os.ReadFile(filepath.Join("placeholder", name))
		return string(data)
	}

	// Templates from placeholder folder
	SaveSetting(db, "RESUME_TEMPLATE", readPlace("resume.html"))
	SaveSetting(db, "COVER_TEMPLATE", readPlace("cover.html"))

	// Initial User Info Structure
	emptyUserInfo := `{"name":"","email":"","phone":"","location":"","linkedin":"","github":"","education":[],"experience":[],"projects":[],"skills":{"languages":[],"frameworks":[],"dev_tools":[],"databases":[]},"awards":[]}`
	SaveSetting(db, "USER_INFO", emptyUserInfo)
}

func createTable(db *sql.DB) {
	query := `
    CREATE TABLE IF NOT EXISTS Job (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        company TEXT,
        role TEXT,
        link TEXT,
        status INTEGER,
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
}

func CreateJob(db *sql.DB, j model.Job) (int64, error) {
	hasDoc := 0
	if j.Resume != "" && j.Coverletter != "" {
		hasDoc = 1
	}

	query := `INSERT INTO Job (company, role, link, status, created_at, description, resume, coverLetter, question, resume_data, cover_data, source, has_document) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := db.Exec(query, j.Company, j.Role, j.Link, j.Status, time.Now(), j.Description, j.Resume, j.Coverletter, j.Question, j.ResumeData, j.CoverData, j.Source, hasDoc)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func UpdateJob(db *sql.DB, j model.Job) error {
	hasDoc := 0
	if j.Resume != "" && j.Coverletter != "" {
		hasDoc = 1
	}

	query := `UPDATE Job SET company=?, role=?, link=?, status=?, description=?, resume=?, coverLetter=?, question=?, resume_data=?, cover_data=?, source=?, has_document=? 
	          WHERE id=?`

	_, err := db.Exec(query, j.Company, j.Role, j.Link, j.Status, j.Description, j.Resume, j.Coverletter, j.Question, j.ResumeData, j.CoverData, j.Source, hasDoc, j.Id)
	return err
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
	val := GetSetting(db, "USER_INFO")
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
	return SaveSetting(db, "USER_INFO", string(data))
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
