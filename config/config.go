package config

import (
	"crypto/tls"
	"fmt"
	"log"
	filepathpkg "path/filepath"

	"github.com/BurntSushi/toml"
)

type ServerConfig struct {
	Port            int  `toml:"port"`
	UsernameIsEmail bool `toml:"username_is_email"`
	// FrameAncestors, when set, allows Inbrix AI to be embedded as an iframe by the
	// listed origins (space-separated, CSP frame-ancestors syntax). This is what
	// lets a host shell such as Vulos OS embed Inbrix AI as its built-in Mail app.
	// When empty, the default same-origin-only framing policy applies.
	FrameAncestors string `toml:"frame_ancestors"`
	// SecureCookies sets the Secure flag on the session cookie. Enable this when
	// Inbrix AI is served over HTTPS (either directly via [ssl] or behind a TLS
	// reverse proxy). Defaults to false so plain-HTTP local dev works out of the
	// box; set to true in any internet-facing deployment.
	SecureCookies bool `toml:"secure_cookies"`
}

// AuthConfig holds login/authentication preferences that are independent of the
// transport ([imap]/[smtp]) and identity-provider ([oauth2]) sections.
//
//	[auth]
//	allow_full_email_username = true
//
// AllowFullEmailUsername controls what string Inbrix AI passes as the SASL/LOGIN
// username to the IMAP and SMTP servers:
//
//   - true  → the full email address (e.g. "alice@example.com") is sent verbatim.
//     This is what most hosted providers (Gmail, Fastmail, Migadu, …) expect.
//   - false → only the local part before the "@" (e.g. "alice") is sent. Some
//     self-hosted Dovecot/Postfix setups authenticate against the bare handle.
//
// It is a pointer so the loader can tell "explicitly set in [auth]" apart from
// "absent". When absent, the legacy [server] username_is_email key governs (and
// the two are kept in sync). When present, [auth] takes precedence.
type AuthConfig struct {
	AllowFullEmailUsername *bool `toml:"allow_full_email_username"`
}

type IMAPConfig struct {
	Server string `toml:"server"`
	Port   int    `toml:"port"`
	// TLS selects an implicit-TLS (imaps) connection on connect. Defaults to
	// true, preserving the previous always-TLS behaviour; set `tls = false` for
	// a plain-IMAP server (e.g. port 143) that does not speak TLS on connect.
	// Fixes #8 — the field was shown in the config docs/example but never parsed,
	// so plain IMAP always failed with
	// "tls: first record does not look like a TLS handshake".
	TLS bool `toml:"tls"`
}

type SMTPConfig struct {
	Server             string `toml:"server"`
	Port               int    `toml:"port"`
	UseSTARTTLS        bool   `toml:"use_starttls"`         // true for port 587, false for port 465
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"` // allow self-signed certs; default false
}

type JWTConfig struct {
	Secret string `toml:"secret"` // For JWT signing
}

// OAuth2Config configures OAuth2 / OpenID Connect login for IMAP and SMTP.
// The same access token is presented to the mail server using either the
// XOAUTH2 or the OAUTHBEARER SASL mechanism.
type OAuth2Config struct {
	Enabled      bool     `toml:"enabled"`
	ClientID     string   `toml:"client_id"`
	ClientSecret string   `toml:"client_secret"`
	AuthURL      string   `toml:"auth_url"`     // Authorization endpoint
	TokenURL     string   `toml:"token_url"`    // Token endpoint
	UserInfoURL  string   `toml:"userinfo_url"` // Optional; used to look up the email
	RedirectURL  string   `toml:"redirect_url"` // Must match the registered redirect URI
	Scopes       []string `toml:"scopes"`
	Mechanism    string   `toml:"mechanism"`   // "xoauth2" (default) or "oauthbearer"
	EmailClaim   string   `toml:"email_claim"` // Claim holding the email (default "email")
	UsePKCE      bool     `toml:"use_pkce"`    // Use PKCE (S256); recommended
}

