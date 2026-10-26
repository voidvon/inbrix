package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"inbrix/internal/agentruntime"
	"inbrix/mailstore"
)

func TestStopAICancelsSilentProviderAndReleasesSession(t *testing.T) {
	for _, accept := range []string{"text/event-stream", "application/json", "write-document"} {
		t.Run(accept, func(t *testing.T) {
			h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
			app.Post("/api/ai/stop", h.HandleStopAI)
			app.Post("/api/ai/write-document", h.HandleWriteDocument)
			started, cancelled := make(chan struct{}), make(chan struct{})
			h.client = &http.Client{Transport: agentStreamTransport(func(r *http.Request) (*http.Response, error) {
				close(started)
				<-r.Context().Done()
				close(cancelled)
				return nil, r.Context().Err()
			})}
			ctx := context.Background()
			session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "Stop test", "quotation")
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go func() { _ = app.Listener(listener) }()
			defer app.Shutdown()
			base := "http://" + listener.Addr().String()
			version := session.Version
			input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "silent-provider", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "Generate a quotation"}}}
			body, _ := json.Marshal(input)
			path := "/api/ai/agent-chat"
			if accept == "write-document" {
				path = "/api/ai/write-document"
				body, _ = json.Marshal(aiDocumentInput{AccountEmail: account.Email, RequestID: input.RequestID, DocumentType: "quotation", Mode: "rewrite", Instruction: "Rewrite", CurrentHTML: "<p>Original document</p>"})
			}
			request, _ := http.NewRequest(http.MethodPost, base+path, strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", accept)
			client := &http.Client{Timeout: 5 * time.Second}
			finished := make(chan string, 1)
			go func() {
				response, err := client.Do(request)
				if err != nil {
					finished <- err.Error()
					return
				}
				defer response.Body.Close()
				raw, _ := io.ReadAll(response.Body)
				finished <- string(raw)
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("provider did not start")
			}
			stopInput := map[string]string{"accountEmail": account.Email, "requestId": input.RequestID}
			if accept != "write-document" {
				stopInput["sessionId"] = session.ID
			}
			stopBody, _ := json.Marshal(stopInput)
			stopRequest, _ := http.NewRequest(http.MethodPost, base+"/api/ai/stop", strings.NewReader(string(stopBody)))
			stopRequest.Header.Set("Content-Type", "application/json")
			before := time.Now()
			response, err := client.Do(stopRequest)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 204 || time.Since(before) > time.Second {
				t.Fatalf("stop did not acknowledge promptly: status=%d duration=%s", response.StatusCode, time.Since(before))
			}
			select {
			case <-cancelled:
			default:
				t.Fatal("upstream still running after stop acknowledgement")
			}
			select {
			case raw := <-finished:
				if strings.Contains(raw, `"type":"done"`) {
					t.Fatalf("cancelled turn completed: %s", raw)
				}
			case <-time.After(time.Second):
				t.Fatal("generation HTTP request did not finish")
			}
			saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
			if accept == "write-document" {
				if err != nil || saved.LastRun != nil || len(saved.Messages) != 0 {
					t.Fatalf("cancelled document created chat output: %+v %v", saved, err)
				}
			} else if err != nil || saved.LastRun == nil || saved.LastRun.Status != "failed" || saved.LastRun.Retryable || len(saved.Messages) != 1 {
				t.Fatalf("cancelled run retained a lease, retry input or assistant message: %+v %v", saved, err)
			}
		})
	}
}

