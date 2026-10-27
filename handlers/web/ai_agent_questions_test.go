package web

import (
	"context"
	"strings"
	"testing"
)

func TestAskUserValidation(t *testing.T) {
	for _, args := range []map[string]any{nil, {"question": "  "}, {"question": 42}} {
		if _, _, err := executeAskUser(args); err == nil {
			t.Fatalf("accepted invalid question: %v", args)
		}
	}
	value, question, err := executeAskUser(map[string]any{"question": " 请提供数量和口径？ "})
	if err != nil || question != "请提供数量和口径？" || value.(*AIUserQuestion).Question != question {
		t.Fatalf("unexpected question: %v %q %v", value, question, err)
	}
}

func TestAskUserSessionResumesFromPersistedQuestion(t *testing.T) {
	calls := 0
	h, app, account, _ := agentContextTestApp(t, func(r agentTestRequest) string {
		calls++
		if calls == 1 {
			return "我还需要确认数量。\n```tool_call\n{\"name\":\"ask_user\",\"arguments\":{\"question\":\"需要多少件？\",\"options\":[\"10件\",\"需要20件\"]}}\n```"
		}
		for _, required := range []string{"请帮我准备报价", "需要多少件？", "需要20件"} {
			if !strings.Contains(r.Input, required) {
				t.Errorf("missing context %q in %s", required, r.Input)
			}
		}
		return "已收到，按20件继续准备报价。"
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	version := int64(0)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "ask-1", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "请帮我准备报价"}}}
	status, question, raw := postAgentContext(t, app, input)
	if status != 200 || calls != 1 || question.Content != "需要多少件？" {
		t.Fatalf("question: %d calls=%d %s", status, calls, raw)
	}
	if question.Question == nil || len(question.Question.Options) != 2 {
		t.Fatalf("missing structured question: %s", raw)
	}
	if len(question.Parts) != 3 || question.Parts[0].Content != "我还需要确认数量。" || question.Parts[2].Content != question.Content {
		t.Fatalf("question order: %+v", question.Parts)
	}
	saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if err != nil || !strings.Contains(string(saved.Messages[1]), "ask_user") || !strings.Contains(string(saved.Messages[1]), "需要20件") {
		t.Fatalf("question not persisted: %+v %v", saved, err)
	}
	// Replaying the asking turn must not ask the model again.
	status, _, raw = postAgentContext(t, app, input)
	if status != 200 || calls != 1 {
		t.Fatalf("replay: %d %s", status, raw)
	}
	version = question.Version
	input.RequestID = "answer-1"
	input.Messages = []AIAgentChatMessage{{Role: "user", Content: "需要20件"}}
	status, answer, raw := postAgentContext(t, app, input)
	if status != 200 || calls != 2 || !strings.Contains(answer.Content, "20件") {
		t.Fatalf("resume: %d %s", status, raw)
	}
}

func TestAskUserOptionsValidation(t *testing.T) {
	for _, options := range []any{nil, "invalid", []any{42}, []string{""}, []string{"A", " A "}, []string{"1", "2", "3", "4", "5", "6", "7"}} {
		if _, _, err := executeAskUser(map[string]any{"question": "请选择", "options": options}); err == nil {
			t.Fatalf("accepted options: %v", options)
		}
	}
	value, _, err := executeAskUser(map[string]any{"question": "请选择", "options": []any{" A ", "B"}})
	if err != nil || value.(*AIUserQuestion).Options[0] != "A" {
		t.Fatalf("options normalization: %v %v", value, err)
	}
}
