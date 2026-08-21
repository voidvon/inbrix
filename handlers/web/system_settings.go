package web

import (
	"errors"
	"strings"
	"time"

	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

type SystemSettingsHandler struct {
	mailDB  *mailstore.Store
	version string
}

type systemUserJSON struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	CreatedAt   string `json:"createdAt"`
}

func NewSystemSettingsHandler(mailDB *mailstore.Store, version string) *SystemSettingsHandler {
	return &SystemSettingsHandler{mailDB: mailDB, version: version}
}

func (h *SystemSettingsHandler) currentUser(c *fiber.Ctx) (mailstore.User, error) {
	if h == nil || h.mailDB == nil {
		return mailstore.User{}, fiber.NewError(fiber.StatusNotImplemented, "local user accounts are unavailable")
	}
	userID, _ := c.Locals("user_id").(string)
	if strings.TrimSpace(userID) == "" {
		return mailstore.User{}, fiber.ErrUnauthorized
	}
	user, err := h.mailDB.GetUser(c.UserContext(), userID)
	if errors.Is(err, mailstore.ErrNotFound) {
		return mailstore.User{}, fiber.ErrUnauthorized
	}
	if err != nil {
		return mailstore.User{}, fiber.ErrInternalServerError
	}
	return user, nil
}

func (h *SystemSettingsHandler) RequireSuperAdmin(c *fiber.Ctx) error {
	user, err := h.currentUser(c)
	if err != nil {
		return err
	}
	if user.Role != mailstore.RoleSuperAdmin {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Super administrator access required"})
	}
	c.Locals("system_admin", user)
	return c.Next()
}

func (h *SystemSettingsHandler) HandleGet(c *fiber.Ctx) error {
	admin, ok := c.Locals("system_admin").(mailstore.User)
	if !ok {
		return fiber.ErrForbidden
	}
	users, err := h.mailDB.ListUsers(c.UserContext())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	registrationOpen, err := h.mailDB.RegistrationOpen(c.UserContext())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	out := make([]systemUserJSON, 0, len(users))
	for _, user := range users {
		out = append(out, publicSystemUser(user))
	}
	return c.JSON(fiber.Map{
		"version":          h.version,
		"currentUserId":    admin.ID,
		"registrationOpen": registrationOpen,
		"users":            out,
	})
}

func (h *SystemSettingsHandler) HandleRegistrationStatus(c *fiber.Ctx) error {
	if h == nil || h.mailDB == nil {
		return c.JSON(fiber.Map{"registrationOpen": false})
	}
	registrationOpen, err := h.mailDB.RegistrationOpen(c.UserContext())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"registrationOpen": registrationOpen})
}

func (h *SystemSettingsHandler) HandleUpdateRegistration(c *fiber.Ctx) error {
	var input struct {
		RegistrationOpen *bool `json:"registrationOpen"`
	}
	if err := c.BodyParser(&input); err != nil || input.RegistrationOpen == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "registrationOpen must be a boolean"})
	}
	if err := h.mailDB.SetRegistrationOpen(c.UserContext(), *input.RegistrationOpen); err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"registrationOpen": *input.RegistrationOpen})
}

func (h *SystemSettingsHandler) HandleUpdateUserRole(c *fiber.Ctx) error {
	admin, ok := c.Locals("system_admin").(mailstore.User)
	if !ok {
		return fiber.ErrForbidden
	}
	var input struct {
		Role string `json:"role"`
	}
	if err := c.BodyParser(&input); err != nil || !mailstore.ValidUserRole(input.Role) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "role must be user or super_admin"})
	}
	target, err := h.mailDB.GetUser(c.UserContext(), c.Params("id"))
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if target.ID == admin.ID && input.Role != mailstore.RoleSuperAdmin {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "a super administrator cannot demote their own account"})
	}
	if err := h.mailDB.SetUserRole(c.UserContext(), target.ID, input.Role); err != nil {
		return fiber.ErrInternalServerError
	}
	target.Role = input.Role
	return c.JSON(publicSystemUser(target))
}

func (h *SystemSettingsHandler) HandleUpdateProfile(c *fiber.Ctx) error {
	user, err := h.currentUser(c)
	if err != nil {
		return err
	}
	var input struct {
		DisplayName string `json:"displayName"`
	}
	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "displayName must be a string"})
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if len([]rune(input.DisplayName)) > 80 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "displayName must be at most 80 characters"})
	}
	if err := h.mailDB.SetUserDisplayName(c.UserContext(), user.ID, input.DisplayName); err != nil {
		if errors.Is(err, mailstore.ErrNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	user.DisplayName = input.DisplayName
	return c.JSON(publicSystemUser(user))
}

func publicSystemUser(user mailstore.User) systemUserJSON {
	createdAt := ""
	if !user.CreatedAt.IsZero() {
		createdAt = user.CreatedAt.UTC().Format(time.RFC3339)
	}
	return systemUserJSON{
		ID:          user.ID,
		Login:       user.Login,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		CreatedAt:   createdAt,
	}
}
