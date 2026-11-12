package web

import (
	"encoding/json"
	"errors"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

func (h *AISettingsHandler) RegisterMailIgnoreRuleRoutes(router fiber.Router) {
	router.Get("/accounts/:id/automation-ignore-rules", h.HandleGetMailIgnoreRules)
	router.Put("/accounts/:id/automation-ignore-rules", h.HandleSaveMailIgnoreRules)
	router.Post("/accounts/:id/automation-ignore-rules/preview", h.HandlePreviewMailIgnoreRules)
}

func mailIgnoreRulesError(err error) error {
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if errors.Is(err, mailstore.ErrInvalidMailIgnoreRules) {
		return fiber.NewError(400, err.Error())
	}
	return fiber.ErrInternalServerError
}

func (h *AISettingsHandler) HandleGetMailIgnoreRules(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	rules, err := h.mailDB.GetMailIgnoreRules(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return mailIgnoreRulesError(err)
	}
	return c.JSON(fiber.Map{"rules": rules})
}

// Require an explicit array so a typo or missing body cannot clear the rules.
func readMailIgnoreRules(c *fiber.Ctx) ([]mailstore.MailIgnoreRule, error) {
	if len(c.Body()) > 1<<20 {
		return nil, fiber.NewError(413, "ignore rules exceed 1 MiB")
	}
	var input struct {
		Rules json.RawMessage `json:"rules"`
	}
	if c.BodyParser(&input) != nil || len(input.Rules) == 0 || input.Rules[0] != '[' {
		return nil, fiber.NewError(400, "rules must be an explicit array")
	}
	var rules []mailstore.MailIgnoreRule
	if json.Unmarshal(input.Rules, &rules) != nil {
		return nil, fiber.NewError(400, "invalid ignore rules")
	}
	return rules, nil
}

func (h *AISettingsHandler) HandleSaveMailIgnoreRules(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	rules, err := readMailIgnoreRules(c)
	if err != nil {
		return err
	}
	rules, err = h.mailDB.SaveMailIgnoreRules(c.UserContext(), owner, c.Params("id"), rules)
	if err != nil {
		return mailIgnoreRulesError(err)
	}
	return c.JSON(fiber.Map{"rules": rules})
}

func (h *AISettingsHandler) HandlePreviewMailIgnoreRules(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	rules, err := readMailIgnoreRules(c)
	if err != nil {
		return err
	}
	preview, err := h.mailDB.PreviewMailIgnoreRules(c.UserContext(), owner, c.Params("id"), rules)
	if err != nil {
		return mailIgnoreRulesError(err)
	}
	return c.JSON(preview)
}
