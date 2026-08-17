package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	mailapi "lilmail/handlers/api"
	"lilmail/models"
)

type summaryTestClient struct {
	mu       sync.Mutex
	calls    int
	block    <-chan struct{}
	entered  chan<- struct{}
	failCall int
	inputs   []string
}

func (c *summaryTestClient) Do(req *http.Request) (*http.Response, error) {
	var request struct {
		Input string `json:"input"`
	}
	if raw, err := io.ReadAll(req.Body); err == nil {
		_ = json.Unmarshal(raw, &request)
	}
	c.mu.Lock()
	c.calls++
	call := c.calls
	c.inputs = append(c.inputs, request.Input)
	c.mu.Unlock()
	if c.entered != nil {
		select {
		case c.entered <- struct{}{}:
		default:
		}
	}
	if c.block != nil {
		<-c.block
	}
	if call == c.failCall {
		return nil, errors.New("model unavailable")
	}
	body := fmt.Sprintf(`{"output_text":"{\"客户\":\"测试客户\",\"需求\":\"十台阀门\",\"要求\":\"这是第%d次生成，需确认型号和交期。\",\"问题\":\"\"}"}`, call)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func (c *summaryTestClient) lastInput() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.inputs) == 0 {
		return ""
	}
	return c.inputs[len(c.inputs)-1]
}

func (c *summaryTestClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func setupMailSummaryTest(t *testing.T) (*Store, Account, models.Email, string) {
	t.Helper()
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "summary-owner@example.com", "", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	account := testAccount(t, s, owner.ID, "sales@example.com", true)
	message := models.Email{
		ID: "42", Folder: "INBOX", From: "buyer@example.com", To: account.Email,
		Subject: "Valve RFQ", Body: "Please quote 10 valves", BodyCached: true,
	}
	if err := s.UpsertMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatalf("UpsertMessages: %v", err)
	}
	const encryptionKey = "0123456789abcdef0123456789abcdef"
	encryptedKey, err := mailapi.EncryptJSON("test-api-key", encryptionKey)
	if err != nil {
		t.Fatalf("EncryptJSON: %v", err)
	}
	if _, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: owner.ID, BaseURL: "https://api.openai.com/v1", Model: "gpt-test", ReasoningEffort: "low", EncryptedAPIKey: encryptedKey}); err != nil {
		t.Fatalf("CreateAIModel: %v", err)
	}
	if _, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: owner.ID, Name: "Mail summary", Prompt: "请总结邮件。", Purpose: "mail_summary"}); err != nil {
		t.Fatalf("CreateAIAgent: %v", err)
	}
	return s, account, message, encryptionKey
}

func TestMessageSummaryLeaseAndCascade(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	claim := MessageSummaryRecord{
		MessageSummaryKey: MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID},
		SourceHash:        "source", ConfigHash: "config", ModelID: "model", ModelName: "gpt-test", AgentID: "agent", PipelineVersion: 1,
	}
	first, claimed, err := s.ClaimMessageSummaryGeneration(ctx, claim, false, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	if _, claimed, err := s.ClaimMessageSummaryGeneration(ctx, claim, false, time.Minute); err != nil || claimed {
		t.Fatalf("second claim: claimed=%v err=%v", claimed, err)
	}
	ready, err := s.CompleteMessageSummaryGeneration(ctx, claim, first.GenerationToken, "已保存的总结")
	if err != nil || ready.Status != "ready" || ready.Summary != "已保存的总结" {
		t.Fatalf("complete summary: %+v err=%v", ready, err)
	}
	other := testAccount(t, s, account.OwnerID, "other-summary@example.com", false)
	if err := s.UpsertMessages(ctx, other.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatalf("UpsertMessages other account: %v", err)
	}
	otherKey := MessageSummaryKey{AccountID: other.ID, FolderName: message.Folder, UID: message.ID}
	if _, err := s.GetMessageSummary(ctx, otherKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("summary leaked to another account: %v", err)
	}
	if err := s.DeleteMessage(ctx, account.ID, message.Folder, message.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if _, err := s.GetMessageSummary(ctx, claim.MessageSummaryKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("summary survived message deletion: %v", err)
	}
}

func TestMessageSummaryExpiredLeaseCanBeReclaimed(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	claim := MessageSummaryRecord{MessageSummaryKey: MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}}
	if _, claimed, err := s.ClaimMessageSummaryGeneration(ctx, claim, false, -time.Second); err != nil || !claimed {
		t.Fatalf("expired claim setup: claimed=%v err=%v", claimed, err)
	}
	if _, claimed, err := s.ClaimMessageSummaryGeneration(ctx, claim, false, time.Minute); err != nil || !claimed {
		t.Fatalf("reclaim: claimed=%v err=%v", claimed, err)
	}
}

