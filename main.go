package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"inbrix/config"
	"inbrix/handlers/ai"
	"inbrix/handlers/api"
	"inbrix/handlers/jsonapi"
	"inbrix/handlers/web"
	"inbrix/mailstore"
	"inbrix/storage"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "dev"

//go:embed all:assets
var assetsFS embed.FS

// frontendFS contains the production Vite bundle. The repository keeps a
// tracked dist placeholder so `go test` still compiles before npm build; the
// a frontend build is required before producing the Go binary.
//
//go:embed all:frontend/dist
var frontendFS embed.FS

// thirdPartyNotices is the generated attribution file for every third-party
// component inbrix redistributes (Go modules linked into this binary and the
// vendored JavaScript served to the browser). Their licences require the notice
// to accompany the copy, so it is embedded in the binary and served at
// /licenses.txt. Regenerate with ./scripts/gen-notices.sh.
//
//go:embed THIRD-PARTY-NOTICES.txt
var thirdPartyNotices string

var store *session.Store

// Helper function to determine if request is an API request
func isAPIRequest(c *fiber.Ctx) bool {
	if c == nil {
		return false
	}

	path := c.Path()
	return strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/v1")
}

// resolveRuntimePaths anchors relative persistent paths to the executable
// directory. This keeps a packaged binary's data beside the binary even when
// it is launched from another working directory (for example via Finder).
func resolveRuntimePaths(cfg *config.Config) {
	resolveRuntimePathsAt(cfg, runtimeBaseDir())
}

func runtimeBaseDir() string {
	if override := strings.TrimSpace(os.Getenv("INBRIX_RUNTIME_DIR")); override != "" {
		if absolute, err := filepath.Abs(override); err == nil {
			return absolute
		}
		return filepath.Clean(override)
	}
	path, err := os.Executable()
	if err != nil {
		path, _ = os.Getwd()
		return path
	}
	return filepath.Dir(path)
}

func resolveRuntimePathsAt(cfg *config.Config, baseDir string) {
	cfg.Cache.Folder = resolveRuntimePath(baseDir, cfg.Cache.Folder)
	cfg.MailSync.Database = resolveRuntimePath(baseDir, cfg.MailSync.Database)
	cfg.Notifications.VAPIDKeyFile = resolveRuntimePath(cfg.Cache.Folder, cfg.Notifications.VAPIDKeyFile)
	cfg.Accounts.StoreFile = resolveRuntimePath(cfg.Cache.Folder, cfg.Accounts.StoreFile)
}

