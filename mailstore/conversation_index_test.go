package mailstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"inbrix/models"
)

func TestConversationIndexPersistsAndRejectsSupersededRebuild(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mail.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	account := testAccount(t, store, "owner", "index@example.test", true)
	if err := store.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: "INBOX"}); err != nil {
		t.Fatal(err)
	}
	revision, _, err := store.ConversationIndexRevision(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	row := ConversationIndexRow{
		ID: "conversation", Date: time.Unix(110, 0), Status: "unanswered",
		UnreadCount: 1, Summary: []byte(`{"id":"conversation"}`), Messages: []byte("[]"),
	}
	if err := store.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "1"}}); err != nil {
		t.Fatal(err)
	}
	if saved, err := store.ReplaceConversationIndex(ctx, account.ID, revision, []ConversationIndexRow{row}); err != nil || saved {
		t.Fatalf("superseded rebuild: saved=%t, err=%v", saved, err)
	}
	if _, _, _, err := store.ReadConversationIndexDetail(ctx, account.ID, row.ID); !errors.Is(err, ErrConversationIndexStale) {
		t.Fatalf("read stale index: %v", err)
	}
	revision, _, err = store.ConversationIndexRevision(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved, err := store.ReplaceConversationIndex(ctx, account.ID, revision, []ConversationIndexRow{row}); err != nil || !saved {
		t.Fatalf("rebuild current index: saved=%t, err=%v", saved, err)
	}
	if err := store.MarkFolderSync(ctx, account.ID, "INBOX", true, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	summary, _, actual, err := reopened.ReadConversationIndexDetail(ctx, account.ID, row.ID)
	if err != nil || string(summary) != string(row.Summary) || actual != revision {
		t.Fatalf("read persisted index: summary=%s, revision=%d, err=%v", summary, actual, err)
	}
}
