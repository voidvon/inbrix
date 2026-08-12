// handlers/web/auth.go
package web

import (
	"errors"
	"fmt"
	"lilmail/config"
	"lilmail/handlers/api"
	"lilmail/mailstore"
	"lilmail/utils"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"golang.org/x/crypto/bcrypt"
)

// demoAuthType is the session auth_type value used when demo mode is active.
const demoAuthType = "demo"

type AuthHandler struct {
	store  *session.Store
	config *config.Config
	mirror *mailstore.Store
	syncer *mailstore.SyncManager
}

// NewAuthHandler creates a new instance of AuthHandler
func NewAuthHandler(store *session.Store, config *config.Config) *AuthHandler {
	return &AuthHandler{
		store:  store,
		config: config,
	}
}

// SetMailMirror wires the persistent account/mirror layer. It is optional so
// unit tests and embedded JSON API callers can keep the legacy session-only
// authentication path.
func (h *AuthHandler) SetMailMirror(mirror *mailstore.Store, syncer *mailstore.SyncManager) {
	h.mirror = mirror
	h.syncer = syncer
}

// ShowLogin renders the login page
func (h *AuthHandler) ShowLogin(c *fiber.Ctx) error {
	// The SQLite mirror is the canonical account system. Direct mailbox login
	// remains available only when the mirror is explicitly disabled, so an old
	// bookmark cannot silently send the request back to IMAP.
	if h.mirror != nil {
		return c.Redirect("/user-login")
	}
	sess, err := h.store.Get(c)
	if err == nil {
		authenticated := sess.Get("authenticated")
		if authenticated == true {
			return c.Redirect("/inbox")
		}
	}
	return Render(c, "login", fiber.Map{
		"OAuth2Enabled": h.config.OAuth2.Enabled,
	})
}

func (h *AuthHandler) ShowUserLogin(c *fiber.Ctx) error {
	return Render(c, "user-login", nil)
}

func (h *AuthHandler) ShowRegister(c *fiber.Ctx) error {
	return Render(c, "register", nil)
}

// HandleUserLogin authenticates a lilmail application user. Mailbox
// credentials are never used as the application password; the selected
// mailbox is loaded separately from the encrypted account table.
func (h *AuthHandler) HandleUserLogin(c *fiber.Ctx) error {
	if h.mirror == nil {
		return c.Status(fiber.StatusNotFound).SendString("Local user accounts are not enabled")
	}
	login := strings.TrimSpace(strings.ToLower(c.FormValue("login")))
	password := c.FormValue("password")
	user, err := h.mirror.GetUserByLogin(c.UserContext(), login)
	if err != nil || user.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return RenderStatus(c, fiber.StatusUnauthorized, "user-login", fiber.Map{"Error": "Invalid application login"})
	}
	accounts, err := h.mirror.ListAccounts(c.UserContext(), user.ID)
	if err != nil {
		return RenderStatus(c, 500, "user-login", fiber.Map{"Error": "Failed to load mail accounts"})
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return RenderStatus(c, 500, "user-login", fiber.Map{"Error": "Session error"})
	}
	email := user.Login
	sess.Delete("auth_type")
	sess.Delete("oauth_token")
	sess.Set("authenticated", true)
	sess.Set("user_id", user.ID)
	sess.Set("username", user.Login)
	if len(accounts) > 0 {
		email = accounts[0].Email
		sess.Set("account_id", accounts[0].ID)
		sess.Set("credentials", accounts[0].EncryptedPassword)
	} else {
		sess.Delete("account_id")
		sess.Delete("credentials")
	}
	token, err := api.GenerateToken(user.Login, email, h.config.JWT.Secret)
	if err != nil {
		return RenderStatus(c, 500, "user-login", fiber.Map{"Error": "Failed to create authentication token"})
	}
	sess.Set("email", email)
	sess.Set("token", token)
	sess.SetExpiry(30 * 24 * 60 * 60 * time.Second)
	if err := sess.Save(); err != nil {
		return RenderStatus(c, 500, "user-login", fiber.Map{"Error": "Failed to create session"})
	}
	if len(accounts) == 0 {
		return c.Redirect("/settings?setup=1")
	}
	if h.syncer != nil {
		for _, account := range accounts {
			h.syncer.StartAccount(account.ID)
		}
	}
	return c.Redirect("/inbox")
}

