package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"inbrix/config"
	"inbrix/handlers/web"
	"inbrix/mailstore"
	"inbrix/models"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

func TestConversationAIChatEndpoints(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := db.CreateUser(ctx, "chatuser", "Chat User", "password123")
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

	if err := db.UpsertFolder(ctx, mailstore.Folder{
		AccountID: account.ID,
		Name:      "INBOX",
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{
			ID:        "100",
			Folder:    "INBOX",
			Subject:   "Project Discussion",
			From:      "alice@example.com",
			To:        "user@example.com",
			Date:      now,
			Body:      "Let's discuss the requirements.",
			MessageID: "<msg1@example.com>",
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

	app.Get("/api/conversations", emailHandler.HandleConversationListJSON)
	app.Get("/api/conversations/:id/ai-chat", emailHandler.HandleGetConversationAIChatJSON)
	app.Put("/api/conversations/:id/ai-chat", emailHandler.HandleSaveConversationAIChatJSON)
	app.Delete("/api/conversations/:id/ai-chat", emailHandler.HandleDeleteConversationAIChatJSON)

	// 1. Fetch conversation ID
	listReq := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	listResp, err := app.Test(listReq)
	if err != nil {
		t.Fatal(err)
	}
	var listBody struct {
		Conversations []struct {
			ID string `json:"id"`
		} `json:"conversations"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Conversations) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(listBody.Conversations))
	}
	convID := listBody.Conversations[0].ID

	// 2. Initial GET should return empty messages
	getReq := httptest.NewRequest(http.MethodGet, "/api/conversations/"+convID+"/ai-chat", nil)
	getResp, err := app.Test(getReq)
	if err != nil {
		t.Fatal(err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", getResp.StatusCode)
	}
	var getBody struct {
		OK       bool  `json:"ok"`
		Messages []any `json:"messages"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&getBody); err != nil {
		t.Fatal(err)
	}
	if !getBody.OK || len(getBody.Messages) != 0 {
		t.Fatalf("expected ok=true and empty messages, got ok=%v, len=%d", getBody.OK, len(getBody.Messages))
	}

	// 3. PUT to save AI chat messages
	savePayload := `{"messages":[{"id":"1","role":"user","content":"summary"},{"id":"2","role":"assistant","content":"here is summary"}]}`
	putReq := httptest.NewRequest(http.MethodPut, "/api/conversations/"+convID+"/ai-chat", strings.NewReader(savePayload))
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := app.Test(putReq)
	if err != nil {
		t.Fatal(err)
	}
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("put status = %d", putResp.StatusCode)
	}

	// 4. GET again should return the 2 saved messages
	getReq2 := httptest.NewRequest(http.MethodGet, "/api/conversations/"+convID+"/ai-chat", nil)
	getResp2, err := app.Test(getReq2)
	if err != nil {
		t.Fatal(err)
	}
	var getBody2 struct {
		OK       bool `json:"ok"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(getResp2.Body).Decode(&getBody2); err != nil {
		t.Fatal(err)
	}
	if !getBody2.OK || len(getBody2.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(getBody2.Messages))
	}
	if getBody2.Messages[0].Content != "summary" || getBody2.Messages[1].Content != "here is summary" {
		t.Fatalf("unexpected messages: %v", getBody2.Messages)
	}

	// 5. DELETE to clear AI chat messages
	delReq := httptest.NewRequest(http.MethodDelete, "/api/conversations/"+convID+"/ai-chat", nil)
	delResp, err := app.Test(delReq)
	if err != nil {
		t.Fatal(err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", delResp.StatusCode)
	}

	// 6. GET should be empty again
	getReq3 := httptest.NewRequest(http.MethodGet, "/api/conversations/"+convID+"/ai-chat", nil)
	getResp3, err := app.Test(getReq3)
	if err != nil {
		t.Fatal(err)
	}
	var getBody3 struct {
		OK       bool  `json:"ok"`
		Messages []any `json:"messages"`
	}
	if err := json.NewDecoder(getResp3.Body).Decode(&getBody3); err != nil {
		t.Fatal(err)
	}
	if len(getBody3.Messages) != 0 {
		t.Fatalf("expected 0 messages after delete, got %d", len(getBody3.Messages))
	}
}
