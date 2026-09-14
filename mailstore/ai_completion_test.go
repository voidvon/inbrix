package mailstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildGeminiURL(t *testing.T) {
	tests := []struct {
		baseURL string
		model   string
		want    string
	}{
		{
			baseURL: "https://generativelanguage.googleapis.com",
			model:   "gemini-3.8-flash",
			want:    "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent",
		},
		{
			baseURL: "https://generativelanguage.googleapis.com/v1beta",
			model:   "gemini-3.8-flash",
			want:    "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent",
		},
		{
			baseURL: "https://generativelanguage.googleapis.com/v1beta/",
			model:   "models/gemini-3.8-flash",
			want:    "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent",
		},
		{
			baseURL: "",
			model:   "gemini-3.8-flash",
			want:    "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent",
		},
		{
			baseURL: "https://proxy.example.com/api",
			model:   "gemini-3.8-flash",
			want:    "https://proxy.example.com/api/v1beta/models/gemini-3.8-flash:generateContent",
		},
	}

	for _, tt := range tests {
		got := BuildGeminiURL(tt.baseURL, tt.model)
		if got != tt.want {
			t.Errorf("BuildGeminiURL(%q, %q) = %q; want %q", tt.baseURL, tt.model, got, tt.want)
		}
	}
}

func TestCreateGeminiResponseHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1beta/models/gemini-3.8-flash:generateContent" {
			t.Errorf("path: got %s", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "gemini-key-123" {
			t.Errorf("api key header: got %q", got)
		}

		var req struct {
			SystemInstruction struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"systemInstruction"`
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				MaxOutputTokens int `json:"maxOutputTokens"`
			} `json:"generationConfig"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if len(req.Contents) == 0 || req.Contents[0].Parts[0].Text != "user prompt input" {
			t.Errorf("unexpected content: %+v", req.Contents)
		}
		if req.SystemInstruction.Parts[0].Text != "system prompt instructions" {
			t.Errorf("unexpected instruction: %+v", req.SystemInstruction)
		}
		if req.GenerationConfig.MaxOutputTokens != 800 {
			t.Errorf("unexpected max output tokens: %d", req.GenerationConfig.MaxOutputTokens)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [
				{
					"content": {
						"role": "model",
						"parts": [
							{"text": "internal thinking process...", "thought": true},
							{"text": "Gemini generated answer"}
						]
					},
					"finishReason": "STOP"
				}
			]
		}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "gemini",
		BaseURL:  server.URL,
		Model:    "gemini-3.8-flash",
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "gemini-key-123", "system prompt instructions", "user prompt input", 800, "medium")
	if err != nil {
		t.Fatalf("CreateAIResponse: %v", err)
	}

	if got != "Gemini generated answer" {
		t.Fatalf("got %q, want %q", got, "Gemini generated answer")
	}
}

func TestCreateGeminiResponseThinkingFallback(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		genCfg, _ := body["generationConfig"].(map[string]any)

		if attempts == 1 {
			// First attempt sends thinkingConfig, server simulates older model rejecting it
			if genCfg["thinkingConfig"] == nil {
				t.Errorf("first attempt should include thinkingConfig")
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error": {"code": 400, "message": "Unknown field: thinkingConfig", "status": "INVALID_ARGUMENT"}}`)
			return
		}

		// Second attempt should succeed without thinkingConfig
		if genCfg["thinkingConfig"] != nil {
			t.Errorf("retried request should omit thinkingConfig")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"candidates": [{"content": {"parts": [{"text": "Fallback answer"}]}}]}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "gemini",
		BaseURL:  server.URL,
		Model:    "gemini-3.8-flash",
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 800, "low")
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if got != "Fallback answer" {
		t.Fatalf("got %q, want 'Fallback answer'", got)
	}
}

func TestCreateGeminiResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": {"code": 400, "message": "API key not valid. Please pass a valid API key.", "status": "INVALID_ARGUMENT"}}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "gemini",
		BaseURL:  server.URL,
		Model:    "gemini-3.8-flash",
	}

	_, err := CreateAIResponse(context.Background(), server.Client(), model, "invalid-key", "inst", "inp", 800, "medium")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCleanJSONFence(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "```json\n{\"key\": \"val\"}\n```",
			want:  "{\"key\": \"val\"}",
		},
		{
			input: "```\n{\"key\": \"val\"}\n```",
			want:  "{\"key\": \"val\"}",
		},
		{
			input: "{\"key\": \"val\"}",
			want:  "{\"key\": \"val\"}",
		},
	}

	for _, tt := range tests {
		got := CleanJSONFence(tt.input)
		if got != tt.want {
			t.Errorf("CleanJSONFence(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildDeepSeekURL(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{
			baseURL: "https://api.deepseek.com",
			want:    "https://api.deepseek.com/chat/completions",
		},
		{
			baseURL: "https://api.deepseek.com/v1",
			want:    "https://api.deepseek.com/v1/chat/completions",
		},
		{
			baseURL: "https://api.deepseek.com/chat/completions",
			want:    "https://api.deepseek.com/chat/completions",
		},
		{
			baseURL: "",
			want:    "https://api.deepseek.com/chat/completions",
		},
	}

	for _, tt := range tests {
		got := BuildDeepSeekURL(tt.baseURL)
		if got != tt.want {
			t.Errorf("BuildDeepSeekURL(%q) = %q; want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestCreateDeepSeekResponseHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ds-test-key" {
			t.Errorf("authorization header: got %q", got)
		}

		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens int `json:"max_tokens"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if req.Model != "deepseek-chat" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if len(req.Messages) != 2 || req.Messages[0].Content != "system prompt" || req.Messages[1].Content != "user query" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"choices": [
				{
					"message": {
						"role": "assistant",
						"content": "DeepSeek generated response",
						"reasoning_content": "DeepSeek reasoning chain..."
					},
					"finish_reason": "stop"
				}
			]
		}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "deepseek",
		BaseURL:  server.URL,
		Model:    "deepseek-chat",
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "ds-test-key", "system prompt", "user query", 1000, "medium")
	if err != nil {
		t.Fatalf("CreateAIResponse: %v", err)
	}

	if got != "DeepSeek generated response" {
		t.Fatalf("got %q, want 'DeepSeek generated response'", got)
	}
}

func TestCreateDeepSeekResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": {"message": "Authentication failed", "type": "authentication_error"}}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "deepseek",
		BaseURL:  server.URL,
		Model:    "deepseek-chat",
	}

	_, err := CreateAIResponse(context.Background(), server.Client(), model, "bad-key", "inst", "inp", 800, "medium")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Authentication failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCreateDeepSeekResponseTruncatedByLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"choices": [
				{
					"message": {
						"role": "assistant",
						"content": "",
						"reasoning_content": "partial thinking..."
					},
					"finish_reason": "length"
				}
			]
		}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "deepseek",
		BaseURL:  server.URL,
		Model:    "deepseek-reasoner",
	}

	_, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 500, "medium")
	if err == nil {
		t.Fatal("expected error for empty content with finish_reason length, got nil")
	}
	if !strings.Contains(err.Error(), "max tokens limit reached during reasoning or generation") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCreateDeepSeekResponseDefaultMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if maxTok, ok := req["max_tokens"].(float64); !ok || int(maxTok) != 8192 {
			t.Errorf("expected max_tokens to default to 8192 when maxOutputTokens <= 0, got: %v", req["max_tokens"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"choices": [
				{
					"message": {
						"role": "assistant",
						"content": "ok"
					},
					"finish_reason": "stop"
				}
			]
		}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "deepseek",
		BaseURL:  server.URL,
		Model:    "deepseek-flash",
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 0, "medium")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want 'ok'", got)
	}
}

func TestStripThinkTags(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "<think>\nThinking about the problem...\n</think>\nActual response",
			want:  "Actual response",
		},
		{
			input: "Direct response without think tags",
			want:  "Direct response without think tags",
		},
		{
			input: "<think>\nUnclosed think tag due to length truncation",
			want:  "",
		},
		{
			input: "<think>thinking</think>",
			want:  "",
		},
	}
	for _, tt := range tests {
		got := StripThinkTags(tt.input)
		if got != tt.want {
			t.Errorf("StripThinkTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