// HandleRegister creates an application user. A legacy user automatically
// created by direct mailbox login can claim the same login by setting a local
// password here; the mailbox account remains unchanged.
func (h *AuthHandler) HandleRegister(c *fiber.Ctx) error {
	if h.mirror == nil {
		return c.Status(fiber.StatusNotFound).SendString("Local user accounts are not enabled")
	}
	login := strings.TrimSpace(strings.ToLower(c.FormValue("login")))
	displayName := strings.TrimSpace(c.FormValue("display_name"))
	password := c.FormValue("password")
	confirmation := c.FormValue("password_confirm")
	if login == "" || len(password) < 8 || password != confirmation {
		return RenderStatus(c, fiber.StatusBadRequest, "register", fiber.Map{"Error": "Login and matching password of at least 8 characters are required", "Login": login, "DisplayName": displayName})
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return RenderStatus(c, 500, "register", fiber.Map{"Error": "Failed to secure application password"})
	}
	user, err := h.mirror.GetUserByLogin(c.UserContext(), login)
	if err == nil {
		if user.PasswordHash != "" {
			return RenderStatus(c, fiber.StatusConflict, "register", fiber.Map{"Error": "An application account with this login already exists", "Login": login, "DisplayName": displayName})
		}
		if err := h.mirror.SetUserPassword(c.UserContext(), user.ID, string(hash)); err != nil {
			return RenderStatus(c, 500, "register", fiber.Map{"Error": "Failed to save application account"})
		}
	} else if errors.Is(err, mailstore.ErrNotFound) {
		user, err = h.mirror.CreateUser(c.UserContext(), login, displayName, string(hash))
		if err != nil {
			return RenderStatus(c, fiber.StatusConflict, "register", fiber.Map{"Error": "Could not create application account", "Login": login, "DisplayName": displayName})
		}
	} else {
		return RenderStatus(c, 500, "register", fiber.Map{"Error": "Failed to load application account"})
	}

	sess, err := h.store.Get(c)
	if err != nil {
		return RenderStatus(c, 500, "register", fiber.Map{"Error": "Session error"})
	}
	token, err := api.GenerateToken(user.Login, user.Login, h.config.JWT.Secret)
	if err != nil {
		return RenderStatus(c, 500, "register", fiber.Map{"Error": "Failed to create authentication token"})
	}
	sess.Set("authenticated", true)
	sess.Set("user_id", user.ID)
	sess.Set("username", user.Login)
	sess.Set("email", user.Login)
	sess.Set("token", token)
	sess.SetExpiry(30 * 24 * 60 * 60 * time.Second)
	if err := sess.Save(); err != nil {
		return RenderStatus(c, 500, "register", fiber.Map{"Error": "Failed to create session"})
	}
	return c.Redirect("/settings?setup=1")
}

