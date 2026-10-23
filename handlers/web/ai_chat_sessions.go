package web

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

func chatStoreError(err error) error {
	if errors.Is(err, mailstore.ErrAIChatCursor) {
		return fiber.NewError(400, err.Error())
	}
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if errors.Is(err, mailstore.ErrAIChatConflict) || errors.Is(err, mailstore.ErrAIChatAttachmentInUse) {
		return fiber.NewError(409, err.Error())
	}
	return fiber.ErrInternalServerError
}

func (h *AISettingsHandler) HandleAIChatSessions(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	email := strings.TrimSpace(c.Query("accountEmail"))
	var body struct {
		AccountEmail string `json:"accountEmail"`
		Title        string `json:"title"`
		DocumentType string `json:"documentType"`
	}
	if c.Method() == "POST" {
		if err = c.BodyParser(&body); err != nil {
			return fiber.ErrBadRequest
		}
		email = strings.TrimSpace(body.AccountEmail)
	}
	if email == "" {
		return fiber.NewError(400, "accountEmail is required")
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, email)
	if err != nil {
		return chatStoreError(err)
	}
	if c.Method() == "GET" {
		limit, e := strconv.Atoi(c.Query("limit", "30"))
		if e != nil || limit < 1 || limit > 100 {
			return fiber.ErrBadRequest
		}
		sessions, e := h.mailDB.ListAIChatSessionPage(c.UserContext(), owner, account.ID, limit, c.Query("cursor"))
		if e != nil {
			return chatStoreError(e)
		}
		return c.JSON(sessions)
	}
	if body.DocumentType != "quotation" && body.DocumentType != "contract" {
		return fiber.NewError(400, "invalid documentType")
	}
	if len([]rune(body.Title)) > 120 {
		return fiber.ErrBadRequest
	}
	session, err := h.mailDB.CreateAIChatSession(c.UserContext(), owner, account.ID, "document", "", body.Title, body.DocumentType)
	if err != nil {
		return chatStoreError(err)
	}
	return c.Status(201).JSON(session)
}

func (h *AISettingsHandler) HandleAIChatSession(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	email := strings.TrimSpace(c.Query("accountEmail"))
	if email == "" {
		return fiber.NewError(400, "accountEmail is required")
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, email)
	if err != nil {
		return chatStoreError(err)
	}
	id := c.Params("id")
	page, err := chatMessagePage(c)
	if err != nil {
		return err
	}
	session, err := h.mailDB.GetAIChatSession(c.UserContext(), owner, account.ID, id, page)
	if err != nil {
		return chatStoreError(err)
	}
	if c.Method() == "GET" {
		draft, e := sessionDocumentContext(session, "")
		if e != nil {
			return fiber.ErrInternalServerError
		}
		if draft != nil {
			session.CurrentDocument, _ = json.Marshal(draft)
		}
		return c.JSON(session)
	}
	if c.Method() == "DELETE" {
		version, e := strconv.ParseInt(c.Query("version"), 10, 64)
		if e != nil {
			return fiber.NewError(400, "version is required")
		}
		err = h.mailDB.DeleteAIChatSession(c.UserContext(), owner, account.ID, id, version, c.Query("clear") == "true")
		if err != nil {
			return chatStoreError(err)
		}
		return c.SendStatus(204)
	}
	var body struct {
		Version         *int64                      `json:"version"`
		BusinessContext mailstore.AIBusinessContext `json:"businessContext"`
	}
	if err = c.BodyParser(&body); err != nil || body.Version == nil {
		return fiber.ErrBadRequest
	}
	body.BusinessContext, err = mailstore.NormalizeAIBusinessContext(body.BusinessContext)
	if err != nil {
		return fiber.NewError(400, err.Error())
	}
	// Reuse the authoritative validation used by mailbox chat preferences.
	if err = h.mailDB.ValidateAIChatBusiness(c.UserContext(), owner, account.ID, body.BusinessContext); err != nil {
		return fiber.NewError(400, err.Error())
	}
	if err = h.mailDB.UpdateAIChatBusiness(c.UserContext(), owner, account.ID, id, *body.Version, body.BusinessContext); err != nil {
		return chatStoreError(err)
	}
	session, err = h.mailDB.GetAIChatSession(c.UserContext(), owner, account.ID, id, page)
	if err != nil {
		return chatStoreError(err)
	}
	return c.JSON(session)
}

func chatMessagePage(c *fiber.Ctx) (mailstore.AIChatMessagePage, error) {
	limit, err := strconv.Atoi(c.Query("limit", "100"))
	if err != nil || limit < 1 || limit > 100 {
		return mailstore.AIChatMessagePage{}, fiber.ErrBadRequest
	}
	page := mailstore.AIChatMessagePage{Limit: limit}
	if raw := c.Query("before"); raw != "" {
		before, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || before < 0 {
			return page, fiber.ErrBadRequest
		}
		page.Before = &before
	}
	return page, nil
}

// decodeSessionMessages keeps the store independent of provider and UI types.
func decodeSessionMessages(session mailstore.AIChatSession) ([]AIAgentChatMessage, error) {
	out := make([]AIAgentChatMessage, 0, len(session.Messages))
	for _, raw := range session.Messages {
		var m AIAgentChatMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func sendAIChatReplay(c *fiber.Ctx, raw json.RawMessage) error {
	var saved AIAgentChatResponse
	if json.Unmarshal(raw, &saved) != nil {
		return fiber.ErrInternalServerError
	}
	if strings.Contains(c.Get("Accept"), "text/event-stream") {
		return streamAgentChat(c, func(context.Context, func(agentChatEvent) error) (*AIAgentChatResponse, error) { return &saved, nil })
	}
	return c.JSON(saved)
}
