# Apply-Help

A desktop GUI for tracking job applications and generating tailored
résumés and cover letters with an LLM. Written in Go using the
[Fyne](https://fyne.io) toolkit.

## What it does

- Tracks job postings in a local SQLite database.
- Polls [SimplifyJobs/New-Grad-Positions](https://github.com/SimplifyJobs/New-Grad-Positions)
  on launch and ingests any new postings since last run.
- For a given job: fetches the posting URL, asks an LLM to extract
  company and role, generates a tailored résumé + cover-letter JSON,
  fills HTML templates, and renders to PDF.
- **Import your resume** (PDF, DOCX, or TXT) from Settings → User
  Profile → Import Resume. The selected LLM extracts your contact,
  experience, projects, education, and skills into the profile fields.
  The preview is editable JSON, so you can fix any LLM mistakes
  before applying.
- Supports Google Gemini, Anthropic Claude, and OpenAI / NVIDIA-NIM
  (OpenAI-compatible) as LLM backends — pick one per job.

## Build

```sh
go build .
```

Requires:

- **Go 1.26+** (see `go.mod`).
- **Google Chrome or Chromium** anywhere `chromedp` can find it. On macOS
  that's `/Applications/Google Chrome.app/...`; on Linux it's `google-chrome`
  / `chromium` on PATH; on Windows it's the standard install location.
  Used for both PDF generation and the preview screenshot.

## Run

```sh
./32-Adarsha
```

The dashboard opens fullscreen. Use the gear icon to configure API
keys, prompts, output schema, HTML templates, and your résumé data.

## Where state lives

By default, the app keeps its database, generated documents, and any
`.env` overrides under the user's config directory:

- macOS: `~/Library/Application Support/applyhelp/`
- Linux: `~/.config/applyhelp/`
- Windows: `%AppData%\applyhelp\`

Set `APPLYHELP_DIR=/some/path` before launch to override. The
SQLite file `jobs.db` is created with `0600` perms because the
Settings table holds your API keys in plaintext.

A `.env` file is loaded from the current working directory first
(handy in development), then from `${UserConfigDir}/applyhelp/.env`.

## Project layout

```
main.go              Entry: theme, DB init, background scraper, launch UI
model/               Domain types: Job, UserInfo + sub-records
services/
  DataBase.go        SQLite Job + Settings + ErrorLogs tables
  llm.go             Gemini, Claude, OpenAI/NVIDIA behind PromptAI()
  AutoApply.go       Fetch → LLM extract → LLM generate → fill → PDF
  paths.go           AppDir() — where the app stores state
  settings_keys.go   Setting key constants + default model identifiers
  defaults/          Embedded extraction + combined prompts + schema
  placeholder/       Embedded resume.html + cover.html templates
joppull/             Background scraper of New-Grad-Positions
theme/               Custom Fyne theme + bundled TTF fonts
ui/
  window.go          Main window + table resizer
  window_addjob.go   "Add Job" popup
  window_settings.go Settings view (API keys, prompts, schemas, profile)
  window_editjob.go  Edit-job view + PDF preview + Q&A tab
  Table.go           JobTable widget
```

## License

The upstream repository at `github.com/fthrvi/Apply-Help` does not
include a LICENSE file. Under default copyright law that means the
upstream code is "all rights reserved." Add a LICENSE here (e.g.
MIT) only after confirming with the original author, or limit any
explicit license to your own contributions.
