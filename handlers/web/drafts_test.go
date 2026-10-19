package web_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/handlers/web"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

type draftAPI struct {
	app   *fiber.App
	store *mailstore.Store
	owner string
}

func newDraftApp(t *testing.T) *draftAPI {
	t.Helper()
	ctx := context.Background()
	store, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	owner, err := store.CreateUser(ctx, "drafter", "Drafter", "hash")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := store.CreateUser(ctx, "outsider", "Outsider", "hash")
	if err != nil {
		t.Fatal(err)
	}

	handler := web.NewDraftHandler(session.New(), store)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if header := c.Get("X-Test-Anonymous"); header != "" {
			return c.Next()
		}
		current := owner.ID
		if c.Get("X-Test-Outsider") != "" {
			current = outsider.ID
		}
		c.Locals("user_id", current)
		return c.Next()
	})
	app.Get("/api/drafts", handler.HandleListDrafts)
	app.Post("/api/drafts", handler.HandleSaveDraft)
	app.Get("/api/drafts/:id", handler.HandleGetDraft)
	app.Delete("/api/drafts/:id", handler.HandleDeleteDraft)
	return &draftAPI{app: app, store: store, owner: owner.ID}
}

func (api *draftAPI) request(t *testing.T, method, path, body string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := api.app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload := map[string]any{}
	data, _ := io.ReadAll(response.Body)
	if len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	return response.StatusCode, payload
}

func (api *draftAPI) save(t *testing.T, body string) map[string]any {
	t.Helper()
	status, payload := api.request(t, http.MethodPost, "/api/drafts", body, nil)
	if status != http.StatusOK {
		t.Fatalf("save draft %d: %+v", status, payload)
	}
	draft, ok := payload["draft"].(map[string]any)
	if !ok {
		t.Fatalf("save draft returned no draft: %+v", payload)
	}
	return draft
}

func TestDraftRoundTrip(t *testing.T) {
	api := newDraftApp(t)

	saved := api.save(t, `{"accountEmail":"me@example.com","conversationId":"conv-1","mode":"reply",
		"to":"peer@example.com","cc":"boss@example.com","subject":"Re: quote","inReplyTo":"<m1@example.com>",
		"references":"<m0@example.com> <m1@example.com>","bodyHtml":"<p>half written</p>"}`)

	id, _ := saved["id"].(string)
	if id == "" {
		t.Fatalf("draft has no id: %+v", saved)
	}
	if saved["bodyText"] != "half written" {
		t.Fatalf("plain-text alternative was not derived: %+v", saved)
	}
	if saved["conversationId"] != "conv-1" || saved["mode"] != "reply" {
		t.Fatalf("draft identity lost: %+v", saved)
	}

	status, payload := api.request(t, http.MethodGet, "/api/drafts/"+id, "", nil)
	if status != http.StatusOK {
		t.Fatalf("get draft %d: %+v", status, payload)
	}
	fetched := payload["draft"].(map[string]any)
	if fetched["bodyHtml"] != "<p>half written</p>" {
		t.Fatalf("stored body = %v", fetched["bodyHtml"])
	}

	status, payload = api.request(t, http.MethodGet, "/api/drafts?conversation_id=conv-1", "", nil)
	if status != http.StatusOK {
		t.Fatalf("list drafts %d: %+v", status, payload)
	}
	if drafts := payload["drafts"].([]any); len(drafts) != 1 {
		t.Fatalf("drafts for conversation = %+v", drafts)
	}

	// Autosave sends the handle back and must land on the same row.
	updated := api.save(t, `{"id":"`+id+`","accountEmail":"me@example.com","conversationId":"conv-1","mode":"reply_all","to":"peer@example.com","subject":"Re: quote","bodyHtml":"<p>nearly done</p>"}`)
	if updated["id"] != id {
		t.Fatalf("autosave created a second draft: %v -> %v", id, updated["id"])
	}

	status, _ = api.request(t, http.MethodDelete, "/api/drafts/"+id, "", nil)
	if status != http.StatusOK {
		t.Fatalf("delete draft = %d", status)
	}
	if status, _ := api.request(t, http.MethodGet, "/api/drafts/"+id, "", nil); status != http.StatusNotFound {
		t.Fatalf("get deleted draft = %d, want 404", status)
	}
	// Discarding twice is not an error.
	if status, _ := api.request(t, http.MethodDelete, "/api/drafts/"+id, "", nil); status != http.StatusOK {
		t.Fatalf("second delete = %d, want 200", status)
	}
}

