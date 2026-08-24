package mailstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNeedsInitialFolderSyncUntilBaselineIsEstablished(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := testAccount(t, s, "owner", "sales@example.com", true)

	if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: "INBOX"}); err != nil {
		t.Fatalf("UpsertFolder: %v", err)
	}
	state, err := s.GetSyncState(ctx, account.ID, "INBOX")
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if !needsInitialFolderSync(state, nil) {
		t.Fatal("a newly discovered folder must establish a baseline before messages can be treated as new")
	}

	if err := s.MarkFolderSync(ctx, account.ID, "INBOX", false, nil); err != nil {
		t.Fatalf("MarkFolderSync: %v", err)
	}
	state, err = s.GetSyncState(ctx, account.ID, "INBOX")
	if err != nil {
		t.Fatalf("GetSyncState after baseline: %v", err)
	}
	if needsInitialFolderSync(state, nil) {
		t.Fatal("a capped but successful initial scan must allow later incremental new-mail processing")
	}
}

func TestNeedsInitialFolderSyncRetriesMissingOrFailedState(t *testing.T) {
	now := time.Now()
	if !needsInitialFolderSync(SyncState{}, ErrNotFound) {
		t.Fatal("a missing folder state must require an initial sync")
	}
	if !needsInitialFolderSync(SyncState{LastSyncAt: now, LastError: "fetch failed"}, nil) {
		t.Fatal("a failed folder sync must be retried as a full sync")
	}
	if !needsInitialFolderSync(SyncState{LastSyncAt: now}, errors.New("read state")) {
		t.Fatal("a state lookup error must not enter incremental processing")
	}
}
