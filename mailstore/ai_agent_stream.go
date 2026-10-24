package mailstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Native tool arguments and opaque reasoning are buffered privately; only text
// deltas reach the caller. A finish event is required before calls may execute.
func readAgentStream(reader io.Reader, provider string, onDelta func(string) error) ([]byte, error) {
	var text, reasoning strings.Builder
	type callDelta struct{ ID, Name, Arguments string }
	calls := map[int]*callDelta{}
	output := map[int]json.RawMessage{}
	geminiParts := []json.RawMessage{}
	complete := false
	finish := ""
	var authoritative json.RawMessage
	emit := func(s string) error {
		if text.Len()+len(s) > maxAIResponseBytes {
			return errors.New("AI response exceeded output budget")
		}
		text.WriteString(s)
		if onDelta != nil && s != "" {
			return onDelta(s)
		}
		return nil
	}
	err := readAISSE(reader, func(data string) error {
		if data == "[DONE]" {
			return nil
		}
		raw := []byte(data)
		var event struct {
			Type        string          `json:"type"`
			Delta       string          `json:"delta"`
			OutputIndex int             `json:"output_index"`
			Item        json.RawMessage `json:"item"`
			Response    json.RawMessage `json:"response"`
			Error       json.RawMessage `json:"error"`
			Data        json.RawMessage `json:"data"`
		}
		for depth := 0; depth < 4; depth++ {
			if json.Unmarshal(raw, &event) != nil {
				return errors.New("AI stream returned invalid JSON")
			}
			if msg := aiErrorValueMessage(event.Error); msg != "" {
				return fmt.Errorf("AI stream error: %s", msg)
			}
			if len(event.Data) == 0 {
				break
			}
			raw = event.Data
			event.Data = nil
		}
		switch provider {
		case "openai":
			switch event.Type {
			case "response.output_text.delta":
				return emit(event.Delta)
			case "response.output_item.done":
				output[event.OutputIndex] = event.Item
			case "response.completed":
				complete = true
				authoritative = event.Response
			case "response.failed", "response.incomplete", "error":
				return fmt.Errorf("AI stream %s", event.Type)
			}
		case "gemini":
			var chunk struct {
				Candidates []struct {
					Content struct {
						Parts []json.RawMessage `json:"parts"`
					} `json:"content"`
					FinishReason string `json:"finishReason"`
				} `json:"candidates"`
				PromptFeedback struct {
					BlockReason string `json:"blockReason"`
				} `json:"promptFeedback"`
			}
			if json.Unmarshal(raw, &chunk) != nil {
				return errors.New("invalid Gemini stream")
			}
			if chunk.PromptFeedback.BlockReason != "" {
				return errors.New("Gemini blocked response")
			}
			if len(chunk.Candidates) > 0 {
				c := chunk.Candidates[0]
				for _, part := range c.Content.Parts {
					geminiParts = append(geminiParts, part)
					var p struct {
						Text    string
						Thought bool
					}
					_ = json.Unmarshal(part, &p)
					if !p.Thought {
						if err := emit(p.Text); err != nil {
							return err
						}
					}
				}
				if c.FinishReason != "" {
					if c.FinishReason != "STOP" {
						return fmt.Errorf("Gemini stream stopped: %s", c.FinishReason)
					}
					complete = true
				}
			}
		default:
			var chunk struct {
				Choices []struct {
					Index int `json:"index"`
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
						ToolCalls        []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string          `json:"finish_reason"`
					Error        json.RawMessage `json:"error"`
				} `json:"choices"`
			}
			if json.Unmarshal(raw, &chunk) != nil {
				return errors.New("invalid AI stream")
			}
			for _, c := range chunk.Choices {
				if c.Index != 0 {
					continue
				}
				if msg := aiErrorValueMessage(c.Error); msg != "" {
					return fmt.Errorf("AI stream error: %s", msg)
				}
				if err := emit(c.Delta.Content); err != nil {
					return err
				}
				reasoning.WriteString(c.Delta.ReasoningContent)
				for _, delta := range c.Delta.ToolCalls {
					if delta.Index < 0 || delta.Index >= 128 {
						return errors.New("AI stream tool budget exceeded")
					}
					call := calls[delta.Index]
					if call == nil {
						call = &callDelta{}
						calls[delta.Index] = call
					}
					call.ID += delta.ID
					call.Name += delta.Function.Name
					call.Arguments += delta.Function.Arguments
				}
				if c.FinishReason != "" {
					if c.FinishReason != "stop" && c.FinishReason != "tool_calls" {
						return fmt.Errorf("AI stream stopped: %s", c.FinishReason)
					}
					complete = true
					finish = c.FinishReason
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !complete {
		return nil, errors.New("AI stream interrupted before completion")
	}
	switch provider {
	case "openai":
		var full struct {
			Output     json.RawMessage `json:"output"`
			OutputText string          `json:"output_text"`
		}
		if len(authoritative) > 0 && json.Unmarshal(authoritative, &full) == nil && (len(full.Output) > 0 || full.OutputText != "") {
			return authoritative, nil
		}
		items := []json.RawMessage{}
		for i := 0; i < len(output); i++ {
			item, ok := output[i]
			if !ok {
				return nil, errors.New("AI stream output items were incomplete")
			}
			items = append(items, item)
		}
		return json.Marshal(map[string]any{"status": "completed", "output_text": text.String(), "output": items})
	case "gemini":
		return json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": geminiParts}, "finishReason": "STOP"}}})
	default:
		wire := []any{}
		for i := 0; i < len(calls); i++ {
			c := calls[i]
			if c == nil {
				return nil, errors.New("AI stream tool calls were incomplete")
			}
			wire = append(wire, map[string]any{"id": c.ID, "type": "function", "function": map[string]string{"name": c.Name, "arguments": c.Arguments}})
		}
		return json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text.String(), "tool_calls": wire, "reasoning_content": reasoning.String()}, "finish_reason": finish}}})
	}
}
