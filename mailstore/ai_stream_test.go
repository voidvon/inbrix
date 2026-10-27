package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAIResponseStreamsBeforeCompletion(t *testing.T) {
	for _, provider := range []string{"openai", "openai-compatible", "gemini"} {
		t.Run(provider, func(t *testing.T) {
			release := make(chan struct{})
			defer close(release)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if provider == "gemini" {
					if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
						t.Errorf("wrong Gemini stream endpoint: %s", r.URL)
					}
				} else if body["stream"] != true {
					t.Error("provider streaming was not enabled")
				}
				if body["temperature"] != float64(0.3) {
					t.Error("stream lost extra parameters")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				frame := func(raw string) {
					_, _ = fmt.Fprintf(w, "data: %s\r\n\r\n", raw)
					w.(http.Flusher).Flush()
				}
				switch provider {
				case "openai":
					frame(`{"type":"response.output_text.delta","delta":"你好"}`)
				case "gemini":
					frame(`{"candidates":[{"content":{"parts":[{"text":"private reasoning","thought":true},{"text":"你好"}]}}]}`)
				default:
					frame(`{"data":{"choices":[{"index":0,"delta":{"reasoning_content":"private reasoning","content":"你好"}}]}}`)
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				switch provider {
				case "openai":
					frame(`{"type":"response.output_text.delta","delta":"，世界"}`)
					frame(`{"type":"response.completed"}`)
				case "gemini":
					frame(`{"candidates":[{"content":{"parts":[{"text":"，世界"}]},"finishReason":"STOP"}]}`)
				default:
					frame(`{"choices":[{"index":0,"delta":{"content":"，世界"},"finish_reason":"stop"}]}`)
					frame(`[DONE]`)
				}
			}))
			defer server.Close()
			// Unblock the server even when a streaming assertion fails.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first := make(chan string, 4)
			done := make(chan error, 1)
			go func() {
				text, err := CreateAIResponseStream(ctx, server.Client(), AIModelRecord{Provider: provider, BaseURL: server.URL, Model: "test", ExtraParams: `{"temperature":0.3}`}, "key", "instructions", "question", 4096, "medium", func(delta string) error {
					first <- delta
					return nil
				})
				if err == nil && text != "你好，世界" {
					err = fmt.Errorf("unexpected final text: %q", text)
				}
				done <- err
			}()
			select {
			case text := <-first:
				if text != "你好" {
					t.Fatalf("first delta: %q", text)
				}
			case <-ctx.Done():
				t.Fatal("no text before provider completion")
			}
			// A single release lets the provider finish without closing twice.
			release <- struct{}{}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAIStreamRejectsIncompleteAndFailedResponses(t *testing.T) {
	for _, tc := range []struct{ name, provider, wire string }{
		{"truncated", "openai", `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n"},
		{"incomplete", "openai", `data: {"type":"response.incomplete"}` + "\n\n"},
		{"failed", "openai", `data: {"type":"response.failed","response":{"error":{"message":"failed"}}}` + "\n\n"},
		{"gateway error", "openai-compatible", `data: {"data":{"error":"gateway failed"}}` + "\n\n"},
		{"length", "openai-compatible", `data: {"choices":[{"delta":{"content":"partial"},"finish_reason":"length"}]}` + "\n\n"},
		{"blocked", "gemini", `data: {"promptFeedback":{"blockReason":"SAFETY"}}` + "\n\n"},
		{"invalid", "openai", "data: invalid\n\n"},
		{"done without finish", "openai-compatible", "data: [DONE]\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.wire))}
			if _, err := readAIResponse(resp, tc.provider, func(string) error { return nil }); err == nil {
				t.Fatal("expected stream failure")
			}
		})
	}
}

func TestAISSEMultilineAndCallbackCancellation(t *testing.T) {
	wire := ": heartbeat\r\nevent: message\r\ndata: {\r\ndata: \"ok\": true}\r\n\r\n"
	if err := readAISSE(strings.NewReader(wire), func(data string) error {
		if data != "{\n\"ok\": true}" {
			t.Fatalf("unexpected data: %q", data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("consumer disconnected")
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"))}
	if _, err := readAIResponse(resp, "openai", func(string) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("callback error not preserved: %v", err)
	}
}
