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

func TestHandleConversationDeleteJSONQueuesMoves(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := db.CreateUser(ctx, "testuser", "Test User", "password123")
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

	for _, folder := range []string{"INBOX", "Sent", "Trash"} {
		attrs := []string{}
		if folder == "Trash" {
			attrs = []string{`\Trash`}
		}
		if err := db.UpsertFolder(ctx, mailstore.Folder{
			AccountID:  account.ID,
			Name:       folder,
			Attributes: attrs,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{
			ID:        "100",
			Folder:    "INBOX",
			From:      "alice@example.com",
			To:        "user@example.com",
			Subject:   "Hello",
			Body:      "World",
			MessageID: "<m1@example.com>",
			Date:      time.Now(),
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
	app.Delete("/api/conversations/:id", emailHandler.HandleConversationDeleteJSON)
	app.Delete("/api/conversations/:id/messages/:uid", emailHandler.HandleConversationMessageDeleteJSON)

	// 1. Fetch conversation list to get conversation ID
	listReq := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	listResp, err := app.Test(listReq)
	if err != nil {
		t.Fatal(err)
	}
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", listResp.StatusCode)
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

	// 2. Delete the conversation
	delReq := httptest.NewRequest(http.MethodDelete, "/api/conversations/"+convID, nil)
	delResp, err := app.Test(delReq)
	if err != nil {
		t.Fatal(err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete conversation status = %d", delResp.StatusCode)
	}
	var delBody struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(delResp.Body).Decode(&delBody); err != nil {
		t.Fatal(err)
	}
	if !delBody.OK {
		t.Fatalf("delete body not ok: %+v", delBody)
	}

	// 3. Verify message is deleted from local messages table
	if _, err := db.GetMessage(ctx, account.ID, "INBOX", "100"); err == nil {
		t.Fatal("expected INBOX/100 to be deleted locally")
	}

	// 4. Verify move is queued in pending_message_moves
	pending, err := db.ListPendingMessageMoves(ctx, account.ID, time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].UID != "100" || pending[0].FolderName != "INBOX" {
		t.Fatalf("unexpected pending moves: %+v", pending)
	}
	if pending[0].TargetFolder != "Trash" {
		t.Fatalf("target folder = %q, want 'Trash'", pending[0].TargetFolder)
	}
}

func TestHandleConversationMessageDeleteJSONQueuesMove(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := db.CreateUser(ctx, "testuser2", "Test User 2", "password123")
	if err != nil {
		t.Fatal(err)
	}
	account, err := db.UpsertAccount(ctx, mailstore.Account{
		OwnerID:           user.ID,
		Email:             "user2@example.com",
		Username:          "user2@example.com",
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

	for _, folder := range []string{"INBOX", "Sent", "Trash"} {
		attrs := []string{}
		if folder == "Trash" {
			attrs = []string{`\Trash`}
		}
		if err := db.UpsertFolder(ctx, mailstore.Folder{
			AccountID:  account.ID,
			Name:       folder,
			Attributes: attrs,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{
			ID:        "200",
			Folder:    "INBOX",
			From:      "bob@example.com",
			To:        "user2@example.com",
			Subject:   "Message delete test",
			Body:      "Single message",
			MessageID: "<m2@example.com>",
			Date:      time.Now(),
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
	app.Delete("/api/conversations/:id/messages/:uid", emailHandler.HandleConversationMessageDeleteJSON)

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
	_ = json.NewDecoder(listResp.Body).Decode(&listBody)
	convID := listBody.Conversations[0].ID

	// Delete single message
	delReq := httptest.NewRequest(http.MethodDelete, "/api/conversations/"+convID+"/messages/200", strings.NewReader(`{"folder":"INBOX"}`))
	delReq.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	delResp, err := app.Test(delReq)
	if err != nil {
		t.Fatal(err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", delResp.StatusCode)
	}

	// Verify local delete and pending move
	if _, err := db.GetMessage(ctx, account.ID, "INBOX", "200"); err == nil {
		t.Fatal("expected INBOX/200 to be deleted locally")
	}
	pending, err := db.ListPendingMessageMoves(ctx, account.ID, time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].UID != "200" {
		t.Fatalf("unexpected pending: %+v", pending)
	}
}
