package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"inbrix/internal/agentruntime"
)

func commandCodeFixture(events ...any) string {
	var out strings.Builder
	for _, event := range events {
		raw, _ := json.Marshal(event)
		fmt.Fprintln(&out, string(raw))
	}
	return out.String()
}

func TestCommandCodeGoTextAndImages(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", streaming), func(t *testing.T) {
			client := nativeAgentClient(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://api.commandcode.ai/alpha/generate" || req.Header.Get("Authorization") != "Bearer fake-key" || req.Header.Get("x-command-code-version") == "" {
					t.Fatal("wrong Command Code endpoint or auth headers")
				}
				var payload struct {
					Config struct{ Date, Environment string } `json:"config"`
					Params struct {
						Model       string           `json:"model"`
						Messages    []map[string]any `json:"messages"`
						System      []map[string]any `json:"system"`
						Temperature float64          `json:"temperature"`
						Effort      string           `json:"reasoning_effort"`
						MaxTokens   int              `json:"max_tokens"`
						Stream      bool             `json:"stream"`
					} `json:"params"`
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				p := payload.Params
				if payload.Config.Date == "" || payload.Config.Environment == "" {
					t.Fatal("missing required CLI environment metadata")
				}
				if p.Model != "deepseek/test" || p.System[0]["text"] != "instructions" || p.Temperature != 0.3 || p.Effort != "low" || p.MaxTokens != 4096 || !p.Stream {
					t.Fatalf("wrong CLI generation params: %+v", p)
				}
				content := p.Messages[0]["content"].([]any)
				if content[2].(map[string]any)["image"] != "data:image/png;base64,YQ==" {
					t.Fatal("lost image input", content)
				}
				wire := commandCodeFixture(map[string]any{"type": "reasoning-delta", "text": "private-reasoning"}, map[string]any{"type": "text-delta", "text": "你好"}, map[string]any{"type": "finish", "finishReason": "stop"})
				if streaming {
					lines := strings.Split(strings.TrimSpace(wire), "\n")
					wire = "data: " + strings.Join(lines, "\r\n\r\ndata: ") + "\r\n\r\n"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			})
			var visible strings.Builder
			var delta func(string) error
			if streaming {
				delta = func(s string) error { visible.WriteString(s); return nil }
			}
			model := AIModelRecord{Provider: "commandcode-go", Model: "deepseek/test", ExtraParams: `{"temperature":0.3}`}
			got, err := CreateAIResponseWithImages(context.Background(), client, model, "fake-key", "instructions", "question", []AIInputImage{{Label: "test image", ContentType: "image/png", Base64: "YQ=="}}, 4096, "low", delta)
			if err != nil || got != "你好" || (streaming && visible.String() != "你好") {
				t.Fatalf("text=%q visible=%q err=%v", got, visible.String(), err)
			}
		})
	}
}

func TestCommandCodeGoNativeToolRoundTrip(t *testing.T) {
	requests, executed := 0, 0
	var visible strings.Builder
	client := nativeAgentClient(func(req *http.Request) (*http.Response, error) {
		requests++
		raw, _ := io.ReadAll(req.Body)
		if requests == 2 {
			for _, part := range []string{`"type":"reasoning"`, "private-continuation", `"type":"tool-call"`, `"type":"tool-result"`, `"toolCallId":"call1"`, `"value":"\"evidence\""`} {
				if !strings.Contains(string(raw), part) {
					t.Errorf("lost native continuation: %s", part)
				}
			}
		}
		wire := commandCodeFixture(map[string]any{"type": "reasoning-delta", "text": "private-continuation"}, map[string]any{"type": "tool-call", "toolCallId": "call1", "toolName": "read_source", "input": map[string]any{"uid": "42"}}, map[string]any{"type": "finish", "finishReason": "tool-calls"})
		if requests == 2 {
			wire = commandCodeFixture(map[string]any{"type": "text-delta", "text": "verified"}, map[string]any{"type": "finish", "finishReason": "stop"})
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}, nil
	})
	out, err := agentruntime.Run(context.Background(), agentruntime.Options{
		Input: "investigate", Tools: []agentruntime.ToolSpec{{Name: "read_source", Parameters: map[string]any{"type": "object"}}},
		Limits: agentruntime.Limits{Turns: 3, CallsPerTurn: 4, Calls: 8, InputBytes: 24000, ResultBytes: 2000},
		Complete: func(ctx context.Context, request agentruntime.ModelRequest) (agentruntime.ModelResponse, error) {
			return CreateAgentResponse(ctx, client, AIModelRecord{Provider: "commandcode-go", Model: "test"}, "fake-key", "instructions", request, nil, 4096, "medium", func(s string) error { visible.WriteString(s); return nil })
		},
		Execute: func(_ context.Context, call agentruntime.ToolCall) (any, string, error) {
			executed++
			if call.ID != "call1" || call.Name != "read_source" || call.Args["uid"] != "42" {
				t.Fatal("corrupt native tool call", call)
			}
			return "evidence", "read", nil
		},
	})
	if err != nil || requests != 2 || executed != 1 || out.Text != "verified" || visible.String() != "verified" {
		t.Fatalf("requests=%d executed=%d text=%q visible=%q err=%v", requests, executed, out.Text, visible.String(), err)
	}
}

