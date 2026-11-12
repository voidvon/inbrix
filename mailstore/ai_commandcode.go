package mailstore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"inbrix/internal/agentruntime"
)

// Command Code Go uses the CLI protocol, not the paid Provider API protocol.
// Keep conversion in-process, shared by chat, model tests and mail tasks.
const CommandCodeBaseURL = "https://api.commandcode.ai"

func commandCodeRequest(model AIModelRecord, instructions string, messages []agentruntime.Message, tools []agentruntime.ToolSpec, images []AIInputImage, maxTokens int, effort string) ([]byte, error) {
	if len(images) > 0 && !model.SupportsImageInput() {
		return nil, errors.New("当前模型未启用图片输入 / Enable image input in model settings")
	}
	history := []any{}
	callNames := map[string]string{}
	for i, m := range messages {
		content := []any{}
		switch m.Role {
		case "user", "assistant":
			if m.Role == "assistant" && len(m.ProviderData) > 0 {
				var continuation struct {
					Reasoning string `json:"reasoning_content"`
				}
				if json.Unmarshal(m.ProviderData, &continuation) != nil {
					return nil, errors.New("invalid Command Code continuation")
				}
				if continuation.Reasoning != "" {
					content = append(content, map[string]any{"type": "reasoning", "text": continuation.Reasoning})
				}
			}
			if m.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Text})
			}
			if i == 0 && m.Role == "user" {
				for _, img := range images {
					content = append(content, map[string]any{"type": "text", "text": img.Label}, map[string]any{
						"type": "image", "image": "data:" + img.ContentType + ";base64," + img.Base64, "mimeType": img.ContentType,
					})
				}
			}
			for _, call := range m.ToolCalls {
				callNames[call.ID] = call.Name
				content = append(content, map[string]any{"type": "tool-call", "toolCallId": call.ID, "toolName": call.Name, "input": call.Args})
			}
		case "tool":
			name := callNames[m.ToolCallID]
			if name == "" {
				return nil, errors.New("Command Code tool result has no preceding tool call")
			}
			content = append(content, map[string]any{"type": "tool-result", "toolCallId": m.ToolCallID, "toolName": name,
				"output": map[string]any{"type": "text", "value": m.Text}})
		default:
			return nil, fmt.Errorf("unsupported Command Code message role: %q", m.Role)
		}
		if len(content) > 0 {
			history = append(history, map[string]any{"role": m.Role, "content": content})
		}
	}
	nativeTools := []any{}
	for _, tool := range tools {
		nativeTools = append(nativeTools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "input_schema": tool.Parameters})
	}
	var system any = ""
	if instructions != "" {
		system = []any{map[string]any{"type": "text", "text": instructions, "cache_control": map[string]string{"type": "ephemeral"}}}
	}
	if maxTokens <= 0 {
		maxTokens = 64000
	}
	params := map[string]any{"model": model.Model, "messages": history, "tools": nativeTools, "system": system, "max_tokens": min(maxTokens, 131072), "stream": true}
	if effort == "" {
		effort = model.ReasoningEffort
	}
	if effort != "" {
		params["reasoning_effort"] = effort
	}
	// Extra parameters belong inside params, where the CLI places generation
	// options. The shared validator protects structural fields such as system.
	extra, err := parseAIExtraParams(model.ExtraParams)
	if err != nil {
		return nil, err
	}
	mergeJSONMaps(params, extra)
	first := ""
	if len(messages) > 0 {
		first = messages[0].Text
	}
	digest := sha256.Sum256([]byte(model.ID + "\x00" + instructions + "\x00" + first))
	hash := hex.EncodeToString(digest[:])
	threadID := fmt.Sprintf("%s-%s-5%s-a%s-%s", hash[:8], hash[8:12], hash[13:16], hash[17:20], hash[20:32])
	return json.Marshal(map[string]any{
		"config": map[string]any{
			"workingDir": "inbrix", "date": time.Now().UTC().Format("2006-01-02"),
			"environment": runtime.GOOS + "-" + runtime.GOARCH + ", Go " + runtime.Version(),
			"structure":   []any{}, "isGitRepo": false, "currentBranch": "", "mainBranch": "", "gitStatus": "", "recentCommits": []any{},
		},
		"memory": nil, "taste": nil, "skills": nil, "permissionMode": "standard", "params": params, "threadId": threadID,
	})
}

func createCommandCodeResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions string, messages []agentruntime.Message, tools []agentruntime.ToolSpec, images []AIInputImage, maxTokens int, effort string, onDelta func(string) error) (agentruntime.Message, error) {
	var empty agentruntime.Message
	body, err := commandCodeRequest(model, instructions, messages, tools, images, maxTokens, effort)
	if err != nil {
		return empty, err
	}
	base := strings.TrimRight(strings.TrimSpace(model.BaseURL), "/")
	if base == "" {
		base = CommandCodeBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/alpha/generate", bytes.NewReader(body))
	if err != nil {
		return empty, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "inbrix-commandcode/1.0")
	req.Header.Set("x-command-code-version", "1.88.5")
	req.Header.Set("x-cli-environment", "production")
	req.Header.Set("x-project-slug", "inbrix")
	req.Header.Set("x-taste-learning", "false")
	req.Header.Set("x-co-flag", "false")
	resp, err := client.Do(req)
	if err != nil {
		return empty, upstreamTransportError("Command Code", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAIResponseBytes))
		if err != nil {
			return empty, err
		}
		return empty, upstreamHTTPError("Command Code", resp, openAICompatibleResponseError(raw))
	}
	return readCommandCodeResponse(resp.Body, onDelta)
}

// The CLI returns bare JSON lines, including when Content-Type claims SSE.
// Some gateways frame these same events as SSE; support both representations.
func readCommandCodeEvents(reader io.Reader, handle func([]byte) error) error {
	limited := &io.LimitedReader{R: reader, N: 16 * maxAIResponseBytes}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), maxAIResponseBytes)
	var frame strings.Builder
	dispatch := func() error {
		data := strings.TrimSpace(frame.String())
		frame.Reset()
		if data == "" || data == "[DONE]" {
			return nil
		}
		return handle([]byte(data))
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			if err := dispatch(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "data:"):
			if frame.Len()+len(line) > maxAIResponseBytes {
				return errors.New("Command Code stream frame exceeded output budget")
			}
			frame.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			frame.WriteByte('\n')
		case strings.HasPrefix(line, "{"):
			if err := dispatch(); err != nil {
				return err
			}
			if err := handle([]byte(line)); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"), strings.HasPrefix(line, "event:"), strings.HasPrefix(line, "id:"), strings.HasPrefix(line, "retry:"):
			// SSE metadata and heartbeat comments.
		default:
			return errors.New("Command Code stream returned an invalid frame")
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if limited.N == 0 {
		return errors.New("Command Code stream exceeded wire budget")
	}
	return dispatch()
}

func readCommandCodeResponse(reader io.Reader, onDelta func(string) error) (agentruntime.Message, error) {
	out := agentruntime.Message{Role: "assistant"}
	var text, reasoning strings.Builder
	complete, size := false, 0
	err := readCommandCodeEvents(reader, func(raw []byte) error {
		var event struct {
			Type       string          `json:"type"`
			Text       string          `json:"text"`
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Input      json.RawMessage `json:"input"`
			Args       json.RawMessage `json:"args"`
			Arguments  json.RawMessage `json:"arguments"`
			Error      json.RawMessage `json:"error"`
			Message    string          `json:"message"`
			Finish     string          `json:"finishReason"`
		}
		if json.Unmarshal(raw, &event) != nil {
			return errors.New("Command Code stream returned invalid JSON")
		}
		if msg := aiErrorValueMessage(event.Error); msg != "" {
			return fmt.Errorf("Command Code error: %s", msg)
		}
		switch event.Type {
		case "text-delta", "reasoning-delta", "tool-call":
			if complete {
				return errors.New("Command Code returned content after completion")
			}
			size += len(raw)
			if size > maxAIResponseBytes {
				return errors.New("Command Code response exceeded output budget")
			}
		}
		switch event.Type {
		case "text-delta":
			text.WriteString(event.Text)
			if onDelta != nil && event.Text != "" {
				return onDelta(event.Text)
			}
		case "reasoning-delta":
			reasoning.WriteString(event.Text)
		case "tool-call":
			if len(out.ToolCalls) >= 128 {
				return errors.New("Command Code stream tool budget exceeded")
			}
			args := event.Input
			if len(args) == 0 {
				args = event.Args
			}
			if len(args) == 0 {
				args = event.Arguments
			}
			out.ToolCalls = append(out.ToolCalls, nativeCall(event.ToolCallID, event.ToolName, args))
		case "finish":
			if event.Finish != "stop" && event.Finish != "tool-calls" && event.Finish != "tool_calls" {
				return fmt.Errorf("Command Code response stopped before completion: %s", event.Finish)
			}
			complete = true
		case "error", "abort":
			return fmt.Errorf("Command Code stream %s: %s", event.Type, event.Message)
		}
		return nil
	})
	if err != nil {
		return agentruntime.Message{}, err
	}
	if !complete {
		return agentruntime.Message{}, errors.New("Command Code stream interrupted before completion")
	}
	out.Text = StripThinkTags(text.String())
	if reasoning.Len() > 0 {
		out.ProviderData, _ = json.Marshal(map[string]string{"reasoning_content": reasoning.String()})
	}
	if out.Text == "" && len(out.ToolCalls) == 0 {
		return agentruntime.Message{}, errors.New("Command Code returned neither text nor tool calls")
	}
	return out, nil
}
