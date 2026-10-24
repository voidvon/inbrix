// Package agentruntime runs bounded tool conversations independently of HTTP,
// model providers, business skills, or interactive chat persistence.
package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type ToolCall struct {
	ID      string         `json:"id,omitempty"`
	Name    string         `json:"name"`
	Args    map[string]any `json:"args"`
	Result  any            `json:"result,omitempty"`
	Summary string         `json:"summary,omitempty"`
	Status  string         `json:"status,omitempty"`
}

type Limits struct {
	Turns, CallsPerTurn, Calls, InputBytes, ResultBytes int
}

type Options struct {
	Input   string
	Limits  Limits
	Respond func(context.Context, string) (string, error)
	Execute func(context.Context, ToolCall) (any, string, error)
	// Project strips presentation-only data before feeding a result to the model.
	Project func(any) any
	OnTool  func(string, ToolCall) error
}

type Result struct {
	Text  string
	Calls []ToolCall
}

var ErrIncomplete = errors.New("AI 未完成处理，请重试 / AI requested tools without completing the reply")

// Run always reserves space for the next tool result and final synthesis. It
// never treats a tool request issued after exhaustion as a completed answer.
func Run(ctx context.Context, opts Options) (Result, error) {
	var out Result
	l := opts.Limits
	if opts.Respond == nil || opts.Execute == nil || l.Turns <= 0 || l.CallsPerTurn <= 0 || l.Calls <= 0 || l.ResultBytes < 512 || l.InputBytes < l.ResultBytes+4096 {
		return out, errors.New("invalid agent runtime configuration")
	}
	input := opts.Input
	if len(input) > l.InputBytes-l.ResultBytes {
		return out, errors.New("AI context exceeds the input budget")
	}
	emit := func(kind string, call ToolCall) error {
		if opts.OnTool != nil {
			return opts.OnTool(kind, call)
		}
		return nil
	}
	for turn := 0; turn < l.Turns; turn++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if len(input) > l.InputBytes {
			return out, errors.New("AI context exceeds the input budget")
		}
		raw, err := opts.Respond(ctx, input)
		if err != nil {
			return out, err
		}
		calls, err := ParseToolCalls(raw)
		if err != nil {
			return out, err
		}
		if len(calls) == 0 {
			out.Text = raw
			return out, nil
		}
		stopReason := ""
		for index, call := range calls {
			if index >= l.CallsPerTurn || len(out.Calls) >= l.Calls || len(input) > l.InputBytes-l.ResultBytes-4096 {
				stopReason = "Tool/context budget reached. Some requested tools were NOT executed."
				break
			}
			if err := ctx.Err(); err != nil {
				return out, err
			}
			call.ID = fmt.Sprintf("call_%d", len(out.Calls)+1)
			if err := emit("tool_start", call); err != nil {
				return out, err
			}
			result, summary, toolErr := opts.Execute(ctx, call)
			call.Status, call.Result, call.Summary = "success", result, summary
			if toolErr != nil {
				call.Status = "error"
				call.Result = map[string]string{"error": toolErr.Error()}
				call.Summary = "工具执行出错: " + toolErr.Error()
			} else if opts.Project != nil {
				call.Result = opts.Project(result)
			}
			encoded, marshalErr := json.Marshal(call.Result)
			if marshalErr != nil || len(encoded) > l.ResultBytes {
				call.Result = map[string]any{"outputOmitted": true, "summary": clip(call.Summary, min(500, (l.ResultBytes-256)/6)), "notice": "Result exceeded the output budget. Request a smaller page or narrower data. Do not claim to have read the omitted data."}
				encoded, _ = json.Marshal(call.Result)
			}
			if err := emit("tool_end", call); err != nil {
				return out, err
			}
			out.Calls = append(out.Calls, call)
			args, _ := json.Marshal(call.Args)
			if len(args) > 2048 {
				args = []byte(`{"argumentsOmitted":true}`)
			}
			input += fmt.Sprintf("\n\n[Agent Tool Call]: %s(%s)\n[Tool Result (%s)]: %s", clip(call.Name, 80), args, clip(call.Name, 80), encoded)
		}
		if turn == l.Turns-1 || stopReason != "" {
			input += "\n\n[System]: " + stopReason + " No more tools are available for this response. Give a final answer from the evidence actually read. Explicitly state any unread pages, missing mail bodies, or incomplete coverage; never claim to have reviewed the whole thread unless you did."
			if len(input) > l.InputBytes {
				return out, errors.New("AI context exceeds the input budget")
			}
			if err := ctx.Err(); err != nil {
				return out, err
			}
			out.Text, err = opts.Respond(ctx, input)
			if err != nil {
				return out, err
			}
			if calls, err := ParseToolCalls(out.Text); err != nil || len(calls) > 0 {
				return out, ErrIncomplete
			}
			return out, nil
		}
		input += "\n\nBased on the tool results above, continue answering the user or call another tool if needed."
	}
	return out, ErrIncomplete
}

func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}
