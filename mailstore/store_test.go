package mailstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"lilmail/models"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testAccount(t *testing.T, s *Store, owner, email string, defaultAccount bool) Account {
	t.Helper()
	a, err := s.UpsertAccount(context.Background(), Account{
		OwnerID:           owner,
		Email:             email,
		Username:          email,
		Label:             email,
		IMAPServer:        "imap.example.com",
		IMAPPort:          993,
		IMAPTLS:           true,
		SMTPServer:        "smtp.example.com",
		SMTPPort:          587,
		SMTPStartTLS:      true,
		EncryptedPassword: "encrypted",
		AuthType:          "password",
		IsDefault:         defaultAccount,
	})
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	return a
}

func TestUsersAndAccountsAreIsolated(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	alice, err := s.CreateUser(ctx, "Alice@example.com", "Alice", "hash-a")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	bob, err := s.CreateUser(ctx, "bob@example.com", "Bob", "hash-b")
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	if alice.Login != "alice@example.com" {
		t.Fatalf("login was not normalized: %q", alice.Login)
	}
	if _, err := s.GetUserByLogin(ctx, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user error: got %v", err)
	}

	first := testAccount(t, s, alice.ID, "work@example.com", false)
	second := testAccount(t, s, alice.ID, "personal@example.com", false)
	_ = testAccount(t, s, bob.ID, "bob@example.com", true)

	aliceAccounts, err := s.ListAccounts(ctx, alice.ID)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(aliceAccounts) != 2 {
		t.Fatalf("alice account count: got %d, want 2", len(aliceAccounts))
	}
	if !aliceAccounts[0].IsDefault || aliceAccounts[0].ID != first.ID {
		t.Fatalf("first account should be default: %+v", aliceAccounts)
	}
	if second.OwnerID != alice.ID {
		t.Fatalf("second account owner: %q", second.OwnerID)
	}

	if err := s.SetDefaultAccount(ctx, alice.ID, second.ID); err != nil {
		t.Fatalf("SetDefaultAccount: %v", err)
	}
	aliceAccounts, _ = s.ListAccounts(ctx, alice.ID)
	if !aliceAccounts[0].IsDefault || aliceAccounts[0].ID != second.ID {
		t.Fatalf("default account was not changed: %+v", aliceAccounts)
	}
	if err := s.DeleteAccount(ctx, alice.ID, second.ID); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	aliceAccounts, _ = s.ListAccounts(ctx, alice.ID)
	if len(aliceAccounts) != 1 || !aliceAccounts[0].IsDefault {
		t.Fatalf("remaining account should be default: %+v", aliceAccounts)
	}

	bobAccounts, _ := s.ListAccounts(ctx, bob.ID)
	if len(bobAccounts) != 1 || bobAccounts[0].OwnerID != bob.ID {
		t.Fatalf("bob accounts leaked or disappeared: %+v", bobAccounts)
	}
}

