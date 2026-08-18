package web

import (
	"encoding/json"
	"inbrix/mailstore"
	"inbrix/models"
	"strings"
	"testing"
	"time"
)

func TestFlagsWithSeen(t *testing.T) {
	tests := []struct {
		name        string
		flags       []string
		seen        bool
		want        []string
		wantChanged bool
	}{
		{name: "mark read", flags: []string{`\Flagged`}, seen: true, want: []string{`\Flagged`, `\Seen`}, wantChanged: true},
		{name: "already read", flags: []string{`\Seen`, `\Flagged`}, seen: true, want: []string{`\Seen`, `\Flagged`}},
		{name: "mark unread", flags: []string{`\Seen`, `\Flagged`}, seen: false, want: []string{`\Flagged`}, wantChanged: true},
		{name: "already unread", flags: []string{`\Flagged`}, seen: false, want: []string{`\Flagged`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed := flagsWithSeen(test.flags, test.seen)
			if changed != test.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, test.wantChanged)
			}
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Fatalf("flags = %v, want %v", got, test.want)
			}
		})
	}
}

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

func TestConversationIdentityUsesParticipantsNotMessagesOrSubject(t *testing.T) {
	account := mailstore.Account{ID: "acct-stable", Email: "me@example.com"}
	when := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	first := models.Email{ID: "1", Folder: "INBOX", MessageID: "<one@example.com>", From: "Alice <ALICE@example.com>", To: "me@example.com", Subject: "First subject", Date: when}
	initial := buildConversations(account, []models.Email{first})
	if len(initial) != 1 {
		t.Fatalf("initial conversation count = %d, want 1", len(initial))
	}

	second := models.Email{ID: "2", Folder: "Sent Messages", MessageID: "<two@example.com>", From: "me@example.com", To: "alice@example.com", Subject: "Unrelated subject", Date: when.Add(time.Hour)}
	updated := buildConversations(account, []models.Email{first, second})
	if len(updated) != 1 || updated[0].Count != 2 {
		t.Fatalf("same participant messages were not merged: %+v", updated)
	}
	if updated[0].ID != initial[0].ID {
		t.Fatalf("conversation ID changed after adding a message: %q -> %q", initial[0].ID, updated[0].ID)
	}
	if updated[0].PeerEmail != "alice@example.com" {
		t.Fatalf("participant was not normalized: %q", updated[0].PeerEmail)
	}
}

func TestConversationIdentitySeparatesParticipantSets(t *testing.T) {
	account := mailstore.Account{ID: "acct-participants", Email: "me@example.com"}
	when := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	emails := []models.Email{
		{ID: "1", Folder: "INBOX", From: "alice@example.com", To: "me@example.com", Subject: "Alice", Date: when},
		{ID: "2", Folder: "Sent Messages", From: "me@example.com", To: "bob@example.com, alice@example.com", Subject: "Alice and Bob", Date: when.Add(time.Hour)},
		{ID: "3", Folder: "INBOX", From: "bob@example.com", To: "Alice <alice@example.com>, Me <ME@example.com>", Subject: "Order differs", Date: when.Add(2 * time.Hour)},
	}
	conversations := buildConversations(account, emails)
	if len(conversations) != 2 {
		t.Fatalf("conversation count = %d, want 2", len(conversations))
	}
	if conversations[0].PeerEmail != "alice@example.com, bob@example.com" || conversations[0].Count != 2 {
		t.Fatalf("multi-participant conversation = %+v", conversations[0])
	}
	if conversations[1].PeerEmail != "alice@example.com" || conversations[1].Count != 1 {
		t.Fatalf("single-participant conversation = %+v", conversations[1])
	}
	if conversations[0].ID == conversations[1].ID {
		t.Fatal("different participant sets produced the same conversation ID")
	}
}

