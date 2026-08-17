package mailstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	mailapi "lilmail/handlers/api"
	"lilmail/models"
)

const (
	maxWebhookResponseBytes = 64 << 10
	maxOpenAIResponseBytes  = 2 << 20
)

const inquiryOutputRules = `最高优先级输出规则：
1. 只输出一个合法的 JSON 对象，不要输出 Markdown、代码块或任何解释。
2. JSON 必须且只能包含 company、country、products、requirements、question 五个字符串字段。
3. 字段内容使用简体中文；产品型号、公司名、人名和国家名可保留原文。不得推测邮件中没有的信息。
4. company 是客户公司名称，country 是客户国家，缺失时填空字符串。
5. products 只写询价产品，可包含型号和数量；缺失时填“未提及”。
6. requirements 用一句话概括交期、技术参数、认证、价格或其他要求，最多 80 个字；缺失时填“未提及具体要求”。
7. question 只写客户明确提出的问题，最多 80 个字；没有问题时必须填空字符串。
输出示例：{"company":"Acme GmbH","country":"德国","products":"10 台 FT14 浮球式蒸汽疏水阀","requirements":"报价需包含交期、运费及 CE 认证资料。","question":"该型号是否适用于 16 bar 工况？"}`

var (
	markdownLinkPattern = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)]+)\)`)
	htmlTagPattern      = regexp.MustCompile(`<[^>]+>`)
)

// ValidateFeishuWebhookURL permits only the official bot-webhook endpoints.
// Besides catching configuration mistakes, this prevents a logged-in user from
// turning the synchronizer into a request primitive against an internal host.
func ValidateFeishuWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("webhook URL must be an official Feishu HTTPS URL")
	}
	host := strings.ToLower(u.Hostname())
	if (host != "open.feishu.cn" && host != "open.larksuite.com") || u.Port() != "" {
		return errors.New("webhook URL host must be open.feishu.cn or open.larksuite.com")
	}
	if !strings.HasPrefix(u.EscapedPath(), "/open-apis/bot/v2/hook/") || strings.TrimPrefix(u.EscapedPath(), "/open-apis/bot/v2/hook/") == "" {
		return errors.New("webhook URL must be a Feishu custom bot URL")
	}
	return nil
}

func (s *Store) GetWebhookSettings(ctx context.Context, ownerID string) (WebhookSettings, error) {
	var cfg WebhookSettings
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT enabled, url FROM webhook_settings WHERE owner_id = ?`, ownerID).Scan(&enabled, &cfg.URL)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return WebhookSettings{}, fmt.Errorf("mailstore: get webhook settings: %w", err)
	}
	cfg.Enabled = intBool(enabled)
	return cfg, nil
}

