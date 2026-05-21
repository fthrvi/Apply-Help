package services

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
)

// urlHash hashes a job-page URL for use as a cache key. The first 16
// bytes are kept — collisions cause a cache miss (re-fetch), not data
// corruption.
func urlHash(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h[:16])
}

// promptHash hashes the full LLM call inputs (model + final prompt) so
// any change to description, profile, schema, or prompt template
// naturally invalidates the cache.
func promptHash(model, finalPrompt string) string {
	h := sha256.Sum256([]byte(model + "\x00" + finalPrompt))
	return fmt.Sprintf("%x", h[:16])
}

// GetDescriptionCache returns the cleaned job-page text for a URL if
// cached within ttl, else "". A 24h ttl is a reasonable default — job
// postings change rarely enough that a day-old cache is almost always
// still correct, and the user can edit the description manually if it
// matters.
func GetDescriptionCache(db *sql.DB, url string, ttl time.Duration) string {
	if db == nil {
		return ""
	}
	var desc string
	var fetchedAt time.Time
	err := db.QueryRow(
		"SELECT description, fetched_at FROM JobDescriptionCache WHERE url_hash = ?",
		urlHash(url),
	).Scan(&desc, &fetchedAt)
	if err != nil || desc == "" {
		return ""
	}
	if ttl > 0 && time.Since(fetchedAt) > ttl {
		return ""
	}
	return desc
}

func SaveDescriptionCache(db *sql.DB, url, description, fetchedVia string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(
		`INSERT OR REPLACE INTO JobDescriptionCache (url_hash, description, fetched_via, fetched_at) VALUES (?, ?, ?, ?)`,
		urlHash(url), description, fetchedVia, time.Now(),
	)
	return err
}

// GetLLMResponseCache returns the cached LLM response for the given
// (model, final prompt) pair, or "" on miss. No TTL — the cache key
// includes every input that influences output, so a stale hit is
// impossible unless the user manually deletes the entry.
func GetLLMResponseCache(db *sql.DB, model, finalPrompt string) string {
	if db == nil {
		return ""
	}
	var resp string
	err := db.QueryRow(
		"SELECT response FROM LLMResponseCache WHERE input_hash = ?",
		promptHash(model, finalPrompt),
	).Scan(&resp)
	if err != nil {
		return ""
	}
	return resp
}

func SaveLLMResponseCache(db *sql.DB, model, finalPrompt, response string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(
		`INSERT OR REPLACE INTO LLMResponseCache (input_hash, response, cached_at) VALUES (?, ?, ?)`,
		promptHash(model, finalPrompt), response, time.Now(),
	)
	return err
}
