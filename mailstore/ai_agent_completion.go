package mailstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"inbrix/internal/agentruntime"
)

// CreateAgentResponse adapts the shared typed conversation to native provider
// protocols. Tool calls are never inferred from prose, fences, or tool logs.
func CreateAgentResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions string, input agentruntime.ModelRequest, images []AIInputImage, maxTokens int, effort string, onDelta func(string) error) (agentruntime.ModelResponse, error) {
	provider := strings.ToLower(strings.TrimSpace(model.Provider))
	if provider == "" {
		provider = "openai"
	}
	tools := input.Tools
	if input.DisableTools {
		tools = nil
		for _, t := range input.Tools {
			if t.Name == "submit_result" {
				tools = append(tools, t)
			}
		}
	}
	payload, endpoint, err := agentRequest(model, provider, instructions, input.Messages, tools, input.DisableTools, images, maxTokens, effort, onDelta != nil)
	if err != nil {
		return agentruntime.ModelResponse{}, err
	}
	body, err := marshalAIRequestBody(payload, model.ExtraParams)
	if err != nil {
		return agentruntime.ModelResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return agentruntime.ModelResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider == "gemini" {
		req.Header.Set("x-goog-api-key", apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return agentruntime.ModelResponse{}, upstreamTransportError(provider, err)
	}
	defer resp.Body.Close()
	var raw []byte
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		raw, err = readAgentStream(resp.Body, provider, onDelta)
	} else {
		raw, err = io.ReadAll(io.LimitReader(resp.Body, maxAIResponseBytes+1))
		if len(raw) > maxAIResponseBytes {
			err = errors.New("AI response exceeded output budget")
		}
	}
	if err != nil {
		return agentruntime.ModelResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agentruntime.ModelResponse{}, upstreamHTTPError(provider, resp, openAICompatibleResponseError(raw))
	}
	message, err := decodeAgentMessage(raw, provider, len(input.Messages))
	if err != nil {
		return agentruntime.ModelResponse{}, err
	}
	if onDelta != nil && !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") && message.Text != "" {
		if err := onDelta(message.Text); err != nil {
			return agentruntime.ModelResponse{}, err
		}
	}
	return agentruntime.ModelResponse{Message: message}, nil
}

