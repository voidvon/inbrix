package web

import (
	"lilmail/mailstore"
	"lilmail/models"
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
