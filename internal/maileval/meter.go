package maileval

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"inbrix/mailstore"
)

type RequestUsage struct {
	Status       int   `json:"status"`
	LatencyMS    int64 `json:"latencyMs"`
	Known        bool  `json:"known"`
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	TotalTokens  int64 `json:"totalTokens"`
}
type Meter struct {
	Client      mailstore.HTTPClient
	MaxRequests int
	mu          sync.Mutex
	requests    int
	usage       []RequestUsage
	lastOutput  string
	hidden      []string
	violations  []string
}

func (m *Meter) BeginCase(hidden []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = nil
	m.lastOutput = ""
	m.hidden = append([]string{}, hidden...)
	m.violations = nil
}

func (m *Meter) Snapshot() ([]RequestUsage, string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]RequestUsage{}, m.usage...), m.lastOutput, append([]string{}, m.violations...)
}

func (m *Meter) Do(req *http.Request) (*http.Response, error) {
	// Check the exact outgoing body, never log it. Escaped JSON is decoded as
	// text too so a boundary regression cannot hide behind Unicode escaping.
	if req.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(req.Body, 2<<20+1))
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(raw) > 2<<20 {
			return nil, errors.New("evaluation request exceeds budget")
		}
		decoded := string(raw)
		var value any
		if json.Unmarshal(raw, &value) == nil {
			decoded += "\n" + flattenJSONStrings(value)
		}
		m.mu.Lock()
		for _, canary := range m.hidden {
			if strings.Contains(decoded, canary) {
				m.violations = append(m.violations, canary)
			}
		}
		violated := len(m.violations) > 0
		m.mu.Unlock()
		if violated {
			return nil, errors.New("evaluation mail boundary violation; request blocked")
		}
		req.Body = io.NopCloser(bytes.NewReader(raw))
	}
	m.mu.Lock()
	if m.requests >= m.MaxRequests {
		m.mu.Unlock()
		return nil, errors.New("evaluation request budget exhausted")
	}
	m.requests++
	m.mu.Unlock()
	started := time.Now()
	response, err := m.Client.Do(req)
	usage := RequestUsage{LatencyMS: time.Since(started).Milliseconds()}
	if response != nil {
		usage.Status = response.StatusCode
	}
	if err != nil {
		m.appendUsage(usage, "")
		return response, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	response.Body.Close()
	usage.LatencyMS = time.Since(started).Milliseconds()
	if readErr != nil {
		m.appendUsage(usage, "")
		return nil, readErr
	}
	if len(raw) > 2<<20 {
		m.appendUsage(usage, "")
		return nil, errors.New("evaluation response exceeds budget")
	}
	usage, output := parseUsage(raw, usage)
	m.appendUsage(usage, output)
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}

func (m *Meter) appendUsage(usage RequestUsage, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = append(m.usage, usage)
	if output != "" {
		m.lastOutput = output
	}
}

func parseUsage(raw []byte, usage RequestUsage) (RequestUsage, string) {
	text := ""
	for depth := 0; depth < 4; depth++ {
		var result struct {
			Data       json.RawMessage `json:"data"`
			OutputText string          `json:"output_text"`
			Output     []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
			Choices []struct {
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(raw, &result) != nil {
			break
		}
		var counters struct {
			Input      *int64 `json:"input_tokens"`
			Output     *int64 `json:"output_tokens"`
			Prompt     *int64 `json:"prompt_tokens"`
			Completion *int64 `json:"completion_tokens"`
		}
		if !usage.Known && json.Unmarshal(result.Usage, &counters) == nil {
			input, output := counters.Input, counters.Output
			if input == nil {
				input = counters.Prompt
			}
			if output == nil {
				output = counters.Completion
			}
			if input != nil && output != nil && *input >= 0 && *output >= 0 {
				usage.Known = true
				usage.InputTokens = *input
				usage.OutputTokens = *output
				usage.TotalTokens = *input + *output
			}
		}
		if text == "" {
			text = result.OutputText
			if text == "" {
				for _, item := range result.Output {
					for _, content := range item.Content {
						text += content.Text
					}
				}
			}
			if text == "" && len(result.Choices) > 0 {
				content := result.Choices[0].Message.Content
				if json.Unmarshal(content, &text) != nil {
					var parts []struct {
						Text string `json:"text"`
					}
					if json.Unmarshal(content, &parts) == nil {
						for _, part := range parts {
							text += part.Text
						}
					}
				}
			}
		}
		if len(result.Data) == 0 {
			break
		}
		raw = result.Data
	}
	if len(text) > 64000 {
		runes := []rune(text)
		text = string(runes[:min(16000, len(runes))])
	}
	return usage, text
}

func flattenJSONStrings(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var s strings.Builder
		for _, item := range v {
			s.WriteString(flattenJSONStrings(item))
			s.WriteByte('\n')
		}
		return s.String()
	case map[string]any:
		var s strings.Builder
		for _, item := range v {
			s.WriteString(flattenJSONStrings(item))
			s.WriteByte('\n')
		}
		return s.String()
	}
	return ""
}
