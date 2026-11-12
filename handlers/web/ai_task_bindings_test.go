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

func TestMailboxAITaskBindingSavesAgentWithoutModel(t *testing.T) {
	emailHandler, app, account, _ := outboxTestApp(t)
	ctx := context.Background()
	agent, err := emailHandler.mailDB.CreateAIAgent(ctx, mailstore.AIAgentRecord{OwnerID: account.OwnerID, Name: "Writer", Prompt: "Write replies"})
	if err != nil {
		t.Fatal(err)
	}
	h := NewAISettingsHandler(emailHandler.store, emailHandler.config, emailHandler.mailDB)
	app.Put("/task-bindings", h.HandleSaveTaskBinding)
	app.Get("/task-bindings", h.HandleListTaskBindings)
	for _, enabled := range []bool{true, false} {
		body := fmt.Sprintf(`{"accountEmail":%q,"taskType":"email_draft","agentId":%q,"enabled":%t}`, account.Email, agent.ID, enabled)
		req := httptest.NewRequest(http.MethodPut, "/task-bindings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("save binding without model: %d", res.StatusCode)
		}
		res, err = app.Test(httptest.NewRequest(http.MethodGet, "/task-bindings", nil))
		if err != nil {
			t.Fatal(err)
		}
		var out struct{ Bindings []aiTaskBindingPublic }
		err = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("list bindings: %d, %v", res.StatusCode, err)
		}
		found := false
		for _, binding := range out.Bindings {
			if binding.AccountEmail == account.Email && binding.TaskType == mailstore.EmailDraftTask {
				found = true
				if binding.AgentID != agent.ID || !binding.Explicit || binding.Enabled != enabled {
					t.Fatalf("binding was not preserved: %+v", binding)
				}
			}
		}
		if !found {
			t.Fatal("saved binding missing from list")
		}
	}
}
