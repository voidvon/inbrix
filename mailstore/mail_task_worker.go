package mailstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"inbrix/events"
	mailapi "inbrix/handlers/api"
	"inbrix/models"
)

func (m *SyncManager) setAccountSyncing(accountID string, delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.syncing == nil {
		m.syncing = make(map[string]int)
	}
	m.syncing[accountID] += delta
	if m.syncing[accountID] <= 0 {
		delete(m.syncing, accountID)
	}
}

func (m *SyncManager) wakeMailTasks(accountID string) bool {
	m.mu.Lock()
	wake := m.taskWake[accountID]
	m.mu.Unlock()
	if wake == nil {
		return false
	}
	select {
	case wake <- struct{}{}:
	default:
	}
	return true
}

// The sync adapter only supplies readiness and application execution. Queue
// scheduling, leases, retries and attempt history belong to mailTaskRunner.
func (m *SyncManager) taskRunner() mailTaskRunner {
	return mailTaskRunner{store: m.store, execute: m.executeMailTask, after: m.wakeMailTasks,
		claim: func(ctx context.Context, accountID string, now time.Time, lane string) (MailTask, bool, error) {
			if lane == mailTaskLaneNotification {
				return m.store.claimMailTaskInLane(ctx, accountID, now, lane)
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.syncing[accountID] > 0 {
				return MailTask{}, false, nil
			}
			return m.store.claimMailTaskInLane(ctx, accountID, now, lane)
		},
	}
}

func (m *SyncManager) runMailTasks(ctx context.Context, accountID string, wake, stop <-chan struct{}) {
	runner := m.taskRunner()
	runner.run(ctx, accountID, wake, stop)
}

// Deterministic drain for maintenance and diagnostics. The long-lived runner
// uses independent lanes instead, so a slow model cannot hold notification work.
func (m *SyncManager) processMailTasks(ctx context.Context, account Account, limit int) error {
	runner := m.taskRunner()
	return runner.process(ctx, account, limit, "")
}

func (m *SyncManager) executeMailTask(ctx context.Context, account Account, task MailTask) (bool, error) {
	if skipped, err := m.skipIgnoredMailTask(ctx, account, task); skipped || err != nil {
		return skipped, err
	}
	aiClient := m.aiClient
	if aiClient == nil {
		aiClient = &http.Client{Timeout: 60 * time.Second}
	}
	switch task.Type {
	case mailTaskAnalysis, mailTaskReply:
		// Disabled/unconfigured tasks are skipped rather than retried forever.
		var cfgErr error
		if task.Type == mailTaskAnalysis {
			_, _, _, cfgErr = resolveMailSummaryMetadata(ctx, m.store, account)
		} else {
			_, cfgErr = resolveReplySuggestionConfig(ctx, m.store, m.key, account)
		}
		if errors.Is(cfgErr, ErrNotFound) || errors.Is(cfgErr, ErrAITaskDisabled) {
			return true, nil
		}
		if cfgErr != nil {
			return false, cfgErr
		}
		if err := m.store.recordMailTaskPhase(ctx, "body"); err != nil {
			return false, err
		}
		message, err := m.loadMailTaskBody(ctx, account, task)
		if err != nil {
			return false, err
		}
		// Recheck after the network fetch: rules may have changed meanwhile.
		if skipped, err := m.skipIgnoredMailTask(ctx, account, task); skipped || err != nil {
			return skipped, err
		}
		if task.Type == mailTaskAnalysis {
			var result MailSummaryResult
			result, err = GetOrCreateMailSummary(ctx, aiClient, m.store, m.key, account, message, false)
			if err == nil {
				err = m.store.recordMailTaskCacheResult(ctx, result.Cached, result.Stale, result.Record)
			}
		} else {
			var result ReplySuggestionResult
			result, err = GetOrCreateReplySuggestion(ctx, aiClient, m.store, m.key, account, message, false)
			if err == nil {
				err = m.store.recordMailTaskCacheResult(ctx, result.Cached, false, result.Record)
			}
		}
		// The mailbox setting may have changed while its body was loading.
		if errors.Is(err, ErrAITaskDisabled) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		m.store.PublishAccountEvent(ctx, account.ID, events.Event{Kind: events.KindConversationUpdated, Folder: task.FolderName, MessageID: task.UID})
		return false, nil
	case mailTaskNotification:
		if err := m.store.recordMailTaskPhase(ctx, "delivery"); err != nil {
			return false, err
		}
		cfg, err := m.store.GetAccountWebhookSettings(ctx, account.OwnerID, account.ID)
		if err != nil {
			return false, err
		}
		if !cfg.Enabled || cfg.URL == "" {
			return true, nil
		}
		message, err := m.store.GetMessage(ctx, account.ID, task.FolderName, task.UID)
		if err != nil {
			return false, err
		}
		payload := task.Payload
		if payload == "" {
			summary := ""
			record, err := m.store.GetMessageSummary(ctx, task.MessageSummaryKey)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return false, err
			}
			if err == nil && record.Status == "ready" {
				summary = record.Summary
			}
			if strings.TrimSpace(summary) == "" {
				subject := strings.TrimSpace(message.Subject)
				if subject == "" {
					subject = "（无主题）"
				}
				summary = "收到新邮件：" + subject + "\nAI 分析暂未完成，请打开邮箱查看。"
			}
			payload = feishuMailText(message, summary)
			// Freeze before sending: delivery retries do not change notification
			// content or create a second notification after AI later succeeds.
			if err := m.store.saveMailTaskPayload(ctx, task, payload); err != nil {
				return false, err
			}
		}
		client := m.webhookClient
		if client == nil {
			client = &http.Client{Timeout: 10 * time.Second}
		}
		// Also applies to retries whose notification payload is already frozen.
		if skipped, err := m.skipIgnoredMailTask(ctx, account, task); skipped || err != nil {
			return skipped, err
		}
		return false, sendFeishuText(ctx, client, cfg.URL, payload)
	default:
		return false, permanentMailTaskError("configuration", fmt.Errorf("unknown mail task type %q", task.Type))
	}
}

