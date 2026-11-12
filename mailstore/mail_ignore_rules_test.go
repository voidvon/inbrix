package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"inbrix/models"
)

func TestMailIgnoreRulesMatchEachMessageAndExactDomains(t *testing.T) {
	rules := DefaultMailIgnoreRules()
	for _, tc := range []struct {
		name, from, subject, id string
		want                    bool
	}{
		{"Tencent without sender", "", "系统邮件拦截通知", "<mis_123@exmail.weixin.qq.com>", true},
		{"encoded subject", "", "=?UTF-8?B?57O757uf6YKu5Lu25oum5oiq6YCa55+l?=", "<mis_123@exmail.weixin.qq.com>", true},
		{"unrelated empty sender", "", "Verificación de facturas y documentos", "<123@grupo-bio.com>", false},
		{"same subject wrong domain", "", "系统邮件拦截通知", "<123@attacker.example>", false},
		{"domain suffix attack", "", "系统邮件拦截通知", "<123@exmail.weixin.qq.com.attacker.example>", false},
		{"malformed message ID", "", "系统邮件拦截通知", "<123@exmail.weixin.qq.com> <456@attacker.example>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []string{mailTaskAnalysis, mailTaskReply, mailTaskNotification} {
				match := MatchMailIgnoreRule(rules, models.Email{From: tc.from, Subject: tc.subject, MessageID: tc.id}, action)
				if (match != nil) != tc.want {
					t.Fatal(action, match, tc.want)
				}
			}
		})
	}
	rules = []MailIgnoreRule{{ID: "sender", Name: "Sender", Enabled: true, Conditions: []MailIgnoreCondition{{Field: "sender_domain", Operator: "equals", Value: "example.com"}}, Actions: []string{mailTaskNotification}}}
	for _, from := range []string{"Buyer <Alice@EXAMPLE.COM>", "alice@example.com"} {
		if MatchMailIgnoreRule(rules, models.Email{From: from}, mailTaskNotification) == nil {
			t.Fatal(from)
		}
	}
	for _, from := range []string{"", "alice@notexample.com", "alice@sub.example.com", "alice@example.com.attacker.net"} {
		if MatchMailIgnoreRule(rules, models.Email{From: from}, mailTaskNotification) != nil {
			t.Fatal(from)
		}
	}
	if MatchMailIgnoreRule(rules, models.Email{From: "alice@example.com"}, mailTaskAnalysis) != nil {
		t.Fatal("notification-only rule affected analysis")
	}
	rules[0].Enabled = false
	if MatchMailIgnoreRule(rules, models.Email{From: "alice@example.com"}, mailTaskNotification) != nil {
		t.Fatal("disabled rule matched")
	}
}

func TestMailIgnoreSettingsValidationOwnershipAndPreview(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	defaults, err := s.GetMailIgnoreRules(ctx, account.OwnerID, account.ID)
	if err != nil || len(defaults) != 1 || !defaults[0].Enabled {
		t.Fatal(defaults, err)
	}
	for _, operation := range []func() error{
		func() error { _, err := s.GetMailIgnoreRules(ctx, "other", account.ID); return err },
		func() error { _, err := s.SaveMailIgnoreRules(ctx, "other", account.ID, nil); return err },
		func() error { _, err := s.PreviewMailIgnoreRules(ctx, "other", account.ID, defaults); return err },
	} {
		if err := operation(); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-owner access", err)
		}
	}
	for _, mutate := range []func(*MailIgnoreRule){
		func(r *MailIgnoreRule) { r.Conditions = nil },
		func(r *MailIgnoreRule) { r.Actions = nil },
		func(r *MailIgnoreRule) { r.Actions = []string{"unknown"} },
		func(r *MailIgnoreRule) { r.Conditions[0].Field = "unknown" },
		func(r *MailIgnoreRule) { r.Conditions[1].Operator = "contains" },
		func(r *MailIgnoreRule) { r.Conditions[1].Value = "*.qq.com" },
		func(r *MailIgnoreRule) { r.Name = "" },
	} {
		rule := DefaultMailIgnoreRules()[0]
		mutate(&rule)
		if _, err := s.SaveMailIgnoreRules(ctx, account.OwnerID, account.ID, []MailIgnoreRule{rule}); !errors.Is(err, ErrInvalidMailIgnoreRules) {
			t.Fatal("invalid rule accepted", rule, err)
		}
	}
	message.ID, message.From, message.Subject, message.MessageID = "43", "", "系统邮件拦截通知", "<mis_1@exmail.weixin.qq.com>"
	if err := s.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewMailIgnoreRules(ctx, account.OwnerID, account.ID, defaults)
	if err != nil || preview.Total != 1 || preview.Scanned != 2 || len(preview.Messages) != 1 || len(preview.Messages[0].Matches) != 3 {
		t.Fatal(preview, err)
	}
	if tasks, err := s.ListMailTasks(ctx, account.ID, "", "", 100); err != nil || len(tasks) != 0 {
		t.Fatal("preview created tasks", tasks, err)
	}
	// Clearing the array is explicit and must survive reopening, without the default returning.
	if _, err := s.SaveMailIgnoreRules(ctx, account.OwnerID, account.ID, []MailIgnoreRule{}); err != nil {
		t.Fatal(err)
	}
	other, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	got, err := other.GetMailIgnoreRules(ctx, account.OwnerID, account.ID)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}

