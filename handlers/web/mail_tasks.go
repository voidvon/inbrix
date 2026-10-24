package web

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"inbrix/mailstore"
)

func (h *AISettingsHandler) HandleListMailTasks(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, strings.TrimSpace(c.Query("accountEmail")))
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	limit := 50
	if value := c.Query("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return fiber.NewError(400, "limit must be between 1 and 100")
		}
	}
	uid := strings.TrimSpace(c.Query("messageId"))
	if uid != "" {
		if n, err := strconv.ParseUint(uid, 10, 32); err != nil || n == 0 {
			return fiber.NewError(400, "invalid messageId")
		}
	}
	tasks, err := h.mailDB.ListMailTasks(c.UserContext(), account.ID, strings.TrimSpace(c.Query("folder")), uid, limit)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	items := make([]fiber.Map, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, fiber.Map{"folder": task.FolderName, "messageId": task.UID, "taskType": task.Type, "status": task.Status, "attempts": task.Attempts, "nextAttempt": task.NextAttempt, "leaseUntil": task.LeaseUntil, "error": task.ErrorMessage, "createdAt": task.CreatedAt, "updatedAt": task.UpdatedAt})
	}
	return c.JSON(fiber.Map{"tasks": items})
}

// The run journal exposes execution identity and diagnostic metadata, never
// fencing tokens, notification payloads, credentials or full mail contents.
func (h *AISettingsHandler) HandleListMailTaskRuns(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, strings.TrimSpace(c.Query("accountEmail")))
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	limit := 50
	if value := c.Query("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return fiber.NewError(400, "limit must be between 1 and 100")
		}
	}
	uid := strings.TrimSpace(c.Query("messageId"))
	if uid != "" {
		if n, err := strconv.ParseUint(uid, 10, 32); err != nil || n == 0 {
			return fiber.NewError(400, "invalid messageId")
		}
	}
	kind := strings.TrimSpace(c.Query("taskType"))
	if kind != "" && kind != "analysis" && kind != "reply" && kind != "notification" {
		return fiber.NewError(400, "invalid taskType")
	}
	runs, err := h.mailDB.ListMailTaskRuns(c.UserContext(), account.ID, strings.TrimSpace(c.Query("folder")), uid, kind, limit)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"runs": runs})
}

func (h *AISettingsHandler) HandleRetryMailTask(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		AccountEmail string `json:"accountEmail"`
		Folder       string `json:"folder"`
		MessageID    string `json:"messageId"`
		TaskType     string `json:"taskType"`
	}
	if c.BodyParser(&input) != nil {
		return fiber.NewError(400, "invalid JSON body")
	}
	if input.TaskType != "analysis" && input.TaskType != "reply" && input.TaskType != "notification" {
		return fiber.NewError(400, "invalid taskType")
	}
	input.Folder = strings.TrimSpace(input.Folder)
	input.MessageID = strings.TrimSpace(input.MessageID)
	if n, err := strconv.ParseUint(input.MessageID, 10, 32); err != nil || n == 0 || input.Folder == "" {
		return fiber.NewError(400, "folder and messageId are required")
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, strings.TrimSpace(input.AccountEmail))
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	err = h.mailDB.RetryMailTask(c.UserContext(), mailstore.MessageSummaryKey{AccountID: account.ID, FolderName: input.Folder, UID: input.MessageID}, input.TaskType)
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.NewError(404, "failed mail task not found")
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"status": "pending"})
}
