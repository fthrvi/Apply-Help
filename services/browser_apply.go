package services

import (
	model "32-Adarsha/model"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// RunAutofillAgent opens a visible Chrome window at jobURL and runs
// the three-tier autofill agent against every visible form field. The
// browser is intentionally left open so the user reviews + clicks
// Submit themselves — we never auto-submit.
//
// Lifecycle:
//   1. Build the autofill profile (with embeddings if Ollama is up)
//   2. Open Chrome, navigate to jobURL
//   3. Loop: scan visible fields → classify each → fill → wait → re-scan
//      Stops when no new fields appear for one full tick or after the
//      maxTicks safety cap.
//   4. Returns; user takes over.
//
// progressFn receives short status lines for the UI ("Filled 8 fields",
// "Looking for new fields...", etc.).
func RunAutofillAgent(jobURL string, ui *model.UserInfo, jobDescription string, progressFn func(string)) error {
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

	// Spawn Chrome — visible, no automation flag, no fresh-profile
	// flag so the user could in principle authenticate persistently
	// across runs if Chrome reuses the same data dir. (We don't pin
	// a dir, so it's per-spawn for now.)
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
	allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, _ := chromedp.NewContext(allocCtx)

	go func() {
		// Navigate + give the page time to render the initial form. SPA
		// frameworks need a beat.
		if err := chromedp.Run(browserCtx,
			chromedp.Navigate(jobURL),
			chromedp.Sleep(2500*time.Millisecond),
		); err != nil {
			LogError(GlobalDB, fmt.Sprintf("autofill navigate: %v", err))
			report(fmt.Sprintf("Navigate failed: %v", err))
			return
		}

		// Tick loop: scan → classify → fill → wait.
		filledSoFar := map[string]bool{}
		stableTicks := 0
		const maxTicks = 20
		const tickInterval = 1200 * time.Millisecond

		for tick := 0; tick < maxTicks; tick++ {
			report(fmt.Sprintf("Tick %d: scanning fields…", tick+1))
			var rawJSON string
			if err := chromedp.Run(browserCtx, chromedp.Evaluate(scanFieldsJS, &rawJSON)); err != nil {
				report(fmt.Sprintf("Scan error: %v", err))
				return
			}
			var fields []FieldSignature
			if err := json.Unmarshal([]byte(rawJSON), &fields); err != nil {
				report(fmt.Sprintf("Parse error: %v (raw=%.120s)", err, rawJSON))
				return
			}

			newlyFilled := 0
			for _, f := range fields {
				key := f.ID + "|" + f.Name
				if filledSoFar[key] {
					continue
				}
				fill := ClassifyField(f, profile, jobDescription)
				if fill.Source == "skip" || fill.Value == "" {
					continue
				}
				if err := applyFill(browserCtx, f, fill); err != nil {
					LogError(GlobalDB, fmt.Sprintf("autofill apply %q: %v", f.Label, err))
					continue
				}
				filledSoFar[key] = true
				newlyFilled++
			}

			if newlyFilled > 0 {
				report(fmt.Sprintf("Filled %d field(s) this tick (total %d)", newlyFilled, len(filledSoFar)))
				stableTicks = 0
			} else {
				stableTicks++
			}
			// Two consecutive idle ticks → form is stable, we're done.
			if stableTicks >= 2 {
				break
			}
			time.Sleep(tickInterval)
		}

		report(fmt.Sprintf("✓ Done — filled %d field(s). Review and click Submit yourself.", len(filledSoFar)))
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
  } else {
    el.focus();
    el.value = val;
  }
  fire(el, 'input');
  fire(el, 'change');
  fire(el, 'blur');
  return true;
})()`

// OpenAndPreFill — legacy entrypoint kept for any callers that still
// use it. Now dispatches to the agent.
func OpenAndPreFill(jobURL string, ui *model.UserInfo) error {
	return RunAutofillAgent(jobURL, ui, "", nil)
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
