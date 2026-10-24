// Package agenttest adapts legacy human-readable test scripts to real native
// model envelopes. Production code never imports this package.
package agenttest

import (
	"encoding/json"
	"fmt"
	"strings"

	"inbrix/internal/agentruntime"
)

func InputText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var items []struct {
		Role, Type, CallID, Name string
		CallIDWire               string `json:"call_id"`
		Output                   string
		Content                  json.RawMessage
		Arguments                string
		ToolCallID               string `json:"tool_call_id"`
		ToolCalls                []struct {
			ID       string
			Function struct{ Name string }
		} `json:"tool_calls"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	names := map[string]string{}
	var parts []string
	for _, i := range items {
		for _, call := range i.ToolCalls {
			names[call.ID] = call.Function.Name
		}
		if i.Role == "tool" {
			var content string
			_ = json.Unmarshal(i.Content, &content)
			parts = append(parts, "[Tool Result ("+names[i.ToolCallID]+")]: "+content)
			continue
		}
		if i.Type == "function_call" {
			names[i.CallIDWire] = i.Name
			continue
		}
		if i.Type == "function_call_output" {
			parts = append(parts, "[Tool Result ("+names[i.CallIDWire]+")]: "+i.Output)
			continue
		}
		var s string
		if json.Unmarshal(i.Content, &s) == nil {
			parts = append(parts, s)
			continue
		}
		var content []struct{ Text string }
		if json.Unmarshal(i.Content, &content) == nil {
			for _, c := range content {
				parts = append(parts, c.Text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func Response(text string, sequence int) map[string]any {
	calls, err := agentruntime.ParseToolCalls(text)
	if err != nil || len(calls) == 0 {
		return map[string]any{"output_text": text}
	}
	items := []any{}
	for i, c := range calls {
		args, _ := json.Marshal(c.Args)
		items = append(items, map[string]any{"type": "function_call", "call_id": fmt.Sprintf("test_%d_%d", sequence, i), "name": c.Name, "arguments": string(args)})
	}
	// Preserve ordinary commentary separately from native tool arguments.
	visible := text
	for _, marker := range []string{"```tool_call", "```json:tool_call", "```toolCall", "<｜"} {
		if n := strings.Index(visible, marker); n >= 0 {
			visible = visible[:n]
		}
	}
	if strings.HasPrefix(strings.TrimSpace(visible), "{") || strings.HasPrefix(strings.TrimSpace(visible), "[") {
		visible = ""
	}
	return map[string]any{"status": "completed", "output_text": strings.TrimSpace(visible), "output": items}
}

func CompatibleResponse(text string, sequence int) map[string]any {
	response := Response(text, sequence)
	message := map[string]any{"content": response["output_text"]}
	calls := []any{}
	if items, ok := response["output"].([]any); ok {
		for _, value := range items {
			item := value.(map[string]any)
			calls = append(calls, map[string]any{"id": item["call_id"], "type": "function", "function": map[string]any{"name": item["name"], "arguments": item["arguments"]}})
		}
	}
	finish := "stop"
	if len(calls) > 0 {
		message["tool_calls"] = calls
		finish = "tool_calls"
	}
	return map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}}
}
