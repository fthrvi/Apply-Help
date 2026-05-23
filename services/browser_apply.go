package services

import (
	model "32-Adarsha/model"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	cdp "github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// RunAutofillAgent opens a visible Chrome window at jobURL and runs
// the agentic perceive-reason-act loop powered by the local LLM.
//
// resumePath and coverPath are absolute paths to the candidate's
// already-generated PDFs for this job (typically job.Resume and
// job.Coverletter). When non-empty AND the file exists, the agent
// auto-uploads them before the LLM loop begins, matching file inputs
// by label ("Resume/CV" → resume.pdf, "Cover Letter" → cover.pdf).
// Empty paths skip the upload phase — user can attach manually.
//
// The browser is intentionally left open so the user reviews + clicks
// Submit themselves — we never auto-submit.
func RunAutofillAgent(jobURL string, ui *model.UserInfo, jobDescription string, role, company string, resumePath, coverPath string, progressFn func(string)) error {
	jobURL = strings.TrimSpace(jobURL)
	if jobURL == "" {
		return fmt.Errorf("no job URL")
	}
	if !strings.HasPrefix(jobURL, "http://") && !strings.HasPrefix(jobURL, "https://") {
		jobURL = "https://" + jobURL
	}
	report := func(s string) {
		if progressFn != nil {
			progressFn(s)
		}
	}

	report("Preparing profile + embeddings…")
	profile := BuildAutofillProfile(ui)
	if len(profile.Keys) == 0 {
		return fmt.Errorf("profile is empty — fill out Settings → User Profile first")
	}
	if len(profile.Embeds) > 0 {
		report(fmt.Sprintf("Profile ready (%d fields, %d embeddings)", len(profile.Keys), countEmbeds(profile.Embeds)))
	} else {
		report(fmt.Sprintf("Profile ready (%d fields, regex-only — set Local LLM endpoint in Settings for smarter matching)", len(profile.Keys)))
	}

	// Two browser modes:
	//   1. Connect to the user's real Chrome (KeyConnectToRealBrowser=true)
	//      → inherits cookies / fingerprint / extensions, no automation
	//      banner, no navigator.webdriver flag. Most invisible to ATS
	//      bot detection.
	//   2. Spawn a fresh visible Chrome (default fallback) — clean
	//      instance, "controlled by automated test software" banner.
	var allocCtx context.Context
	if strings.EqualFold(GetSetting(GlobalDB, KeyConnectToRealBrowser), "true") {
		endpoint := strings.TrimSpace(GetSetting(GlobalDB, KeyRemoteDebugURL))
		if endpoint == "" {
			endpoint = "http://localhost:9222"
		}
		wsURL, err := discoverChromeDebugURL(endpoint)
		if err != nil {
			report(fmt.Sprintf("Couldn't reach Chrome at %s: %v.\nLaunch Chrome with: open -na 'Google Chrome' --args --remote-debugging-port=9222 --user-data-dir=$HOME/Library/Application\\ Support/Google/Chrome", endpoint, err))
			return fmt.Errorf("real-browser mode: %w", err)
		}
		allocCtx, _ = chromedp.NewRemoteAllocator(context.Background(), wsURL)
		report("Connected to your existing Chrome — new tab opening with your real session.")
	} else {
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", false),
			chromedp.Flag("disable-blink-features", "AutomationControlled"),
		)
		allocCtx, _ = chromedp.NewExecAllocator(context.Background(), opts...)
	}
	browserCtx, _ := chromedp.NewContext(allocCtx)

	// Auto-accept native JavaScript dialogs (alert / confirm / prompt).
	// These block the page until dismissed and would freeze the agent
	// otherwise. Most are safe to accept — they're "Are you sure you
	// want to leave?" style nags. The user still sees them flash.
	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		if _, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			go func() {
				_ = chromedp.Run(browserCtx, page.HandleJavaScriptDialog(true))
			}()
		}
	})

	go func() {
		if err := chromedp.Run(browserCtx,
			chromedp.Navigate(jobURL),
			chromedp.Sleep(2500*time.Millisecond),
		); err != nil {
			LogError(GlobalDB, fmt.Sprintf("autofill navigate: %v", err))
			report(fmt.Sprintf("Navigate failed: %v", err))
			return
		}

		// File upload phase — runs once before the LLM loop. We do this
		// deterministically (label-match + chromedp.SetUploadFiles)
		// because (a) browsers won't let JS set filenames at all and
		// (b) the LLM doesn't need to spend turns deciding where the
		// resume goes when the label tells us unambiguously.
		if resumePath != "" || coverPath != "" {
			runAutoUploadPhase(browserCtx, resumePath, coverPath, report)
		}

		// Hand off to the LLM-driven loop. When the local LLM endpoint
		// isn't configured, runAgenticLoop reports the missing config
		// and returns immediately — the user can still use the form
		// manually in the open browser.
		runAgenticLoop(browserCtx, profile, jobDescription, role, company, report)
	}()
	return nil
}

