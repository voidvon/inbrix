package web

import (
	"inbrix/models"
	"strings"
	"testing"
)

func TestSearchSnippet(t *testing.T) {
	text := strings.Repeat("filler ", 40) + "the invoice total is 4200 EUR and the rest follows here"

	snippet, matched := searchSnippet(text, []string{"invoice"})
	if !matched {
		t.Fatal("expected a match")
	}
	if !strings.Contains(snippet, "invoice total") {
		t.Fatalf("snippet = %q", snippet)
	}
	if !strings.HasPrefix(snippet, "…") {
		t.Fatalf("expected leading ellipsis: %q", snippet)
	}
	if strings.Contains(snippet, "filler filler filler filler filler filler") {
		t.Fatalf("snippet kept too much context: %q", snippet)
	}

	head, matched := searchSnippet("short body", []string{"absent"})
	if matched || head != "short body" {
		t.Fatalf("fallback snippet = %q, matched = %v", head, matched)
	}

	if snippet, matched := searchSnippet("", []string{"x"}); matched || snippet != "" {
		t.Fatalf("empty text snippet = %q, matched = %v", snippet, matched)
	}

	long := strings.Repeat("字", 300)
	clipped, matched := searchSnippet(long, nil)
	if matched {
		t.Fatal("nil terms must not report a match")
	}
	if len([]rune(clipped)) != mailSearchSnippetLead+mailSearchSnippetTail+1 {
		t.Fatalf("clipped rune length = %d", len([]rune(clipped)))
	}
}

func TestMessagePlainTextFallsBackToHTML(t *testing.T) {
	htmlOnly := models.Email{
		Subject: "Newsletter",
		HTML:    `<html><head><style>.a{color:#333}</style></head><body><p>Spring sale on widgets</p></body></html>`,
	}
	text := messagePlainText(htmlOnly)
	if !strings.Contains(text, "Spring sale on widgets") {
		t.Fatalf("plain text = %q", text)
	}
	if strings.Contains(text, "color") || strings.Contains(text, "style") {
		t.Fatalf("style content leaked into plain text: %q", text)
	}

	// The reader sees the HTML part, so it also supplies the snippet text even
	// when a text/plain alternative exists.
	withBody := models.Email{Body: "plain body", HTML: "<p>rich body</p>"}
	if got := messagePlainText(withBody); got != "rich body" {
		t.Fatalf("plain text = %q", got)
	}

	bodyOnly := models.Email{Body: "plain body"}
	if got := messagePlainText(bodyOnly); got != "plain body" {
		t.Fatalf("plain text = %q", got)
	}
}

func TestMailMessageMatchesTerms(t *testing.T) {
	email := models.Email{
		Subject: "Quarterly report",
		From:    "alice@example.com",
		To:      "user@example.com",
		Body:    "The invoice total is 4200 EUR.",
	}
	if !mailMessageMatchesTerms(email, []string{"invoice"}) {
		t.Fatal("body term did not match")
	}
	if !mailMessageMatchesTerms(email, []string{"invoice", "quarterly"}) {
		t.Fatal("terms across body and subject did not match")
	}
	if mailMessageMatchesTerms(email, []string{"invoice", "missing"}) {
		t.Fatal("missing term matched")
	}
	if !mailMessageMatchesTerms(email, nil) {
		t.Fatal("an operator-only query must match")
	}

	markupOnly := models.Email{Subject: "Plain note", HTML: `<style>.widget{color:red}</style><p>Nothing</p>`}
	if mailMessageMatchesTerms(markupOnly, []string{"widget"}) {
		t.Fatal("markup-only term matched")
	}
}
