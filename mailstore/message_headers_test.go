package mailstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"inbrix/models"
)

func TestHeaderProjectionOmitsFullBodiesAndHydrationRestoresThem(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	user, err := store.CreateUser(ctx, "headers", "Headers", "password123")
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.UpsertAccount(ctx, Account{
		OwnerID: user.ID, Email: "user@example.com", Username: "user@example.com",
		EncryptedPassword: "enc", IMAPServer: "imap.example.com", IMAPPort: 993,
		SMTPServer: "smtp.example.com", SMTPPort: 587, IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: "INBOX"}); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("b", 5000)
	htmlBody := "<p>" + strings.Repeat("h", 5000) + "</p>"
	if err := store.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{
		ID: "300", Folder: "INBOX", From: "alice@example.com", To: "user@example.com",
		Subject: "Large body", Body: body, HTML: htmlBody, BodyCached: true,
		Date: time.Now(), MessageID: "<large@example.com>",
	}}); err != nil {
		t.Fatal(err)
	}

	headers, err := store.ListMessageHeadersForFolders(ctx, account.ID, []string{"INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 1 {
		t.Fatalf("headers = %+v", headers)
	}
	if len(headers[0].Body) > 1024 || headers[0].HTML != "" {
		t.Fatalf("header projection kept a full body: body=%d html=%d", len(headers[0].Body), len(headers[0].HTML))
	}

	full, err := store.ListMessagesByKeys(ctx, account.ID, []MessageKey{{FolderName: "INBOX", UID: "300"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 1 || full[0].Body != body || full[0].HTML != htmlBody {
		t.Fatalf("hydrated message lost its body: body=%d html=%d", len(full[0].Body), len(full[0].HTML))
	}
}