func TestStopAICancelsRunningToolBeforeNextCall(t *testing.T) {
	var registry aiRequestRegistry
	key := aiRequestKey{"owner", "account", "session", "tool-request"}
	ctx, finish, err := registry.begin(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := agentruntime.Run(ctx, agentruntime.Options{
			Input: "read mail", Limits: agentruntime.Limits{Turns: 3, CallsPerTurn: 2, Calls: 4, InputBytes: 16000, ResultBytes: 2000},
			Respond: func(ctx context.Context, input string) (string, error) {
				return "```tool_call\n[{\"name\":\"read_email\",\"args\":{\"uid\":\"1\"}},{\"name\":\"read_email\",\"args\":{\"uid\":\"2\"}}]\n```", nil
			},
			Execute: func(ctx context.Context, call agentruntime.ToolCall) (any, string, error) {
				if call.Args["uid"] != "1" {
					t.Error("executed the next tool after stop")
					return nil, "", context.Canceled
				}
				close(started)
				<-ctx.Done()
				return nil, "", ctx.Err()
			},
		})
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}
	if _, err := registry.stop(key); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("tool execution not cancelled: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tool continued after stop")
	}
}

func TestStopAIBeforeStartAndAccountIsolation(t *testing.T) {
	h, app, account, other := agentContextTestApp(t, func(agentTestRequest) string { t.Error("stopped request reached provider"); return "unexpected" })
	app.Post("/api/ai/stop", h.HandleStopAI)
	stop := func(email, requestID string, outsider bool) int {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"accountEmail": email, "requestId": requestID})
		req := httptest.NewRequest(http.MethodPost, "/api/ai/stop", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		if outsider {
			req.Header.Set("X-Test-Outsider", "1")
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if status := stop(account.Email, "early-stop", true); status != 404 {
		t.Fatalf("outsider stopped another account: %d", status)
	}
	if status := stop(other.Email, "early-stop", false); status != 404 {
		t.Fatalf("owner accessed another account: %d", status)
	}
	if status := stop(account.Email, "early-stop", false); status != 204 {
		t.Fatalf("early stop: %d", status)
	}
	status, _, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, RequestID: "early-stop", Messages: []AIAgentChatMessage{{Role: "user", Content: "Write a reply"}}})
	if status != 409 {
		t.Fatalf("stopped request ran later: %d %s", status, raw)
	}
	if status := stop(account.Email, "early-stop", false); status != 204 {
		t.Fatalf("repeated stop is not idempotent: %d", status)
	}
}

func TestStopAIOnlyCancelsTargetSessionAndRejectsLateCompletion(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	app.Post("/api/ai/stop", h.HandleStopAI)
	ctx := context.Background()
	var turns []mailstore.AIChatTurn
	for range 2 {
		session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "Test", "quotation")
		if err != nil {
			t.Fatal(err)
		}
		turn, err := h.mailDB.BeginAIChatTurn(ctx, account.OwnerID, account.ID, session.ID, "shared-request-id", "hash", session.Version, json.RawMessage(`{"role":"user","content":"Generate"}`), mailstore.AIBusinessContext{}, mailstore.AIChatTurnOptions{Request: json.RawMessage(`{"request":"snapshot"}`)})
		if err != nil {
			t.Fatal(err)
		}
		turns = append(turns, turn)
	}
	body, _ := json.Marshal(map[string]string{"accountEmail": account.Email, "sessionId": turns[0].SessionID, "requestId": turns[0].RequestID})
	req := httptest.NewRequest(http.MethodPost, "/api/ai/stop", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatalf("stop: %d", response.StatusCode)
	}
	for i, turn := range turns {
		saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, turn.SessionID)
		if err != nil || saved.LastRun == nil {
			t.Fatalf("missing run: %+v %v", saved, err)
		}
		if i == 0 {
			if saved.LastRun.Status != "failed" || saved.LastRun.Retryable {
				t.Fatal("target run was not cancelled")
			}
		} else if saved.LastRun.Status != "pending" {
			t.Fatal("stopped a different session sharing the same request id")
		}
	}
	_, err = h.mailDB.FinishAIChatTurn(ctx, turns[0], json.RawMessage(`{"role":"assistant","content":"late"}`), func(string, int64) (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
	if !errors.Is(err, mailstore.ErrAIChatConflict) {
		t.Fatalf("late completion was accepted: %v", err)
	}
}
