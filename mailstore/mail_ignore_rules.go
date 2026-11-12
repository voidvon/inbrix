package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	mailapi "inbrix/handlers/api"
	"inbrix/models"
)

// Rules affect background work only. Explicit user requests remain available.
type MailIgnoreCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type MailIgnoreRule struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Enabled    bool                  `json:"enabled"`
	Conditions []MailIgnoreCondition `json:"conditions"`
	Actions    []string              `json:"actions"`
}

type MailIgnoreMatch struct {
	RuleID   string `json:"ruleId"`
	RuleName string `json:"ruleName"`
	Action   string `json:"action"`
}

type MailIgnorePreviewMessage struct {
	Folder    string            `json:"folder"`
	MessageID string            `json:"messageId"`
	From      string            `json:"from"`
	Subject   string            `json:"subject"`
	Date      time.Time         `json:"date"`
	Matches   []MailIgnoreMatch `json:"matches"`
}

type MailIgnorePreview struct {
	Total    int                        `json:"total"`
	Scanned  int                        `json:"scanned"`
	Messages []MailIgnorePreviewMessage `json:"messages"`
}

var ErrInvalidMailIgnoreRules = errors.New("invalid mail ignore rules")
var ignoreDomainRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func DefaultMailIgnoreRules() []MailIgnoreRule {
	return []MailIgnoreRule{{ID: "tencent-interception-notice", Name: "腾讯企业邮箱拦截通知", Enabled: true,
		Conditions: []MailIgnoreCondition{{Field: "subject", Operator: "equals", Value: "系统邮件拦截通知"}, {Field: "message_id_domain", Operator: "equals", Value: "exmail.weixin.qq.com"}},
		Actions:    []string{mailTaskAnalysis, mailTaskReply, mailTaskNotification}}}
}

// Each rule is an AND of conditions; matching rules are combined by action.
// Domain matching is exact, so example.com never matches notexample.com.
func MatchMailIgnoreRule(rules []MailIgnoreRule, message models.Email, action string) *MailIgnoreMatch {
	for _, rule := range rules {
		if !rule.Enabled || len(rule.Conditions) == 0 || !slices.Contains(rule.Actions, action) {
			continue
		}
		matched := true
		for _, condition := range rule.Conditions {
			value := ""
			switch condition.Field {
			case "sender", "sender_domain":
				address, err := mail.ParseAddress(strings.TrimSpace(message.From))
				if err == nil {
					value = strings.ToLower(address.Address)
					if condition.Field == "sender_domain" {
						value = value[strings.LastIndex(value, "@")+1:]
					}
				}
			case "subject":
				value = strings.TrimSpace(mailapi.DecodeMIMEHeader(message.Subject))
			case "message_id_domain":
				id := strings.TrimSpace(message.MessageID)
				if strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") {
					id = id[1 : len(id)-1]
				}
				if at := strings.LastIndex(id, "@"); at > 0 && !strings.ContainsAny(id, " \t\r\n<>") {
					value = strings.ToLower(id[at+1:])
				}
			}
			if value == "" || (condition.Operator == "equals" && !strings.EqualFold(value, condition.Value)) ||
				(condition.Operator == "contains" && !strings.Contains(strings.ToLower(value), strings.ToLower(condition.Value))) ||
				(condition.Operator != "equals" && condition.Operator != "contains") {
				matched = false
				break
			}
		}
		if matched {
			return &MailIgnoreMatch{RuleID: rule.ID, RuleName: rule.Name, Action: action}
		}
	}
	return nil
}

func NormalizeMailIgnoreRules(rules []MailIgnoreRule) ([]MailIgnoreRule, error) {
	bad := func(message string) ([]MailIgnoreRule, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMailIgnoreRules, message)
	}
	if len(rules) > 50 {
		return bad("at most 50 rules")
	}
	result := make([]MailIgnoreRule, 0, len(rules))
	seen := map[string]bool{}
	for _, source := range rules {
		rule := source
		rule.ID, rule.Name = strings.TrimSpace(rule.ID), strings.TrimSpace(rule.Name)
		if rule.ID == "" {
			var err error
			rule.ID, err = newID("ignore")
			if err != nil {
				return nil, err
			}
		}
		if len(rule.ID) > 100 || seen[rule.ID] || strings.ContainsAny(rule.ID, "\r\n\x00") {
			return bad("invalid or duplicate rule ID")
		}
		seen[rule.ID] = true
		if utf8.RuneCountInString(rule.Name) < 1 || utf8.RuneCountInString(rule.Name) > 80 || strings.ContainsAny(rule.Name, "\r\n\x00") {
			return bad("rule name must be 1–80 characters")
		}
		if len(rule.Conditions) < 1 || len(rule.Conditions) > 8 {
			return bad("each rule needs 1–8 conditions")
		}
		if len(rule.Actions) < 1 || len(rule.Actions) > 3 {
			return bad("choose at least one action")
		}
		rule.Conditions = slices.Clone(source.Conditions)
		rule.Actions = slices.Clone(source.Actions)
		slices.Sort(rule.Actions)
		for i, action := range rule.Actions {
			if (action != mailTaskAnalysis && action != mailTaskReply && action != mailTaskNotification) || (i > 0 && rule.Actions[i-1] == action) {
				return bad("invalid or duplicate action")
			}
		}
		for i := range rule.Conditions {
			c := &rule.Conditions[i]
			c.Value = strings.TrimSpace(c.Value)
			if c.Value == "" || utf8.RuneCountInString(c.Value) > 300 || strings.ContainsAny(c.Value, "\r\n\x00") {
				return bad("condition value must be 1–300 characters")
			}
			if c.Operator != "equals" && !(c.Field == "subject" && c.Operator == "contains") {
				return bad("only subjects support contains; other fields require equals")
			}
			switch c.Field {
			case "subject":
			case "sender":
				address, err := mail.ParseAddress(c.Value)
				if err != nil || !strings.Contains(address.Address, "@") {
					return bad("invalid sender address")
				}
				c.Value = strings.ToLower(address.Address)
			case "sender_domain", "message_id_domain":
				c.Value = strings.ToLower(c.Value)
				if len(c.Value) > 253 || !ignoreDomainRE.MatchString(c.Value) {
					return bad("invalid domain; enter a domain without @ or wildcards")
				}
			default:
				return bad("unknown condition field")
			}
		}
		result = append(result, rule)
	}
	return result, nil
}

type mailIgnoreQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadMailIgnoreHeaders(ctx context.Context, q mailIgnoreQuerier, accountID, folder string, uid any) (models.Email, error) {
	var message models.Email
	err := q.QueryRowContext(ctx, `SELECT from_addr,subject,message_id FROM messages WHERE account_id=? AND folder_name=? AND uid=?`, accountID, folder, uid).Scan(&message.From, &message.Subject, &message.MessageID)
	if errors.Is(err, sql.ErrNoRows) {
		return message, ErrNotFound
	}
	return message, err
}

func loadMailIgnoreRules(ctx context.Context, q mailIgnoreQuerier, accountID string) ([]MailIgnoreRule, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT rules_json FROM account_mail_ignore_rules WHERE account_id=?`, accountID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultMailIgnoreRules(), nil
	}
	if err != nil {
		return nil, err
	}
	var rules []MailIgnoreRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func (s *Store) GetMailIgnoreRules(ctx context.Context, ownerID, accountID string) ([]MailIgnoreRule, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	return loadMailIgnoreRules(ctx, s.db, accountID)
}

func (s *Store) SaveMailIgnoreRules(ctx context.Context, ownerID, accountID string, rules []MailIgnoreRule) ([]MailIgnoreRule, error) {
	if _, err := s.GetMailIgnoreRules(ctx, ownerID, accountID); err != nil {
		return nil, err
	}
	normalized, err := NormalizeMailIgnoreRules(rules)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO account_mail_ignore_rules(account_id,rules_json,updated_at) VALUES(?,?,?) ON CONFLICT(account_id) DO UPDATE SET rules_json=excluded.rules_json,updated_at=excluded.updated_at`, accountID, string(raw), time.Now().Unix())
	return normalized, err
}

// Preview only cached INBOX headers: no IMAP, AI call, or notification delivery.
func (s *Store) PreviewMailIgnoreRules(ctx context.Context, ownerID, accountID string, rules []MailIgnoreRule) (MailIgnorePreview, error) {
	preview := MailIgnorePreview{Messages: []MailIgnorePreviewMessage{}}
	if _, err := s.GetMailIgnoreRules(ctx, ownerID, accountID); err != nil {
		return preview, err
	}
	rules, err := NormalizeMailIgnoreRules(rules)
	if err != nil {
		return preview, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT folder_name,uid,from_addr,subject,message_id,date_unix FROM messages WHERE account_id=? AND lower(folder_name)='inbox' ORDER BY date_unix DESC,uid DESC`, accountID)
	if err != nil {
		return preview, err
	}
	defer rows.Close()
	for rows.Next() {
		var message models.Email
		var uid, date int64
		if err := rows.Scan(&message.Folder, &uid, &message.From, &message.Subject, &message.MessageID, &date); err != nil {
			return preview, err
		}
		preview.Scanned++
		matches := []MailIgnoreMatch{}
		for _, action := range []string{mailTaskAnalysis, mailTaskReply, mailTaskNotification} {
			if match := MatchMailIgnoreRule(rules, message, action); match != nil {
				matches = append(matches, *match)
			}
		}
		if len(matches) == 0 {
			continue
		}
		preview.Total++
		if len(preview.Messages) < 20 {
			preview.Messages = append(preview.Messages, MailIgnorePreviewMessage{Folder: message.Folder, MessageID: fmt.Sprint(uid), From: message.From, Subject: mailapi.DecodeMIMEHeader(message.Subject), Date: timeFromUnix(date), Matches: matches})
		}
	}
	return preview, rows.Err()
}

func (s *Store) ensureMailTaskSkipReasonColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(mail_tasks)`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "skip_reason" {
			has = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || has {
		return err
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE mail_tasks ADD COLUMN skip_reason TEXT NOT NULL DEFAULT ''`)
	return err
}
