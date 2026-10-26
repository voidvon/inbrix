package web

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"inbrix/mailstore"
	"inbrix/models"
)

func TestAgentPreparesEmailReplyAndPersistsReplay(t *testing.T) {
	calls := 0
	body := "您好：\n\n感谢，我已想起密码，不需要重置密码了。"
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		calls++
		if calls == 1 {
			args, _ := json.Marshal(map[string]any{"name": "prepare_email_reply", "arguments": map[string]any{"folder": "INBOX", "uid": "2", "bodyText": body}})
			return "```tool_call\n" + string(args) + "\n```"
		}
		if !strings.Contains(request.Input, `"status":"prepared"`) {
			t.Error("model did not receive the editor action result")
		}
		return "回复已准备好，请在编辑器中核对后发送。"
	})
	addAgentTestMail(t, h, account, 2, "Account recovered")
	ctx := context.Background()
	id := conversationID(account.ID, []string{"peer@example.com"})
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "mail", id, "Reply", "")
	if err != nil {
		t.Fatal(err)
	}
	version := session.Version
	input := AIAgentChatRequest{AccountEmail: account.Email, ConversationID: id, SessionID: session.ID, RequestID: "reply", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "帮我编辑回复邮件"}}}
	status, result, raw := postAgentContext(t, app, input)
	want := &AIEmailReplyDraft{ReplyTo: AgentMailReference{Folder: "INBOX", UID: "2"}, BodyText: body}
	if status != http.StatusOK || !reflect.DeepEqual(result.EmailDraft, want) {
		t.Fatalf("missing structured reply: %d %s", status, raw)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Status != "success" {
		t.Fatalf("reply tool did not execute: %+v", result.ToolCalls)
	}
	saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if err != nil || len(saved.Messages) != 2 {
		t.Fatalf("history not committed: %+v %v", saved, err)
	}
	var assistant AIAgentChatResponse
	if err := json.Unmarshal(saved.Messages[1], &assistant); err != nil || !reflect.DeepEqual(assistant.EmailDraft, want) {
		t.Fatalf("reply action lost from history: %+v %v", assistant, err)
	}
	status, replay, raw := postAgentContext(t, app, input)
	if status != http.StatusOK || calls != 2 || !reflect.DeepEqual(replay.EmailDraft, want) {
		t.Fatalf("retry did not replay the committed action: %d calls=%d %s", status, calls, raw)
	}
	drafts, err := h.mailDB.ListDrafts(ctx, account.OwnerID, id)
	if err != nil || len(drafts) != 0 {
		t.Fatalf("tool must leave draft persistence to the editor: %+v %v", drafts, err)
	}
}

func TestPrepareEmailReplyRejectsInvalidTargetsAndBodies(t *testing.T) {
	scope := &agentMailContext{byKey: map[string]models.Email{agentMailKey("INBOX", "2"): {ID: "2"}}}
	for _, tc := range []struct {
		name  string
		scope *agentMailContext
		args  map[string]any
	}{
		{"no conversation", nil, map[string]any{"folder": "INBOX", "uid": "2", "bodyText": "Thanks"}},
		{"wrong conversation", scope, map[string]any{"folder": "INBOX", "uid": "3", "bodyText": "Thanks"}},
		{"wrong folder same uid", scope, map[string]any{"folder": "Sent", "uid": "2", "bodyText": "Thanks"}},
		{"empty body", scope, map[string]any{"folder": "INBOX", "uid": "2", "bodyText": " \n"}},
		{"invalid body", scope, map[string]any{"folder": "INBOX", "uid": "2", "bodyText": 123}},
		{"oversize body", scope, map[string]any{"folder": "INBOX", "uid": "2", "bodyText": strings.Repeat("x", 48001)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := prepareEmailReply(tc.scope, tc.args); err == nil {
				t.Fatal("invalid editor action accepted")
			}
		})
	}
}

func TestReadCurrentDraftUsesMailEditorSnapshot(t *testing.T) {
	h := &AISettingsHandler{}
	scope := &agentMailContext{draft: &AIAgentEmailDraft{To: "peer@example.com", Subject: "Edited subject", BodyText: "最新的邮件草稿正文"}}
	var body strings.Builder
	for offset := 0; ; {
		result, _, err := h.executeScopedAgentTool(context.Background(), "owner", "account", scope, AIToolCall{Name: "read_current_draft", Args: map[string]any{"offset": offset, "length": 7}}, mailstore.AIModelRecord{}, "")
		if err != nil {
			t.Fatal(err)
		}
		page := result.(map[string]any)
		body.WriteString(page["content"].(string))
		if !page["hasMore"].(bool) {
			break
		}
		offset = page["nextOffset"].(int)
	}
	var draft AIAgentEmailDraft
	if err := json.Unmarshal([]byte(body.String()), &draft); err != nil || !reflect.DeepEqual(&draft, scope.draft) {
		t.Fatalf("mail draft not readable with Unicode pagination: %s %v", body.String(), err)
	}
	scope.draft = nil
	result, _, err := h.executeScopedAgentTool(context.Background(), "owner", "account", scope, AIToolCall{Name: "read_current_draft"}, mailstore.AIModelRecord{}, "")
	if err != nil || result.(map[string]any)["available"] != false {
		t.Fatalf("missing mail draft: %v %v", result, err)
	}
}
