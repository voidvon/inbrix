package mailstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	maxAIResponseBytes = 2 << 20 // 2 MB
)

// CreateAIResponse calls the configured model backend (OpenAI or Gemini) and returns the output text.
func CreateAIResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, maxOutputTokens int, effort string) (string, error) {
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
	switch provider {
	case "gemini":
		return createGeminiResponse(ctx, client, model, apiKey, instructions, input, maxOutputTokens, effort)
	case "deepseek":
		return createDeepSeekResponse(ctx, client, model, apiKey, instructions, input, maxOutputTokens, effort)
	case "openai":
		return createOpenAIResponse(ctx, client, model, apiKey, instructions, input, maxOutputTokens, effort)
	default:
		return "", fmt.Errorf("unsupported AI provider: %q", model.Provider)
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

type geminiPart struct {
	Text    string `json:"text,omitempty"`
	Thought bool   `json:"thought,omitempty"`
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

func createGeminiResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, maxOutputTokens int, effort string) (string, error) {
	endpoint := BuildGeminiURL(model.BaseURL, model.Model)

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

		bodyBytes, err := json.Marshal(reqBody)
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
			return nil, nil, fmt.Errorf("Gemini request failed: %w", err)
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAIResponseBytes))
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
			return "", fmt.Errorf("Gemini returned HTTP %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		return "", fmt.Errorf("Gemini returned HTTP %d", resp.StatusCode)
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

func createOpenAIResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, maxOutputTokens int, effort string) (string, error) {
	payload := map[string]any{
		"model":        model.Model,
		"instructions": instructions,
		"input":        input,
		"reasoning":    map[string]string{"effort": effort},
	}
	if maxOutputTokens > 0 {
		payload["max_output_tokens"] = maxOutputTokens
	}
	body, err := json.Marshal(payload)
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
		return "", fmt.Errorf("OpenAI request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAIResponseBytes))
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
			return "", fmt.Errorf("OpenAI returned HTTP %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		return "", fmt.Errorf("OpenAI returned HTTP %d", resp.StatusCode)
	}
	return openAIResponseText(raw)
}

// BuildDeepSeekURL constructs the endpoint for DeepSeek chat completions API calls.
func BuildDeepSeekURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	return baseURL + "/chat/completions"
}

type deepSeekChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepSeekRequest struct {
	Model     string                `json:"model"`
	Messages  []deepSeekChatMessage `json:"messages"`
	MaxTokens int                   `json:"max_tokens,omitempty"`
	Stream    bool                  `json:"stream"`
}

type deepSeekResponse struct {
	Choices []struct {
		Message struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func createDeepSeekResponse(ctx context.Context, client HTTPClient, model AIModelRecord, apiKey, instructions, input string, maxOutputTokens int, effort string) (string, error) {
	endpoint := BuildDeepSeekURL(model.BaseURL)

	var msgs []deepSeekChatMessage
	if strings.TrimSpace(instructions) != "" {
		msgs = append(msgs, deepSeekChatMessage{Role: "system", Content: instructions})
	}
	if strings.TrimSpace(input) != "" {
		msgs = append(msgs, deepSeekChatMessage{Role: "user", Content: input})
	}

	// For DeepSeek (including V3, R1 / reasoner models, and proxies like deepseek-flash),
	// reasoning models consume substantial tokens during thinking. We ensure max_tokens is
	// always 8192 (DeepSeek's maximum output limit) so reasoning never starves generation.
	if maxOutputTokens < 8192 {
		maxOutputTokens = 8192
	} else if maxOutputTokens > 8192 {
		maxOutputTokens = 8192
	}

	reqBody := deepSeekRequest{
		Model:     model.Model,
		Messages:  msgs,
		MaxTokens: maxOutputTokens,
		Stream:    false,
	}

	bodyBytes, err := json.Marshal(reqBody)
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
		return "", fmt.Errorf("DeepSeek request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAIResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read DeepSeek response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr deepSeekResponse
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Error != nil && apiErr.Error.Message != "" {
			return "", fmt.Errorf("DeepSeek returned HTTP %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		return "", fmt.Errorf("DeepSeek returned HTTP %d", resp.StatusCode)
	}

	var result deepSeekResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", errors.New("DeepSeek returned invalid JSON")
	}

	if result.Error != nil && result.Error.Message != "" {
		return "", fmt.Errorf("DeepSeek error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", errors.New("DeepSeek returned no choices")
	}

	content := strings.TrimSpace(result.Choices[0].Message.Content)
	content = StripThinkTags(content)
	if content == "" {
		if len(result.Choices) > 0 {
			switch result.Choices[0].FinishReason {
			case "length":
				return "", errors.New("DeepSeek response was truncated: max tokens limit reached during reasoning or generation")
			case "content_filter":
				return "", errors.New("DeepSeek response was filtered by content moderation policy")
			}
		}
		return "", errors.New("DeepSeek returned empty content")
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