type CacheConfig struct {
	Folder string `toml:"folder"`
}

// MailSyncConfig controls the local SQLite mail mirror. The mirror stores
// message metadata and full MIME bodies during background sync by default, so
// normal reads do not need to contact IMAP. Set SyncBodies to false to opt out
// of background body caching.
//
//	[mail_sync]
//	enabled                 = true
//	database                = "./cache/mail.db"
//	interval                = 60
//	batch_size              = 200
//	max_messages_per_folder = 5000
//	sync_bodies             = true
type MailSyncConfig struct {
	Enabled              bool   `toml:"enabled"`
	Database             string `toml:"database"`
	Interval             int    `toml:"interval"`                // seconds
	BatchSize            int    `toml:"batch_size"`              // IMAP page size
	MaxMessagesPerFolder int    `toml:"max_messages_per_folder"` // 0 = all
	SyncBodies           bool   `toml:"sync_bodies"`
}

// StorageConfig selects the durable key-value backend used for caches and
// shared state (thread metadata, recipients, push subscriptions, …).
//
// The default standalone path is "sqlite", stored in the mail mirror database.
// Set backend =
// "postgres" (with a DSN) when several inbrix/Vulos instances must share one
// store, or when another Vulos service wants to read the same data. Postgres is
// strictly opt-in; it is never the default.
type StorageConfig struct {
	Backend     string `toml:"backend"`      // "sqlite" (default) | "postgres"
	PostgresDSN string `toml:"postgres_dsn"` // e.g. postgres://user:pw@host:5432/db?sslmode=require
}

type EncryptionConfig struct {
	Key string `toml:"key"` // 32-byte key for AES encryption
}

// SSLConfig does NOT make Inbrix AI serve HTTPS. There is one listener and it is
// plain HTTP on [server] port (app.Listen in main.go). Enabling this section
// does exactly two things: ValidateSSL checks that cert_file and key_file load
// as an X.509 pair (fatal at startup if they do not), and — when Domain is also
// set — GetSecurityHeaders emits Strict-Transport-Security. Terminate TLS in a
// reverse proxy.
//
// The section once also carried `port`, `http_port` and `auto_redirect`. They
// were only ever defaulted, never read: nothing listened on them and no
// redirect listener existed. They have been removed rather than left promising
// control they never had. Existing config files that still set them keep
// loading — toml.DecodeFile below discards the MetaData and so ignores unknown
// keys (TestLoadConfig_UnknownSSLKeysAreIgnored proves it).
type SSLConfig struct {
	Enabled    bool   `toml:"enabled"`
	CertFile   string `toml:"cert_file"`    // Path to fullchain.pem
	KeyFile    string `toml:"key_file"`     // Path to privkey.pem
	Domain     string `toml:"domain"`       // Domain name for HSTS; required for the header to be sent
	HSTSMaxAge int    `toml:"hsts_max_age"` // Max age for HSTS in seconds
}

// CalDAVConfig configures the optional CalDAV calendar integration.
// Set [caldav] enabled = true in config.toml to activate the calendar routes.
type CalDAVConfig struct {
	Enabled  bool   `toml:"enabled"`
	URL      string `toml:"url"`      // CalDAV endpoint / principal or discovery URL
	Auth     string `toml:"auth"`     // "basic" (default) or "oauth2"
	Username string `toml:"username"` // used when auth = "basic"
	Password string `toml:"password"` // used when auth = "basic"
}

