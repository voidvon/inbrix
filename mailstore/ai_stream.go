package mailstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// readAIResponse normalizes SSE to the existing provider response shape, so
// streaming and buffered requests share request construction and validation.
func readAIResponse(resp *http.Response, provider string, onDelta func(string) error) ([]byte, error) {
	if onDelta == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return io.ReadAll(io.LimitReader(resp.Body, maxAIResponseBytes))
	}
	var text strings.Builder
	complete := false
	emit := func(delta string) error {
		if text.Len()+len(delta) > maxAIResponseBytes {
			return errors.New("AI response exceeded output budget")
		}
		text.WriteString(delta)
		if delta != "" {
			return onDelta(delta)
		}
		return nil
	}
	err := readAISSE(resp.Body, func(data string) error {
		if data == "[DONE]" {
			// A finish event is still required; [DONE] alone can hide a
			// truncated response or a gateway error.
			return nil
		}
		var event struct {
			Type     string          `json:"type"`
			Delta    string          `json:"delta"`
			Error    json.RawMessage `json:"error"`
			Response struct {
				Error json.RawMessage `json:"error"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return errors.New("AI stream returned invalid JSON")
		}
		if msg := aiErrorValueMessage(event.Error); msg != "" {
			return fmt.Errorf("AI stream error: %s", msg)
		}
		switch provider {
		case "openai":
			switch event.Type {
			case "response.output_text.delta":
				return emit(event.Delta)
			case "response.completed":
				complete = true
			case "response.failed", "response.incomplete", "error":
				return fmt.Errorf("AI stream %s: %s", event.Type, aiErrorValueMessage(event.Response.Error))
			}
		case "gemini":
			var result struct {
				Candidates []struct {
					Content      geminiContent `json:"content"`
					FinishReason string        `json:"finishReason"`
				} `json:"candidates"`
				PromptFeedback struct {
					BlockReason string `json:"blockReason"`
				} `json:"promptFeedback"`
			}
			_ = json.Unmarshal([]byte(data), &result)
			if reason := result.PromptFeedback.BlockReason; reason != "" {
				return fmt.Errorf("Gemini blocked response: %s", reason)
			}
			if len(result.Candidates) > 0 {
				candidate := result.Candidates[0]
				for _, part := range candidate.Content.Parts {
					if !part.Thought {
						if err := emit(part.Text); err != nil {
							return err
						}
					}
				}
				if candidate.FinishReason != "" {
					if candidate.FinishReason != "STOP" {
						return fmt.Errorf("Gemini stream stopped: %s", candidate.FinishReason)
					}
					complete = true
				}
			}
		default:
			var chunk struct {
				Choices []struct {
					Index int `json:"index"`
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason string          `json:"finish_reason"`
					Error        json.RawMessage `json:"error"`
				} `json:"choices"`
				Data json.RawMessage `json:"data"`
			}
			raw := []byte(data)
			for depth := 0; depth < 4; depth++ {
				if msg := openAICompatibleResponseError(raw); msg != "" {
					return fmt.Errorf("AI stream error: %s", msg)
				}
				if err := json.Unmarshal(raw, &chunk); err != nil {
					return errors.New("AI stream returned invalid chunk")
				}
				if len(chunk.Choices) > 0 || len(chunk.Data) == 0 {
					break
				}
				raw, chunk.Data = chunk.Data, nil
			}
			for _, choice := range chunk.Choices {
				if choice.Index != 0 {
					continue
				}
				if msg := aiErrorValueMessage(choice.Error); msg != "" {
					return fmt.Errorf("AI stream error: %s", msg)
				}
				if err := emit(choice.Delta.Content); err != nil {
					return err
				}
				if choice.FinishReason != "" {
					if choice.FinishReason != "stop" {
						return fmt.Errorf("AI stream stopped: %s", choice.FinishReason)
					}
					complete = true
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
	// Only the output is normalized. The original streaming chunks have already
	// reached the consumer, including whitespace and Markdown formatting.
	switch provider {
	case "openai":
		return json.Marshal(map[string]any{"output_text": text.String()})
	case "gemini":
		return json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": geminiContent{Parts: []geminiPart{{Text: text.String()}}}, "finishReason": "STOP"}}})
	default:
		return json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text.String()}, "finish_reason": "stop"}}})
	}
}

func readAISSE(reader io.Reader, onData func(string) error) error {
	// Bound both individual frames and the complete wire response (including
	// reasoning and metadata that are intentionally never sent to the browser).
	limited := &io.LimitedReader{R: reader, N: 16 * maxAIResponseBytes}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), maxAIResponseBytes)
	var lines []string
	frameBytes := 0
	dispatch := func() error {
		if len(lines) == 0 {
			return nil
		}
		data := strings.Join(lines, "\n")
		lines, frameBytes = nil, 0
		return onData(data)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			frameBytes += len(value)
			if frameBytes > maxAIResponseBytes {
				return errors.New("AI stream frame exceeded output budget")
			}
			lines = append(lines, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if limited.N == 0 {
		return errors.New("AI stream exceeded wire budget")
	}
	return dispatch()
}
