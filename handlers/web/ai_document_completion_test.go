package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestDocumentGenerationRecoversBeforeApplyingCommands(t *testing.T) {
	const edit = `{"commands":[{"type":"set_fields","fields":{"currency":"USD"}},{"type":"add_line","fields":{"model":"DP27 DN40","qty":"1","price":"3714.00"}},{"type":"add_line","fields":{"model":"SV607 DN40/DN65","qty":"1","price":"1350.00"}}],"message":"已填写报价"}`
	for _, tc := range []struct {
		name                      string
		status, calls, wantStatus int
		first                     string
		final                     string
	}{
		{"empty gateway response recovers", 500, 2, 200, "", edit},
		{"gateway keeps failing", 500, 2, 422, "", ""},
		{"authentication is not retried", 401, 1, 422, "", ""},
		{"invalid output is corrected once", 200, 2, 200, "invalid JSON", edit},
		{"unknown fields are corrected once", 200, 2, 200, `{"commands":[],"message":"解释","unexpected":true}`, edit},
		{"persistent invalid output fails safely", 200, 2, 422, "invalid JSON", "invalid JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
			ctx := context.Background()
			model, err := h.mailDB.GetDefaultAIModel(ctx, account.OwnerID)
			if err != nil {
				t.Fatal(err)
			}
			model.Provider, model.ExtraParams = "openai-compatible", `{"reasoning_effort":"high"}`
			if _, err := h.mailDB.UpdateAIModel(ctx, model); err != nil {
				t.Fatal(err)
			}
			calls := 0
			h.client = &http.Client{Transport: agentStreamTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				var payload struct {
					MaxTokens       int    `json:"max_tokens"`
					ReasoningEffort string `json:"reasoning_effort"`
					ResponseFormat  struct {
						Type string `json:"type"`
					} `json:"response_format"`
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload.MaxTokens < 16384 || payload.ReasoningEffort != "high" {
					t.Fatalf("insufficient reasoning/output budget or lost model settings: %+v", payload)
				}
				if payload.ResponseFormat.Type != "json_object" {
					t.Fatal("document request did not constrain JSON output")
				}
				if calls == 2 && tc.first != "" {
					if len(payload.Messages) != 2 || !strings.Contains(payload.Messages[1].Content, "validationError") || !strings.Contains(payload.Messages[1].Content, "invalidResponse") || !strings.Contains(payload.Messages[1].Content, "3714") {
						t.Fatal("correction lost original prices, snapshot, or parser feedback")
					}
				}
				status, raw := tc.status, `{"error":{"message":"empty response content"}}`
				if status == 200 || (calls == 2 && tc.final != "") {
					status = 200
					content := tc.final
					if calls == 1 && tc.first != "" {
						content = tc.first
					}
					encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}, "finish_reason": "stop"}}})
					raw = string(encoded)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}, nil
			})}
			app.Post("/api/ai/write-document", h.HandleWriteDocument)
			canvas := canvasBody(t, "spirax-quotation", []map[string]any{
				controlElement("currency", "USD"), controlElement("total_amount", ""),
				tableElement([][]map[string]any{{
					controlElement("item_model_0", ""), controlElement("item_qty_0", ""),
					controlElement("item_price_0", ""), controlElement("item_amount_0", ""),
				}}),
			})
			saved, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "Original", HTML: canvas}, 0)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(aiDocumentInput{AccountEmail: account.Email, Mode: "variables", DocumentType: "quotation", Canvas: canvas, Instruction: "创建美元报价：DP27 DN40 数量1单价3714；SV607 DN40/DN65 数量1单价1350"})
			req := httptest.NewRequest(http.MethodPost, "/api/ai/write-document", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			res, err := app.Test(req, 5000)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			raw, _ := io.ReadAll(res.Body)
			if calls != tc.calls || res.StatusCode != tc.wantStatus {
				t.Fatalf("calls=%d status=%d body=%s", calls, res.StatusCode, raw)
			}
			if res.StatusCode == 200 {
				var result struct {
					HTML string `json:"html"`
				}
				if err := json.Unmarshal(raw, &result); err != nil {
					t.Fatal(err)
				}
				values, items, ok := canvasDocumentContent(result.HTML)
				if !ok || len(items) != 2 || values["currency"] != "USD" || values["total_amount"] != "5064.00" {
					t.Fatalf("retry duplicated lines or changed prices: values=%v items=%+v", values, items)
				}
			}
			unchanged, err := h.mailDB.GetDocument(ctx, account.OwnerID, saved.ID)
			if err != nil || unchanged.HTML != canvas {
				t.Fatal("generation saved a document before application", err)
			}
			logs, err := h.mailDB.ListAIErrorLogs(ctx, account.OwnerID, 100)
			wantLogs := 1
			if tc.wantStatus == 200 {
				wantLogs = 0
			}
			if err != nil || len(logs) != wantLogs {
				t.Fatalf("logs=%+v err=%v", logs, err)
			}
			if tc.name == "persistent invalid output fails safely" && !strings.HasPrefix(logs[0].ErrorMessage, "invalid document JSON: 文档助手回复无效:") {
				t.Fatal("parse error missing from start of diagnostic log", logs[0].ErrorMessage)
			}
		})
	}
}

func TestDocumentEditReplyPreservesJSONStrings(t *testing.T) {
	for _, message := range []string{"说明中保留 ```json 标记和 {花括号}", "报价说明\n```json\n{\"price\":\"3714.00\"}\n```"} {
		body, _ := json.Marshal(documentEditReply{Commands: []DocumentCommand{}, Message: message})
		for _, wrapped := range []string{string(body), "```json\n" + string(body) + "\n```"} {
			reply, err := decodeDocumentEditReply(wrapped)
			if err != nil || reply.Message != message {
				t.Fatalf("valid JSON changed: reply=%+v err=%v", reply, err)
			}
		}
	}
	for _, body := range []string{`{"commands":[],"message":"ok","extra":true}`, `{"commands":[],"message":"ok"}{"commands":[]}`, `{"commands":[{"type":"add_line","unknown":true,"fields":{"price":"1"}}]}`} {
		if _, err := decodeDocumentEditReply(body); err == nil {
			t.Fatal("unrecognized/multiple JSON objects accepted", body)
		}
	}
}

func TestDocumentGenerationCancellationDuringFormatCorrection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	h := &AISettingsHandler{client: &http.Client{Transport: agentStreamTransport(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"invalid JSON"},"finish_reason":"stop"}]}`))}, nil
	})}}
	_, err := h.createDocumentEditReply(ctx, mailstore.AIModelRecord{Provider: "openai-compatible", BaseURL: "https://model.invalid/v1", Model: "test"}, "key", "instructions", "prompt")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("stopped generation continued correcting: calls=%d err=%v", calls, err)
	}
}

func TestDocumentGenerationCancellationDuringRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	h := &AISettingsHandler{client: &http.Client{Transport: agentStreamTransport(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"empty response content"}`))}, nil
	})}}
	_, err := h.createDocumentEditResponse(ctx, mailstore.AIModelRecord{Provider: "openai-compatible", BaseURL: "https://model.invalid/v1", Model: "test"}, "key", "instructions", "prompt")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled request retried: calls=%d err=%v", calls, err)
	}
}