// CardDAVContactsConfig configures an optional CardDAV address book used for
// recipient autocomplete in the compose modal. When Enabled is false (the
// default), autocomplete uses only the recent-recipients SQLite store.
//
//	[carddav]
//	enabled  = true
//	url      = "https://dav.example.com"
//	username = "alice@example.com"
//	password = "secret"
type CardDAVContactsConfig struct {
	Enabled  bool   `toml:"enabled"`
	URL      string `toml:"url"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

// AI completion modes. See AIConfig.Mode.
const (
	// AIModeRemote POSTs to a configurable OpenAI-compatible endpoint. Default.
	AIModeRemote = "remote"
	// AIModeEmbedded runs llmux in-process as a Go library — no gateway hop.
	AIModeEmbedded = "embedded"
)

// AIConfig configures the mail-AI assistant endpoints.
//
// Inbrix AI performs no inference of its own. There are two ways to get
// completions, selected by `mode`:
//
//   - mode = "remote" (default) — forward to a configurable OpenAI-compatible
//     SSE chat endpoint (just a base URL + Bearer token), so Inbrix AI has no
//     hard dependency on any particular gateway:
//
//   - Standalone / BYO: point at any OpenAI-compatible SSE chat endpoint
//     (the provider directly, the Vulos OS airouter's /api/ai/chat, etc.).
//
//   - Vulos suite: point at the central llmux gateway's
//     /v1/chat/completions endpoint. llmux resolves the forwarded Bearer
//     token to an account and applies BYOK-vs-central key selection
//     on the account's behalf — Inbrix AI itself does not
//     decide BYOK vs central, it only forwards the account's token.
//
//     [ai]
//     enabled        = true
//     mode           = "remote"
//     endpoint       = "http://llmux:4000/v1/chat/completions"  # or airouter /api/ai/chat
//     api_key        = ""      # static Bearer token (e.g. an llmux virtual key) for standalone
//     account_header = ""      # inbound request header whose value is forwarded as the Bearer (suite)
//     model          = ""      # forwarded to the endpoint; leave empty to use endpoint default
//
//   - mode = "embedded" — run llmux (github.com/vul-os/llmux) as an in-process
//     library. No gateway to deploy and no completion hop leaves the machine
//     unless llmux's own provider config says so; llmux does the routing,
//     failover, sovereignty enforcement and BYOK inside Inbrix AI's process:
//
//     [ai]
//     enabled      = true
//     mode         = "embedded"
//     model        = "llama3.1"                # REQUIRED: embedded llmux has no default model
//     llmux_config = "/etc/inbrix/llmux.json" # llmux's own JSON config (providers/routes/keys)
//     llmux_cache  = false                     # opt in to llmux's in-memory response cache
type AIConfig struct {
	// Enabled is the master switch. When false, all /api/ai/* routes return
	// 404 {"error":"ai_disabled"}, no completion backend is constructed at all
	// (in particular no embedded llmux gateway exists), and the feature can
	// emit no packets. Default: false (opt-in).
	Enabled bool `toml:"enabled"`

	// Mode selects the completion backend: "remote" (default) or "embedded".
	// An empty value means "remote", so existing configs keep working verbatim.
	Mode string `toml:"mode"`

	// Endpoint is the URL of the OpenAI-compatible SSE chat-completion API
	// (mode = "remote" only; ignored in embedded mode). Defaults to the Vulos
	// OS airouter URL so Inbrix AI works out of the box when embedded in Vulos;
	// set it to llmux's /v1/chat/completions to route through the central
	// gateway, or to any OpenAI-compatible endpoint for standalone / BYO use.
	Endpoint string `toml:"endpoint"`

	// APIKey is the static Bearer token sent as "Authorization: Bearer <key>"
	// when no per-request account token is available (see AccountHeader).
	// For llmux this is typically a standalone virtual key. Leave empty when
	// calling the local Vulos airouter (it uses session auth).
	APIKey string `toml:"api_key"`

	// AccountHeader names an inbound HTTP request header that carries the
	// caller's account token (e.g. one injected by the host shell when Inbrix AI
	// is embedded in the Vulos suite). When set and present on the incoming
	// request, its value is forwarded as the "Authorization: Bearer <token>"
	// to the completion endpoint, so a central gateway such as llmux can
	// resolve it to an account and apply BYOK-vs-central + metering. When the
	// header is empty or absent, Inbrix AI falls back to the static APIKey.
	// Leave empty for standalone deployments.
	AccountHeader string `toml:"account_header"`

	// Model is the model slug forwarded to the completion endpoint.
	// Leave empty to use the endpoint's configured default.
	//
	// In embedded mode it is REQUIRED and must be routable by the embedded
	// llmux config: llmux rejects a request with no model, so an empty value
	// would fail every AI call at runtime. Startup fails loudly instead.
	Model string `toml:"model"`

	// LLMuxConfig is the path to llmux's own JSON configuration file
	// (providers, routes, virtual keys, BYOK — llmux's schema, not Inbrix AI's),
	// used only in embedded mode. Empty means llmux's built-in defaults plus
	// its environment auto-detection (OLLAMA_HOST, OPENAI_API_KEY, ...).
	//
	// Whatever the file says, Inbrix AI overrides four things when it builds the
	// embedded gateway, because a mail client must not host them:
	// no listener, no price-feed sync (no outbound calls of its own), no
	// Postgres/Redis (the two things that would open sockets at construction),
	// and no response cache unless LLMuxCache is set.
	LLMuxConfig string `toml:"llmux_config"`

	// LLMuxCache opts the embedded gateway into llmux's response cache: an
	// in-memory, TTL-bounded, size-bounded LRU keyed by a SHA-256 of the
	// request. It is OFF by default because it would retain model output
	// derived from your mail in process memory after the request finishes.
	// It is never written to disk on this path (llmux's Redis cache backend
	// is unreachable here — see LLMuxConfig).
	LLMuxCache bool `toml:"llmux_cache"`
}

// EmbeddedAI reports whether the AI assistant runs llmux in-process.
func (c AIConfig) EmbeddedAI() bool { return c.Mode == AIModeEmbedded }

// NotificationsConfig configures Phase-6 real-time notifications.
// Everything is opt-in and default-disabled: with Enabled = false (the
// default) the application behaves exactly as without this feature — no extra
// goroutines and no SSE route.
//
//	[notifications]
//	enabled      = false          # master switch — MUST be true to activate anything
//	idle         = true           # start an IMAP IDLE watcher when enabled
//	desktop      = false          # native OS toast via gen2brain/beeep (local runs)
//	webpush      = false          # VAPID Web Push (background, even with no open tab)
//	vapid_key_file = "vapid.json" # relative paths live under the runtime data directory
type NotificationsConfig struct {
	Enabled      bool   `toml:"enabled"`        // master switch; default false
	Idle         bool   `toml:"idle"`           // IMAP IDLE watcher; default true when Enabled
	Desktop      bool   `toml:"desktop"`        // native OS toasts via beeep; default false
	WebPush      bool   `toml:"webpush"`        // VAPID Web Push; default false
	VAPIDKeyFile string `toml:"vapid_key_file"` // path to JSON key file; auto-generated
}

// AccountsConfig enables the non-mirror multi-account compatibility flow.
// Its durable state uses the configured SQLite/Postgres KV backend.
type AccountsConfig struct {
	Enabled bool `toml:"enabled"`
}

// RateLimitConfig configures per-IP rate limits on high-risk endpoints to
// guard against brute-force and abuse. All three surfaces are tuned
// independently because their cost and risk profiles differ:
//
//   - Login: brute-force protection; tight window, low max.
//
//   - Send (POST /v1/messages): spam/abuse; medium limits.
//
//   - AI (/api/ai/*): compute cost + abuse; medium limits.
//
//     [rate_limit]
//     login_max    = 10    # attempts per window per IP
//     login_window = 60    # seconds
//     send_max     = 30
//     send_window  = 60
//     ai_max       = 20
//     ai_window    = 60
type RateLimitConfig struct {
	LoginMax    int `toml:"login_max"`    // default 10
	LoginWindow int `toml:"login_window"` // seconds, default 60
	SendMax     int `toml:"send_max"`     // default 30
	SendWindow  int `toml:"send_window"`  // seconds, default 60
	AIMax       int `toml:"ai_max"`       // default 20
	AIWindow    int `toml:"ai_window"`    // seconds, default 60
}

// DemoConfig configures the optional demo / screenshot mode.
//
// When enabled, a POST /demo-login route is registered that accepts the
// configured username/password without contacting any IMAP server. The
// resulting session returns in-memory seed messages so the UI can be
// screenshotted without real credentials.
//
//	[demo]
//	enabled  = true
//	email    = "demo@inbrix.dev"
//	password = "demo"
type DemoConfig struct {
	Enabled  bool   `toml:"enabled"`
	Email    string `toml:"email"`
	Password string `toml:"password"`
}

type Config struct {
	Server        ServerConfig          `toml:"server"`
	Auth          AuthConfig            `toml:"auth"`
	IMAP          IMAPConfig            `toml:"imap"`
	SMTP          SMTPConfig            `toml:"smtp"`
	JWT           JWTConfig             `toml:"jwt"`
	Cache         CacheConfig           `toml:"cache"`
	MailSync      MailSyncConfig        `toml:"mail_sync"`
	Storage       StorageConfig         `toml:"storage"`
	Encryption    EncryptionConfig      `toml:"encryption"`
	SSL           SSLConfig             `toml:"ssl"`
	OAuth2        OAuth2Config          `toml:"oauth2"`
	CalDAV        CalDAVConfig          `toml:"caldav"`
	CardDAV       CardDAVContactsConfig `toml:"carddav"`
	Notifications NotificationsConfig   `toml:"notifications"`
	AI            AIConfig              `toml:"ai"`
	Accounts      AccountsConfig        `toml:"accounts"`
	Demo          DemoConfig            `toml:"demo"`
	RateLimit     RateLimitConfig       `toml:"rate_limit"`
}

func LoadConfig(filepath string) (*Config, error) {
	var config Config

	config.Server.UsernameIsEmail = true
	config.Server.Port = 2342
	// Durable state defaults to the embedded SQLite backend (single-binary, no
	// external services). Postgres is opt-in via [storage].
	config.Storage.Backend = "sqlite"
	// Cache/staging directory. Defaults to ./data (matching config.toml.example)
	// so the embedded database AND — crucially — outbound attachment staging
	// (POST /v1/attachments, see handlers/jsonapi/compose_attachments.go) work out
	// of the box. When this is empty, attachment UPLOADS fail with 503 "staging
	// unavailable" while downloads keep working, which reads to a user as
	// "attachments are broken" even though received mail attaches fine. A config
	// file may still override it via [cache] folder.
	config.Cache.Folder = "./data"
	config.MailSync.Enabled = true
	// Derive this after TOML decoding so overriding only [cache].folder also
	// moves the default SQLite database.
	config.MailSync.Database = ""
	config.MailSync.Interval = 60
	config.MailSync.BatchSize = 200
	config.MailSync.MaxMessagesPerFolder = 5000
	config.MailSync.SyncBodies = true
	// Set default values
	config.SMTP.Port = 587 // Default to STARTTLS port
	config.SMTP.UseSTARTTLS = true

	// IMAP defaults to implicit TLS (imaps). Because defaults are applied BEFORE
	// the TOML decode below, an explicit `tls = false` in the config overrides
	// this and an absent key keeps the secure default. (Fixes #8.)
	config.IMAP.TLS = true

	// Default SSL configuration. Only the HSTS max-age is meaningful — see the
	// comment on SSLConfig for why there is no port/redirect default here.
	config.SSL.HSTSMaxAge = 31536000 // 1 year

	// Default OAuth2 configuration
	config.OAuth2.Mechanism = "xoauth2"
	config.OAuth2.EmailClaim = "email"
	config.OAuth2.UsePKCE = true

	// Default CalDAV configuration
	config.CalDAV.Auth = "basic"

	// Default Notifications configuration — everything OFF by default.
	// Idle is set to true here so that it activates automatically once the
	// user opts in by setting enabled = true; they can still turn it off
	// individually with idle = false.
	config.Notifications.Enabled = false
	config.Notifications.Idle = true
	config.Notifications.Desktop = false
	config.Notifications.WebPush = false
	config.Notifications.VAPIDKeyFile = "vapid.json"
	config.Accounts.Enabled = false

	// Default AI configuration.
	// Enabled defaults to false (explicit opt-in required).
	// The default endpoint is the Vulos OS airouter so Inbrix AI works without
	// extra configuration when embedded in a Vulos installation.
	config.AI.Enabled = false
	config.AI.Mode = AIModeRemote
	config.AI.Endpoint = "http://localhost:8080/api/ai/chat"
	config.AI.APIKey = ""
	config.AI.AccountHeader = ""
	config.AI.Model = ""
	config.AI.LLMuxConfig = ""
	config.AI.LLMuxCache = false

	// Default rate-limit configuration.
	// Login: tight (brute-force surface). Send + AI: moderate (abuse/cost).
	// Override via [rate_limit] in config.toml.
	config.RateLimit.LoginMax = 10
	config.RateLimit.LoginWindow = 60
	config.RateLimit.SendMax = 30
	config.RateLimit.SendWindow = 60
	config.RateLimit.AIMax = 20
	config.RateLimit.AIWindow = 60

	// Load config file
	_, err := toml.DecodeFile(filepath, &config)
	if err != nil {
		return nil, err
	}
	if config.MailSync.Database == "" {
		config.MailSync.Database = filepathpkg.Join(config.Cache.Folder, "mail.db")
	}
	if config.MailSync.Interval <= 0 {
		config.MailSync.Interval = 60
	}
	if config.MailSync.BatchSize <= 0 {
		config.MailSync.BatchSize = 200
	}
	if config.MailSync.MaxMessagesPerFolder < 0 {
		return nil, fmt.Errorf("[mail_sync] max_messages_per_folder cannot be negative")
	}

	// Reconcile the [auth] allow_full_email_username key with the legacy
	// [server] username_is_email key. [auth] is the documented option going
	// forward; when it is explicitly set it wins. When it is absent we mirror
	// the [server] value into it so downstream code (which all reads
	// Server.UsernameIsEmail) sees a single, consistent source of truth.
	if config.Auth.AllowFullEmailUsername != nil {
		config.Server.UsernameIsEmail = *config.Auth.AllowFullEmailUsername
	} else {
		v := config.Server.UsernameIsEmail
		config.Auth.AllowFullEmailUsername = &v
	}

	// Normalise + validate the AI mode. An empty mode (a config written before
	// embedded mode existed) is "remote". A typo — "embeded", "local" — must
	// not silently fall back to remote: it would send mail to whatever endpoint
	// happened to be configured instead of the in-process gateway the operator
	// asked for.
	if config.AI.Mode == "" {
		config.AI.Mode = AIModeRemote
	}
	switch config.AI.Mode {
	case AIModeRemote, AIModeEmbedded:
	default:
		return nil, fmt.Errorf("[ai] mode must be %q or %q, got %q", AIModeRemote, AIModeEmbedded, config.AI.Mode)
	}

	// If SMTP server is not specified, derive it from IMAP server
	if config.SMTP.Server == "" {
		config.SMTP.Server = config.IMAP.Server
		// Convert imap.server.com to smtp.server.com
		if len(config.SMTP.Server) > 5 && config.SMTP.Server[:5] == "imap." {
			config.SMTP.Server = "smtp" + config.SMTP.Server[4:]
		}
	}

	// Validate SSL configuration if enabled
	if config.SSL.Enabled {
		if err := config.ValidateSSL(); err != nil {
			return nil, fmt.Errorf("SSL configuration error: %w", err)
		}
	}

	// Fail fast on a misconfigured encryption key rather than surfacing an opaque
	// AES error (and a 500) on the first login. AES accepts only 16/24/32-byte
	// keys (AES-128/192/256). A wrong-length non-empty key is always a mistake; an
	// empty key disables credential encryption (login will fail), so warn loudly.
	switch n := len(config.Encryption.Key); n {
	case 0:
		log.Println("warning: [encryption] key is empty; credential encryption is unavailable and login will fail — set a 16, 24, or 32-byte key")
	case 16, 24, 32:
		// valid AES-128/192/256 key length
	default:
		return nil, fmt.Errorf("[encryption] key must be 16, 24, or 32 bytes for AES-128/192/256, got %d", n)
	}

	// JWT secret is used to sign session/API tokens; an empty secret means tokens
	// are not securely signed. Warn rather than fail so standalone dev still runs.
	if config.JWT.Secret == "" {
		log.Println("warning: [jwt] secret is empty; tokens will not be securely signed — set a strong random secret for any non-dev deployment")
	}

	return &config, nil
}

// Helper method to get the appropriate SMTP port based on encryption
func (c *SMTPConfig) GetPort() int {
	if c.Port != 0 {
		return c.Port
	}
	if c.UseSTARTTLS {
		return 587 // STARTTLS port
	}
	return 465 // SSL/TLS port
}

// ValidateSSL checks if the SSL configuration is valid
func (c *Config) ValidateSSL() error {
	if !c.SSL.Enabled {
		return nil
	}

	if c.SSL.CertFile == "" {
		return fmt.Errorf("SSL certificate file path is required")
	}

	if c.SSL.KeyFile == "" {
		return fmt.Errorf("SSL key file path is required")
	}

	// Try loading the certificates to verify they're valid
	_, err := tls.LoadX509KeyPair(c.SSL.CertFile, c.SSL.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to load SSL certificates: %w", err)
	}

	return nil
}

// GetSecurityHeaders returns a map of security headers based on the configuration.
//
// The baseline hardening headers (content-type, XSS, referrer, and the framing
// policy) are emitted unconditionally so they apply whether or not TLS is
// terminated here — Inbrix AI commonly runs plain HTTP behind a host shell or
// reverse proxy. HSTS is the only SSL-gated header (it is meaningless without
// TLS).
func (c *Config) GetSecurityHeaders() map[string]string {
	headers := make(map[string]string)

	// HSTS only makes sense when TLS is terminated by Inbrix AI itself.
	if c.SSL.Enabled && c.SSL.Domain != "" {
		headers["Strict-Transport-Security"] = fmt.Sprintf("max-age=%d; includeSubDomains", c.SSL.HSTSMaxAge)
	}

	headers["X-Content-Type-Options"] = "nosniff"
	headers["X-XSS-Protection"] = "1; mode=block"
	headers["Referrer-Policy"] = "strict-origin-when-cross-origin"

	// Content-Security-Policy — combines the framing policy with a strict
	// script-src so that injected HTML (e.g. from a broken srcdoc attribute)
	// cannot execute scripts in the Inbrix AI origin.
	//
	// 'self'            — allow scripts/styles loaded from the same origin
	// 'unsafe-inline'   — needed for the browser's inline style attributes and
	//                     the small inline style blocks used by the client shell.
	// blob:             — allows browser APIs to use blob: object URLs when needed.
	//
	// Email HTML bodies are sandboxed inside <iframe sandbox> (no allow-scripts)
	// so they never reach this CSP; this policy is the outer-page defence.
	scriptSrc := "'self'"
	imgSrc := "'self' data: blob:"
	connectSrc := "'self'"
	csp := "default-src 'self'; script-src " + scriptSrc + "; style-src 'self' 'unsafe-inline'; img-src " + imgSrc + "; connect-src " + connectSrc + "; object-src 'none'; base-uri 'self';"

	// Framing policy. When a host shell (e.g. Vulos OS) is allowed to embed
	// Inbrix AI, express it via CSP frame-ancestors and omit the legacy
	// X-Frame-Options header (which has no allow-list form). Otherwise keep
	// the strict same-origin default.
	if c.Server.FrameAncestors != "" {
		csp += " frame-ancestors " + c.Server.FrameAncestors
	} else {
		headers["X-Frame-Options"] = "SAMEORIGIN"
		csp += " frame-ancestors 'self'"
	}
	headers["Content-Security-Policy"] = csp

	return headers
}
