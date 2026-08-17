package web

import (
	stdhtml "html"
	"strings"
	"time"

	"lilmail/handlers/api"
	"lilmail/mailstore"

	"github.com/gofiber/fiber/v2"
)

type AttachmentJSON struct {
	ID           string    `json:"id"`
	PartID       string    `json:"partId"`
	Filename     string    `json:"filename"`
	ContentType  string    `json:"contentType"`
	Size         int       `json:"size"`
	Folder       string    `json:"folder"`
	MessageID    string    `json:"messageId"`
	MessageDate  time.Time `json:"messageDate"`
	MessageFrom  string    `json:"messageFrom"`
	FromName     string    `json:"fromName,omitempty"`
	MessageTitle string    `json:"messageSubject"`
	AccountEmail string    `json:"accountEmail"`
}

type AttachmentsJSON struct {
	Attachments []AttachmentJSON `json:"attachments"`
	Total       int              `json:"total"`
	Limit       int              `json:"limit"`
	Offset      int              `json:"offset"`
	NextOffset  *int             `json:"nextOffset"`
}

func attachmentJSON(record mailstore.AttachmentRecord, accountEmail string) AttachmentJSON {
	id := strings.TrimSpace(record.AttachmentID)
	if id == "" && strings.TrimSpace(record.PartID) != "" {
		id = api.EncodeAttachmentID(record.FolderName, record.UID, record.PartID)
	}
	return AttachmentJSON{
		ID: id, PartID: record.PartID, Filename: stdhtml.UnescapeString(api.DecodeMIMEHeader(record.Filename)),
		ContentType: record.ContentType, Size: record.Size, Folder: record.FolderName,
		MessageID: record.UID, MessageDate: record.MessageDate, MessageFrom: record.From,
		FromName: api.DecodeMIMEHeader(record.FromName), MessageTitle: stdhtml.UnescapeString(record.Subject),
		AccountEmail: accountEmail,
	}
}

func (h *EmailHandler) HandleLocalAttachmentsJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	account, err := h.localFolderAccount(c)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	limit := localFolderPageInt(c, "limit", 100)
	if limit == 0 || limit > maxLocalFolderPageSize {
		limit = 100
	}
	offset := localFolderPageInt(c, "offset", 0)
	records, total, err := h.mailDB.ListAttachments(c.UserContext(), account.ID, mailstore.AttachmentListOptions{
		Query: c.Query("q"), Kind: c.Query("type"), Limit: limit, Offset: offset,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read attachment index"})
	}
	attachments := make([]AttachmentJSON, 0, len(records))
	for _, record := range records {
		attachments = append(attachments, attachmentJSON(record, account.Email))
	}
	var nextOffset *int
	if offset+len(attachments) < total {
		next := offset + len(attachments)
		nextOffset = &next
	}
	return c.JSON(AttachmentsJSON{Attachments: attachments, Total: total, Limit: limit, Offset: offset, NextOffset: nextOffset})
}