func mailIgnoreReason(match MailIgnoreMatch) string { return "命中忽略规则：" + match.RuleName }

func (m *SyncManager) skipIgnoredMailTask(ctx context.Context, account Account, task MailTask) (bool, error) {
	rules, err := m.store.GetMailIgnoreRules(ctx, account.OwnerID, account.ID)
	if err != nil {
		return false, err
	}
	message, err := loadMailIgnoreHeaders(ctx, m.store.db, account.ID, task.FolderName, task.UID)
	if err != nil {
		return false, err
	}
	match := MatchMailIgnoreRule(rules, message, task.Type)
	if match == nil {
		return false, nil
	}
	// Keep the reason separate from errors, and preserve the attempt journal.
	tx, err := m.store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE mail_tasks SET skip_reason=? WHERE account_id=? AND folder_name=? AND uid=? AND task_type=? AND generation_token=? AND status='running'`, mailIgnoreReason(*match), task.AccountID, task.FolderName, task.UID, task.Type, task.Token)
	if err := checkMailTaskUpdate(result, err); err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE mail_task_runs SET phase='ignore_rule',context_json=json_set(context_json,'$.ignoreRule',json(?)) WHERE lease_token=? AND status='running'`, marshalJSON(match, "{}"), task.Token)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (m *SyncManager) loadMailTaskBody(ctx context.Context, account Account, task MailTask) (models.Email, error) {
	message, err := m.store.GetMessage(ctx, account.ID, task.FolderName, task.UID)
	if err != nil {
		return models.Email{}, err
	}
	if message.BodyCached {
		return message, nil
	}
	if strings.EqualFold(account.AuthType, "oauth2") {
		return models.Email{}, permanentMailTaskError("configuration", errors.New("background OAuth2 body retrieval is not available"))
	}
	var password string
	if err := mailapi.DecryptJSON(account.EncryptedPassword, &password, m.key); err != nil {
		return models.Email{}, permanentMailTaskError("configuration", fmt.Errorf("decrypt mail credentials: %w", err))
	}
	client, err := mailapi.NewClientTLS(account.IMAPServer, account.IMAPPort, account.Username, password, account.IMAPTLS)
	if err != nil {
		return models.Email{}, err
	}
	defer client.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
		case <-done:
		}
	}()
	fetched, err := client.FetchSingleMessage(task.FolderName, task.UID)
	if err != nil {
		return models.Email{}, err
	}
	fetched.Folder = task.FolderName
	fetched.AttachmentMetadataCached = true
	if err := m.store.UpsertMessages(ctx, account.ID, task.FolderName, []models.Email{fetched}); err != nil {
		return models.Email{}, err
	}
	return m.store.GetMessage(ctx, account.ID, task.FolderName, task.UID)
}
