// handlers/web/accounts.go
//
// Multi-account HTTP handlers.
//
// The primary account is always the one stored in the session.  Additional
// accounts are stored in the shared durable store and rendered in a Settings panel.
//
// Routes are registered for the SQLite mirror by default, or for the legacy
// compatibility path when [mail_sync] is disabled and [accounts] is enabled:
//
//	GET  /api/accounts              → JSON list of additional accounts
//	POST /api/accounts              → add an account (validate IMAP, store encrypted)
//	DELETE /api/accounts/:email     → remove an account
//	POST /api/accounts/:email/switch → switch active session to this account
//	GET  /settings                  → settings page (accounts panel + push enable)
package web

import (
	"errors"
	"fmt"
	"inbrix/config"
	"inbrix/handlers/api"
	"inbrix/mailstore"
	"inbrix/storage"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

type webhookSettingsInput struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

type webhookTestInput struct {
	URL string `json:"url"`
}

type accountUpdateInput struct {
	Password   string `json:"password"`
	Label      string `json:"label"`
	Color      string `json:"color"`
	IMAPServer string `json:"imap_server"`
	IMAPPort   int    `json:"imap_port"`
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"`
}

// AccountsHandler manages multi-account operations.
type AccountsHandler struct {
	store     *session.Store
	config    *config.Config
	auth      *AuthHandler
	acctStore *AccountStore
	mailDB    *mailstore.Store
	syncer    *mailstore.SyncManager
}

func (h *AccountsHandler) SetMailMirror(db *mailstore.Store, syncer *mailstore.SyncManager) {
	h.mailDB = db
	h.syncer = syncer
}

func (h *AccountsHandler) mirrorOwner(c *fiber.Ctx) string {
	sess, err := h.store.Get(c)
	if err != nil {
		return ""
	}
	owner, _ := sess.Get("user_id").(string)
	return owner
}

// NewAccountsHandler creates a handler. acctStore must be non-nil.
func NewAccountsHandler(store *session.Store, cfg *config.Config, auth *AuthHandler, acctStore *AccountStore) *AccountsHandler {
	return &AccountsHandler{
		store:     store,
		config:    cfg,
		auth:      auth,
		acctStore: acctStore,
	}
}

// HandleListAccounts returns the list of additional accounts for the current user.
func (h *AccountsHandler) HandleListAccounts(c *fiber.Ctx) error {
	if h.mailDB != nil {
		owner := h.mirrorOwner(c)
		if owner == "" {
			return fiber.ErrUnauthorized
		}
		entries, err := h.mailDB.ListAccounts(c.UserContext(), owner)
		if err != nil {
			return fiber.ErrInternalServerError
		}
		return c.JSON(mirrorSafeAccounts(entries))
	}
	owner, _ := c.Locals("username").(string)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	entries, err := h.acctStore.List(owner)
	if err != nil {
		log.Printf("accounts: list for %s: %v", owner, err)
		return fiber.ErrInternalServerError
	}
	// Strip encrypted password from API response.
	type safe struct {
		Email      string `json:"email"`
		Label      string `json:"label"`
		Color      string `json:"color,omitempty"`
		IMAPServer string `json:"imap_server"`
		IMAPPort   int    `json:"imap_port"`
		SMTPServer string `json:"smtp_server"`
		SMTPPort   int    `json:"smtp_port"`
	}
	out := make([]safe, 0, len(entries))
	for _, e := range entries {
		out = append(out, safe{
			Email:      e.Email,
			Label:      e.Label,
			Color:      e.Color,
			IMAPServer: e.IMAPServer,
			IMAPPort:   e.IMAPPort,
			SMTPServer: e.SMTPServer,
			SMTPPort:   e.SMTPPort,
		})
	}
	return c.JSON(out)
}

// HandleResyncAttachments invalidates the local MIME attachment markers for
// the active mailbox and wakes its background worker. The work is intentionally
// asynchronous: a mailbox can contain thousands of messages, and the request
// should return as soon as the repair has been queued.
func (h *AccountsHandler) HandleResyncAttachments(c *fiber.Ctx) error {
	if h.mailDB == nil || h.syncer == nil {
		return fiber.NewError(fiber.StatusNotFound, "mail mirror sync is not enabled")
	}
	owner := h.mirrorOwner(c)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	accountID, _ := sess.Get("account_id").(string)
	if accountID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "no active mailbox")
	}
	account, err := h.mailDB.GetAccount(c.UserContext(), accountID)
	if err != nil || account.OwnerID != owner {
		return fiber.ErrForbidden
	}
	if err := h.mailDB.ResetAttachmentMetadata(c.UserContext(), account.ID); err != nil {
		log.Printf("accounts: reset attachment metadata for %s: %v", account.Email, err)
		return fiber.ErrInternalServerError
	}
	h.syncer.Trigger(account.ID)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"ok":      true,
		"queued":  true,
		"account": account.Email,
	})
}

