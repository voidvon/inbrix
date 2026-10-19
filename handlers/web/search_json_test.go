package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		ConversationID string `json:"conversationId"`
		MessageID      string `json:"messageId"`
		Folder         string `json:"folder"`
		Subject        string `json:"subject"`
		Snippet        string `json:"snippet"`
		MatchedSubject bool   `json:"matchedSubject"`
		MatchedBody    bool   `json:"matchedBody"`
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
			ID: "100", Folder: "INBOX", From: "alice@example.com", FromName: "Alice", To: "user@example.com",
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

	search := func(t *testing.T, query string) (int, mailSearchResponse) {
		t.Helper()
		response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/mail/search?q="+query, nil))
		if err != nil {
			t.Fatal(err)
		}
		body := mailSearchResponse{}
		if response.StatusCode == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
		}
		return response.StatusCode, body
	}

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
}
