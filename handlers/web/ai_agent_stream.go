package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

type agentChatEvent struct {
	Type     string               `json:"type"`
	Delta    string               `json:"delta,omitempty"`
	Tool     *AIToolCall          `json:"tool,omitempty"`
	Response *AIAgentChatResponse `json:"response,omitempty"`
	Error    string               `json:"error,omitempty"`
}

func sendToolEvent(emit func(agentChatEvent) error, kind string, call AIToolCall) error {
	if emit == nil {
		return nil
	}
	// Tool results can contain entire mail bodies. Only send display metadata.
	return emit(agentChatEvent{Type: kind, Tool: &AIToolCall{ID: call.ID, Name: call.Name, Summary: call.Summary, Status: call.Status}})
}

func streamAgentChat(c *fiber.Ctx, run func(context.Context, func(agentChatEvent) error) (*AIAgentChatResponse, error)) error {
	requestContext := c.UserContext()
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache, no-transform")
	c.Set("X-Accel-Buffering", "no")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithTimeout(requestContext, 10*time.Minute)
		defer cancel()
		write := func(event agentChatEvent) error {
			raw, err := json.Marshal(event)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
				return err
			}
			return w.Flush()
		}
		if err := write(agentChatEvent{Type: "started"}); err != nil {
			return
		}
		events := make(chan agentChatEvent, 16)
		emit := func(event agentChatEvent) error {
			select {
			case events <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		go func() {
			defer close(events)
			response, err := run(ctx, emit)
			if err != nil {
				message := err.Error()
				var fiberError *fiber.Error
				if errors.As(err, &fiberError) {
					message = fiberError.Message
				}
				_ = emit(agentChatEvent{Type: "error", Error: message})
				return
			}
			_ = emit(agentChatEvent{Type: "done", Response: response})
		}()
		heartbeat := time.NewTicker(10 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				if err := write(event); err != nil {
					return // Cancels upstream HTTP and tool work on disconnect.
				}
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case <-ctx.Done():
				_ = write(agentChatEvent{Type: "error", Error: ctx.Err().Error()})
				return
			}
		}
	})
	return nil
}

// agentVisibleText withholds internal tool/document fences and reasoning, even
// when their delimiters arrive one byte at a time. Ordinary Markdown code blocks
// are streamed after their language line is known.
func agentVisibleText(raw string) string {
	var out strings.Builder
	for raw != "" {
		// Some gateways ignore the requested fence and return a bare JSON
		// tool call. Keep that provisional payload private until final parsing.
		trimmed := strings.TrimSpace(raw)
		arrayOfTools := strings.HasPrefix(trimmed, "[") && (strings.TrimSpace(trimmed[1:]) == "" || strings.HasPrefix(strings.TrimSpace(trimmed[1:]), "{"))
		if strings.HasPrefix(trimmed, "{") || arrayOfTools {
			break
		}
		index, token := len(raw), ""
		for _, candidate := range []string{"```", "<think>"} {
			if i := strings.Index(raw, candidate); i >= 0 && i < index {
				index, token = i, candidate
			}
		}
		if token == "" {
			// Hold incomplete delimiters until the next delta disambiguates them.
			keep := 0
			for _, candidate := range []string{"```", "<think>"} {
				for n := 1; n < len(candidate); n++ {
					if strings.HasSuffix(raw, candidate[:n]) && n > keep {
						keep = n
					}
				}
			}
			out.WriteString(raw[:len(raw)-keep])
			break
		}
		out.WriteString(raw[:index])
		raw = raw[index:]
		if token == "<think>" {
			end := strings.Index(raw, "</think>")
			if end < 0 {
				break
			}
			raw = raw[end+len("</think>"):]
			continue
		}
		lineEnd := strings.IndexByte(raw, '\n')
		if lineEnd < 0 {
			break
		}
		language := strings.TrimSpace(raw[3:lineEnd])
		hidden := language == "tool_call" || language == "json:tool_call" || language == "toolCall" || language == "document" || language == "json:document"
		end := strings.Index(raw[lineEnd+1:], "```")
		if language == "json" {
			if end < 0 {
				break
			}
			hidden = len(parseToolCallsFromJSON(raw[lineEnd+1:lineEnd+1+end])) > 0
		}
		if hidden {
			if end < 0 {
				break
			}
			raw = raw[lineEnd+1+end+3:]
		} else {
			if end < 0 {
				out.WriteString(raw)
				break
			}
			end += lineEnd + 1 + 3
			out.WriteString(raw[:end])
			raw = raw[end:]
		}
	}
	return out.String()
}