func TestMessagesPreserveCachedBodyAndSupportSearch(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "owner@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, owner.ID, "owner@example.com", true)
	other := testAccount(t, s, owner.ID, "other@example.com", false)

	for _, folder := range []string{"INBOX", "Archive"} {
		if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: folder, SyncComplete: true}); err != nil {
			t.Fatal(err)
		}
	}
	when := time.Unix(1_700_000_000, 0)
	metadata := []models.Email{
		{ID: "10", From: "alice@example.com", Subject: "Hello", Preview: "preview", Date: when, Flags: []string{}},
		{ID: "11", From: "bob@example.com", Subject: "Report", Preview: "report", Date: when.Add(time.Minute), Flags: []string{`\Seen`}, HasAttachments: true},
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", metadata); err != nil {
		t.Fatalf("UpsertMessages metadata: %v", err)
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{
		ID: "10", From: "alice@example.com", Subject: "Hello full", Preview: "full preview", Body: "cached body", HTML: "<p>cached</p>", Date: when,
		Flags: []string{`\Seen`},
	}}); err != nil {
		t.Fatalf("UpsertMessages body: %v", err)
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{
		ID: "10", From: "alice@example.com", Subject: "Hello updated", Preview: "new preview", Date: when,
		Flags: []string{},
	}}); err != nil {
		t.Fatalf("UpsertMessages refresh: %v", err)
	}

	message, err := s.GetMessage(ctx, account.ID, "INBOX", "10")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if message.Subject != "Hello updated" || message.Body != "cached body" || message.HTML != "<p>cached</p>" {
		t.Fatalf("metadata refresh overwrote body: %+v", message)
	}
	if !message.BodyCached {
		t.Fatal("cached body flag was not restored from SQLite")
	}
	if len(message.Flags) != 0 {
		t.Fatalf("flags were not refreshed: %v", message.Flags)
	}

	if err := s.UpdateFolderStats(ctx, account.ID, "INBOX"); err != nil {
		t.Fatalf("UpdateFolderStats: %v", err)
	}
	folders, err := s.ListFolders(ctx, account.ID)
	if err != nil || len(folders) != 2 {
		t.Fatalf("ListFolders: %v, %+v", err, folders)
	}
	for _, folder := range folders {
		if folder.Name == "INBOX" && (folder.MessageCount != 2 || folder.UnreadCount != 1) {
			t.Fatalf("folder stats: %+v", folder)
		}
	}

	if err := s.UpsertMessages(ctx, account.ID, "Archive", []models.Email{{ID: "20", From: "alice@example.com", Subject: "Hello archive", Date: when}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessages(ctx, other.ID, "INBOX", []models.Email{{ID: "10", From: "other@example.com", Subject: "Private", Date: when}}); err != nil {
		t.Fatal(err)
	}

	got, folder, err := s.SearchMessages(ctx, account.ID, "INBOX", "in:Archive subject:Hello", 10)
	if err != nil {
		t.Fatalf("SearchMessages in: %v", err)
	}
	if folder != "Archive" || len(got) != 1 || got[0].ID != "20" {
		t.Fatalf("in: search: folder=%q messages=%+v", folder, got)
	}
	got, _, err = s.SearchMessages(ctx, account.ID, "INBOX", "is:unread", 10)
	if err != nil || len(got) != 1 || got[0].ID != "10" {
		t.Fatalf("is:unread: err=%v messages=%+v", err, got)
	}
	got, _, err = s.SearchMessages(ctx, account.ID, "INBOX", "is:read", 10)
	if err != nil || len(got) != 1 || got[0].ID != "11" {
		t.Fatalf("is:read: err=%v messages=%+v", err, got)
	}
	got, _, err = s.SearchMessages(ctx, account.ID, "INBOX", "has:attachment", 10)
	if err != nil || len(got) != 1 || got[0].ID != "11" {
		t.Fatalf("has:attachment: err=%v messages=%+v", err, got)
	}

	maxUID, err := s.MaxMessageUID(ctx, account.ID, "INBOX")
	if err != nil || maxUID != 11 {
		t.Fatalf("MaxMessageUID: got %d, err %v", maxUID, err)
	}
	if err := s.PruneFolder(ctx, account.ID, "INBOX", []uint32{10}); err != nil {
		t.Fatalf("PruneFolder: %v", err)
	}
	if _, err := s.GetMessage(ctx, account.ID, "INBOX", "11"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pruned message still exists: %v", err)
	}
	if err := s.DeleteMessage(ctx, other.ID, "INBOX", "10"); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
}

func TestMessageMetadataRefreshPreservesCachedAttachments(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "attachments@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, owner.ID, "attachments@example.com", true)

	// The list fetch intentionally has no BODYSTRUCTURE, so it has no
	// attachment metadata even though the complete fetch will find one.
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{
		ID: "42", Subject: "Report", Date: time.Unix(1_700_000_000, 0),
	}}); err != nil {
		t.Fatalf("metadata upsert: %v", err)
	}
	attachment := models.Attachment{
		ID: "attachment-token", PartID: "2", Filename: "report.pdf", ContentType: "application/pdf", Size: 1234,
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{
			ID: "42", Subject: "Report", BodyCached: true, HasAttachments: true,
			Attachments: []models.Attachment{attachment}, Date: time.Unix(1_700_000_000, 0),
		},
	}); err != nil {
		t.Fatalf("full message upsert: %v", err)
	}

	// A later lightweight refresh must update ordinary metadata without
	// erasing the attachment list or its derived flag.
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{
			ID: "42", Subject: "Report (updated flags)", Flags: []string{`\Seen`}, Date: time.Unix(1_700_000_000, 0),
		},
	}); err != nil {
		t.Fatalf("metadata refresh: %v", err)
	}
	got, err := s.GetMessage(ctx, account.ID, "INBOX", "42")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if !got.HasAttachments || len(got.Attachments) != 1 || got.Attachments[0].Filename != "report.pdf" {
		t.Fatalf("cached attachments were lost: has=%v attachments=%+v", got.HasAttachments, got.Attachments)
	}
}

func TestAttachmentMetadataCanBeRefreshedWithoutBody(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "metadata-refresh@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, owner.ID, "metadata-refresh@example.com", true)
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{
		ID: "7", Subject: "Legacy", Body: "already cached", BodyCached: true, Date: time.Unix(1_700_000_000, 0),
	}}); err != nil {
		t.Fatal(err)
	}
	missing, err := s.ListMessageUIDsMissingAttachmentMetadata(ctx, account.ID, "INBOX")
	if err != nil || len(missing) != 1 || missing[0] != "7" {
		t.Fatalf("pending metadata UIDs = %v, err=%v", missing, err)
	}
	attachments := []models.Attachment{{ID: "token", PartID: "2", Filename: "legacy.pdf", Size: 12}}
	if err := s.UpdateAttachmentMetadata(ctx, account.ID, "INBOX", "7", attachments); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMessage(ctx, account.ID, "INBOX", "7")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "already cached" || !got.BodyCached || !got.AttachmentMetadataCached || !got.HasAttachments || len(got.Attachments) != 1 {
		t.Fatalf("body or attachment metadata was not preserved: %+v", got)
	}
	missing, err = s.ListMessageUIDsMissingAttachmentMetadata(ctx, account.ID, "INBOX")
	if err != nil || len(missing) != 0 {
		t.Fatalf("metadata still pending: %v, err=%v", missing, err)
	}
	if err := s.ResetAttachmentMetadata(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	missing, err = s.ListMessageUIDsMissingAttachmentMetadata(ctx, account.ID, "INBOX")
	if err != nil || len(missing) != 1 || missing[0] != "7" {
		t.Fatalf("reset did not queue metadata refresh: %v, err=%v", missing, err)
	}
}

func TestListMessagesForFoldersPreservesSourceFolder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "conversation@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, owner.ID, "conversation@example.com", true)
	when := time.Unix(1_700_000_000, 0)
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "1", Subject: "Hello", Date: when}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessages(ctx, account.ID, "Sent Messages", []models.Email{{ID: "2", Subject: "Re: Hello", Date: when.Add(time.Minute)}}); err != nil {
		t.Fatal(err)
	}

	messages, err := s.ListMessagesForFolders(ctx, account.ID, []string{"INBOX", "Sent Messages"})
	if err != nil {
		t.Fatalf("ListMessagesForFolders: %v", err)
	}
	if len(messages) != 2 || messages[0].Folder != "INBOX" || messages[1].Folder != "Sent Messages" {
		t.Fatalf("source folders were not preserved: %+v", messages)
	}
}
