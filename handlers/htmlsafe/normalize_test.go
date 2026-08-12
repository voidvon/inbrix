package htmlsafe

import (
	"strings"
	"testing"
)

func TestNormalizeHTMLUsesMarkdownAsSafeIntermediate(t *testing.T) {
	in := `<div style="font-family:Arial" data-layout="mail"><p>Hello <strong>world</strong>.</p>` +
		`<a href="https://example.com">Read more</a>` +
		`<img src="cid:logo" onerror="alert(1)" alt="Logo">` +
		`<script>alert(document.cookie)</script>` +
		`<a href="javascript:alert(1)">bad link</a></div>`

	out, err := NormalizeHTML(in)
	if err != nil {
		t.Fatalf("NormalizeHTML: %v", err)
	}
	lower := strings.ToLower(out)
	for _, banned := range []string{"<script", "onerror", "javascript:", "style=", "data-layout", "<div"} {
		if strings.Contains(lower, banned) {
			t.Errorf("normalized HTML contains %q: %q", banned, out)
		}
	}
	for _, want := range []string{"<strong>world</strong>", `href="https://example.com"`, `src="cid:logo"`, "bad link"} {
		if !strings.Contains(out, want) {
			t.Errorf("normalized HTML is missing %q: %q", want, out)
		}
	}
}

func TestNormalizeHTMLRejectsOversizedBody(t *testing.T) {
	_, err := NormalizeHTML(strings.Repeat("x", maxNormalizedHTMLBytes+1))
	if err == nil {
		t.Fatal("NormalizeHTML accepted an oversized body")
	}
}

func TestPlainTextFromHTML(t *testing.T) {
	got := PlainTextFromHTML(`<p>Hello&nbsp;<strong>there</strong></p><p>Next line</p>`)
	if got != "Hello there Next line" {
		t.Fatalf("PlainTextFromHTML = %q", got)
	}
}
