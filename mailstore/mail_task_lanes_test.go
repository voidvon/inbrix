package mailstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"inbrix/models"
)

func enqueueNotifiedTask(t *testing.T, s *Store, account Account, message models.Email) {
	t.Helper()
	ctx := context.Background()
	if err := s.SaveAccountWebhookSettings(ctx, account.OwnerID, account.ID, WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
}

func TestMailTaskNotificationDeadlineDoesNotWaitForRunningAI(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	enqueueNotifiedTask(t, s, account, message)
	now := time.Now()
	ai, claimed, err := s.claimMailTaskInLane(ctx, account.ID, now, mailTaskLaneAI)
	if err != nil || !claimed || ai.Type != mailTaskAnalysis {
		t.Fatal(ai, claimed, err)
	}
	if _, claimed, err := s.claimMailTaskInLane(ctx, account.ID, now, mailTaskLaneAI); err != nil || claimed {
		t.Fatal("parallel AI in one account", claimed, err)
	}
	if _, claimed, err := s.claimMailTaskInLane(ctx, account.ID, now, mailTaskLaneNotification); err != nil || claimed {
		t.Fatal("notification did not wait for initial analysis", claimed, err)
	}
	notification, claimed, err := s.claimMailTaskInLane(ctx, account.ID, now.Add(mailTaskNotificationWait+time.Second), mailTaskLaneNotification)
	if err != nil || !claimed || notification.Type != mailTaskNotification || notification.RunID == notification.Token {
		t.Fatal(notification, claimed, err)
	}
	if _, claimed, err := s.claimMailTaskInLane(ctx, account.ID, now.Add(mailTaskNotificationWait+time.Second), mailTaskLaneNotification); err != nil || claimed {
		t.Fatal("parallel notification delivery", claimed, err)
	}
	if err := s.completeMailTask(ctx, notification, false); err != nil {
		t.Fatal(err)
	}
	if err := s.completeMailTask(ctx, ai, false); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryMailTask(ctx, notification.MessageSummaryKey, notification.Type); !errors.Is(err, ErrNotFound) {
		t.Fatal("notification replayed", err)
	}
}

func TestMailTaskNotificationReleasedImmediatelyAfterFirstAnalysisFailure(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	enqueueNotifiedTask(t, s, account, message)
	now := time.Now()
	ai, _, err := s.claimMailTaskInLane(ctx, account.ID, now, mailTaskLaneAI)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.failMailTask(ctx, ai, errors.New("temporary model outage")); err != nil {
		t.Fatal(err)
	}
	notification, claimed, err := s.claimMailTaskInLane(ctx, account.ID, now.Add(time.Second), mailTaskLaneNotification)
	if err != nil || !claimed || notification.Type != mailTaskNotification {
		t.Fatal(notification, claimed, err)
	}
}

func TestMailTaskGlobalAICapDoesNotConsumeNotificationCapacity(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	now := time.Now()
	var held []MailTask
	var last Account
	for i := 0; i < mailTaskMaxConcurrentAI+1; i++ {
		current := testAccount(t, s, account.OwnerID, fmt.Sprintf("lane-%d@example.com", i), false)
		enqueueNotifiedTask(t, s, current, message)
		task, claimed, err := s.claimMailTaskInLane(ctx, current.ID, now, mailTaskLaneAI)
		if err != nil {
			t.Fatal(err)
		}
		if i < mailTaskMaxConcurrentAI {
			if !claimed {
				t.Fatal("capacity missing", i)
			}
			held = append(held, task)
		} else {
			if claimed {
				t.Fatal("global cap exceeded")
			}
			last = current
		}
	}
	other, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, claimed, err := other.claimMailTaskInLane(ctx, last.ID, now, mailTaskLaneAI); err != nil || claimed {
		t.Fatal("another process bypassed global cap", claimed, err)
	}
	if _, claimed, err := other.claimMailTaskInLane(ctx, last.ID, now.Add(mailTaskNotificationWait+time.Second), mailTaskLaneNotification); err != nil || !claimed {
		t.Fatal("notification blocked behind AI cap", claimed, err)
	}
	if err := s.completeMailTask(ctx, held[0], false); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := other.claimMailTaskInLane(ctx, last.ID, now, mailTaskLaneAI); err != nil || !claimed {
		t.Fatal("released capacity not reusable", claimed, err)
	}
}

func TestMailTaskRunnerRunsNotificationWhileModelIsBlockedAndJoinsOnStop(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	enqueueNotifiedTask(t, s, account, message)
	if _, err := s.db.Exec(`UPDATE mail_tasks SET created_at=?`, time.Now().Add(-mailTaskNotificationWait-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	aiStarted, notified := make(chan struct{}), make(chan struct{})
	runner := mailTaskRunner{store: s, claim: s.claimMailTaskInLane, execute: func(ctx context.Context, _ Account, task MailTask) (bool, error) {
		switch task.Type {
		case mailTaskAnalysis:
			close(aiStarted)
			<-ctx.Done()
			return false, ctx.Err()
		case mailTaskNotification:
			close(notified)
			return false, nil
		default:
			return true, nil
		}
	}}
	wake, stop, done := make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	go func() { defer close(done); runner.run(ctx, account.ID, wake, stop) }()
	defer func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
		<-done
	}()
	select {
	case <-aiStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("AI lane not started")
	}
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("model blocked notification lane")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runner failed to join cancelled lanes")
	}
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}, mailTaskAnalysis)
	if err != nil || task.Status != "pending" || task.Attempts != 0 {
		t.Fatal("shutdown consumed retry budget", task, err)
	}
	runs, err := s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, mailTaskAnalysis, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != "interrupted" || !runs[0].Retryable {
		t.Fatal(runs, err)
	}
}

