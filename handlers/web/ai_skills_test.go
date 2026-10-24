package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestChatUsesManagedSkillRevisionAndRejectsDisabledSelection(t *testing.T) {
	var prompts []string
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string { prompts = append(prompts, request.Instructions); return "done" })
	ctx := context.Background()
	skill, err := h.mailDB.SaveAISkill(ctx, mailstore.AISkillRecord{ID: "customer-follow-up", OwnerID: account.OwnerID, Name: "Follow up", Kind: mailstore.AISkillChat, Instructions: "MANAGED-CHAT-FIRST: review pending requests.", DependsOn: []string{"email_analysis"}, Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	request := AIAgentChatRequest{AccountEmail: account.Email, ConversationID: conversationID(account.ID, []string{"peer@example.com"}), BusinessContext: &mailstore.AIBusinessContext{SkillID: skill.ID}, Messages: []AIAgentChatMessage{{Role: "user", Content: "Follow up"}}}
	status, _, body := postAgentContext(t, app, request)
	if status != http.StatusOK || len(prompts) != 1 || !strings.Contains(prompts[0], "MANAGED-CHAT-FIRST") || !strings.Contains(prompts[0], "Skill email_analysis:") {
		t.Fatalf("first run: %d %s %v", status, body, prompts)
	}
	skill.Instructions = "MANAGED-CHAT-SECOND: verify the latest changed requirements."
	skill, err = h.mailDB.SaveAISkill(ctx, skill, skill.Revision)
	if err != nil {
		t.Fatal(err)
	}
	request.BusinessContext = nil
	status, _, body = postAgentContext(t, app, request)
	if status != http.StatusOK || len(prompts) != 2 || !strings.Contains(prompts[1], "MANAGED-CHAT-SECOND") || strings.Contains(prompts[1], "MANAGED-CHAT-FIRST") {
		t.Fatalf("updated run: %d %s %v", status, body, prompts)
	}
	skill.Enabled = false
	if _, err = h.mailDB.SaveAISkill(ctx, skill, skill.Revision); err != nil {
		t.Fatal(err)
	}
	status, _, body = postAgentContext(t, app, request)
	if status != http.StatusBadRequest || len(prompts) != 2 {
		t.Fatalf("disabled selected skill used: %d %s", status, body)
	}
	request.BusinessContext = &mailstore.AIBusinessContext{}
	status, _, body = postAgentContext(t, app, request)
	if status != http.StatusOK || len(prompts) != 3 || strings.Contains(prompts[2], "MANAGED-CHAT-SECOND") {
		t.Fatalf("disabled skill auto-loaded: %d %s", status, body)
	}
}

func TestChatLoadsSkillsOnDemandAndPinsRevisionWithinTurn(t *testing.T) {
	var h *AISettingsHandler
	var account mailstore.Account
	var prompts []string
	var inputs []string
	var err error
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		prompts = append(prompts, request.Instructions)
		inputs = append(inputs, request.Input)
		if len(prompts) == 1 {
			if strings.Contains(request.Instructions, "ON-DEMAND-OLD") {
				t.Fatal("auto mode eagerly loaded instructions")
			}
			skill, e := h.mailDB.GetAISkill(context.Background(), account.OwnerID, "follow-up")
			if e != nil {
				t.Fatal(e)
			}
			skill.Instructions = "ON-DEMAND-NEW"
			if _, e = h.mailDB.SaveAISkill(context.Background(), skill, skill.Revision); e != nil {
				t.Fatal(e)
			}
			return `{"name":"load_skill","arguments":{"id":"follow-up"}}`
		}
		return "done"
	})
	_, err = h.mailDB.SaveAISkill(context.Background(), mailstore.AISkillRecord{ID: "follow-up", OwnerID: account.OwnerID, Name: "Follow up", Kind: mailstore.AISkillChat, Instructions: "ON-DEMAND-OLD", Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	request := AIAgentChatRequest{AccountEmail: account.Email, ConversationID: conversationID(account.ID, []string{"peer@example.com"}), BusinessContext: &mailstore.AIBusinessContext{}, Messages: []AIAgentChatMessage{{Role: "user", Content: "Follow up"}}}
	status, result, body := postAgentContext(t, app, request)
	if status != http.StatusOK || len(prompts) != 2 || !strings.Contains(inputs[1], "ON-DEMAND-OLD") || strings.Contains(inputs[1], "ON-DEMAND-NEW") || len(result.SkillSnapshots) != 1 || result.SkillSnapshots[0].Revision != 1 {
		t.Fatal(status, body, inputs)
	}
	request.BusinessContext = &mailstore.AIBusinessContext{SkillID: "follow-up"}
	status, result, body = postAgentContext(t, app, request)
	if status != http.StatusOK || !strings.Contains(prompts[2], "ON-DEMAND-NEW") || result.SkillSnapshots[0].Revision != 2 {
		t.Fatal(status, body, prompts)
	}
}