// HandleGetWebhookSettings returns the current user's Feishu bot settings.
func (h *AccountsHandler) HandleGetWebhookSettings(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "mail mirror sync is not enabled")
	}
	owner := h.mirrorOwner(c)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	cfg, err := h.mailDB.GetWebhookSettings(c.UserContext(), owner)
	if err != nil {
		log.Printf("webhook settings: load: %v", err)
		return fiber.ErrInternalServerError
	}
	return c.JSON(cfg)
}

// HandlePutWebhookSettings validates and replaces the current user's settings.
func (h *AccountsHandler) HandlePutWebhookSettings(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "mail mirror sync is not enabled")
	}
	owner := h.mirrorOwner(c)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	var input webhookSettingsInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	cfg := mailstore.WebhookSettings{Enabled: input.Enabled, URL: strings.TrimSpace(input.URL)}
	if cfg.Enabled && cfg.URL == "" {
		return fiber.NewError(fiber.StatusBadRequest, "an enabled webhook needs a URL")
	}
	if cfg.URL != "" {
		if err := mailstore.ValidateFeishuWebhookURL(cfg.URL); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}
	if err := h.mailDB.SaveWebhookSettings(c.UserContext(), owner, cfg); err != nil {
		log.Printf("webhook settings: save: %v", err)
		return fiber.ErrInternalServerError
	}
	return c.JSON(cfg)
}

