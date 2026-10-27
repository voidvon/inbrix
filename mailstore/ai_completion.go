package mailstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	maxAIResponseBytes    = 2 << 20  // 2 MB
	maxAIExtraParamsBytes = 16 << 10 // 16 KB
)

// reservedAIExtraParamKeys are structural fields the app builds itself. Letting
// extra parameters replace them would break prompt construction or streaming,
// so they are rejected when the model is saved.
var reservedAIExtraParamKeys = map[string]struct{}{
	"model":             {},
	"messages":          {},
	"input":             {},
	"instructions":      {},
	"contents":          {},
	"systeminstruction": {},
	"stream":            {},
	"tools":             {},
	"tool_choice":       {},
	"toolconfig":        {},
}

// CreateAIResponse calls the configured model backend (OpenAI or Gemini) and returns the output text.
func CreateAIResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, maxOutputTokens int, effort string) (string, error) {
	return CreateAIResponseWithImages(ctx, client, model, apiKey, instructions, input, nil, maxOutputTokens, effort, nil)
}

// CreateAIResponseStream emits text as the provider generates it. JSON-only
// gateways remain supported, while other callers keep the non-streaming API.
func CreateAIResponseStream(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, maxOutputTokens int, effort string, onDelta func(string) error) (string, error) {
	return CreateAIResponseWithImages(ctx, client, model, apiKey, instructions, input, nil, maxOutputTokens, effort, onDelta)
}

// AIInputImage contains trusted image bytes resolved from owned attachments.
// Label associates each image with its original turn even after text compaction.
type AIInputImage struct {
	Label       string
	ContentType string
	Base64      string
}

func CreateAIResponseWithImages(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, images []AIInputImage, maxOutputTokens int, effort string, onDelta func(string) error) (string, error) {
	if onDelta == nil {
		return createAIResponse(ctx, client, model, apiKey, instructions, input, images, maxOutputTokens, effort, nil)
	}
	emitted := false
	text, err := createAIResponse(ctx, client, model, apiKey, instructions, input, images, maxOutputTokens, effort, func(delta string) error { emitted = true; return onDelta(delta) })
	if err == nil && !emitted && text != "" {
		err = onDelta(text)
	}
	return text, err
}

func createAIResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, images []AIInputImage, maxOutputTokens int, effort string, onDelta func(string) error) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(model.Provider))
	if provider == "" {
		provider = "openai"
	}
	if effort == "" {
		effort = model.ReasoningEffort
	}
	if effort == "" {
		effort = "medium"
	}
	if maxOutputTokens < 0 {
		maxOutputTokens = 0
	}
	if len(images) > 0 && !model.SupportsImageInput() {
		return "", errors.New("当前模型未启用图片输入，请在编辑模型中开启“支持图片”或选择其他模型 / Enable image input in model settings or select a vision model")
	}
	switch provider {
	case "gemini":
		return createGeminiResponse(ctx, client, model, apiKey, instructions, input, images, maxOutputTokens, effort, onDelta)
	case "openai":
		return createOpenAIResponse(ctx, client, model, apiKey, instructions, input, images, maxOutputTokens, effort, onDelta)
	case "openai-compatible":
		return createOpenAICompatibleResponse(ctx, client, model, apiKey, instructions, input, images, maxOutputTokens, effort, onDelta)
	default:
		return "", fmt.Errorf("unsupported AI provider: %q", model.Provider)
	}
}

// ValidateAIExtraParams checks that raw is an optional JSON object whose keys do
// not collide with the request fields the app generates itself.
func ValidateAIExtraParams(raw string) error {
	_, err := parseAIExtraParams(raw)
	return err
}

func parseAIExtraParams(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return nil, nil
	}
	if len(trimmed) > maxAIExtraParamsBytes {
		return nil, fmt.Errorf("extra parameters must be at most %d bytes", maxAIExtraParamsBytes)
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(trimmed), &params); err != nil {
		return nil, errors.New("extra parameters must be a valid JSON object")
	}
	if params == nil {
		return nil, errors.New("extra parameters must be a JSON object")
	}
	for key := range params {
		if _, reserved := reservedAIExtraParamKeys[strings.ToLower(strings.TrimSpace(key))]; reserved {
			return nil, fmt.Errorf("extra parameters cannot override %q", key)
		}
	}
	return params, nil
}

