package web

import (
	"errors"
	"io"

	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

func documentAttachmentError(err error) error {
	if errors.Is(err, mailstore.ErrDocumentAttachmentInvalid) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}
	return fiber.NewError(fiber.StatusInternalServerError, "Failed to store document image")
}

// HandleUploadDocumentAttachment accepts images independently of a saved draft.
// Their immutable URL can be used in the editor before the first document save.
func (h *DocumentsHandler) HandleUploadDocumentAttachment(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	file, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(400, "Image file is required")
	}
	if file.Size > mailstore.MaxDocumentAttachmentBytes {
		return fiber.NewError(413, "Image exceeds 10 MiB")
	}
	reader, err := file.Open()
	if err != nil {
		return fiber.NewError(400, "Cannot read image")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, mailstore.MaxDocumentAttachmentBytes+1))
	if err != nil {
		return fiber.NewError(400, "Cannot read image")
	}
	attachment, err := h.mailDB.SaveDocumentAttachment(c.UserContext(), owner, data)
	if err != nil {
		return documentAttachmentError(err)
	}
	return c.JSON(fiber.Map{"attachment": attachment})
}

func (h *DocumentsHandler) HandleGetDocumentAttachment(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	// Authenticate and check ownership even for conditional/cache requests.
	data, mime, err := h.mailDB.GetDocumentAttachment(c.UserContext(), owner, c.Params("id"))
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	c.Set("Content-Type", mime)
	c.Set("X-Content-Type-Options", "nosniff")
	// Revalidate on reuse so switching accounts cannot reuse a previous owner's
	// image. Successful revalidation avoids transferring the image bytes again.
	c.Set("Cache-Control", "private, no-cache")
	c.Set("ETag", `"`+c.Params("id")+`"`)
	// SVGs may be opened as documents as well as drawn into an image/canvas.
	c.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	if c.Fresh() {
		return c.SendStatus(fiber.StatusNotModified)
	}
	return c.Send(data)
}