// HandleLogin processes the login form
func (h *AuthHandler) HandleLogin(c *fiber.Ctx) error {
	if h.mirror != nil {
		return c.Redirect("/user-login")
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return c.Status(500).SendString("Session error")
	}

	email := strings.TrimSpace(c.FormValue("email"))
	password := strings.TrimSpace(c.FormValue("password"))

	if email == "" || password == "" {
		return RenderStatus(c, 400, "login", fiber.Map{
			"Error": "Email and password are required",
			"Email": email,
		})
	}
	var username string

	if h.config.Server.UsernameIsEmail {
		username = email
	} else {
		username = api.GetUsernameFromEmail(email)
	}
	if username == "" {
		return RenderStatus(c, 400, "login", fiber.Map{
			"Error": "Invalid email format",
			"Email": email,
		})
	}

	client, err := api.NewClientTLS(
		h.config.IMAP.Server,
		h.config.IMAP.Port,
		username,
		password,
		h.config.IMAP.TLS,
	)
	if err != nil {
		return RenderStatus(c, 401, "login", fiber.Map{
			"Error": "Invalid credentials or server error",
			"Email": email,
		})
	}
	defer client.Close()

	userCacheFolder := filepath.Join(h.config.Cache.Folder, api.SanitizeUsername(username))
	if err := h.ensureUserCacheFolder(userCacheFolder); err != nil {
		return RenderStatus(c, 500, "login", fiber.Map{
			"Error": "Server error occurred during setup",
			"Email": email,
		})
	}

	token, err := api.GenerateToken(username, email, h.config.JWT.Secret)
	if err != nil {
		return RenderStatus(c, 500, "login", fiber.Map{
			"Error": "Failed to create authentication token",
			"Email": email,
		})
	}

	encryptedCreds, err := api.EncryptJSON(&api.Credentials{Email: email, Password: password}, h.config.Encryption.Key)
	if err != nil {
		return RenderStatus(c, 500, "login", fiber.Map{
			"Error": "Failed to secure credentials",
			"Email": email,
		})
	}

	var mirrorUser mailstore.User
	var mirrorAccount mailstore.Account
	if h.mirror != nil {
		mirrorPassword, passwordErr := api.EncryptJSON(password, h.config.Encryption.Key)
		if passwordErr != nil {
			log.Printf("auth: encrypt mirror password for %s: %v", email, passwordErr)
		} else {
			mirrorUser, err = h.mirror.EnsureLegacyUser(c.UserContext(), email)
			if err != nil {
				log.Printf("auth: register local mirror user for %s: %v", email, err)
			} else {
				mirrorAccount, err = h.mirror.UpsertAccount(c.UserContext(), mailstore.Account{
					OwnerID:           mirrorUser.ID,
					Email:             email,
					Username:          username,
					Label:             email,
					IMAPServer:        h.config.IMAP.Server,
					IMAPPort:          h.config.IMAP.Port,
					IMAPTLS:           h.config.IMAP.TLS,
					SMTPServer:        h.config.SMTP.Server,
					SMTPPort:          h.config.SMTP.GetPort(),
					SMTPStartTLS:      h.config.SMTP.UseSTARTTLS,
					EncryptedPassword: mirrorPassword,
					AuthType:          "password",
					IsDefault:         true,
				})
				if err != nil {
					log.Printf("auth: register mirror account for %s: %v", email, err)
				}
			}
		}
	}

	sess.Set("authenticated", true)
	sess.Set("email", email)
	sess.Set("username", username)
	sess.Set("token", token)
	sess.Set("credentials", encryptedCreds)
	if mirrorUser.ID != "" {
		sess.Set("user_id", mirrorUser.ID)
	}
	if mirrorAccount.ID != "" {
		sess.Set("account_id", mirrorAccount.ID)
	}
	sess.SetExpiry(24 * 60 * 60 * time.Second)

	if err := sess.Save(); err != nil {
		return RenderStatus(c, 500, "login", fiber.Map{
			"Error": "Failed to create session",
			"Email": email,
		})
	}

	if err := h.fetchInitialData(client, userCacheFolder); err != nil {
		log.Printf("auth: fetchInitialData for %s: %v", username, err)
	}
	if h.syncer != nil && mirrorAccount.ID != "" {
		h.syncer.StartAccount(mirrorAccount.ID)
	}

	return c.Redirect("/inbox")
}

// HandleLogout processes user logout. Must be called via POST to prevent
// CSRF-triggered forced-logout attacks on GET. For HTMX callers the handler
// returns an HX-Redirect header so the client navigates without a full-page
// reload; for regular POST callers a 302 redirect is returned.
func (h *AuthHandler) HandleLogout(c *fiber.Ctx) error {
	sess, err := h.store.Get(c)
	if err != nil {
		return c.Redirect("/login")
	}

	username := sess.Get("username")
	if username != nil {
		userStr, ok := username.(string)
		if ok {
			userCacheFolder := filepath.Join(h.config.Cache.Folder, api.SanitizeUsername(userStr))
			if err := h.clearUserCache(userCacheFolder); err != nil {
				log.Printf("auth: clearUserCache for %s: %v", userStr, err)
			}
		}
	}

	if err := sess.Destroy(); err != nil {
		return c.Status(500).SendString("Error during logout")
	}

	// HTMX callers: use the HX-Redirect response header so the client performs
	// a full-page navigation without swapping content into an element.
	if c.Get("HX-Request") != "" {
		c.Set("HX-Redirect", "/login")
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect("/login")
}

func (h *AuthHandler) ensureUserCacheFolder(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, 0700)
	}
	return nil
}

