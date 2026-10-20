package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConversationNoteSharedByListAndTranscript(t *testing.T) {
	h, app, account, _ := outboxTestApp(t)
	app.Put("/api/conversations/:id/note", h.HandleConversationNoteJSON)
	id := conversationID(account.ID, []string{"peer@example.com"})
	for _, note := range []string{"下周跟进\n确认报价", ""} {
		body, _ := json.Marshal(map[string]string{"note": note})
		req := httptest.NewRequest(http.MethodPut, "/api/conversations/"+id+"/note", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("save note: %d", resp.StatusCode)
		}

		resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations/"+id, nil))
		if err != nil {
			t.Fatal(err)
		}
		var detail struct {
			Conversation ConversationDetailJSON `json:"conversation"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if detail.Conversation.Note != note {
			t.Fatalf("transcript note = %q, want %q", detail.Conversation.Note, note)
		}

		resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations", nil))
		if err != nil {
			t.Fatal(err)
		}
		var list ConversationListJSON
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if len(list.Conversations) != 1 || list.Conversations[0].Note != note {
			t.Fatalf("list note differs from transcript: %+v", list.Conversations)
		}
	}
}
