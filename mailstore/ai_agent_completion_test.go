package mailstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"inbrix/internal/agentruntime"
)

type nativeAgentClient func(*http.Request) (*http.Response, error)

func (f nativeAgentClient) Do(req *http.Request) (*http.Response, error) { return f(req) }

func nativeAgentFixture(provider string, final bool) map[string]any {
	text := ""
	if final {
		text = "verified"
	}
	switch provider {
	case "openai":
		items := []any{map[string]any{"type": "reasoning", "id": "reason1", "summary": []any{}, "encrypted_content": "private-continuation"}}
		if final {
			items = append(items, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}})
		} else {
			items = append(items, map[string]any{"type": "function_call", "call_id": "call1", "name": "read_source", "arguments": `{"uid":"42"}`})
		}
		return map[string]any{"status": "completed", "output": items}
	case "gemini":
		parts := []any{map[string]any{"thought": true, "text": "private-thought", "thoughtSignature": "private-continuation"}}
		if final {
			parts = append(parts, map[string]any{"text": text})
		} else {
			parts = append(parts, map[string]any{"functionCall": map[string]any{"id": "call1", "name": "read_source", "args": map[string]any{"uid": "42"}}, "thoughtSignature": "tool-signature"})
		}
		return map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}}}
	default:
		message := map[string]any{"role": "assistant", "content": text, "reasoning_content": "private-continuation"}
		finish := "stop"
		if !final {
			finish = "tool_calls"
			message["tool_calls"] = []any{map[string]any{"id": "call1", "type": "function", "function": map[string]any{"name": "read_source", "arguments": `{"uid":"42"}`}}}
		}
		return map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}}
	}
}

func nativeAgentSSE(provider string, final bool) string {
	var wire strings.Builder
	frame := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(&wire, "data: %s\n\n", b) }
	switch provider {
	case "openai":
		if final {
			frame(map[string]any{"type": "response.output_text.delta", "delta": "verified"})
		}
		frame(map[string]any{"type": "response.completed", "response": nativeAgentFixture(provider, final)})
	case "gemini":
		frame(nativeAgentFixture(provider, final))
	default:
		chunk := func(delta any, finish any) {
			frame(map[string]any{"data": map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}})
		}
		chunk(map[string]any{"reasoning_content": "private-continuation"}, nil)
		if final {
			chunk(map[string]any{"content": "verified"}, "stop")
		} else {
			chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call1", "function": map[string]any{"name": "read_source", "arguments": `{"uid":`}}}}, nil)
			chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": `"42"}`}}}}, "tool_calls")
		}
		wire.WriteString("data: [DONE]\n\n")
	}
	return wire.String()
}