func (h *AuthHandler) clearUserCache(path string) error {
	if path == "" {
		return nil
	}

	dir, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer dir.Close()

	names, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}

	for _, name := range names {
		err = os.RemoveAll(filepath.Join(path, name))
		if err != nil {
			return err
		}
	}

	return nil
}

func (h *AuthHandler) fetchInitialData(client *api.Client, cacheFolder string) error {
	folders, err := client.FetchFolders()
	if err != nil {
		return fmt.Errorf("failed to fetch folders: %v", err)
	}
	if err := utils.SaveCache(filepath.Join(cacheFolder, "folders.json"), folders); err != nil {
		return fmt.Errorf("failed to cache folders: %v", err)
	}

	messages, err := client.FetchMessages("INBOX", 10)
	if err != nil {
		return fmt.Errorf("failed to fetch messages: %v", err)
	}

	if err := utils.SaveCache(filepath.Join(cacheFolder, "emails.json"), messages); err != nil {
		return fmt.Errorf("failed to cache messages: %v", err)
	}

	return nil
}

// CreateIMAPClient returns a MailClient for the authenticated session.
// In demo mode it returns a *api.DemoClient (no network); otherwise it opens
// a real IMAP TLS connection using the session credentials.
func (h *AuthHandler) CreateIMAPClient(c *fiber.Ctx) (api.MailClient, error) {
	// Get credentials from session
	sess, err := h.store.Get(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %v", err)
	}

	// Demo mode: return an in-memory client with seed data.
	if authType, _ := sess.Get("auth_type").(string); authType == demoAuthType {
		return api.NewDemoClient(), nil
	}

	// OAuth2 sessions authenticate with a (possibly refreshed) bearer token.
	if authType, _ := sess.Get("auth_type").(string); authType == "oauth2" {
		username, token, err := h.validOAuthToken(c)
		if err != nil {
			return nil, err
		}
		return api.NewClientOAuth(
			h.config.IMAP.Server,
			h.config.IMAP.Port,
			username,
			token,
			h.config.OAuth2.Mechanism,
		)
	}

	if h.mirror != nil {
		if accountID, _ := sess.Get("account_id").(string); accountID != "" {
			account, err := h.mirror.GetAccount(c.UserContext(), accountID)
			if err != nil {
				return nil, fmt.Errorf("failed to load mail account: %v", err)
			}
			return h.CreateIMAPClientForMirrorAccount(account)
		}
		return nil, fmt.Errorf("no active local mail account in session")
	}

	encryptedCreds := sess.Get("credentials")
	if encryptedCreds == nil {
		return nil, fmt.Errorf("no credentials found in session")
	}

	encryptedStr, ok := encryptedCreds.(string)
	if !ok {
		return nil, fmt.Errorf("invalid credentials format")
	}

	// Decrypt credentials
	var creds api.Credentials
	if err := api.DecryptJSON(encryptedStr, &creds, h.config.Encryption.Key); err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %v", err)
	}

	// Get username from email
	var username string
	if h.config.Server.UsernameIsEmail {
		username = creds.Email
	} else {
		username = api.GetUsernameFromEmail(creds.Email)
	}

	if username == "" {
		return nil, fmt.Errorf("invalid email format")
	}

	// Create new IMAP client
	return api.NewClientTLS(
		h.config.IMAP.Server,
		h.config.IMAP.Port,
		username,
		creds.Password,
		h.config.IMAP.TLS,
	)
}

// CreateIMAPClientForMirrorAccount opens a connection for an owner-scoped
// SQLite mailbox. Callers must close the returned client.
func (h *AuthHandler) CreateIMAPClientForMirrorAccount(account mailstore.Account) (api.MailClient, error) {
	if strings.EqualFold(account.AuthType, "oauth2") {
		return nil, fmt.Errorf("OAuth2 mirror accounts are not supported by background mailbox selection")
	}
	var password string
	if err := api.DecryptJSON(account.EncryptedPassword, &password, h.config.Encryption.Key); err != nil {
		return nil, fmt.Errorf("failed to decrypt account credentials: %v", err)
	}
	return api.NewClientTLS(account.IMAPServer, account.IMAPPort, account.Username, password, account.IMAPTLS)
}

