package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"inbrix/mailstore"
)

type aiSkillInput struct {
	ID           *string   `json:"id"`
	Name         *string   `json:"name"`
	Description  *string   `json:"description"`
	Instructions *string   `json:"instructions"`
	Kind         *string   `json:"kind"`
	DependsOn    *[]string `json:"dependsOn"`
	Enabled      *bool     `json:"enabled"`
	Revision     *int64    `json:"revision"`
}

func (h *AISettingsHandler) RegisterSkillRoutes(router fiber.Router) {
	router.Get("/skills", h.HandleListSkills)
	router.Post("/skills", h.HandleCreateSkill)
	router.Get("/skills/:id", h.HandleGetSkill)
	router.Put("/skills/:id", h.HandleUpdateSkill)
	router.Patch("/skills/:id", h.HandleUpdateSkill)
	router.Delete("/skills/:id", h.HandleDeleteSkill)
	router.Post("/skills/:id/use-for-mail-summary", h.HandleUseMailSummarySkill)
	router.Get("/skills/:id/versions", h.HandleListSkillVersions)
	router.Get("/skills/:id/versions/:revision", h.HandleGetSkillVersion)
	router.Post("/skills/:id/restore", h.HandleRestoreSkillVersion)
}

func skillChangeContext(c *fiber.Ctx) context.Context {
	source := "browser"
	if id, _ := c.Locals("api_key_id").(string); id != "" {
		source = "api_key:" + id
	}
	return mailstore.WithAISkillChangeSource(c.UserContext(), source)
}

func (h *AISettingsHandler) HandleListSkillVersions(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var before int64
	if value := c.Query("before"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before < 1 {
			return fiber.NewError(400, "invalid before revision")
		}
	}
	versions, err := h.mailDB.ListAISkillVersions(c.UserContext(), owner, c.Params("id"), before, 50)
	if err != nil {
		return skillHTTPError(err)
	}
	return c.JSON(fiber.Map{"versions": versions})
}

func (h *AISettingsHandler) HandleGetSkillVersion(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	revision, err := strconv.ParseInt(c.Params("revision"), 10, 64)
	if err != nil || revision < 1 {
		return fiber.NewError(400, "invalid revision")
	}
	v, err := h.mailDB.GetAISkillVersion(c.UserContext(), owner, c.Params("id"), revision)
	if err != nil {
		return skillHTTPError(err)
	}
	return c.JSON(v)
}

func (h *AISettingsHandler) HandleRestoreSkillVersion(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		Revision         int64 `json:"revision"`
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(c.Body())))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Revision < 1 || input.ExpectedRevision < 1 {
		return fiber.NewError(400, "revision and expectedRevision must be positive")
	}
	restored, err := h.mailDB.RestoreAISkillVersion(skillChangeContext(c), owner, c.Params("id"), input.Revision, input.ExpectedRevision)
	if err != nil {
		return skillHTTPError(err)
	}
	return c.JSON(restored)
}

func skillHTTPError(err error) error {
	switch {
	case errors.Is(err, mailstore.ErrNotFound):
		return fiber.ErrNotFound
	case errors.Is(err, mailstore.ErrAISkillConflict):
		return fiber.NewError(409, "Skill 已被修改或标识已存在，请刷新后重试")
	case errors.Is(err, mailstore.ErrAISkillInUse):
		return fiber.NewError(409, err.Error())
	case errors.Is(err, mailstore.ErrInvalidAISkill):
		return fiber.NewError(400, err.Error())
	default:
		return fiber.ErrInternalServerError
	}
}

func decodeSkillInput(c *fiber.Ctx) (aiSkillInput, error) {
	var input aiSkillInput
	decoder := json.NewDecoder(strings.NewReader(string(c.Body())))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fiber.NewError(400, "invalid Skill JSON: "+err.Error())
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return input, fiber.NewError(400, "invalid trailing JSON")
	}
	if input.Revision != nil && *input.Revision < 1 {
		return input, fiber.NewError(400, "revision must be positive")
	}
	return input, nil
}

func (h *AISettingsHandler) HandleListSkills(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	skills, err := h.mailDB.ListAISkills(c.UserContext(), owner)
	if err != nil {
		return skillHTTPError(err)
	}
	id, err := h.mailDB.MailSummarySkillID(c.UserContext(), owner)
	if err != nil {
		return skillHTTPError(err)
	}
	return c.JSON(fiber.Map{"skills": skills, "mailSummarySkillId": id})
}

func (h *AISettingsHandler) HandleGetSkill(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	skill, err := h.mailDB.GetAISkill(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return skillHTTPError(err)
	}
	return c.JSON(skill)
}

func (h *AISettingsHandler) HandleCreateSkill(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	input, err := decodeSkillInput(c)
	if err != nil {
		return err
	}
	skill := mailstore.AISkillRecord{OwnerID: owner, Enabled: true, Kind: mailstore.AISkillChat, DependsOn: []string{}}
	applySkillInput(&skill, input)
	created, err := h.mailDB.SaveAISkill(skillChangeContext(c), skill, 0)
	if err != nil {
		return skillHTTPError(err)
	}
	return c.Status(201).JSON(created)
}

func applySkillInput(skill *mailstore.AISkillRecord, input aiSkillInput) {
	if input.ID != nil {
		skill.ID = *input.ID
	}
	if input.Name != nil {
		skill.Name = *input.Name
	}
	if input.Description != nil {
		skill.Description = *input.Description
	}
	if input.Instructions != nil {
		skill.Instructions = *input.Instructions
	}
	if input.Kind != nil {
		skill.Kind = *input.Kind
	}
	if input.DependsOn != nil {
		skill.DependsOn = *input.DependsOn
	}
	if input.Enabled != nil {
		skill.Enabled = *input.Enabled
	}
}

func (h *AISettingsHandler) HandleUpdateSkill(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	input, err := decodeSkillInput(c)
	if err != nil {
		return err
	}
	skill, err := h.mailDB.GetAISkill(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return skillHTTPError(err)
	}
	if input.ID != nil && *input.ID != skill.ID {
		return fiber.NewError(400, "Skill id cannot be changed")
	}
	expected := skill.Revision
	if input.Revision != nil {
		expected = *input.Revision
	}
	applySkillInput(&skill, input)
	updated, err := h.mailDB.SaveAISkill(skillChangeContext(c), skill, expected)
	if err != nil {
		return skillHTTPError(err)
	}
	return c.JSON(updated)
}

func (h *AISettingsHandler) HandleDeleteSkill(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var revision int64
	if value := c.Query("revision"); value != "" {
		revision, err = strconv.ParseInt(value, 10, 64)
		if err != nil || revision < 1 {
			return fiber.NewError(400, "invalid revision")
		}
	}
	if err := h.mailDB.DeleteAISkill(c.UserContext(), owner, c.Params("id"), revision); err != nil {
		return skillHTTPError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *AISettingsHandler) HandleUseMailSummarySkill(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if len(c.Body()) > 0 && (c.BodyParser(&input) != nil || input.Revision < 0) {
		return fiber.NewError(400, "invalid revision")
	}
	if err := h.mailDB.SetMailSummarySkill(c.UserContext(), owner, c.Params("id"), input.Revision); err != nil {
		return skillHTTPError(err)
	}
	return c.JSON(fiber.Map{"mailSummarySkillId": c.Params("id")})
}
