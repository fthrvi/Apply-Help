package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// PageElement is one interactive thing on the page the LLM can reason
// about. Tag/Type/Text drive its decision; Selector is what we feed
// chromedp to act on it. The selector is a data-attribute we inject
// during capture so it stays stable across re-renders within the
// same DOM (it gets blown away if the page navigates, which is fine
// — we re-capture at the start of every step).
type PageElement struct {
	Index    int      `json:"index"`            // 1-based; the LLM references this
	Selector string   `json:"-"`                // [data-aplhelp-id="N"]
	Tag      string   `json:"tag"`              // button|a|input|select|textarea
	Type     string   `json:"type,omitempty"`   // input subtype (text|email|password|file|...)
	Text     string   `json:"text,omitempty"`   // visible label / button text / placeholder
	Value    string   `json:"value,omitempty"`  // current value
	Options  []string `json:"options,omitempty"`
	// Status drives the LLM's prioritization. One of:
	//   empty                  - input has no value, available to fill
	//   filled                 - input already has a non-empty value
	//   upload_not_supported   - input type=file, cannot be filled by JS
	Status string `json:"status,omitempty"`
}

// AgentAction is the LLM's decision for the current step.
type AgentAction struct {
	Action    string `json:"action"`               // click|fill|select|batch_fill|wait|done
	ElementID int    `json:"element_id,omitempty"` // index into the captured PageElements
	Value     string `json:"value,omitempty"`      // for fill/select/upload
	Status    string `json:"status,omitempty"`     // for done: ready_for_review|login_required|cant_proceed|user_action_needed
	Reason    string `json:"reason,omitempty"`     // free-text explanation, surfaced in the UI log
}