// HandleDemoLogin establishes a demo session without contacting any IMAP server.
// Only available when [demo] enabled = true in config.toml.
func (h *AuthHandler) HandleDemoLogin(c *fiber.Ctx) error {
	if !h.config.Demo.Enabled {
		return c.Status(404).SendString("Demo mode is not enabled")
	}

	sess, err := h.store.Get(c)
	if err != nil {
		return c.Status(500).SendString("Session error")
	}

	email := h.config.Demo.Email
	if email == "" {
		email = "demo@lilmail.dev"
	}
	username := api.GetUsernameFromEmail(email)
	if username == "" {
		username = "demo"
	}

	// Write seed cache data so the inbox handler can load folders.json.
	demoClient := api.NewDemoClient()
	userCacheFolder := filepath.Join(h.config.Cache.Folder, api.SanitizeUsername(username))
	if err := h.ensureUserCacheFolder(userCacheFolder); err != nil {
		log.Printf("demo: ensureUserCacheFolder: %v", err)
	}
	if folders, fErr := demoClient.FetchFolders(); fErr == nil {
		if cErr := utils.SaveCache(filepath.Join(userCacheFolder, "folders.json"), folders); cErr != nil {
			log.Printf("demo: save folders cache: %v", cErr)
		}
	}

	token, err := api.GenerateToken(username, email, h.config.JWT.Secret)
	if err != nil {
		return c.Status(500).SendString("Failed to create token")
	}

	sess.Set("authenticated", true)
	sess.Set("email", email)
	sess.Set("username", username)
	sess.Set("token", token)
	sess.Set("auth_type", demoAuthType)
	sess.SetExpiry(24 * 60 * 60 * time.Second)

	if err := sess.Save(); err != nil {
		return c.Status(500).SendString("Failed to create session")
	}

	return c.Redirect("/inbox")
}

// CreateIMAPClientForAccount opens an IMAP connection for a stored additional
// account.  It decrypts the password from the AccountEntry using the
// application encryption key and derives the IMAP username exactly as the rest
// of the app does.  The caller must close the returned client.
func (h *AuthHandler) CreateIMAPClientForAccount(entry AccountEntry) (api.MailClient, error) {
	var password string
	if err := api.DecryptJSON(entry.EncryptedPassword, &password, h.config.Encryption.Key); err != nil {
		return nil, fmt.Errorf("decrypt password for %s: %w", entry.Email, err)
	}

	username := entry.Email
	if !h.config.Server.UsernameIsEmail {
		username = api.GetUsernameFromEmail(entry.Email)
	}
	if username == "" {
		return nil, fmt.Errorf("invalid email format for account %s", entry.Email)
	}

	return api.NewClientTLS(entry.IMAPServer, entry.IMAPPort, username, password, h.config.IMAP.TLS)
}

// CreateSMTPClientForAccount opens an SMTP connection for a stored additional
// account.  The caller is responsible for closing/recycling the connection.
func (h *AuthHandler) CreateSMTPClientForAccount(entry AccountEntry) (*api.SMTPClient, error) {
	var password string
	if err := api.DecryptJSON(entry.EncryptedPassword, &password, h.config.Encryption.Key); err != nil {
		return nil, fmt.Errorf("decrypt password for %s: %w", entry.Email, err)
	}

	client := api.NewSMTPClient(
		entry.SMTPServer, entry.SMTPPort,
		entry.Email, password,
		h.config.SMTP.UseSTARTTLS,
	)
	if client == nil {
		return nil, fmt.Errorf("failed to create SMTP client for %s", entry.Email)
	}
	client.SetInsecureSkipVerify(h.config.SMTP.InsecureSkipVerify)
	return client, nil
}

// CalDAVClient returns a CalDAVClient authenticated for the current session.
// For OAuth2 sessions the bearer token is retrieved (and refreshed) transparently;
// for basic-auth sessions the [caldav] config credentials are used. This is the
// single CalDAV client-construction path shared by the HTMX calendar routes and
// the JSON API (/v1/calendar).
func (h *AuthHandler) CalDAVClient(c *fiber.Ctx) (*api.CalDAVClient, error) {
	sess, err := h.store.Get(c)
	if err != nil {
		return nil, fmt.Errorf("calendar: failed to get session: %w", err)
	}

	bearerToken := ""
	if authType, _ := sess.Get("auth_type").(string); authType == "oauth2" {
		_, token, err := h.validOAuthToken(c)
		if err != nil {
			return nil, fmt.Errorf("calendar: failed to get OAuth token: %w", err)
		}
		bearerToken = token
	}

	return api.NewCalDAVClient(h.config.CalDAV, bearerToken)
}

