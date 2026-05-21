package services

import (
	model "32-Adarsha/model"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
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

// RepoToProject converts a GitHubRepo into a model.Project suitable for
// the candidate's profile. Language + Topics become Technologies (deduped,
// case-insensitive); Description and the first non-heading README line
// become Bullets. Empty repos fall back to "<name> — <url>" so a project
// without metadata still surfaces.
//
// The second bullet is dropped when it overlaps the first (common for
// repos where the GitHub "About" field restates the README opener).
func RepoToProject(r GitHubRepo) model.Project {
	var techs []string
	seen := map[string]bool{}
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t == "" {
			return
		}
		k := strings.ToLower(t)
		if seen[k] {
			return
		}
		seen[k] = true
		techs = append(techs, t)
	}
	add(r.Language)
	for _, t := range r.Topics {
		add(t)
	}

	var bullets []string
	if d := collapseWS(r.Description); d != "" {
		bullets = append(bullets, d)
	}
	if line := firstReadmeLine(r.ReadmeSnippet); line != "" && !bulletsOverlap(bullets, line) {
		bullets = append(bullets, line)
	}
	if len(bullets) == 0 {
		bullets = []string{r.Name + " — " + r.URL}
	}

	return model.Project{
		Name:         r.Name,
		URL:          r.URL,
		Technologies: techs,
		Bullets:      bullets,
	}
}

// firstReadmeLine returns the first non-empty content line from a README
// snippet. Skips markdown headings, blockquotes, tables, HTML, horizontal
// rules, and admonition fences. For list items ("- ..." / "* ...") the
// marker is stripped and the content is returned. Used to produce a
// project bullet when the repo's description field is empty or thin.
func firstReadmeLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = collapseWS(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			content := strings.TrimSpace(line[2:])
			if content != "" {
				return content
			}
			continue
		}
		if isReadmeJunkLine(line) {
			continue
		}
		return line
	}
	return ""
}