func TestMailTaskNotificationLaneDoesNotWaitForIMAPRefresh(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	enqueueNotifiedTask(t, s, account, message)
	if _, err := s.db.Exec(`UPDATE mail_tasks SET created_at=?`, time.Now().Add(-mailTaskNotificationWait-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	delivery := &taskDeliveryClient{}
	m := &SyncManager{store: s, key: key, webhookClient: delivery}
	m.setAccountSyncing(account.ID, 1)
	runner := m.taskRunner()
	if err := runner.process(ctx, account, 10, mailTaskLaneNotification); err != nil {
		t.Fatal(err)
	}
	if delivery.calls != 1 {
		t.Fatal("sync blocked header-only notification")
	}
	if _, claimed, err := runner.claim(ctx, account.ID, time.Now(), mailTaskLaneAI); err != nil || claimed {
		t.Fatal("AI started during refresh", claimed, err)
	}
}

func TestMailTaskNotificationWaitsForItsSummaryRatherThanReplyOrTranslation(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	enqueueNotifiedTask(t, s, account, message)
	now := time.Now()
	if _, err := s.db.Exec(`INSERT INTO message_summaries(account_id,folder_name,uid,summary_type,summary_text,status,created_at,updated_at) VALUES(?,?,?,'reply_suggestion','Ready reply','ready',0,0)`, account.ID, message.Folder, message.ID); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := s.claimMailTaskInLane(ctx, account.ID, now, mailTaskLaneNotification); err != nil || claimed {
		t.Fatal("reply result released summary wait", claimed, err)
	}
	if _, err := s.db.Exec(`INSERT INTO message_summaries(account_id,folder_name,uid,summary_type,summary_text,status,created_at,updated_at) VALUES(?,?,?,'mail_summary','Ready summary','ready',0,0)`, account.ID, message.Folder, message.ID); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := s.claimMailTaskInLane(ctx, account.ID, now, mailTaskLaneNotification); err != nil || !claimed {
		t.Fatal("ready summary did not release notification", claimed, err)
	}
}

func TestMailTaskConcurrentClaimsRespectGlobalCapacity(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	accounts := []Account{}
	for i := range 8 {
		a := testAccount(t, s, account.OwnerID, fmt.Sprintf("concurrent-%d@example.com", i), false)
		if err := s.UpsertIncomingMessages(ctx, a.ID, message.Folder, []models.Email{message}); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, a)
	}
	var workers sync.WaitGroup
	results := make(chan bool, len(accounts))
	failures := make(chan error, len(accounts))
	for _, a := range accounts {
		workers.Add(1)
		go func(accountID string) {
			defer workers.Done()
			_, claimed, err := s.claimMailTaskInLane(ctx, accountID, time.Now(), mailTaskLaneAI)
			results <- claimed
			failures <- err
		}(a.ID)
	}
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for claimed := range results {
		if claimed {
			count++
		}
	}
	if count != mailTaskMaxConcurrentAI {
		t.Fatal("non-atomic concurrency cap", count)
	}
}
