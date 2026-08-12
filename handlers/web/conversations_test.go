package web

import (
	"encoding/json"
	"lilmail/mailstore"
	"lilmail/models"
	"strings"
	"testing"
	"time"
)

func TestBuildConversationsMergesInboxAndSent(t *testing.T) {
	account := mailstore.Account{ID: "acct-1", Email: "me@example.com", Label: "Work"}
	when := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	emails := []models.Email{
		{
			ID:        "10",
			Folder:    "INBOX",
			MessageID: "<root@example.com>",
			From:      "alice@example.com",
			FromName:  "Alice",
			To:        "me@example.com",
			Subject:   "Project update",
			Body:      "Here is the update.",
			Date:      when,
		},
		{
			ID:         "22",
			Folder:     "Sent Messages",
			MessageID:  "<reply@example.com>",
			InReplyTo:  "<root@example.com>",
			References: []string{"<root@example.com>"},
			From:       "me@example.com",
			To:         "alice@example.com",
			ToNames:    []string{"Alice"},
			Subject:    "Re: Project update",
			Body:       "Thanks, sent.",
			Date:       when.Add(time.Hour),
		},
	}

	conversations := buildConversations(account, emails)
	if len(conversations) != 1 {
		t.Fatalf("conversation count = %d, want 1", len(conversations))
	}
	conversation := conversations[0]
	if conversation.Count != 2 {
		t.Fatalf("message count = %d, want 2", conversation.Count)
	}
	if conversation.Title != "Alice" || conversation.PeerEmail != "alice@example.com" {
		t.Fatalf("participant = %q <%s>", conversation.Title, conversation.PeerEmail)
	}
	if conversation.Messages[0].Outgoing || !conversation.Messages[1].Outgoing {
		t.Fatalf("message directions = %+v", conversation.Messages)
	}
	if conversation.Latest.ID != "22" || conversation.Preview != "Thanks, sent." {
		t.Fatalf("latest message = %+v", conversation.Latest)
	}
}

func TestConversationFoldersRecognizeQQSentMessages(t *testing.T) {
	folders := []mailstore.Folder{
		{Name: "INBOX"},
		{Name: "Sent Messages"},
		{Name: "Junk"},
	}
	names := conversationFolderNames(folders)
	if len(names) != 2 || names[0] != "INBOX" || names[1] != "Sent Messages" {
		t.Fatalf("conversation folders = %v", names)
	}
	if !isConversationMailboxName("Sent Messages") || isConversationMailboxName("Junk") {
		t.Fatal("conversation mailbox detection is incorrect")
	}
}

func TestConversationAttachmentMetadataDrivesJSON(t *testing.T) {
	account := mailstore.Account{ID: "acct-attachments", Email: "me@example.com"}
	conversations := buildConversations(account, []models.Email{{
		ID: "42", Folder: "INBOX", MessageID: "<attachment@example.com>",
		From: "alice@example.com", To: "me@example.com", Subject: "Report",
		Date: time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC),
		// Simulate a legacy mirror row whose boolean marker was reset while
		// attachment metadata survived the lightweight refresh.
		Attachments: []models.Attachment{{ID: "token", Filename: "report.pdf", Size: 1234}},
	}})
	if len(conversations) != 1 || !conversations[0].HasAttachments {
		t.Fatalf("conversation did not detect attachment metadata: %+v", conversations)
	}

	raw, err := json.Marshal(conversationDetailJSON(conversations[0]))
	if err != nil {
		t.Fatalf("marshal conversation detail: %v", err)
	}
	if !containsJSONBoolAndFilename(raw, "report.pdf") {
		t.Fatalf("conversation JSON omitted attachment metadata: %s", raw)
	}
}