func (s *Store) SaveWebhookSettings(ctx context.Context, ownerID string, cfg WebhookSettings) error {
	ownerID = strings.TrimSpace(ownerID)
	cfg.URL = strings.TrimSpace(cfg.URL)
	if ownerID == "" {
		return errors.New("mailstore: webhook owner is required")
	}
	if cfg.URL != "" {
		if err := ValidateFeishuWebhookURL(cfg.URL); err != nil {
			return err
		}
	}
	if cfg.Enabled && cfg.URL == "" {
		return errors.New("an enabled webhook needs a URL")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_settings(owner_id, enabled, url, updated_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(owner_id) DO UPDATE SET enabled=excluded.enabled, url=excluded.url, updated_at=excluded.updated_at`,
		ownerID, boolInt(cfg.Enabled), cfg.URL, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("mailstore: save webhook settings: %w", err)
	}
	return nil
}

func (m *SyncManager) notifyNewMessages(ctx context.Context, mailClient *mailapi.Client, account Account, messages []models.Email) {
	if len(messages) == 0 {
		return
	}
	cfg, err := m.store.GetWebhookSettings(ctx, account.OwnerID)
	if err != nil {
		log.Printf("mail webhook: load settings for account %s: %v", account.ID, err)
		return
	}
	if !cfg.Enabled || cfg.URL == "" {
		return
	}
	webhookClient := &http.Client{Timeout: 10 * time.Second}
	aiClient := &http.Client{Timeout: 60 * time.Second}
	for _, message := range messages {
		fullMessage, err := m.store.GetMessage(ctx, account.ID, "INBOX", message.ID)
		if err != nil {
			log.Printf("mail webhook: load full message %s/%s: %v", account.ID, message.ID, err)
			continue
		}
		if !fullMessage.BodyCached && mailClient != nil {
			fetched, fetchErr := mailClient.FetchSingleMessage("INBOX", message.ID)
			if fetchErr != nil {
				log.Printf("mail webhook: fetch full message %s/%s: %v", account.ID, message.ID, fetchErr)
				continue
			}
			if err := m.store.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{fetched}); err != nil {
				log.Printf("mail webhook: cache full message %s/%s: %v", account.ID, message.ID, err)
				continue
			}
			fullMessage = fetched
		}
		fullMessage.Folder = "INBOX"
		if !fullMessage.BodyCached {
			log.Printf("mail webhook: skip message %s/%s because full body is unavailable", account.ID, message.ID)
			continue
		}
		result, err := GetOrCreateMailSummary(ctx, aiClient, m.store, m.key, account, fullMessage, false)
		if err != nil {
			log.Printf("mail webhook: analyze account %s message %s: %v", account.ID, message.ID, err)
			continue
		}
		if err := sendFeishuWebhook(ctx, webhookClient, cfg.URL, account, fullMessage, result.Record.Summary); err != nil {
			log.Printf("mail webhook: notify account %s message %s: %v", account.ID, message.ID, err)
		}
	}
}

func sendFeishuWebhook(ctx context.Context, client HTTPClient, webhookURL string, account Account, message models.Email, summary string) error {
	from := strings.TrimSpace(message.From)
	if name := strings.TrimSpace(message.FromName); name != "" {
		if from != "" {
			from = fmt.Sprintf("%s <%s>", name, from)
		} else {
			from = name
		}
	}
	if from == "" {
		from = "（未知发件人）"
	}
	subject := strings.TrimSpace(message.Subject)
	if subject == "" {
		subject = "（无主题）"
	}
	text := fmt.Sprintf("📬 新邮件\n邮箱：%s\n发件人：%s\n主题：%s", account.Email, from, subject)
	if !message.Date.IsZero() {
		text += "\n时间：" + message.Date.Local().Format("2006-01-02 15:04")
	}
	text += "\n\n🤖 Spirax Sarco 询价分析\n" + strings.TrimSpace(summary)
	return sendFeishuText(ctx, client, webhookURL, text)
}

func (m *SyncManager) summarizeInquiryForWebhook(ctx context.Context, client HTTPClient, account Account, message models.Email) (string, error) {
	return SummarizeMail(ctx, client, m.store, m.key, account, message)
}

func createOpenAIWebhookResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input, effort string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":             model.Model,
		"instructions":      instructions,
		"input":             input,
		"max_output_tokens": 500,
		"reasoning":         map[string]string{"effort": effort},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(model.BaseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OpenAI request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxOpenAIResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read OpenAI response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error.Message != "" {
			return "", fmt.Errorf("OpenAI returned HTTP %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		return "", fmt.Errorf("OpenAI returned HTTP %d", resp.StatusCode)
	}
	return openAIResponseText(raw)
}

func stripMarkdown(text string) string {
	text = markdownLinkPattern.ReplaceAllString(text, "$1（$2）")
	text = htmlTagPattern.ReplaceAllString(text, "")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || line == "---" || line == "***" || line == "___" {
			continue
		}
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"> ", "- ", "* ", "+ "} {
			if strings.HasPrefix(line, prefix) {
				line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
				break
			}
		}
		line = stripOrderedListMarker(line)
		line = strings.NewReplacer("**", "", "__", "", "~~", "", "`", "").Replace(line)
		if line == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func stripOrderedListMarker(line string) string {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(line) && (line[i] == '.' || line[i] == ')') && line[i+1] == ' ' {
		return strings.TrimSpace(line[i+2:])
	}
	return line
}

func isSimplifiedChineseDominant(text string) bool {
	var han, latin int
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			han++
		} else if unicode.Is(unicode.Latin, r) {
			latin++
		}
	}
	if han < 8 || han*2 < latin {
		return false
	}
	return !strings.ContainsAny(text, "體為與個這還後發應務產專聯絡數壓溫閥號項報價風險議確")
}

func fullEmailForInquiryAnalysis(account Account, message models.Email) string {
	content := strings.TrimSpace(message.Body)
	if content == "" {
		content = strings.TrimSpace(message.HTML)
	}
	var attachments []string
	for _, attachment := range message.Attachments {
		if name := strings.TrimSpace(attachment.Filename); name != "" {
			attachments = append(attachments, name)
		}
	}
	return fmt.Sprintf("邮箱：%s\n发件人：%s\n收件人：%s\n抄送：%s\n主题：%s\n时间：%s\n附件：%s\n\n完整正文：\n%s",
		account.Email, message.From, message.To, message.Cc, message.Subject, message.Date.Format(time.RFC3339), strings.Join(attachments, "、"), content)
}

func openAIResponseText(raw []byte) (string, error) {
	var result struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", errors.New("OpenAI returned invalid JSON")
	}
	if text := strings.TrimSpace(result.OutputText); text != "" {
		return text, nil
	}
	var parts []string
	for _, item := range result.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, strings.TrimSpace(content.Text))
			}
		}
	}
	if len(parts) == 0 {
		return "", errors.New("OpenAI returned no analysis text")
	}
	return strings.Join(parts, "\n"), nil
}

// SendFeishuTestWebhook sends an immediate diagnostic message without changing
// the saved webhook settings.
func SendFeishuTestWebhook(ctx context.Context, webhookURL string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	text := "✅ LilMail 飞书 Webhook 测试成功\n发送时间：" + time.Now().Format("2006-01-02 15:04:05")
	return sendFeishuText(ctx, client, webhookURL, text)
}

func sendFeishuText(ctx context.Context, client HTTPClient, webhookURL, text string) error {
	if err := ValidateFeishuWebhookURL(webhookURL); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}{MsgType: "text", Content: struct {
		Text string `json:"text"`
	}{Text: text}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWebhookResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Feishu returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Code       *int   `json:"code"`
		StatusCode *int   `json:"StatusCode"`
		Message    string `json:"msg"`
	}
	if len(body) > 0 && json.Unmarshal(body, &result) == nil {
		if result.Code != nil && *result.Code != 0 {
			return fmt.Errorf("Feishu returned code %d: %s", *result.Code, result.Message)
		}
		if result.StatusCode != nil && *result.StatusCode != 0 {
			return fmt.Errorf("Feishu returned status %d", *result.StatusCode)
		}
	}
	return nil
}
