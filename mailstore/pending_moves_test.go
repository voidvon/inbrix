package mailstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"inbrix/models"
)

type recordingMoveWriter struct {
	moves       []string
	trashFolder string
	discoverErr error
	moveErr     error
}

func (r *recordingMoveWriter) MoveMessage(srcFolder, uid, destFolder string) error {
	if r.moveErr != nil {
		return r.moveErr
	}
	r.moves = append(r.moves, srcFolder+":"+uid+"->"+destFolder)
	return nil
}

func (r *recordingMoveWriter) DiscoverTrashFolder() (string, error) {
	if r.discoverErr != nil {
		return "", r.discoverErr
	}
	if r.trashFolder != "" {
		return r.trashFolder, nil
	}
	return "Trash", nil
}

func seedPendingMoveMessages(t *testing.T, s *Store) Account {
	t.Helper()
	ctx := context.Background()
	account := testAccount(t, s, "owner", "owner@example.com", true)
	for _, folder := range []string{"INBOX", "Sent", "Trash"} {
		attrs := []string{}
		if folder == "Trash" {
			attrs = []string{`\Trash`}
		}
		if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: folder, Attributes: attrs, SyncComplete: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{ID: "101", Flags: nil},
		{ID: "102", Flags: []string{`\Seen`}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessages(ctx, account.ID, "Sent", []models.Email{
		{ID: "201", Flags: []string{`\Seen`}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateFolderStats(ctx, account.ID, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateFolderStats(ctx, account.ID, "Sent"); err != nil {
		t.Fatal(err)
	}
	return account
}

func TestQueueMessageMovesIsAtomicAndDeletesLocally(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := seedPendingMoveMessages(t, s)

	// Check initial folder stats
	folders, _ := s.ListFolders(ctx, account.ID)
	for _, f := range folders {
		if f.Name == "INBOX" && (f.MessageCount != 2 || f.UnreadCount != 1) {
			t.Fatalf("unexpected INBOX stats before move: %+v", f)
		}
	}

	keys := []MessageMoveKey{
		{FolderName: "INBOX", UID: "101"},
		{FolderName: "Sent", UID: "201"},
	}
	queued, err := s.QueueMessageMoves(ctx, account.ID, keys, "Trash")
	if err != nil || queued != 2 {
		t.Fatalf("QueueMessageMoves: queued=%d err=%v", queued, err)
	}

	// 1. Local messages should be deleted immediately
	if _, err := s.GetMessage(ctx, account.ID, "INBOX", "101"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("INBOX/101 was not deleted locally: %v", err)
	}
	if _, err := s.GetMessage(ctx, account.ID, "Sent", "201"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Sent/201 was not deleted locally: %v", err)
	}
	// INBOX/102 was NOT deleted
	if _, err := s.GetMessage(ctx, account.ID, "INBOX", "102"); err != nil {
		t.Fatalf("INBOX/102 should still exist: %v", err)
	}

	// 2. Folder stats should be updated immediately
	folders, _ = s.ListFolders(ctx, account.ID)
	for _, f := range folders {
		if f.Name == "INBOX" && (f.MessageCount != 1 || f.UnreadCount != 0) {
			t.Fatalf("unexpected INBOX stats after move: %+v", f)
		}
		if f.Name == "Sent" && (f.MessageCount != 0 || f.UnreadCount != 0) {
			t.Fatalf("unexpected Sent stats after move: %+v", f)
		}
	}

	// 3. Pending moves should be recorded
	pending, err := s.ListPendingMessageMoves(ctx, account.ID, time.Now(), 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending moves = %+v err=%v", pending, err)
	}
}

func TestUpsertMessagesPreventsGhostResurrection(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := seedPendingMoveMessages(t, s)

	keys := []MessageMoveKey{
		{FolderName: "INBOX", UID: "101"},
	}
	if _, err := s.QueueMessageMoves(ctx, account.ID, keys, "Trash"); err != nil {
		t.Fatal(err)
	}

	// An upstream sync arrives with message 101 still present on IMAP server
	syncIncoming := []models.Email{
		{ID: "101", Flags: nil, Subject: "Resurrect attempt"},
		{ID: "103", Flags: nil, Subject: "Legitimate new email"},
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", syncIncoming); err != nil {
		t.Fatal(err)
	}

	// 101 MUST NOT be resurrected
	if _, err := s.GetMessage(ctx, account.ID, "INBOX", "101"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Ghost message 101 was resurrected: %v", err)
	}
	// 103 SHOULD be inserted
	if msg, err := s.GetMessage(ctx, account.ID, "INBOX", "103"); err != nil || msg.Subject != "Legitimate new email" {
		t.Fatalf("New message 103 should exist: %v, %+v", err, msg)
	}
}

func TestFlushPendingMovesRetriesAndCompletes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := seedPendingMoveMessages(t, s)

	keys := []MessageMoveKey{
		{FolderName: "INBOX", UID: "101"},
		{FolderName: "Sent", UID: "201"},
	}
	if _, err := s.QueueMessageMoves(ctx, account.ID, keys, ""); err != nil {
		t.Fatal(err)
	}

	manager := &SyncManager{store: s}
	writer := &recordingMoveWriter{trashFolder: "Trash"}
	if err := manager.flushPendingMoves(ctx, writer, account.ID); err != nil {
		t.Fatal(err)
	}

	if len(writer.moves) != 2 {
		t.Fatalf("writer moves count = %d, want 2 (%v)", len(writer.moves), writer.moves)
	}
	expectedMoves := map[string]bool{
		"INBOX:101->Trash": true,
		"Sent:201->Trash":  true,
	}
	for _, m := range writer.moves {
		if !expectedMoves[m] {
			t.Fatalf("unexpected move: %s", m)
		}
	}

	// After success, pending moves should be cleared
	pending, _ := s.ListPendingMessageMoves(ctx, account.ID, time.Now(), 10)
	if len(pending) != 0 {
		t.Fatalf("completed moves remain pending: %+v", pending)
	}

	// Test failure and retry backoff
	if _, err := s.QueueMessageMoves(ctx, account.ID, []MessageMoveKey{{FolderName: "INBOX", UID: "102"}}, "Trash"); err != nil {
		t.Fatal(err)
	}
	failing := &recordingMoveWriter{moveErr: errors.New("network timeout during IMAP move")}
	if err := manager.flushPendingMoves(ctx, failing, account.ID); err != nil {
		t.Fatal(err)
	}

	var attempts int
	var lastError string
	if err := s.db.QueryRowContext(ctx, `SELECT attempt_count, last_error FROM pending_message_moves WHERE account_id = ? AND folder_name = 'INBOX' AND uid = 102`, account.ID).Scan(&attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || !strings.Contains(lastError, "network timeout") {
		t.Fatalf("retry state: attempts=%d error=%q", attempts, lastError)
	}
}

func TestResolveTrashFolder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := testAccount(t, s, "owner", "owner@example.com", true)

	// 1. By special use attribute
	if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: "CustomTrash", Attributes: []string{`\Trash`}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: "INBOX"}); err != nil {
		t.Fatal(err)
	}
	trash, err := s.ResolveTrashFolder(ctx, account.ID)
	if err != nil || trash != "CustomTrash" {
		t.Fatalf("ResolveTrashFolder: trash=%q err=%v, want CustomTrash", trash, err)
	}

	// 2. By name fallback when no special attribute
	account2 := testAccount(t, s, "owner2", "owner2@example.com", false)
	if err := s.UpsertFolder(ctx, Folder{AccountID: account2.ID, Name: "Deleted Items"}); err != nil {
		t.Fatal(err)
	}
	trash2, err := s.ResolveTrashFolder(ctx, account2.ID)
	if err != nil || trash2 != "Deleted Items" {
		t.Fatalf("ResolveTrashFolder fallback: trash=%q err=%v, want 'Deleted Items'", trash2, err)
	}
}