// runAgenticLoop is the perceive-reason-act loop. Replaces the
// keyword-matched apply-click + tick-based scan. The LLM gets the
// page state on every step and decides what to do, including
// switching to batch-fill mode when it sees a real form.
func runAgenticLoop(browserCtx context.Context, profile *AutofillProfile, jobDescription string, role, company string, report func(string)) {
	if strings.TrimSpace(GetSetting(GlobalDB, KeyLocalLLMEndpoint)) == "" {
		report("Local LLM endpoint not configured — agent cannot navigate. Set it in Settings → API Keys → Local LLM.")
		return
	}

	systemPrompt := buildAgentSystemPrompt(profile, jobDescription, role, company)

	history := []string{}
	filledFieldKeys := map[string]bool{}
	const maxSteps = 25
	// Safety net: two consecutive batch_fill calls that yield zero new
	// fills means the form is exhausted. The LLM should output
	// done(ready_for_review) at that point but if it doesn't, we
	// terminate cleanly instead of burning the step budget.
	batchFillZero := 0

	for step := 0; step < maxSteps; step++ {
		// Capture
		state, err := capturePageState(browserCtx)
		if err != nil {
			report(fmt.Sprintf("Capture error: %v", err))
			return
		}

		if len(state.Elements) == 0 {
			report("No interactive elements on the page yet. Waiting…")
			time.Sleep(1500 * time.Millisecond)
			continue
		}

		// Decide
		userPrompt := buildAgentUserPrompt(state, history)
		raw, err := LocalLLMChat(systemPrompt, userPrompt, true)
		if err != nil {
			report(fmt.Sprintf("LLM error: %v", err))
			return
		}
		action, err := parseAgentAction(raw)
		if err != nil {
			report(fmt.Sprintf("Bad action JSON: %v (raw=%.200s)", err, raw))
			return
		}

		report(fmt.Sprintf("Step %d · %s — %s", step+1, action.Action, action.Reason))

		// Act
		switch action.Action {
		case "done":
			switch action.Status {
			case "login_required":
				report("⚠ Sign-in needed. Log in in the open Chrome window, then click Auto-Fill again.")
			case "ready_for_review":
				finalizeReadyForReview(browserCtx, report)
			case "user_action_needed":
				report("⚠ Agent needs you to take a manual step. " + action.Reason)
			default:
				report("Agent stopped: " + action.Reason)
			}
			return
		case "wait":
			time.Sleep(1500 * time.Millisecond)
		case "click":
			if err := executeClick(browserCtx, state.Elements, action.ElementID); err != nil {
				report(fmt.Sprintf("Click failed: %v", err))
				history = append(history, fmt.Sprintf("click on %d FAILED: %v", action.ElementID, err))
			} else {
				history = append(history, fmt.Sprintf("clicked: %s", elementSummary(state.Elements, action.ElementID)))
			}
			time.Sleep(1800 * time.Millisecond) // navigation/modal beat
		case "fill":
			if err := executeFill(browserCtx, state.Elements, action.ElementID, action.Value); err != nil {
				report(fmt.Sprintf("Fill failed: %v", err))
				history = append(history, fmt.Sprintf("fill on %d FAILED: %v", action.ElementID, err))
			} else {
				history = append(history, fmt.Sprintf("filled: %s = %s", elementSummary(state.Elements, action.ElementID), truncate(action.Value, 40)))
			}
		case "select":
			if err := executeSelect(browserCtx, state.Elements, action.ElementID, action.Value); err != nil {
				report(fmt.Sprintf("Select failed: %v", err))
				history = append(history, fmt.Sprintf("select on %d FAILED: %v", action.ElementID, err))
			} else {
				history = append(history, fmt.Sprintf("selected: %s = %s", elementSummary(state.Elements, action.ElementID), action.Value))
			}
		case "batch_fill":
			n := runBatchFill(browserCtx, profile, jobDescription, filledFieldKeys)
			report(fmt.Sprintf("Batch-filled %d field(s)", n))
			history = append(history, fmt.Sprintf("batch_fill (%d filled)", n))
			if n == 0 {
				batchFillZero++
				if batchFillZero >= 2 {
					report("Form exhausted — two empty batch_fills in a row.")
					finalizeReadyForReview(browserCtx, report)
					return
				}
			} else {
				batchFillZero = 0
			}
			time.Sleep(800 * time.Millisecond)
		default:
			report(fmt.Sprintf("Unknown action: %s", action.Action))
			history = append(history, fmt.Sprintf("unknown action: %s", action.Action))
		}

		// Keep history bounded so the prompt doesn't grow unbounded.
		if len(history) > 10 {
			history = history[len(history)-10:]
		}
	}

	report("Agent ran for the max step budget — stopping to prevent loops. Review the open browser.")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func elementSummary(els []PageElement, idx int) string {
	for _, e := range els {
		if e.Index == idx {
			t := e.Text
			if t == "" {
				t = e.Type
			}
			return fmt.Sprintf("[%d] %s %q", e.Index, e.Tag, truncate(t, 50))
		}
	}
	return fmt.Sprintf("[%d] (gone)", idx)
}

// capturedState is what capturePageState returns; mirrors the JSON
// emitted by the page-capture script below.
type capturedState struct {
	URL      string        `json:"url"`
	Title    string        `json:"title"`
	Heading  string        `json:"heading,omitempty"`
	Elements []PageElement `json:"elements"`
}

func capturePageState(ctx context.Context) (*capturedState, error) {
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(captureStateJS, &raw)); err != nil {
		return nil, err
	}
	var s capturedState
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, fmt.Errorf("decode state: %w (raw=%.200s)", err, raw)
	}
	// Re-derive selectors from the index we stamped during capture.
	for i := range s.Elements {
		s.Elements[i].Selector = fmt.Sprintf(`[data-aplhelp-id="%d"]`, s.Elements[i].Index)
	}
	return &s, nil
}

