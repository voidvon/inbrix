package web

import (
	"strings"

	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// DocumentsHandler handles HTTP REST endpoints for quotation/contract documents, templates, and stamps.
type DocumentsHandler struct {
	store  *session.Store
	mailDB *mailstore.Store
}

// NewDocumentsHandler creates a new DocumentsHandler.
func NewDocumentsHandler(store *session.Store, mailDB *mailstore.Store) *DocumentsHandler {
	return &DocumentsHandler{
		store:  store,
		mailDB: mailDB,
	}
}

func (h *DocumentsHandler) owner(c *fiber.Ctx) string {
	if h == nil {
		return ""
	}
	if userID, ok := c.Locals("user_id").(string); ok && strings.TrimSpace(userID) != "" {
		return strings.TrimSpace(userID)
	}
	if h.store != nil {
		sess, err := h.store.Get(c)
		if err == nil {
			owner, _ := sess.Get("user_id").(string)
			return strings.TrimSpace(owner)
		}
	}
	return ""
}

func (h *DocumentsHandler) ready(c *fiber.Ctx) (string, error) {
	if h == nil || h.mailDB == nil {
		return "", fiber.NewError(fiber.StatusNotImplemented, "Local database unavailable")
	}
	owner := h.owner(c)
	if owner == "" {
		return "", fiber.ErrUnauthorized
	}
	return owner, nil
}

// HandleListDocuments returns all documents for the current user, optionally filtered by type.
func (h *DocumentsHandler) HandleListDocuments(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	docType := c.Query("type", "all")
	docs, err := h.mailDB.ListDocuments(c.UserContext(), owner, docType)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to load documents: "+err.Error())
	}
	return c.JSON(fiber.Map{"documents": docs})
}

// HandleGetDocument returns a single document by ID.
func (h *DocumentsHandler) HandleGetDocument(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Document ID is required")
	}
	doc, err := h.mailDB.GetDocument(c.UserContext(), owner, id)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "Document not found")
	}
	return c.JSON(fiber.Map{"document": doc})
}

type saveDocumentInput struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Company string `json:"company"`
	HTML    string `json:"html"`
}

// HandleSaveDocument inserts or updates a document.
func (h *DocumentsHandler) HandleSaveDocument(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input saveDocumentInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}
	if strings.TrimSpace(input.HTML) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Document content cannot be empty")
	}
	record := mailstore.DocumentRecord{
		ID:      strings.TrimSpace(input.ID),
		OwnerID: owner,
		Type:    input.Type,
		Name:    input.Name,
		Company: input.Company,
		HTML:    input.HTML,
	}
	saved, err := h.mailDB.SaveDocument(c.UserContext(), record)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to save document: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "document": saved})
}

// HandleDeleteDocument deletes a single document by ID.
func (h *DocumentsHandler) HandleDeleteDocument(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Document ID is required")
	}
	if err := h.mailDB.DeleteDocuments(c.UserContext(), owner, []string{id}); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to delete document: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

type batchDeleteInput struct {
	IDs []string `json:"ids"`
}

// HandleBatchDeleteDocuments deletes multiple documents by ID.
func (h *DocumentsHandler) HandleBatchDeleteDocuments(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input batchDeleteInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}
	if err := h.mailDB.DeleteDocuments(c.UserContext(), owner, input.IDs); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to delete documents: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "count": len(input.IDs)})
}

// HandleListTemplates returns custom templates for the user.
func (h *DocumentsHandler) HandleListTemplates(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	templates, err := h.mailDB.ListDocumentTemplates(c.UserContext(), owner)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to load templates: "+err.Error())
	}
	return c.JSON(fiber.Map{"templates": templates})
}

type saveTemplateInput struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
	HTML string `json:"html"`
}

// HandleSaveTemplate inserts or updates a custom template.
func (h *DocumentsHandler) HandleSaveTemplate(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input saveTemplateInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}
	if strings.TrimSpace(input.HTML) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Template content cannot be empty")
	}
	record := mailstore.DocumentTemplateRecord{
		ID:      strings.TrimSpace(input.ID),
		OwnerID: owner,
		Type:    input.Type,
		Name:    input.Name,
		HTML:    input.HTML,
	}
	saved, err := h.mailDB.SaveDocumentTemplate(c.UserContext(), record)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to save template: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "template": saved})
}

// HandleDeleteTemplate removes a custom template.
func (h *DocumentsHandler) HandleDeleteTemplate(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Template ID is required")
	}
	if err := h.mailDB.DeleteDocumentTemplate(c.UserContext(), owner, id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to delete template: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

// HandleListStamps returns all stamps for the user.
func (h *DocumentsHandler) HandleListStamps(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	stamps, err := h.mailDB.ListDocumentStamps(c.UserContext(), owner)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to load stamps: "+err.Error())
	}
	return c.JSON(fiber.Map{"stamps": stamps})
}

type saveStampInput struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	DataURL     string  `json:"dataUrl"`
	Value       string  `json:"value"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	InsertWidth int     `json:"insertWidth"`
	AspectRatio float64 `json:"aspectRatio"`
	CreatedAt   int64   `json:"createdAt"`
}

// HandleSaveStamp saves a stamp image.
func (h *DocumentsHandler) HandleSaveStamp(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input saveStampInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}
	dataURL := strings.TrimSpace(input.DataURL)
	if dataURL == "" {
		dataURL = strings.TrimSpace(input.Value)
	}
	if dataURL == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Stamp image data cannot be empty")
	}
	record := mailstore.DocumentStampRecord{
		ID:          strings.TrimSpace(input.ID),
		OwnerID:     owner,
		Name:        input.Name,
		DataURL:     dataURL,
		Value:       dataURL,
		Width:       input.Width,
		Height:      input.Height,
		InsertWidth: input.InsertWidth,
		AspectRatio: input.AspectRatio,
		CreatedAt:   input.CreatedAt,
	}
	saved, err := h.mailDB.SaveDocumentStamp(c.UserContext(), record)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to save stamp: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "stamp": saved})
}

// HandleDeleteStamp deletes a stamp.
func (h *DocumentsHandler) HandleDeleteStamp(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Stamp ID is required")
	}
	if err := h.mailDB.DeleteDocumentStamp(c.UserContext(), owner, id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to delete stamp: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}
