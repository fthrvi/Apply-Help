package services

import (
	"strings"
	"testing"
)

func TestFillTemplateString_Substitutes(t *testing.T) {
	out := FillTemplateString("Hello {- NAME -}!", map[string]any{"NAME": "World"})
	if out != "Hello World!" {
		t.Errorf("got %q, want %q", out, "Hello World!")
	}
}

func TestFillTemplateString_EscapesHTML(t *testing.T) {
	// LLM-supplied values must not be inserted as raw HTML — Chrome renders
	// the resulting file when producing the PDF, so an unescaped <img> or
	// <script> would fire network requests or run code.
	in := `<img src=x onerror=alert(1)>`
	out := FillTemplateString("<p>{- BODY -}</p>", map[string]any{"BODY": in})

	if strings.Contains(out, "<img") {
		t.Errorf("unescaped <img> reached output: %q", out)
	}
	if !strings.Contains(out, "&lt;img") {
		t.Errorf("expected &lt;img escape, got %q", out)
	}
	if strings.Contains(out, "onerror=alert") && !strings.Contains(out, "&#34;") && !strings.Contains(out, "&#39;") {
		// At minimum the angle brackets must be escaped; checking that to
		// avoid making the assertion brittle to Go's specific escape style.
	}
}

func TestFillTemplateString_UnknownKeyKept(t *testing.T) {
	out := FillTemplateString("A: {- A -}, B: {- MISSING -}", map[string]any{"A": "x"})
	if !strings.Contains(out, "{- MISSING -}") {
		t.Errorf("unknown key should be left intact, got %q", out)
	}
}

func TestFillTemplateString_HandlesWhitespaceVariants(t *testing.T) {
	out := FillTemplateString("{- KEY -}|{-KEY-}|{-  KEY  -}", map[string]any{"KEY": "x"})
	want := "x|x|x"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Foo Bar Inc", "Foo Bar Inc"},
		{"Acme/Co.", "Acme_Co."},
		{"  trimmed  ", "trimmed"},
		{"a@b#c$d", "a_b_c_d"},
		{"a@@@b", "a_b"}, // run of disallowed chars collapses to one underscore
		{"file.txt", "file.txt"},
		{"weird;name|chars", "weird_name_chars"},
	}
	for _, tc := range cases {
		got := sanitizeFilename(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
