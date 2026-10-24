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
		if errors.Is(cfgErr, ErrNotFound) {
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
		return false, sendFeishuText(ctx, client, cfg.URL, payload)
	default:
		return false, permanentMailTaskError("configuration", fmt.Errorf("unknown mail task type %q", task.Type))
	}
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