func agentRequest(model AIModelRecord, provider, instructions string, messages []agentruntime.Message, tools []agentruntime.ToolSpec, disabled bool, images []AIInputImage, maxTokens int, effort string, stream bool) (map[string]any, string, error) {
	nativeTools := []any{}
	for _, tool := range tools {
		function := map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters}
		if provider == "openai" {
			function["type"] = "function"
			function["strict"] = false
			nativeTools = append(nativeTools, function)
		} else {
			nativeTools = append(nativeTools, map[string]any{"type": "function", "function": function})
		}
	}
	switch provider {
	case "openai":
		history := []any{}
		for i, m := range messages {
			if m.Role == "assistant" && len(m.ProviderData) > 0 {
				var items []any
				if json.Unmarshal(m.ProviderData, &items) != nil {
					return nil, "", errors.New("invalid provider continuation")
				}
				history = append(history, items...)
				continue
			}
			if m.Role == "tool" {
				history = append(history, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Text})
				continue
			}
			if m.Text != "" {
				kind := "input_text"
				if m.Role == "assistant" {
					kind = "output_text"
				}
				content := []any{map[string]any{"type": kind, "text": m.Text}}
				if i == 0 {
					for _, img := range images {
						content = append(content, map[string]any{"type": "input_text", "text": img.Label}, map[string]any{"type": "input_image", "image_url": "data:" + img.ContentType + ";base64," + img.Base64})
					}
				}
				history = append(history, map[string]any{"role": m.Role, "content": content})
			}
			for _, call := range m.ToolCalls {
				args, _ := json.Marshal(call.Args)
				history = append(history, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(args)})
			}
		}
		p := map[string]any{"model": model.Model, "instructions": instructions, "input": history, "stream": stream, "tools": nativeTools}
		if maxTokens > 0 {
			p["max_output_tokens"] = maxTokens
		}
		if effort != "" {
			p["reasoning"] = map[string]string{"effort": effort}
		}
		if disabled {
			if len(tools) > 0 {
				p["tool_choice"] = map[string]any{"type": "function", "name": "submit_result"}
			} else {
				p["tool_choice"] = "none"
			}
		}
		return p, strings.TrimRight(model.BaseURL, "/") + "/responses", nil
	case "deepseek", "openai-compatible":
		if provider == "deepseek" && len(images) > 0 {
			return nil, "", errors.New("当前 DeepSeek 接口不支持图片输入，请选择支持图片的模型")
		}
		history := []any{map[string]any{"role": "system", "content": instructions}}
		for i, m := range messages {
			item := map[string]any{"role": m.Role, "content": m.Text}
			if m.Role == "tool" {
				item["tool_call_id"] = m.ToolCallID
			}
			if len(m.ToolCalls) > 0 {
				calls := []any{}
				for _, call := range m.ToolCalls {
					args, _ := json.Marshal(call.Args)
					calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(args)}})
				}
				item["tool_calls"] = calls
			}
			if len(m.ProviderData) > 0 {
				var data map[string]any
				if json.Unmarshal(m.ProviderData, &data) != nil {
					return nil, "", errors.New("invalid provider continuation")
				}
				if reasoning, ok := data["reasoning_content"]; ok {
					item["reasoning_content"] = reasoning
				}
			}
			if i == 0 && len(images) > 0 {
				content := []any{map[string]any{"type": "text", "text": m.Text}}
				for _, img := range images {
					content = append(content, map[string]any{"type": "text", "text": img.Label}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + img.ContentType + ";base64," + img.Base64}})
				}
				item["content"] = content
			}
			history = append(history, item)
		}
		p := map[string]any{"model": model.Model, "messages": history, "stream": stream, "tools": nativeTools}
		if provider == "deepseek" {
			p["max_tokens"] = 8192
		} else if maxTokens > 0 {
			p["max_tokens"] = maxTokens
		}
		if disabled {
			if len(tools) > 0 {
				p["tool_choice"] = map[string]any{"type": "function", "function": map[string]string{"name": "submit_result"}}
			} else {
				p["tool_choice"] = "none"
			}
		}
		endpoint := BuildOpenAICompatibleURL(model.BaseURL)
		if provider == "deepseek" {
			endpoint = BuildDeepSeekURL(model.BaseURL)
		}
		return p, endpoint, nil
	case "gemini":
		contents := []any{}
		providerCallIDs := map[string]bool{}
		appendContent := func(role string, parts []any) {
			if len(contents) > 0 {
				last := contents[len(contents)-1].(map[string]any)
				if last["role"] == role {
					last["parts"] = append(last["parts"].([]any), parts...)
					return
				}
			}
			contents = append(contents, map[string]any{"role": role, "parts": parts})
		}
		for i, m := range messages {
			if m.Role == "assistant" && len(m.ProviderData) > 0 {
				var parts []any
				if json.Unmarshal(m.ProviderData, &parts) != nil {
					return nil, "", errors.New("invalid provider continuation")
				}
				for _, value := range parts {
					if part, ok := value.(map[string]any); ok {
						if call, ok := part["functionCall"].(map[string]any); ok {
							if id, ok := call["id"].(string); ok && id != "" {
								providerCallIDs[id] = true
							}
						}
					}
				}
				appendContent("model", parts)
				continue
			}
			role := m.Role
			if role == "assistant" {
				role = "model"
			}
			if role == "tool" {
				role = "user"
			}
			parts := []any{}
			if m.Role == "tool" {
				var result any
				_ = json.Unmarshal([]byte(m.Text), &result)
				response := map[string]any{"name": m.Name, "response": map[string]any{"result": result}}
				if providerCallIDs[m.ToolCallID] {
					response["id"] = m.ToolCallID
				}
				parts = append(parts, map[string]any{"functionResponse": response})
			} else {
				if m.Text != "" {
					parts = append(parts, map[string]any{"text": m.Text})
				}
				for _, call := range m.ToolCalls {
					parts = append(parts, map[string]any{"functionCall": map[string]any{"id": call.ID, "name": call.Name, "args": call.Args}})
					providerCallIDs[call.ID] = true
				}
			}
			if i == 0 {
				for _, img := range images {
					parts = append(parts, map[string]any{"text": img.Label}, map[string]any{"inlineData": map[string]string{"mimeType": img.ContentType, "data": img.Base64}})
				}
			}
			appendContent(role, parts)
		}
		declarations := []any{}
		for _, tool := range tools {
			declarations = append(declarations, map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters})
		}
		p := map[string]any{"systemInstruction": map[string]any{"parts": []any{map[string]string{"text": instructions}}}, "contents": contents, "generationConfig": map[string]any{"maxOutputTokens": maxTokens}}
		if len(declarations) > 0 {
			p["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
		}
		if disabled {
			mode := "NONE"
			fc := map[string]any{}
			if len(tools) > 0 {
				mode = "ANY"
				fc["allowedFunctionNames"] = []string{"submit_result"}
			}
			fc["mode"] = mode
			p["toolConfig"] = map[string]any{"functionCallingConfig": fc}
		}
		endpoint := BuildGeminiURL(model.BaseURL, model.Model)
		if stream {
			endpoint = strings.TrimSuffix(endpoint, ":generateContent") + ":streamGenerateContent?alt=sse"
		}
		return p, endpoint, nil
	default:
		return nil, "", fmt.Errorf("unsupported AI provider: %q", provider)
	}
}