// HandleTestWebhook sends one test message to the supplied Feishu bot URL.
// It deliberately does not save or enable the setting.
func (h *AccountsHandler) HandleTestWebhook(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "mail mirror sync is not enabled")
	}
	if h.mirrorOwner(c) == "" {
		return fiber.ErrUnauthorized
	}
	var input webhookTestInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	input.URL = strings.TrimSpace(input.URL)
	if err := mailstore.ValidateFeishuWebhookURL(input.URL); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := mailstore.SendFeishuTestWebhook(c.UserContext(), input.URL); err != nil {
		log.Printf("webhook settings: test send failed: %v", err)
		return fiber.NewError(fiber.StatusBadGateway, "could not send test message: "+err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

type mirrorSafeAccount struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Label      string `json:"label"`
	Color      string `json:"color,omitempty"`
	IMAPServer string `json:"imap_server"`
	IMAPPort   int    `json:"imap_port"`
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"`
	IsDefault  bool   `json:"is_default"`
}

func mirrorSafeAccounts(entries []mailstore.Account) []mirrorSafeAccount {
	out := make([]mirrorSafeAccount, 0, len(entries))
	for _, entry := range entries {
		out = append(out, mirrorSafeAccount{
			ID:         entry.ID,
			Email:      entry.Email,
			Label:      entry.Label,
			Color:      entry.Color,
			IMAPServer: entry.IMAPServer,
			IMAPPort:   entry.IMAPPort,
			SMTPServer: entry.SMTPServer,
			SMTPPort:   entry.SMTPPort,
			IsDefault:  entry.IsDefault,
		})
	}
	return out
}

// HandleAddAccount validates the new account credentials against the IMAP
// server and, if successful, stores the account in AccountStore.
//
// Body (JSON):
//
//	{
//	  "email":       "alice@other.com",
//	  "password":    "secret",
//	  "label":       "Work",
//	  "color":       "#4CAF50",
//	  "imap_server": "imap.other.com",
//	  "imap_port":   993,
//	  "smtp_server": "smtp.other.com",
//	  "smtp_port":   587
//	}
func (h *AccountsHandler) HandleAddAccount(c *fiber.Ctx) error {
	if h.mailDB != nil {
		return h.handleAddMirrorAccount(c)
	}
	owner, _ := c.Locals("username").(string)
	if owner == "" {
		return fiber.ErrUnauthorized
	}

	var req struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		Label      string `json:"label"`
		Color      string `json:"color"`
		IMAPServer string `json:"imap_server"`
		IMAPPort   int    `json:"imap_port"`
		SMTPServer string `json:"smtp_server"`
		SMTPPort   int    `json:"smtp_port"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Password = strings.TrimSpace(req.Password)
	if req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email and password required")
	}

	// Fill in defaults from global config when not specified.
	if req.IMAPServer == "" {
		req.IMAPServer = h.config.IMAP.Server
	}
	if req.IMAPPort == 0 {
		req.IMAPPort = h.config.IMAP.Port
	}
	if req.SMTPServer == "" {
		req.SMTPServer = h.config.SMTP.Server
	}
	if req.SMTPPort == 0 {
		req.SMTPPort = h.config.SMTP.GetPort()
	}
	if req.Label == "" {
		req.Label = req.Email
	}

	// Derive IMAP username.
	username := req.Email
	if !h.config.Server.UsernameIsEmail {
		username = api.GetUsernameFromEmail(req.Email)
	}
	if username == "" {
		return fiber.NewError(fiber.StatusBadRequest, "invalid email format")
	}

	// Validate credentials by opening and immediately closing an IMAP connection.
	client, err := api.NewClientTLS(req.IMAPServer, req.IMAPPort, username, req.Password, h.config.IMAP.TLS)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, fmt.Sprintf("IMAP login failed: %v", err))
	}
	client.Close()

	// Encrypt the password using the application encryption key.
	encPwd, err := api.EncryptJSON(req.Password, h.config.Encryption.Key)
	if err != nil {
		log.Printf("accounts: encrypt password for %s: %v", req.Email, err)
		return fiber.ErrInternalServerError
	}

	entry := AccountEntry{
		Email:             req.Email,
		Label:             req.Label,
		Color:             req.Color,
		IMAPServer:        req.IMAPServer,
		IMAPPort:          req.IMAPPort,
		SMTPServer:        req.SMTPServer,
		SMTPPort:          req.SMTPPort,
		EncryptedPassword: encPwd,
	}
	if err := h.acctStore.Save(owner, entry); err != nil {
		log.Printf("accounts: save for %s: %v", owner, err)
		return fiber.ErrInternalServerError
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"ok":    true,
		"email": entry.Email,
		"label": entry.Label,
	})
}

func (h *AccountsHandler) handleAddMirrorAccount(c *fiber.Ctx) error {
	owner := h.mirrorOwner(c)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	var req struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		Label      string `json:"label"`
		Color      string `json:"color"`
		IMAPServer string `json:"imap_server"`
		IMAPPort   int    `json:"imap_port"`
		SMTPServer string `json:"smtp_server"`
		SMTPPort   int    `json:"smtp_port"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Password = strings.TrimSpace(req.Password)
	if req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email and password required")
	}
	if req.IMAPServer == "" {
		req.IMAPServer = h.config.IMAP.Server
	}
	if req.IMAPPort == 0 {
		req.IMAPPort = h.config.IMAP.Port
	}
	if req.SMTPServer == "" {
		req.SMTPServer = h.config.SMTP.Server
	}
	if req.SMTPPort == 0 {
		req.SMTPPort = h.config.SMTP.GetPort()
	}
	if req.Label == "" {
		req.Label = req.Email
	}
	username := req.Email
	if !h.config.Server.UsernameIsEmail {
		username = api.GetUsernameFromEmail(req.Email)
	}
	if username == "" {
		return fiber.NewError(fiber.StatusBadRequest, "invalid email format")
	}
	client, err := api.NewClientTLS(req.IMAPServer, req.IMAPPort, username, req.Password, h.config.IMAP.TLS)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, fmt.Sprintf("IMAP login failed: %v", err))
	}
	_ = client.Close()
	encPwd, err := api.EncryptJSON(req.Password, h.config.Encryption.Key)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	account, err := h.mailDB.UpsertAccount(c.UserContext(), mailstore.Account{
		OwnerID:           owner,
		Email:             req.Email,
		Username:          username,
		Label:             req.Label,
		Color:             req.Color,
		IMAPServer:        req.IMAPServer,
		IMAPPort:          req.IMAPPort,
		IMAPTLS:           h.config.IMAP.TLS,
		SMTPServer:        req.SMTPServer,
		SMTPPort:          req.SMTPPort,
		SMTPStartTLS:      smtpUseSTARTTLS(req.SMTPPort, h.config.SMTP.UseSTARTTLS),
		EncryptedPassword: encPwd,
		AuthType:          "password",
		IsDefault:         false,
	})
	if err != nil {
		return fiber.ErrInternalServerError
	}
	// A newly registered application user has no active mailbox yet. Make the
	// first successfully added account immediately usable by the current
	// session; otherwise inbox rendering can find it through the owner query
	// while IMAP/SMTP handlers still have no account credentials in session.
	activeID, _ := sess.Get("account_id").(string)
	if activeID == "" {
		if err := h.setMirrorSession(c, sess, owner, account); err != nil {
			return fiber.ErrInternalServerError
		}
		if err := sess.Save(); err != nil {
			return fiber.ErrInternalServerError
		}
	}
	if h.syncer != nil {
		h.syncer.StartAccount(account.ID)
		h.syncer.Trigger(account.ID)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true, "id": account.ID, "email": account.Email, "label": account.Label})
}

