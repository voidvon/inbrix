package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// Regression for conversation 18165b8dc2349bb232fedef5: the gateway sometimes
// omits the opening calls tag while retaining complete invoke/parameter tags.
const dsmlReportedCalls = `<｜｜DSML｜｜ invoke name="list_documents">
<｜｜DSML｜｜ parameter name="type" string="true">quotation</｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>
<｜｜DSML｜｜ invoke name="list_documents">
<｜｜DSML｜｜ parameter name="query" string="true">MAKTO</｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>
</｜｜DSML｜｜ calls>`

func TestDSMLToolCalls(t *testing.T) {
	for _, prefix := range []string{"", "<｜｜DSML｜｜ calls>\n"} {
		calls, err := parseToolCallsChecked(prefix + dsmlReportedCalls)
		want := []AIToolCall{
			{Name: "list_documents", Args: map[string]any{"type": "quotation"}},
			{Name: "list_documents", Args: map[string]any{"query": "MAKTO"}},
		}
		if err != nil || !reflect.DeepEqual(calls, want) {
			t.Fatalf("reported calls: %+v, %v", calls, err)
		}
	}
	input := `<｜｜DSML｜｜ invoke name='calculate_pricing'>
<｜｜DSML｜｜ parameter string='false' name='items'>[{"qty":3,"price":220}]</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="tax_rate" string="false">0</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="enabled" string="false">true</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="optional" string="false">null</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="notes" string="true"> 中英 & <quoted> "text" &amp; </｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="empty" string="true"></｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>`
	calls, err := parseToolCallsChecked(input)
	if err != nil || len(calls) != 1 {
		t.Fatalf("typed arguments: %+v, %v", calls, err)
	}
	wantArgs := map[string]any{"items": []any{map[string]any{"qty": float64(3), "price": float64(220)}}, "tax_rate": float64(0), "enabled": true, "optional": nil, "notes": ` 中英 & <quoted> "text" &amp; `, "empty": ""}
	if !reflect.DeepEqual(calls[0].Args, wantArgs) {
		t.Fatalf("argument corruption: %#v", calls[0].Args)
	}
	codeParameter := strings.Replace(input, ` 中英 & <quoted> "text" &amp; `, "```go\nfmt.Println(1)\n```", 1)
	calls, err = parseToolCallsChecked(codeParameter)
	if err != nil || len(calls) != 1 || calls[0].Args["notes"] != "```go\nfmt.Println(1)\n```" {
		t.Fatalf("code fence in argument corrupted the call: %+v, %v", calls, err)
	}
	mixed := "```tool_call\n{\"name\":\"search_products\",\"args\":{}}\n```\n" + dsmlReportedCalls
	calls, err = parseToolCallsChecked(mixed)
	if err != nil || len(calls) != 3 || calls[0].Name != "search_products" || calls[2].Args["query"] != "MAKTO" {
		t.Fatalf("mixed protocol order: %+v, %v", calls, err)
	}
}

func TestDSMLRejectsMalformedTurns(t *testing.T) {
	valid := `<｜｜DSML｜｜ invoke name="list_documents"></｜｜DSML｜｜ invoke>`
	for _, raw := range []string{
		`<｜｜DSM`,
		`</｜｜DSML｜`,
		`<｜｜DSML｜｜ calls>`,
		`<｜｜DSML｜｜ invoke name="list_documents">`,
		`<｜｜DSML｜｜ invoke></｜｜DSML｜｜ invoke>`,
		`<｜｜DSML｜｜ invoke name="list_documents" name="read_email"></｜｜DSML｜｜ invoke>`,
		`<｜｜DSML｜｜ invoke name="list_documents"><｜｜DSML｜｜ parameter name="query" string="true">partial`,
		strings.Replace(dsmlReportedCalls, `string="true"`, `string="maybe"`, 1),
		strings.Replace(dsmlReportedCalls, `string="true"`, `string="false"`, 1),
		strings.Replace(dsmlReportedCalls, `name="type"`, `other="type"`, 1),
		strings.Replace(dsmlReportedCalls, `name="type"`, `name=""`, 1),
		strings.Replace(dsmlReportedCalls, `</｜｜DSML｜｜ invoke>`, `unexpected</｜｜DSML｜｜ invoke>`, 1),
		`<｜｜DSML｜｜ invoke name="list_documents"><｜｜DSML｜｜ parameter name="q" string="true">a</｜｜DSML｜｜ parameter><｜｜DSML｜｜ parameter name="q" string="true">b</｜｜DSML｜｜ parameter></｜｜DSML｜｜ invoke>`,
		valid + `<｜｜DSML｜｜ invoke name="read_email">`,
	} {
		calls, err := parseToolCallsChecked(raw)
		if err == nil || len(calls) != 0 {
			t.Errorf("malformed turn accepted: %q -> %+v, %v", raw, calls, err)
		}
	}
}