func nativeCall(id, name string, arguments json.RawMessage) agentruntime.ToolCall {
	call := agentruntime.ToolCall{ID: id, Name: name}
	var text string
	if json.Unmarshal(arguments, &text) == nil {
		arguments = []byte(text)
	}
	if json.Unmarshal(arguments, &call.Args) != nil || call.Args == nil {
		call.ArgumentError = "tool arguments must be a JSON object"
		call.Args = map[string]any{}
	}
	return call
}

func decodeAgentMessage(raw []byte, provider string, step int) (agentruntime.Message, error) {
	out := agentruntime.Message{Role: "assistant"}
	var envelope struct {
		Error      json.RawMessage `json:"error"`
		Data       json.RawMessage `json:"data"`
		Status     string          `json:"status"`
		OutputText string          `json:"output_text"`
		Output     json.RawMessage `json:"output"`
		Choices    []struct {
			Message struct {
				Content          string  `json:"content"`
				ReasoningContent *string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string          `json:"finish_reason"`
			Error        json.RawMessage `json:"error"`
		} `json:"choices"`
		Candidates []struct {
			Content struct {
				Parts json.RawMessage `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	for depth := 0; depth < 4; depth++ {
		if json.Unmarshal(raw, &envelope) != nil {
			return out, errors.New("AI returned invalid response JSON")
		}
		if msg := aiErrorValueMessage(envelope.Error); msg != "" {
			return out, fmt.Errorf("AI error: %s", msg)
		}
		if len(envelope.Data) == 0 {
			break
		}
		raw = envelope.Data
		envelope.Data = nil
	}
	switch provider {
	case "openai":
		if envelope.Status != "" && envelope.Status != "completed" {
			return out, errors.New("AI response was incomplete or failed")
		}
		// API uses snake_case for call_id, unlike Go's automatic field matching.
		var wire []struct {
			Type      string          `json:"type"`
			Name      string          `json:"name"`
			CallID    string          `json:"call_id"`
			Arguments json.RawMessage `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if len(envelope.Output) > 0 && json.Unmarshal(envelope.Output, &wire) != nil {
			return out, errors.New("AI returned invalid output items")
		}
		var texts []string
		for _, item := range wire {
			if item.Type == "function_call" {
				out.ToolCalls = append(out.ToolCalls, nativeCall(item.CallID, item.Name, item.Arguments))
			}
			if item.Type == "message" {
				for _, c := range item.Content {
					if c.Type == "output_text" {
						texts = append(texts, c.Text)
					} else if c.Type == "refusal" {
						return out, errors.New("AI refused the request")
					}
				}
			}
		}
		out.Text = strings.Join(texts, "\n")
		if out.Text == "" {
			out.Text = envelope.OutputText
		}
		out.ProviderData = envelope.Output
	case "deepseek", "openai-compatible":
		if len(envelope.Choices) == 0 {
			return out, errors.New("AI returned no choices")
		}
		choice := envelope.Choices[0]
		if msg := aiErrorValueMessage(choice.Error); msg != "" {
			return out, fmt.Errorf("AI error: %s", msg)
		}
		if choice.FinishReason != "" && choice.FinishReason != "stop" && choice.FinishReason != "tool_calls" {
			return out, fmt.Errorf("AI response stopped before completion: %s", choice.FinishReason)
		}
		out.Text = choice.Message.Content
		for _, call := range choice.Message.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, nativeCall(call.ID, call.Function.Name, call.Function.Arguments))
		}
		if choice.Message.ReasoningContent != nil {
			out.ProviderData, _ = json.Marshal(map[string]any{"reasoning_content": *choice.Message.ReasoningContent})
		}
	case "gemini":
		if len(envelope.Candidates) == 0 {
			return out, errors.New("Gemini returned no candidates")
		}
		candidate := envelope.Candidates[0]
		if candidate.FinishReason != "" && candidate.FinishReason != "STOP" {
			return out, fmt.Errorf("Gemini response stopped: %s", candidate.FinishReason)
		}
		var parts []struct {
			Text         string `json:"text"`
			Thought      bool   `json:"thought"`
			FunctionCall *struct {
				ID   string          `json:"id"`
				Name string          `json:"name"`
				Args json.RawMessage `json:"args"`
			} `json:"functionCall"`
		}
		if json.Unmarshal(candidate.Content.Parts, &parts) != nil {
			return out, errors.New("Gemini returned invalid content")
		}
		for i, p := range parts {
			if !p.Thought {
				out.Text += p.Text
			}
			if c := p.FunctionCall; c != nil {
				id := c.ID
				if id == "" {
					id = fmt.Sprintf("gemini_%d_%d", step, i)
				}
				out.ToolCalls = append(out.ToolCalls, nativeCall(id, c.Name, c.Args))
			}
		}
		out.ProviderData = candidate.Content.Parts
	}
	out.Text = StripThinkTags(out.Text)
	if out.Text == "" && len(out.ToolCalls) == 0 {
		return out, errors.New("AI returned neither text nor tool calls")
	}
	return out, nil
}