func countEmbeds(es [][]float32) int {
	n := 0
	for _, e := range es {
		if len(e) > 0 {
			n++
		}
	}
	return n
}

// applyFill sets a value on a form element + fires the right DOM
// events so React/Vue/Svelte forms register the change. For <select>
// it does a fuzzy match against option text (LLM-returned values
// don't always match exactly).
func applyFill(ctx context.Context, f FieldSignature, fill FieldFill) error {
	// Pick a stable selector. Prefer ID; fall back to name attribute.
	selector := ""
	if f.ID != "" {
		selector = "[id=" + jsString(f.ID) + "]"
	} else if f.Name != "" {
		selector = "[name=" + jsString(f.Name) + "]"
	} else {
		return fmt.Errorf("no selector for field %q", f.Label)
	}
	script := fmt.Sprintf(applyFillJS, selector, jsString(fill.Value), jsString(f.Type), jsString(f.Tag))
	return chromedp.Run(ctx, chromedp.Evaluate(script, nil))
}

// jsString JSON-encodes a string for safe inline injection into the
// applyFill template — handles quotes, backslashes, unicode escapes.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// scanFieldsJS walks the DOM and returns a JSON array of every
// visible, enabled, non-button form field. The resolved label
// follows the standard: <label for=id>, then wrapping <label>, then
// aria-label, then aria-labelledby's target.
const scanFieldsJS = `(function() {
  function visible(el) {
    if (!el) return false;
    var r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return false;
    var s = window.getComputedStyle(el);
    return s.visibility !== 'hidden' && s.display !== 'none';
  }
  function labelText(el) {
    if (el.id) {
      var lbl = document.querySelector('label[for="' + el.id.replace(/"/g, '\\"') + '"]');
      if (lbl && lbl.textContent.trim()) return lbl.textContent.trim();
    }
    var wrap = el.closest('label');
    if (wrap) {
      var clone = wrap.cloneNode(true);
      // Strip the input from the clone so we don't double-count its placeholder.
      clone.querySelectorAll('input, select, textarea, button').forEach(function(n){ n.remove(); });
      var t = clone.textContent.trim();
      if (t) return t;
    }
    var aria = el.getAttribute('aria-label');
    if (aria) return aria.trim();
    var refId = el.getAttribute('aria-labelledby');
    if (refId) {
      var ref = document.getElementById(refId);
      if (ref) return ref.textContent.trim();
    }
    return '';
  }
  var sel = 'input:not([type=hidden]):not([type=submit]):not([type=button]):not([type=reset]):not([type=image]), select, textarea';
  var out = [];
  document.querySelectorAll(sel).forEach(function(el) {
    if (!visible(el)) return;
    if (el.disabled || el.readOnly) return;
    var options = [];
    if (el.tagName === 'SELECT') {
      el.querySelectorAll('option').forEach(function(o) {
        var t = (o.textContent || '').trim();
        if (t) options.push(t);
      });
    }
    out.push({
      id: el.id || '',
      name: el.name || '',
      type: (el.type || el.tagName).toLowerCase(),
      tag: el.tagName.toLowerCase(),
      label: labelText(el),
      placeholder: el.placeholder || '',
      required: !!el.required,
      options: options,
      current_value: el.value || ''
    });
  });
  return JSON.stringify(out);
})()`

