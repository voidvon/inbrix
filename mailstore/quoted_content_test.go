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

func TestDirectParentMessageIDExtractsLastToken(t *testing.T) {
	message := models.Email{InReplyTo: "replying to <older@example.com> <parent@example.com>"}
	if got := directParentMessageID(message); got != "<parent@example.com>" {
		t.Fatalf("directParentMessageID = %q", got)
	}
}