func buildAgentSystemPrompt(profile *AutofillProfile, jobDescription, role, company string) string {
	type kv struct {
		K string `json:"k"`
		V string `json:"v"`
	}
	entries := make([]kv, 0, len(profile.Keys))
	for i, k := range profile.Keys {
		entries = append(entries, kv{K: k, V: profile.Values[i]})
	}
	profileJSON, _ := json.Marshal(entries)

	target := company
	if role != "" {
		target = role + " at " + company
	}
	if jobDescription != "" && len(jobDescription) > 1500 {
		jobDescription = jobDescription[:1500] + "…"
	}

	return `You are an autofill agent navigating a job-application website on behalf of the candidate.

Goal: navigate through the application flow and fill every field you can, so the candidate has a final review page ready. You DO NOT submit.

You will receive the page state on every step (URL, title, numbered interactive elements). Reply with EXACTLY ONE JSON action — no prose, no markdown:

{"action":"click","element_id":N,"reason":"..."}        — click element N
{"action":"fill","element_id":N,"value":"...","reason":"..."}  — fill input N with value
{"action":"select","element_id":N,"value":"...","reason":"..."} — choose option in select N (value can be partial match)
{"action":"batch_fill","reason":"..."}                  — invoke the heuristic batch filler on all visible form fields. USE THIS when you see a real form with many fillable text/email/phone inputs; it's much faster than filling each individually.
{"action":"wait","reason":"..."}                        — wait 1.5s for the page to settle
{"action":"done","status":"...","reason":"..."}         — stop. status values:
   - ready_for_review: form is filled and the next button is "Submit"; user takes over
   - login_required: a sign-in form is visible (email + password). User must log in manually.
   - user_action_needed: something the agent can't do (CAPTCHA, file picker that requires drag-drop, etc.)
   - cant_proceed: agent is stuck

Each element comes with a status:
- status=empty   — input is unfilled; eligible for fill
- status=filled  — input already has a value; DO NOT re-fill, treat as done
- status=upload_not_supported — file upload (input type=file); CANNOT be filled by JS, skip it, the user will upload manually
- (no status)    — buttons, links; for click decisions

Rules — read in order, the first match wins:

- HIGHEST PRIORITY: if the visible list contains 3 or more INPUT/TEXTAREA elements (any type=text/email/tel/url/number) waiting to be filled, your FIRST action MUST be batch_fill. Do not pick individual fill actions before running batch_fill. batch_fill uses deterministic regex + embedding matching against the profile — it WILL map the right value to the right field. Individual fill on standard text fields is brittle and frequently picks wrong values.

- After batch_fill: re-evaluate. The next step will show fewer (or zero) empty fields. Pick batch_fill again only if new fields appeared. Otherwise handle remaining specialty fields (custom dropdowns, free-text questions, acknowledgement checkboxes) one at a time.

- When you DO pick an individual fill, the value MUST come VERBATIM from the candidate's profile JSON. Do not paraphrase, abbreviate, capitalize differently, or substitute. If no profile key matches the field's purpose, use action=wait and let batch_fill handle it next turn.

- "Website" / "Portfolio" / "Personal URL" fields refer to the CANDIDATE's site. Use the profile's portfolio_website_url, github_profile_url, or linkedin_profile_url — in that order. NEVER use the hiring company's URL even if it appears in the job description. The Anduril / Google / Micron URL belongs to the employer, not the candidate.

- NEVER fill (or batch_fill) a field with status=filled. They're hidden from your view because they're done. (If you reference an element_id that isn't in this view, you're hallucinating — pick a visible one.)
- NEVER fill input type=file (status=upload_not_supported). The user uploads files manually.
- NEVER fill a non-input element (anchors, divs, buttons). Fills only work on INPUT, TEXTAREA, SELECT.
- NEVER click buttons whose text contains "Submit", "Submit Application", "Send Application", or "Confirm and submit". The candidate must click Submit themselves after reviewing. When this is the only thing left, your action is done(ready_for_review) — NOT click.
- NEVER click buttons labelled "Autofill with MyGreenhouse", "Sign in to use saved profile", "Autofill with LinkedIn", or any ATS session-restore option. These would overwrite the data the agent just filled with the candidate's prior application from a different account.
- Stop condition: when 0 fillable inputs are visible AND any Submit-style button is visible, your ONLY valid action is done(ready_for_review). Do not batch_fill again (it'll return 0). Do not click anything. Output: {"action":"done","status":"ready_for_review","reason":"Form filled and ready for user review"}.
- If batch_fill has already returned 0 once in this run AND no new fields appeared, your next action MUST be done(ready_for_review). The form is exhausted.
- If you see a sign-in form (password field present, text like "Sign in to apply") AND it's a login (not a registration), output done(login_required).
- Multi-step forms have "Next", "Continue", "Save and Continue" — click those after filling. Expect a fresh form on the next step.
- Modal asks "Apply Manually" vs "Autofill with Resume" → prefer "Apply Manually".
- Checkbox decisions — categorize by purpose first, then act:
   (a) LEGAL/CONSENT (must check): "I agree to the Privacy Statement", "I acknowledge the
       Terms of Service", "EEO consent", "I have read and agree to..."  →  check (fill value=yes).
       The user has implicitly consented by clicking Auto-Fill.
   (b) INTEREST/CATEGORY (selective): "What are you interested in?", "Areas of focus",
       "Job functions you'd consider", "Departments", "Industries"  →  check ONLY 1-3 boxes
       that DIRECTLY match the candidate's actual experience and the target role. NEVER bulk-check
       the whole list. Example: software engineer applying for a SWE role → check
       "Engineering – Software", maybe "Information Technology", nothing else.
   (c) NEWSLETTER/MARKETING (never check): "Send me job alerts", "Subscribe to product updates",
       "I want to hear about new opportunities", "Marketing communications", "Promotional emails"
        →  LEAVE UNCHECKED. The user does not want these.
   (d) DEMOGRAPHIC self-ID (optional, default skip): "I am a veteran", "I have a disability",
       race/ethnicity. These are voluntary EEO disclosures. If the form has an explicit
       "I don't wish to answer" option, select that. Otherwise leave unchecked.
- Radio button yes/no (work authorization, etc.): pick the option matching the profile. For required questions, prefer Yes when the candidate's location/profile suggests eligibility.
- Cookie / GDPR consent banners: click Accept.
- Validation errors ("You need to agree to the terms"): re-check that specific field.
- Anti-loop: if your last 2 actions were on the same element with no state change, switch strategies — try wait, then a different element, or output done.

Candidate profile (key → value):
` + string(profileJSON) + `

Target role: ` + target + `

Job description excerpt (for free-text answers like "Why are you interested?"):
` + jobDescription
}

