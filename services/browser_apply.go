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

// OpenAndPreFill launches a visible Chrome window, navigates to jobURL,
// and runs a best-effort JS snippet that fills common application form
// fields from ui. The browser is intentionally left open: the user
// reviews the pre-filled form, fixes anything wrong, uploads their
// résumé, and submits manually.
//
// Caveats worth knowing:
//   - Best-effort field detection. Greenhouse / Lever / Ashby boards
//     work well; SPA-heavy ATS like Workday and login-walled flows
//     like LinkedIn Easy Apply do not.
//   - The launched Chrome is a fresh instance — it does NOT share
//     cookies / sessions with your everyday browser.
//   - We don't cancel the chromedp allocator on purpose; the browser
//     stays open until the user closes it. If the app exits, the
//     spawned Chrome process exits with it.
func OpenAndPreFill(jobURL string, ui *model.UserInfo) error {
	jobURL = strings.TrimSpace(jobURL)
	if jobURL == "" {
		return fmt.Errorf("no job URL on this entry")
	}
	if !strings.HasPrefix(jobURL, "http://") && !strings.HasPrefix(jobURL, "https://") {
		jobURL = "https://" + jobURL
	}

	data := map[string]string{
		"fullName":  ui.Name,
		"firstName": firstName(ui.Name),
		"lastName":  lastName(ui.Name),
		"email":     ui.Email,
		"phone":     ui.Phone,
		"linkedin":  ui.LinkedIn,
		"github":    ui.GitHub,
		"location":  ui.Location,
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Plain string concatenation — dataJSON is already safe JSON.
	script := preFillScriptPrefix + string(dataJSON) + preFillScriptSuffix

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
	// Intentionally not deferred — see function comment.
	allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, _ := chromedp.NewContext(allocCtx)

	go func() {
		if err := chromedp.Run(browserCtx,
			chromedp.Navigate(jobURL),
			chromedp.Sleep(2*time.Second), // let SPAs render and fonts load
			chromedp.Evaluate(script, nil),
		); err != nil {
			LogError(GlobalDB, fmt.Sprintf("OpenAndPreFill: %v", err))
		}
	}()
	return nil
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

// JS sandwiched around the user-data JSON below. Pure DOM querying — no
// libraries, no externals. Walks every text-like input, scores it
// against common naming patterns, and fills the best match. Fires
// input/change/blur events so React/Vue forms register the value.
const preFillScriptPrefix = `
(function(d) {
  function fire(el) {
    el.dispatchEvent(new Event('input',  { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    el.dispatchEvent(new Event('blur',   { bubbles: true }));
  }
  function nameOf(el) {
    return ((el.name || '') + ' ' + (el.id || '') + ' ' + (el.placeholder || '') + ' ' + (el.getAttribute('aria-label') || '')).toLowerCase();
  }
  function fillInput(el) {
    if (el.disabled || el.readOnly || el.value) return;
    var n = nameOf(el);
    var rules = [
      [/first.?name|firstname|fname|given/,                d.firstName],
      [/last.?name|lastname|lname|family|surname/,         d.lastName],
      [/full.?name|^name$|your.?name|candidate.?name/,     d.fullName],
      [/e[-_ ]?mail/,                                      d.email],
      [/phone|mobile|telephone|tel\b/,                     d.phone],
      [/linkedin/,                                         d.linkedin],
      [/github/,                                           d.github],
      [/location|city|address/,                            d.location]
    ];
    for (var i = 0; i < rules.length; i++) {
      if (rules[i][0].test(n) && rules[i][1]) {
        el.value = rules[i][1];
        fire(el);
        return;
      }
    }
  }
  var sel = 'input[type=text], input[type=email], input[type=tel], input[type=url], input:not([type])';
  document.querySelectorAll(sel).forEach(fillInput);
})(`

const preFillScriptSuffix = `);`