// marshalAIRequestBody serializes the app-built request and deep-merges the
// model's optional extra parameters on top. Extra parameters win on conflicts,
// except for the reserved structural keys rejected by ValidateAIExtraParams.
func marshalAIRequestBody(defaultBody any, extraRaw string) ([]byte, error) {
	raw, err := json.Marshal(defaultBody)
	if err != nil {
		return nil, err
	}
	extra, err := parseAIExtraParams(extraRaw)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return raw, nil
	}
	var base map[string]any
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, err
	}
	mergeJSONMaps(base, extra)
	return json.Marshal(base)
}

func mergeJSONMaps(base, override map[string]any) {
	for key, value := range override {
		if overrideMap, ok := value.(map[string]any); ok {
			if baseMap, ok := base[key].(map[string]any); ok {
				mergeJSONMaps(baseMap, overrideMap)
				continue
			}
		}
		base[key] = value
	}
}

// BuildGeminiURL constructs the endpoint for Gemini generateContent API calls.
func BuildGeminiURL(baseURL, model string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	cleanModel := strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if !strings.HasSuffix(baseURL, "/v1beta") && !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1beta"
	}
	return baseURL + "/models/" + url.PathEscape(cleanModel) + ":generateContent"
}

type geminiInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiPart struct {
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
	Text       string            `json:"text,omitempty"`
	Thought    bool              `json:"thought,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int                   `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent         `json:"systemInstruction,omitempty"`
	Contents          []geminiContent        `json:"contents"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig"`
}

func createGeminiResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, images []AIInputImage, maxOutputTokens int, effort string, onDelta func(string) error) (string, error) {
	endpoint := BuildGeminiURL(model.BaseURL, model.Model)
	if onDelta != nil {
		endpoint = strings.TrimSuffix(endpoint, ":generateContent") + ":streamGenerateContent?alt=sse"
	}

	doReq := func(includeThinking bool) (*http.Response, []byte, error) {
		reqBody := geminiRequest{
			Contents: []geminiContent{
				{
					Role:  "user",
					Parts: []geminiPart{{Text: input}},
				},
			},
			GenerationConfig: geminiGenerationConfig{
				MaxOutputTokens: maxOutputTokens,
			},
		}
		for _, img := range images {
			reqBody.Contents[0].Parts = append(reqBody.Contents[0].Parts, geminiPart{Text: img.Label}, geminiPart{InlineData: &geminiInlineData{MIMEType: img.ContentType, Data: img.Base64}})
		}
		if strings.TrimSpace(instructions) != "" {
			reqBody.SystemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: instructions}},
			}
		}
		if includeThinking && effort == "low" {
			reqBody.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{
				ThinkingBudget: 0,
			}
		}

		bodyBytes, err := marshalAIRequestBody(reqBody, model.ExtraParams)
		if err != nil {
			return nil, nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-goog-api-key", apiKey)

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, upstreamTransportError("Gemini", err)
		}
		defer resp.Body.Close()

		raw, err := readAIResponse(resp, "gemini", onDelta)
		if err != nil {
			return nil, nil, fmt.Errorf("read Gemini response: %w", err)
		}
		return resp, raw, nil
	}

	includeThinking := effort == "low"
	resp, raw, err := doReq(includeThinking)
	if err != nil {
		return "", err
	}

	// If thinkingConfig was rejected with 400 (e.g. non-thinking models), retry cleanly without it.
	if resp.StatusCode == http.StatusBadRequest && includeThinking {
		var checkErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &checkErr)
		if strings.Contains(strings.ToLower(checkErr.Error.Message), "thinking") {
			resp, raw, err = doReq(false)
			if err != nil {
				return "", err
			}
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error.Message != "" {
			return "", upstreamHTTPError("Gemini", resp, apiErr.Error.Message)
		}
		return "", upstreamHTTPError("Gemini", resp, "")
	}

	return geminiResponseText(raw)
}

