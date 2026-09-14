package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"inbrix/mailstore"
)

func TestValidateOpenAIBaseURL(t *testing.T) {
	valid := []string{
		"https://api.openai.com/v1",
		"https://gateway.example.com/openai/v1",
		"http://localhost:8080/v1",
		"http://127.0.0.1:8080/v1",
	}
	for _, raw := range valid {
		if err := validateOpenAIBaseURL(raw); err != nil {
			t.Errorf("valid URL %q: %v", raw, err)
		}
	}
	invalid := []string{
		"http://api.example.com/v1",
		"https://user:pass@example.com/v1",
		"https://example.com/v1?token=secret",
		"https://example.com/v1/responses",
		"not-a-url",
	}
	for _, raw := range invalid {
		if err := validateOpenAIBaseURL(raw); err == nil {
			t.Errorf("invalid URL accepted: %q", raw)
		}
	}
}

func TestCreateOpenAIResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization: %q", got)
		}
		var body struct {
			Model           string `json:"model"`
			Input           string `json:"input"`
			Instructions    string `json:"instructions"`
			MaxOutputTokens int    `json:"max_output_tokens"`
			Reasoning       struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Model != "gpt-test" || body.Input != "mail thread" || body.Instructions == "" || body.MaxOutputTokens != 8192 || body.Reasoning.Effort != "low" {
			t.Errorf("unexpected request: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"Concise summary"}]}]}`)
	}))
	defer server.Close()

	h := &AISettingsHandler{client: server.Client()}
	got, err := h.createOpenAIResponse(context.Background(), mailstore.AIModelRecord{
		BaseURL:         server.URL + "/v1",
		Model:           "gpt-test",
		ReasoningEffort: "low",
	}, "test-key", "mail thread")
	if err != nil {
		t.Fatalf("createOpenAIResponse: %v", err)
	}
	if got != "Concise summary" {
		t.Fatalf("summary: got %q", got)
	}
}

func TestCreateOpenAIResponseWithComposeInstructions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input           string `json:"input"`
			Instructions    string `json:"instructions"`
			MaxOutputTokens int    `json:"max_output_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Input != "compose prompt" || body.Instructions != "compose instructions" || body.MaxOutputTokens != 1200 {
			t.Errorf("unexpected request: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"output_text":"Generated email"}`)
	}))
	defer server.Close()

	h := &AISettingsHandler{client: server.Client()}
	got, err := h.createOpenAIResponseWithInstructions(context.Background(), mailstore.AIModelRecord{
		BaseURL: server.URL,
		Model:   "gpt-test",
	}, "test-key", "compose instructions", "compose prompt", 1200)
	if err != nil {
		t.Fatalf("createOpenAIResponseWithInstructions: %v", err)
	}
	if got != "Generated email" {
		t.Fatalf("output: got %q", got)
	}
}

func TestEmailDraftInstructions(t *testing.T) {
	t.Run("system prompt only", func(t *testing.T) {
		if got := emailDraftInstructions("  "); got != emailDraftSystemPrompt {
			t.Fatalf("emailDraftInstructions() = %q, want system prompt", got)
		}
	})
	t.Run("combines system and agent prompts", func(t *testing.T) {
		got := emailDraftInstructions("  Reply in concise business Chinese.  ")
		want := emailDraftSystemPrompt + "\n\nAgent instructions:\nReply in concise business Chinese."
		if got != want {
			t.Fatalf("emailDraftInstructions() = %q, want %q", got, want)
		}
	})
}

func TestStripBestRegards(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"signature block": {"Thanks for confirming.\n\nBest regards,\nAlice", "Thanks for confirming."},
		"case and CRLF":   {"See you Tuesday.\r\n\r\nbEsT ReGaRdS，\r\nAlice", "See you Tuesday."},
		"no signoff":      {"Thanks for confirming.", "Thanks for confirming."},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := stripBestRegards(test.input); got != test.want {
				t.Fatalf("stripBestRegards() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateGeminiBaseURL(t *testing.T) {
	valid := []string{
		"https://generativelanguage.googleapis.com",
		"https://generativelanguage.googleapis.com/v1beta",
		"https://proxy.example.com/gemini",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
	}
	for _, raw := range valid {
		if err := validateAIBaseURL("gemini", raw); err != nil {
			t.Errorf("valid Gemini URL %q: %v", raw, err)
		}
	}
	invalid := []string{
		"http://api.example.com",
		"https://user:pass@example.com",
		"https://generativelanguage.googleapis.com/v1beta/models",
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent",
		"not-a-url",
	}
	for _, raw := range invalid {
		if err := validateAIBaseURL("gemini", raw); err == nil {
			t.Errorf("invalid Gemini URL accepted: %q", raw)
		}
	}
}

func TestCreateGeminiResponseInWebHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-3.8-flash:generateContent" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "test-gemini-key" {
			t.Errorf("api key header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"Gemini test output"}]}}]}`)
	}))
	defer server.Close()

	h := &AISettingsHandler{client: server.Client()}
	got, err := h.createAIResponse(context.Background(), mailstore.AIModelRecord{
		Provider: "gemini",
		BaseURL:  server.URL,
		Model:    "gemini-3.8-flash",
	}, "test-gemini-key", "test prompt")
	if err != nil {
		t.Fatalf("createAIResponse: %v", err)
	}
	if got != "Gemini test output" {
		t.Fatalf("output: got %q, want 'Gemini test output'", got)
	}
}

func TestValidateDeepSeekBaseURL(t *testing.T) {
	valid := []string{
		"https://api.deepseek.com",
		"https://api.deepseek.com/v1",
		"https://custom-proxy.example.com",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
	}
	for _, raw := range valid {
		if err := validateAIBaseURL("deepseek", raw); err != nil {
			t.Errorf("valid DeepSeek URL %q: %v", raw, err)
		}
	}
	invalid := []string{
		"http://api.deepseek.com",
		"https://user:pass@example.com",
		"https://api.deepseek.com/chat/completions",
		"not-a-url",
	}
	for _, raw := range invalid {
		if err := validateAIBaseURL("deepseek", raw); err == nil {
			t.Errorf("invalid DeepSeek URL accepted: %q", raw)
		}
	}
}

func TestCreateDeepSeekResponseInWebHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-ds-key" {
			t.Errorf("authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"DeepSeek web test output","reasoning_content":"thought"}}],"finish_reason":"stop"}`)
	}))
	defer server.Close()

	h := &AISettingsHandler{client: server.Client()}
	got, err := h.createAIResponse(context.Background(), mailstore.AIModelRecord{
		Provider: "deepseek",
		BaseURL:  server.URL,
		Model:    "deepseek-chat",
	}, "test-ds-key", "test prompt")
	if err != nil {
		t.Fatalf("createAIResponse: %v", err)
	}
	if got != "DeepSeek web test output" {
		t.Fatalf("output: got %q, want 'DeepSeek web test output'", got)
	}
}
