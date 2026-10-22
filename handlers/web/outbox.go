package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"inbrix/events"
	"inbrix/handlers/api"
	"inbrix/mailstore"
	"inbrix/models"

	"github.com/gofiber/fiber/v2"
)

type outboxSender interface{ SendRawMessage([]string, []byte) error }

func (h *EmailHandler) queueOutgoingCompose(c *fiber.Ctx, from, thread, draft, to, cc, subject, body, html string, recipients []string, raw []byte) error {
	account, ok := h.mirrorAccountForEmail(c, from)
	if !ok {
		return c.Status(403).JSON(fiber.Map{"error": "Sending account is not owned by this user"})
	}
	header, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not preserve outgoing message headers"})
	}
	files, err := outgoingAttachments(raw)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not preserve outgoing attachments"})
	}
	message := models.Email{From: from, To: to, Cc: cc, Subject: subject, Body: body, HTML: html, Preview: body, Date: time.Now().UTC(), Flags: []string{`\Seen`}, MessageID: header.Header.Get("Message-ID"), InReplyTo: c.FormValue("in_reply_to"), References: strings.Fields(c.FormValue("references")), Attachments: files, BodyCached: true, AttachmentMetadataCached: true}
	if thread == "" {
		thread = conversationID(account.ID, messageParticipants(message, account.Email))
	}
	// The supplied conversation must belong to this mailbox and these peers.
	if thread != conversationID(account.ID, messageParticipants(message, account.Email)) {
		// Replies may change To (e.g. reply-all), so accept an existing owned
		// conversation as well as the computed identity for a new message.
		conversations, _, err := h.localConversations(c, []mailstore.Account{account}, false)
		var selected *Conversation
		if err == nil {
			selected = findConversation(conversations, thread)
		}
		if selected == nil || !sameMailboxAddress(selected.AccountEmail, account.Email) {
			return c.Status(403).JSON(fiber.Map{"error": "Conversation is not owned by this account"})
		}
	}
	key := c.FormValue("request_id")
	if len(key) > 200 {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid outgoing request ID"})
	}
	record := mailstore.OutgoingMessage{ReplacesID: c.FormValue("replaces_outbox_id"), AccountID: account.ID, ClientKey: key, ConversationID: thread, Message: message, Recipients: recipients, RawMIME: raw, DraftUID: draft}
	if c.Path() == "/api/outbox/import" {
		// One-time migration of the old browser-only bubbles. Importing never
		// sends a message; the user still clicks the red retry button.
		record.Status = "failed"
		record.LastError = "Previous send was interrupted or failed"
		if c.FormValue("original_status") != "failed" {
			record.Status = "unknown"
		}
		if date, err := time.Parse(time.RFC3339, c.FormValue("original_date")); err == nil {
			record.Message.Date = date
		}
		if c.FormValue("missing_attachments") == "true" {
			record.RawMIME = nil
			record.LastError = "Original attachments must be reattached before sending"
			var metadata []models.Attachment
			if err := json.Unmarshal([]byte(c.FormValue("attachment_metadata")), &metadata); err != nil {
				return c.Status(400).JSON(fiber.Map{"error": "Invalid old attachment metadata"})
			}
			for i := range metadata {
				metadata[i].Content = nil
			}
			record.Message.Attachments = metadata
		}
	}
	saved, err := h.mailDB.CreateOutgoing(c.UserContext(), record)
	if err != nil {
		if errors.Is(err, mailstore.ErrOutboxBusy) {
			return c.Status(409).JSON(fiber.Map{"error": "The original email is already sending or sent"})
		}
		log.Printf("outbox: persist compose: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Could not save email to the server outbox"})
	}
	// The reply is queued, so the draft it came from is spent. Best-effort: the
	// message is already persisted, so a cleanup failure must not fail the send.
	// The browser discards its draft too; this covers the case where the tab
	// closed before it could.
	if thread != "" && h.mailDB != nil {
		if ownerID := requestOwnerID(c, h.store); ownerID != "" {
			if _, err := h.mailDB.DeleteDraftsForConversation(c.UserContext(), ownerID, from, thread); err != nil {
				log.Printf("outbox: discard draft for %s: %v", thread, err)
			}
		}
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"success": true, "queued": saved.Status == "queued", "outboxId": saved.ID, "conversationId": saved.ConversationID, "status": saved.Status})
}