func resolveRuntimePath(baseDir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	portOverride := flag.Int("port", 0, "HTTP listen port (overrides [server] port)")
	flag.Parse()
	if *showVersion {
		fmt.Println("inbrix", Version)
		return
	}

	// Load configuration
	config, err := config.LoadConfig("config.toml")
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}
	resolveRuntimePaths(config)
	if *portOverride > 0 {
		config.Server.Port = *portOverride
	}

	// Initialize session store now that we have the config (CookieSecure needs it).
	{
		fileStorage, err := storage.NewFileStorage(filepath.Join(config.Cache.Folder, "sessions"))
		if err != nil {
			log.Fatal("Failed to initialize session storage:", err)
		}
		store = session.New(session.Config{
			Storage:        fileStorage,
			Expiration:     24 * time.Hour,
			CookieSecure:   config.Server.SecureCookies, // true in TLS-terminated deployments
			CookieHTTPOnly: true,
			CookieSameSite: "Lax", // Prevents CSRF via cross-site form submissions
		})
	}

	// Initialize Fiber as an API server plus static React host.
	app := fiber.New(fiber.Config{
		BodyLimit: 25 * 1024 * 1024, // 25 MiB — guards compose form uploads
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}

			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		},
	})

	// Add middleware
	app.Use(recover.New()) // Recover from panics
	app.Use(logger.New())  // Request logging

	// Apply security headers on every response.
	app.Use(func(c *fiber.Ctx) error {
		for h, v := range config.GetSecurityHeaders() {
			c.Set(h, v)
		}
		return c.Next()
	})

	// Serve embedded assets
	assetsSub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		log.Fatal("Failed to sub assets FS:", err)
	}
	app.Use("/assets", filesystem.New(filesystem.Config{
		Root:         http.FS(assetsSub),
		MaxAge:       int(24 * time.Hour / time.Second),
		NotFoundFile: "",
	}))

	frontendSub, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Fatal("Failed to sub frontend FS:", err)
	}
	app.Use("/app", filesystem.New(filesystem.Config{
		Root:         http.FS(frontendSub),
		MaxAge:       int(24 * time.Hour / time.Second),
		NotFoundFile: "",
	}))
	serveSPA := func(c *fiber.Ctx) error {
		index, readErr := frontendFS.ReadFile("frontend/dist/index.html")
		if readErr != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "frontend bundle is not built"})
		}
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		c.Set(fiber.HeaderCacheControl, "no-cache")
		return c.Send(index)
	}

	// Service worker must be served at the root scope (not under /assets/)
	// so that it can intercept fetch requests and show push notifications for
	// the entire origin.  Served with Cache-Control: no-cache so the browser
	// always picks up updates promptly.
	app.Get("/sw.js", func(c *fiber.Ctx) error {
		swBytes, readErr := assetsFS.ReadFile("assets/sw.js")
		if readErr != nil {
			return fiber.ErrNotFound
		}
		c.Set("Content-Type", "application/javascript; charset=utf-8")
		c.Set("Cache-Control", "no-cache")
		c.Set("Service-Worker-Allowed", "/") // Allow SW to control the full origin.
		return c.Send(swBytes)
	})

	// Third-party notices. inbrix redistributes MIT/BSD/ISC/Apache-2.0 code
	// (Go modules compiled into this binary, plus the vendored JS served to the
	// browser); those licences require their copyright notice and licence text
	// to travel with every copy. Served unauthenticated so any user of a running
	// inbrix can read them. Regenerate with ./scripts/gen-notices.sh.
	app.Get("/licenses.txt", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; charset=utf-8")
		c.Set("Cache-Control", "public, max-age=3600")
		return c.SendString(thirdPartyNotices)
	})

	// Initialize web handlers
	var mailMirror *mailstore.Store
	var mailSync *mailstore.SyncManager
	if config.MailSync.Enabled {
		mailMirror, err = mailstore.Open(config.MailSync.Database)
		if err != nil {
			log.Fatal("Failed to initialize mail mirror:", err)
		}
		mailSync = mailstore.NewSyncManager(mailMirror, config.Encryption.Key, config.MailSync)
		if accounts, listErr := mailMirror.ListAllAccounts(context.Background()); listErr != nil {
			log.Printf("mail sync: could not restore accounts: %v", listErr)
		} else {
			mailSync.StartAll(accounts)
		}
		// Stop workers before closing SQLite so an in-flight sync cannot
		// write through a closed database handle during shutdown.
		defer mailMirror.Close()
		defer mailSync.Stop()
	}
	webAuthHandler := web.NewAuthHandler(store, config)
	webAuthHandler.SetMailMirror(mailMirror, mailSync)
	webEmailHandler := web.NewEmailHandler(store, config, webAuthHandler)
	webEmailHandler.SetMailMirror(mailMirror)
	var systemSettings *web.SystemSettingsHandler
	if mailMirror != nil {
		systemSettings = web.NewSystemSettingsHandler(mailMirror, Version)
	}

	// Browser mutations use the double-submit cookie pattern. The same handler
	// is composed into /v1 below; its Next hook skips requests that the JSON API's
	// broker middleware has already authenticated with the shared broker secret.
	csrfMiddleware := csrf.New(csrf.Config{
		KeyLookup:      "header:X-CSRF-Token",
		CookieName:     "_csrf",
		CookieHTTPOnly: false, // React reads the token to send the matching header.
		CookieSameSite: "Lax",
		CookieSecure:   config.Server.SecureCookies,
		Expiration:     24 * time.Hour,
		Next:           jsonapi.IsBrokerRequest,
		ContextKey:     "csrfToken",
	})

	// JSON/REST API (/v1/*) — machine-readable contract used by the React client
	// and other rich clients. It reuses the same engine and session auth, and
	// returns 401 JSON instead of redirecting.
	//
	// A durable KV store (bbolt by default, Postgres when configured) backs
	// scheduled send (send-later): the store persists pending scheduled sends and
	// a poll-based drain delivers them at their sendAt with restart catch-up. If
	// the store cannot be opened we log and fall back to the storeless handler, so
	// the rest of the API keeps working (only send-later is unavailable).
	// The cache folder is otherwise created on first login, which is too late:
	// this store is opened once, here, so on a fresh install bbolt failed on the
	// missing directory and send-later stayed unavailable for the life of the
	// process — until an operator happened to restart after logging in once.
	if err := os.MkdirAll(config.Cache.Folder, 0o700); err != nil {
		log.Printf("cache folder %q could not be created: %v", config.Cache.Folder, err)
	}
	scheduleDBPath := filepath.Join(config.Cache.Folder, "scheduled.db")
	if kv, kvErr := storage.Open(config, scheduleDBPath); kvErr != nil {
		log.Printf("scheduled send unavailable (store open failed): %v", kvErr)
		jsonapi.New(store, config, webAuthHandler).RegisterWithMiddleware(app, csrfMiddleware)
	} else {
		jsonAPI := jsonapi.NewWithStore(store, config, webAuthHandler, kv)
		jsonAPI.RegisterWithMiddleware(app, csrfMiddleware)
		defer jsonAPI.StopScheduler()
		defer kv.Close()
	}

	// Rate limiters — applied to the three highest-risk surfaces.
	// Login: tight (brute-force). Send + AI: moderate (abuse/cost).
	// All limits are configurable via [rate_limit] in config.toml.
	loginLimiter := limiter.New(limiter.Config{
		Max:        config.RateLimit.LoginMax,
		Expiration: time.Duration(config.RateLimit.LoginWindow) * time.Second,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "Too many login attempts. Please wait and try again."})
		},
	})

	// React owns every browser page. The shell is public; data endpoints remain
	// session-gated and return JSON when the user is not authenticated.
	for _, path := range []string{"/", "/login", "/user-login", "/register", "/inbox", "/attachments", "/settings", "/calendar", "/calendar/week"} {
		app.Get(path, csrfMiddleware, serveSPA)
	}
	app.Get("/csrf", csrfMiddleware, func(c *fiber.Ctx) error {
		token, _ := c.Locals("csrfToken").(string)
		return c.JSON(fiber.Map{"token": token})
	})
	if systemSettings != nil {
		app.Get("/api/public/settings", systemSettings.HandleRegistrationStatus)
	}
	app.Get("/folder/*", csrfMiddleware, serveSPA)
	app.Post("/login", loginLimiter, csrfMiddleware, webAuthHandler.HandleLogin)
	app.Post("/user-login", loginLimiter, csrfMiddleware, webAuthHandler.HandleUserLogin)
	app.Post("/register", csrfMiddleware, webAuthHandler.HandleRegister)
	app.Get("/language", web.HandleLanguage)

	// Demo / screenshot mode — registered only when [demo] enabled = true.
	// Both GET and POST /demo-login immediately establish a demo session
	// (no IMAP contact) and redirect to /inbox. This lets Playwright simply
	// navigate to /demo-login and follow the redirect.
	if config.Demo.Enabled {
		app.Get("/demo-login", webAuthHandler.HandleDemoLogin)
		app.Post("/demo-login", webAuthHandler.HandleDemoLogin)
	}

	// OAuth2 login routes (public; the callback establishes the session)
	if config.OAuth2.Enabled {
		app.Get("/auth/oauth/login", webAuthHandler.HandleOAuthLogin)
		app.Get("/auth/oauth/callback", webAuthHandler.HandleOAuthCallback)
	}

	// Health check endpoint — MUST be registered BEFORE the protected group.
	// The protected group uses an empty prefix (app.Group("", SessionMiddleware)),
	// so its middleware attaches to every route registered AFTER it. Registering
	// /health here keeps it public (no 302 to /login) for liveness probes.
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	// CSRF middleware for all web (cookie-session) protected routes.
	// Uses the double-submit cookie pattern:
	//   1. Middleware sets a JS-readable "_csrf" cookie on GET responses.
	//   2. React's API client reads the cookie and sends its value as
	//      "X-CSRF-Token" on every mutating request.
	//   3. Middleware validates header == cookie value before the handler runs.
	// WebPush subscribe/unsubscribe routes use the same cookie session as all
	// other browser mutations and are covered by this middleware as well.
	//
	// CORRECTION (found while typechecking assets/js/push.js, 2026-08-06): the
	// skip comment used to justify this as "they carry Authorization: Bearer
	// headers which already prevent cross-origin CSRF". That is not what
	// happens — handlers/web/push.go's HandleSubscribe/HandleUnsubscribe never
	// read the Authorization header at all; they authorize purely from
	// c.Locals("username"), set below by SessionMiddleware from the session
	// cookie. The Authorization: Bearer header push.js sends on these routes
	// is therefore inert on the server. What actually keeps these two routes
	// safe from cross-origin CSRF today is CookieSameSite: "Lax" below — a
	// forged cross-site POST/DELETE does not carry the session cookie at all,
	// so SessionMiddleware has nothing to authorize — the same protection
	// every other protected route relies on. This exemption is not currently
	// exploitable, but it rests on an inaccurate premise and should be
	// resolved deliberately (either validate the Bearer token server-side to
	// match the comment's original intent, or drop the exemption and the now-
	// pointless header) rather than left as accidentally-correct.
	// When the SQLite mirror is enabled it is the canonical account system. Do
	// not let an old direct-mailbox session fall through to the legacy IMAP
	// handlers: that makes the inbox look slow and defeats the local mirror.
	// Settings and account-management routes remain available without an active
	// mailbox so a newly registered user can attach the first one.
	mirrorSessionMiddleware := func(c *fiber.Ctx) error {
		if mailMirror == nil || c.Path() == "/logout" {
			return c.Next()
		}
		sess, err := store.Get(c)
		if err != nil {
			if isAPIRequest(c) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Application account required"})
			}
			return c.Redirect("/user-login")
		}
		ownerID, _ := sess.Get("user_id").(string)
		if ownerID == "" {
			if isAPIRequest(c) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Application account required"})
			}
			return c.Redirect("/user-login")
		}
		accountID, _ := sess.Get("account_id").(string)
		optionalMailboxPath := c.Path() == "/settings" || c.Path() == "/api/capabilities" || strings.HasPrefix(c.Path(), "/api/accounts") || strings.HasPrefix(c.Path(), "/api/settings/") || strings.HasPrefix(c.Path(), "/api/system/")
		if accountID == "" {
			if optionalMailboxPath {
				return c.Next()
			}
			if isAPIRequest(c) {
				return c.Status(fiber.StatusPreconditionRequired).JSON(fiber.Map{"error": "mailbox setup required"})
			}
			return c.Redirect("/settings?setup=1")
		}
		account, accountErr := mailMirror.GetAccount(c.UserContext(), accountID)
		if accountErr != nil || account.OwnerID != ownerID {
			sess.Delete("account_id")
			sess.Delete("credentials")
			_ = sess.Save()
			if optionalMailboxPath {
				return c.Next()
			}
			if isAPIRequest(c) {
				return c.Status(fiber.StatusPreconditionRequired).JSON(fiber.Map{"error": "mailbox setup required"})
			}
			return c.Redirect("/settings?setup=1")
		}
		return c.Next()
	}

	// Protected routes group — session-gated, mirror-session-gated, and CSRF-protected.
	protected := app.Group("", api.SessionMiddleware(store), mirrorSessionMiddleware, csrfMiddleware)

	// Logout MUST be POST to prevent forced-logout via a crafted GET link
	// (CSRF attack). Placed in the protected group so CSRF middleware covers it.
	protected.Post("/logout", webAuthHandler.HandleLogout)

	// JSON endpoints used by the React client during the /v1 migration.
	apiRoutes := protected.Group("/api")
	{
		apiRoutes.Get("/capabilities", func(c *fiber.Ctx) error {
			role := mailstore.RoleUser
			if mailMirror != nil {
				userID, _ := c.Locals("user_id").(string)
				if user, userErr := mailMirror.GetUser(c.UserContext(), userID); userErr == nil {
					role = user.Role
				}
			}
			return c.JSON(fiber.Map{
				"notifications": config.Notifications.Enabled,
				"webPush":       config.Notifications.Enabled && config.Notifications.WebPush,
				"calendar":      config.CalDAV.Enabled,
				"role":          role,
			})
		})
		apiRoutes.Get("/csrf", func(c *fiber.Ctx) error {
			token, _ := c.Locals("csrfToken").(string)
			return c.JSON(fiber.Map{"token": token})
		})
		apiRoutes.Get("/conversations", webEmailHandler.HandleConversationListJSON)
		apiRoutes.Get("/conversations/search", webEmailHandler.HandleConversationListJSON)
		apiRoutes.Get("/conversations/:id", webEmailHandler.HandleConversationViewJSON)
		apiRoutes.Get("/mail/folders", webEmailHandler.HandleLocalFoldersJSON)
		apiRoutes.Get("/mail/messages", webEmailHandler.HandleLocalFolderMessagesJSON)
		apiRoutes.Get("/mail/messages/:uid", webEmailHandler.HandleLocalFolderMessageJSON)
		apiRoutes.Patch("/mail/messages/:uid/read", webEmailHandler.HandleLocalFolderMessageReadJSON)
		apiRoutes.Get("/attachments", webEmailHandler.HandleLocalAttachmentsJSON)
		apiRoutes.Post("/mail/messages/:uid/not-spam", webEmailHandler.HandleLocalJunkMessageRestoreJSON)
		apiRoutes.Delete("/mail/messages/:uid", webEmailHandler.HandleLocalJunkMessageDeleteJSON)
		apiRoutes.Put("/conversations/:id/note", webEmailHandler.HandleConversationNoteJSON)
		apiRoutes.Patch("/conversations/:id/read", webEmailHandler.HandleConversationReadJSON)
		apiRoutes.Patch("/conversations/:id/unread", webEmailHandler.HandleConversationUnreadJSON)
		apiRoutes.Delete("/conversations/:id", webEmailHandler.HandleConversationDeleteJSON)
		apiRoutes.Delete("/conversations/:id/messages/:uid", webEmailHandler.HandleConversationMessageDeleteJSON)

		// Attachment download (ID encodes folder + UID + MIME part)
		apiRoutes.Get("/attachment/:id", webEmailHandler.HandleAttachment)

		apiRoutes.Post("/compose", webEmailHandler.HandleComposeEmail)
		if systemSettings != nil {
			systemRoutes := apiRoutes.Group("/system", systemSettings.RequireSuperAdmin)
			systemRoutes.Get("/settings", systemSettings.HandleGet)
			systemRoutes.Patch("/settings/registration", systemSettings.HandleUpdateRegistration)
			systemRoutes.Patch("/users/:id/role", systemSettings.HandleUpdateUserRole)
		}
	}

	// AI mail-assistant routes — registered always (gated internally on config.AI.Enabled).
	// When disabled, all /api/ai/* routes return 404 {"error":"ai_disabled"}.
	// Rate-limit AI routes before registration to guard against compute-cost abuse
	// and brute-force of the AI endpoint (limits configurable via [rate_limit]).
	aiLimiter := limiter.New(limiter.Config{
		Max:        config.RateLimit.AIMax,
		Expiration: time.Duration(config.RateLimit.AIWindow) * time.Second,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "rate limit exceeded",
			})
		},
	})
	apiRoutes.Use("/ai", aiLimiter)
	userAIHandler := web.NewAISettingsHandler(store, config, mailMirror)
	protected.Get("/api/settings/ai/models", userAIHandler.HandleListModels)
	protected.Post("/api/settings/ai/models", userAIHandler.HandleCreateModel)
	protected.Post("/api/settings/ai/models/test", userAIHandler.HandleTestModel)
	protected.Post("/api/settings/ai/models/:id/test", userAIHandler.HandleTestSavedModel)
	protected.Put("/api/settings/ai/models/:id", userAIHandler.HandleUpdateModel)
	protected.Delete("/api/settings/ai/models/:id", userAIHandler.HandleDeleteModel)
	protected.Post("/api/settings/ai/models/:id/default", userAIHandler.HandleSetDefaultModel)
	protected.Get("/api/settings/ai/agents", userAIHandler.HandleListAgents)
	protected.Post("/api/settings/ai/agents", userAIHandler.HandleCreateAgent)
	protected.Put("/api/settings/ai/agents/:id", userAIHandler.HandleUpdateAgent)
	protected.Delete("/api/settings/ai/agents/:id", userAIHandler.HandleDeleteAgent)
	protected.Get("/api/settings/ai/task-bindings", userAIHandler.HandleListTaskBindings)
	protected.Put("/api/settings/ai/task-bindings", userAIHandler.HandleSaveTaskBinding)
	apiRoutes.Post("/ai/summary", userAIHandler.HandleSummarize)
	apiRoutes.Post("/ai/mail-summary", userAIHandler.HandleSummarizeMail)
	apiRoutes.Post("/ai/write-email", userAIHandler.HandleWriteEmail)
	// Build the completion backend before registering. With [ai] enabled = false
	// this builds nothing at all; with mode = "embedded" it constructs the
	// in-process llmux gateway and fails startup on a configuration that could
	// not serve (no providers, an unroutable model, an unreadable llmux config)
	// rather than surfacing it as a 502 on the first AI request.
	aiHandler, aiErr := ai.NewHandler(config.AI)
	if aiErr != nil {
		// The error already carries an "ai:" prefix; don't double it.
		log.Fatal(aiErr)
	}
	defer aiHandler.Close()
	aiHandler.Register(apiRoutes)

	// Notifications routes — registered only when notifications.enabled = true.
	// With enabled = false (the default) this block is never entered, so no
	// extra goroutines are created and no new routes appear.
	if config.Notifications.Enabled {
		// Optional VAPID Web Push.
		var vapidKeys *web.VAPIDKeys
		var pushStore *web.PushStore
		if config.Notifications.WebPush {
			var err error
			vapidKeys, err = web.LoadOrGenerateVAPIDKeys(config.Notifications.VAPIDKeyFile)
			if err != nil {
				log.Printf("webpush: VAPID key init failed (%v) — web push disabled", err)
			} else {
				log.Printf("webpush: VAPID public key loaded (%s)", config.Notifications.VAPIDKeyFile)
				cacheRoot := config.Cache.Folder
				if cacheRoot == "" {
					cacheRoot = "."
				}
				pushStore = web.NewPushStore(cacheRoot)
			}
		}

		hub := web.NewNotificationHub(store, config, webAuthHandler, vapidKeys, pushStore)
		notifHandler := web.NewNotificationsHandler(hub)
		protected.Get("/events", notifHandler.HandleSSE)

		// VAPID public key endpoint — public (no session required) so the SW can
		// fetch it before the user navigates to an authenticated page.
		if vapidKeys != nil {
			pushHandler := web.NewPushHandler(vapidKeys, pushStore)
			app.Get("/api/push/vapid-public", pushHandler.HandleVAPIDPublicKey)
			protected.Post("/api/push/subscribe", pushHandler.HandleSubscribe)
			protected.Delete("/api/push/subscribe", pushHandler.HandleUnsubscribe)
		}
	}

	// Multi-account routes — registered only when accounts.enabled = true.
	var acctHandler *web.AccountsHandler
	if config.Accounts.Enabled && mailMirror == nil {
		acctStore, err := web.OpenAccountStore(config.Accounts.StoreFile)
		if err != nil {
			log.Fatalf("accounts: open store: %v", err)
		}
		// Wire the account store into the email handler so unified-inbox fetches work.
		webEmailHandler.SetAccountStore(acctStore)

		acctHandler = web.NewAccountsHandler(store, config, webAuthHandler, acctStore)

		protected.Get("/api/accounts", acctHandler.HandleListAccounts)
		protected.Post("/api/accounts", acctHandler.HandleAddAccount)
		protected.Post("/api/accounts/resync-attachments", acctHandler.HandleResyncAttachments)
		protected.Get("/api/settings/feishu-webhook", acctHandler.HandleGetWebhookSettings)
		protected.Put("/api/settings/feishu-webhook", acctHandler.HandlePutWebhookSettings)
		protected.Post("/api/settings/feishu-webhook/test", acctHandler.HandleTestWebhook)
		protected.Delete("/api/accounts/:email", acctHandler.HandleDeleteAccount)
		protected.Post("/api/accounts/:email/switch", acctHandler.HandleSwitchAccount)
	}
	if mailMirror != nil {
		if acctHandler == nil {
			acctHandler = web.NewAccountsHandler(store, config, webAuthHandler, nil)
		}
		acctHandler.SetMailMirror(mailMirror, mailSync)
		protected.Get("/api/accounts", acctHandler.HandleListAccounts)
		protected.Post("/api/accounts", acctHandler.HandleAddAccount)
		protected.Post("/api/accounts/resync-attachments", acctHandler.HandleResyncAttachments)
		protected.Get("/api/settings/feishu-webhook", acctHandler.HandleGetWebhookSettings)
		protected.Put("/api/settings/feishu-webhook", acctHandler.HandlePutWebhookSettings)
		protected.Post("/api/settings/feishu-webhook/test", acctHandler.HandleTestWebhook)
		protected.Delete("/api/accounts/:email", acctHandler.HandleDeleteAccount)
		protected.Post("/api/accounts/:email/switch", acctHandler.HandleSwitchAccount)
	}
	// 404 Handler for undefined routes
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(404).JSON(fiber.Map{"error": "Not Found"})
	})

	// Start server
	log.Printf("Starting server on port %d...\n", config.Server.Port)
	if err := app.Listen(fmt.Sprintf(":%d", config.Server.Port)); err != nil {
		log.Fatal("Error starting server: ", err)
	}
}