func buildAgentUserPrompt(state *capturedState, history []string) string {
	var b strings.Builder
	b.WriteString("URL: ")
	b.WriteString(state.URL)
	b.WriteString("\nTitle: ")
	b.WriteString(state.Title)
	if state.Heading != "" {
		b.WriteString("\nHeading: ")
		b.WriteString(state.Heading)
	}
	// Filter elements before showing to the LLM. Hiding status=filled
	// and status=upload_not_supported entries from the prompt is
	// stronger than relying on the LLM to read an inline "status="
	// flag — the LLM literally can't pick them. Summary counts below
	// tell the LLM "yes, fields exist, they're already done".
	visibleElements := make([]PageElement, 0, len(state.Elements))
	filledCount := 0
	fileUploadCount := 0
	for _, e := range state.Elements {
		switch e.Status {
		case "filled":
			filledCount++
			continue
		case "upload_not_supported":
			fileUploadCount++
			continue
		}
		visibleElements = append(visibleElements, e)
	}

	if filledCount > 0 || fileUploadCount > 0 {
		b.WriteString(fmt.Sprintf("\n\nHidden from this view (already handled): %d filled field(s)", filledCount))
		if fileUploadCount > 0 {
			b.WriteString(fmt.Sprintf(", %d file upload(s) the user will handle manually", fileUploadCount))
		}
		b.WriteString(".")
	}

	b.WriteString("\n\nVisible interactive elements (only what's not yet done):\n")
	for _, e := range visibleElements {
		b.WriteString(fmt.Sprintf("[%d] %s", e.Index, strings.ToUpper(e.Tag)))
		if e.Type != "" && e.Type != e.Tag {
			b.WriteString(" type=")
			b.WriteString(e.Type)
		}
		if e.Text != "" {
			b.WriteString(" \"")
			b.WriteString(truncate(e.Text, 140))
			b.WriteString("\"")
		}
		if len(e.Options) > 0 {
			b.WriteString(" options=[")
			for i, opt := range e.Options {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(truncate(opt, 30))
				if i >= 8 {
					b.WriteString(", …")
					break
				}
			}
			b.WriteString("]")
		}
		b.WriteString("\n")
	}
	if len(history) > 0 {
		b.WriteString("\nRecent actions (oldest first):\n")
		for _, h := range history {
			b.WriteString("- ")
			b.WriteString(h)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nNext action?")
	return b.String()
}

func parseAgentAction(raw string) (*AgentAction, error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") {
		// Some local LLMs leak prose before the JSON despite format=json.
		// Extract the first {...} block.
		start := strings.Index(s, "{")
		end := strings.LastIndex(s, "}")
		if start >= 0 && end > start {
			s = s[start : end+1]
		}
	}
	var a AgentAction
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return nil, err
	}
	a.Action = strings.ToLower(strings.TrimSpace(a.Action))
	return &a, nil
}