func TestGetOrCreateMailSummaryCachesAndMarksStale(t *testing.T) {
	s, account, message, encryptionKey := setupMailSummaryTest(t)
	ctx := context.Background()
	client := &summaryTestClient{}

	first, err := GetOrCreateMailSummary(ctx, client, s, encryptionKey, account, message, false)
	if err != nil || first.Cached || first.Record.Status != "ready" {
		t.Fatalf("first summary: %+v err=%v", first, err)
	}
	if first.Record.Summary != "客户：测试客户\n需求：十台阀门\n要求：这是第1次生成，需确认型号和交期。" {
		t.Fatalf("configured fields were not formatted: %q", first.Record.Summary)
	}
	second, err := GetOrCreateMailSummary(ctx, client, s, encryptionKey, account, message, false)
	if err != nil || !second.Cached || second.Stale || second.Record.Summary != first.Record.Summary {
		t.Fatalf("cached summary: %+v err=%v", second, err)
	}
	changed := message
	changed.Body = "Please quote 20 valves"
	stale, err := GetOrCreateMailSummary(ctx, client, s, encryptionKey, account, changed, false)
	if err != nil || !stale.Cached || !stale.Stale {
		t.Fatalf("source-stale summary: %+v err=%v", stale, err)
	}
	if client.callCount() != 1 {
		t.Fatalf("model calls after cache reads: got %d, want 1", client.callCount())
	}

	agent, err := s.GetMailSummaryAgent(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	agent.Prompt = "请用新的格式总结邮件。"
	if _, err := s.UpdateAIAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAIAgent: %v", err)
	}
	stale, err = GetOrCreateMailSummary(ctx, client, s, encryptionKey, account, message, false)
	if err != nil || !stale.Stale || client.callCount() != 1 {
		t.Fatalf("config-stale summary: %+v calls=%d err=%v", stale, client.callCount(), err)
	}
	if _, err := CurrentMailSummaryConfigHash(ctx, s, account); err != nil {
		t.Fatalf("config fingerprint should not decrypt API key: %v", err)
	}
}

