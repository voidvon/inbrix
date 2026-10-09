package mailstore

import (
	"strings"
	"testing"

	"inbrix/models"
)

func TestCurrentMessageTextStripsPlainTextQuotes(t *testing.T) {
	input := "Current answer\r\n\r\n在 2026年8月17日，Alice 写道：\r\n> Previous message"
	if got := currentMessageText(input, ""); got != "Current answer" {
		t.Fatalf("currentMessageText = %q", got)
	}
}

func TestCurrentMessageTextStripsHTMLQuoteContainers(t *testing.T) {
	input := `<div>Current <strong>answer</strong></div><div class="gmail_quote"><blockquote>Previous message</blockquote></div>`
	got := currentMessageText("", input)
	if !strings.Contains(got, "Current answer") || strings.Contains(got, "Previous message") {
		t.Fatalf("currentMessageText = %q", got)
	}
}

func TestCurrentMessageTextPreservesUnquotedBody(t *testing.T) {
	input := "Current answer\nwith a normal second paragraph."
	if got := currentMessageText(input, ""); got != input {
		t.Fatalf("currentMessageText = %q", got)
	}
}

func TestCurrentMessageTextStripsOutlookHeaders(t *testing.T) {
	input := "Current answer\r\n\r\nFrom: Alice <alice@example.com>\r\nSent: Monday, October 5, 2026\r\nTo: Bob <bob@example.com>\r\nSubject: Old subject\r\n\r\nPrevious content"
	if got := currentMessageText(input, ""); got != "Current answer" {
		t.Fatalf("currentMessageText = %q", got)
	}
}

func TestCurrentMessageTextPrefersCleanHTMLWhenPlainHasQuotes(t *testing.T) {
	plain := "Current answer\n\nSome unformatted quote lines from older email"
	html := `<div>Current answer</div><div data-inbrix-reply-quote="true">Some unformatted quote lines from older email</div>`
	got := currentMessageText(plain, html)
	if strings.Contains(got, "Some unformatted quote lines") || !strings.Contains(got, "Current answer") {
		t.Fatalf("currentMessageText should prefer stripped HTML, got %q", got)
	}
}

func TestDirectParentMessageIDExtractsLastToken(t *testing.T) {
	message := models.Email{InReplyTo: "replying to <older@example.com> <parent@example.com>"}
	if got := directParentMessageID(message); got != "<parent@example.com>" {
		t.Fatalf("directParentMessageID = %q", got)
	}
}
