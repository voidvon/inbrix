package mailstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"inbrix/config"
	"inbrix/handlers/api"
	"inbrix/models"

	"github.com/emersion/go-imap"
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

func TestNeedsInitialFolderSyncPreservesBaselineAfterFailure(t *testing.T) {
	now := time.Now()
	if !needsInitialFolderSync(SyncState{}, ErrNotFound) {
		t.Fatal("a missing folder state must require an initial sync")
	}
	if needsInitialFolderSync(SyncState{LastSyncAt: now, LastError: "fetch failed"}, nil) {
		t.Fatal("a transient failure must not turn new mail into initial history")
	}
	if !needsInitialFolderSync(SyncState{LastSyncAt: now}, errors.New("read state")) {
		t.Fatal("a state lookup error must not enter incremental processing")
	}
}

// Embedding the real client makes unexpected operations fail rather than
// silently succeeding. These tests supply only the IMAP responses they need.
type recoverySyncClient struct {
	*api.Client
	selectErr error
	pages     [][]models.Email
	afterUID  uint32
	fullCalls int
}

func (c *recoverySyncClient) Select(string, bool) (*imap.MailboxStatus, error) {
	return &imap.MailboxStatus{UidNext: 752, Messages: 2}, c.selectErr
}

func (c *recoverySyncClient) FetchMessageFlagsPaged(string, uint32, uint32) ([]api.MessageFlagItem, error) {
	return nil, nil
}

func (c *recoverySyncClient) FetchMessagesSinceUID(_ string, after, _ uint32) ([]models.Email, error) {
	c.afterUID = after
	return []models.Email{{ID: "751", Body: "new inquiry", BodyCached: true, AttachmentMetadataCached: true}}, nil
}

func (c *recoverySyncClient) FetchMessagesPaged(_ string, _, offset uint32) ([]models.Email, error) {
	c.fullCalls++
	if int(offset) >= len(c.pages) {
		return nil, errors.New("connection lost during initial scan")
	}
	return c.pages[offset], nil
}

func TestSyncRecoveryQueuesNewMailWithoutReplayingHistory(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := testAccount(t, s, "owner", "sales@example.com", true)
	if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: "INBOX", MessageCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAccountWebhookSettings(ctx, account.OwnerID, account.ID, WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "750", BodyCached: true, AttachmentMetadataCached: true}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFolderSync(ctx, account.ID, "INBOX", true, nil); err != nil {
		t.Fatal(err)
	}
	baseline := time.Unix(1700000000, 0)
	if _, err := s.db.ExecContext(ctx, `UPDATE folders SET last_sync_at=? WHERE account_id=? AND name='INBOX'`, baseline.Unix(), account.ID); err != nil {
		t.Fatal(err)
	}
	m := NewSyncManager(s, "", config.MailSyncConfig{BatchSize: 200})
	client := &recoverySyncClient{selectErr: errors.New("database is locked")}
	cause := m.syncFolder(ctx, client, account.ID, "INBOX")
	if cause == nil {
		t.Fatal("expected a failed sync")
	}
	if err := s.MarkFolderSync(ctx, account.ID, "INBOX", false, cause); err != nil {
		t.Fatal(err)
	}
	state, err := s.GetSyncState(ctx, account.ID, "INBOX")
	if err != nil || !state.LastSyncAt.Equal(baseline) || state.LastError == "" {
		t.Fatalf("failure changed the baseline: %+v %v", state, err)
	}
	// Match the previous release's persisted error state. Recovery must use
	// the existing baseline even when last_error is populated after restart.
	client.selectErr = nil
	if err := m.syncFolder(ctx, client, account.ID, "INBOX"); err != nil {
		t.Fatalf("recover sync: %v", err)
	}
	if client.afterUID != 750 {
		t.Fatalf("recovery cursor = %d, want 750", client.afterUID)
	}
	tasks, err := s.ListMailTasks(ctx, account.ID, "INBOX", "751", 100)
	if err != nil || len(tasks) != 3 {
		t.Fatalf("new mail tasks = %+v, err = %v", tasks, err)
	}
	for _, kind := range []string{mailTaskAnalysis, mailTaskReply} {
		task, ok, err := s.claimMailTaskInLane(ctx, account.ID, time.Now(), mailTaskLaneAI)
		if err != nil || !ok || task.Type != kind {
			t.Fatalf("claim %s: %+v %v %v", kind, task, ok, err)
		}
		if err := s.completeMailTask(ctx, task, true); err != nil {
			t.Fatal(err)
		}
	}
	// Use the notification lane, which can deliver after analysis is skipped.
	task, ok, err := s.claimMailTaskInLane(ctx, account.ID, time.Now(), mailTaskLaneNotification)
	if err != nil || !ok {
		t.Fatalf("claim notification: %+v %v %v", task, ok, err)
	}
	delivery := &taskDeliveryClient{}
	m.webhookClient = delivery
	if skipped, err := m.executeMailTask(ctx, account, task); err != nil || skipped {
		t.Fatalf("delivery: skipped=%v err=%v", skipped, err)
	}
	if err := s.completeMailTask(ctx, task, false); err != nil {
		t.Fatal(err)
	}
	if err := m.syncFolder(ctx, client, account.ID, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.claimMailTaskInLane(ctx, account.ID, time.Now(), mailTaskLaneNotification); err != nil || ok || delivery.calls != 1 {
		t.Fatalf("notification replayed: claimed=%v calls=%d err=%v", ok, delivery.calls, err)
	}
	if tasks, err := s.ListMailTasks(ctx, account.ID, "INBOX", "750", 100); err != nil || len(tasks) != 0 {
		t.Fatalf("historical mail queued: %+v %v", tasks, err)
	}
}

func TestSyncStateReadFailureDoesNotFallBackToHistory(t *testing.T) {
	s := openTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	m := NewSyncManager(s, "", config.MailSyncConfig{})
	client := &recoverySyncClient{}
	if err := m.syncFolder(context.Background(), client, "account", "INBOX"); err == nil {
		t.Fatal("expected sync state read failure")
	}
	if client.fullCalls != 0 {
		t.Fatal("unknown sync state was treated as initial history")
	}
}

func TestFailedInitialSyncDoesNotEstablishBaseline(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := testAccount(t, s, "owner", "sales@example.com", true)
	if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: "INBOX"}); err != nil {
		t.Fatal(err)
	}
	m := NewSyncManager(s, "", config.MailSyncConfig{BatchSize: 1})
	client := &recoverySyncClient{pages: [][]models.Email{{{ID: "750", BodyCached: true, AttachmentMetadataCached: true}}}}
	cause := m.syncFolder(ctx, client, account.ID, "INBOX")
	if cause == nil {
		t.Fatal("expected initial sync failure")
	}
	if err := s.MarkFolderSync(ctx, account.ID, "INBOX", false, cause); err != nil {
		t.Fatal(err)
	}
	state, err := s.GetSyncState(ctx, account.ID, "INBOX")
	if err != nil || !state.LastSyncAt.IsZero() || state.LastError == "" {
		t.Fatalf("failed initial sync established baseline: %+v %v", state, err)
	}
	client.pages = append(client.pages, nil)
	if err := m.syncFolder(ctx, client, account.ID, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if tasks, err := s.ListMailTasks(ctx, account.ID, "INBOX", "", 100); err != nil || len(tasks) != 0 {
		t.Fatalf("initial history created jobs: %+v %v", tasks, err)
	}
}