func executeClick(ctx context.Context, els []PageElement, idx int) error {
	e := findElement(els, idx)
	if e == nil {
		return fmt.Errorf("element %d not in last state", idx)
	}
	// Human-like mode: get the element's center, simulate a curved
	// mouse path with jittered intermediate points + per-segment
	// delays, then dispatch the actual click via Input events.
	// Falls back to plain JS .click() when humanlike is off or when
	// we can't get viewport coords (e.g., element below the fold and
	// scrollIntoView hasn't run yet).
	if strings.EqualFold(GetSetting(GlobalDB, KeyHumanLikeInput), "true") {
		if humanClick(ctx, e.Selector) {
			return nil
		}
	}
	return chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(clickElementJS, jsString(e.Selector)), nil))
}

// humanClick scrolls the target into view, computes its center, and
// dispatches CDP Input.dispatchMouseEvent along a jittered path
// before clicking. Returns true on success; caller falls back to JS
// click on false.
func humanClick(ctx context.Context, selector string) bool {
	// 1) Make sure the element is in the viewport so its rect makes sense.
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`(function(s){var e=document.querySelector(s); if(e){try{e.scrollIntoView({block:'center'});}catch(_){}} return !!e;})(%s)`, jsString(selector)), nil)); err != nil {
		return false
	}
	time.Sleep(120 * time.Millisecond)

	// 2) Get viewport-relative coords of the element's center.
	var rect struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	}
	js := fmt.Sprintf(`(function(s){var e=document.querySelector(s); if(!e) return null; var r=e.getBoundingClientRect(); return {x:r.left,y:r.top,w:r.width,h:r.height};})(%s)`, jsString(selector))
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &rect)); err != nil {
		return false
	}
	if rect.W <= 0 || rect.H <= 0 {
		return false
	}
	targetX := rect.X + rect.W/2
	targetY := rect.Y + rect.H/2

	// 3) Pick a plausible starting point and walk a jittered path
	// toward the target. The path has ~8-14 intermediate samples;
	// per-segment delays jittered 10-25ms. Total movement time ~150-350ms.
	startX := targetX + float64(rand.Intn(400)-200)
	startY := targetY + float64(rand.Intn(400)-200)
	if startX < 0 {
		startX = 0
	}
	if startY < 0 {
		startY = 0
	}
	steps := 8 + rand.Intn(7)
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		// Quadratic ease-out — fast at start, slowing into the target.
		eased := 1 - (1-t)*(1-t)
		x := startX + (targetX-startX)*eased + float64(rand.Intn(7)-3)
		y := startY + (targetY-startY)*eased + float64(rand.Intn(7)-3)
		if err := chromedp.Run(ctx, input.DispatchMouseEvent("mouseMoved", x, y)); err != nil {
			return false
		}
		time.Sleep(time.Duration(10+rand.Intn(15)) * time.Millisecond)
	}

	// 4) Press + release.
	pressEv := input.DispatchMouseEvent("mousePressed", targetX, targetY).
		WithButton(input.Left).
		WithClickCount(1)
	if err := chromedp.Run(ctx, pressEv); err != nil {
		return false
	}
	time.Sleep(time.Duration(40+rand.Intn(40)) * time.Millisecond)
	releaseEv := input.DispatchMouseEvent("mouseReleased", targetX, targetY).
		WithButton(input.Left).
		WithClickCount(1)
	if err := chromedp.Run(ctx, releaseEv); err != nil {
		return false
	}
	return true
}

