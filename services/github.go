package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// GitHubContext is the structured GitHub data we feed to the LLM during
// résumé/cover generation. Kept intentionally small so it doesn't blow
// the prompt budget.
type GitHubContext struct {
	Username    string       `json:"username"`
	Name        string       `json:"name,omitempty"`
	Bio         string       `json:"bio,omitempty"`
	Location    string       `json:"location,omitempty"`
	Company     string       `json:"company,omitempty"`
	BlogURL     string       `json:"blog_url,omitempty"`
	PublicRepos int          `json:"public_repos"`
	Followers   int          `json:"followers"`
	Repos       []GitHubRepo `json:"repos"`
}

type GitHubRepo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Language      string   `json:"language,omitempty"`
	Stars         int      `json:"stars"`
	Forks         int      `json:"forks"`
	URL           string   `json:"url"`
	Topics        []string `json:"topics,omitempty"`
	ReadmeSnippet string   `json:"readme_snippet,omitempty"`
}

// ── cache ─────────────────────────────────────────────────────────────
// One in-process cache keyed by username. The TTL is short enough that a
// user who just updated a README sees the change next launch, but long
// enough that opening multiple jobs in one session doesn't hammer the API.
const githubCacheTTL = 30 * time.Minute

var (
	ghCacheMu sync.RWMutex
	ghCache   = map[string]ghCacheEntry{}
)

type ghCacheEntry struct {
	ctx *GitHubContext
	at  time.Time
}

// ── public entry points ───────────────────────────────────────────────

// GitHubContextForCurrentUser returns a freshly-fetched or cached
// GitHubContext for the username configured in Settings. Returns nil
// (with no error) if no username is configured — callers should treat
// that as "GitHub integration disabled."
func GitHubContextForCurrentUser() (*GitHubContext, error) {
	user := strings.TrimSpace(GetSetting(GlobalDB, KeyGithubUsername))
	if user == "" {
		return nil, nil
	}
	token := strings.TrimSpace(GetSetting(GlobalDB, KeyGithubToken))

	ghCacheMu.RLock()
	if entry, ok := ghCache[user]; ok && time.Since(entry.at) < githubCacheTTL {
		ghCacheMu.RUnlock()
		return entry.ctx, nil
	}
	ghCacheMu.RUnlock()

	ctx, err := FetchGitHubContext(user, token)
	if err != nil {
		return nil, err
	}
	ghCacheMu.Lock()
	ghCache[user] = ghCacheEntry{ctx: ctx, at: time.Now()}
	ghCacheMu.Unlock()
	return ctx, nil
}

// FetchGitHubContext makes the actual HTTP calls. Exported so callers
// (e.g. a "Refresh GitHub data" button) can bypass the cache.
func FetchGitHubContext(username, token string) (*GitHubContext, error) {
	if username == "" {
		return nil, fmt.Errorf("github username is empty")
	}
	client := &http.Client{Timeout: 20 * time.Second}

	profile, err := ghGetUser(client, username, token)
	if err != nil {
		return nil, err
	}
	ctx := &GitHubContext{
		Username:    profile.Login,
		Name:        profile.Name,
		Bio:         profile.Bio,
		Location:    profile.Location,
		Company:     profile.Company,
		BlogURL:     profile.Blog,
		PublicRepos: profile.PublicRepos,
		Followers:   profile.Followers,
	}

	rawRepos, err := ghListRepos(client, username, token)
	if err != nil {
		return nil, err
	}

	// Keep up to the 8 most-recently-updated non-fork repos so the prompt
	// doesn't balloon. Fetch READMEs for the top 4 of those.
	const maxRepos = 8
	const maxReadmes = 4

	for _, r := range rawRepos {
		if r.Fork {
			continue
		}
		repo := GitHubRepo{
			Name:        r.Name,
			Description: r.Description,
			Language:    r.Language,
			Stars:       r.Stargazers,
			Forks:       r.Forks,
			URL:         r.HTMLURL,
			Topics:      r.Topics,
		}
		if len(ctx.Repos) < maxReadmes {
			if snippet, err := ghReadme(client, username, r.Name, token); err == nil {
				repo.ReadmeSnippet = snippet
			}
		}
		ctx.Repos = append(ctx.Repos, repo)
		if len(ctx.Repos) >= maxRepos {
			break
		}
	}
	return ctx, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────

type ghUser struct {
	Login       string `json:"login"`
	Name        string `json:"name"`
	Bio         string `json:"bio"`
	Location    string `json:"location"`
	Company     string `json:"company"`
	Blog        string `json:"blog"`
	PublicRepos int    `json:"public_repos"`
	Followers   int    `json:"followers"`
}

type ghRepoListItem struct {
	Name        string   `json:"name"`
	Fork        bool     `json:"fork"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	Stargazers  int      `json:"stargazers_count"`
	Forks       int      `json:"forks_count"`
	HTMLURL     string   `json:"html_url"`
	Topics      []string `json:"topics"`
}

func ghDo(client *http.Client, url, token string, accept string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "applyhelp")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Cap body so a misbehaving proxy can't stream forever.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func ghGetUser(client *http.Client, username, token string) (*ghUser, error) {
	body, err := ghDo(client, "https://api.github.com/users/"+username, token, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	var u ghUser
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func ghListRepos(client *http.Client, username, token string) ([]ghRepoListItem, error) {
	url := "https://api.github.com/users/" + username + "/repos?sort=updated&per_page=30&type=owner"
	body, err := ghDo(client, url, token, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	var repos []ghRepoListItem
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

func ghReadme(client *http.Client, username, repo, token string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/readme", username, repo)
	// "application/vnd.github.raw" returns the README as plain text/markdown
	// instead of the default JSON-with-base64-content envelope.
	body, err := ghDo(client, url, token, "application/vnd.github.raw")
	if err != nil {
		return "", err
	}
	// Strip the heaviest Markdown to save tokens. The LLM doesn't need
	// images, code-fence syntax, or HTML for context.
	s := string(body)
	s = stripMarkdown(s)
	if len(s) > 1200 {
		s = s[:1200] + "…"
	}
	return s, nil
}

// stripMarkdown removes the syntax that bulks up READMEs without carrying
// project meaning — image/badge lines, HTML, code-fence backticks. We keep
// the prose and headings.
func stripMarkdown(s string) string {
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			out.WriteByte('\n')
			continue
		}
		// Skip image / badge lines (e.g. ![Build](https://...))
		if strings.HasPrefix(t, "![") || strings.HasPrefix(t, "<img") {
			continue
		}
		// Skip raw HTML lines.
		if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
			continue
		}
		// Drop code-fence delimiters but keep the code lines themselves.
		if strings.HasPrefix(t, "```") {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}
