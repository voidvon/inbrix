package web

import (
	"errors"
	"strings"

	"lilmail/mailstore"

	"github.com/gofiber/fiber/v2"
)

type aiAgentInput struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
}

type aiAgentPublic struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
}

func publicAIAgent(agent mailstore.AIAgentRecord) aiAgentPublic {
	return aiAgentPublic{ID: agent.ID, Name: agent.Name, Prompt: agent.Prompt}
}

func (h *AISettingsHandler) HandleListAgents(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	agents, err := h.mailDB.ListAIAgents(c.UserContext(), owner)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	out := make([]aiAgentPublic, 0, len(agents))
	for _, agent := range agents {
		out = append(out, publicAIAgent(agent))
	}
	return c.JSON(fiber.Map{"agents": out})
}

func (h *AISettingsHandler) HandleCreateAgent(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input aiAgentInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Name == "" || input.Prompt == "" {
		return fiber.NewError(fiber.StatusBadRequest, "agent name and prompt are required")
	}
	created, err := h.mailDB.CreateAIAgent(c.UserContext(), mailstore.AIAgentRecord{OwnerID: owner, Name: input.Name, Prompt: input.Prompt})
	if err != nil {
		return fiber.NewError(fiber.StatusConflict, "this agent already exists")
	}
	return c.Status(fiber.StatusCreated).JSON(publicAIAgent(created))
}

func (h *AISettingsHandler) HandleUpdateAgent(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input aiAgentInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Name == "" || input.Prompt == "" {
		return fiber.NewError(fiber.StatusBadRequest, "agent name and prompt are required")
	}
	updated, err := h.mailDB.UpdateAIAgent(c.UserContext(), mailstore.AIAgentRecord{ID: c.Params("id"), OwnerID: owner, Name: input.Name, Prompt: input.Prompt})
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.NewError(fiber.StatusConflict, "this agent already exists")
	}
	return c.JSON(publicAIAgent(updated))
}

func (h *AISettingsHandler) HandleDeleteAgent(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	if err := h.mailDB.DeleteAIAgent(c.UserContext(), owner, c.Params("id")); errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	} else if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"ok": true})
}