// applyFillJS sets a value on a single element. %s placeholders (in
// order): selector, value, type, tag. Fires input/change/blur so
// React's controlled inputs notice. For <select> falls back to fuzzy
// matching against option text when the value isn't an exact option.
const applyFillJS = `(function() {
  var sel = %s;
  var val = %s;
  var type = %s;
  var tag = %s;
  var el = document.querySelector(sel);
  if (!el) return false;
  if (el.disabled || el.readOnly) return false;
  function fire(e, name) {
    e.dispatchEvent(new Event(name, { bubbles: true }));
  }
  if (tag === 'select') {
    var found = false;
    var lower = String(val).toLowerCase();
    for (var i = 0; i < el.options.length; i++) {
      var ot = el.options[i].text.toLowerCase();
      if (ot === lower || ot.indexOf(lower) !== -1 || lower.indexOf(ot) !== -1) {
        el.selectedIndex = i;
        found = true;
        break;
      }
    }
    if (!found) return false;
  } else if (type === 'checkbox' || type === 'radio') {
    var truthy = (val === true || val === 1 || /^(true|yes|y|1|on)$/i.test(String(val)));
    el.checked = truthy;
  } else if (type === 'file') {
    return false;
  } else {
    // Only INPUT and TEXTAREA accept a fill. Calling the native value
    // setter on anything else (<a>, <div>, custom elements) throws
    // TypeError: Illegal invocation. The agent shouldn't ask to fill
    // non-form elements but defensive code matters.
    var t = (el.tagName || '').toUpperCase();
    if (t !== 'INPUT' && t !== 'TEXTAREA') {
      return false;
    }
    try {
      el.focus();
    } catch (e) { /* ignore — some custom inputs reject focus */ }
    // React (and other controlled-input frameworks) monkey-patch the
    // value setter on HTMLInputElement.prototype. Setting el.value
    // directly is silently swallowed by React's reconciler — the DOM
    // input visually updates but React's state stays empty, so on
    // re-render the value flips back. Workaround: get the *native*
    // setter via Object.getOwnPropertyDescriptor and call it directly,
    // then dispatch input so React picks up the change. Wrapped in
    // try/catch because custom elements with their own descriptors
    // sometimes still throw Illegal invocation.
    try {
      var proto = t === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
      var desc = Object.getOwnPropertyDescriptor(proto, 'value');
      if (desc && desc.set) {
        desc.set.call(el, val);
      } else {
        el.value = val;
      }
    } catch (e) {
      try { el.value = val; } catch (e2) { return false; }
    }
  }
  fire(el, 'input');
  fire(el, 'change');
  fire(el, 'blur');
  return true;
})()`

// uploadCandidate is one file input we want to drive. The "kind" is
// derived from nearby label text — resume / cover / other.
type uploadCandidate struct {
	Index   int    `json:"idx"`
	Kind    string `json:"kind"`  // resume | cover | other
	Label   string `json:"label"` // for logging
	Section string `json:"section"`
}

// runAutoUploadPhase finds every <input type="file"> on the page,
// classifies it by nearby label/section heading, and uses the Chrome
// DevTools Protocol to set the file directly. Works on hidden inputs
// (which Greenhouse / Lever / Ashby all use behind their custom
// upload UI). resumePath and coverPath may be empty — skip those.
func runAutoUploadPhase(ctx context.Context, resumePath, coverPath string, report func(string)) {
	// Verify files actually exist; report once if they don't.
	resumeOK := resumePath != "" && fileExists(resumePath)
	coverOK := coverPath != "" && fileExists(coverPath)
	if !resumeOK && !coverOK {
		report("No generated PDFs to upload (resume/cover not produced yet).")
		return
	}

	var rawJSON string
	if err := chromedp.Run(ctx, chromedp.Evaluate(findFileInputsJS, &rawJSON)); err != nil {
		report(fmt.Sprintf("Couldn't scan file inputs: %v", err))
		return
	}
	var cands []uploadCandidate
	if err := json.Unmarshal([]byte(rawJSON), &cands); err != nil {
		return
	}

	uploaded := 0
	for _, c := range cands {
		var path string
		switch c.Kind {
		case "resume":
			if !resumeOK {
				continue
			}
			path = resumePath
		case "cover":
			if !coverOK {
				continue
			}
			path = coverPath
		default:
			// "other" (portfolio, transcripts, additional docs) — the
			// agent doesn't auto-fill these; user handles manually.
			continue
		}

		sel := fmt.Sprintf(`input[type="file"][data-aplhelp-upload="%s-%d"]`, c.Kind, c.Index)
		var nodes []*cdp.Node
		if err := chromedp.Run(ctx, chromedp.Nodes(sel, &nodes, chromedp.ByQuery)); err != nil || len(nodes) == 0 {
			continue
		}
		if err := chromedp.Run(ctx, dom.SetFileInputFiles([]string{path}).WithNodeID(nodes[0].NodeID)); err != nil {
			report(fmt.Sprintf("Upload to %s input failed: %v", c.Kind, err))
			continue
		}
		uploaded++
		label := c.Label
		if label == "" {
			label = c.Section
		}
		report(fmt.Sprintf("Uploaded %s.pdf → %s", c.Kind, truncate(label, 50)))
	}

	if uploaded == 0 {
		report("No matching resume/cover file inputs on this page (might appear on a later step).")
	}
}