// GetSessionEmail returns the authenticated user's email address from the
// session, or an empty string if the session is unavailable.
func (h *AuthHandler) GetSessionEmail(c *fiber.Ctx) string {
	sess, err := h.store.Get(c)
	if err != nil {
		return ""
	}
	email, _ := sess.Get("email").(string)
	return email
}

func (h *AuthHandler) CreateSMTPClient(c *fiber.Ctx) (*api.SMTPClient, error) {
	// Use the configured SMTP server (the config loader derives it from the
	// IMAP server when not explicitly set).
	smtpServer := h.config.SMTP.Server
	if smtpServer == "" {
		smtpServer = strings.Replace(h.config.IMAP.Server, "imap.", "smtp.", 1)
	}

	// Get SMTP port from config, or use default
	smtpPort := h.config.SMTP.GetPort()

	// Get credentials from session
	sess, err := h.store.Get(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %v", err)
	}

	if h.mirror != nil {
		if accountID, _ := sess.Get("account_id").(string); accountID != "" {
			account, err := h.mirror.GetAccount(c.UserContext(), accountID)
			if err != nil {
				return nil, fmt.Errorf("failed to load mail account: %v", err)
			}
			return h.CreateSMTPClientForMirrorAccount(account)
		}
		return nil, fmt.Errorf("no active local mail account in session")
	}

	// OAuth2 sessions authenticate with a (possibly refreshed) bearer token.
	if authType, _ := sess.Get("auth_type").(string); authType == "oauth2" {
		_, token, err := h.validOAuthToken(c)
		if err != nil {
			return nil, err
		}
		email, _ := sess.Get("email").(string)
		client := api.NewSMTPClientOAuth(smtpServer, smtpPort, email, token, h.config.OAuth2.Mechanism, h.config.SMTP.UseSTARTTLS)
		if client == nil {
			return nil, fmt.Errorf("failed to create SMTP client")
		}
		client.SetInsecureSkipVerify(h.config.SMTP.InsecureSkipVerify)
		return client, nil
	}

	encryptedCreds := sess.Get("credentials")
	if encryptedCreds == nil {
		return nil, fmt.Errorf("no credentials found in session")
	}

	encryptedStr, ok := encryptedCreds.(string)
	if !ok {
		return nil, fmt.Errorf("invalid credentials format")
	}

	// Decrypt credentials
	var creds api.Credentials
	if err := api.DecryptJSON(encryptedStr, &creds, h.config.Encryption.Key); err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %v", err)
	}

	client := api.NewSMTPClient(smtpServer, smtpPort, creds.Email, creds.Password, h.config.SMTP.UseSTARTTLS)
	if client == nil {
		return nil, fmt.Errorf("failed to create SMTP client")
	}
	client.SetInsecureSkipVerify(h.config.SMTP.InsecureSkipVerify)

	return client, nil
}

// CreateSMTPClientForMirrorAccount opens SMTP using a SQLite mailbox's own
// server overrides. Callers must close the returned client.
func (h *AuthHandler) CreateSMTPClientForMirrorAccount(account mailstore.Account) (*api.SMTPClient, error) {
	var password string
	if err := api.DecryptJSON(account.EncryptedPassword, &password, h.config.Encryption.Key); err != nil {
		return nil, fmt.Errorf("failed to decrypt account credentials: %v", err)
	}
	smtpServer := account.SMTPServer
	if smtpServer == "" {
		smtpServer = h.config.SMTP.Server
	}
	smtpPort := account.SMTPPort
	if smtpPort == 0 {
		smtpPort = h.config.SMTP.GetPort()
	}
	client := api.NewSMTPClient(smtpServer, smtpPort, account.Email, password, account.SMTPStartTLS)
	if client == nil {
		return nil, fmt.Errorf("failed to create SMTP client")
	}
	client.SetInsecureSkipVerify(h.config.SMTP.InsecureSkipVerify)
	return client, nil
}
