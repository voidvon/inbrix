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

	t.Run("message history", func(t *testing.T) {
		// Equal timestamps and identical UIDs in INBOX/Sent must still have a
		// stable order across page boundaries.
		var emails []models.Email
		for _, id := range []string{"201", "202", "203", "204", "205", "206", "207"} {
			emails = append(emails, models.Email{
				ID: id, Folder: "INBOX", From: "alice@example.com", To: account.Email,
				Subject: "History", Body: "body " + id, HTML: "<p>html " + id + "</p>", Date: when.Add(10 * time.Hour),
			})
		}
		if err := db.UpsertMessages(ctx, account.ID, "INBOX", emails); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertFolder(ctx, mailstore.Folder{AccountID: account.ID, Name: "Sent"}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertMessages(ctx, account.ID, "Sent", []models.Email{{
			ID: "205", Folder: "Sent", From: account.Email, To: "alice@example.com",
			Subject: "Reply", Body: "sent body", Date: when.Add(10 * time.Hour),
		}}); err != nil {
			t.Fatal(err)
		}
		read := func(path string) web.ConversationDetailJSON {
			t.Helper()
			response := get(path)
			defer response.Body.Close()
			var page struct {
				Conversation web.ConversationDetailJSON `json:"conversation"`
			}
			if response.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("history status %d: %s", response.StatusCode, body)
			}
			if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
				t.Fatal(err)
			}
			return page.Conversation
		}
		path := "/api/conversations/" + aliceID
		first := read(path)
		if len(first.Messages) != 3 || first.Count != 9 || !first.HasMore || first.OldestCursor == "" {
			t.Fatalf("first history page = %+v", first)
		}
		if first.Messages[0].ID != "205" || first.Messages[0].Folder != "Sent" || !first.Messages[0].Outgoing {
			t.Fatalf("first message = %+v", first.Messages[0])
		}
		second := read(path + "?cursor=" + first.OldestCursor)
		third := read(path + "?cursor=" + second.OldestCursor)
		if len(second.Messages) != 3 || !second.HasMore || len(third.Messages) != 3 || third.HasMore {
			t.Fatalf("history pages: second=%+v third=%+v", second, third)
		}
		seen := map[string]bool{}
		for _, message := range append(append(third.Messages, second.Messages...), first.Messages...) {
			key := message.Folder + "/" + message.ID
			if seen[key] || message.Body == "" {
				t.Fatalf("duplicate or unhydrated message: %+v", message)
			}
			seen[key] = true
		}
		if len(seen) != 9 {
			t.Fatalf("loaded %d messages, want 9", len(seen))
		}
		// A search hit opens only its local window, even when far from latest.
		middle := read(path + "?around=204&folder=INBOX")
		if len(middle.Messages) != 3 || middle.Messages[1].ID != "204" || !middle.HasMore || !middle.HasNewer || middle.NewestCursor == "" {
			t.Fatalf("search window = %+v", middle)
		}
		up := read(path + "?cursor=" + middle.OldestCursor)
		down := read(path + "?after=" + middle.NewestCursor)
		if len(up.Messages) != 3 || up.HasMore || len(down.Messages) != 3 || down.HasNewer || down.Messages[0].Folder != "Sent" || down.Messages[0].ID != "205" {
			t.Fatalf("bidirectional pages: up=%+v down=%+v", up, down)
		}
		bounded := read(path + "?through=" + middle.OldestCursor + "&until=" + middle.NewestCursor)
		if len(bounded.Messages) != 3 || bounded.Messages[1].ID != "204" || !bounded.HasNewer {
			t.Fatalf("bounded refresh = %+v", bounded)
		}
		sentTarget := read(path + "?around=205&folder=Sent&limit=1")
		if len(sentTarget.Messages) != 1 || sentTarget.Messages[0].Folder != "Sent" || !sentTarget.HasNewer || !sentTarget.HasMore {
			t.Fatalf("mailbox-specific search = %+v", sentTarget)
		}
		for _, query := range []string{"?around=missing&folder=INBOX", "?around=204&folder=Sent"} {
			response := get(path + query)
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("%s status = %d, want 404", query, response.StatusCode)
			}
		}

		// A new head and a deleted boundary cannot shift or erase loaded history.
		if err := db.DeleteMessage(ctx, account.ID, "INBOX", "203"); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{
			ID: "208", Folder: "INBOX", From: "alice@example.com", To: account.Email,
			Body: "new head", Date: when.Add(11 * time.Hour),
		}}); err != nil {
			t.Fatal(err)
		}
		refreshed := read(path + "?through=" + second.OldestCursor)
		if len(refreshed.Messages) != 6 || refreshed.Messages[0].ID != "204" || refreshed.Messages[5].ID != "208" || !refreshed.HasMore {
			t.Fatalf("refreshed window = %+v", refreshed)
		}
		older := read(path + "?cursor=" + second.OldestCursor)
		if len(older.Messages) != 3 || older.Messages[2].ID != "202" {
			t.Fatalf("deleted boundary continuation = %+v", older)
		}
		deleted := read(path + "?after=" + middle.OldestCursor)
		if len(deleted.Messages) != 3 || deleted.Messages[0].ID != "204" || deleted.Messages[2].Folder != "Sent" {
			t.Fatalf("deleted cursor forward continuation = %+v", deleted)
		}
		bounded = read(path + "?through=" + middle.OldestCursor + "&until=" + middle.NewestCursor)
		if len(bounded.Messages) != 2 || bounded.Messages[0].ID != "204" || !bounded.HasNewer {
			t.Fatalf("bounded refresh after deletion/new mail = %+v", bounded)
		}

		complete := read(path + "?through=" + third.OldestCursor)
		if len(complete.Messages) != 9 || complete.HasMore {
			t.Fatalf("complete refresh = %+v", complete)
		}
		for _, query := range []string{"?cursor=broken", "?through=broken", "?after=broken", "?until=broken", "?through=" + middle.OldestCursor + "&until=broken", "?after=" + middle.NewestCursor + "&around=204", "?through=" + first.NewestCursor + "&until=" + third.OldestCursor, "?cursor=" + first.OldestCursor + "&through=" + second.OldestCursor} {
			response := get(path + query)
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s status = %d, want 400", query, response.StatusCode)
			}
		}
		for _, conversation := range all.Conversations {
			if conversation.ID == aliceID {
				continue
			}
			for _, query := range []string{"?cursor=" + first.OldestCursor, "?after=" + first.NewestCursor, "?through=" + third.OldestCursor + "&until=" + first.NewestCursor} {
				response := get("/api/conversations/" + conversation.ID + query)
				response.Body.Close()
				if response.StatusCode != http.StatusBadRequest {
					t.Fatalf("foreign cursor status = %d, want 400", response.StatusCode)
				}
			}
			break
		}
		one := read(path + "?around=204&folder=INBOX&limit=1")
		if err := db.DeleteMessage(ctx, account.ID, "INBOX", "204"); err != nil {
			t.Fatal(err)
		}
		empty := read(path + "?through=" + one.OldestCursor + "&until=" + one.NewestCursor)
		if len(empty.Messages) != 0 || !empty.HasMore || !empty.HasNewer || empty.OldestCursor != one.OldestCursor || empty.NewestCursor != one.NewestCursor {
			t.Fatalf("deleted window lost its boundaries: %+v", empty)
		}
		if next := read(path + "?after=" + empty.NewestCursor); len(next.Messages) != 3 || next.Messages[0].ID != "205" || next.Messages[0].Folder != "INBOX" {
			t.Fatalf("navigation after window deletion = %+v", next)
		}

	})
}