func fileExists(p string) bool {
	if _, err := os.Stat(p); err == nil {
		return true
	}
	return false
}

// findFileInputsJS labels every <input type="file"> with a
// data-aplhelp-upload attribute encoding its kind + index, so Go can
// re-select them via CSS. Classification is based on the input's
// label (label-for, parent label, aria-label) and the nearest section
// heading text — "resume" / "cv" / "curriculum" → resume; "cover
// letter" → cover; anything else → other (we don't auto-upload).
const findFileInputsJS = `(function() {
  function labelOf(el) {
    if (el.id) {
      var lbl = document.querySelector('label[for="' + el.id.replace(/"/g, '\\"') + '"]');
      if (lbl) {
        var t = (lbl.innerText || lbl.textContent || '').trim();
        if (t) return t;
      }
    }
    var wrap = el.closest('label');
    if (wrap) {
      var c = wrap.cloneNode(true);
      c.querySelectorAll('input, select, textarea, button').forEach(function(n) { n.remove(); });
      var t = (c.innerText || c.textContent || '').trim();
      if (t) return t;
    }
    var aria = el.getAttribute('aria-label') || el.getAttribute('placeholder');
    return aria ? aria.trim() : '';
  }
  function sectionHeadingOf(el) {
    // Walk up looking for the nearest preceding heading or labeled section.
    var cur = el;
    for (var i = 0; i < 12 && cur && cur !== document.body; i++) {
      cur = cur.parentElement;
      if (!cur) break;
      // Look for a heading element directly inside this container, before our input.
      var headings = cur.querySelectorAll('h1, h2, h3, h4, h5, h6, [class*="label"], [class*="heading"], [class*="title"]');
      for (var j = 0; j < headings.length; j++) {
        var h = headings[j];
        // Only count headings that appear before our element in document order.
        if (el.compareDocumentPosition(h) & Node.DOCUMENT_POSITION_PRECEDING) {
          var t = (h.innerText || h.textContent || '').trim();
          if (t && t.length < 80) return t;
        }
      }
    }
    return '';
  }

  var out = [];
  document.querySelectorAll('input[type="file"]').forEach(function(el, idx) {
    var label = labelOf(el);
    var section = sectionHeadingOf(el);
    var hay = (label + ' ' + section).toLowerCase();
    var kind = 'other';
    if (/resume|cv\b|curriculum/.test(hay)) kind = 'resume';
    else if (/cover.?letter/.test(hay)) kind = 'cover';
    el.setAttribute('data-aplhelp-upload', kind + '-' + idx);
    out.push({idx: idx, kind: kind, label: label, section: section});
  });
  return JSON.stringify(out);
})()`

// OpenAndPreFill — legacy entrypoint kept for any callers that still
// use it. Now dispatches to the agent with empty job context.
func OpenAndPreFill(jobURL string, ui *model.UserInfo) error {
	return RunAutofillAgent(jobURL, ui, "", "", "", "", "", nil)
}

// discoverChromeDebugURL queries Chrome's remote-debug endpoint and
// returns the browser-level WebSocket URL chromedp needs. Chrome must
// have been launched with --remote-debugging-port=N; the standard
// endpoint is http://localhost:9222. Errors clearly if Chrome isn't
// running with the flag set.
func discoverChromeDebugURL(httpEndpoint string) (string, error) {
	httpEndpoint = strings.TrimRight(httpEndpoint, "/")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(httpEndpoint + "/json/version")
	if err != nil {
		return "", fmt.Errorf("connect %s: %w", httpEndpoint, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d from %s/json/version", resp.StatusCode, httpEndpoint)
	}
	var data struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse /json/version: %w", err)
	}
	if data.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("/json/version returned no webSocketDebuggerUrl — is Chrome running with --remote-debugging-port?")
	}
	return data.WebSocketDebuggerURL, nil
}

func firstName(full string) string {
	parts := strings.Fields(full)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func lastName(full string) string {
	parts := strings.Fields(full)
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-1]
}
