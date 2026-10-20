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

func TestStartConversationDraftLifecycle(t *testing.T) {
	h, app, account, other := outboxTestApp(t)
	app.Post("/api/conversations", h.HandleStartConversationJSON)
	app.Delete("/api/conversations/:id", h.HandleConversationDeleteJSON)
	start := func(to string) ConversationDetailJSON {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"to": to, "accountEmail": account.Email})
		req := httptest.NewRequest(http.MethodPost, "/api/conversations", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("start: status %d", resp.StatusCode)
		}
		var result struct {
			Conversation ConversationDetailJSON `json:"conversation"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result.Conversation
	}
	created := start("NEW@example.com")
	if created.ID != conversationID(account.ID, []string{"new@example.com"}) || len(created.Messages) != 0 || created.PeerEmail != "new@example.com" {
		t.Fatalf("new conversation = %+v", created)
	}
	ctx := context.Background()
	drafts, err := h.mailDB.ListDrafts(ctx, account.OwnerID, created.ID)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("drafts = %+v, %v", drafts, err)
	}
	if _, err := h.mailDB.SaveDraft(ctx, account.OwnerID, mailstore.DraftInput{
		ID: drafts[0].ID, AccountEmail: account.Email, ConversationID: created.ID,
		Mode: mailstore.DraftModeNew, To: "new@example.com", Subject: "Unsent subject", BodyText: "Work in progress",
	}); err != nil {
		t.Fatal(err)
	}
	if again := start("new@example.com"); again.ID != created.ID || again.Subject != "Unsent subject" {
		t.Fatalf("reopening lost draft: %+v", again)
	}
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations/"+created.ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("reload draft conversation: %d", resp.StatusCode)
	}

	// An existing peer opens its original transcript and keeps its draft intact.
	existing := start("PEER@example.com")
	if len(existing.Messages) != 1 {
		t.Fatalf("existing transcript = %+v", existing)
	}
	if existing.ID == created.ID {
		t.Fatal("different peers merged")
	}

	// A draft-only conversation must disappear when deleted.
	resp, err = app.Test(httptest.NewRequest(http.MethodDelete, "/api/conversations/"+created.ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	drafts, err = h.mailDB.ListDrafts(ctx, account.OwnerID, created.ID)
	if err != nil || len(drafts) != 0 {
		t.Fatalf("deleted drafts = %+v, %v", drafts, err)
	}
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations/"+created.ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("deleted conversation returned %d", resp.StatusCode)
	}

	// Sending the first email replaces the draft with a real transcript while
	// preserving its conversation identity.
	recreated := start("new@example.com")
	if recreated.ID != created.ID {
		t.Fatal("recipient identity changed after recreation")
	}
	submitOutbox(t, app, "/api/compose", map[string]string{
		"to": "new@example.com", "account_email": account.Email,
		"conversation_id": recreated.ID, "in_reply_to": "", "html_body": "<p>First email</p>",
	}, false)
	sent := start("new@example.com")
	if sent.ID != created.ID || len(sent.Messages) != 1 || sent.Messages[0].InReplyTo != "" {
		t.Fatalf("sent conversation = %+v", sent)
	}
	drafts, err = h.mailDB.ListDrafts(ctx, account.OwnerID, created.ID)
	if err != nil || len(drafts) != 0 {
		t.Fatalf("sent draft still present: %+v, %v", drafts, err)
	}

	for _, test := range []struct {
		to, email string
		status    int
	}{
		{"invalid", account.Email, 400},
		{"a@example.com, b@example.com", account.Email, 400},
		{"new@example.com", other.Email, 404},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/conversations", strings.NewReader(fmt.Sprintf(`{"to":%q,"accountEmail":%q}`, test.to, test.email)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != test.status {
			t.Errorf("start %q for %q: %d, want %d", test.to, test.email, resp.StatusCode, test.status)
		}
	}
}

func TestConversationDraftStatusOverridesAndRestoresProcessingStatus(t *testing.T) {
	h, app, account, other := outboxTestApp(t)
	ctx := context.Background()
	id := conversationID(account.ID, []string{"peer@example.com"})
	status := func() string {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations", nil))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list status: %d", resp.StatusCode)
		}
		var list ConversationListJSON
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatal(err)
		}
		for _, conversation := range list.Conversations {
			if conversation.ID == id {
				return conversation.Status
			}
		}
		t.Fatal("conversation missing")
		return ""
	}
	if got := status(); got != "unanswered" {
		t.Fatalf("initial status = %q", got)
	}
	if err := h.mailDB.SetConversationStatus(ctx, account.ID, id, "no_action", "INBOX/1"); err != nil {
		t.Fatal(err)
	}
	// Another account's draft must never affect the displayed status.
	if _, err := h.mailDB.SaveDraft(ctx, other.OwnerID, mailstore.DraftInput{
		AccountEmail: other.Email, ConversationID: id, To: "peer@example.com", BodyText: "Private draft",
	}); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "no_action" {
		t.Fatalf("manual status = %q", got)
	}
	save := func() mailstore.Draft {
		t.Helper()
		draft, err := h.mailDB.SaveDraft(ctx, account.OwnerID, mailstore.DraftInput{
			AccountEmail: account.Email, ConversationID: id, To: "peer@example.com",
			Mode: mailstore.DraftModeReply, InReplyTo: "<incoming@example.com>", BodyText: "Half-written reply",
		})
		if err != nil {
			t.Fatal(err)
		}
		return draft
	}
	draft := save()
	if got := status(); got != "draft" {
		t.Fatalf("half-written status = %q", got)
	}
	if err := h.mailDB.DeleteDraft(ctx, account.OwnerID, draft.ID); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "no_action" {
		t.Fatalf("discard must restore processing status, got %q", got)
	}
	save()
	submitOutbox(t, app, "/api/compose", map[string]string{
		"to": "peer@example.com", "account_email": account.Email,
		"conversation_id": id, "html_body": "<p>Finished reply</p>",
	}, false)
	if got := status(); got == "draft" {
		t.Fatal("sending must clear draft status")
	}
}
