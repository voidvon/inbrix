package mailstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"inbrix/config"
	"inbrix/models"
)

func TestIncomingMailAndTasksCommitAtomically(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	message.ID = "43"
	if _, err := s.db.Exec(`CREATE TRIGGER fail_mail_queue BEFORE INSERT ON mail_tasks BEGIN SELECT RAISE(ABORT,'queue unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, "INBOX", []models.Email{message}); err == nil {
		t.Fatal("expected queue failure")
	}
	if _, err := s.GetMessage(ctx, account.ID, "INBOX", message.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mail committed without tasks: %v", err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_mail_queue`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListMailTasks(ctx, account.ID, "INBOX", message.ID, 100)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	if err := s.SaveAccountWebhookSettings(ctx, account.OwnerID, account.ID, WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: "INBOX", UID: message.ID}, mailTaskNotification); !errors.Is(err, ErrNotFound) {
		t.Fatalf("enabling notifications backfilled old mail: %v", err)
	}
	message.ID = "44"
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	tasks, err = s.ListMailTasks(ctx, account.ID, "INBOX", message.ID, 100)
	if err != nil || len(tasks) != 0 {
		t.Fatal("history synchronization created new-mail jobs")
	}
}

func TestMailTaskLeaseRecoveryAndAccountSerialization(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first, claimed, err := s.ClaimMailTask(ctx, account.ID, now)
	if err != nil || !claimed || first.Type != mailTaskAnalysis {
		t.Fatalf("first=%+v claimed=%v err=%v", first, claimed, err)
	}
	// A new Store instance represents another process or a restart.
	other, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, claimed, err := other.ClaimMailTask(ctx, account.ID, now); err != nil || claimed {
		t.Fatalf("concurrent account job claimed: %v %v", claimed, err)
	}
	recovered, claimed, err := other.ClaimMailTask(ctx, account.ID, now.Add(mailTaskLease+time.Second))
	if err != nil || !claimed || recovered.Type != first.Type || recovered.Token == first.Token || recovered.Attempts != 2 {
		t.Fatalf("recovery=%+v claimed=%v err=%v", recovered, claimed, err)
	}
	if err := s.completeMailTask(ctx, first, false); err == nil {
		t.Fatal("old lease completed recovered task")
	}
	if err := other.completeMailTask(ctx, recovered, false); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryMailTask(ctx, first.MessageSummaryKey, first.Type); !errors.Is(err, ErrNotFound) {
		t.Fatal("successful task could be replayed")
	}
	if err := s.DeleteMessage(ctx, account.ID, message.Folder, message.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMailTask(ctx, first.MessageSummaryKey, first.Type); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted message retained jobs")
	}
}

type taskDeliveryClient struct {
	calls    int
	fail     bool
	payloads []string
}

func (c *taskDeliveryClient) Do(req *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(req.Body)
	c.calls++
	c.payloads = append(c.payloads, string(raw))
	if c.fail {
		return nil, errors.New("delivery unavailable")
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":0}`))}, nil
}

func TestMailTaskRetriesDeliveryWithoutRegeneratingAnalysis(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.SaveAccountWebhookSettings(ctx, account.OwnerID, account.ID, WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	ai := &analysisScriptClient{reply: func(_ int, _ string) string { return standaloneAnalysisOutput }}
	delivery := &taskDeliveryClient{fail: true}
	m := &SyncManager{store: s, key: key, aiClient: ai, webhookClient: delivery}
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}, mailTaskNotification)
	if err != nil || task.Status != "pending" || task.Attempts != 1 || task.Payload == "" {
		t.Fatalf("delivery state=%+v err=%v", task, err)
	}
	modelCalls := len(ai.inputs)
	if _, err := s.db.Exec(`UPDATE mail_tasks SET next_attempt=0 WHERE task_type='notification'`); err != nil {
		t.Fatal(err)
	}
	delivery.fail = false
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	if delivery.calls != 2 || delivery.payloads[0] != delivery.payloads[1] || len(ai.inputs) != modelCalls {
		t.Fatal("delivery retry changed content or regenerated AI")
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	if delivery.calls != 2 {
		t.Fatal("successful notification was replayed")
	}
}

func TestMailAnalysisRetriesAfterFallbackWithoutSecondNotification(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.SaveAccountWebhookSettings(ctx, account.OwnerID, account.ID, WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	ai := &analysisScriptClient{reply: func(turn int, _ string) string {
		if turn == 1 {
			return "invalid analysis"
		}
		return standaloneAnalysisOutput
	}}
	delivery := &taskDeliveryClient{}
	m := &SyncManager{store: s, key: key, aiClient: ai, webhookClient: delivery}
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	if delivery.calls != 1 || !strings.Contains(delivery.payloads[0], "AI 分析暂未完成") {
		t.Fatalf("fallback missing: %v", delivery.payloads)
	}
	if _, err := s.db.Exec(`UPDATE mail_tasks SET next_attempt=0 WHERE task_type='analysis'`); err != nil {
		t.Fatal(err)
	}
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	result, err := s.GetMessageSummary(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID})
	if err != nil || result.Status != "ready" || delivery.calls != 1 {
		t.Fatalf("summary=%+v deliveries=%d err=%v", result, delivery.calls, err)
	}
}

func TestMailTaskExhaustionAndExplicitRetry(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	var task MailTask
	for range mailTaskMaxAttempts(mailTaskAnalysis) {
		if _, err := s.db.Exec(`UPDATE mail_tasks SET next_attempt=0 WHERE task_type='analysis'`); err != nil {
			t.Fatal(err)
		}
		var claimed bool
		var err error
		task, claimed, err = s.ClaimMailTask(ctx, account.ID, time.Now())
		if err != nil || !claimed || task.Type != mailTaskAnalysis {
			t.Fatalf("claim=%+v err=%v", task, err)
		}
		if err := s.failMailTask(ctx, task, errors.New("model unavailable")); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := s.GetMailTask(ctx, task.MessageSummaryKey, task.Type)
	if err != nil || stored.Status != "failed" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if err := s.RetryMailTask(ctx, task.MessageSummaryKey, task.Type); err != nil {
		t.Fatal(err)
	}
	stored, err = s.GetMailTask(ctx, task.MessageSummaryKey, task.Type)
	if err != nil || stored.Status != "pending" || stored.Attempts != 0 {
		t.Fatalf("retry=%+v err=%v", stored, err)
	}
}

func TestMailTasksWaitForSyncAndStopWithAccount(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	client := &cancelledAnalysisClient{entered: make(chan struct{})}
	m := NewSyncManager(s, key, config.MailSyncConfig{Enabled: true, Interval: 1})
	m.aiClient = client
	m.setAccountSyncing(account.ID, 1)
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.entered:
		t.Fatal("AI started during synchronization")
	default:
	}
	m.setAccountSyncing(account.ID, -1)
	m.StartAccount(account.ID)
	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		m.Stop()
		t.Fatal("task worker did not start")
	}
	done := make(chan struct{})
	go func() { m.StopAccount(account.ID); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("account stop did not cancel and join its task worker")
	}
	m.Stop()
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}, mailTaskAnalysis)
	if err != nil || task.Status != "pending" || task.Token != "" {
		t.Fatalf("interrupted task is not recoverable: %+v %v", task, err)
	}
}
