package web_test

import (
	"context"
	"encoding/json"
	"io"
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

type conversationListPage struct {
	Conversations []struct {
		ID          string `json:"id"`
		Preview     string `json:"preview"`
		UnreadCount int    `json:"unreadCount"`
	} `json:"conversations"`
	NextCursor string `json:"nextCursor"`
	HasMore    bool   `json:"hasMore"`
	Total      int    `json:"total"`
	Unread     int    `json:"unreadCount"`
}

func TestConversationListPaginationAndDetailHydration(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := db.CreateUser(ctx, "pageuser", "Page User", "password123")
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
	if err := db.UpsertFolder(ctx, mailstore.Folder{AccountID: account.ID, Name: "INBOX"}); err != nil {
		t.Fatal(err)
	}

	when := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{
		{
			ID: "101", Folder: "INBOX", From: "alice@example.com", To: "user@example.com",
			Subject: "Oldest", Preview: "alice preview", Body: "alice body", HTML: "<p>alice html</p>",
			Date: when, Flags: []string{`\Seen`}, MessageID: "<alice@example.com>",
		},
		{
			ID: "102", Folder: "INBOX", From: "bob@example.com", To: "user@example.com",
			Subject: "Middle", Body: "bob fallback body", HTML: "<p>bob html</p>",
			Date: when.Add(time.Hour), MessageID: "<bob@example.com>",
		},
		{
			ID: "103", Folder: "INBOX", From: "carol@example.com", To: "user@example.com",
			Subject: "Newest", Preview: "carol preview", Body: "carol body", HTML: "<p>carol html</p>",
			Date: when.Add(2 * time.Hour), MessageID: "<carol@example.com>",
		},
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
		c.Locals("email", account.Email)
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
	app.Get("/api/conversations/:id", emailHandler.HandleConversationViewJSON)

	get := func(path string) *http.Response {
		t.Helper()
		response, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	decodePage := func(response *http.Response) conversationListPage {
		t.Helper()
		defer response.Body.Close()
		var page conversationListPage
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page
	}

	firstResponse := get("/api/conversations?limit=2")
	if firstResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(firstResponse.Body)
		t.Fatalf("list status = %d body=%s", firstResponse.StatusCode, body)
	}
	first := decodePage(firstResponse)
	if len(first.Conversations) != 2 || !first.HasMore || first.Total != 3 || first.Unread != 2 {
		t.Fatalf("first page = %+v", first)
	}
	if first.Conversations[0].ID == first.Conversations[1].ID {
		t.Fatalf("first page duplicated a conversation: %+v", first.Conversations)
	}
	if !strings.Contains(first.Conversations[1].Preview, "bob fallback body") {
		t.Fatalf("header projection lost the body preview fallback: %+v", first.Conversations[1])
	}
	if first.NextCursor == "" {
		t.Fatal("first page did not return a next cursor")
	}

	second := decodePage(get("/api/conversations?limit=2&cursor=" + first.NextCursor))
	if len(second.Conversations) != 1 || second.HasMore {
		t.Fatalf("second page = %+v", second)
	}
	if second.Total != 3 || second.Unread != 2 {
		t.Fatalf("second page totals = total %d unread %d", second.Total, second.Unread)
	}
	seen := map[string]bool{}
	for _, conversation := range append(first.Conversations, second.Conversations...) {
		if seen[conversation.ID] {
			t.Fatalf("conversation %s appeared on two pages", conversation.ID)
		}
		seen[conversation.ID] = true
	}

	all := decodePage(get("/api/conversations"))
	if len(all.Conversations) != 3 || all.HasMore || all.Total != 3 {
		t.Fatalf("legacy unpaginated list = %+v", all)
	}

	aliceID := ""
	for _, conversation := range all.Conversations {
		if strings.Contains(conversation.Preview, "alice preview") {
			aliceID = conversation.ID
		}
	}
	if aliceID == "" {
		t.Fatalf("alice conversation not found in %+v", all.Conversations)
	}
	if err := db.SetConversationStatus(ctx, account.ID, aliceID, "answered", "INBOX/101"); err != nil {
		t.Fatal(err)
	}
	open := decodePage(get("/api/conversations?filter=open&limit=10"))
	if len(open.Conversations) != 2 || open.Total != 2 {
		t.Fatalf("open filter = %+v, want 2 conversations", open)
	}
	for _, conversation := range open.Conversations {
		if conversation.ID == aliceID {
			t.Fatalf("answered conversation leaked into the open filter: %+v", open)
		}
	}
	allAfterStatus := decodePage(get("/api/conversations?filter=all&limit=10"))
	if len(allAfterStatus.Conversations) != 3 || allAfterStatus.Total != 3 {
		t.Fatalf("all filter = %+v, want 3 conversations", allAfterStatus)
	}

	bad := get("/api/conversations?limit=2&cursor=not-a-cursor")
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d, want 400", bad.StatusCode)
	}

	detailResponse := get("/api/conversations/" + first.Conversations[0].ID)
	if detailResponse.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d", detailResponse.StatusCode)
	}
	defer detailResponse.Body.Close()
	var detail struct {
		Conversation struct {
			Messages []struct {
				Body string `json:"body"`
				HTML string `json:"html"`
			} `json:"messages"`
		} `json:"conversation"`
	}
	if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Conversation.Messages) != 1 {
		t.Fatalf("detail messages = %+v", detail.Conversation.Messages)
	}
	if detail.Conversation.Messages[0].Body == "" || !strings.Contains(detail.Conversation.Messages[0].HTML, "carol html") {
		t.Fatalf("detail did not hydrate the selected thread: %+v", detail.Conversation.Messages[0])
	}
}
