package mailstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"inbrix/models"
)

type taskHTTPClient struct {
	status     int
	body       string
	retryAfter string
}

func (c taskHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	header := make(http.Header)
	header.Set("Retry-After", c.retryAfter)
	return &http.Response{StatusCode: c.status, Header: header, Body: io.NopCloser(strings.NewReader(c.body))}, nil
}

func TestMailTaskRunStartsAtomicallyWithLease(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_run_insert BEFORE INSERT ON mail_task_runs BEGIN SELECT RAISE(ABORT,'journal unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimMailTask(ctx, account.ID, time.Now()); err == nil {
		t.Fatal("lease claimed without journal")
	}
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}, mailTaskAnalysis)
	if err != nil || task.Status != "pending" || task.Attempts != 0 || task.Token != "" {
		t.Fatal(task, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_run_insert`); err != nil {
		t.Fatal(err)
	}
	leased, claimed, err := s.ClaimMailTask(ctx, account.ID, time.Now())
	if err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_run_finish BEFORE UPDATE ON mail_task_runs BEGIN SELECT RAISE(ABORT,'journal unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.completeMailTask(ctx, leased, false); err == nil {
		t.Fatal("completion committed without journal")
	}
	task, err = s.GetMailTask(ctx, leased.MessageSummaryKey, leased.Type)
	if err != nil || task.Status != "running" || task.Token != leased.Token {
		t.Fatal("partial completion", task, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_run_finish`); err != nil {
		t.Fatal(err)
	}
	if err := s.completeMailTask(ctx, leased, false); err != nil {
		t.Fatal(err)
	}
}

func TestMailTaskPermanentFailureCapturesActualModelAndSkillVersions(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	m := &SyncManager{store: s, key: key, aiClient: taskHTTPClient{status: 401, body: `{"error":{"message":"API key expired"}}`}}
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}, mailTaskAnalysis)
	if err != nil || task.Status != "failed" || task.Attempts != 1 || !task.NextAttempt.IsZero() {
		t.Fatal(task, err)
	}
	runs, err := s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, mailTaskAnalysis, 10)
	if err != nil || len(runs) != 1 {
		t.Fatal(runs, err)
	}
	run := runs[0]
	if run.Status != "failed" || run.Phase != "generation" || run.ErrorKind != "authentication" || run.Retryable || run.Context.ModelID == "" || run.Context.ConfigHash == "" || run.Context.SkillFingerprint == "" || len(run.Context.SkillVersions) != 1 || run.Context.SkillVersions[0].Revision != 1 || run.FinishedAt.IsZero() {
		t.Fatal(run)
	}
	if err := s.RetryMailTask(ctx, task.MessageSummaryKey, task.Type); err != nil {
		t.Fatal(err)
	}
	m.aiClient = &analysisScriptClient{reply: func(_ int, _ string) string { return standaloneAnalysisOutput }}
	if err := m.processMailTasks(ctx, account, 1); err != nil {
		t.Fatal(err)
	}
	runs, err = s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, mailTaskAnalysis, 10)
	if err != nil || len(runs) != 2 || runs[0].Status != "ready" || runs[1].ErrorKind != "authentication" || runs[0].ID == runs[1].ID {
		t.Fatal(runs, err)
	}
}

func TestMailTaskRateLimitHonorsRetryAfter(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	m := &SyncManager{store: s, key: key, aiClient: taskHTTPClient{status: 429, body: `{"error":{"message":"slow down"}}`, retryAfter: "120"}}
	start := time.Now()
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}, mailTaskAnalysis)
	if err != nil || task.Status != "pending" || task.NextAttempt.Before(start.Add(119*time.Second)) {
		t.Fatal(task, err)
	}
	runs, err := s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, mailTaskAnalysis, 10)
	if err != nil || len(runs) != 1 || runs[0].ErrorKind != "rate_limit" || !runs[0].Retryable {
		t.Fatal(runs, err)
	}
}

