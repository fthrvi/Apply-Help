package pull

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	model "32-Adarsha/Model"
	"32-Adarsha/services"

	"github.com/PuerkitoBio/goquery"
)

// Commit helps us parse the GitHub API JSON response
type Commit struct {
	Sha string `json:"sha"`
}

// Job represents the extracted data structure (kept for reference, though we use model.Job for DB)
type Job struct {
	Company string `json:"company"`
	Role    string `json:"role"`
	Link    string `json:"link"`
	ID      string `json:"id"`
}

// extractID grabs the unique identifier from the end of the URL path
func extractID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/apply")
	path = strings.TrimSuffix(path, "/application")

	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return ""
}

func PullLatestJobs(db *sql.DB) {
	// STEP 1: Use the API to get the latest commit SHA
	apiURL := "https://api.github.com/repos/SimplifyJobs/New-Grad-Positions/commits?path=README.md&sha=dev"
	apiResp, err := http.Get(apiURL)
	if err != nil {
		log.Printf("API Network error: %v", err)
		return
	}
	defer apiResp.Body.Close()

	var commits []Commit
	if err := json.NewDecoder(apiResp.Body).Decode(&commits); err != nil {
		log.Printf("Error parsing API JSON: %v", err)
		return
	}

	if len(commits) == 0 {
		log.Printf("No commits found.")
		return
	}

	latestSha := commits[0].Sha
	fmt.Printf("Successfully found latest commit: %s\n", latestSha)

	lastCommitSha := services.GetSetting(db, "LAST_COMMIT_SHA")
	if lastCommitSha == latestSha {
		fmt.Println("Already up to date with latest commit. No new jobs to pull.")
		return
	}

	// STEP 2: Use the GitHub API to get the README as rendered HTML
	// The raw.githubusercontent.com URL returns markdown text, which goquery can't parse.
	// Instead, use the API with Accept: application/vnd.github.html to get rendered HTML.
	readmeURL := "https://api.github.com/repos/SimplifyJobs/New-Grad-Positions/readme?ref=dev"
	req, err := http.NewRequest("GET", readmeURL, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github.html")

	client := &http.Client{}
	fileResp, err := client.Do(req)
	if err != nil {
		log.Printf("Error downloading README HTML: %v", err)
		return
	}
	defer fileResp.Body.Close()

	if fileResp.StatusCode != 200 {
		body, _ := io.ReadAll(fileResp.Body)
		log.Printf("GitHub API returned status %d: %s", fileResp.StatusCode, string(body))
		return
	}

	// ---------------------------------------------------------
	// STEP 3: Parse the HTML and extract the data
	// ---------------------------------------------------------
	doc, err := goquery.NewDocumentFromReader(fileResp.Body)
	if err != nil {
		log.Printf("Failed to parse document: %v", err)
		return
	}

	var jobsAdded int
	currentCompany := "Unknown"

	// Fetch existing jobs to avoid duplicates
	existingJobs, _ := services.GetAllJobs(db)
	existingLinks := make(map[string]bool)
	for _, job := range existingJobs {
		existingLinks[job.Link] = true
	}

	doc.Find("tr").Each(func(i int, s *goquery.Selection) {
		cols := s.Find("td")

		if cols.Length() == 5 {
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

			// Prevent inserting duplicate jobs based on their Link
			if existingLinks[link] {
				return
			}

			j := model.Job{
				Company: currentCompany,
				Role:    role,
				Link:    link,
				Status:  "New", // Default status
				Source:  "Simplify",
			}

			_, err := services.CreateJob(db, j)
			if err != nil {
				log.Printf("Failed to insert job: %v", err)
			} else {
				jobsAdded++
			}
		}
	})

	// 4. Output the results
	fmt.Printf("Added %d new jobs from commit %s\n", jobsAdded, latestSha)
	services.SaveSetting(db, "LAST_COMMIT_SHA", latestSha)
}
