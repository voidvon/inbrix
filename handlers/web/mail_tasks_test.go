package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/models"
)

func TestMailTaskListIsOwnerScopedAndOmitsTokens(t *testing.T) {
	email, app, account, other := outboxTestApp(t)
	h := NewAISettingsHandler(email.store, email.config, email.mailDB)
	app.Get("/api/settings/ai/mail-tasks", h.HandleListMailTasks)
	app.Post("/api/settings/ai/mail-tasks/retry", h.HandleRetryMailTask)
	if err := email.mailDB.UpsertIncomingMessages(context.Background(), account.ID, "INBOX", []models.Email{{ID: "2", From: "buyer@example.com", To: account.Email, Body: "RFQ"}}); err != nil {
		t.Fatal(err)
	}
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/settings/ai/mail-tasks?accountEmail="+account.Email, nil))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(raw), `"taskType":"analysis"`) || strings.Contains(string(raw), "generation_token") || strings.Contains(string(raw), "payload") {
		t.Fatalf("response=%d %s", response.StatusCode, raw)
	}
	response, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/settings/ai/mail-tasks?accountEmail="+other.Email, nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("other owner's tasks returned status %d", response.StatusCode)
	}
	for _, address := range []string{account.Email, other.Email} {
		req := httptest.NewRequest(http.MethodPost, "/api/settings/ai/mail-tasks/retry", strings.NewReader(`{"accountEmail":"`+address+`","folder":"INBOX","messageId":"2","taskType":"notification"}`))
		req.Header.Set("Content-Type", "application/json")
		response, err = app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("invalid retry allowed: %d", response.StatusCode)
		}
	}
}
