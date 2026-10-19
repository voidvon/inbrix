package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdhtml "html"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"inbrix/config"
	"inbrix/mailstore"
	"inbrix/models"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

type fakeOutboxSender struct {
	calls      int
	result     error
	raw        []byte
	recipients []string
}

func (s *fakeOutboxSender) SendRawMessage(to []string, raw []byte) error {
	s.calls++
	s.raw = append([]byte(nil), raw...)
	s.recipients = append([]string(nil), to...)
	return s.result
}

func outboxTestApp(t *testing.T) (*EmailHandler, *fiber.App, mailstore.Account, mailstore.Account) {
	t.Helper()
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	user, err := db.CreateUser(ctx, "owner", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := db.CreateUser(ctx, "outsider", "Outsider", "hash")
	if err != nil {
		t.Fatal(err)
	}
	create := func(owner, email string) mailstore.Account {
		account, err := db.UpsertAccount(ctx, mailstore.Account{OwnerID: owner, Email: email, Username: email, EncryptedPassword: "enc", IMAPServer: "imap.example.com", IMAPPort: 993, SMTPServer: "smtp.example.com", SMTPPort: 465, IsDefault: true})
		if err != nil {
			t.Fatal(err)
		}
		return account
	}
	account := create(user.ID, "me@example.com")
	other := create(outsider.ID, "other@example.com")
	for _, name := range []string{"INBOX", "Sent"} {
		if err := db.UpsertFolder(ctx, mailstore.Folder{AccountID: account.ID, Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	incoming := models.Email{ID: "1", From: "peer@example.com", To: account.Email, Subject: "hello", Body: "incoming", Date: time.Now().Add(-time.Minute), MessageID: "<incoming@example.com>"}
	if err := db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{incoming}); err != nil {
		t.Fatal(err)
	}
	sessions := session.New()
	cfg := &config.Config{}
	auth := NewAuthHandler(sessions, cfg)
	auth.SetMailMirror(db, nil)
	h := NewEmailHandler(sessions, cfg, auth)
	h.SetMailMirror(db)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		current := account
		if c.Get("X-Test-Outsider") != "" {
			current = other
		}
		c.Locals("username", "owner")
		// Mirror what the session middleware does in production, so handlers
		// that resolve the owner from Locals are exercised here too.
		c.Locals("user_id", current.OwnerID)
		sess, err := sessions.Get(c)
		if err != nil {
			return err
		}
		sess.Set("user_id", current.OwnerID)
		sess.Set("account_id", current.ID)
		sess.Set("email", current.Email)
		sess.Set("token", "test-token")
		sess.Set("username", "owner")
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	app.Post("/api/compose", h.HandleComposeEmail)
	app.Post("/api/outbox/import", h.HandleComposeEmail)
	app.Post("/api/outbox/:id/retry", h.HandleRetryOutgoing)
	app.Delete("/api/outbox/:id", h.HandleDeleteOutgoing)
	app.Get("/api/outbox/:id/attachments/:part", h.HandleOutgoingAttachment)
	app.Get("/api/conversations", h.HandleConversationListJSON)
	app.Get("/api/conversations/:id", h.HandleConversationViewJSON)
	return h, app, account, other
}

func submitOutbox(t *testing.T, app *fiber.App, path string, fields map[string]string, withFiles bool) string {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	values := map[string]string{"to": "peer@example.com", "subject": "hello", "body": "reply", "html_body": `<p>reply<img src="cid:image@inbrix"></p>`, "bcc": "hidden@example.com", "in_reply_to": "<incoming@example.com>"}
	if withFiles {
		values["inline_attachments"] = `[{"field":"inline_image_0","contentId":"image@inbrix"}]`
	}
	for key, value := range fields {
		values[key] = value
	}
	for key, value := range values {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if withFiles {
		part, err := writer.CreateFormFile("attachments", "data.bin")
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte("attachment\x00\xff"))
		part, err = writer.CreateFormFile("inline_image_0", "image.png")
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte("\x89PNG\r\n\x1a\nimage"))
	}
	writer.Close()
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 202 {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("compose %d: %s", response.StatusCode, data)
	}
	var payload struct {
		OutboxID string `json:"outboxId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.OutboxID
}

func TestOutboxServerAcknowledgesThenFailsAndRetriesSameMIME(t *testing.T) {
	h, app, account, _ := outboxTestApp(t)
	ctx := context.Background()
	sender := &fakeOutboxSender{result: errors.New("TLS handshake failed: EOF")}
	h.outboxSMTP = func(mailstore.Account) (outboxSender, error) { return sender, nil }
	h.outboxSaveSent = func(mailstore.Account, mailstore.OutgoingMessage) error { return errors.New("IMAP unavailable") }
	id := submitOutbox(t, app, "/api/compose", map[string]string{"request_id": "same-upload"}, true)
	if sender.calls != 0 {
		t.Fatal("SMTP ran before durable acknowledgement")
	}
	record, err := h.mailDB.GetOutgoing(ctx, account.ID, id)
	if err != nil || record.Status != "queued" || len(record.RawMIME) == 0 || len(record.Recipients) != 2 {
		t.Fatalf("not persisted: %v %+v", err, record)
	}
	if duplicate := submitOutbox(t, app, "/api/compose", map[string]string{"request_id": "same-upload"}, true); duplicate != id {
		t.Fatal("repeat upload created duplicate")
	}
	raw := append([]byte(nil), record.RawMIME...)
	claimed, err := h.mailDB.ClaimOutgoing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.deliverOutgoing(claimed)
	record, _ = h.mailDB.GetOutgoing(ctx, account.ID, id)
	if record.Status != "failed" || record.LastError == "" || !bytes.Equal(record.RawMIME, raw) {
		t.Fatalf("failure not retained: %+v", record)
	}
	assertOutboxConversation(t, app, "unanswered", "failed", 2)
	files, err := outgoingAttachments(raw)
	if err != nil || len(files) != 2 {
		t.Fatalf("MIME: %v %+v", err, files)
	}
	for i, file := range files {
		response, err := app.Test(httptest.NewRequest(http.MethodGet, outgoingAttachmentURL(id+"."+string(rune('0'+i))), nil))
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || !bytes.Equal(content, file.Content) {
			t.Fatalf("attachment changed: %d %q", response.StatusCode, content)
		}
	}
	sender.result = nil
	response, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/outbox/"+id+"/retry", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatalf("retry status %d", response.StatusCode)
	}
	claimed, err = h.mailDB.ClaimOutgoing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.deliverOutgoing(claimed)
	record, _ = h.mailDB.GetOutgoing(ctx, account.ID, id)
	if record.Status != "sent" || sender.calls != 2 || !bytes.Equal(sender.raw, raw) || sender.recipients[1] != "hidden@example.com" {
		t.Fatalf("retry changed payload or IMAP failure made it retryable: %+v", record)
	}
	assertOutboxConversation(t, app, "answered", "sent", 2)
}

func assertOutboxConversation(t *testing.T, app *fiber.App, status, sendStatus string, count int) {
	t.Helper()
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var list ConversationListJSON
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Conversations) != 1 || list.Conversations[0].Status != status || list.Conversations[0].Count != count {
		t.Fatalf("list: %+v", list.Conversations)
	}
	response, err = app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations/"+list.Conversations[0].ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var detail struct {
		Conversation ConversationDetailJSON `json:"conversation"`
	}
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range detail.Conversation.Messages {
		if message.Folder == "Outbox" {
			found = true
			if message.SendStatus != sendStatus || message.SendRetryable == nil || !*message.SendRetryable {
				t.Fatalf("bubble state: %+v", message)
			}
			if message.HTML != "" && bytes.Contains([]byte(message.HTML), []byte("cid:")) {
				t.Fatalf("inline CID not rewritten: %s", message.HTML)
			}
			for _, match := range regexp.MustCompile(`src="([^"]+)"`).FindAllStringSubmatch(message.HTML, -1) {
				inline, err := app.Test(httptest.NewRequest(http.MethodGet, stdhtml.UnescapeString(match[1]), nil))
				if err != nil {
					t.Fatal(err)
				}
				data, _ := io.ReadAll(inline.Body)
				inline.Body.Close()
				if inline.StatusCode != 200 || !bytes.Equal(data, []byte("\x89PNG\r\n\x1a\nimage")) {
					t.Fatalf("rendered inline attachment broken: %s %d %q", match[1], inline.StatusCode, data)
				}
			}
		}
	}
	if !found {
		t.Fatal("outbox bubble missing")
	}
}

