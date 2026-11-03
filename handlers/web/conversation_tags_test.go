package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"inbrix/models"
)

func TestConversationTagsAPIAndPaginatedFilters(t *testing.T) {
	h, app, account, _ := outboxTestApp(t)
	app.Put("/api/conversations/:id/tags", h.HandleConversationTagsJSON)
	id := conversationID(account.ID, []string{"peer@example.com"})
	put := func(body string, outsider bool) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/conversations/"+id+"/tags", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if outsider {
			req.Header.Set("X-Test-Outsider", "true")
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if status := put(`{"tags":[" 跟进 ","重点","跟进"]}`, false); status != http.StatusOK {
		t.Fatalf("save tags status = %d", status)
	}
	for _, body := range []string{`{}`, `{"tags":null}`, `{"tags":"bad"}`, `{"tags":[""]}`, `{"tags":["line\nbreak"]}`, `{"tags":["` + strings.Repeat("字", 33) + `"]}`} {
		if status := put(body, false); status != http.StatusBadRequest {
			t.Fatalf("invalid body %s status = %d", body, status)
		}
	}
	if status := put(`{"tags":["stolen"]}`, true); status != http.StatusNotFound {
		t.Fatalf("outsider write status = %d", status)
	}
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations/"+id, nil))
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Conversation ConversationDetailJSON `json:"conversation"`
	}
	err = json.NewDecoder(resp.Body).Decode(&detail)
	resp.Body.Close()
	if err != nil || !reflect.DeepEqual(detail.Conversation.Tags, []string{"跟进", "重点"}) {
		t.Fatalf("detail tags = %v, err = %v", detail.Conversation.Tags, err)
	}
	ctx := context.Background()
	if err := h.mailDB.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{ID: "2", From: "untagged@example.com", To: account.Email, Subject: "Newest", Date: time.Now().Add(time.Minute)},
		{ID: "3", From: "completed@example.com", To: account.Email, Subject: "Completed", Date: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	completedID := conversationID(account.ID, []string{"completed@example.com"})
	if err := h.mailDB.SetConversationTags(ctx, account.ID, completedID, []string{"跟进"}); err != nil {
		t.Fatal(err)
	}
	if err := h.mailDB.SetConversationStatus(ctx, account.ID, completedID, "answered", "INBOX/3"); err != nil {
		t.Fatal(err)
	}
	get := func(path string) ConversationListJSON {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result ConversationListJSON
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := get("/api/conversations?filter=open&limit=1")
	if len(first.Conversations) != 1 || len(first.Conversations[0].Tags) != 0 || !reflect.DeepEqual(first.Tags, []string{"跟进", "重点"}) {
		t.Fatalf("first page must include later-page and completed tags: %+v", first)
	}
	path := "/api/conversations?filter=all&limit=1&tag=" + url.QueryEscape("跟进")
	filtered := get(path)
	if filtered.Total != 2 || len(filtered.Conversations) != 1 || filtered.Conversations[0].ID != completedID || !filtered.HasMore {
		t.Fatalf("tag filter was not applied before pagination: %+v", filtered)
	}
	next := get(path + "&cursor=" + url.QueryEscape(filtered.NextCursor))
	if len(next.Conversations) != 1 || next.Conversations[0].ID != id || next.HasMore || next.Total != 2 {
		t.Fatalf("filtered continuation: %+v", next)
	}
	open := get("/api/conversations?filter=open&tag=" + url.QueryEscape("跟进"))
	if open.Total != 1 || open.Conversations[0].ID != id {
		t.Fatalf("combined filters: %+v", open)
	}
	if status := put(`{"tags":[]}`, false); status != http.StatusOK {
		t.Fatalf("clear tags status = %d", status)
	}
	if cleared := get("/api/conversations?tag=" + url.QueryEscape("重点")); cleared.Total != 0 {
		t.Fatalf("cleared tag still matched: %+v", cleared)
	}
}