func executeFill(ctx context.Context, els []PageElement, idx int, value string) error {
	e := findElement(els, idx)
	if e == nil {
		return fmt.Errorf("element %d not in last state", idx)
	}
	script := fmt.Sprintf(applyFillJS, jsString(e.Selector), jsString(value), jsString(e.Type), jsString(e.Tag))
	return chromedp.Run(ctx, chromedp.Evaluate(script, nil))
}

func executeSelect(ctx context.Context, els []PageElement, idx int, value string) error {
	e := findElement(els, idx)
	if e == nil {
		return fmt.Errorf("element %d not in last state", idx)
	}
	tag := e.Tag
	if tag == "" {
		tag = "select"
	}
	script := fmt.Sprintf(applyFillJS, jsString(e.Selector), jsString(value), jsString(e.Type), jsString(tag))
	return chromedp.Run(ctx, chromedp.Evaluate(script, nil))
}

func findElement(els []PageElement, idx int) *PageElement {
	for i := range els {
		if els[i].Index == idx {
			return &els[i]
		}
	}
	return nil
}

// runBatchFill is the legacy three-tier classifier loop wrapped as a
// single agent action. It scans every visible form field and fills
// the ones it can match, using filledSoFar to avoid re-filling fields
// across multiple batch_fill calls (the agent may invoke batch_fill on
// every step of a multi-page wizard).
func runBatchFill(ctx context.Context, profile *AutofillProfile, jobDescription string, filledSoFar map[string]bool) int {
	var rawJSON string
	if err := chromedp.Run(ctx, chromedp.Evaluate(scanFieldsJS, &rawJSON)); err != nil {
		return 0
	}
	var fields []FieldSignature
	if err := json.Unmarshal([]byte(rawJSON), &fields); err != nil {
		return 0
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
		if err := applyFill(ctx, f, fill); err != nil {
			continue
		}
		filledSoFar[key] = true
		newlyFilled++
	}
	return newlyFilled
}