// A draft body is handed back to the compose editor, which assigns it to a
// contenteditable in the MAIN document. Raw mail HTML there is a stored-XSS
// sink, so the server must never persist active content — whatever the client
// sends.
func TestDraftBodyIsSanitizedOnWrite(t *testing.T) {
	api := newDraftApp(t)

	hostile := "<div>Draft body</div>" +
		`<img src=x onerror="fetch('/api/send',{method:'POST'})">` +
		"<script>alert(document.cookie)</script>" +
		"<svg onload=alert(1)></svg>" +
		`<a href="javascript:alert(1)">x</a>` +
		`<iframe src="https://evil.example"></iframe>` +
		"<style>body{display:none}</style>"
	encoded, err := json.Marshal(map[string]string{"accountEmail": "me@example.com", "subject": "xss", "bodyHtml": hostile})
	if err != nil {
		t.Fatal(err)
	}
	saved := api.save(t, string(encoded))

	body, _ := saved["bodyHtml"].(string)
	lowered := strings.ToLower(body)
	for _, banned := range []string{"onerror", "onload", "<script", "<iframe", "<style", "<svg", "javascript:"} {
		if strings.Contains(lowered, banned) {
			t.Errorf("stored draft still contains %q: %q", banned, body)
		}
	}
	if !strings.Contains(lowered, "draft body") {
		t.Errorf("benign draft content was lost: %q", body)
	}

	// The sanitized value is what a later read serves, not just the response.
	status, payload := api.request(t, http.MethodGet, "/api/drafts/"+saved["id"].(string), "", nil)
	if status != http.StatusOK {
		t.Fatalf("get draft = %d", status)
	}
	if reread := payload["draft"].(map[string]any)["bodyHtml"].(string); strings.Contains(strings.ToLower(reread), "<script") {
		t.Fatalf("re-read draft is not sanitized: %q", reread)
	}
}

// Drafts are owner-scoped: another account's id answers exactly like a missing
// one, and can neither be read nor overwritten.
func TestDraftsAreNotSharedBetweenAccounts(t *testing.T) {
	api := newDraftApp(t)
	outsider := map[string]string{"X-Test-Outsider": "1"}

	saved := api.save(t, `{"accountEmail":"me@example.com","conversationId":"conv-1","subject":"private","bodyHtml":"<p>secret</p>"}`)
	id := saved["id"].(string)

	if status, _ := api.request(t, http.MethodGet, "/api/drafts/"+id, "", outsider); status != http.StatusNotFound {
		t.Fatalf("cross-account read = %d, want 404", status)
	}
	if status, payload := api.request(t, http.MethodGet, "/api/drafts", "", outsider); status != http.StatusOK || len(payload["drafts"].([]any)) != 0 {
		t.Fatalf("cross-account list = %d %+v", status, payload)
	}
	// A cross-account delete is a no-op, not a way to destroy someone's draft.
	if status, _ := api.request(t, http.MethodDelete, "/api/drafts/"+id, "", outsider); status != http.StatusOK {
		t.Fatalf("cross-account delete = %d", status)
	}
	status, payload := api.request(t, http.MethodGet, "/api/drafts/"+id, "", nil)
	if status != http.StatusOK || payload["draft"].(map[string]any)["bodyHtml"] != "<p>secret</p>" {
		t.Fatalf("owner draft was affected: %d %+v", status, payload)
	}
}

func TestDraftRejectsBadRequests(t *testing.T) {
	api := newDraftApp(t)

	if status, _ := api.request(t, http.MethodPost, "/api/drafts", `{"subject":"x"}`, map[string]string{"X-Test-Anonymous": "1"}); status != http.StatusUnauthorized {
		t.Fatalf("anonymous save = %d, want 401", status)
	}
	if status, _ := api.request(t, http.MethodGet, "/api/drafts", "", map[string]string{"X-Test-Anonymous": "1"}); status != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", status)
	}
	if status, _ := api.request(t, http.MethodPost, "/api/drafts", `{"mode":"sneaky"}`, nil); status != http.StatusBadRequest {
		t.Fatalf("unknown mode = %d, want 400", status)
	}
	if status, _ := api.request(t, http.MethodPost, "/api/drafts", `not json`, nil); status != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", status)
	}
	if status, _ := api.request(t, http.MethodGet, "/api/drafts/missing", "", nil); status != http.StatusNotFound {
		t.Fatalf("missing draft = %d, want 404", status)
	}

	// A rejected save must not leave a row behind.
	status, payload := api.request(t, http.MethodGet, "/api/drafts", "", nil)
	if status != http.StatusOK {
		t.Fatalf("list drafts = %d", status)
	}
	if drafts := payload["drafts"].([]any); len(drafts) != 0 {
		t.Fatalf("rejected drafts were persisted: %+v", drafts)
	}
}