func TestConversationIDIgnoresParticipantOrder(t *testing.T) {
	accountID := "acct-order"
	first := []string{"alice@example.com", "bob@example.com"}
	second := []string{"bob@example.com", "alice@example.com"}
	if conversationID(accountID, first) != conversationID(accountID, second) {
		t.Fatal("participant order changed the conversation ID")
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

func TestConversationJSONRepairsCIDImagesFromLegacyMetadata(t *testing.T) {
	account := mailstore.Account{ID: "acct-cid-legacy", Email: "me@example.com"}
	conversations := buildConversations(account, []models.Email{{
		ID: "111", Folder: "Sent Messages", MessageID: "<cid-legacy@example.com>",
		From: "me@example.com", To: "alice@example.com", Subject: "Inline images",
		HTML: `<img src="cid:CF554050@E2A49B21.4E377C6A00000000.jpg"><img src="cid:2FDA3758@6797342C.4E377C6A00000000.jpg"><img src="cid:57567628@217D9117.4E377C6A00000000.jpg">`,
		Date: time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC),
		Attachments: []models.Attachment{
			{ID: "image-1", Filename: "=?gbk?B?NTc1Njc2MjhAMjE3RDkxMTcuNEUzNzdDNkEwMDAw?=", ContentType: "image/jpeg", ContentID: "57567628@217D9117.4E377C6A00000000.jpg"},
			{ID: "image-2", Filename: "=?gbk?B?MkZEQTM3NThANjc5NzM0MkMuNEUzNzdDNkEwMDAw?=", ContentType: "image/jpeg", ContentID: "2FDA3758@6797342C.4E377C6A00000000.jpg"},
			{ID: "image-3", Filename: "=?gbk?B?Q0Y1NTQwNTBARTJBNDlCMjEuNEUzNzdDNkEwMDAw?=", ContentType: "image/jpeg", ContentID: "CF554050@E2A49B21.4E377C6A00000000.jpg"},
			{ID: "pdf", Filename: "Bank_Account_Details.pdf", ContentType: "application/octet-stream", Size: 377116},
		},
	}})

	detail := conversationDetailJSON(conversations[0])
	message := detail.Messages[0]
	if len(message.Attachments) != 1 || message.Attachments[0].Filename != "Bank_Account_Details.pdf" {
		t.Fatalf("CID images were surfaced as regular attachments: %+v", message.Attachments)
	}
	if !message.HasAttachments {
		t.Fatal("real PDF attachment was lost")
	}
	for _, contentID := range []string{
		"57567628@217D9117.4E377C6A00000000.jpg",
		"2FDA3758@6797342C.4E377C6A00000000.jpg",
		"CF554050@E2A49B21.4E377C6A00000000.jpg",
	} {
		if strings.Contains(message.HTML, "cid:"+contentID) || !strings.Contains(message.HTML, "/api/attachment/") {
			t.Fatalf("CID %q was not rewritten to a local image URL: %s", contentID, message.HTML)
		}
	}
}

func TestCollapseQuotedHTML(t *testing.T) {
	out := collapseQuotedHTML(`<div>New reply</div><div class="gmail_quote"><blockquote>Original</blockquote></div>`)
	if !strings.Contains(out, `<details class="inbrix-quoted">`) || !strings.Contains(out, "New reply") || !strings.Contains(out, "Original") {
		t.Fatalf("quoted HTML was not collapsed: %s", out)
	}
}

func TestCollapseQuotedHTMLUsesLocale(t *testing.T) {
	out := collapseQuotedHTMLForLocale(`<div>New reply</div><div class="gmail_quote"><blockquote>Original</blockquote></div>`, "zh-CN")
	if !strings.Contains(out, "显示引用内容") {
		t.Fatalf("quoted HTML did not use the Chinese summary: %s", out)
	}
}

func TestCollapseQuotedHTMLIncludetail(t *testing.T) {
	out := collapseQuotedHTML(`<div>New reply</div><includetail><div>------------------ Original ------------------</div><div>From: old@example.com</div><div>Old message</div></includetail>`)
	if !strings.Contains(out, `<details class="inbrix-quoted">`) || !strings.Contains(out, "New reply") || !strings.Contains(out, "Old message") {
		t.Fatalf("includetail quoted HTML was not collapsed: %s", out)
	}
	if strings.Contains(out, `<details class="inbrix-quoted"><summary>Show quoted message</summary><div>New reply`) {
		t.Fatalf("new reply was incorrectly placed inside quoted block: %s", out)
	}
}

func TestCollapseQuotedHTMLOriginalSeparator(t *testing.T) {
	out := collapseQuotedHTML(`<div>New reply</div><div>------------------ Original ------------------</div><div>From: old@example.com</div><div>Old message</div>`)
	if !strings.Contains(out, `<details class="inbrix-quoted">`) || !strings.Contains(out, "New reply") || !strings.Contains(out, "Old message") {
		t.Fatalf("original separator HTML was not collapsed: %s", out)
	}
	if strings.Contains(out, `<details class="inbrix-quoted"><summary>Show quoted message</summary><div>New reply`) {
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