func TestGetOrCreateMailSummaryReusesDirectParentSummaryAndStripsQuote(t *testing.T) {
	s, account, message, encryptionKey := setupMailSummaryTest(t)
	ctx := context.Background()

	parent := message
	parent.ID = "41"
	parent.Folder = "Sent Messages"
	parent.MessageID = "<parent@example.com>"
	parent.Body = "Original request"
	if err := s.UpsertMessages(ctx, account.ID, parent.Folder, []models.Email{parent}); err != nil {
		t.Fatal(err)
	}
	claim := MessageSummaryRecord{
		MessageSummaryKey: MessageSummaryKey{AccountID: account.ID, FolderName: parent.Folder, UID: parent.ID},
		SourceHash:        "parent-source", ConfigHash: "parent-config", PipelineVersion: mailSummaryPipelineVersion,
	}
	claimed, acquired, err := s.ClaimMessageSummaryGeneration(ctx, claim, false, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("claim parent summary: acquired=%v err=%v", acquired, err)
	}
	parentSummary := "上一封：不应继续向前嵌套\n客户：Acme GmbH、德国\n需求：10 台阀门\n要求：请确认交期。"
	if _, err := s.CompleteMessageSummaryGeneration(ctx, claim, claimed.GenerationToken, parentSummary); err != nil {
		t.Fatal(err)
	}

	message.InReplyTo = parent.MessageID
	message.References = []string{parent.MessageID}
	message.Body = "Please add CE certificates.\n\nOn Monday, buyer@example.com wrote:\n> Original request"
	if err := s.UpsertMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	client := &summaryTestClient{}
	result, err := GetOrCreateMailSummary(ctx, client, s, encryptionKey, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.Summary != "客户：测试客户\n需求：十台阀门\n要求：这是第1次生成，需确认型号和交期。" {
		t.Fatalf("configured fields were not formatted:\n%s", result.Record.Summary)
	}
	input := client.lastInput()
	if !strings.Contains(input, parentSummary) || !strings.Contains(input, "Please add CE certificates.") || strings.Contains(input, "Original request") || strings.Contains(input, "wrote:") {
		t.Fatalf("model input did not include the parent analysis and isolated current body:\n%s", input)
	}

	currentHash, err := CurrentMailSummarySourceHash(ctx, s, account, message)
	if err != nil || currentHash != result.Record.SourceHash {
		t.Fatalf("source hash mismatch: current=%q saved=%q err=%v", currentHash, result.Record.SourceHash, err)
	}
}

func TestConfiguredOutputLabelsControlPromptAndSummaryOrder(t *testing.T) {
	agent := AIAgentRecord{Prompt: "重点分析风险和下一步行动。", OutputLabels: []string{"行动", "风险"}}
	instructions := mailSummaryInstructions(agent)
	if !strings.Contains(instructions, mailSummarySystemPrompt) || !strings.Contains(instructions, agent.Prompt) || !strings.Contains(instructions, `["行动","风险"]`) {
		t.Fatalf("final instructions did not combine system prompt, agent prompt, and labels:\n%s", instructions)
	}
	got, err := formatTaggedMailSummary(`{"风险":"客户未提供工况参数","行动":"确认压力和温度","额外":"不应接受"}`, agent.OutputLabels)
	if err == nil {
		t.Fatalf("unconfigured field was accepted: %q", got)
	}
	got, err = formatTaggedMailSummary(`{"风险":"客户未提供工况参数","行动":"确认压力和温度"}`, agent.OutputLabels)
	if err != nil {
		t.Fatal(err)
	}
	if want := "行动：确认压力和温度\n风险：客户未提供工况参数"; got != want {
		t.Fatalf("summary order: got %q, want %q", got, want)
	}
	model := AIModelRecord{ID: "model", Model: "gpt-test"}
	before := mailSummaryConfigHash(agent, model, "low")
	agent.OutputLabels = []string{"风险", "行动"}
	if after := mailSummaryConfigHash(agent, model, "low"); before == after {
		t.Fatal("changing output label order did not change the configuration hash")
	}
}

func TestGetOrCreateMailSummarySerializesConcurrentGeneration(t *testing.T) {
	s, account, message, encryptionKey := setupMailSummaryTest(t)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	client := &summaryTestClient{block: release, entered: entered}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := GetOrCreateMailSummary(context.Background(), client, s, encryptionKey, account, message, false)
			results <- err
		}()
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("model call did not start")
	}
	time.Sleep(150 * time.Millisecond)
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent summary: %v", err)
		}
	}
	if client.callCount() != 1 {
		t.Fatalf("concurrent model calls: got %d, want 1", client.callCount())
	}
}

func TestRegenerateMailSummaryReplacesResultAndPreservesItOnFailure(t *testing.T) {
	s, account, message, encryptionKey := setupMailSummaryTest(t)
	ctx := context.Background()
	client := &summaryTestClient{failCall: 3}
	first, err := GetOrCreateMailSummary(ctx, client, s, encryptionKey, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GetOrCreateMailSummary(ctx, client, s, encryptionKey, account, message, true)
	if err != nil || second.Cached || second.Record.Summary == first.Record.Summary {
		t.Fatalf("regenerated summary: %+v err=%v", second, err)
	}
	if _, err := GetOrCreateMailSummary(ctx, client, s, encryptionKey, account, message, true); err == nil {
		t.Fatal("expected regeneration failure")
	}
	saved, err := s.GetMessageSummary(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID})
	if err != nil || saved.Status != "ready" || saved.Summary != second.Record.Summary {
		t.Fatalf("saved result after failed regeneration: %+v err=%v", saved, err)
	}
}