// HandleUpdateAccount updates connection settings while keeping the email
// address as the stable account identifier. An empty password preserves the
// existing encrypted credential.
func (h *AccountsHandler) HandleUpdateAccount(c *fiber.Ctx) error {
	if h.mailDB != nil {
		return h.handleUpdateMirrorAccount(c)
	}
	owner, _ := c.Locals("username").(string)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	email := strings.TrimSpace(c.Params("email"))
	if email == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email param required")
	}
	existing, err := h.acctStore.Get(owner, email)
	if errors.Is(err, storage.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	var req accountUpdateInput
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	normalizeAccountUpdate(&req, existing.IMAPServer, existing.IMAPPort, existing.SMTPServer, existing.SMTPPort)
	password := strings.TrimSpace(req.Password)
	connectionChanged := password != "" || req.IMAPServer != existing.IMAPServer || req.IMAPPort != existing.IMAPPort
	if connectionChanged {
		if password == "" {
			if err := api.DecryptJSON(existing.EncryptedPassword, &password, h.config.Encryption.Key); err != nil {
				return fiber.ErrInternalServerError
			}
		}
		username := email
		if !h.config.Server.UsernameIsEmail {
			username = api.GetUsernameFromEmail(email)
		}
		client, err := api.NewClientTLS(req.IMAPServer, req.IMAPPort, username, password, h.config.IMAP.TLS)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, fmt.Sprintf("IMAP login failed: %v", err))
		}
		_ = client.Close()
	}
	if strings.TrimSpace(req.Password) != "" {
		existing.EncryptedPassword, err = api.EncryptJSON(password, h.config.Encryption.Key)
		if err != nil {
			return fiber.ErrInternalServerError
		}
	}
	existing.Label = strings.TrimSpace(req.Label)
	if existing.Label == "" {
		existing.Label = existing.Email
	}
	existing.Color = strings.TrimSpace(req.Color)
	existing.IMAPServer = req.IMAPServer
	existing.IMAPPort = req.IMAPPort
	existing.SMTPServer = req.SMTPServer
	existing.SMTPPort = req.SMTPPort
	if err := h.acctStore.Save(owner, existing); err != nil {
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"ok": true, "email": existing.Email, "label": existing.Label})
}