func TestOutboxImportNeverSendsAndOwnerIsolation(t *testing.T) {
	h, app, account, _ := outboxTestApp(t)
	ctx := context.Background()
	id := submitOutbox(t, app, "/api/outbox/import", map[string]string{"request_id": "legacy-1", "original_status": "failed", "missing_attachments": "true", "attachment_metadata": `[{"filename":"lost.bin"}]`}, false)
	record, err := h.mailDB.GetOutgoing(ctx, account.ID, id)
	if err != nil || record.Status != "failed" || record.HasPayload {
		t.Fatalf("legacy import: %v %+v", err, record)
	}
	if _, err := h.mailDB.ClaimOutgoing(ctx); !errors.Is(err, mailstore.ErrNotFound) {
		t.Fatalf("import queued a real email: %v", err)
	}
	for _, operation := range []struct{ method, suffix string }{{"POST", "/retry"}, {"DELETE", ""}, {"GET", "/attachments/0"}} {
		req := httptest.NewRequest(operation.method, "/api/outbox/"+id+operation.suffix, nil)
		req.Header.Set("X-Test-Outsider", "1")
		response, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("owner leak %s: %d", operation.method, response.StatusCode)
		}
	}
	response, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/outbox/"+id+"/retry", nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 409 {
		t.Fatal("retry discarded missing attachments")
	}
}

