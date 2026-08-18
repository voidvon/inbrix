package mailstore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	mailapi "inbrix/handlers/api"
	"inbrix/models"
)

func TestWebhookSettingsRoundTripAndOwnerIsolation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	alice := WebhookSettings{Enabled: true, URL: "https://open.feishu.cn/open-apis/bot/v2/hook/alice-secret"}
	if err := s.SaveWebhookSettings(ctx, "alice", alice); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}
	got, err := s.GetWebhookSettings(ctx, "alice")
	if err != nil {
		t.Fatalf("GetWebhookSettings: %v", err)
	}
	if got != alice {
		t.Fatalf("settings mismatch: got %+v, want %+v", got, alice)
	}
	bob, err := s.GetWebhookSettings(ctx, "bob")
	if err != nil {
		t.Fatalf("GetWebhookSettings bob: %v", err)
	}
	if bob.Enabled || bob.URL != "" {
		t.Fatalf("bob can see alice settings: %+v", bob)
	}

	alice.Enabled = false
	if err := s.SaveWebhookSettings(ctx, "alice", alice); err != nil {
		t.Fatalf("disable webhook: %v", err)
	}
	got, _ = s.GetWebhookSettings(ctx, "alice")
	if got.Enabled || got.URL != alice.URL {
		t.Fatalf("disabling should retain URL: %+v", got)
	}
}

func TestValidateFeishuWebhookURL(t *testing.T) {
	valid := []string{
		"https://open.feishu.cn/open-apis/bot/v2/hook/secret",
		"https://open.larksuite.com/open-apis/bot/v2/hook/secret",
	}
	for _, raw := range valid {
		if err := ValidateFeishuWebhookURL(raw); err != nil {
			t.Errorf("valid URL %q: %v", raw, err)
		}
	}
	invalid := []string{
		"http://open.feishu.cn/open-apis/bot/v2/hook/secret",
		"https://127.0.0.1/open-apis/bot/v2/hook/secret",
		"https://open.feishu.cn.evil.test/open-apis/bot/v2/hook/secret",
		"https://open.feishu.cn/open-apis/bot/v2/hook/",
		"https://open.feishu.cn/open-apis/bot/v2/hook/secret?x=1",
	}
	for _, raw := range invalid {
		if err := ValidateFeishuWebhookURL(raw); err == nil {
			t.Errorf("invalid URL accepted: %q", raw)
		}
	}
}

type recordingWebhookClient struct {
	status int
	body   string
	req    *http.Request
	data   []byte
}

func (c *recordingWebhookClient) Do(req *http.Request) (*http.Response, error) {
	c.req = req
	c.data, _ = io.ReadAll(req.Body)
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Header:     make(http.Header),
	}, nil
}

func TestSendFeishuWebhookPayloadAndBusinessError(t *testing.T) {
	client := &recordingWebhookClient{status: http.StatusOK, body: `{"code":0,"msg":"success"}`}
	account := Account{Email: "inbox@example.com"}
	message := models.Email{
		ID:       "42",
		From:     "alice@example.com",
		FromName: "Alice",
		Subject:  "Status update",
		Date:     time.Date(2026, time.August, 14, 9, 30, 0, 0, time.Local),
	}
	summary := "客户：Acme GmbH、德国\n需求：10 件 Spirax Sarco MST21 蒸汽疏水阀\n要求：请确认交期。"
	const webhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/secret"
	if err := sendFeishuWebhook(context.Background(), client, webhookURL, account, message, summary); err != nil {
		t.Fatalf("sendFeishuWebhook: %v", err)
	}
	if client.req == nil || client.req.Method != http.MethodPost || client.req.URL.String() != webhookURL {
		t.Fatalf("unexpected request: %+v", client.req)
	}
	var payload struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(client.data, &payload); err != nil {
		t.Fatalf("decode payload: %v (%s)", err, client.data)
	}
	wantText := "alice@example.com\n客户：Acme GmbH、德国\n需求：10 件 Spirax Sarco MST21 蒸汽疏水阀\n要求：请确认交期。"
	if payload.MsgType != "text" || payload.Content.Text != wantText {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	client.body = `{"code":19001,"msg":"invalid webhook"}`
	if err := sendFeishuWebhook(context.Background(), client, webhookURL, account, message, "分析"); err == nil {
		t.Fatal("Feishu business error was treated as success")
	}
}

func TestSummarizeInquiryForWebhookUsesConfiguredAgentAndFullBody(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	const encryptionKey = "0123456789abcdef0123456789abcdef"
	encryptedKey, err := mailapi.EncryptJSON("test-api-key", encryptionKey)
	if err != nil {
		t.Fatalf("EncryptJSON: %v", err)
	}
	if _, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: "alice", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.6-sol", ReasoningEffort: "low", EncryptedAPIKey: encryptedKey}); err != nil {
		t.Fatalf("CreateAIModel: %v", err)
	}
	createdAgent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: "alice", Name: "询价分析", Prompt: "请使用简体中文纯文本分析完整询价需求。"})
	if err != nil {
		t.Fatalf("CreateAIAgent: %v", err)
	}
	agent, err := s.GetWebhookInquiryAgent(ctx, "alice")
	if err != nil || agent.ID != createdAgent.ID {
		t.Fatalf("GetWebhookInquiryAgent: %+v, %v", agent, err)
	}
	client := &recordingWebhookClient{status: http.StatusOK, body: `{"output_text":"{\"客户\":\"Acme GmbH\",\"需求\":\"十台阀门\",\"要求\":\"确认型号和交期\",\"问题\":\"\"}"}`}
	m := &SyncManager{store: s, key: encryptionKey}
	summary, err := m.summarizeInquiryForWebhook(ctx, client, Account{OwnerID: "alice", Email: "sales@example.com"}, models.Email{From: "buyer@example.com", To: "sales@example.com", Subject: "Valve RFQ", Body: "Please quote 10 valves", BodyCached: true})
	if err != nil {
		t.Fatalf("summarizeInquiryForWebhook: %v", err)
	}
	if summary != "客户：Acme GmbH\n需求：十台阀门\n要求：确认型号和交期" {
		t.Fatalf("summary: %q", summary)
	}
	if got := client.req.Header.Get("Authorization"); got != "Bearer test-api-key" {
		t.Fatalf("authorization: %q", got)
	}
	var request struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		Input        string `json:"input"`
		Reasoning    struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(client.data, &request); err != nil {
		t.Fatalf("decode OpenAI request: %v", err)
	}
	if request.Model != "gpt-5.6-sol" || !strings.Contains(request.Instructions, mailSummarySystemPrompt) || !strings.Contains(request.Instructions, agent.Prompt) || !strings.Contains(request.Instructions, `["客户","需求","要求","问题"]`) || request.Reasoning.Effort != "low" || !strings.Contains(request.Input, "Please quote 10 valves") {
		t.Fatalf("unexpected OpenAI request: %+v", request)
	}
}
