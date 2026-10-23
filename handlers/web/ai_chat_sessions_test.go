package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"inbrix/mailstore"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentSessionPersistsAndReplaysWithoutModelCall(t *testing.T) {
	calls := 0
	h, app, account, other := agentContextTestApp(t, func(r agentTestRequest) string {
		calls++
		if calls == 2 && !strings.Contains(r.Input, "first question") {
			t.Error("server history missing")
		}
		return "stored answer"
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	version := int64(0)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "request-1", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "first question"}}}
	status, res, raw := postAgentContext(t, app, input)
	if status != 200 || res.Version != 2 || res.UserMessageID == "" || res.AssistantMessageID == "" {
		t.Fatalf("first: %d %s", status, raw)
	}
	saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if err != nil || len(saved.Messages) != 2 || !strings.Contains(string(saved.Messages[1]), "stored answer") {
		t.Fatalf("result not durable: %+v %v", saved, err)
	}
	status, replayed, raw := postAgentContext(t, app, input)
	if status != 200 || calls != 1 || replayed.AssistantMessageID != res.AssistantMessageID {
		t.Fatalf("retry called model: %d %d %s", status, calls, raw)
	}
	// Replays also work over SSE and contain the already committed response.
	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/api/ai/agent-chat", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := app.Test(req, 10000)
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if calls != 1 || !strings.Contains(string(stream), `"type":"done"`) || !strings.Contains(string(stream), res.AssistantMessageID) {
		t.Fatalf("SSE replay: %s", stream)
	}
	input.RequestID = "request-2"
	input.Messages[0].Content = "follow up"
	status, _, _ = postAgentContext(t, app, input)
	if status != 409 || calls != 1 {
		t.Fatal("stale tab was not rejected", status, calls)
	}
	version = 2
	status, res, raw = postAgentContext(t, app, input)
	if status != 200 || calls != 2 || res.Version != 4 {
		t.Fatalf("follow-up: %d %s", status, raw)
	}
	input.AccountEmail = other.Email
	status, _, _ = postAgentContext(t, app, input)
	if status != 404 {
		t.Fatal("cross-mailbox session used", status)
	}
	if _, e := h.mailDB.ListAIChatSessions(ctx, account.OwnerID, account.ID); e != nil {
		t.Fatal("direct list", e)
	}
	app.Get("/api/ai/chat-sessions", h.HandleAIChatSessions)
	app.Patch("/api/ai/chat-sessions/:id", h.HandleAIChatSession)
	app.Delete("/api/ai/chat-sessions/:id", h.HandleAIChatSession)
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/ai/chat-sessions?accountEmail="+account.Email, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Sessions []mailstore.AIChatSession `json:"sessions"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&list); err != nil || len(list.Sessions) != 1 || len(list.Sessions[0].Messages) != 0 {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestAgentSessionFailureRetainsOneUserForRetry(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "answer" })
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal("create", err)
	}
	// A transport failure occurs after Begin, so the accepted user remains durable.
	original := h.client.Transport
	h.client.Transport = sessionTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"offline"}`))}, nil
	})
	version := int64(0)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "retry-failure", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "question"}}}
	status, _, raw := postAgentContext(t, app, input)
	if status == 200 {
		t.Fatal("expected failure", raw)
	}
	saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if err != nil {
		t.Fatal("read failed turn", err)
	}
	if len(saved.Messages) != 1 || saved.LastRun.Status != "failed" {
		t.Fatalf("failure lost accepted message or lease: %+v", saved)
	}
	h.client.Transport = original
	status, _, raw = postAgentContext(t, app, input)
	if status != 200 {
		t.Fatalf("retry failed: %d %s", status, raw)
	}
	saved, _ = h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if len(saved.Messages) != 2 {
		t.Fatal("retry duplicated the user message")
	}
}

type sessionTestTransport func(*http.Request) (*http.Response, error)

func (f sessionTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAgentSessionSSEPersistsBeforeDone(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "streamed saved answer" })
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	version := int64(0)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "stream-first", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "question"}}}
	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/api/ai/agent-chat", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := app.Test(req, 10000)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if err != nil || len(saved.Messages) != 2 || saved.LastRun.Status != "complete" {
		t.Fatalf("done without persisted result: %+v %v, %s", saved, err, raw)
	}
	var message struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(saved.Messages[1], &message)
	if !strings.Contains(string(raw), `"type":"done"`) || !strings.Contains(string(raw), `"assistantMessageId":"`+message.ID+`"`) {
		t.Fatalf("SSE failed to reconcile committed message: %s", raw)
	}
}

// A new browser only needs mailbox/session/run identity; unsent editor and
// focused-mail inputs are recovered from the server, not reconstructed by UI.
func TestAgentSessionRetryAfterReloadUsesSavedInput(t *testing.T) {
	h, app, account, other := agentContextTestApp(t, func(r agentTestRequest) string {
		if !strings.Contains(r.Instructions, "Original draft") {
			t.Error("lost original document snapshot")
		}
		return "recovered"
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	seedAgentSessionReply(t, h, account, session.ID, 0, map[string]any{"role": "assistant", "document": &AIDocumentCard{Type: "quotation", Title: "Original draft"}})
	transport := h.client.Transport
	h.client.Transport = sessionTestTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	version := int64(2)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "durable-retry", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "question"}}, ActiveDocument: &AIAgentChatDocContext{Type: "quotation", Title: "Forged client draft"}}
	status, _, _ := postAgentContext(t, app, input)
	if status == 200 {
		t.Fatal("expected failure")
	}
	saved, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if saved.LastRun == nil || !saved.LastRun.Retryable {
		t.Fatal("failed run is not recoverable")
	}
	h.client.Transport = transport
	retry := AIAgentChatRequest{AccountEmail: other.Email, SessionID: session.ID, RetryRequestID: input.RequestID}
	status, _, _ = postAgentContext(t, app, retry)
	if status != 404 {
		t.Fatalf("cross-mailbox retry: %d", status)
	}
	retry.AccountEmail = account.Email
	status, res, raw := postAgentContext(t, app, retry)
	if status != 200 || res.RequestID != input.RequestID {
		t.Fatalf("retry: %d %s", status, raw)
	}
	saved, _ = h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if len(saved.Messages) != 4 || saved.LastRun.Retryable {
		t.Fatalf("retry duplicated message or stayed retryable: %+v", saved)
	}
	status, replay, _ := postAgentContext(t, app, retry)
	if status != 200 || replay.AssistantMessageID != res.AssistantMessageID {
		t.Fatal("retry response was not replayed")
	}
}

func TestAgentSessionHistoricalImagesDoNotBlockText(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "answer" })
	h.client.Transport = sessionTestTransport(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(body["input"])
		if count := strings.Count(string(raw), `"type":"input_image"`); count != 24 {
			t.Errorf("selected %d images, want 24", count)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output_text":"answer"}`))}, nil
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	img, err := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "test.png", "image/png", chatTestPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		msg, _ := json.Marshal(AIAgentChatMessage{Role: "user", Content: "images", Attachments: []mailstore.AIChatAttachment{img, img, img, img, img, img}})
		key := fmt.Sprint(i)
		turn, e := h.mailDB.BeginAIChatTurn(ctx, account.OwnerID, account.ID, session.ID, key, key, int64(i), msg, mailstore.AIBusinessContext{})
		if e != nil {
			t.Fatal(e)
		}
		if e = h.mailDB.FailAIChatTurn(ctx, turn); e != nil {
			t.Fatal(e)
		}
	}
	version := int64(5)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "text", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "only text"}}}
	status, _, raw := postAgentContext(t, app, input)
	if status != 200 {
		t.Fatalf("old images blocked text: %d %s", status, raw)
	}
	saved, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if len(saved.Messages) != 7 {
		t.Fatal("context selection changed saved history")
	}
}