func (h *AccountsHandler) handleUpdateMirrorAccount(c *fiber.Ctx) error {
	owner := h.mirrorOwner(c)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	existing, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, c.Params("email"))
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	var req accountUpdateInput
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	normalizeAccountUpdate(&req, existing.IMAPServer, existing.IMAPPort, existing.SMTPServer, existing.SMTPPort)
	password := strings.TrimSpace(req.Password)
	connectionChanged := password != "" || req.IMAPServer != existing.IMAPServer || req.IMAPPort != existing.IMAPPort
	if connectionChanged {
		if password == "" {
			if err := api.DecryptJSON(existing.EncryptedPassword, &password, h.config.Encryption.Key); err != nil {
				return fiber.ErrInternalServerError
			}
		}
		client, err := api.NewClientTLS(req.IMAPServer, req.IMAPPort, existing.Username, password, existing.IMAPTLS)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, fmt.Sprintf("IMAP login failed: %v", err))
		}
		_ = client.Close()
	}
	if strings.TrimSpace(req.Password) != "" {
		existing.EncryptedPassword, err = api.EncryptJSON(password, h.config.Encryption.Key)
		if err != nil {
			return fiber.ErrInternalServerError
		}
	}
	existing.Label = strings.TrimSpace(req.Label)
	if existing.Label == "" {
		existing.Label = existing.Email
	}
	existing.Color = strings.TrimSpace(req.Color)
	existing.IMAPServer = req.IMAPServer
	existing.IMAPPort = req.IMAPPort
	existing.SMTPServer = req.SMTPServer
	existing.SMTPPort = req.SMTPPort
	existing.SMTPStartTLS = smtpUseSTARTTLS(req.SMTPPort, h.config.SMTP.UseSTARTTLS)
	updated, err := h.mailDB.UpsertAccount(c.UserContext(), existing)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if activeID, _ := sess.Get("account_id").(string); activeID == updated.ID {
		if err := h.setMirrorSession(c, sess, owner, updated); err != nil {
			return fiber.ErrInternalServerError
		}
		if err := sess.Save(); err != nil {
			return fiber.ErrInternalServerError
		}
	}
	if h.syncer != nil {
		h.syncer.Trigger(updated.ID)
	}
	return c.JSON(fiber.Map{"ok": true, "id": updated.ID, "email": updated.Email, "label": updated.Label})
}

func normalizeAccountUpdate(req *accountUpdateInput, imapServer string, imapPort int, smtpServer string, smtpPort int) {
	req.IMAPServer = strings.TrimSpace(req.IMAPServer)
	if req.IMAPServer == "" {
		req.IMAPServer = imapServer
	}
	if req.IMAPPort == 0 {
		req.IMAPPort = imapPort
	}
	req.SMTPServer = strings.TrimSpace(req.SMTPServer)
	if req.SMTPServer == "" {
		req.SMTPServer = smtpServer
	}
	if req.SMTPPort == 0 {
		req.SMTPPort = smtpPort
	}
}

// HandleDeleteAccount removes an additional account.
func (h *AccountsHandler) HandleDeleteAccount(c *fiber.Ctx) error {
	if h.mailDB != nil {
		owner := h.mirrorOwner(c)
		if owner == "" {
			return fiber.ErrUnauthorized
		}
		account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, c.Params("email"))
		if err != nil {
			return fiber.ErrNotFound
		}
		if h.syncer != nil {
			h.syncer.StopAccount(account.ID)
		}
		sess, sessErr := h.store.Get(c)
		if sessErr != nil {
			return fiber.ErrInternalServerError
		}
		if err := h.mailDB.DeleteAccount(c.UserContext(), owner, account.ID); err != nil {
			return fiber.ErrInternalServerError
		}
		// If the active mailbox was removed, move the session to the next
		// deterministic default. The user can still reach Settings when no
		// accounts remain and add a new mailbox.
		activeID, _ := sess.Get("account_id").(string)
		if activeID != account.ID {
			return c.JSON(fiber.Map{"ok": true})
		}
		remaining, listErr := h.mailDB.ListAccounts(c.UserContext(), owner)
		if listErr != nil {
			return fiber.ErrInternalServerError
		}
		if len(remaining) == 0 {
			user, userErr := h.mailDB.GetUser(c.UserContext(), owner)
			if userErr != nil {
				return fiber.ErrInternalServerError
			}
			token, tokenErr := api.GenerateToken(user.Login, user.Login, h.config.JWT.Secret)
			if tokenErr != nil {
				return fiber.ErrInternalServerError
			}
			sess.Delete("account_id")
			sess.Delete("credentials")
			sess.Set("username", user.Login)
			sess.Set("email", user.Login)
			sess.Set("token", token)
		} else if err := h.setMirrorSession(c, sess, owner, remaining[0]); err != nil {
			return fiber.ErrInternalServerError
		}
		if err := sess.Save(); err != nil {
			return fiber.ErrInternalServerError
		}
		return c.JSON(fiber.Map{"ok": true})
	}
	owner, _ := c.Locals("username").(string)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	email := c.Params("email")
	if email == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email param required")
	}
	if err := h.acctStore.Delete(owner, email); err != nil {
		log.Printf("accounts: delete %s for %s: %v", email, owner, err)
		return fiber.ErrInternalServerError
	}
	return c.JSON(fiber.Map{"ok": true})
}

