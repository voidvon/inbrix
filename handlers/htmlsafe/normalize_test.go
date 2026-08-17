package htmlsafe

import (
	"strings"
	"testing"
)

func TestNormalizeHTMLSanitizesWithoutRewritingStructure(t *testing.T) {
	in := `<div style="font-family:Arial" data-layout="mail"><p>Hello <strong>world</strong>.</p>` +
		`<div><br></div><table border="1"><tr><td>Cell</td></tr></table>` +
		`<a href="https://example.com">Read more</a>` +
		`<img src="cid:logo" onerror="alert(1)" alt="Logo" width="180">` +
		`<style>.hidden{display:none}</style><script>alert(document.cookie)</script>` +
		`<a href="javascript:alert(1)">bad link</a></div>`

	out, err := NormalizeHTML(in)
	if err != nil {
		t.Fatalf("NormalizeHTML: %v", err)
	}
	lower := strings.ToLower(out)
	for _, banned := range []string{"<script", "onerror", "javascript:", "<style"} {
		if strings.Contains(lower, banned) {
			t.Errorf("normalized HTML contains %q: %q", banned, out)
		}
	}
	for _, want := range []string{`<div style="font-family:Arial" data-layout="mail">`, `<div><br></div>`, `<table border="1"><tr><td>Cell</td></tr></table>`, `<strong>world</strong>`, `href="https://example.com"`, `src="cid:logo"`, `width="180"`, "bad link"} {
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

func TestNormalizeHTMLPreservesImageDimensions(t *testing.T) {
	in := `<p><img src="cid:logo" alt="Logo" width="180" height="42"></p>` +
		`<p><img src="cid:styled" style="width: 180px; height: 42px"></p>`

	out, err := NormalizeHTML(in)
	if err != nil {
		t.Fatalf("NormalizeHTML: %v", err)
	}
	if !strings.Contains(out, `style="width: 180px; height: 42px"`) {
		t.Fatalf("inline image style was not preserved: %q", out)
	}
}

func TestPlainTextFromHTMLPreservesLineBreaks(t *testing.T) {
	got := PlainTextFromHTML(`<p>Hello&nbsp;<strong>there</strong></p><p>Next line</p>`)
	if got != "Hello there\nNext line" {
		t.Fatalf("PlainTextFromHTML = %q", got)
	}
	if got := PlainTextFromHTML(`<p>First</p><p><br></p><p>Third</p>`); got != "First\n\nThird" {
		t.Fatalf("PlainTextFromHTML blank lines = %q", got)
	}
}
