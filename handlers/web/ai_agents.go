package web

import (
	"errors"
	"strings"

	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

type aiAgentInput struct {
	Name         string   `json:"name"`
	Prompt       string   `json:"prompt"`
	OutputLabels []string `json:"outputLabels"`
}

type aiAgentPublic struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Prompt       string   `json:"prompt"`
	OutputLabels []string `json:"outputLabels"`
}

type aiTaskBindingInput struct {
	AccountEmail string `json:"accountEmail"`
	TaskType     string `json:"taskType"`
	AgentID      string `json:"agentId"`
	ModelID      string `json:"modelId"`
}

type aiTaskBindingPublic struct {
	AccountEmail string `json:"accountEmail"`
	TaskType     string `json:"taskType"`
	AgentID      string `json:"agentId"`
	ModelID      string `json:"modelId"`
	Explicit     bool   `json:"explicit"`
}

func publicAIAgent(agent mailstore.AIAgentRecord) aiAgentPublic {
	return aiAgentPublic{ID: agent.ID, Name: agent.Name, Prompt: agent.Prompt, OutputLabels: agent.OutputLabels}
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
	input.OutputLabels, err = mailstore.NormalizeAIAgentOutputLabels(input.OutputLabels)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if input.Name == "" || input.Prompt == "" {
		return fiber.NewError(fiber.StatusBadRequest, "agent name and prompt are required")
	}
	created, err := h.mailDB.CreateAIAgent(c.UserContext(), mailstore.AIAgentRecord{OwnerID: owner, Name: input.Name, Prompt: input.Prompt, OutputLabels: input.OutputLabels})
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
	input.OutputLabels, err = mailstore.NormalizeAIAgentOutputLabels(input.OutputLabels)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if input.Name == "" || input.Prompt == "" {
		return fiber.NewError(fiber.StatusBadRequest, "agent name and prompt are required")
	}
	updated, err := h.mailDB.UpdateAIAgent(c.UserContext(), mailstore.AIAgentRecord{ID: c.Params("id"), OwnerID: owner, Name: input.Name, Prompt: input.Prompt, OutputLabels: input.OutputLabels})
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

func (h *AISettingsHandler) HandleListTaskBindings(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	accounts, err := h.mailDB.ListAccounts(c.UserContext(), owner)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	out := make([]aiTaskBindingPublic, 0, len(accounts)*2)
	for _, account := range accounts {
		for _, taskType := range []string{mailstore.MailSummaryTask, mailstore.EmailDraftTask} {
			item := aiTaskBindingPublic{AccountEmail: account.Email, TaskType: taskType}
			binding, bindingErr := h.mailDB.GetAITaskBinding(c.UserContext(), owner, account.ID, taskType)
			if bindingErr == nil {
				item.AgentID, item.ModelID, item.Explicit = binding.AgentID, binding.ModelID, true
			} else if errors.Is(bindingErr, mailstore.ErrNotFound) {
				model, modelErr := h.mailDB.GetDefaultAIModel(c.UserContext(), owner)
				if modelErr == nil {
					item.ModelID = model.ID
				} else if !errors.Is(modelErr, mailstore.ErrNotFound) {
					return fiber.ErrInternalServerError
				}
				if taskType == mailstore.MailSummaryTask {
					agent, agentErr := h.mailDB.GetMailSummaryAgent(c.UserContext(), owner)
					if agentErr == nil {
						item.AgentID = agent.ID
					} else if !errors.Is(agentErr, mailstore.ErrNotFound) {
						return fiber.ErrInternalServerError
					}
				}
			} else {
				return fiber.ErrInternalServerError
			}
			out = append(out, item)
		}
	}
	return c.JSON(fiber.Map{"bindings": out})
}

func (h *AISettingsHandler) HandleSaveTaskBinding(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input aiTaskBindingInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	input.AccountEmail = strings.TrimSpace(input.AccountEmail)
	input.TaskType = strings.TrimSpace(input.TaskType)
	input.AgentID = strings.TrimSpace(input.AgentID)
	input.ModelID = strings.TrimSpace(input.ModelID)
	if input.TaskType != mailstore.MailSummaryTask && input.TaskType != mailstore.EmailDraftTask {
		return fiber.NewError(fiber.StatusBadRequest, "unsupported AI task type")
	}
	if input.AccountEmail == "" || input.AgentID == "" || input.ModelID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "accountEmail, agentId, and modelId are required")
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, input.AccountEmail)
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if input.TaskType == mailstore.MailSummaryTask {
		agent, agentErr := h.mailDB.GetAIAgent(c.UserContext(), owner, input.AgentID)
		if errors.Is(agentErr, mailstore.ErrNotFound) {
			return fiber.NewError(fiber.StatusBadRequest, "mailbox, agent, or model does not belong to this user")
		}
		if agentErr != nil {
			return fiber.ErrInternalServerError
		}
		if len(agent.OutputLabels) == 0 {
			return fiber.NewError(fiber.StatusBadRequest, "a mail summary agent must have output labels")
		}
	}
	binding, err := h.mailDB.SaveAITaskBinding(c.UserContext(), owner, mailstore.AITaskBindingRecord{
		AccountID: account.ID,
		TaskType:  input.TaskType,
		AgentID:   input.AgentID,
		ModelID:   input.ModelID,
	})
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.NewError(fiber.StatusBadRequest, "mailbox, agent, or model does not belong to this user")
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(aiTaskBindingPublic{
		AccountEmail: account.Email,
		TaskType:     binding.TaskType,
		AgentID:      binding.AgentID,
		ModelID:      binding.ModelID,
		Explicit:     true,
	})
}
