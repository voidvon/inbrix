package mailstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"inbrix/models"
)

func TestSearchMessagePageScopedPagination(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	user, err := db.CreateUser(ctx, "search-pages", "Search", "password")
	if err != nil {
		t.Fatal(err)
	}
	account, err := db.UpsertAccount(ctx, Account{OwnerID: user.ID, Email: "me@example.com", Username: "me@example.com", EncryptedPassword: "enc", IMAPServer: "imap.example.com", IMAPPort: 993, SMTPServer: "smtp.example.com", SMTPPort: 587})
	if err != nil {
		t.Fatal(err)
	}
	keys := []MessageKey{}
	when := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	for _, folder := range []string{"INBOX", "Sent"} {
		if err := db.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: folder}); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 4; i++ {
			uid := fmt.Sprint(i)
			email := models.Email{ID: uid, From: "peer@example.com", To: account.Email, Subject: "交期", Body: "交期为两周", Date: when}
			if err := db.UpsertMessages(ctx, account.ID, folder, []models.Email{email}); err != nil {
				t.Fatal(err)
			}
			keys = append(keys, MessageKey{FolderName: folder, UID: uid})
		}
	}
	// A different conversation in the same account must not leak into results.
	if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "99", From: "elsewhere@example.com", Subject: "交期", Date: when.Add(time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	cursor := ""
	for pageNumber := 0; ; pageNumber++ {
		page, err := db.SearchMessagePage(ctx, account.ID, "thread-1", "交期", cursor, 2, keys)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range page.Messages {
			key := message.Folder + "/" + message.ID
			if seen[key] || message.ID == "99" {
				t.Fatalf("duplicate/unrelated result: %s", key)
			}
			seen[key] = true
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Fatal("last page has a cursor")
			}
			break
		}
		if page.NextCursor == "" || pageNumber > 5 {
			t.Fatal("pagination did not advance")
		}
		if pageNumber == 0 {
			for _, request := range []struct{ account, scope, query string }{
				{account.ID, "thread-2", "交期"}, {account.ID, "thread-1", "付款"}, {"another-account", "thread-1", "交期"},
			} {
				if _, err := db.SearchMessagePage(ctx, request.account, request.scope, request.query, page.NextCursor, 2, keys); err == nil {
					t.Fatal("cursor accepted for a different account/scope/query")
				}
			}
			// New mail between pages must not shift the already established boundary.
			if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "100", Subject: "交期", Date: when.Add(2 * time.Hour)}}); err != nil {
				t.Fatal(err)
			}
			keys = append(keys, MessageKey{FolderName: "INBOX", UID: "100"})
		}
		cursor = page.NextCursor
	}
	if len(seen) != 8 {
		t.Fatalf("lost messages across tied dates/folders: %v", seen)
	}
	empty, err := db.SearchMessagePage(ctx, account.ID, "empty-thread", "", "", 2, []MessageKey{})
	if err != nil || len(empty.Messages) != 0 {
		t.Fatalf("empty scope returned mailbox contents: %+v, %v", empty, err)
	}
	global, err := db.SearchMessagePage(ctx, account.ID, "mailbox", "", "", 20, nil)
	if err != nil || len(global.Messages) != 10 {
		t.Fatalf("empty query did not list mail: %+v, %v", global, err)
	}
}