func TestIgnoredIncomingMailSkipsTasksButAllowsManualSummary(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.SaveAccountWebhookSettings(ctx, account.OwnerID, account.ID, WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"}); err != nil {
		t.Fatal(err)
	}
	message.ID, message.From, message.Subject, message.MessageID = "43", "", "系统邮件拦截通知", "<mis_1@exmail.weixin.qq.com>"
	if err := s.UpsertIncomingMessages(ctx, account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListMailTasks(ctx, account.ID, "INBOX", message.ID, 100)
	if err != nil || len(tasks) != 3 {
		t.Fatal(tasks, err)
	}
	for _, task := range tasks {
		if task.Status != "skipped" || !strings.Contains(task.SkipReason, "腾讯") || task.Attempts != 0 || task.ErrorMessage != "" {
			t.Fatal(task)
		}
	}
	ai := &summaryTestClient{}
	delivery := &taskDeliveryClient{}
	m := &SyncManager{store: s, key: key, aiClient: ai, webhookClient: delivery}
	if err := m.processMailTasks(ctx, account, 10); err != nil || ai.callCount() != 0 || delivery.calls != 0 {
		t.Fatal(err, ai.callCount(), delivery.calls)
	}
	if _, err := GetOrCreateMailSummary(ctx, ai, s, key, account, message, false); err != nil || ai.callCount() != 1 {
		t.Fatal("manual summary blocked", err, ai.callCount())
	}
	// Disabling does not replay skipped messages, including a later sync refresh.
	if _, err := s.SaveMailIgnoreRules(ctx, account.OwnerID, account.ID, []MailIgnoreRule{}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := s.ClaimMailTask(ctx, account.ID, time.Now()); err != nil || claimed {
		t.Fatal("skipped tasks replayed", claimed, err)
	}
}

func TestNewIgnoreRulesSkipPendingTasksAndFrozenDeliveryRetries(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.SaveAccountWebhookSettings(ctx, account.OwnerID, account.ID, WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	// Freeze a retry payload, as if the first delivery had failed before a rule was saved.
	if _, err := s.db.Exec(`UPDATE mail_tasks SET payload='frozen notification',attempts=1 WHERE task_type='notification'`); err != nil {
		t.Fatal(err)
	}
	rules := []MailIgnoreRule{{ID: "customer", Name: "Ignored sender", Enabled: true, Conditions: []MailIgnoreCondition{{Field: "sender", Operator: "equals", Value: message.From}}, Actions: []string{mailTaskAnalysis, mailTaskReply, mailTaskNotification}}}
	if _, err := s.SaveMailIgnoreRules(ctx, account.OwnerID, account.ID, rules); err != nil {
		t.Fatal(err)
	}
	ai := &summaryTestClient{}
	delivery := &taskDeliveryClient{}
	m := &SyncManager{store: s, key: key, aiClient: ai, webhookClient: delivery}
	if err := m.processMailTasks(ctx, account, 10); err != nil {
		t.Fatal(err)
	}
	if ai.callCount() != 0 || delivery.calls != 0 {
		t.Fatal("ignored task made external request")
	}
	tasks, _ := s.ListMailTasks(ctx, account.ID, "INBOX", message.ID, 100)
	for _, task := range tasks {
		if task.Status != "skipped" || task.SkipReason == "" {
			t.Fatal(task)
		}
		runs, err := s.ListMailTaskRuns(ctx, account.ID, "INBOX", message.ID, task.Type, 10)
		if err != nil || len(runs) != 1 || runs[0].Status != "skipped" || runs[0].Retryable || runs[0].Context.IgnoreRule == nil || runs[0].Context.IgnoreRule.RuleID != "customer" {
			t.Fatal(runs, err)
		}
	}
}

func TestNotificationRechecksRulesImmediatelyBeforeDelivery(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	if err := s.SaveAccountWebhookSettings(ctx, account.OwnerID, account.ID, WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertIncomingMessages(ctx, account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	rules := []MailIgnoreRule{{ID: "late", Name: "Late notification rule", Enabled: true, Conditions: []MailIgnoreCondition{{Field: "sender", Operator: "equals", Value: message.From}}, Actions: []string{mailTaskNotification}}}
	raw, _ := json.Marshal(rules)
	// A deterministic race: saving the payload makes a new rule visible, after
	// the worker's initial check but before it invokes the HTTP client.
	if _, err := s.db.Exec(`CREATE TRIGGER late_ignore_rule AFTER UPDATE OF payload ON mail_tasks WHEN NEW.task_type='notification' BEGIN INSERT INTO account_mail_ignore_rules(account_id,rules_json,updated_at) VALUES('` + account.ID + `','` + string(raw) + `',0); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE mail_tasks SET status='skipped' WHERE task_type<>'notification'`); err != nil {
		t.Fatal(err)
	}
	delivery := &taskDeliveryClient{}
	m := &SyncManager{store: s, key: key, webhookClient: delivery}
	if err := m.processMailTasks(ctx, account, 5); err != nil || delivery.calls != 0 {
		t.Fatal(err, delivery.calls)
	}
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: "INBOX", UID: message.ID}, mailTaskNotification)
	if err != nil || task.Status != "skipped" || task.Payload == "" || task.SkipReason == "" {
		t.Fatal(task, err)
	}
}