func TestMailTaskRunLeaseRecoveryFencesOldCompletionAndCascades(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first, _, err := s.ClaimMailTask(ctx, account.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	second, claimed, err := s.ClaimMailTask(ctx, account.ID, now.Add(mailTaskLease+time.Second))
	if err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err := s.completeMailTask(ctx, first, false); err == nil {
		t.Fatal("old worker completed recovered run")
	}
	if err := s.completeMailTask(ctx, second, false); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, mailTaskAnalysis, 10)
	if err != nil || len(runs) != 2 || runs[0].Status != "ready" || runs[1].Status != "expired" || runs[1].ErrorKind != "lease_expired" {
		t.Fatal(runs, err)
	}
	if err := s.DeleteMessage(ctx, account.ID, message.Folder, message.ID); err != nil {
		t.Fatal(err)
	}
	runs, err = s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, "", 10)
	if err != nil || len(runs) != 0 {
		t.Fatal("orphan journal", runs, err)
	}
}

func TestMailTaskRunRetentionBoundsExplicitRetries(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	var last MailTask
	for i := range 55 {
		task, claimed, err := s.claimMailTaskInLane(ctx, account.ID, time.Now().Add(time.Duration(i)*time.Millisecond), mailTaskLaneAI)
		if err != nil || !claimed || task.Type != mailTaskAnalysis {
			t.Fatal(task, claimed, err)
		}
		if err := s.failMailTask(ctx, task, permanentMailTaskError("configuration", errors.New("repair required"))); err != nil {
			t.Fatal(err)
		}
		if i < 54 {
			if err := s.RetryMailTask(ctx, task.MessageSummaryKey, task.Type); err != nil {
				t.Fatal(err)
			}
		}
		last = task
	}
	runs, err := s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, mailTaskAnalysis, 100)
	if err != nil || len(runs) != 50 || runs[0].ID != last.RunID {
		t.Fatal(len(runs), err)
	}
}

func TestMailTaskRunCacheHitRecordsHistoricalVersions(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	client := &analysisScriptClient{reply: func(_ int, _ string) string { return standaloneAnalysisOutput }}
	first, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := s.GetAISkill(ctx, account.OwnerID, "mail-analysis")
	if err != nil {
		t.Fatal(err)
	}
	skill.Instructions += "\nNew workflow"
	if _, err := s.SaveAISkill(ctx, skill, skill.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	m := &SyncManager{store: s, key: key, aiClient: client}
	if err := m.processMailTasks(ctx, account, 1); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, mailTaskAnalysis, 10)
	if err != nil || len(runs) != 1 || !runs[0].Context.Cached || !runs[0].Context.Stale || runs[0].Context.ConfigHash != first.Record.ConfigHash || runs[0].Context.SkillVersions[0].Revision != 1 || len(client.inputs) != 1 {
		t.Fatal(runs, err)
	}
}

func TestMailTaskReplyCancellationReleasesGenerationLease(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &cancelledAnalysisClient{entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := GetOrCreateReplySuggestion(ctx, client, s, key, account, message, false)
		done <- err
	}()
	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("reply generation did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reply generation did not stop")
	}
	record, err := s.GetMessageReplySuggestion(context.Background(), MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID})
	if err != nil || record.Status != "failed" || record.GenerationToken != "" || !record.LeaseUntil.IsZero() {
		t.Fatal("cancelled reply retained its lease", record, err)
	}
}

func TestMailTaskInvalidAnalysisStopsAfterOneRetryWithoutRenotifying(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	enqueueNotifiedTask(t, s, account, message)
	client := &analysisScriptClient{reply: func(_ int, _ string) string { return "malformed analysis output" }}
	delivery := &taskDeliveryClient{}
	m := &SyncManager{store: s, key: key, aiClient: client, webhookClient: delivery}
	for i := range 2 {
		if i > 0 {
			if _, err := s.db.Exec(`UPDATE mail_tasks SET next_attempt=0 WHERE task_type='analysis'`); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.processMailTasks(ctx, account, 10); err != nil {
			t.Fatal(err)
		}
	}
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}, mailTaskAnalysis)
	if err != nil || task.Status != "failed" || task.Attempts != 2 || len(client.inputs) != 2 || delivery.calls != 1 {
		t.Fatal(task, err, len(client.inputs), delivery.calls)
	}
	runs, err := s.ListMailTaskRuns(ctx, account.ID, message.Folder, message.ID, mailTaskAnalysis, 10)
	if err != nil || len(runs) != 2 || runs[0].Retryable || !runs[1].Retryable || runs[0].ErrorKind != "invalid_output" {
		t.Fatal(runs, err)
	}
}