// captureStateJS extracts a compact, LLM-readable summary of the
// current page: URL, title, primary heading, and every visible
// interactive element (buttons, links acting as buttons, inputs,
// selects, textareas). Each element is stamped with a stable
// data-aplhelp-id attribute the agent can re-select on.
const captureStateJS = `(function() {
  // Clear stale IDs from previous capture so re-renders get fresh numbers.
  document.querySelectorAll('[data-aplhelp-id]').forEach(function(el) {
    el.removeAttribute('data-aplhelp-id');
  });

  function visible(el) {
    var r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return false;
    var s = window.getComputedStyle(el);
    return s.visibility !== 'hidden' && s.display !== 'none' && s.opacity !== '0';
  }
  function textOf(el) {
    var t = (el.innerText || el.textContent || '').trim();
    if (t) return t;
    if (el.value) return el.value;
    if (el.getAttribute('aria-label')) return el.getAttribute('aria-label').trim();
    if (el.getAttribute('placeholder')) return el.getAttribute('placeholder').trim();
    return '';
  }
  function labelFor(el) {
    // 1) <label for="id">
    if (el.id) {
      var lbl = document.querySelector('label[for="' + el.id.replace(/"/g, '\\"') + '"]');
      if (lbl) {
        var t = (lbl.innerText || lbl.textContent || '').trim();
        if (t) return t;
      }
    }
    // 2) wrapping <label>
    var wrap = el.closest('label');
    if (wrap) {
      var clone = wrap.cloneNode(true);
      clone.querySelectorAll('input, select, textarea, button').forEach(function(n) { n.remove(); });
      var t = (clone.innerText || clone.textContent || '').trim();
      if (t) return t;
    }
    // 3) aria-labelledby (space-separated list of ids) — Workday + many
    //    modern ATS use this; without it checkbox labels look empty.
    var lblIds = el.getAttribute('aria-labelledby');
    if (lblIds) {
      var combined = lblIds.split(/\s+/).map(function(id) {
        var ref = document.getElementById(id);
        if (!ref) return '';
        return (ref.innerText || ref.textContent || '').trim();
      }).filter(Boolean).join(' ');
      if (combined) return combined;
    }
    // 4) aria-label, placeholder
    var aria = el.getAttribute('aria-label') || el.getAttribute('placeholder');
    if (aria) return aria.trim();
    // 5) Nearby text fallback for checkboxes/radios specifically: walk
    //    up to the nearest containing div/li/section and grab the
    //    surrounding text (with our input's value stripped).
    if (el.type === 'checkbox' || el.type === 'radio') {
      var ctx = el.closest('div, li, fieldset, section, [class*="form"], [class*="field"]');
      if (ctx && ctx !== document.body) {
        var clone2 = ctx.cloneNode(true);
        clone2.querySelectorAll('input, select, textarea, button').forEach(function(n) { n.remove(); });
        var t2 = (clone2.innerText || clone2.textContent || '').trim().replace(/\s+/g, ' ');
        if (t2 && t2.length < 300) return t2;
      }
    }
    return '';
  }

  var out = [];
  var seq = 0;
  function add(el, tag, type, text, value, options, status) {
    if (!visible(el)) return;
    if (el.disabled) return;
    seq++;
    el.setAttribute('data-aplhelp-id', String(seq));
    out.push({
      index: seq,
      tag: tag.toLowerCase(),
      type: type ? type.toLowerCase() : '',
      text: text || '',
      value: value || '',
      options: options || undefined,
      status: status || ''
    });
  }
  // Compute the perception-layer status the LLM keys off of.
  function fieldStatus(el, type, value) {
    if (type === 'file') return 'upload_not_supported';
    if (type === 'checkbox' || type === 'radio') {
      return el.checked ? 'filled' : 'empty';
    }
    return (value && String(value).length > 0) ? 'filled' : 'empty';
  }

  // Buttons + button-role + clickable links. Skip header / nav menu
  // links — too noisy. (Keep links whose ancestor is a form / main.)
  var btnSel = 'button, [role="button"], input[type="submit"], input[type="button"]';
  document.querySelectorAll(btnSel).forEach(function(el) {
    add(el, 'button', '', textOf(el), '', null, '');
  });

  document.querySelectorAll('a').forEach(function(el) {
    var t = (el.innerText || '').trim();
    if (t.length === 0 || t.length > 80) return;
    var inNavOrFooter = el.closest('nav, header, footer');
    if (inNavOrFooter) {
      var lower = t.toLowerCase();
      if (lower.indexOf('apply') < 0 && lower.indexOf('continue') < 0) return;
    }
    add(el, 'a', '', t, el.href || '', null, '');
  });

  // Inputs (text-like + checkboxes/radios + files all included; status
  // tells the LLM what to do with each).
  var inputSel = 'input:not([type=hidden]):not([type=submit]):not([type=button]):not([type=image])';
  document.querySelectorAll(inputSel).forEach(function(el) {
    var type = (el.type || 'text').toLowerCase();
    var val = el.value || '';
    add(el, 'input', type, labelFor(el), val, null, fieldStatus(el, type, val));
  });

  // Selects
  document.querySelectorAll('select').forEach(function(el) {
    var opts = [];
    el.querySelectorAll('option').forEach(function(o) {
      var t = (o.textContent || '').trim();
      if (t) opts.push(t);
    });
    var val = el.value || '';
    add(el, 'select', '', labelFor(el), val, opts, fieldStatus(el, 'select', val));
  });

  // Textareas
  document.querySelectorAll('textarea').forEach(function(el) {
    var val = el.value || '';
    add(el, 'textarea', '', labelFor(el), val, null, fieldStatus(el, 'textarea', val));
  });

  return JSON.stringify({
    url: location.href,
    title: document.title || '',
    heading: (function() {
      var h = document.querySelector('h1');
      return h ? (h.innerText || h.textContent || '').trim() : '';
    })(),
    elements: out
  });
})()`

