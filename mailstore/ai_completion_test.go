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

func TestBuildOpenAICompatibleURL(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{
			baseURL: "https://api.cline.bot/api/v1",
			want:    "https://api.cline.bot/api/v1/chat/completions",
		},
		{
			baseURL: "https://api.cline.bot/api/v1/",
			want:    "https://api.cline.bot/api/v1/chat/completions",
		},
		{
			baseURL: "https://api.cline.bot/api/v1/chat/completions",
			want:    "https://api.cline.bot/api/v1/chat/completions",
		},
		{
			baseURL: "",
			want:    "https://api.openai.com/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		got := BuildOpenAICompatibleURL(tt.baseURL)
		if got != tt.want {
			t.Errorf("BuildOpenAICompatibleURL(%q) = %q; want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestCreateOpenAICompatibleResponseHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/chat/completions" {
			t.Errorf("path: got %s, want /api/v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer cline-key" {
			t.Errorf("authorization header: got %q", got)
		}

		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens int  `json:"max_tokens"`
			Stream    bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "cline-pass/deepseek-v4.1-flash-1017" {
			t.Errorf("unexpected model: %q", req.Model)
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}
		if req.MaxTokens != 4096 {
			t.Errorf("max_tokens: got %d, want 4096", req.MaxTokens)
		}
		if req.Stream {
			t.Error("stream must be false for a non-streaming request")
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"<think>hidden reasoning</think>\nCline answer"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "openai-compatible",
		BaseURL:  server.URL + "/api/v1",
		Model:    "cline-pass/deepseek-v4.1-flash-1017",
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "cline-key", "system prompt", "user query", 4096, "medium")
	if err != nil {
		t.Fatalf("CreateAIResponse: %v", err)
	}
	if got != "Cline answer" {
		t.Fatalf("got %q, want %q", got, "Cline answer")
	}
}

func TestCreateOpenAICompatibleResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"model not found"}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "openai-compatible",
		BaseURL:  server.URL + "/api/v1",
		Model:    "missing-model",
	}

	_, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 800, "medium")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 404: model not found") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestOpenAICompatibleErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "object envelope",
			raw:  `{"error":{"message":"invalid api key","type":"authentication_error"}}`,
			want: "invalid api key",
		},
		{
			name: "string envelope",
			raw:  `{"error":"Unauthorized: re-authenticate"}`,
			want: "Unauthorized: re-authenticate",
		},
		{
			name: "top level message",
			raw:  `{"message":"bad request"}`,
			want: "bad request",
		},
		{
			name: "no message",
			raw:  `{"choices":[]}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openAICompatibleErrorMessage([]byte(tt.raw)); got != tt.want {
				t.Errorf("openAICompatibleErrorMessage(%s) = %q; want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestValidateAIExtraParams(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "empty", raw: ""},
		{name: "empty object", raw: "{}"},
		{name: "top level values", raw: `{"temperature":0.2,"reasoning_effort":"high"}`},
		{name: "nested object", raw: `{"reasoning":{"effort":"high"}}`},
		{name: "invalid json", raw: `{"temperature":`, wantErr: true},
		{name: "array", raw: `[1,2]`, wantErr: true},
		{name: "null", raw: `null`, wantErr: true},
		{name: "reserved messages", raw: `{"messages":[]}`, wantErr: true},
		{name: "reserved stream", raw: `{"stream":true}`, wantErr: true},
		{name: "reserved model", raw: `{"model":"other"}`, wantErr: true},
		{name: "too large", raw: `{"x":"` + strings.Repeat("a", maxAIExtraParamsBytes) + `"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAIExtraParams(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateAIExtraParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCreateOpenAICompatibleResponseExtraParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "cline-pass/deepseek-v4.1-flash-1017" {
			t.Errorf("model must not be overridden: %v", body["model"])
		}
		if body["reasoning_effort"] != "high" {
			t.Errorf("reasoning_effort: got %v", body["reasoning_effort"])
		}
		if body["temperature"] != 0.2 {
			t.Errorf("temperature: got %v", body["temperature"])
		}
		if body["max_tokens"] != float64(4096) {
			t.Errorf("max_tokens should be preserved: got %v", body["max_tokens"])
		}
		if body["stream"] != false {
			t.Errorf("stream must stay false: got %v", body["stream"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider:    "openai-compatible",
		BaseURL:     server.URL,
		Model:       "cline-pass/deepseek-v4.1-flash-1017",
		ExtraParams: `{"temperature":0.2,"reasoning_effort":"high"}`,
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 4096, "medium")
	if err != nil {
		t.Fatalf("CreateAIResponse: %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want ok", got)
	}
}

func TestCreateOpenAIResponseExtraParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		if reasoning["effort"] != "high" {
			t.Errorf("reasoning.effort: got %v", reasoning["effort"])
		}
		if body["max_output_tokens"] != float64(4096) {
			t.Errorf("max_output_tokens should be preserved: got %v", body["max_output_tokens"])
		}
		if body["temperature"] != 0.1 {
			t.Errorf("temperature: got %v", body["temperature"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"output_text":"ok"}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider:    "openai",
		BaseURL:     server.URL,
		Model:       "gpt-test",
		ExtraParams: `{"reasoning":{"effort":"high"},"temperature":0.1}`,
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 4096, "low")
	if err != nil {
		t.Fatalf("CreateAIResponse: %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want ok", got)
	}
}

func TestCreateGeminiResponseExtraParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		genCfg, _ := body["generationConfig"].(map[string]any)
		if genCfg["maxOutputTokens"] != float64(800) {
			t.Errorf("maxOutputTokens should be preserved: got %v", genCfg["maxOutputTokens"])
		}
		thinking, _ := genCfg["thinkingConfig"].(map[string]any)
		if thinking["thinkingBudget"] != float64(1024) {
			t.Errorf("thinkingConfig.thinkingBudget: got %v", thinking["thinkingBudget"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider:    "gemini",
		BaseURL:     server.URL,
		Model:       "gemini-test",
		ExtraParams: `{"generationConfig":{"thinkingConfig":{"thinkingBudget":1024}}}`,
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 800, "medium")
	if err != nil {
		t.Fatalf("CreateAIResponse: %v", err)
	}
	if got != "ok" {
		t.Fatalf("got %q, want ok", got)
	}
}

func TestCreateOpenAICompatibleResponseNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "openai-compatible",
		BaseURL:  server.URL,
		Model:    "cline-pass/not-a-real-model",
	}

	_, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 800, "medium")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no choices") || !strings.Contains(err.Error(), `{"choices":[]}`) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCreateOpenAICompatibleResponseChoiceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"finish_reason":"error","error":{"code":"context_length_exceeded","message":"too many tokens"}}]}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "openai-compatible",
		BaseURL:  server.URL,
		Model:    "some-model",
	}

	_, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 800, "medium")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context_length_exceeded: too many tokens") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestCreateOpenAICompatibleResponseWrappedData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"choices":[{"finish_reason":"stop","message":{"content":"OK"}}]}}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "openai-compatible",
		BaseURL:  server.URL,
		Model:    "cline-pass/deepseek-v4.1-flash",
	}

	got, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 800, "medium")
	if err != nil {
		t.Fatalf("CreateAIResponse: %v", err)
	}
	if got != "OK" {
		t.Fatalf("got %q, want OK", got)
	}
}

func TestCreateOpenAICompatibleResponseWrappedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"error":{"message":"quota exceeded"}}}`)
	}))
	defer server.Close()

	model := AIModelRecord{
		Provider: "openai-compatible",
		BaseURL:  server.URL,
		Model:    "cline-pass/deepseek-v4.1-flash",
	}

	_, err := CreateAIResponse(context.Background(), server.Client(), model, "key", "inst", "inp", 800, "medium")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
