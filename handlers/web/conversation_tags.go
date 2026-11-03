package web

import (
	"inbrix/mailstore"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (h *EmailHandler) HandleConversationTagsJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "Mail mirror is unavailable")
	}
	var body struct {
		Tags *[]string `json:"tags"`
	}
	if err := c.BodyParser(&body); err != nil || body.Tags == nil {
		return fiber.NewError(fiber.StatusBadRequest, "A tags array is required")
	}
	tags, err := mailstore.NormalizeConversationTags(*body.Tags)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Error loading local conversation")
	}
	id := strings.TrimSpace(c.Params("id"))
	selected := findConversation(data["Conversations"].([]Conversation), id)
	if selected == nil {
		return fiber.NewError(fiber.StatusNotFound, "Conversation not found")
	}
	account, ok := h.mirrorAccountForEmail(c, selected.AccountEmail)
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "Mail account not found")
	}
	if err := h.mailDB.SetConversationTags(c.UserContext(), account.ID, id, tags); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Could not save conversation tags")
	}
	return c.JSON(fiber.Map{"ok": true, "tags": tags})
}
