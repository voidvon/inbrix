package mailstore

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"inbrix/models"
)

func seedPendingFlagMessages(t *testing.T, s *Store) Account {
	t.Helper()
	ctx := context.Background()
	account := testAccount(t, s, "owner", "owner@example.com", true)
	for _, folder := range []string{"INBOX", "Archive"} {
		if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: folder, SyncComplete: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{ID: "1", Flags: nil},
		{ID: "2", Flags: []string{`\Flagged`}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessages(ctx, account.ID, "Archive", []models.Email{{ID: "3", Flags: nil}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateFolderStats(ctx, account.ID, "INBOX"); err != nil {
		t.Fatal(err)
	}
	return account
}

func TestQueueSeenUpdatesIsAtomicAndCoalescesLatestIntent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := seedPendingFlagMessages(t, s)

	updated, err := s.QueueSeenUpdates(ctx, account.ID, []MessageFlagKey{{FolderName: "INBOX", UID: "1"}, {FolderName: "INBOX", UID: "2"}}, true)
	if err != nil || updated != 2 {
		t.Fatalf("queue read: updated=%d err=%v", updated, err)
	}
	message, _ := s.GetMessage(ctx, account.ID, "INBOX", "1")
	if !hasFlag(message.Flags, seenFlag) {
		t.Fatalf("message was not locally marked read: %v", message.Flags)
	}
	folders, _ := s.ListFolders(ctx, account.ID)
	for _, folder := range folders {
		if folder.Name == "INBOX" && folder.UnreadCount != 0 {
			t.Fatalf("local unread count = %d, want 0", folder.UnreadCount)
		}
	}

	first, err := s.ListPendingFlagUpdates(ctx, account.ID, time.Now(), 10)
	if err != nil || len(first) != 2 {
		t.Fatalf("pending reads = %+v err=%v", first, err)
	}
	var old PendingFlagUpdate
	for _, update := range first {
		if update.UID == "1" {
			old = update
		}
	}
	updated, err = s.QueueSeenUpdates(ctx, account.ID, []MessageFlagKey{{FolderName: "INBOX", UID: "1"}}, false)
	if err != nil || updated != 1 {
		t.Fatalf("queue unread: updated=%d err=%v", updated, err)
	}
	if err := s.CompletePendingFlagUpdate(ctx, old); err != nil {
		t.Fatal(err)
	}
	pending, _ := s.ListPendingFlagUpdates(ctx, account.ID, time.Now(), 10)
	if len(pending) != 2 {
		t.Fatalf("stale acknowledgement removed newer intent: %+v", pending)
	}
	for _, update := range pending {
		if update.UID == "1" && (update.Add || update.Version <= old.Version) {
			t.Fatalf("latest intent was not coalesced: old=%+v new=%+v", old, update)
		}
	}

	_, err = s.QueueSeenUpdates(ctx, account.ID, []MessageFlagKey{{FolderName: "Archive", UID: "3"}, {FolderName: "Archive", UID: "999"}}, true)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing message error = %v", err)
	}
	archive, _ := s.GetMessage(ctx, account.ID, "Archive", "3")
	if hasFlag(archive.Flags, seenFlag) {
		t.Fatal("partial batch update was not rolled back")
	}
}

func TestMessageRefreshPreservesOnlyPendingSeenIntent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := seedPendingFlagMessages(t, s)
	if _, err := s.QueueSeenUpdates(ctx, account.ID, []MessageFlagKey{{FolderName: "INBOX", UID: "1"}}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "1", Flags: []string{`\Flagged`}}}); err != nil {
		t.Fatal(err)
	}
	message, _ := s.GetMessage(ctx, account.ID, "INBOX", "1")
	if !hasFlag(message.Flags, seenFlag) || !hasFlag(message.Flags, `\Flagged`) {
		t.Fatalf("pending seen override did not merge remote flags: %v", message.Flags)
	}
	pending, _ := s.ListPendingFlagUpdates(ctx, account.ID, time.Now(), 10)
	if err := s.CompletePendingFlagUpdate(ctx, pending[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "1", Flags: []string{`\Flagged`}}}); err != nil {
		t.Fatal(err)
	}
	message, _ = s.GetMessage(ctx, account.ID, "INBOX", "1")
	if hasFlag(message.Flags, seenFlag) || !hasFlag(message.Flags, `\Flagged`) {
		t.Fatalf("remote flags were not authoritative after completion: %v", message.Flags)
	}
}

type recordingFlagWriter struct {
	calls []string
	err   error
}

func (w *recordingFlagWriter) SetMessageFlags(folder string, uids []string, flag string, add bool) error {
	sort.Strings(uids)
	w.calls = append(w.calls, folder+":"+stringsJoin(uids)+":"+flag+":"+fmtBool(add))
	return w.err
}

func stringsJoin(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += ","
		}
		result += value
	}
	return result
}

func fmtBool(value bool) string {
	if value {
		return "add"
	}
	return "remove"
}

func TestFlushPendingFlagsBatchesAndRetries(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := seedPendingFlagMessages(t, s)
	if _, err := s.QueueSeenUpdates(ctx, account.ID, []MessageFlagKey{{FolderName: "INBOX", UID: "1"}, {FolderName: "INBOX", UID: "2"}, {FolderName: "Archive", UID: "3"}}, true); err != nil {
		t.Fatal(err)
	}
	manager := &SyncManager{store: s}
	writer := &recordingFlagWriter{}
	if err := manager.flushPendingFlags(ctx, writer, account.ID); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) != 2 {
		t.Fatalf("batch calls = %v, want one per folder", writer.calls)
	}
	pending, _ := s.ListPendingFlagUpdates(ctx, account.ID, time.Now(), 10)
	if len(pending) != 0 {
		t.Fatalf("completed updates remain pending: %+v", pending)
	}

	if _, err := s.QueueSeenUpdates(ctx, account.ID, []MessageFlagKey{{FolderName: "INBOX", UID: "1"}}, false); err != nil {
		t.Fatal(err)
	}
	failing := &recordingFlagWriter{err: errors.New("temporary IMAP failure")}
	if err := manager.flushPendingFlags(ctx, failing, account.ID); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var lastError string
	if err := s.db.QueryRowContext(ctx, `SELECT attempt_count, last_error FROM pending_flag_updates WHERE account_id = ? AND folder_name = 'INBOX' AND uid = 1`, account.ID).Scan(&attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || lastError == "" {
		t.Fatalf("retry state: attempts=%d error=%q", attempts, lastError)
	}
}

func hasFlag(flags []string, wanted string) bool {
	for _, flag := range flags {
		if flag == wanted {
			return true
		}
	}
	return false
}
