package web

import (
	"errors"
	"strings"
	"time"

	"inbrix/handlers/api"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

func requestSession(c *fiber.Ctx, store *session.Store) (*session.Session, error) {
	return api.RequestSession(c, store)
}

func apiKeyToken(c *fiber.Ctx) (string, bool) {
	if token := strings.TrimSpace(c.Get("X-API-Key")); token != "" {
		return token, true
	}
	parts := strings.Fields(c.Get(fiber.HeaderAuthorization))
	if len(parts) >= 2 && strings.EqualFold(parts[0], "Bearer") && strings.HasPrefix(parts[1], mailstore.APIKeyPrefix) {
		if len(parts) != 2 {
			return "", true
		}
		return parts[1], true
	}
	return "", false
}

// APIKeyMiddleware authenticates the owner before the existing session and
// authorization middleware. Explicit invalid keys never fall back to cookies.
func (h *AuthHandler) APIKeyMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		token, supplied := apiKeyToken(c)
		if !supplied {
			return c.Next()
		}
		if h.mirror == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "API Key authentication is unavailable")
		}
		user, _, err := h.mirror.AuthenticateAPIKey(c.UserContext(), token, c.IP())
		if errors.Is(err, mailstore.ErrNotFound) {
			return fiber.NewError(fiber.StatusUnauthorized, "Invalid, expired or revoked API Key")
		}
		if err != nil {
			return fiber.ErrInternalServerError
		}
		accounts, err := h.mirror.ListAccounts(c.UserContext(), user.ID)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		email := user.Login
		values := map[string]any{"authenticated": true, "user_id": user.ID, "username": user.Login}
		// Explicit selection is owner-scoped; otherwise use the user's default.
		selected := strings.TrimSpace(c.Get("X-Account-ID"))
		if selected != "" {
			account, err := h.mirror.GetAccount(c.UserContext(), selected)
			if err != nil || account.OwnerID != user.ID {
				return fiber.NewError(fiber.StatusNotFound, "Mailbox not found")
			}
			accounts = []mailstore.Account{account}
		}
		if len(accounts) > 0 {
			account := accounts[0]
			email = account.Email
			values["account_id"], values["credentials"] = account.ID, account.EncryptedPassword
			if account.AuthType == demoAuthType {
				values["auth_type"] = demoAuthType
			}
		}
		values["email"] = email
		// Some compatibility routes additionally validate the session's JWT.
		// It is request-local and cannot replace key validation on later requests.
		jwt, err := api.GenerateToken(user.Login, email, h.config.JWT.Secret)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		values["token"] = jwt
		if err := api.UseAPIKeySession(c, values); err != nil {
			return fiber.ErrInternalServerError
		}
		c.Locals("user_id", user.ID)
		c.Locals("username", user.Login)
		c.Locals("email", email)
		defer api.ClearRequestSessionHeader(c)
		c.Request().Header.Set(fiber.HeaderAuthorization, "Bearer "+jwt)
		return c.Next()
	}
}

type APIKeysHandler struct{ mailDB *mailstore.Store }

func NewAPIKeysHandler(db *mailstore.Store) *APIKeysHandler { return &APIKeysHandler{mailDB: db} }

func (h *APIKeysHandler) owner(c *fiber.Ctx) (string, error) {
	c.Set(fiber.HeaderCacheControl, "private, no-store, max-age=0")
	if h.mailDB == nil {
		return "", fiber.NewError(fiber.StatusNotImplemented, "Local user accounts are unavailable")
	}
	owner, _ := c.Locals("user_id").(string)
	if owner == "" {
		return "", fiber.ErrUnauthorized
	}
	if _, err := h.mailDB.GetUser(c.UserContext(), owner); errors.Is(err, mailstore.ErrNotFound) {
		return "", fiber.ErrUnauthorized
	} else if err != nil {
		return "", fiber.ErrInternalServerError
	}
	return owner, nil
}

func apiKeyError(err error) error {
	switch {
	case errors.Is(err, mailstore.ErrNotFound):
		return fiber.ErrNotFound
	case errors.Is(err, mailstore.ErrInvalidAPIKeyInput):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, mailstore.ErrAPIKeyInactive):
		return fiber.NewError(fiber.StatusConflict, "API Key 已过期或撤销，无法轮换")
	default:
		return fiber.ErrInternalServerError
	}
}

func (h *APIKeysHandler) Register(router fiber.Router) {
	router.Get("/settings/api-keys", h.HandleList)
	router.Post("/settings/api-keys", h.HandleCreate)
	router.Post("/settings/api-keys/:id/rotate", h.HandleRotate)
	router.Post("/settings/api-keys/:id/revoke", h.HandleRevoke)
	router.Get("/settings/api-keys/:id/events", h.HandleEvents)
}

func (h *APIKeysHandler) HandleList(c *fiber.Ctx) error {
	owner, err := h.owner(c)
	if err != nil {
		return err
	}
	keys, err := h.mailDB.ListAPIKeys(c.UserContext(), owner)
	if err != nil {
		return apiKeyError(err)
	}
	return c.JSON(fiber.Map{"keys": keys})
}

func (h *APIKeysHandler) HandleCreate(c *fiber.Ctx) error {
	owner, err := h.owner(c)
	if err != nil {
		return err
	}
	var input struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid API Key input")
	}
	key, err := h.mailDB.CreateAPIKey(c.UserContext(), owner, input.Name, input.ExpiresAt, c.IP())
	if err != nil {
		return apiKeyError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(key)
}

func (h *APIKeysHandler) HandleRotate(c *fiber.Ctx) error {
	owner, err := h.owner(c)
	if err != nil {
		return err
	}
	key, err := h.mailDB.RotateAPIKey(c.UserContext(), owner, c.Params("id"), c.IP())
	if err != nil {
		return apiKeyError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(key)
}

func (h *APIKeysHandler) HandleRevoke(c *fiber.Ctx) error {
	owner, err := h.owner(c)
	if err != nil {
		return err
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&input); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "Invalid API Key input")
		}
	}
	if err := h.mailDB.RevokeAPIKey(c.UserContext(), owner, c.Params("id"), c.IP(), input.Reason); err != nil {
		return apiKeyError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *APIKeysHandler) HandleEvents(c *fiber.Ctx) error {
	owner, err := h.owner(c)
	if err != nil {
		return err
	}
	events, err := h.mailDB.ListAPIKeyEvents(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return apiKeyError(err)
	}
	return c.JSON(fiber.Map{"events": events})
}
