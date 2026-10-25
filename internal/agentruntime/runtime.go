// Package agentruntime runs bounded tool conversations independently of HTTP,
// model providers, business skills, or interactive chat persistence.
package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ToolCall struct {
	ID            string         `json:"id,omitempty"`
	Name          string         `json:"name"`
	Args          map[string]any `json:"args"`
	Result        any            `json:"result,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Status        string         `json:"status,omitempty"`
	ArgumentError string         `json:"-"`
}

type Limits struct {
	Turns, CallsPerTurn, Calls, InputBytes, ResultBytes int
	ValidationRetries                                   int
}

type Options struct {
	Input        string
	Tools        []ToolSpec
	OutputSchema map[string]any
	Complete     func(context.Context, ModelRequest) (ModelResponse, error)
	// Respond is a legacy text adapter. Production agents use Complete.
	Limits  Limits
	Respond func(context.Context, string) (string, error)
	Execute func(context.Context, ToolCall) (any, string, error)
	// Project strips presentation-only data before feeding a result to the model.
	Project func(any) any
	OnTool  func(string, ToolCall) error
	// Validate checks final answers before accepting them. Rejected answers may
	// be corrected within both the turn budget and ValidationRetries limit.
	Validate func(string) error
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
	native := opts.Complete != nil
	if (!native && opts.Respond == nil) || opts.Execute == nil || l.Turns <= 0 || l.CallsPerTurn <= 0 || l.Calls <= 0 || l.ResultBytes < 512 || l.InputBytes < l.ResultBytes+4096 {
		return out, errors.New("invalid agent runtime configuration")
	}
	allowed := map[string]bool{}
	if native && opts.OutputSchema != nil {
		opts.Tools = append(append([]ToolSpec{}, opts.Tools...), ToolSpec{Name: "submit_result", Description: "Submit the completed task result. Call this alone after gathering the evidence required by the task.", Parameters: opts.OutputSchema})
	}
	for _, spec := range opts.Tools {
		if spec.Name == "" || allowed[spec.Name] || spec.Parameters == nil {
			return out, errors.New("invalid agent tool registry")
		}
		allowed[spec.Name] = true
	}
	complete := opts.Complete
	if !native {
		complete = legacyModel(opts.Respond)
	}
	messages := []Message{{Role: "user", Text: opts.Input}}
	size := func(ms []Message) int {
		b, _ := json.Marshal(ModelRequest{Messages: ms, Tools: opts.Tools})
		return len(b)
	}
	if size(messages) > l.InputBytes-l.ResultBytes {
		return out, errors.New("AI context exceeds the input budget")
	}
	emit := func(kind string, call ToolCall) error {
		if opts.OnTool != nil {
			return opts.OnTool(kind, call)
		}
		return nil
	}
	validationRetries := 0
	seenIDs := map[string]bool{}
	checkCalls := func(calls []ToolCall) error {
		if len(calls) > 128 {
			return errors.New("AI requested too many tools in one response")
		}
		for _, call := range calls {
			if call.ID == "" || seenIDs[call.ID] {
				return errors.New("AI returned missing or duplicate tool call IDs")
			}
			seenIDs[call.ID] = true
		}
		return nil
	}
	for turn := 0; turn < l.Turns; turn++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if size(messages) > l.InputBytes {
			return out, errors.New("AI context exceeds the input budget")
		}
		response, err := complete(ctx, ModelRequest{Messages: messages, Tools: opts.Tools})
		if err != nil {
			return out, err
		}
		assistant := response.Message
		assistant.Role = "assistant"
		answerText := assistant.Text
		calls := assistant.ToolCalls
		if err := checkCalls(calls); err != nil {
			return out, err
		}
		var answerErr error
		terminal := len(calls) == 1 && opts.OutputSchema != nil && calls[0].Name == "submit_result"
		if terminal && calls[0].ArgumentError != "" {
			// A malformed submission is a failed tool call, not a failed answer:
			// the parse error goes back as its tool result and the turn continues,
			// mirroring how harnesses recover unparsable tool arguments.
			terminal = false
		}
		if terminal {
			if calls[0].ArgumentError != "" {
				answerText = ""
				answerErr = errors.New(calls[0].ArgumentError)
			} else {
				raw, _ := json.Marshal(calls[0].Args)
				answerText = string(raw)
			}
			calls = nil
		}
		if len(calls) == 0 {
			if answerErr == nil && opts.Validate != nil {
				answerErr = opts.Validate(answerText)
			}
			if err := answerErr; err != nil {
				if turn == l.Turns-1 || validationRetries >= l.ValidationRetries {
					return out, err
				}
				feedback, _ := json.Marshal(map[string]string{"validationError": err.Error(), "instruction": "Correct the answer using the required output contract. Use the remaining tools if evidence is missing."})
				correction := Message{Role: "user", Text: string(feedback)}
				if terminal {
					correction = Message{Role: "tool", ToolCallID: assistant.ToolCalls[0].ID, Name: "submit_result", Text: string(feedback)}
				}
				next := append(append([]Message{}, messages...), assistant, correction)
				if size(next) > l.InputBytes-l.ResultBytes {
					return out, err
				}
				messages = next
				validationRetries++
				continue
			}
			out.Text = answerText
			return out, nil
		}
		messages = append(messages, assistant)
		// Every native call receives a matching result, including calls refused
		// by policy or budgets. Never leave dangling call IDs in the next request.
		stop := false
		for index, call := range calls {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			if index >= l.CallsPerTurn || len(out.Calls) >= l.Calls || size(messages) > l.InputBytes-l.ResultBytes-4096 {
				stop = true
				messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Name: call.Name, Text: `{"error":"Tool/context budget reached; this call was NOT executed."}`})
				continue
			}
			if err := emit("tool_start", call); err != nil {
				return out, err
			}
			var value any
			var summary string
			var toolErr error
			if native && !allowed[call.Name] {
				toolErr = errors.New("tool is not available in this execution")
			} else if call.ArgumentError != "" {
				toolErr = errors.New(call.ArgumentError)
			} else if call.Name == "submit_result" && opts.OutputSchema != nil {
				toolErr = errors.New("submit_result must be called alone after completing evidence gathering")
			} else {
				value, summary, toolErr = opts.Execute(ctx, call)
			}
			call.Status, call.Result, call.Summary = "success", value, summary
			if toolErr != nil {
				call.Status = "error"
				call.Result = map[string]string{"error": toolErr.Error()}
				call.Summary = "工具执行出错: " + toolErr.Error()
			} else if opts.Project != nil {
				call.Result = opts.Project(value)
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
			messages = append(messages, Message{Role: "tool", ToolCallID: call.ID, Name: call.Name, Text: string(encoded)})
		}
		if turn == l.Turns-1 || stop {
			messages = append(messages, Message{Role: "user", Text: "No more tools are available. Give the final answer from evidence actually read and explicitly state incomplete coverage."})
			if size(messages) > l.InputBytes {
				return out, errors.New("AI context exceeds the input budget")
			}
			if err := ctx.Err(); err != nil {
				return out, err
			}
			var finalText string
			for attempt := 0; ; attempt++ {
				final, err := complete(ctx, ModelRequest{Messages: messages, Tools: opts.Tools, DisableTools: true})
				if err != nil {
					return out, err
				}
				if len(final.Message.ToolCalls) == 1 && opts.OutputSchema != nil && final.Message.ToolCalls[0].Name == "submit_result" {
					if err := checkCalls(final.Message.ToolCalls); err != nil {
						return out, err
					}
					if call := final.Message.ToolCalls[0]; call.ArgumentError != "" && attempt == 0 {
						// One corrective round for unparsable arguments; a second
						// failure ends the turn with the parse error.
						errorPayload, _ := json.Marshal(map[string]string{"error": call.ArgumentError})
						messages = append(messages,
							Message{Role: "assistant", Text: final.Message.Text, ToolCalls: final.Message.ToolCalls},
							Message{Role: "tool", ToolCallID: call.ID, Name: "submit_result", Text: string(errorPayload)})
						if size(messages) > l.InputBytes {
							return out, errors.New(call.ArgumentError)
						}
						if err := ctx.Err(); err != nil {
							return out, err
						}
						continue
					}
					if final.Message.ToolCalls[0].ArgumentError != "" {
						return out, errors.New(final.Message.ToolCalls[0].ArgumentError)
					}
					raw, _ := json.Marshal(final.Message.ToolCalls[0].Args)
					finalText = string(raw)
				} else if len(final.Message.ToolCalls) > 0 {
					return out, ErrIncomplete
				} else {
					finalText = final.Message.Text
				}
				if opts.Validate != nil {
					if err := opts.Validate(finalText); err != nil {
						return out, err
					}
				}
				out.Text = finalText
				return out, nil
			}
		}
	}
	return out, ErrIncomplete
}

// Legacy formatting is confined to this compatibility adapter, never used as
// the wire protocol for a native model. It can be removed with text gateways.
func legacyModel(respond func(context.Context, string) (string, error)) func(context.Context, ModelRequest) (ModelResponse, error) {
	sequence := 0
	return func(ctx context.Context, req ModelRequest) (ModelResponse, error) {
		var text strings.Builder
		for _, m := range req.Messages {
			if m.Role == "tool" {
				fmt.Fprintf(&text, "\n[Tool Result (%s)]: %s", m.Name, m.Text)
			} else {
				text.WriteString("\n" + m.Text)
			}
		}
		raw, err := respond(ctx, strings.TrimPrefix(text.String(), "\n"))
		if err != nil {
			return ModelResponse{}, err
		}
		calls, err := ParseToolCalls(raw)
		if err != nil {
			return ModelResponse{}, err
		}
		for i := range calls {
			sequence++
			calls[i].ID = fmt.Sprintf("call_%d", sequence)
		}
		return ModelResponse{Message: Message{Role: "assistant", Text: raw, ToolCalls: calls}}, nil
	}
}

func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}