func geminiResponseText(raw []byte) (string, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
				Role string `json:"role"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback *struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", errors.New("Gemini returned invalid JSON")
	}

	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("Gemini error: %s", resp.Error.Message)
	}

	if len(resp.Candidates) == 0 {
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			return "", fmt.Errorf("Gemini blocked response: %s", resp.PromptFeedback.BlockReason)
		}
		return "", errors.New("Gemini returned no candidates")
	}

	var parts []string
	for _, part := range resp.Candidates[0].Content.Parts {
		// Filter out internal thoughts emitted by thinking models
		if part.Thought {
			continue
		}
		if trimmed := strings.TrimSpace(part.Text); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	// Fallback in case only thought parts were present or thought flag wasn't set as expected
	if len(parts) == 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			if trimmed := strings.TrimSpace(part.Text); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
	}

	if len(parts) == 0 {
		if reason := resp.Candidates[0].FinishReason; reason != "" && reason != "STOP" {
			if reason == "MAX_TOKENS" {
				return "", errors.New("Gemini response was truncated: max output tokens limit reached during thinking or generation")
			}
			return "", fmt.Errorf("Gemini returned no output text (finish reason: %s)", reason)
		}
		return "", errors.New("Gemini returned no output text")
	}

	return strings.Join(parts, "\n"), nil
}

func createOpenAIResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, images []AIInputImage, maxOutputTokens int, effort string, onDelta func(string) error) (string, error) {
	payload := map[string]any{
		"stream":       onDelta != nil,
		"model":        model.Model,
		"instructions": instructions,
		"input":        input,
		"reasoning":    map[string]string{"effort": effort},
	}
	if len(images) > 0 {
		content := []map[string]any{{"type": "input_text", "text": input}}
		for _, img := range images {
			content = append(content, map[string]any{"type": "input_text", "text": img.Label}, map[string]any{"type": "input_image", "image_url": "data:" + img.ContentType + ";base64," + img.Base64})
		}
		payload["input"] = []map[string]any{{"role": "user", "content": content}}
	}
	if maxOutputTokens > 0 {
		payload["max_output_tokens"] = maxOutputTokens
	}
	body, err := marshalAIRequestBody(payload, model.ExtraParams)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(model.BaseURL, "/") + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", upstreamTransportError("OpenAI", err)
	}
	defer resp.Body.Close()
	raw, err := readAIResponse(resp, "openai", onDelta)
	if err != nil {
		return "", fmt.Errorf("read OpenAI response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error.Message != "" {
			return "", upstreamHTTPError("OpenAI", resp, apiErr.Error.Message)
		}
		return "", upstreamHTTPError("OpenAI", resp, "")
	}
	return openAIResponseText(raw)
}

// BuildOpenAICompatibleURL constructs the endpoint for OpenAI-compatible chat
// completions APIs (Cline, OpenRouter, local gateways, and similar services).
func BuildOpenAICompatibleURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	return baseURL + "/chat/completions"
}

type openAICompatibleMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type openAICompatibleRequest struct {
	Model     string                    `json:"model"`
	Messages  []openAICompatibleMessage `json:"messages"`
	MaxTokens int                       `json:"max_tokens,omitempty"`
	Stream    bool                      `json:"stream"`
}

type openAICompatibleChoice struct {
	Message struct {
		Content          string `json:"content"`
		ReasoningContent string `json:"reasoning_content"`
	} `json:"message"`
	FinishReason string          `json:"finish_reason"`
	Error        json.RawMessage `json:"error"`
}

// openAICompatibleResponse accepts both the standard OpenAI shape
// ({"choices":[...]}) and the envelope used by some gateways, including
// ClinePass, which nests the payload under "data" ({"data":{"choices":[...]}}).
type openAICompatibleResponse struct {
	Choices []openAICompatibleChoice  `json:"choices"`
	Error   json.RawMessage           `json:"error"`
	Data    *openAICompatibleResponse `json:"data"`
}

func (r *openAICompatibleResponse) choiceList() []openAICompatibleChoice {
	for depth := 0; r != nil && depth < 4; depth++ {
		if len(r.Choices) > 0 {
			return r.Choices
		}
		r = r.Data
	}
	return nil
}

func (r *openAICompatibleResponse) errorMessage() string {
	for depth := 0; r != nil && depth < 4; depth++ {
		if len(r.Error) > 0 {
			if msg := aiErrorValueMessage(r.Error); msg != "" {
				return msg
			}
		}
		r = r.Data
	}
	return ""
}

func openAICompatibleResponseError(raw []byte) string {
	if msg := openAICompatibleErrorMessage(raw); msg != "" {
		return msg
	}
	var result openAICompatibleResponse
	if err := json.Unmarshal(raw, &result); err == nil {
		return result.errorMessage()
	}
	return ""
}

// openAICompatibleErrorMessage extracts the human-readable message from the
// error envelopes used by OpenAI-compatible gateways: {"error":{"message":...}}
// or the shorter {"error":"..."}.
func openAICompatibleErrorMessage(raw []byte) string {
	var payload struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if len(payload.Error) > 0 {
		return aiErrorValueMessage(payload.Error)
	}
	return strings.TrimSpace(payload.Message)
}

// aiErrorValueMessage accepts both error shapes used by OpenAI-compatible
// gateways: a plain string or an object with message/code fields.
func aiErrorValueMessage(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var obj struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	message := strings.TrimSpace(obj.Message)
	code := strings.Trim(strings.TrimSpace(string(obj.Code)), `"`)
	if code != "" && code != "null" {
		if message == "" {
			return code
		}
		return code + ": " + message
	}
	return message
}

