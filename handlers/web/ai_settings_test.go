package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"lilmail/mailstore"
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
		if body.Model != "gpt-test" || body.Input != "mail thread" || body.Instructions == "" || body.MaxOutputTokens != 800 || body.Reasoning.Effort != "low" {
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