func (h *EmailHandler) StartOutbox() error {
	if h.mailDB == nil || h.outboxCancel != nil {
		return nil
	}
	if err := h.mailDB.RecoverOutgoing(context.Background()); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.outboxCancel = cancel
	for range 2 {
		h.outboxWG.Add(1)
		go func() {
			defer h.outboxWG.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				record, err := h.mailDB.ClaimOutgoing(ctx)
				if err == nil {
					h.deliverOutgoing(record)
					continue
				}
				if !errors.Is(err, mailstore.ErrNotFound) && ctx.Err() == nil {
					log.Printf("outbox: claim: %v", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	return nil
}

func (h *EmailHandler) StopOutbox() {
	if h.outboxCancel != nil {
		h.outboxCancel()
		h.outboxWG.Wait()
		h.outboxCancel = nil
	}
}

func (h *EmailHandler) deliverOutgoing(record mailstore.OutgoingMessage) {
	ctx := context.Background()
	account, err := h.mailDB.GetAccount(ctx, record.AccountID)
	var sender outboxSender
	if err == nil {
		if h.outboxSMTP != nil {
			sender, err = h.outboxSMTP(account)
		} else {
			sender, err = h.auth.CreateSMTPClientForMirrorAccount(account)
		}
	}
	if err == nil {
		if sender == nil {
			err = errors.New("SMTP sender is unavailable")
		} else {
			err = sender.SendRawMessage(record.Recipients, record.RawMIME)
		}
	}
	status, message := "sent", ""
	if err != nil {
		status, message = "failed", err.Error()
		// A lost response after the terminating DATA dot can mean acceptance.
		if errors.Is(err, api.ErrSMTPDeliveryUnknown) {
			status = "unknown"
		}
		log.Printf("outbox: %s attempt %d: %s: %v", record.ID, record.Attempts, status, err)
	}
	if persistErr := h.mailDB.FinishOutgoing(ctx, record.AccountID, record.ID, status, message); persistErr != nil {
		log.Printf("outbox: record send result %s: %v", record.ID, persistErr)
		return
	}
	// The browser no longer polls a "sending" message. Publish the terminal
	// status so every tab refetches the conversation immediately.
	h.mailDB.PublishAccountEvent(ctx, record.AccountID, events.Event{
		Kind:           events.KindOutboxUpdated,
		ConversationID: record.ConversationID,
		MessageID:      record.ID,
		Folder:         "Outbox",
	})
	if err != nil {
		return
	}
	// SMTP acceptance is durable before best-effort IMAP work. A Sent-copy
	// failure cannot turn a delivered email into a retryable failure.
	if h.outboxSaveSent != nil {
		err = h.outboxSaveSent(account, record)
	} else {
		err = h.saveOutgoingSent(account, record)
	}
	if err != nil {
		log.Printf("outbox: save Sent copy %s: %v", record.ID, err)
	}
	if h.kv != nil {
		if owner, ownerErr := h.mailDB.GetUser(ctx, account.OwnerID); ownerErr == nil {
			store := api.NewRecipientsStore(h.kv, api.SanitizeUsername(owner.Login))
			entries := append(api.ParseAddressField(record.Message.To), api.ParseAddressField(record.Message.Cc)...)
			if err := store.Record(entries); err != nil {
				log.Printf("outbox: recipients %s: %v", record.ID, err)
			}
		}
	}
}

func (h *EmailHandler) saveOutgoingSent(account mailstore.Account, record mailstore.OutgoingMessage) error {
	client, err := h.auth.CreateIMAPClientForMirrorAccount(account)
	if err != nil {
		return err
	}
	defer client.Close()
	var folder string
	if saver, ok := client.(interface {
		SaveToSentWithFolder(string, string, string, []byte) (string, error)
	}); ok {
		folder, err = saver.SaveToSentWithFolder(record.Message.To, record.Message.Subject, record.Message.Body, record.RawMIME)
	} else {
		err = client.SaveToSent(record.Message.To, record.Message.Subject, record.Message.Body, record.RawMIME)
	}
	if err != nil {
		return err
	}
	if record.DraftUID != "" {
		if err := client.DeleteDraft(record.DraftUID); err != nil {
			log.Printf("outbox: delete draft: %v", err)
		}
	}
	if folder != "" && h.auth.syncer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := h.auth.syncer.SyncNewMessagesNow(ctx, account.ID, folder); err != nil {
			h.auth.syncer.Trigger(account.ID)
			return err
		}
	}
	return nil
}

func (h *EmailHandler) ownedOutgoing(c *fiber.Ctx) (mailstore.OutgoingMessage, bool) {
	if h.mailDB == nil {
		_ = c.Status(501).JSON(fiber.Map{"error": "Server outbox is unavailable"})
		return mailstore.OutgoingMessage{}, false
	}
	sess, err := requestSession(c, h.store)
	if err != nil {
		_ = c.SendStatus(401)
		return mailstore.OutgoingMessage{}, false
	}
	owner, _ := sess.Get("user_id").(string)
	accounts, err := h.mailDB.ListAccounts(c.UserContext(), owner)
	if err != nil {
		_ = c.SendStatus(500)
		return mailstore.OutgoingMessage{}, false
	}
	for _, account := range accounts {
		record, err := h.mailDB.GetOutgoing(c.UserContext(), account.ID, c.Params("id"))
		if err == nil {
			return record, true
		}
		if !errors.Is(err, mailstore.ErrNotFound) {
			_ = c.SendStatus(500)
			return record, false
		}
	}
	_ = c.Status(404).JSON(fiber.Map{"error": "Outgoing message not found"})
	return mailstore.OutgoingMessage{}, false
}

func (h *EmailHandler) HandleRetryOutgoing(c *fiber.Ctx) error {
	record, ok := h.ownedOutgoing(c)
	if !ok {
		return nil
	}
	if !record.HasPayload {
		return c.Status(409).JSON(fiber.Map{"error": "Original attachments must be reattached before sending", "needsAttachments": true})
	}
	if err := h.mailDB.RetryOutgoing(c.UserContext(), record.AccountID, record.ID); err != nil {
		if errors.Is(err, mailstore.ErrOutboxBusy) {
			return c.Status(409).JSON(fiber.Map{"error": "This email is already sending or sent"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "Could not queue email retry"})
	}
	return c.Status(202).JSON(fiber.Map{"ok": true})
}

func (h *EmailHandler) HandleDeleteOutgoing(c *fiber.Ctx) error {
	record, ok := h.ownedOutgoing(c)
	if !ok {
		return nil
	}
	if err := h.mailDB.DeleteOutgoing(c.UserContext(), record.AccountID, record.ID); err != nil {
		if errors.Is(err, mailstore.ErrOutboxBusy) {
			return c.Status(409).JSON(fiber.Map{"error": "Cannot delete email while SMTP submission is in progress"})
		}
		return c.SendStatus(500)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func outgoingAttachmentURL(id string) string {
	messageID, part, ok := strings.Cut(id, ".")
	if !ok {
		return ""
	}
	return "/api/outbox/" + url.PathEscape(messageID) + "/attachments/" + url.PathEscape(part)
}

func (h *EmailHandler) HandleOutgoingAttachment(c *fiber.Ctx) error {
	record, ok := h.ownedOutgoing(c)
	if !ok {
		return nil
	}
	index, err := strconv.Atoi(c.Params("part"))
	if err != nil || index < 0 {
		return c.SendStatus(404)
	}
	files, err := outgoingAttachments(record.RawMIME)
	if err != nil || index >= len(files) {
		return c.SendStatus(404)
	}
	file := files[index]
	c.Set("Cache-Control", "private, no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Content-Type", file.ContentType)
	// Inline rendering is required for in-browser preview of an outgoing
	// attachment; embedded (CID) parts are always inline.
	disposition := "attachment"
	if file.IsInline || c.QueryBool("inline", false) {
		disposition = "inline"
	}
	c.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": file.Filename}))
	return c.Send(file.Content)
}

// Decode only the attachment leaves of MIME produced by our builder. Keeping
// the original MIME also preserves BCC envelope recipients and inline images
// byte-for-byte across failures and restarts.
func outgoingAttachments(raw []byte) ([]models.Attachment, error) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var files []models.Attachment
	var walk func(io.Reader, string, string, string, string, int) error
	walk = func(body io.Reader, contentType, transfer, disposition, contentID string, depth int) error {
		if depth > 12 {
			return fmt.Errorf("outgoing MIME nesting is too deep")
		}
		mediaType, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			return err
		}
		if strings.HasPrefix(mediaType, "multipart/") {
			reader := multipart.NewReader(body, params["boundary"])
			for {
				part, err := reader.NextRawPart()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if err := walk(part, part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part.Header.Get("Content-Disposition"), part.Header.Get("Content-ID"), depth+1); err != nil {
					return err
				}
			}
		}
		kind, attributes, _ := mime.ParseMediaType(disposition)
		if kind != "attachment" && contentID == "" {
			return nil
		}
		if strings.EqualFold(transfer, "base64") {
			body = base64.NewDecoder(base64.StdEncoding, body)
		}
		if strings.EqualFold(transfer, "quoted-printable") {
			body = quotedprintable.NewReader(body)
		}
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		files = append(files, models.Attachment{Filename: attributes["filename"], ContentType: mediaType, ContentID: strings.Trim(contentID, "<>"), IsInline: kind == "inline" || contentID != "", Size: len(data), Content: data})
		return nil
	}
	if err := walk(message.Body, message.Header.Get("Content-Type"), message.Header.Get("Content-Transfer-Encoding"), message.Header.Get("Content-Disposition"), message.Header.Get("Content-ID"), 0); err != nil {
		return nil, err
	}
	return files, nil
}