// HandleSwitchAccount replaces the session credentials with those of the
// requested additional account and redirects to /inbox.  The previously active
// account credentials are saved as an additional account under the NEW owner so
// the user can switch back.
func (h *AccountsHandler) HandleSwitchAccount(c *fiber.Ctx) error {
	if h.mailDB != nil {
		return h.handleSwitchMirrorAccount(c)
	}
	owner, _ := c.Locals("username").(string)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	targetEmail := c.Params("email")
	if targetEmail == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email param required")
	}

	// Load the target account.
	entries, err := h.acctStore.List(owner)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	var target *AccountEntry
	for i := range entries {
		if entries[i].Email == targetEmail {
			target = &entries[i]
			break
		}
	}
	if target == nil {
		return fiber.NewError(fiber.StatusNotFound, "account not found")
	}

	// Decrypt the target password.
	var password string
	if err := api.DecryptJSON(target.EncryptedPassword, &password, h.config.Encryption.Key); err != nil {
		log.Printf("accounts: decrypt password for %s: %v", targetEmail, err)
		return fiber.ErrInternalServerError
	}

	// Derive IMAP username for the target account.
	targetUsername := targetEmail
	if !h.config.Server.UsernameIsEmail {
		targetUsername = api.GetUsernameFromEmail(targetEmail)
	}

	// Validate the target credentials are still good.
	client, err := api.NewClient(target.IMAPServer, target.IMAPPort, targetUsername, password)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, fmt.Sprintf("IMAP login failed for target account: %v", err))
	}
	client.Close()

	// Generate a new JWT for the target identity.
	token, err := api.GenerateToken(targetUsername, targetEmail, h.config.JWT.Secret)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	// Encrypt the new credentials.
	encCreds, err := api.EncryptJSON(&api.Credentials{Email: targetEmail, Password: password}, h.config.Encryption.Key)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	// Persist the current session account as an additional account under the new owner
	// (so the user can switch back).  We need the current password from the session.
	sess, err := h.store.Get(c)
	if err == nil {
		currentEmail, _ := sess.Get("email").(string)
		currentEncCreds, _ := sess.Get("credentials").(string)
		if currentEmail != "" && currentEncCreds != "" {
			var currentCreds api.Credentials
			if decErr := api.DecryptJSON(currentEncCreds, &currentCreds, h.config.Encryption.Key); decErr == nil {
				encBack, encErr := api.EncryptJSON(currentCreds.Password, h.config.Encryption.Key)
				if encErr == nil {
					backEntry := AccountEntry{
						Email:             currentEmail,
						Label:             currentEmail,
						IMAPServer:        h.config.IMAP.Server,
						IMAPPort:          h.config.IMAP.Port,
						SMTPServer:        h.config.SMTP.Server,
						SMTPPort:          h.config.SMTP.GetPort(),
						EncryptedPassword: encBack,
					}
					// Store it under the target user (our new identity).
					if saveErr := h.acctStore.Save(targetEmail, backEntry); saveErr != nil {
						log.Printf("accounts: save back-link account: %v", saveErr)
					}
				}
			}
		}
	}

	// Overwrite session with new identity.
	if sess == nil {
		sess, err = h.store.Get(c)
		if err != nil {
			return fiber.ErrInternalServerError
		}
	}
	sess.Set("authenticated", true)
	sess.Set("email", targetEmail)
	sess.Set("username", targetUsername)
	sess.Set("token", token)
	sess.Set("credentials", encCreds)
	// Clear any OAuth state from previous session.
	sess.Delete("auth_type")
	sess.Delete("oauth_token")

	if err := sess.Save(); err != nil {
		return fiber.ErrInternalServerError
	}

	return c.Redirect("/inbox")
}