func TestNativeAgentProviderToolRoundTrip(t *testing.T) {
	for _, provider := range []string{"openai", "deepseek", "openai-compatible", "gemini"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", provider, stream), func(t *testing.T) {
				requests, executed := 0, 0
				var visible strings.Builder
				client := nativeAgentClient(func(req *http.Request) (*http.Response, error) {
					requests++
					var payload map[string]any
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if provider == "gemini" {
						if req.Header.Get("x-goog-api-key") != "test-key" {
							t.Fatal("missing Gemini auth")
						}
					} else if req.Header.Get("Authorization") != "Bearer test-key" {
						t.Fatal("missing auth")
					}
					if payload["tools"] == nil {
						t.Fatal("native tool registry missing")
					}
					if requests == 2 {
						raw, _ := json.Marshal(payload)
						s := string(raw)
						if !strings.Contains(s, "private-continuation") || !strings.Contains(s, "evidence") || !strings.Contains(s, "call1") {
							t.Fatal("lost opaque continuation or native tool output", s)
						}
						if strings.Contains(s, "[Tool Result") || strings.Contains(s, "Agent Tool Call") {
							t.Fatal("text tool transcript leaked", s)
						}
						switch provider {
						case "openai":
							input := payload["input"].([]any)
							last := input[len(input)-1].(map[string]any)
							if last["type"] != "function_call_output" || last["call_id"] != "call1" {
								t.Fatal(last)
							}
						case "gemini":
							contents := payload["contents"].([]any)
							last := contents[len(contents)-1].(map[string]any)
							response := last["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
							if response["id"] != "call1" || !strings.Contains(s, "tool-signature") {
								t.Fatal("Gemini id/signature lost", last)
							}
						default:
							messages := payload["messages"].([]any)
							last := messages[len(messages)-1].(map[string]any)
							if last["role"] != "tool" || last["tool_call_id"] != "call1" {
								t.Fatal(last)
							}
						}
					}
					body, _ := json.Marshal(nativeAgentFixture(provider, requests > 1))
					contentType := "application/json"
					if stream {
						body = []byte(nativeAgentSSE(provider, requests > 1))
						contentType = "text/event-stream"
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
				})
				out, err := agentruntime.Run(context.Background(), agentruntime.Options{Input: "investigate", Tools: []agentruntime.ToolSpec{{Name: "read_source", Parameters: map[string]any{"type": "object", "properties": map[string]any{"uid": map[string]any{"type": "string"}}}}}, Limits: agentruntime.Limits{Turns: 3, CallsPerTurn: 4, Calls: 8, InputBytes: 24000, ResultBytes: 2000},
					Complete: func(ctx context.Context, request agentruntime.ModelRequest) (agentruntime.ModelResponse, error) {
						var delta func(string) error
						if stream {
							delta = func(s string) error { visible.WriteString(s); return nil }
						}
						return CreateAgentResponse(ctx, client, AIModelRecord{Provider: provider, Model: "test", BaseURL: "https://model.invalid/v1"}, "test-key", "generic instructions", request, nil, 4000, "medium", delta)
					},
					Execute: func(_ context.Context, call agentruntime.ToolCall) (any, string, error) {
						executed++
						if call.Args["uid"] != "42" {
							t.Fatal("corrupt native arguments", call)
						}
						return "evidence", "read", nil
					},
				})
				if err != nil || out.Text != "verified" || executed != 1 || requests != 2 {
					t.Fatal(out, requests, executed, err)
				}
				if stream && visible.String() != "verified" {
					t.Fatal("reasoning or arguments leaked into visible stream", visible.String())
				}
			})
		}
	}
}

func TestNativeAgentRejectsTruncatedResponsesAndStreams(t *testing.T) {
	for _, tc := range []struct{ provider, raw string }{
		{"openai", `{"status":"incomplete","output_text":"partial"}`},
		{"deepseek", `{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`},
		{"openai-compatible", `{"data":{"choices":[{"message":{"content":"partial"},"finish_reason":"content_filter"}]}}`},
		{"gemini", `{"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"MAX_TOKENS"}]}`},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			if _, err := decodeAgentMessage([]byte(tc.raw), tc.provider, 0); err == nil {
				t.Fatal("truncation accepted")
			}
		})
	}
	for _, provider := range []string{"openai", "deepseek", "gemini"} {
		if _, err := readAgentStream(strings.NewReader("data: [DONE]\n\n"), provider, nil); err == nil {
			t.Fatal("interrupted stream accepted", provider)
		}
	}
}

func TestNativeGeminiBatchesParallelResultsWithoutInventingProviderIDs(t *testing.T) {
	message, err := decodeAgentMessage([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"read_source","args":{}}},{"functionCall":{"name":"read_source","args":{}}}]},"finishReason":"STOP"}]}`), "gemini", 1)
	if err != nil {
		t.Fatal(err)
	}
	messages := []agentruntime.Message{{Role: "user", Text: "read"}, message}
	for _, call := range message.ToolCalls {
		messages = append(messages, agentruntime.Message{Role: "tool", ToolCallID: call.ID, Name: call.Name, Text: `{"result":"evidence"}`})
	}
	payload, _, err := agentRequest(AIModelRecord{}, "gemini", "", messages, nil, false, nil, 4000, "", false)
	if err != nil {
		t.Fatal(err)
	}
	contents := payload["contents"].([]any)
	parts := contents[2].(map[string]any)["parts"].([]any)
	if len(contents) != 3 || len(parts) != 2 {
		t.Fatal("parallel results did not form one response turn", contents)
	}
	for _, part := range parts {
		response := part.(map[string]any)["functionResponse"].(map[string]any)
		if _, ok := response["id"]; ok {
			t.Fatal("invented Gemini provider ID", response)
		}
	}
}
