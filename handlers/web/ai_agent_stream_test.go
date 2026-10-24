package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"inbrix/internal/agenttest"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

type agentStreamTransport func(*http.Request) (*http.Response, error)

func (f agentStreamTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAgentVisibleTextHidesSplitInternalBlocks(t *testing.T) {
	for _, tc := range []struct{ raw, visible string }{
		{"你好，**世界**。", "你好，**世界**。"},
		{"[来源](https://example.com) 正文", "[来源](https://example.com) 正文"},
		{"<think>private</think>正文", "正文"},
		{"开始\n```tool_call\n{\"name\":\"search_products\"}\n```\n结束", "开始\n\n结束"},
		{"```json:tool_call\nsecret\n```正文", "正文"},
		{"```toolCall\nsecret\n```正文", "正文"},
		{"正文```document\n{\"title\":\"private json\"}\n```", "正文"},
		{"```json:document\nsecret\n```正文", "正文"},
		{"{\"name\":\"search_products\",\"arguments\":{}}", ""},
		{"```json\n{\"name\":\"search_products\",\"arguments\":{}}\n```正文", "正文"},
		{"```go\nfmt.Println(1)\n```\n正文", "```go\nfmt.Println(1)\n```\n正文"},
	} {
		previous := ""
		for i := 1; i <= len(tc.raw); i++ {
			visible := agentVisibleText(tc.raw[:i])
			if !strings.HasPrefix(visible, previous) {
				t.Fatalf("preview is not append-only: %q -> %q", previous, visible)
			}
			if strings.Contains(visible, "secret") || strings.Contains(visible, "private") || strings.Contains(visible, "search_products") {
				t.Fatalf("internal data leaked at byte %d: %q", i, visible)
			}
			previous = visible
		}
		if previous != tc.visible {
			t.Fatalf("got %q, want %q", previous, tc.visible)
		}
	}
}

func TestAgentChatStreamsToolsAndTextBeforeFinalDocument(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	release := make(chan struct{})
	defer close(release)
	var turn atomic.Int32
	h.client = &http.Client{Transport: agentStreamTransport(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Error("upstream must stream")
		}
		reader, writer := io.Pipe()
		currentTurn := turn.Add(1)
		go func() {
			defer writer.Close()
			frame := func(kind, delta string) {
				raw, _ := json.Marshal(map[string]string{"type": kind, "delta": delta})
				_, _ = fmt.Fprintf(writer, "data: %s\n\n", raw)
			}

			nativeTurn := func(text string) {
				response := agenttest.Response(text, int(currentTurn))
				frame("response.output_text.delta", response["output_text"].(string))
				for i, item := range response["output"].([]any) {
					raw, _ := json.Marshal(map[string]any{"type": "response.output_item.done", "output_index": i, "item": item})
					_, _ = fmt.Fprintf(writer, "data: %s\n\n", raw)
				}
			}
			if currentTurn == 1 {
				nativeTurn("先检索产品。\n```tool_call\n{\"name\":\"search_products\",\"args\":{\"query\":\"test\"}}\n```")
			} else if currentTurn == 2 {
				nativeTurn("再查看分类。\n```tool_call\n{\"name\":\"list_product_categories\",\"args\":{}}\n```")
			} else {
				frame("response.output_text.delta", "逐步")
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				frame("response.output_text.delta", "输出。\n```document\n{\"type\":\"quotation\",\"title\":\"测试报价\",\"counterparty\":\"客户\",\"items\":[]}\n```")
			}

			frame("response.completed", "")
		}()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(listener) }()
	defer app.Shutdown()
	body, _ := json.Marshal(AIAgentChatRequest{AccountEmail: account.Email, Messages: []AIAgentChatMessage{{Role: "user", Content: "查产品并报价"}}})
	req, _ := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+"/api/ai/agent-chat", strings.NewReader(string(body)))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("proxy buffering was not disabled")
	}
	scanner := bufio.NewScanner(response.Body)
	var visible strings.Builder
	started, finished := false, false
	released := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event agentChatEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		switch event.Type {
		case "tool_start":
			started = started || event.Tool.Name == "search_products"
		case "tool_end":
			finished = event.Tool.Status == "success"
		case "text_reset":
			visible.Reset()
		case "text_delta":
			visible.WriteString(event.Delta)
			if !released && visible.String() == "逐步" {
				if !started || !finished {
					t.Fatal("missing live tool progress")
				}
				// This text arrived while the provider is blocked before
				// completing its answer, over a real HTTP connection.
				release <- struct{}{}
				released = true
			}
		case "error":
			t.Fatalf("stream error: %s", event.Error)
		case "done":
			if !released || event.Response.Content != "逐步输出。" || event.Response.Document == nil || event.Response.Document.Title != "测试报价" || len(event.Response.ToolCalls) != 2 {
				t.Fatalf("incomplete final response: %+v", event.Response)
			}
			parts := event.Response.Parts
			if len(parts) != 5 || parts[0].Content != "先检索产品。" || parts[1].Tool == nil || parts[1].Tool.Name != "search_products" || parts[2].Content != "再查看分类。" || parts[3].Tool == nil || parts[3].Tool.Name != "list_product_categories" || parts[4].Content != "逐步输出。" {
				t.Fatalf("lost activity order: %+v", parts)
			}
			for _, part := range parts {
				if part.Tool != nil && (part.Tool.Result != nil || part.Tool.Args != nil) {
					t.Fatal("ordered tool display must not contain private payloads")
				}
			}
			if strings.Contains(visible.String(), "private") || strings.Contains(visible.String(), "```") {
				t.Fatalf("internal blocks leaked: %q", visible.String())
			}
			return
		}
	}
	t.Fatalf("stream ended without final result: %v", scanner.Err())
}

func TestAgentStreamErrorAndDisconnect(t *testing.T) {
	t.Run("error event", func(t *testing.T) {
		app := fiber.New()
		app.Post("/", func(c *fiber.Ctx) error {
			return streamAgentChat(c, func(ctx context.Context, emit func(agentChatEvent) error) (*AIAgentChatResponse, error) {
				_ = emit(agentChatEvent{Type: "text_delta", Delta: "partial"})
				return nil, fiber.NewError(422, "upstream interrupted")
			})
		})
		resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(raw), `"type":"error"`) || strings.Contains(string(raw), `"type":"done"`) || !strings.Contains(string(raw), "partial") {
			t.Fatalf("unexpected wire response: %s", raw)
		}
	})
	t.Run("client disconnect cancels work", func(t *testing.T) {
		cancelled := make(chan struct{})
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/", func(c *fiber.Ctx) error {
			return streamAgentChat(c, func(ctx context.Context, emit func(agentChatEvent) error) (*AIAgentChatResponse, error) {
				defer close(cancelled)
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						if err := emit(agentChatEvent{Type: "text_delta", Delta: "streaming"}); err != nil {
							return nil, err
						}
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			})
		})
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() { _ = app.Listener(listener) }()
		defer app.Shutdown()
		client := &http.Client{Timeout: 5 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		select {
		case <-cancelled:
		case <-time.After(5 * time.Second):
			t.Fatal("AI work continued after the client disconnected")
		}
	})
}