func (h *AccountsHandler) handleSwitchMirrorAccount(c *fiber.Ctx) error {
	owner := h.mirrorOwner(c)
	if owner == "" {
		return fiber.ErrUnauthorized
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, c.Params("email"))
	if err != nil {
		return fiber.ErrNotFound
	}
	user, err := h.mailDB.GetUser(c.UserContext(), owner)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if err := h.setMirrorSession(c, sess, user.Login, account); err != nil {
		return fiber.ErrInternalServerError
	}
	if err := h.mailDB.SetDefaultAccount(c.UserContext(), owner, account.ID); err != nil {
		return fiber.ErrInternalServerError
	}
	if err := sess.Save(); err != nil {
		return fiber.ErrInternalServerError
	}
	if h.syncer != nil {
		h.syncer.StartAccount(account.ID)
		h.syncer.Trigger(account.ID)
	}
	if strings.Contains(c.Get(fiber.HeaderAccept), fiber.MIMEApplicationJSON) {
		return c.JSON(fiber.Map{"ok": true, "next": "/inbox"})
	}
	return c.Redirect("/inbox")
}

func (h *AccountsHandler) setMirrorSession(c *fiber.Ctx, sess *session.Session, username string, account mailstore.Account) error {
	token, err := api.GenerateToken(username, account.Email, h.config.JWT.Secret)
	if err != nil {
		return err
	}
	sess.Set("authenticated", true)
	sess.Set("user_id", account.OwnerID)
	sess.Set("username", username)
	sess.Set("email", account.Email)
	sess.Set("account_id", account.ID)
	sess.Set("credentials", account.EncryptedPassword)
	sess.Set("token", token)
	sess.Delete("auth_type")
	sess.Delete("oauth_token")
	return nil
}

// HandleSettings renders the settings page with the accounts panel.
func (h *AccountsHandler) HandleSettings(c *fiber.Ctx) error {
	if h.mailDB != nil {
		return h.handleMirrorSettings(c)
	}
	owner, _ := c.Locals("username").(string)
	email, _ := c.Locals("email").(string)

	// Load additional accounts for display.
	entries, err := h.acctStore.List(owner)
	if err != nil {
		log.Printf("settings: list accounts for %s: %v", owner, err)
		entries = nil
	}
	// Strip passwords.
	type safeEntry struct {
		Email      string `json:"email"`
		Label      string
		Color      string
		IMAPServer string
	}
	safe := make([]safeEntry, 0, len(entries))
	for _, e := range entries {
		safe = append(safe, safeEntry{
			Email:      e.Email,
			Label:      e.Label,
			Color:      e.Color,
			IMAPServer: e.IMAPServer,
		})
	}

	// Load token from session.
	sess, _ := h.store.Get(c)
	token, _ := sess.Get("token").(string)

	return c.JSON(fiber.Map{
		"Title":           "Settings",
		"Username":        owner,
		"Email":           email,
		"Token":           token,
		"Accounts":        safe,
		"AccountsEnabled": h.config.Accounts.Enabled,
	})
}

func (h *AccountsHandler) handleMirrorSettings(c *fiber.Ctx) error {
	owner := h.mirrorOwner(c)
	email, _ := c.Locals("email").(string)
	entries, err := h.mailDB.ListAccounts(c.UserContext(), owner)
	if err != nil {
		return c.Status(500).SendString("Failed to load mail accounts")
	}
	sess, _ := h.store.Get(c)
	token, _ := sess.Get("token").(string)
	return c.JSON(fiber.Map{
		"Title":             "Settings",
		"Username":          c.Locals("username"),
		"Email":             email,
		"Token":             token,
		"Accounts":          mirrorSafeAccounts(entries),
		"AccountsEnabled":   true,
		"MailMirrorEnabled": true,
	})
}
