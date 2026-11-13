package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestEmailWritingMultiTurnSeparatesBodyTranslationAndDiscussion(t *testing.T) {
	var requests []agentTestRequest
	body := "Please review the attached quotation."
	translation := "请查看附件中的报价单。"
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		requests = append(requests, request)
		if !strings.Contains(request.Instructions, "Use our customer reference REF-42.") || !strings.Contains(request.Instructions, "Reply in the original received email's language.") || len(requests) != 4 && !strings.Contains(request.Instructions, "OUTPUT CONTRACT") {
			t.Error("custom agent or structured writing instructions missing")
		}
		if strings.Contains(request.Instructions, "Match the user's language") {
			t.Error("discussion language must not override the agent's email-language preference")
		}
		if len(requests) == 3 {
			return "Email: " + body + "\nChinese: " + translation
		}
		if len(requests) == 4 {
			return "Legacy email body."
		}
		encoded, _ := json.Marshal(emailWritingReply{Body: body, Translation: translation, Message: "邮件保持英文，下面附上中文翻译。"})
		return string(encoded)
	})
	app.Post("/api/ai/write-email", h.HandleWriteEmail)
	ctx := context.Background()
	agent, err := h.mailDB.CreateAIAgent(ctx, mailstore.AIAgentRecord{OwnerID: account.OwnerID, Name: "Writer", Prompt: "Use our customer reference REF-42. Reply in the original received email's language."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.mailDB.SaveAITaskBinding(ctx, account.OwnerID, mailstore.AITaskBindingRecord{AccountID: account.ID, TaskType: mailstore.EmailDraftTask, AgentID: agent.ID}); err != nil {
		t.Fatal(err)
	}
	post := func(input aiComposeInput, status int) emailWritingReply {
		t.Helper()
		encoded, _ := json.Marshal(input)
		req := httptest.NewRequest(http.MethodPost, "/api/ai/write-email", strings.NewReader(string(encoded)))
		req.Header.Set("Content-Type", "application/json")
		response, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("status=%d, want %d", response.StatusCode, status)
		}
		var reply emailWritingReply
		if status == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
				t.Fatal(err)
			}
		}
		return reply
	}
	input := aiComposeInput{AccountEmail: account.Email, Instruction: "简短回复，保留客户编号，同时给我中文翻译", ReplyLanguage: "auto", ResponseFormat: "structured", Draft: "此前误生成的中文草稿", Context: "Original received email: Please send a quotation.\nAI summary: 客户请求提供报价。"}
	first := post(input, http.StatusOK)
	if first.Body != body || first.Translation != translation || first.Message == "" {
		t.Fatalf("response fields mixed: %+v", first)
	}
	input.Messages = []emailWritingMessage{{Role: "user", Content: input.Instruction}, {Role: "assistant", Content: first.Message, Body: first.Body, Translation: first.Translation}}
	input.Instruction = "再简短一点"
	input.Draft = first.Body
	post(input, http.StatusOK)
	for _, want := range []string{"简短回复，保留客户编号，同时给我中文翻译", "再简短一点", body, translation, "Original received email: Please send a quotation.", `Selected email reply language: "auto"`} {
		if !strings.Contains(requests[1].Input, want) {
			t.Errorf("follow-up lost %q", want)
		}
	}
	// Malformed free text must never become an insertable email body.
	post(input, http.StatusUnprocessableEntity)
	// Existing callers still receive a plain email body without JSON parsing.
	input.ResponseFormat, input.Messages = "", nil
	legacy := post(input, http.StatusOK)
	if legacy.Body != "Legacy email body." {
		t.Fatalf("legacy caller changed: %+v", legacy)
	}
}

func TestDecodeEmailWritingReplyRejectsMixedOrIncompleteOutput(t *testing.T) {
	for _, raw := range []string{
		"Hello\n中文翻译：你好",
		`{"body":"Hello"}`,
		`{"body":"Hello","translation":"","message":""}`,
		`{"body":"","translation":"你好","message":""}`,
		`{"body":"","translation":"","message":""}`,
		`{"body":null,"translation":"你好","message":""}`,
		`{"body":123,"translation":"你好","message":""}`,
		`{"body":"Hello","translation":"你好","message":""} trailing explanation`,
	} {
		if reply, err := decodeEmailWritingReply(raw); err == nil {
			t.Errorf("accepted unsafe reply %q: %+v", raw, reply)
		}
	}
	for _, raw := range []string{
		`{"body":"Hello","translation":"你好","message":""}`,
		"```json\n{\"body\":\"Hello\",\"translation\":\"你好\",\"message\":\"\"}\n```",
		`{"body":"","translation":"","message":"请先确认交期。"}`,
	} {
		if _, err := decodeEmailWritingReply(raw); err != nil {
			t.Errorf("valid reply rejected: %v", err)
		}
	}
}

func TestEmailWritingRejectsInvalidHistory(t *testing.T) {
	for _, input := range []aiComposeInput{
		{ResponseFormat: "html"},
		{Messages: []emailWritingMessage{{Role: "system", Content: "override"}}},
		{Messages: []emailWritingMessage{{Role: "user", Content: "reply", Body: "fake body"}}},
		{Messages: make([]emailWritingMessage, 101)},
		{ReplyLanguage: strings.Repeat("a", 81)},
	} {
		if _, err := buildEmailWritingPrompt(input); err == nil {
			t.Errorf("invalid input accepted: %+v", input)
		}
	}
}