const clickElementJS = `(function(sel) {
  var el = document.querySelector(sel);
  if (!el) return false;
  try { el.scrollIntoView({block: 'center'}); } catch (e) {}
  el.click();
  return true;
})(%s)`

// finalizeReadyForReview is the terminal action when the form is
// filled. If the user has opted into auto-submit (KeyAutofillAutoSubmit
// = "true"), we look for a Submit button and click it. Default
// behavior is to leave Chrome open so the candidate reviews + clicks
// Submit themselves — the only path that's truly safe against
// ATS bot-detection.
func finalizeReadyForReview(ctx context.Context, report func(string)) {
	if strings.EqualFold(GetSetting(GlobalDB, KeyAutofillAutoSubmit), "true") {
		report("Auto-submit is ON — looking for the Submit button…")
		var clicked bool
		err := chromedp.Run(ctx, chromedp.Evaluate(clickSubmitJS, &clicked))
		if err != nil {
			report(fmt.Sprintf("Submit-click error: %v — please click Submit manually.", err))
			return
		}
		if clicked {
			report("✓ Submitted. The browser stays open so you can confirm the confirmation page.")
		} else {
			report("Couldn't find a Submit button — please click Submit manually.")
		}
		return
	}
	report("✓ Application filled and ready. Review and click Submit yourself. (Enable auto-submit in Settings → API Keys → Local LLM if you want the agent to click Submit too.)")
}

// clickSubmitJS finds and clicks the most-likely Submit button. Matches
// case-insensitively on text/value/aria-label. Skips disabled and
// invisible candidates; scrolls into view before clicking to satisfy
// any IntersectionObserver-based form validators.
const clickSubmitJS = `(function() {
  function visible(el) {
    if (el.disabled) return false;
    var r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return false;
    var s = window.getComputedStyle(el);
    return s.visibility !== 'hidden' && s.display !== 'none';
  }
  // Order matters — try the most specific labels first.
  var patterns = [
    'submit application', 'submit & apply', 'send application',
    'apply now', 'apply', 'submit'
  ];
  var candidates = document.querySelectorAll('button, input[type="submit"], [role="button"]');
  for (var p = 0; p < patterns.length; p++) {
    for (var i = 0; i < candidates.length; i++) {
      var el = candidates[i];
      if (!visible(el)) continue;
      var t = ((el.innerText || el.textContent || '') + ' ' + (el.value || '') + ' ' + (el.getAttribute('aria-label') || '')).trim().toLowerCase();
      if (!t) continue;
      if (t === patterns[p] || t.indexOf(patterns[p]) >= 0) {
        try { el.scrollIntoView({block: 'center'}); } catch (e) {}
        el.click();
        return true;
      }
    }
  }
  return false;
})()`
