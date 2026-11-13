package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"inbrix/config"
	"inbrix/handlers/web"
	"inbrix/mailstore"
	"inbrix/models"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

type mailSearchResponse struct {
	Query   string   `json:"query"`
	Terms   []string `json:"terms"`
	HasMore bool     `json:"hasMore"`
	Results []struct {
		Kind           string                       `json:"kind"`
		Conversation   *web.ConversationSummaryJSON `json:"conversation"`
		MatchedNote    bool                         `json:"matchedNote"`
		ConversationID string                       `json:"conversationId"`
		MessageID      string                       `json:"messageId"`
		Folder         string                       `json:"folder"`
		Subject        string                       `json:"subject"`
		Snippet        string                       `json:"snippet"`
		MatchedSubject bool                         `json:"matchedSubject"`
		MatchedBody    bool                         `json:"matchedBody"`
	} `json:"results"`
}

func TestHandleMailSearchJSON(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := db.CreateUser(ctx, "searcher", "Searcher", "password123")
	if err != nil {
		t.Fatal(err)
	}
	account, err := db.UpsertAccount(ctx, mailstore.Account{
		OwnerID:           user.ID,
		Email:             "user@example.com",
		Username:          "user@example.com",
		EncryptedPassword: "enc",
		IMAPServer:        "imap.example.com",
		IMAPPort:          993,
		SMTPServer:        "smtp.example.com",
		SMTPPort:          587,
		IsDefault:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"INBOX", "Sent"} {
		if err := db.UpsertFolder(ctx, mailstore.Folder{AccountID: account.ID, Name: folder}); err != nil {
			t.Fatal(err)
		}
	}

	when := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{
			ID: "100", Folder: "INBOX", From: "alice@example.com", FromName: "Alice", To: "user@example.com", Cc: "logistics@example.com",
			Subject: "Quarterly report", Body: "The invoice total is 4200 EUR.", MessageID: "<m100@example.com>",
			Date: when, BodyCached: true,
		},
		{
			// HTML-only mail: no text/plain part, so the body column is empty and
			// only the HTML projection carries the searchable content.
			ID: "101", Folder: "INBOX", From: "news@example.com", To: "user@example.com",
			Subject: "Newsletter", HTML: `<html><head><style>.a{color:#333}</style></head><body><p>Spring sale on widgets</p></body></html>`,
			MessageID: "<m101@example.com>", Date: when.Add(time.Hour), BodyCached: true,
		},
		{
			// The term only appears inside markup, so this message must not be
			// reported as a match for "widget".
			ID: "102", Folder: "INBOX", From: "bob@example.com", To: "user@example.com",
			Subject: "Plain note", HTML: `<html><head><style>.widget{color:red}</style></head><body><p>Nothing to see</p></body></html>`,
			MessageID: "<m102@example.com>", Date: when.Add(2 * time.Hour), BodyCached: true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateFolderStats(ctx, account.ID, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessages(ctx, account.ID, "Sent", []models.Email{{
		ID: "200", From: account.Email, To: "alice@example.com", Subject: "Latest reply",
		Preview: "Latest reply preview", Body: "Latest reply body", Date: when.Add(3 * time.Hour),
		Attachments: []models.Attachment{{ID: "report", Filename: "report.pdf", ContentType: "application/pdf"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveDraft(ctx, user.ID, mailstore.DraftInput{
		AccountEmail: account.Email, ConversationID: "draft-search", Mode: mailstore.DraftModeNew,
		To: "draft@example.com", Subject: "Unsent subject", BodyText: "Unsent preview",
	}); err != nil {
		t.Fatal(err)
	}

	sessStore := session.New()
	cfg := &config.Config{}
	authHandler := web.NewAuthHandler(sessStore, cfg)
	emailHandler := web.NewEmailHandler(sessStore, cfg, authHandler)
	emailHandler.SetMailMirror(db)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("username", user.Login)
		sess, sErr := sessStore.Get(c)
		if sErr == nil {
			sess.Set("user_id", user.ID)
			sess.Set("account_id", account.ID)
			sess.Set("username", user.Login)
			sess.Set("token", "test-token")
			_ = sess.Save()
		}
		return c.Next()
	})
	app.Get("/api/mail/search", emailHandler.HandleMailSearchJSON)
	app.Get("/api/conversations", emailHandler.HandleConversationListJSON)
	listConversations := func(t *testing.T) []web.ConversationSummaryJSON {
		t.Helper()
		response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations", nil))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var list struct {
			Conversations []web.ConversationSummaryJSON `json:"conversations"`
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("list status = %d", response.StatusCode)
		}
		if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
			t.Fatal(err)
		}
		return list.Conversations
	}
	var alice web.ConversationSummaryJSON
	for _, conversation := range listConversations(t) {
		if conversation.PeerEmail == "alice@example.com" {
			alice = conversation
		}
	}
	if alice.ID == "" {
		t.Fatal("Alice conversation missing")
	}
	if err := db.SetConversationNote(ctx, account.ID, alice.ID, "重点客户 华南项目"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConversationTags(ctx, account.ID, alice.ID, []string{"客户"}); err != nil {
		t.Fatal(err)
	}
	// An orphaned note and another mailbox's note must never create search hits.
	if err := db.SetConversationNote(ctx, account.ID, "deleted", "已删除对话"); err != nil {
		t.Fatal(err)
	}
	other, err := db.UpsertAccount(ctx, mailstore.Account{
		OwnerID: user.ID, Email: "other@example.com", Username: "other@example.com", EncryptedPassword: "enc",
		IMAPServer: "imap.example.com", IMAPPort: 993, SMTPServer: "smtp.example.com", SMTPPort: 587,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConversationNote(ctx, other.ID, alice.ID, "仅其他邮箱"); err != nil {
		t.Fatal(err)
	}
	for _, conversation := range listConversations(t) {
		if conversation.ID == alice.ID {
			alice = conversation
		}
	}

	search := func(t *testing.T, query string) (int, mailSearchResponse) {
		t.Helper()
		response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/mail/search?q="+url.QueryEscape(query), nil))
		if err != nil {
			t.Fatal(err)
		}
		body := mailSearchResponse{}
		defer response.Body.Close()
		if response.StatusCode == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
		}
		return response.StatusCode, body
	}

	for _, query := range []string{"ALICE@example.com", "alice@", "logistics@example.com", "重点客户", "华南", "alice 华南"} {
		t.Run("conversation lookup "+query, func(t *testing.T) {
			status, body := search(t, query)
			if status != http.StatusOK || len(body.Results) != 1 {
				t.Fatalf("status=%d results=%+v", status, body.Results)
			}
			result := body.Results[0]
			if result.Kind != "conversation" || result.MessageID != "" || !reflect.DeepEqual(result.Conversation, &alice) {
				t.Fatalf("search result differs from inbox summary: %+v, want %+v", result, alice)
			}
			if alice.Count != 2 || alice.Preview != "Latest reply preview" || !alice.LatestHasAttachments {
				t.Fatalf("unexpected inbox fixture: %+v", alice)
			}
			if query == "华南" && !result.MatchedNote {
				t.Fatal("note match not identified")
			}
		})
	}
	for _, query := range []string{"华南 missing", "from:nobody@example.com 华南", "仅其他邮箱", "已删除对话"} {
		t.Run("no conversation leak "+query, func(t *testing.T) {
			status, body := search(t, query)
			if status != http.StatusOK || len(body.Results) != 0 {
				t.Fatalf("status=%d results=%+v", status, body.Results)
			}
		})
	}
	t.Run("draft only conversation", func(t *testing.T) {
		_, body := search(t, "draft@example.com")
		if len(body.Results) != 1 || body.Results[0].Conversation == nil || body.Results[0].Conversation.Status != "draft" || body.Results[0].Conversation.Count != 0 {
			t.Fatalf("draft lookup = %+v", body.Results)
		}
	})
	t.Run("combined limit", func(t *testing.T) {
		response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/mail/search?q=example.com&limit=1", nil))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body mailSearchResponse
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || len(body.Results) != 1 || !body.HasMore || body.Results[0].Kind != "conversation" {
			t.Fatalf("limited results = %+v", body)
		}
	})
	t.Run("note changes take effect", func(t *testing.T) {
		if err := db.SetConversationNote(ctx, account.ID, alice.ID, "欧洲客户"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.SetConversationNote(ctx, account.ID, alice.ID, alice.Note); err != nil {
				t.Error(err)
			}
		})
		if _, body := search(t, "华南"); len(body.Results) != 0 {
			t.Fatalf("old note still searchable: %+v", body.Results)
		}
		_, body := search(t, "欧洲客户")
		if len(body.Results) != 1 || body.Results[0].Conversation == nil || body.Results[0].Conversation.Note != "欧洲客户" || !body.Results[0].MatchedNote {
			t.Fatalf("updated note not searchable: %+v", body.Results)
		}
	})

	t.Run("body match", func(t *testing.T) {
		status, body := search(t, "invoice")
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if len(body.Results) != 1 || body.Results[0].MessageID != "100" {
			t.Fatalf("results = %+v", body.Results)
		}
		if !body.Results[0].MatchedBody || body.Results[0].MatchedSubject {
			t.Fatalf("match flags = %+v", body.Results[0])
		}
		if body.Results[0].ConversationID == "" {
			t.Fatal("conversation id is empty")
		}
	})

	t.Run("html only body match", func(t *testing.T) {
		status, body := search(t, "widgets")
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if len(body.Results) != 1 || body.Results[0].MessageID != "101" {
			t.Fatalf("results = %+v", body.Results)
		}
		if !body.Results[0].MatchedBody {
			t.Fatalf("expected a body match: %+v", body.Results[0])
		}
		if body.Results[0].Snippet == "" {
			t.Fatal("snippet is empty for an HTML-only match")
		}
	})

	t.Run("markup only match is filtered", func(t *testing.T) {
		status, body := search(t, "widget")
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		for _, result := range body.Results {
			if result.MessageID == "102" {
				t.Fatalf("markup-only match leaked into results: %+v", result)
			}
		}
	})

	t.Run("subject match", func(t *testing.T) {
		status, body := search(t, "Quarterly")
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if len(body.Results) != 1 || !body.Results[0].MatchedSubject {
			t.Fatalf("results = %+v", body.Results)
		}
	})

	t.Run("result resolves to a conversation", func(t *testing.T) {
		_, body := search(t, "widgets")
		if len(body.Results) != 1 {
			t.Fatalf("results = %+v", body.Results)
		}
		response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations", nil))
		if err != nil {
			t.Fatal(err)
		}
		var list struct {
			Conversations []struct {
				ID string `json:"id"`
			} `json:"conversations"`
		}
		if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, conversation := range list.Conversations {
			if conversation.ID == body.Results[0].ConversationID {
				found = true
			}
		}
		if !found {
			t.Fatalf("conversation %q not present in the inbox list", body.Results[0].ConversationID)
		}
	})

	t.Run("empty query is rejected", func(t *testing.T) {
		status, _ := search(t, "")
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", status)
		}
	})

	t.Run("pending outgoing message", func(t *testing.T) {
		record, err := db.CreateOutgoing(ctx, mailstore.OutgoingMessage{
			AccountID: account.ID, ConversationID: alice.ID,
			Message: models.Email{
				From: account.Email, To: "alice@example.com", Subject: "Pending reply",
				Body: "outbox-search-token", Date: when.Add(4 * time.Hour), MessageID: "<pending-search@example.com>",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		status, body := search(t, "outbox-search-token")
		if status != http.StatusOK || len(body.Results) != 1 {
			t.Fatalf("outgoing search status=%d results=%+v", status, body.Results)
		}
		result := body.Results[0]
		if result.MessageID != record.ID || result.Folder != "Outbox" || result.ConversationID != alice.ID || !result.MatchedBody {
			t.Fatalf("outgoing result = %+v", result)
		}
	})
}
