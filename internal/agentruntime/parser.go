package agentruntime

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

var toolCallRegex = regexp.MustCompile("(?s)```(?:tool_call|json:tool_call|toolCall)\\s*(.*?)\\s*```")

func ParseToolCalls(output string) ([]ToolCall, error) {
	normalized, err := NormalizeDSMLToolCalls(output)
	if err != nil {
		return nil, err
	}
	return parseJSONToolCalls(normalized), nil
}

func parseJSONToolCalls(output string) []ToolCall {
	var calls []ToolCall

	// 1. Match all ```tool_call, ```json:tool_call, ```toolCall code fences
	matches := toolCallRegex.FindAllStringSubmatch(output, -1)
	for _, match := range matches {
		if len(match) > 1 {
			fenceContent := strings.TrimSpace(match[1])
			if fenceContent == "" {
				continue
			}
			calls = append(calls, ParseToolCallsFromJSON(fenceContent)...)
		}
	}

	if len(calls) > 0 {
		return calls
	}

	// 2. Fallback: check if the entire output (or code block) is raw JSON tool call
	trimmed := strings.TrimSpace(output)
	return ParseToolCallsFromJSON(trimmed)
}

func ParseToolCallsFromJSON(raw string) []ToolCall {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		var single struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Args      map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(raw), &single); err == nil && single.Name != "" {
			args := single.Arguments
			if args == nil {
				args = single.Args
			}
			if args == nil {
				args = make(map[string]any)
			}
			return []ToolCall{{Name: single.Name, Args: args}}
		}
		// Some compatible gateways emit multiple complete bare tool objects
		// without fences or an enclosing array. Accept the whole JSON stream
		// atomically; never execute a valid prefix of a malformed/mixed reply.
		if stream := parseBareToolStream(raw); len(stream) > 1 {
			return stream
		}
	} else if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		var list []struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Args      map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(raw), &list); err == nil && len(list) > 0 {
			var res []ToolCall
			for _, item := range list {
				if item.Name != "" {
					args := item.Arguments
					if args == nil {
						args = item.Args
					}
					if args == nil {
						args = make(map[string]any)
					}
					res = append(res, ToolCall{Name: item.Name, Args: args})
				}
			}
			return res
		}
	} else {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start != -1 && end > start {
			sub := raw[start : end+1]
			var single struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
				Args      map[string]any `json:"args"`
			}
			if err := json.Unmarshal([]byte(sub), &single); err == nil && single.Name != "" {
				args := single.Arguments
				if args == nil {
					args = single.Args
				}
				if args == nil {
					args = make(map[string]any)
				}
				return []ToolCall{{Name: single.Name, Args: args}}
			}
		}
	}

	return nil
}

func parseBareToolStream(raw string) []ToolCall {
	decoder := json.NewDecoder(strings.NewReader(raw))
	var calls []ToolCall
	for {
		var object map[string]json.RawMessage
		if err := decoder.Decode(&object); err == io.EOF {
			return calls
		} else if err != nil || object == nil {
			return nil
		}
		if len(object) < 2 || len(object) > 3 {
			return nil
		}
		for key := range object {
			if key != "name" && key != "arguments" && key != "args" {
				return nil
			}
		}
		var name string
		if json.Unmarshal(object["name"], &name) != nil || strings.TrimSpace(name) == "" {
			return nil
		}
		arguments, hasArguments := object["arguments"]
		alias, hasAlias := object["args"]
		if hasArguments == hasAlias {
			return nil
		}
		if hasAlias {
			arguments = alias
		}
		var args map[string]any
		if json.Unmarshal(arguments, &args) != nil {
			return nil
		}
		if args == nil {
			args = map[string]any{}
		}
		calls = append(calls, ToolCall{Name: name, Args: args})
		if len(calls) > 64 {
			return nil
		}
	}
}