// truncateForError keeps diagnostic response snippets readable in error logs
// without dumping an entire upstream response.
func truncateForError(s string, limit int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func createOpenAICompatibleResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, images []AIInputImage, maxOutputTokens int, effort string, onDelta func(string) error) (string, error) {
	endpoint := BuildOpenAICompatibleURL(model.BaseURL)

	var msgs []openAICompatibleMessage
	if strings.TrimSpace(instructions) != "" {
		msgs = append(msgs, openAICompatibleMessage{Role: "system", Content: instructions})
	}
	if len(images) > 0 {
		content := []map[string]any{{"type": "text", "text": input}}
		for _, img := range images {
			content = append(content, map[string]any{"type": "text", "text": img.Label}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + img.ContentType + ";base64," + img.Base64}})
		}
		msgs = append(msgs, openAICompatibleMessage{Role: "user", Content: content})
	} else if strings.TrimSpace(input) != "" {
		msgs = append(msgs, openAICompatibleMessage{Role: "user", Content: input})
	}

	reqBody := openAICompatibleRequest{
		Model:     model.Model,
		Messages:  msgs,
		MaxTokens: maxOutputTokens,
		// Only interactive chat requests opt into streaming.
		Stream: onDelta != nil,
	}
	bodyBytes, err := marshalAIRequestBody(reqBody, model.ExtraParams)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", upstreamTransportError("OpenAI-compatible", err)
	}
	defer resp.Body.Close()

	raw, err := readAIResponse(resp, "openai-compatible", onDelta)
	if err != nil {
		return "", fmt.Errorf("read OpenAI-compatible response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if msg := openAICompatibleResponseError(raw); msg != "" {
			return "", upstreamHTTPError("OpenAI-compatible", resp, msg)
		}
		return "", upstreamHTTPError("OpenAI-compatible", resp, "")
	}

	var result openAICompatibleResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("OpenAI-compatible returned invalid JSON (response: %s)", truncateForError(string(raw), 200))
	}
	if msg := result.errorMessage(); msg != "" {
		return "", fmt.Errorf("OpenAI-compatible error: %s", msg)
	}
	choices := result.choiceList()
	if len(choices) == 0 {
		if snippet := strings.TrimSpace(string(raw)); snippet != "" {
			return "", fmt.Errorf("OpenAI-compatible returned no choices for model %q (response: %s)", model.Model, truncateForError(snippet, 400))
		}
		return "", fmt.Errorf("OpenAI-compatible returned no choices for model %q", model.Model)
	}
	if len(choices[0].Error) > 0 {
		if msg := aiErrorValueMessage(choices[0].Error); msg != "" {
			return "", fmt.Errorf("OpenAI-compatible error: %s", msg)
		}
	}

	content := StripThinkTags(strings.TrimSpace(choices[0].Message.Content))
	switch choices[0].FinishReason {
	case "length":
		return "", errors.New("OpenAI-compatible response was truncated: max tokens limit reached during reasoning or generation")
	case "content_filter":
		return "", errors.New("OpenAI-compatible response was filtered by content moderation policy")
	}
	if content == "" {
		switch reason := choices[0].FinishReason; reason {
		case "error":
			return "", errors.New("OpenAI-compatible finished with an error and no content")
		case "":
			return "", errors.New("OpenAI-compatible returned empty content")
		default:
			return "", fmt.Errorf("OpenAI-compatible returned empty content (finish reason: %s)", reason)
		}
	}
	return content, nil
}

var thinkTagRegex = regexp.MustCompile(`(?s)<think>.*?</think>`)

// StripThinkTags removes <think>...</think> reasoning blocks from model responses
// (emitted by some DeepSeek / reasoning providers in the content field).
// If an unclosed <think> tag exists (due to token truncation during thinking),
// everything from <think> onward is stripped.
func StripThinkTags(s string) string {
	s = thinkTagRegex.ReplaceAllString(s, "")
	if idx := strings.Index(s, "<think>"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// CleanJSONFence strips markdown code fences (```json ... ``` or ``` ... ```) from model output.
func CleanJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}
