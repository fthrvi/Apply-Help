package pull

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	model "32-Adarsha/model"
	"32-Adarsha/services"

	"github.com/PuerkitoBio/goquery"
)

// Commit helps us parse the GitHub API JSON response.
type Commit struct {
	Sha string `json:"sha"`
}

var simplifyClient = &http.Client{Timeout: 30 * time.Second}

// PullLatestJobs scrapes new postings from SimplifyJobs/New-Grad-Positions
// and inserts them into Job. Returns the number of rows actually added
// (post-SQL-dedup) and any non-recoverable error.
//
// Polling discipline:
//   - The commits endpoint is queried with `If-None-Match: <stored-etag>`,
//     so the common "nothing changed" path responds 304 with no body —
//     and 304 responses do not count against the GitHub rate limit.
//   - When KeyGithubToken is set, the call is authenticated, lifting the
//     limit from 60/hr to 5000/hr.
//
// Both KeyLastSimplifyETag (the conditional-GET token) and KeyLastCommitSHA
// (a defensive equality check, in case GitHub rotates ETag formats without
// the underlying commit changing) are persisted across launches.
func PullLatestJobs(db *sql.DB) (int, error) {
	token := strings.TrimSpace(services.GetSetting(db, services.KeyGithubToken))

	apiURL := "https://api.github.com/repos/SimplifyJobs/New-Grad-Positions/commits?path=README.md&sha=dev"
	apiReq, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, err
	}
	apiReq.Header.Set("User-Agent", "applyhelp")
	apiReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		apiReq.Header.Set("Authorization", "Bearer "+token)
	}
	if prevETag := strings.TrimSpace(services.GetSetting(db, services.KeyLastSimplifyETag)); prevETag != "" {
		apiReq.Header.Set("If-None-Match", prevETag)
	}

	apiResp, err := simplifyClient.Do(apiReq)
	if err != nil {
		log.Printf("Simplify probe network error: %v", err)
		return 0, err
	}
	defer apiResp.Body.Close()

	// 304 Not Modified — the cheap common path. No rate-limit charge.
	if apiResp.StatusCode == http.StatusNotModified {
		return 0, nil
	}
	if apiResp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(apiResp.Body, 1<<16))
		err := fmt.Errorf("Simplify probe HTTP %d: %s", apiResp.StatusCode, strings.TrimSpace(string(body)))
		log.Println(err)
		return 0, err
	}

	var commits []Commit
	if err := json.NewDecoder(apiResp.Body).Decode(&commits); err != nil {
		log.Printf("Error parsing Simplify probe JSON: %v", err)
		return 0, err
	}
	if len(commits) == 0 {
		return 0, nil
	}
	latestSha := commits[0].Sha
	newETag := apiResp.Header.Get("ETag")

	// Defensive: if the ETag turned over but the underlying commit didn't,
	// don't re-scrape. Just refresh the stored ETag so the next probe is
	// still a cheap 304.
	if services.GetSetting(db, services.KeyLastCommitSHA) == latestSha {
		if newETag != "" {
			_ = services.SaveSetting(db, services.KeyLastSimplifyETag, newETag)
		}
		return 0, nil
	}

	fmt.Printf("Successfully found latest commit: %s\n", latestSha)

	// Fetch the README rendered as HTML so we don't need a markdown table
	// parser. application/vnd.github.html does the rendering server-side.
	readmeURL := "https://api.github.com/repos/SimplifyJobs/New-Grad-Positions/readme?ref=dev"
	req, err := http.NewRequest("GET", readmeURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github.html")
	req.Header.Set("User-Agent", "applyhelp")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	fileResp, err := simplifyClient.Do(req)
	if err != nil {
		log.Printf("Error downloading README HTML: %v", err)
		return 0, err
	}
	defer fileResp.Body.Close()

	if fileResp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(fileResp.Body, 1<<16))
		err := fmt.Errorf("GitHub README HTTP %d: %s", fileResp.StatusCode, strings.TrimSpace(string(body)))
		log.Println(err)
		return 0, err
	}

	doc, err := goquery.NewDocumentFromReader(fileResp.Body)
	if err != nil {
		log.Printf("Failed to parse README HTML: %v", err)
		return 0, err
	}

	var jobsAdded int
	currentCompany := "Unknown"

	// CreateJob deduplicates by link at the SQL layer, so we just insert;
	// colliding rows come back as id=0 with no error.
	doc.Find("tr").Each(func(i int, s *goquery.Selection) {
		cols := s.Find("td")
		if cols.Length() != 5 {
			return
		}

		companyText := strings.TrimSpace(cols.Eq(0).Text())
		if companyText != "↳" && companyText != "" {
			currentCompany = strings.ReplaceAll(companyText, "🔥 ", "")
		}

		role := strings.TrimSpace(cols.Eq(1).Text())
		linkTag := cols.Eq(3).Find("a").First()
		link, exists := linkTag.Attr("href")
		if !exists {
			return
		}

		j := model.Job{
			Company: currentCompany,
			Role:    role,
			Link:    link,
			Status:  "New",
			Source:  "Simplify",
		}

		id, err := services.CreateJob(db, j)
		if err != nil {
			log.Printf("Failed to insert job: %v", err)
		} else if id > 0 {
			jobsAdded++
		}
	})

	fmt.Printf("Added %d new jobs from commit %s\n", jobsAdded, latestSha)
	_ = services.SaveSetting(db, services.KeyLastCommitSHA, latestSha)
	if newETag != "" {
		_ = services.SaveSetting(db, services.KeyLastSimplifyETag, newETag)
	}
	return jobsAdded, nil
}