func TestOutgoingAttachmentInlinePreviewDisposition(t *testing.T) {
	_, app, _, _ := outboxTestApp(t)
	id := submitOutbox(t, app, "/api/compose", nil, true)

	// The regular attachment renders inline only when preview is requested; an
	// embedded (CID) part is always inline.
	seen := map[string]bool{}
	for index := 0; index < 6; index++ {
		for _, request := range []struct{ query, want string }{{"", "attachment"}, {"?inline=true", "inline"}} {
			response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/outbox/"+id+"/attachments/"+strconv.Itoa(index)+request.query, nil))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode == http.StatusNotFound {
				continue
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("attachment %d%s: %d", index, request.query, response.StatusCode)
			}
			disposition, _, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
			if err != nil {
				t.Fatalf("attachment %d%s: %v", index, request.query, err)
			}
			if disposition == "inline" {
				seen["inline"] = true
			}
			if request.query == "?inline=true" && disposition != "inline" {
				t.Fatalf("preview request served as %q for part %d", disposition, index)
			}
		}
	}
	if !seen["inline"] {
		t.Fatal("no part was served inline")
	}
}

func TestOutboxTencentMirrorReplacesBubbleAndPreservesThread(t *testing.T) {
	h, app, account, _ := outboxTestApp(t)
	ctx := context.Background()
	account.SMTPServer = "smtp.exmail.qq.com"
	if _, err := h.mailDB.UpsertAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	// Reply-all changes participants; the original conversation must stay intact
	// after the provider rewrites Message-ID and the outbox bubble disappears.
	thread := conversationID(account.ID, []string{"peer@example.com"})
	id := submitOutbox(t, app, "/api/compose", map[string]string{"to": "peer@example.com, cc@example.com", "conversation_id": thread, "html_body": "<p>reply</p>"}, false)
	claimed, err := h.mailDB.ClaimOutgoing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.mailDB.FinishOutgoing(ctx, account.ID, id, "sent", ""); err != nil {
		t.Fatal(err)
	}
	accepted, err := h.mailDB.GetOutgoing(ctx, account.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	mirrored := claimed.Message
	mirrored.ID = "376"
	mirrored.Folder = "Sent"
	mirrored.MessageID = "<07892A00BBB2336A+" + claimed.Message.MessageID[1:]
	mirrored.Date = time.Now().Add(-time.Hour)
	mirrored.SendStatus = ""
	mirrored.ConversationID = ""
	if err := h.mailDB.UpsertMessages(ctx, account.ID, "Sent", []models.Email{mirrored}); err != nil {
		t.Fatal(err)
	}
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/conversations", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var list ConversationListJSON
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Conversations) != 1 || list.Conversations[0].ID != thread || list.Conversations[0].Count != 2 || list.Conversations[0].Status != "answered" {
		t.Fatalf("mirror split/duplicated the conversation or reset status: %+v", list.Conversations)
	}
	date, err := time.Parse(time.RFC3339Nano, list.Conversations[0].Date)
	if err != nil || date.Sub(accepted.Message.Date).Abs() > time.Millisecond {
		t.Fatalf("acceptance date lost: %s, want %s", list.Conversations[0].Date, accepted.Message.Date)
	}
}

// A reply that reaches the outbox spends its draft. The browser discards its
// own copy, but the server must not depend on that: a tab closed mid-send would
// otherwise leave a stale draft on the conversation forever.
func TestOutboxConsumesConversationDraft(t *testing.T) {
	h, app, account, _ := outboxTestApp(t)
	ctx := context.Background()
	thread := conversationID(account.ID, []string{"peer@example.com"})

	save := func(conversationID, subject string) {
		t.Helper()
		if _, err := h.mailDB.SaveDraft(ctx, account.OwnerID, mailstore.DraftInput{
			AccountEmail:   account.Email,
			ConversationID: conversationID,
			Mode:           mailstore.DraftModeReply,
			To:             "peer@example.com",
			Subject:        subject,
			BodyHTML:       "<p>half written</p>",
		}); err != nil {
			t.Fatal(err)
		}
	}
	save(thread, "Re: hello")
	save("conv-unrelated", "Re: something else")

	submitOutbox(t, app, "/api/compose", map[string]string{"to": "peer@example.com", "conversation_id": thread, "html_body": "<p>reply</p>"}, false)

	sent, err := h.mailDB.ListDrafts(ctx, account.OwnerID, thread)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("draft survived the send: %+v", sent)
	}
	other, err := h.mailDB.ListDrafts(ctx, account.OwnerID, "conv-unrelated")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 {
		t.Fatalf("an unrelated draft was discarded: %+v", other)
	}
}