func TestAgentSessionOversizedCurrentImagesFailBeforeCommit(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { t.Error("called model for invalid turn"); return "" })
	ctx := context.Background()
	session, _ := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	// The store owns image size. A fake client size must not bypass preflight.
	img, err := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "test.png", "image/png", make([]byte, 9<<20))
	if err != nil {
		t.Fatal(err)
	}
	img.Size = 1
	version := int64(0)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "large", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "images", Attachments: []mailstore.AIChatAttachment{img, img, img, img}}}}
	status, _, raw := postAgentContext(t, app, input)
	if status != 413 {
		t.Fatalf("expected 413: %d %s", status, raw)
	}
	saved, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if saved.Version != 0 || len(saved.Messages) != 0 || saved.LastRun != nil {
		t.Fatal("rejected turn mutated session")
	}
}

func TestAgentSessionTextModelImagePreflight(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "text reply" })
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	base, err := h.mailDB.GetDefaultAIModel(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	text, err := h.mailDB.CreateAIModel(ctx, mailstore.AIModelRecord{OwnerID: account.OwnerID, Provider: "deepseek", BaseURL: base.BaseURL, Model: "deepseek-chat", EncryptedAPIKey: base.EncryptedAPIKey})
	if err != nil {
		t.Fatal(err)
	}
	img, err := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "test.png", "image/png", chatTestPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	version := int64(0)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "image", Version: &version, ModelID: text.ID, Messages: []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{img}}}}
	status, _, raw := postAgentContext(t, app, input)
	if status != 400 {
		t.Fatalf("unsupported image: %d %s", status, raw)
	}
	saved, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if saved.Version != 0 || len(saved.Messages) != 0 {
		t.Fatal("unsupported turn was persisted")
	}
}