func TestConversationJSONRendersInlineCIDOnlyInHTML(t *testing.T) {
	account := mailstore.Account{ID: "acct-inline", Email: "me@example.com"}
	conversations := buildConversations(account, []models.Email{{
		ID: "43", Folder: "INBOX", MessageID: "<inline@example.com>",
		From: "alice@example.com", To: "me@example.com", Subject: "Inline image", Body: "fallback",
		HTML: `<p>Hello<img src="cid:logo@example.com"></p>`, Date: time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC),
		Attachments: []models.Attachment{
			{ID: "inline-token", Filename: "logo.png", ContentType: "image/png", IsInline: true, ContentID: "logo@example.com"},
			{ID: "file-token", Filename: "report.pdf", ContentType: "application/pdf"},
		},
	}})

	detail := conversationDetailJSON(conversations[0])
	if len(detail.Messages) != 1 {
		t.Fatalf("messages = %d", len(detail.Messages))
	}
	message := detail.Messages[0]
	if strings.Contains(message.HTML, "cid:logo@example.com") || !strings.Contains(message.HTML, "/api/attachment/inline-token?inline=true&amp;account_email=me%40example.com") {
		t.Fatalf("inline cid was not rewritten: %s", message.HTML)
	}
	if len(message.Attachments) != 1 || message.Attachments[0].Filename != "report.pdf" {
		t.Fatalf("attachment list should contain only regular files: %+v", message.Attachments)
	}
	if !message.HasAttachments {
		t.Fatal("regular attachment marker was lost")
	}
}

func TestConversationJSONInlineOnlyDoesNotClaimAttachment(t *testing.T) {
	account := mailstore.Account{ID: "acct-inline-only", Email: "me@example.com"}
	conversations := buildConversations(account, []models.Email{{
		ID: "44", Folder: "INBOX", MessageID: "<inline-only@example.com>", From: "alice@example.com",
		To: "me@example.com", Subject: "Inline only", Date: time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC),
		Attachments: []models.Attachment{{ID: "inline-token", Filename: "logo.png", IsInline: true, ContentID: "logo"}},
	}})

	detail := conversationDetailJSON(conversations[0])
	if detail.Messages[0].HasAttachments || len(detail.Messages[0].Attachments) != 0 || conversations[0].HasAttachments {
		t.Fatalf("inline-only message surfaced as an attachment: %+v", detail.Messages[0])
	}
}

func TestCollapseQuotedHTML(t *testing.T) {
	out := collapseQuotedHTML(`<div>New reply</div><div class="gmail_quote"><blockquote>Original</blockquote></div>`)
	if !strings.Contains(out, `<details class="lilmail-quoted">`) || !strings.Contains(out, "New reply") || !strings.Contains(out, "Original") {
		t.Fatalf("quoted HTML was not collapsed: %s", out)
	}
}

func TestCollapseQuotedHTMLIncludetail(t *testing.T) {
	out := collapseQuotedHTML(`<div>New reply</div><includetail><div>------------------ Original ------------------</div><div>From: old@example.com</div><div>Old message</div></includetail>`)
	if !strings.Contains(out, `<details class="lilmail-quoted">`) || !strings.Contains(out, "New reply") || !strings.Contains(out, "Old message") {
		t.Fatalf("includetail quoted HTML was not collapsed: %s", out)
	}
	if strings.Contains(out, `<details class="lilmail-quoted"><summary>Show quoted message</summary><div>New reply`) {
		t.Fatalf("new reply was incorrectly placed inside quoted block: %s", out)
	}
}

func TestCollapseQuotedHTMLOriginalSeparator(t *testing.T) {
	out := collapseQuotedHTML(`<div>New reply</div><div>------------------ Original ------------------</div><div>From: old@example.com</div><div>Old message</div>`)
	if !strings.Contains(out, `<details class="lilmail-quoted">`) || !strings.Contains(out, "New reply") || !strings.Contains(out, "Old message") {
		t.Fatalf("original separator HTML was not collapsed: %s", out)
	}
	if strings.Contains(out, `<details class="lilmail-quoted"><summary>Show quoted message</summary><div>New reply`) {
		t.Fatalf("new reply was incorrectly placed inside quoted block: %s", out)
	}
}

func containsJSONBoolAndFilename(raw []byte, filename string) bool {
	var value struct {
		Messages []struct {
			HasAttachments bool `json:"hasAttachments"`
			Attachments    []struct {
				Filename string `json:"filename"`
			} `json:"attachments"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &value) != nil || len(value.Messages) != 1 {
		return false
	}
	return value.Messages[0].HasAttachments && len(value.Messages[0].Attachments) == 1 && value.Messages[0].Attachments[0].Filename == filename
}
