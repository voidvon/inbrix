package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"inbrix/models"
)

type blockingReplySuggestionClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   int
	mu      sync.Mutex
}

func (c *blockingReplySuggestionClient) Do(*http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	c.once.Do(func() { close(c.started) })
	<-c.release
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"output_text":"The shared generated reply."}`)),
		Header:     make(http.Header),
	}, nil
}

func (c *blockingReplySuggestionClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestReplySuggestionPersistsAndRegenerates(t *testing.T) {
	s, account, message, encryptionKey := setupMailSummaryTest(t)
	ctx := context.Background()
	model, err := s.GetDefaultAIModel(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: account.OwnerID, Name: "Reply writer", Prompt: "Keep replies concise and professional."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAITaskBinding(ctx, account.OwnerID, AITaskBindingRecord{AccountID: account.ID, TaskType: ReplySuggestionTask, AgentID: agent.ID, ModelID: model.ID}); err != nil {
		t.Fatal(err)
	}
	client := &recordingWebhookClient{status: http.StatusOK, body: `{"output_text":"Thanks for the details. I will review the request and get back to you shortly."}`}

	first, err := GetOrCreateReplySuggestion(ctx, client, s, encryptionKey, account, message, false)
	if err != nil {
		t.Fatalf("first suggestion: %v", err)
	}
	if first.Cached || !strings.Contains(first.Record.Summary, "Thanks for the details") {
		t.Fatalf("unexpected first suggestion: %+v", first)
	}
	var request struct {
		Instructions string `json:"instructions"`
		Input        string `json:"input"`
	}
	if err := json.Unmarshal(client.data, &request); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request.Instructions, agent.Prompt) || !strings.Contains(request.Input, message.Body) {
		t.Fatalf("reply configuration was not applied: %+v", request)
	}

	client.body = `{"output_text":"Thank you. I will send the requested quotation tomorrow."}`
	second, err := GetOrCreateReplySuggestion(ctx, client, s, encryptionKey, account, message, true)
	if err != nil {
		t.Fatalf("regenerate suggestion: %v", err)
	}
	if second.Cached || !strings.Contains(second.Record.Summary, "quotation tomorrow") {
		t.Fatalf("suggestion was not replaced: %+v", second)
	}

	saved, err := s.GetMessageReplySuggestion(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID})
	if err != nil || saved.Summary != second.Record.Summary || saved.Status != "ready" {
		t.Fatalf("saved suggestion: %+v, %v", saved, err)
	}
}

func TestNewMessageProcessingPersistsSuggestionWithoutWebhook(t *testing.T) {
	s, account, message, encryptionKey := setupMailSummaryTest(t)
	client := &recordingWebhookClient{status: http.StatusOK, body: `{"output_text":"I can confirm the requested quantity."}`}
	manager := &SyncManager{store: s, key: encryptionKey, aiClient: client}

	if err := s.UpsertIncomingMessages(context.Background(), account.ID, "INBOX", []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	if err := manager.processMailTasks(context.Background(), account, 3); err != nil {
		t.Fatal(err)
	}

	saved, err := s.GetMessageReplySuggestion(context.Background(), MessageSummaryKey{AccountID: account.ID, FolderName: "INBOX", UID: message.ID})
	if err != nil || saved.Status != "ready" || saved.Summary != "I can confirm the requested quantity." {
		t.Fatalf("background suggestion: %+v, %v", saved, err)
	}
}

func TestDisabledReplySuggestionDoesNotGenerateOrLogErrors(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	model, err := s.GetDefaultAIModel(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.GetMailSummaryAgent(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	binding := AITaskBindingRecord{AccountID: account.ID, TaskType: ReplySuggestionTask, AgentID: agent.ID, ModelID: model.ID, EnabledSet: true}
	if _, err := s.SaveAITaskBinding(ctx, account.OwnerID, binding); err != nil {
		t.Fatal(err)
	}
	client := &recordingWebhookClient{status: http.StatusOK, body: `{"output_text":"A reply."}`}
	for _, regenerate := range []bool{false, true} {
		if _, err := GetOrCreateReplySuggestion(ctx, client, s, key, account, message, regenerate); !errors.Is(err, ErrAITaskDisabled) {
			t.Fatalf("regenerate=%v: expected disabled, got %v", regenerate, err)
		}
	}
	// Already queued work must be skipped even if the email body is unavailable.
	analysisBinding := binding
	analysisBinding.TaskType = MailSummaryTask
	if _, err := s.SaveAITaskBinding(ctx, account.OwnerID, analysisBinding); err != nil {
		t.Fatal(err)
	}
	manager := &SyncManager{store: s, key: key, aiClient: client}
	if _, err := s.db.ExecContext(ctx, `UPDATE messages SET body_cached=0 WHERE account_id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.enqueueMailTasks(ctx, account.ID, message.Folder, []string{message.ID}); err != nil {
		t.Fatal(err)
	}
	runner := manager.taskRunner()
	if err := runner.process(ctx, account, 3, mailTaskLaneAI); err != nil {
		t.Fatal(err)
	}
	task, err := s.GetMailTask(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}, mailTaskReply)
	if err != nil || task.Status != "skipped" {
		t.Fatalf("disabled queued task: %+v, %v", task, err)
	}
	if client.req != nil {
		t.Fatal("disabled suggestion made a model request")
	}
	if _, err := s.GetMessageReplySuggestion(ctx, task.MessageSummaryKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled suggestion created a generation record: %v", err)
	}
	logs, err := s.ListAIErrorLogs(ctx, account.OwnerID, 100)
	if err != nil || len(logs) != 0 {
		t.Fatalf("disabled suggestion logged errors: %+v, %v", logs, err)
	}
	binding.Enabled = true
	if _, err := s.SaveAITaskBinding(ctx, account.OwnerID, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := GetOrCreateReplySuggestion(ctx, client, s, key, account, message, false); err != nil {
		t.Fatalf("re-enabled suggestion failed: %v", err)
	}
}

func TestReplySuggestionConfigurationFailureStillLogsError(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	model, err := s.GetDefaultAIModel(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAIModel(ctx, account.OwnerID, model.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetOrCreateReplySuggestion(ctx, nil, s, key, account, message, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing configuration, got %v", err)
	}
	logs, err := s.ListAIErrorLogs(ctx, account.OwnerID, 100)
	if err != nil || len(logs) != 1 || logs[0].TaskType != ReplySuggestionTask {
		t.Fatalf("missing configuration was not logged: %+v, %v", logs, err)
	}
}

func TestReplySuggestionRegenerationJoinsGenerationInProgress(t *testing.T) {
	s, account, message, encryptionKey := setupMailSummaryTest(t)
	client := &blockingReplySuggestionClient{started: make(chan struct{}), release: make(chan struct{})}
	backgroundResult := make(chan error, 1)
	go func() {
		_, err := GetOrCreateReplySuggestion(context.Background(), client, s, encryptionKey, account, message, false)
		backgroundResult <- err
	}()

	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("background generation did not start")
	}
	manualResult := make(chan error, 1)
	go func() {
		_, err := GetOrCreateReplySuggestion(context.Background(), client, s, encryptionKey, account, message, true)
		manualResult <- err
	}()
	// Let the manual request observe and join the active generation lease before
	// the model response is released.
	time.Sleep(50 * time.Millisecond)
	close(client.release)

	for name, result := range map[string]<-chan error{"background": backgroundResult, "manual": manualResult} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("%s generation: %v", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s generation did not finish", name)
		}
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("expected concurrent generation to share one model request, got %d", calls)
	}
}