func TestCommandCodeGoRejectsIncompleteAndErrorStreams(t *testing.T) {
	cases := []string{
		`{"type":"text-delta","text":"partial"}`,
		`{"type":"tool-call","toolCallId":"call1","toolName":"test","input":{}}`,
		`{"type":"finish","finishReason":"length"}`,
		`{"type":"finish","finishReason":"content-filter"}`,
		`{"type":"error","error":{"message":"refused"}}`,
		`{"type":"abort"}`,
		`{"type":"text-delta","text":"ok"}` + "\n" + `{"type":"finish","finishReason":"stop"}` + "\n" + `{"type":"error","message":"late failure"}`,
		"data: invalid\n\n",
		"data: [DONE]\n\n",
	}
	for _, wire := range cases {
		got, err := readCommandCodeResponse(strings.NewReader(wire), nil)
		if err == nil || got.Text != "" || len(got.ToolCalls) != 0 {
			t.Errorf("partial response accepted: %s (%+v, %v)", wire, got, err)
		}
	}
	sentinel := errors.New("consumer disconnected")
	_, err := readCommandCodeResponse(strings.NewReader(commandCodeFixture(map[string]any{"type": "text-delta", "text": "ok"})), func(string) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("callback cancellation lost: %v", err)
	}
}

func TestCommandCodeGoRequestBoundaries(t *testing.T) {
	client := nativeAgentClient(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Params struct{ Tools []agentruntime.ToolSpec } `json:"params"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Params.Tools) != 1 || body.Params.Tools[0].Name != "submit_result" {
			t.Fatal("tool budget did not restrict available tools", body.Params.Tools)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(commandCodeFixture(map[string]any{"type": "text-delta", "text": "done"}, map[string]any{"type": "finish", "finishReason": "stop"})))}, nil
	})
	_, err := CreateAgentResponse(context.Background(), client, AIModelRecord{Provider: "commandcode-go", Model: "test"}, "fake-key", "instructions", agentruntime.ModelRequest{Messages: []agentruntime.Message{{Role: "user", Text: "test"}}, Tools: []agentruntime.ToolSpec{{Name: "read"}, {Name: "submit_result"}}, DisableTools: true}, nil, 100, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	for _, model := range []AIModelRecord{
		{ExtraParams: `{"system":"override"}`},
		{ExtraParams: `{"messages":[]}`},
		{SupportsImages: &disabled},
	} {
		_, err := commandCodeRequest(model, "instructions", []agentruntime.Message{{Role: "user", Text: "test"}}, nil, []AIInputImage{{ContentType: "image/png", Base64: "YQ=="}}, 100, "")
		if err == nil {
			t.Fatal("invalid request accepted", model)
		}
	}
	_, err = commandCodeRequest(AIModelRecord{}, "", []agentruntime.Message{{Role: "tool", ToolCallID: "unknown", Text: "result"}}, nil, nil, 100, "")
	if err == nil {
		t.Fatal("orphan tool result accepted")
	}
}

func TestCommandCodeGoUpstreamErrors(t *testing.T) {
	client := nativeAgentClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"model not in plan"}}`))}, nil
	})
	_, err := CreateAIResponse(context.Background(), client, AIModelRecord{Provider: "commandcode-go", Model: "test"}, "fake-key", "", "test", 100, "")
	var upstream *UpstreamHTTPError
	if !errors.As(err, &upstream) || upstream.StatusCode != 403 || !strings.Contains(err.Error(), "model not in plan") {
		t.Fatalf("upstream error details lost: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client = nativeAgentClient(func(req *http.Request) (*http.Response, error) { return nil, req.Context().Err() })
	_, err = CreateAIResponse(ctx, client, AIModelRecord{Provider: "commandcode-go", Model: "test"}, "fake-key", "", "test", 100, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("request cancellation lost: %v", err)
	}
}

// This opt-in smoke test sends only synthetic text and one inert echo tool.
// Regular go test runs never call a live provider or read local credentials.
func TestCommandCodeGoLive(t *testing.T) {
	key := os.Getenv("INBRIX_TEST_COMMANDCODE_GO_KEY")
	if key == "" {
		t.Skip("set INBRIX_TEST_COMMANDCODE_GO_KEY to run the live protocol smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	model := AIModelRecord{Provider: "commandcode-go", BaseURL: CommandCodeBaseURL, Model: "deepseek/deepseek-v4.1-flash", ReasoningEffort: "low"}
	var visible strings.Builder
	answer, err := CreateAIResponseStream(ctx, http.DefaultClient, model, key, "Reply with OK only.", "Connection test.", 512, "low", func(s string) error { visible.WriteString(s); return nil })
	if err != nil || !strings.Contains(answer, "OK") || visible.String() != answer {
		t.Fatalf("live text test failed: %v", err)
	}
	request := agentruntime.ModelRequest{Messages: []agentruntime.Message{{Role: "user", Text: "Call bridge_echo with text=TOOL_OK. After receiving the tool result, reply with TOOL_OK only."}}, Tools: []agentruntime.ToolSpec{{Name: "bridge_echo", Description: "Echo the provided text.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]string{"type": "string"}}, "required": []string{"text"}}}}}
	first, err := CreateAgentResponse(ctx, http.DefaultClient, model, key, "Follow the tool protocol test exactly.", request, nil, 1024, "low", nil)
	if err != nil || len(first.Message.ToolCalls) != 1 || first.Message.ToolCalls[0].Name != "bridge_echo" {
		t.Fatalf("live tool call failed: %v", err)
	}
	request.Messages = append(request.Messages, first.Message, agentruntime.Message{Role: "tool", Name: "bridge_echo", ToolCallID: first.Message.ToolCalls[0].ID, Text: "TOOL_OK"})
	last, err := CreateAgentResponse(ctx, http.DefaultClient, model, key, "Follow the tool protocol test exactly.", request, nil, 1024, "low", nil)
	if err != nil || !strings.Contains(last.Message.Text, "TOOL_OK") {
		t.Fatalf("live tool continuation failed: %v", err)
	}
}
