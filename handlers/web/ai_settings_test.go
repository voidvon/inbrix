package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCleanDocumentHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean html untouched",
			input:    "<div><p>Hello world</p></div>",
			expected: "<div><p>Hello world</p></div>",
		},
		{
			name:     "wrapped in code fence with html tag",
			input:    "```html\n<div><p>Hello world</p></div>\n```",
			expected: "<div><p>Hello world</p></div>",
		},
		{
			name:     "wrapped in code fence without tag",
			input:    "```\n<div><p>Hello world</p></div>\n```",
			expected: "<div><p>Hello world</p></div>",
		},
		{
			name:     "conversational prefix and suffix",
			input:    "Here is the revised document:\n```html\n<p>Updated content</p>\n```\nHope that helps!",
			expected: "<p>Updated content</p>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanDocumentHTML(tc.input)
			if got != tc.expected {
				t.Errorf("cleanDocumentHTML() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestCleanJSONResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean json untouched",
			input:    `{"customer_name":"Acme Corp","price":"100"}`,
			expected: `{"customer_name":"Acme Corp","price":"100"}`,
		},
		{
			name:     "wrapped in code fence with json tag",
			input:    "```json\n{\"customer_name\":\"Acme Corp\"}\n```",
			expected: `{"customer_name":"Acme Corp"}`,
		},
		{
			name:     "conversational prefix and suffix",
			input:    "Here is the data:\n```json\n{\"customer_name\":\"Acme Corp\"}\n```\nHope that helps!",
			expected: `{"customer_name":"Acme Corp"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanJSONResponse(tc.input)
			if got != tc.expected {
				t.Errorf("cleanJSONResponse() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestParseAIDocumentUpdateResponse(t *testing.T) {
	input := `{
		"values": {
			"customer_company": "IT Vision Networks",
			"total_amount": "24,326.00"
		},
		"items": [
			{
				"model": "CLIN001 Part # 67980 PC20",
				"description": "Universal Connector",
				"qty": "18",
				"price": "450.00",
				"amount": "8,100.00"
			},
			{
				"model": "CLIN002 Part # 1465300 UFT32-4.5",
				"description": "Steam Trap",
				"qty": "12",
				"price": "1000.00",
				"amount": "12,000.00"
			}
		]
	}`
	values, items, err := parseAIDocumentUpdateResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if values["customer_company"] != "IT Vision Networks" {
		t.Errorf("customer_company = %q, want %q", values["customer_company"], "IT Vision Networks")
	}
	if values["total_amount"] != "24,326.00" {
		t.Errorf("total_amount = %q, want %q", values["total_amount"], "24,326.00")
	}
	if len(items) != 2 {
		t.Fatalf("items count = %d, want 2", len(items))
	}
	if items[0].Model != "CLIN001 Part # 67980 PC20" || items[0].Qty != "18" {
		t.Errorf("item[0] mismatch: %+v", items[0])
	}
	if items[1].Model != "CLIN002 Part # 1465300 UFT32-4.5" || items[1].Amount != "12,000.00" {
		t.Errorf("item[1] mismatch: %+v", items[1])
	}
}

func TestStripHeavyHTMLForAI(t *testing.T) {
	input := `<div><img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="><svg width="10" height="10"><path d="M0 0h10v10H0z"/></svg><p>Text</p></div>`
	expected := `<div><img src="[image]">[svg-graphic]<p>Text</p></div>`
	got := stripHeavyHTMLForAI(input)
	if got != expected {
		t.Errorf("stripHeavyHTMLForAI() = %q, want %q", got, expected)
	}
}

func TestBuildDocumentVariablesPrompt(t *testing.T) {
	input := aiDocumentInput{
		DocumentType: "contract",
		Title:        "Sales Contract",
		Instruction:  "Change buyer company to Huawei and set total to 24,326",
		CurrentHTML:  `<div><p>Contract body</p><img src="data:image/png;base64,AAAA"><svg><path d="M0 0h1v1H0z"/></svg></div>`,
		Variables: []aiVariableItem{
			{ConceptID: "buyer_company", Label: "Buyer Company", CurrentValue: "Acme"},
			{ConceptID: "order_note_1", Label: "Note 1", CurrentValue: ""},
		},
	}
	instructions, prompt := buildDocumentVariablesPrompt(input)

	// 1. 提示词列出文档真实变量 ID（动态生成，而非固定 schema）
	for _, id := range []string{`"buyer_company"`, `"order_note_1"`} {
		if !strings.Contains(prompt, id) {
			t.Errorf("prompt missing allowed variable id %s", id)
		}
	}
	// 2. 旧的固定示例 schema 已移除
	for _, legacy := range []string{"Available Document Variables", `"customer_company": "..."`, `"total_amount": "..."`} {
		if strings.Contains(instructions, legacy) || strings.Contains(prompt, legacy) {
			t.Errorf("prompt still contains legacy fixed schema %q", legacy)
		}
	}
	// 3. 全文作为全局上下文下发
	if !strings.Contains(prompt, "Full Document Content") {
		t.Error("prompt missing Full Document Content section")
	}
	if !strings.Contains(prompt, "Contract body") {
		t.Error("prompt missing document HTML content")
	}
	// 4. 全文中的图片 / SVG 已被轻量化
	if strings.Contains(prompt, "data:image/png;base64") {
		t.Error("prompt still contains data URI")
	}
	if strings.Contains(prompt, "<svg") {
		t.Error("prompt still contains svg markup")
	}
	if !strings.Contains(prompt, "[image]") || !strings.Contains(prompt, "[svg-graphic]") {
		t.Error("prompt missing lightened placeholders")
	}
	// 5. instructions 要求 values 的 key 只能来自允许列表
	if !strings.Contains(instructions, "ALLOWED VARIABLE IDS") {
		t.Error("instructions missing allowed-ids requirement")
	}
}

func TestBuildDocumentVariablesPromptNoHTML(t *testing.T) {
	input := aiDocumentInput{
		DocumentType: "quotation",
		Title:        "Quote",
		Instruction:  "update price",
		Variables: []aiVariableItem{
			{ConceptID: "total_amount", Label: "Total", CurrentValue: "100"},
		},
	}
	_, prompt := buildDocumentVariablesPrompt(input)
	if strings.Contains(prompt, "Full Document Content") {
		t.Error("prompt should not contain Full Document Content when CurrentHTML is empty")
	}
	if !strings.Contains(prompt, `"total_amount"`) {
		t.Error("prompt missing allowed variable id total_amount")
	}
}