// isReadmeJunkLine matches lines whose prefix marks them as structural
// markdown rather than prose: headings, blockquotes, tables, HTML, rules,
// and GitHub-flavored alerts. Already-collapsed whitespace assumed.
func isReadmeJunkLine(line string) bool {
	if line == "" {
		return true
	}
	for _, p := range []string{"#", ">", "|", "<", "---", "===", ":::", "[!", "```"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// collapseWS replaces every run of unicode whitespace with a single ASCII
// space and trims edges. Markdown READMEs frequently contain double spaces,
// non-breaking spaces, and stray tabs that survive stripMarkdown.
func collapseWS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := true
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
			}
			lastSpace = true
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

// bulletsOverlap reports whether candidate is essentially the same content
// as an existing bullet — substring, superstring, or shared prefix after
// case-folding and whitespace normalization. Used to suppress the common
// case where a repo's GitHub description and its README's first line say
// the same thing.
func bulletsOverlap(existing []string, candidate string) bool {
	nc := normalizeForCompare(candidate)
	if nc == "" {
		return true
	}
	for _, b := range existing {
		nb := normalizeForCompare(b)
		if nb == "" {
			continue
		}
		if nb == nc || strings.Contains(nb, nc) || strings.Contains(nc, nb) {
			return true
		}
	}
	return false
}

// normalizeForCompare lowercases, collapses whitespace, and strips
// trailing punctuation/ellipses so "Foo." and "Foo…" compare equal.
func normalizeForCompare(s string) string {
	s = strings.ToLower(collapseWS(s))
	return strings.TrimRight(s, ".… ")
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

// ── LLM bullet polish ─────────────────────────────────────────────────
//
// The bullets produced by RepoToProject are taglines, not résumé bullets.
// PolishProjectBulletsWithLLM batches every repo in the supplied context
// into a single LLM call and asks the model to rewrite each one as 2–3
// résumé-grade XYZ statements. Results are cached in SQLite, keyed on a
// short hash of (description, README), so re-importing the same repos
// doesn't re-spend tokens.

// PolishProjectBulletsWithLLM returns a map of repo-name → polished
// bullets for every repo in ctx where the LLM (or cache) succeeded.
// Repos missing from the returned map should keep their heuristic
// bullets from RepoToProject. modelChoice mirrors PromptAI's values
// ("gemini" / "claude" / "openai"). An empty modelChoice is a no-op.
//
// The function does its best to return partial results: if the LLM call
// fails outright the cached hits are still returned alongside an error,
// so callers can blend them with the heuristic fallback.
func PolishProjectBulletsWithLLM(ctx *GitHubContext, ui *model.UserInfo, modelChoice string) (map[string][]string, error) {
	modelChoice = strings.ToLower(strings.TrimSpace(modelChoice))
	if ctx == nil || len(ctx.Repos) == 0 || modelChoice == "" {
		return nil, nil
	}
	out := make(map[string][]string, len(ctx.Repos))

	var toPolish []GitHubRepo
	for _, r := range ctx.Repos {
		h := repoContentHash(r)
		if cached := getBulletCache(GlobalDB, r.URL, h); len(cached) > 0 {
			out[r.Name] = cached
			continue
		}
		toPolish = append(toPolish, r)
	}
	if len(toPolish) == 0 {
		return out, nil
	}

	prompt := buildBulletPolishPrompt(toPolish, ui)
	raw, err := PromptAI(prompt, modelChoice)
	if err != nil {
		return out, fmt.Errorf("LLM call: %w", err)
	}

	parsed, perr := parseBulletPolishResponse(raw)
	if perr != nil {
		preview := raw
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return out, fmt.Errorf("parse bullets: %w (raw preview: %s)", perr, preview)
	}

	for _, r := range toPolish {
		bullets, ok := parsed[r.Name]
		if !ok || len(bullets) == 0 {
			continue
		}
		out[r.Name] = bullets
		_ = saveBulletCache(GlobalDB, r.URL, repoContentHash(r), bullets)
	}
	return out, nil
}

// repoContentHash hashes the content that drives bullet generation so the
// cache invalidates when a repo's description or README changes. Only the
// first 8 bytes are kept — collisions don't corrupt anything (cache miss
// just causes a re-call), and a short key keeps the DB row tiny.
func repoContentHash(r GitHubRepo) string {
	h := sha256.Sum256([]byte(r.Description + "\x00" + r.ReadmeSnippet))
	return fmt.Sprintf("%x", h[:8])
}

func buildBulletPolishPrompt(repos []GitHubRepo, ui *model.UserInfo) string {
	type repoLite struct {
		Name        string   `json:"name"`
		Description string   `json:"description,omitempty"`
		Language    string   `json:"language,omitempty"`
		Topics      []string `json:"topics,omitempty"`
		URL         string   `json:"url"`
		Readme      string   `json:"readme,omitempty"`
	}
	list := make([]repoLite, 0, len(repos))
	for _, r := range repos {
		list = append(list, repoLite{
			Name:        r.Name,
			Description: r.Description,
			Language:    r.Language,
			Topics:      r.Topics,
			URL:         r.URL,
			Readme:      r.ReadmeSnippet,
		})
	}
	reposJSON, _ := json.Marshal(list)

	userContext := "{}"
	if ui != nil {
		type uiLite struct {
			Skills   model.Skills `json:"skills"`
			Location string       `json:"location,omitempty"`
		}
		ulb, _ := json.Marshal(uiLite{Skills: ui.Skills, Location: ui.Location})
		userContext = string(ulb)
	}

	return fmt.Sprintf(`You convert GitHub repos into résumé bullets.

For each repo below, write 2-3 résumé-style bullets that:
- start with a strong past-tense action verb (Built, Designed, Implemented, Engineered, Shipped, Optimized, Reduced, Architected)
- name the concrete technologies, libraries, protocols, model formats, or APIs used
- include any measurable outcome the README mentions (latency, throughput, accuracy, dataset size, user count); never fabricate numbers — if no metric exists, describe technical depth instead
- do NOT repeat the project name in the bullet text
- do NOT use marketing prose ("revolutionary", "cutting-edge", "next-gen", "seamless") or generic taglines
- stay under ~30 words each

Candidate's stack (emphasize relevant tech if it matches a repo): %s

Return a single JSON object keyed by exact repo name. Each value is a JSON array of 2-3 bullet strings. No prose, no markdown, no code fences. Example shape:
{"repo-a": ["Built …", "Optimized …"], "repo-b": ["Implemented …"]}

Repos:
%s`, userContext, string(reposJSON))
}

var bulletPolishJSONRe = regexp.MustCompile(`(?s)\{.*\}`)

func parseBulletPolishResponse(raw string) (map[string][]string, error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") {
		if m := bulletPolishJSONRe.FindString(s); m != "" {
			s = m
		}
	}
	var m map[string][]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	for k, v := range m {
		cleaned := make([]string, 0, len(v))
		for _, b := range v {
			b = strings.TrimSpace(b)
			b = strings.TrimPrefix(b, "- ")
			b = strings.TrimPrefix(b, "* ")
			b = strings.TrimSpace(b)
			if b != "" {
				cleaned = append(cleaned, b)
			}
		}
		m[k] = cleaned
	}
	return m, nil
}

// ── cache (SQLite-backed) ─────────────────────────────────────────────

func getBulletCache(db *sql.DB, url, contentHash string) []string {
	if db == nil {
		return nil
	}
	var storedHash, storedBullets string
	err := db.QueryRow(
		"SELECT content_hash, bullets FROM GitHubBulletCache WHERE url = ?",
		url,
	).Scan(&storedHash, &storedBullets)
	if err != nil || storedHash != contentHash || storedBullets == "" {
		return nil
	}
	var bullets []string
	if json.Unmarshal([]byte(storedBullets), &bullets) != nil {
		return nil
	}
	return bullets
}

func saveBulletCache(db *sql.DB, url, contentHash string, bullets []string) error {
	if db == nil {
		return nil
	}
	data, err := json.Marshal(bullets)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT OR REPLACE INTO GitHubBulletCache (url, content_hash, bullets, updated_at) VALUES (?, ?, ?, ?)`,
		url, contentHash, string(data), time.Now(),
	)
	return err
}