func TestDSMLStreamingAndHistoryDoNotLeakToolPayloads(t *testing.T) {
	for _, raw := range []string{
		"正在查询。\n<｜｜DSML｜｜ calls>\n" + dsmlReportedCalls + "\n已完成。",
		"```xml\n" + dsmlReportedCalls + "\n```\n已完成。",
		"正在查询。\n<｜｜DSML｜｜ invoke name=\"list_documents\"><｜｜DSML｜｜ parameter name=\"query\" string=\"true\">secret",
	} {
		previous := ""
		for n := 1; n <= len(raw); n++ {
			visible := agentVisibleText(raw[:n])
			if !strings.HasPrefix(visible, previous) {
				t.Fatalf("non-monotonic stream at %d: %q -> %q", n, previous, visible)
			}
			for _, secret := range []string{"DSML", "list_documents", "quotation", "MAKTO", "secret", "｜", "\ufffd"} {
				if strings.Contains(visible, secret) {
					t.Fatalf("tool payload leaked at %d: %q", n, visible)
				}
			}
			previous = visible
		}
		if strings.Contains(raw, "已完成。") && !strings.Contains(previous, "已完成。") {
			t.Fatalf("ordinary text after calls was lost: %q", previous)
		}
	}
	if cleaned := stripResidualToolCalls(dsmlReportedCalls); cleaned != "" {
		t.Fatalf("residual DSML: %q", cleaned)
	}
	transcript := buildConversationTranscript([]AIAgentChatMessage{
		{Role: "assistant", Content: dsmlReportedCalls},
		{Role: "user", Content: "Please draft a reply"},
	})
	if strings.Contains(transcript, "DSML") || !strings.Contains(transcript, "Please draft a reply") {
		t.Fatalf("contaminated history: %q", transcript)
	}
}

func TestAgentChatContinuesAfterDSMLTools(t *testing.T) {
	for _, provider := range []string{"openai", "openai-compatible"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", provider, stream), func(t *testing.T) {
				h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
				model, err := h.mailDB.GetDefaultAIModel(context.Background(), account.OwnerID)
				if err != nil {
					t.Fatal(err)
				}
				model.Provider = provider
				if _, err := h.mailDB.UpdateAIModel(context.Background(), model); err != nil {
					t.Fatal(err)
				}
				var turns atomic.Int32
				h.client = &http.Client{Transport: agentStreamTransport(func(r *http.Request) (*http.Response, error) {
					var request struct {
						Input    string `json:"input"`
						Stream   bool   `json:"stream"`
						Messages []struct {
							Content string `json:"content"`
						} `json:"messages"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					for _, message := range request.Messages {
						request.Input += message.Content
					}
					if strings.Contains(request.Input, dsmlMarker) {
						t.Error("historical DSML sent back to provider")
					}
					text := dsmlReportedCalls
					if turns.Add(1) > 1 {
						if !strings.Contains(request.Input, "[Tool Result (list_documents)]") {
							t.Error("missing tool results in follow-up")
						}
						text = "Dear customer, please review the attached quotation."
					}
					contentType := "application/json"
					body, _ := json.Marshal(map[string]string{"output_text": text})
					if provider == "openai-compatible" {
						// Match ClinePass's nested Chat Completions envelope.
						body, _ = json.Marshal(map[string]any{"data": map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}, "finish_reason": "stop"}}}})
					}
					if request.Stream {
						contentType = "text/event-stream"
						var wire strings.Builder
						for _, char := range text {
							frame, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": string(char)})
							if provider == "openai-compatible" {
								frame, _ = json.Marshal(map[string]any{"data": map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": string(char)}}}}})
							}
							fmt.Fprintf(&wire, "data: %s\n\n", frame)
						}
						if provider == "openai-compatible" {
							wire.WriteString("data: {\"data\":{\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}}\n\ndata: [DONE]\n\n")
						} else {
							wire.WriteString("data: {\"type\":\"response.completed\"}\n\n")
						}
						body = []byte(wire.String())
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
				})}
				payload, _ := json.Marshal(AIAgentChatRequest{AccountEmail: account.Email, Messages: []AIAgentChatMessage{
					{Role: "assistant", Content: dsmlReportedCalls},
					{Role: "user", Content: "Please draft a reply"},
				}})
				req := httptest.NewRequest(http.MethodPost, "/api/ai/agent-chat", strings.NewReader(string(payload)))
				req.Header.Set("Content-Type", "application/json")
				if stream {
					req.Header.Set("Accept", "text/event-stream")
				}
				resp, err := app.Test(req, 10_000)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				raw, _ := io.ReadAll(resp.Body)
				var result AIAgentChatResponse
				if stream {
					err = readDiagnosticAgentEvents(raw, &result)
				} else {
					err = json.Unmarshal(raw, &result)
				}
				if err != nil || resp.StatusCode != 200 || turns.Load() != 2 || len(result.ToolCalls) != 2 || !strings.Contains(result.Content, "attached quotation") {
					t.Fatalf("agent failed to continue: turns=%d response=%s err=%v", turns.Load(), raw, err)
				}
				if strings.Contains(string(raw), "DSML") {
					t.Fatalf("tool protocol leaked in response: %s", raw)
				}
				for _, call := range result.ToolCalls {
					if call.Name != "list_documents" || call.Status != "success" {
						t.Fatalf("tool did not execute: %+v", call)
					}
				}
			})
		}
	}
}

func readDiagnosticAgentEvents(raw []byte, result *AIAgentChatResponse) error {
	completed := false
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event agentChatEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			return err
		}
		if event.Type == "error" {
			return fmt.Errorf("stream error: %s", event.Error)
		}
		if event.Type == "done" && event.Response != nil {
			*result = *event.Response
			completed = true
		}
	}
	if !completed {
		return fmt.Errorf("missing done event")
	}
	return nil
}

func TestMalformedDSMLIsRecordedAsError(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string {
				return `<｜｜DSML｜｜ invoke name="list_documents">`
			})
			// The fixture uses :memory:, whose schema belongs to one SQLite connection.
			// Error logging also launches a background cleanup query.
			h.mailDB.DB().SetMaxOpenConns(1)
			payload, _ := json.Marshal(AIAgentChatRequest{AccountEmail: account.Email, Messages: []AIAgentChatMessage{{Role: "user", Content: "draft a reply"}}})
			req := httptest.NewRequest(http.MethodPost, "/api/ai/agent-chat", strings.NewReader(string(payload)))
			req.Header.Set("Content-Type", "application/json")
			if stream {
				req.Header.Set("Accept", "text/event-stream")
			}
			resp, err := app.Test(req, 10_000)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(raw), dsmlMarker) || strings.Contains(string(raw), `"type":"done"`) {
				t.Fatalf("malformed tool reply treated as success: %s", raw)
			}
			if stream && !strings.Contains(string(raw), `"type":"error"`) || !stream && resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("missing error response: %d %s", resp.StatusCode, raw)
			}
			logs, err := h.mailDB.ListAIErrorLogs(context.Background(), account.OwnerID, 10)
			if err != nil || len(logs) != 1 || logs[0].TaskType != "agent_chat" {
				t.Fatalf("missing error record: %+v, %v", logs, err)
			}
		})
	}
}

func TestDSMLAtToolBudgetDoesNotClaimSuccess(t *testing.T) {
	_, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string {
		return dsmlReportedCalls
	})
	status, _, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, Messages: []AIAgentChatMessage{{Role: "user", Content: "draft a reply"}}})
	if status != http.StatusUnprocessableEntity || strings.Contains(raw, dsmlMarker) || !strings.Contains(raw, "AI 未完成处理") {
		t.Fatalf("unexecuted synthesis tools treated as success: %d %s", status, raw)
	}
}

func TestDSMLChatReadModel(t *testing.T) {
	input := []map[string]any{
		{"role": "user", "content": dsmlReportedCalls},
		{"role": "assistant", "content": dsmlReportedCalls, "document": map[string]string{"id": "saved-document"}, "parts": []map[string]any{{"type": "tool", "tool": map[string]string{"name": "read_email"}}, {"type": "text", "content": dsmlReportedCalls}}},
		{"role": "assistant", "content": "正常正文" + dsmlReportedCalls + "后续文字"},
	}
	encoded, _ := json.Marshal(input)
	var messages []any
	_ = json.Unmarshal(encoded, &messages)
	sanitizeDSMLChatMessages(messages)
	if messages[0].(map[string]any)["content"] != dsmlReportedCalls {
		t.Fatal("user input changed")
	}
	assistant := messages[1].(map[string]any)
	encoded, _ = json.Marshal(assistant)
	if strings.Contains(string(encoded), "DSML") || !strings.Contains(string(encoded), "incomplete") || !strings.Contains(string(encoded), "saved-document") || !strings.Contains(string(encoded), "read_email") {
		t.Fatalf("invalid historical read model: %s", encoded)
	}
	// Whitespace between removed invocations is retained, but all prose must survive.
	content := messages[2].(map[string]any)["content"].(string)
	if strings.Join(strings.Fields(content), "") != "正常正文后续文字" {
		t.Fatalf("ordinary prose lost: %q", content)
	}
}
