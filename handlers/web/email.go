// handlers/web/email.go
package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"lilmail/config"
	"lilmail/handlers/api"
	"lilmail/handlers/htmlsafe"
	"lilmail/mailstore"
	"lilmail/models"
	"lilmail/storage"
	"lilmail/utils"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// recipientsStorePath returns the path to the per-user bbolt database that
// stores both thread cache and recent recipients (shared file).
func recipientsStorePath(cacheFolder, username string) string {
	return filepath.Join(cacheFolder, api.SanitizeUsername(username), "threads.db")
}

// boltPath returns the path to the per-user bbolt thread-cache database.
func boltPath(cacheFolder, username string) string {
	return recipientsStorePath(cacheFolder, username)
}

type EmailHandler struct {
	store     *session.Store
	config    *config.Config
	auth      *AuthHandler
	acctStore *AccountStore // nil when accounts.enabled = false
	mailDB    *mailstore.Store

	// threadStores caches one open bbolt handle per user so we don't open the
	// single-writer file on every request (which would cause lock contention).
	threadStoresMu sync.Mutex
	threadStores   map[string]*api.ThreadStore
}

func NewEmailHandler(store *session.Store, config *config.Config, auth *AuthHandler) *EmailHandler {
	return &EmailHandler{
		store:        store,
		config:       config,
		auth:         auth,
		threadStores: make(map[string]*api.ThreadStore),
	}
}

// SetAccountStore wires in the multi-account store so HandleInbox can do
// unified fetches.  Called from main.go after the store is opened.
func (h *EmailHandler) SetAccountStore(s *AccountStore) {
	h.acctStore = s
}

func (h *EmailHandler) SetMailMirror(s *mailstore.Store) {
	h.mailDB = s
}

type mailAccountOption struct {
	Email    string
	Label    string
	Color    string
	IsActive bool
}

func (h *EmailHandler) mailAccountOptions(c *fiber.Ctx) []mailAccountOption {
	activeEmail := strings.TrimSpace(h.auth.GetSessionEmail(c))
	options := make([]mailAccountOption, 0)
	seen := make(map[string]struct{})
	appendOption := func(email, label, color string) {
		email = strings.TrimSpace(email)
		if email == "" {
			return
		}
		key := strings.ToLower(email)
		if _, exists := seen[key]; exists {
			return
		}
		if label == "" {
			label = email
		}
		seen[key] = struct{}{}
		options = append(options, mailAccountOption{
			Email:    email,
			Label:    label,
			Color:    color,
			IsActive: strings.EqualFold(email, activeEmail),
		})
	}

	if h.mailDB != nil {
		sess, err := h.store.Get(c)
		if err == nil {
			owner, _ := sess.Get("user_id").(string)
			if owner != "" {
				if accounts, listErr := h.mailDB.ListAccounts(c.UserContext(), owner); listErr == nil {
					for _, account := range accounts {
						appendOption(account.Email, account.Label, account.Color)
					}
				}
			}
		}
		if len(options) == 0 {
			appendOption(activeEmail, activeEmail, "")
		}
		return options
	}

	appendOption(activeEmail, activeEmail, "")
	if h.acctStore != nil && h.config.Accounts.Enabled {
		username, _ := c.Locals("username").(string)
		if entries, err := h.acctStore.List(username); err == nil {
			for _, entry := range entries {
				appendOption(entry.Email, entry.Label, entry.Color)
			}
		}
	}
	return options
}

func (h *EmailHandler) currentMirrorAccount(c *fiber.Ctx) (mailstore.Account, bool) {
	if h.mailDB == nil {
		return mailstore.Account{}, false
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return mailstore.Account{}, false
	}
	if id, _ := sess.Get("account_id").(string); id != "" {
		account, err := h.mailDB.GetAccount(c.UserContext(), id)
		return account, err == nil
	}
	if owner, _ := sess.Get("user_id").(string); owner != "" {
		accounts, err := h.mailDB.ListAccounts(c.UserContext(), owner)
		if err == nil && len(accounts) > 0 {
			return accounts[0], true
		}
	}
	return mailstore.Account{}, false
}

func mirrorMailboxInfos(folders []mailstore.Folder) []*api.MailboxInfo {
	out := make([]*api.MailboxInfo, 0, len(folders))
	for _, folder := range folders {
		out = append(out, &api.MailboxInfo{
			Name:        folder.Name,
			Delimiter:   folder.Delimiter,
			Attributes:  append([]string(nil), folder.Attributes...),
			UnreadCount: folder.UnreadCount,
		})
	}
	return out
}

func tagMirrorEmails(emails []models.Email, account mailstore.Account) []models.Email {
	for i := range emails {
		emails[i].AccountEmail = account.Email
		emails[i].AccountLabel = account.Label
		emails[i].AccountColor = account.Color
	}
	return emails
}

func (h *EmailHandler) localMessages(c *fiber.Ctx, account mailstore.Account, folder string, limit int) ([]models.Email, bool, error) {
	state, stateErr := h.mailDB.GetSyncState(c.UserContext(), account.ID, folder)
	emails, err := h.mailDB.ListMessages(c.UserContext(), account.ID, folder, limit, 0)
	if err != nil {
		return nil, false, err
	}
	// A non-empty mirror is useful even while a capped initial sync is still
	// running. An empty folder becomes authoritative only after a completed
	// sync marker is written.
	ready := len(emails) > 0 || (stateErr == nil && state.SyncComplete)
	return tagMirrorEmails(emails, account), ready, nil
}

func (h *EmailHandler) mirrorAccounts(c *fiber.Ctx) ([]mailstore.Account, bool, error) {
	if h.mailDB == nil {
		return nil, false, nil
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return nil, false, err
	}
	owner, _ := sess.Get("user_id").(string)
	if owner == "" {
		return nil, false, nil
	}
	accounts, err := h.mailDB.ListAccounts(c.UserContext(), owner)
	return accounts, true, err
}

// localUnifiedMessages merges synchronized mail without opening any IMAP
// connections. A slow or unavailable remote mailbox therefore cannot block a
// page that already has local data.
func (h *EmailHandler) localUnifiedMessages(c *fiber.Ctx, accounts []mailstore.Account, folder string, limit int) ([]models.Email, []AccountFetchResult, error) {
	var merged []models.Email
	results := make([]AccountFetchResult, 0, len(accounts))
	for _, account := range accounts {
		result := AccountFetchResult{AccountEmail: account.Email, AccountLabel: account.Label, AccountColor: account.Color}
		if result.AccountLabel == "" {
			result.AccountLabel = account.Email
		}
		emails, err := h.mailDB.ListMessages(c.UserContext(), account.ID, folder, limit, 0)
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}
		result.Emails = tagMirrorEmails(emails, account)
		merged = append(merged, result.Emails...)
		results = append(results, result)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Date.Equal(merged[j].Date) {
			return merged[i].ID > merged[j].ID
		}
		return merged[i].Date.After(merged[j].Date)
	})
	max := limit * len(accounts)
	if max > 200 {
		max = 200
	}
	if max > 0 && len(merged) > max {
		merged = merged[:max]
	}
	return merged, results, nil
}

func (h *EmailHandler) localUserWithoutMailbox(c *fiber.Ctx) bool {
	if h.mailDB == nil {
		return false
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return false
	}
	userID, _ := sess.Get("user_id").(string)
	if userID == "" {
		return false
	}
	authType, _ := sess.Get("auth_type").(string)
	return authType != demoAuthType
}

func (h *EmailHandler) mirrorAccountForEmail(c *fiber.Ctx, email string) (mailstore.Account, bool) {
	if h.mailDB == nil || strings.TrimSpace(email) == "" {
		return mailstore.Account{}, false
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return mailstore.Account{}, false
	}
	owner, _ := sess.Get("user_id").(string)
	if owner == "" {
		return mailstore.Account{}, false
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, email)
	return account, err == nil
}

// requestAccountEmail identifies the source mailbox for a message operation.
// Unified-inbox rows carry this in a header; the query-string fallback keeps
// direct links and attachment-style callers compatible.
func requestAccountEmail(c *fiber.Ctx) string {
	accountEmail := strings.TrimSpace(c.Get("X-Account-Email"))
	if accountEmail == "" {
		accountEmail = strings.TrimSpace(c.Query("account_email"))
	}
	return accountEmail
}

// messageClientForAccount opens the IMAP connection for a message operation
// and returns the owner-scoped mirror account when one exists. The account
// email is never trusted by itself: mirrorAccountForEmail verifies that it
// belongs to the authenticated application user before any network call.
func (h *EmailHandler) messageClientForAccount(c *fiber.Ctx, accountEmail string) (api.MailClient, mailstore.Account, error) {
	if accountEmail != "" {
		if h.mailDB != nil {
			account, ok := h.mirrorAccountForEmail(c, accountEmail)
			if !ok {
				return nil, mailstore.Account{}, fmt.Errorf("account %s not found", accountEmail)
			}
			client, err := h.auth.CreateIMAPClientForMirrorAccount(account)
			return client, account, err
		}

		// Legacy bbolt multi-account mode has no mirror account ID, so resolve
		// the requested mailbox against the current user's stored entries.
		sessionEmail := h.auth.GetSessionEmail(c)
		if strings.EqualFold(accountEmail, sessionEmail) {
			client, err := h.auth.CreateIMAPClient(c)
			return client, mailstore.Account{}, err
		}
		if h.acctStore != nil && h.config.Accounts.Enabled {
			username, _ := c.Locals("username").(string)
			entries, err := h.acctStore.List(username)
			if err != nil {
				return nil, mailstore.Account{}, err
			}
			for _, entry := range entries {
				if strings.EqualFold(entry.Email, accountEmail) {
					client, clientErr := h.auth.CreateIMAPClientForAccount(entry)
					return client, mailstore.Account{}, clientErr
				}
			}
		}
		return nil, mailstore.Account{}, fmt.Errorf("account %s not found", accountEmail)
	}

	if account, ok := h.currentMirrorAccount(c); ok {
		client, err := h.auth.CreateIMAPClientForMirrorAccount(account)
		return client, account, err
	}
	client, err := h.auth.CreateIMAPClient(c)
	return client, mailstore.Account{}, err
}

// getThreadStore returns the shared ThreadStore for the given user, opening it
// if necessary.  On failure it returns nil (callers fall back to in-memory).
func (h *EmailHandler) getThreadStore(username string) *api.ThreadStore {
	h.threadStoresMu.Lock()
	defer h.threadStoresMu.Unlock()

	if ts, ok := h.threadStores[username]; ok {
		return ts
	}
	path := boltPath(h.config.Cache.Folder, username)
	ts, err := api.OpenThreadStore(path)
	if err != nil {
		log.Printf("threadstore: open for %s: %v — will use in-memory threading", username, err)
		return nil
	}
	h.threadStores[username] = ts
	return ts
}

// buildThreads builds JWZ threads using the shared bbolt store when available,
// falling back to in-memory-only threading (api.ThreadMessages, no bbolt
// persistence) when no store is open or the store errors.
func (h *EmailHandler) buildThreads(username, folder string, emails []models.Email) []models.Thread {
	ts := h.getThreadStore(username)
	if ts != nil {
		threads, err := ts.BuildThreads(folder, emails)
		if err == nil {
			return threads
		}
		log.Printf("threadstore: BuildThreads for %s/%s: %v — falling back", username, folder, err)
	}
	// Fallback: in-memory only (no bbolt persistence).
	return api.ThreadMessages(emails)
}

// HandleInbox renders the merged inbox/sent conversation view.
func (h *EmailHandler) HandleInbox(c *fiber.Ctx) error {
	return h.HandleConversations(c)
}

// HandleFolder displays emails from a specific folder
func (h *EmailHandler) HandleFolder(c *fiber.Ctx) error {
	username := c.Locals("username")
	if username == nil {
		return c.Redirect("/login")
	}

	userStr, ok := username.(string)
	if !ok {
		return c.Redirect("/login")
	}
	sessionEmail, _ := c.Locals("email").(string)

	folderName, err := url.QueryUnescape(c.Params("name"))
	if folderName == "" {
		return c.Redirect("/inbox")
	}
	if isConversationMailboxName(folderName) {
		return c.Redirect("/inbox")
	}

	// Load folders for sidebar, preferring the synchronized mirror.
	userCacheFolder := filepath.Join(h.config.Cache.Folder, api.SanitizeUsername(userStr))
	var folders []*api.MailboxInfo
	account, hasMirrorAccount := h.currentMirrorAccount(c)
	if !hasMirrorAccount && h.localUserWithoutMailbox(c) {
		return c.Redirect("/settings?setup=1")
	}
	if hasMirrorAccount {
		if mirroredFolders, folderErr := h.mailDB.ListFolders(c.UserContext(), account.ID); folderErr == nil && len(mirroredFolders) > 0 {
			folders = mirrorMailboxInfos(mirroredFolders)
		}
	}
	if len(folders) == 0 && !hasMirrorAccount {
		if err := utils.LoadCache(filepath.Join(userCacheFolder, "folders.json"), &folders); err != nil {
			return c.Status(500).SendString("Error loading folders")
		}
	}

	var emails []models.Email
	if hasMirrorAccount {
		emails, _, err = h.localMessages(c, account, folderName, 50)
	}
	if !hasMirrorAccount {
		client, clientErr := h.auth.CreateIMAPClient(c)
		if clientErr != nil {
			return c.Status(500).SendString("Error connecting to email server")
		}
		emails, err = client.FetchMessages(folderName, 50)
		_ = client.Close()
	}
	if err != nil {
		return c.Status(500).SendString("Error fetching emails")
	}

	// Get JWT token for API requests
	token, err := api.GetSessionToken(c, h.store)
	if err != nil {
		return c.Redirect("/login")
	}

	// Build JWZ threads using the shared bbolt store.
	threads := h.buildThreads(userStr, folderName, emails)

	return c.JSON(fiber.Map{
		"Username":      userStr,
		"Email":         sessionEmail,
		"MailAccounts":  h.mailAccountOptions(c),
		"Folders":       folders,
		"Emails":        emails,
		"Threads":       threads,
		"CurrentFolder": folderName,
		"Token":         token,
	})
}

// HandleEmailView returns a compatibility JSON payload for a single email.
// In unified mode, X-Account-Email identifies which account's IMAP connection
// to use.  Falls back to the session account when the header is absent.
func (h *EmailHandler) HandleEmailView(c *fiber.Ctx) error {
	// Validate Authorization header
	token := c.Get("Authorization")
	if token == "" || len(token) < 8 || token[:7] != "Bearer " {
		return c.Status(401).SendString("Unauthorized")
	}

	// Get folder and email ID
	folderName := c.Get("X-Folder")
	if folderName == "" {
		folderName = c.Query("folder")
		if folderName == "" {
			folderName = "INBOX"
		}
	}

	emailID := c.Params("id")
	if emailID == "" {
		return c.Status(400).SendString("Email ID required")
	}

	// Unified mode: X-Account-Email tells us which account this message belongs to.
	accountEmail := c.Get("X-Account-Email")

	var client api.MailClient
	var err error
	var email models.Email
	usedMirror := false
	var mirrorAccount mailstore.Account
	if accountEmail == "" {
		if account, ok := h.currentMirrorAccount(c); ok {
			mirrorAccount = account
			cached, dbErr := h.mailDB.GetMessage(c.UserContext(), account.ID, folderName, emailID)
			if dbErr != nil {
				if errors.Is(dbErr, mailstore.ErrNotFound) {
					return mailBodyNotReady(c)
				}
				log.Printf("mail mirror: read message %s/%s: %v", folderName, emailID, dbErr)
				return c.Status(500).JSON(fiber.Map{"error": "Error reading local email cache"})
			}
			if !emailBodyCached(cached) {
				return mailBodyNotReady(c)
			}
			email = cached
			usedMirror = true
		}
	} else if account, ok := h.mirrorAccountForEmail(c, accountEmail); ok {
		mirrorAccount = account
		cached, dbErr := h.mailDB.GetMessage(c.UserContext(), account.ID, folderName, emailID)
		if dbErr != nil {
			if errors.Is(dbErr, mailstore.ErrNotFound) {
				return mailBodyNotReady(c)
			}
			log.Printf("mail mirror: read message %s/%s: %v", folderName, emailID, dbErr)
			return c.Status(500).JSON(fiber.Map{"error": "Error reading local email cache"})
		}
		if !emailBodyCached(cached) {
			return mailBodyNotReady(c)
		}
		email = cached
		usedMirror = true
	}
	if mirrorAccount.ID != "" && !usedMirror {
		return mailBodyNotReady(c)
	}

	if !usedMirror && accountEmail != "" && mirrorAccount.ID != "" {
		client, err = h.auth.CreateIMAPClientForMirrorAccount(mirrorAccount)
	} else if !usedMirror && accountEmail != "" && h.mailDB != nil {
		err = fmt.Errorf("account %s not found", accountEmail)
	} else if !usedMirror && accountEmail != "" && h.acctStore != nil && h.config.Accounts.Enabled {
		// Try to find this account in the store.
		username, _ := c.Locals("username").(string)
		sessionEmail, _ := c.Locals("email").(string)

		if accountEmail == sessionEmail {
			// It's the primary account — use the session client.
			client, err = h.auth.CreateIMAPClient(c)
		} else {
			// It's an additional account.
			entries, listErr := h.acctStore.List(username)
			if listErr == nil {
				for _, e := range entries {
					if e.Email == accountEmail {
						client, err = h.auth.CreateIMAPClientForAccount(e)
						break
					}
				}
			}
			if client == nil && err == nil {
				err = fmt.Errorf("account %s not found", accountEmail)
			}
		}
	} else if !usedMirror {
		client, err = h.auth.CreateIMAPClient(c)
	}

	if !usedMirror && err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": "Error connecting to email server",
		})
	}
	if !usedMirror {
		defer client.Close()

		// Fetch the email from IMAP and populate the local body cache.
		email, err = client.FetchSingleMessage(folderName, emailID)
		if err != nil {
			log.Printf("Error fetching email %s from folder %s: %v", emailID, folderName, err)
			return c.Status(500).JSON(fiber.Map{
				"error": fmt.Sprintf("Error fetching email: %v", err),
			})
		}
		if mirrorAccount.ID != "" {
			if dbErr := h.mailDB.UpsertMessages(c.UserContext(), mirrorAccount.ID, folderName, []models.Email{email}); dbErr != nil {
				log.Printf("mail mirror: cache body %s/%s: %v", folderName, emailID, dbErr)
			}
		}
	}
	if mirrorAccount.ID != "" {
		email.AccountEmail = mirrorAccount.Email
		email.AccountLabel = mirrorAccount.Label
		email.AccountColor = mirrorAccount.Color
	}
	// Detect Drafts folder for compatibility metadata.
	isDrafts := strings.Contains(strings.ToLower(folderName), "draft")

	// Propagate account identity so the reply/compose path can use the right SMTP.
	if accountEmail != "" {
		email.AccountEmail = accountEmail
	}

	// "self" is the address that should be excluded from Reply-All recipients.
	sessionEmail, _ := c.Locals("email").(string)
	self := sessionEmail
	if accountEmail != "" {
		self = accountEmail
	}

	// Prepare the HTML body for the sandboxed reading-pane iframe: inject a
	// readable baseline stylesheet and a <base target="_blank">. Ordinary remote
	// images load by default; other remote-loading content remains blocked.
	var preparedHTML string
	var hasRemote bool
	if email.HTML != "" {
		preparedHTML, hasRemote = prepareEmailHTML(email.HTML)
	}

	// Sanitize the HTML that feeds any edit-draft client path. React never inserts
	// this value into the outer document without sanitizing it first.
	// Raw mail HTML there is a stored-XSS sink (innerHTML fires load/error handlers
	// on inserted nodes), so it must be defanged server-side. This is SEPARATE from
	// preparedHTML above, which drives the sandboxed reading-pane iframe and stays
	// full-fidelity.
	editableHTML := editableDraftHTML(email.HTML)

	// Return the compatibility payload as JSON.
	return c.JSON(fiber.Map{
		"Email":         email,
		"EmailHTML":     preparedHTML,
		"EditableHTML":  editableHTML,
		"HasRemote":     hasRemote,
		"Self":          self,
		"CurrentFolder": folderName,
		"IsDrafts":      isDrafts,
	})
}

func emailBodyCached(email models.Email) bool {
	return email.BodyCached || email.Body != "" || email.HTML != ""
}

func mailBodyNotReady(c *fiber.Ctx) error {
	return c.Status(fiber.StatusConflict).JSON(fiber.Map{
		"error": "message body is still synchronizing locally",
		"code":  "mail_body_not_ready",
	})
}

// HandleAttachment streams a single attachment to the browser. The attachment
// ID encodes the folder, message UID, and MIME part path; the content is
// fetched from the server on demand.
func (h *EmailHandler) HandleAttachment(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return c.Status(400).SendString("Attachment ID required")
	}

	folder, uid, part, err := api.DecodeAttachmentID(id)
	if err != nil {
		return c.Status(400).SendString("Invalid attachment ID")
	}

	// Enforce a 25 MiB limit on attachment downloads to avoid unbounded memory use.
	const maxAttachmentBytes = 25 * 1024 * 1024
	var mirrorAccount mailstore.Account
	if accountEmail := c.Query("account_email"); accountEmail != "" {
		var ok bool
		mirrorAccount, ok = h.mirrorAccountForEmail(c, accountEmail)
		if !ok && h.mailDB != nil {
			return c.Status(404).SendString("Account not found")
		}
	} else if account, ok := h.currentMirrorAccount(c); ok {
		mirrorAccount = account
	}

	// Optional supplementary cache: when the Vulos OS gateway has provisioned an
	// object bucket for this request (and the seam is enabled), serve immutable
	// attachment blobs from it to avoid re-pulling the full MIME part from IMAP.
	// Absent the headers this is a no-op and behaviour is identical to before.
	// IMAP remains the source of truth; the bucket is a pure read-through cache.
	objStore, useCache := storage.ObjectStoreFromHeaders(func(k string) string { return c.Get(k) })
	cachePrefix := mirrorAccount.ID
	if cachePrefix == "" {
		cachePrefix = "session"
	}
	cacheKey := "attachments/" + cachePrefix + "/" + id
	if useCache {
		if obj, cerr := objStore.Get(c.UserContext(), cacheKey); cerr == nil {
			if obj.ContentType != "" {
				c.Set("Content-Type", obj.ContentType)
			}
			c.Set("Content-Disposition", attachmentDisposition(c, obj.Meta["filename"]))
			return c.SendStream(bytes.NewReader(obj.Body), len(obj.Body))
		} else if cerr != storage.ErrNotFound {
			// Cache trouble must never break downloads — log and fall through.
			log.Printf("attachment cache get %s: %v", cacheKey, cerr)
		}
	}

	var client api.MailClient
	if mirrorAccount.ID != "" {
		client, err = h.auth.CreateIMAPClientForMirrorAccount(mirrorAccount)
	} else {
		client, err = h.auth.CreateIMAPClient(c)
	}
	if err != nil {
		return c.Status(500).SendString("Error connecting to email server")
	}
	defer client.Close()

	content, filename, contentType, err := client.FetchAttachment(folder, uid, part)
	if err != nil {
		log.Printf("Error fetching attachment %s/%s/%s: %v", folder, uid, part, err)
		return c.Status(500).SendString("Error fetching attachment")
	}

	if len(content) > maxAttachmentBytes {
		return c.Status(413).SendString("Attachment exceeds maximum allowed size")
	}

	// Best-effort populate the cache (within the size cap). Failures are logged
	// but never surfaced to the user — the download has already succeeded.
	if useCache {
		if perr := objStore.Put(c.UserContext(), cacheKey, content, contentType, map[string]string{"filename": filename}); perr != nil {
			log.Printf("attachment cache put %s: %v", cacheKey, perr)
		}
	}

	if contentType != "" {
		c.Set("Content-Type", contentType)
	}
	c.Set("Content-Disposition", attachmentDisposition(c, filename))
	return c.SendStream(bytes.NewReader(content), len(content))
}

func attachmentDisposition(c *fiber.Ctx, filename string) string {
	disposition := "attachment"
	if c.QueryBool("inline", false) {
		disposition = "inline"
	}
	return fmt.Sprintf("%s; filename=%q", disposition, filename)
}

// HandleDeleteEmail handles the email deletion request
func (h *EmailHandler) HandleDeleteEmail(c *fiber.Ctx) error {
	// Validate Authorization header
	token := c.Get("Authorization")
	if token == "" || len(token) < 8 || token[:7] != "Bearer " {
		return c.Status(401).SendString("Unauthorized")
	}

	// Validate JWT token
	_, err := api.ValidateToken(token[7:], h.config.JWT.Secret)
	if err != nil {
		return c.Status(401).SendString("Invalid token")
	}

	// Get folder and email ID
	folderName := c.Get("X-Folder")
	if folderName == "" {
		folderName = c.Query("folder")
		if folderName == "" {
			folderName = "INBOX"
		}
	}

	emailID := c.Params("id")
	if emailID == "" {
		return c.Status(400).SendString("Email ID required")
	}

	// Open the source mailbox. Unified-inbox requests must identify the
	// account because UIDs are only unique within one IMAP mailbox.
	client, account, err := h.messageClientForAccount(c, requestAccountEmail(c))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": "Error connecting to email server",
		})
	}
	defer client.Close()

	email := models.Email{ID: emailID, Folder: folderName}
	if account.ID != "" {
		if cached, cacheErr := h.mailDB.GetMessage(c.UserContext(), account.ID, folderName, emailID); cacheErr == nil {
			email = cached
		}
	}
	if err := deleteMessageAttachmentCache(c, client, account.ID, email); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Could not clear the attachment cache; the email was not deleted"})
	}

	// Delete the email
	err = client.DeleteMessage(folderName, emailID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": fmt.Sprintf("Error deleting email: %v", err),
		})
	}
	if account.ID != "" {
		if dbErr := h.mailDB.DeleteMessage(c.UserContext(), account.ID, folderName, emailID); dbErr != nil {
			log.Printf("mail mirror: delete %s/%s: %v", folderName, emailID, dbErr)
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Email deleted successfully",
	})
}

// HandleFolderEmails returns folder contents as JSON for compatibility clients.
// Supports unified mode via ?unified=1 query parameter (INBOX only).
func (h *EmailHandler) HandleFolderEmails(c *fiber.Ctx) error {
	folderName, err := url.QueryUnescape(c.Params("name"))
	if err != nil || folderName == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": "Invalid folder name",
		})
	}
	if isConversationMailboxName(folderName) {
		return h.HandleConversationList(c)
	}

	username := c.Locals("username")
	if username == nil {
		return c.Status(401).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}
	userStr, _ := username.(string)
	sessionEmail, _ := c.Locals("email").(string)

	// Get JWT token for API requests
	token, err := api.GetSessionToken(c, h.store)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{
			"error": "Invalid session",
		})
	}

	unified := c.Query("unified") == "1"
	var additionalAccounts []AccountEntry
	var mirrorAccounts []mailstore.Account
	if h.mailDB != nil {
		mirrorAccounts, _, _ = h.mirrorAccounts(c)
	}
	if h.mailDB == nil && h.acctStore != nil && h.config.Accounts.Enabled {
		additionalAccounts, _ = h.acctStore.List(userStr)
	}
	unifiedAvailable := len(mirrorAccounts) > 1 || len(additionalAccounts) > 0

	var emails []models.Email
	var accountErrors []AccountFetchResult
	account, hasMirrorAccount := h.currentMirrorAccount(c)
	usedLocal := false
	if !unified && hasMirrorAccount {
		emails, _, err = h.localMessages(c, account, folderName, 50)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Error loading local mail mirror"})
		}
		usedLocal = true
	}

	if !usedLocal && unified && len(mirrorAccounts) > 1 && folderName == "INBOX" {
		emails, accountErrors, err = h.localUnifiedMessages(c, mirrorAccounts, folderName, 50)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Error loading local mail mirror"})
		}
		usedLocal = true
	} else if !usedLocal && unified && unifiedAvailable && folderName == "INBOX" {
		client, clientErr := h.auth.CreateIMAPClient(c)
		if clientErr != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Error connecting to email server"})
		}
		emails, accountErrors = FetchUnified(
			client,
			sessionEmail, "", "",
			additionalAccounts,
			h.auth,
			folderName,
			50,
		)
		_ = client.Close()
	} else if !usedLocal {
		client, clientErr := h.auth.CreateIMAPClient(c)
		if clientErr != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Error connecting to email server"})
		}
		emails, err = client.FetchMessages(folderName, 50)
		_ = client.Close()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"error": fmt.Sprintf("Error fetching emails: %v", err),
			})
		}
	}

	threadKey := folderName
	if unified && unifiedAvailable && folderName == "INBOX" {
		threadKey = "UNIFIED/INBOX"
	}
	threads := h.buildThreads(userStr, threadKey, emails)

	return c.JSON(fiber.Map{
		"Emails":           emails,
		"Threads":          threads,
		"CurrentFolder":    folderName,
		"Token":            token,
		"Unified":          unified && unifiedAvailable,
		"UnifiedAvailable": unifiedAvailable,
		"AccountErrors":    accountErrors,
	})
}

// HandleComposeEmail handles the email composition and sending.
// Supports:
//   - Plain-text and HTML (rich-text) bodies — multipart/alternative when both present
//   - File attachments — multipart/mixed wrapper with base64-encoded parts
//   - CC, BCC, In-Reply-To, References for reply/forward threading
//   - Draft deletion by UID when "draft_uid" form field is set (replaces draft on send)
//
// The form must use enctype="multipart/form-data" when attachments are included.
func (h *EmailHandler) HandleComposeEmail(c *fiber.Ctx) error {
	// Required fields.
	to := c.FormValue("to")
	subject := c.FormValue("subject")
	plainBody := c.FormValue("body")     // plain-text body
	htmlBody := c.FormValue("html_body") // optional HTML body (rich-text editor)

	if to == "" || subject == "" || (plainBody == "" && htmlBody == "") {
		return c.Status(400).JSON(fiber.Map{
			"error": "To, subject and body are required",
		})
	}

	var normalizeErr error
	plainBody, htmlBody, normalizeErr = htmlsafe.NormalizeComposeBodies(plainBody, htmlBody)
	if normalizeErr != nil {
		log.Printf("compose: normalize HTML body: %v", normalizeErr)
		return c.Status(400).JSON(fiber.Map{"error": "HTML body is invalid or too large"})
	}
	if strings.TrimSpace(plainBody) == "" && strings.TrimSpace(htmlBody) == "" {
		return c.Status(400).JSON(fiber.Map{"error": "To, subject and body are required"})
	}

	// Collect file attachments from the multipart form.
	const (
		maxComposeAttachmentBytes = int64(18 * 1024 * 1024)
		maxOutgoingMessageBytes   = int64(25 * 1024 * 1024)
	)
	var (
		attachments         []api.OutgoingAttachment
		totalAttachmentSize int64
	)
	form, formErr := c.MultipartForm()
	if formErr != nil && strings.HasPrefix(strings.ToLower(c.Get(fiber.HeaderContentType)), "multipart/form-data") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid multipart compose request"})
	}
	if formErr == nil && form != nil {
		for _, fhs := range form.File {
			for _, fh := range fhs {
				if fh.Size <= 0 {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("Attachment %q is empty", fh.Filename)})
				}
				totalAttachmentSize += fh.Size
				if fh.Size > maxComposeAttachmentBytes || totalAttachmentSize > maxComposeAttachmentBytes {
					return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "Attachments exceed the 18 MiB total size limit"})
				}
				ct := fh.Header.Get("Content-Type")
				if ct == "" {
					ct = "application/octet-stream"
				}
				fileHeader := fh
				filename := filepath.Base(fh.Filename)
				if filename == "" || filename == "." || filename == string(filepath.Separator) {
					filename = "attachment"
				}
				attachments = append(attachments, api.OutgoingAttachment{
					Filename:    filename,
					ContentType: ct,
					Open: func() (io.ReadCloser, error) {
						return fileHeader.Open()
					},
				})
			}
		}
	}

	cc := c.FormValue("cc")
	bcc := c.FormValue("bcc")
	inReplyTo := c.FormValue("in_reply_to")
	references := c.FormValue("references")
	draftUID := c.FormValue("draft_uid") // UID of draft to delete after send

	// account_email: when set (unified-view reply), send from that account's SMTP
	// rather than the session account.
	replyAccountEmail := c.FormValue("account_email")

	// Get the sender email from the session (or the specific reply account).
	fromEmail := h.auth.GetSessionEmail(c)
	if replyAccountEmail != "" {
		fromEmail = replyAccountEmail
	}

	// Build the MIME message.
	mimeOpts := api.MIMEMessageOptions{
		From:        fromEmail,
		To:          to,
		Cc:          cc,
		Subject:     subject,
		InReplyTo:   inReplyTo,
		References:  references,
		PlainBody:   plainBody,
		HTMLBody:    htmlBody,
		Attachments: attachments,
	}
	var (
		rawMessage []byte
		mimeFile   *os.File
		mimeSize   int64
		err        error
	)
	if len(attachments) == 0 {
		rawMessage, err = api.BuildMIMEMessage(mimeOpts)
		mimeSize = int64(len(rawMessage))
	} else {
		mimeFile, err = os.CreateTemp("", "lilmail-compose-*.eml")
		if err == nil {
			defer func() {
				mimeFile.Close()
				os.Remove(mimeFile.Name())
			}()
			err = api.WriteMIMEMessage(mimeFile, mimeOpts)
		}
		if err == nil {
			var info os.FileInfo
			info, err = mimeFile.Stat()
			if err == nil {
				mimeSize = info.Size()
			}
		}
	}
	if err != nil {
		log.Printf("compose: build MIME message: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Failed to build message: %v", err)})
	}
	if mimeSize > maxOutgoingMessageBytes {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "Encoded email exceeds the 25 MiB message size limit"})
	}

	// Collect all RCPT TO addresses (To + CC + BCC).
	var allRcpts []string
	for _, a := range api.ParseAddressField(to) {
		allRcpts = append(allRcpts, a.Email)
	}
	for _, a := range api.ParseAddressField(cc) {
		allRcpts = append(allRcpts, a.Email)
	}
	for _, a := range api.ParseAddressField(bcc) {
		allRcpts = append(allRcpts, a.Email)
	}

	// Create SMTP client — use the specific account when replying from unified view.
	var smtpClient *api.SMTPClient
	if replyAccountEmail != "" && replyAccountEmail != h.auth.GetSessionEmail(c) {
		if account, ok := h.mirrorAccountForEmail(c, replyAccountEmail); ok {
			smtpClient, err = h.auth.CreateSMTPClientForMirrorAccount(account)
		} else if h.mailDB != nil {
			err = fmt.Errorf("account %s not found", replyAccountEmail)
		} else if h.acctStore != nil && h.config.Accounts.Enabled {
			// Legacy bbolt account path.
			username, _ := c.Locals("username").(string)
			entries, listErr := h.acctStore.List(username)
			if listErr == nil {
				for _, e := range entries {
					if e.Email == replyAccountEmail {
						smtpClient, err = h.auth.CreateSMTPClientForAccount(e)
						break
					}
				}
			}
			if smtpClient == nil && err == nil {
				err = fmt.Errorf("account %s not found", replyAccountEmail)
			}
		}
	}
	if smtpClient == nil && err == nil {
		smtpClient, err = h.auth.CreateSMTPClient(c)
	}
	if err != nil {
		log.Printf("SMTP client creation error: %v", err)
		return c.Status(500).JSON(fiber.Map{
			"error": "Failed to connect to email server",
		})
	}

	if mimeFile != nil {
		_, err = mimeFile.Seek(0, io.SeekStart)
		if err == nil {
			err = smtpClient.SendRawMessageReader(allRcpts, mimeFile)
		}
	} else {
		err = smtpClient.SendRawMessage(allRcpts, rawMessage)
	}
	if err != nil {
		log.Printf("Email sending error: %v", err)
		return c.Status(500).JSON(fiber.Map{
			"error": fmt.Sprintf("Failed to send email: %v", err),
		})
	}

	// Record recipients for autocomplete.
	username, _ := c.Locals("username").(string)
	if username != "" {
		dbPath := recipientsStorePath(h.config.Cache.Folder, username)
		if rs, err := api.OpenRecipientsStore(dbPath); err == nil {
			defer rs.Close()
			var entries []api.RecipientEntry
			entries = append(entries, api.ParseAddressField(to)...)
			entries = append(entries, api.ParseAddressField(cc)...)
			if err := rs.Record(entries); err != nil {
				log.Printf("compose: record recipients: %v", err)
			}
		}
	}

	// Save to Sent folder (best effort) — use the reply account's IMAP if needed.
	var imapClient api.MailClient
	if replyAccountEmail != "" && replyAccountEmail != h.auth.GetSessionEmail(c) {
		if account, ok := h.mirrorAccountForEmail(c, replyAccountEmail); ok {
			imapClient, err = h.auth.CreateIMAPClientForMirrorAccount(account)
		} else if h.mailDB != nil {
			err = fmt.Errorf("account %s not found", replyAccountEmail)
		} else if h.acctStore != nil && h.config.Accounts.Enabled {
			username, _ := c.Locals("username").(string)
			entries, listErr := h.acctStore.List(username)
			if listErr == nil {
				for _, e := range entries {
					if e.Email == replyAccountEmail {
						imapClient, err = h.auth.CreateIMAPClientForAccount(e)
						break
					}
				}
			}
		}
	}
	if imapClient == nil && err == nil {
		imapClient, err = h.auth.CreateIMAPClient(c)
	}
	if err != nil {
		log.Printf("IMAP client error when saving to Sent: %v", err)
	} else {
		defer imapClient.Close()
		var (
			saveErr    error
			sentFolder string
		)
		if mimeFile != nil {
			if _, seekErr := mimeFile.Seek(0, io.SeekStart); seekErr != nil {
				saveErr = seekErr
			} else if streamingClient, ok := imapClient.(interface {
				SaveToSentReaderWithFolder(io.Reader, int) (string, error)
			}); ok {
				sentFolder, saveErr = streamingClient.SaveToSentReaderWithFolder(mimeFile, int(mimeSize))
			} else if streamingClient, ok := imapClient.(interface {
				SaveToSentReader(io.Reader, int) error
			}); ok {
				saveErr = streamingClient.SaveToSentReader(mimeFile, int(mimeSize))
			} else {
				var buffered []byte
				buffered, saveErr = io.ReadAll(mimeFile)
				if saveErr == nil {
					saveErr = imapClient.SaveToSent(to, subject, plainBody, buffered)
				}
			}
		} else {
			if folderClient, ok := imapClient.(interface {
				SaveToSentWithFolder(string, string, string, []byte) (string, error)
			}); ok {
				sentFolder, saveErr = folderClient.SaveToSentWithFolder(to, subject, plainBody, rawMessage)
			} else {
				saveErr = imapClient.SaveToSent(to, subject, plainBody, rawMessage)
			}
		}
		if saveErr != nil {
			log.Printf("Error saving to Sent folder: %v", saveErr)
		} else if sentFolder != "" && h.auth.syncer != nil {
			if account, ok := h.mirrorAccountForEmail(c, fromEmail); ok {
				syncCtx, cancel := context.WithTimeout(c.UserContext(), 15*time.Second)
				if syncErr := h.auth.syncer.SyncNewMessagesNow(syncCtx, account.ID, sentFolder); syncErr != nil {
					log.Printf("mail mirror: refresh sent folder after compose: %v", syncErr)
					h.auth.syncer.Trigger(account.ID)
				}
				cancel()
			}
		}
		// If this was a draft, delete it from the Drafts folder.
		if draftUID != "" {
			if err := imapClient.DeleteDraft(draftUID); err != nil {
				log.Printf("compose: delete draft %s: %v", draftUID, err)
			}
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Email sent successfully",
		"details": fiber.Map{
			"to":      to,
			"subject": subject,
		},
	})
}

// HandleSaveDraft saves or updates a draft message in the IMAP Drafts folder.
// Route: POST /api/draft
// Form fields: to, cc, bcc, subject, body, html_body, in_reply_to, references, draft_uid
// If draft_uid is set, the old draft is deleted before saving the new one.
func (h *EmailHandler) HandleSaveDraft(c *fiber.Ctx) error {
	to := c.FormValue("to")
	subject := c.FormValue("subject")
	plainBody := c.FormValue("body")
	htmlBody := c.FormValue("html_body")
	cc := c.FormValue("cc")
	inReplyTo := c.FormValue("in_reply_to")
	references := c.FormValue("references")
	oldUID := c.FormValue("draft_uid")
	accountEmail := strings.TrimSpace(c.FormValue("account_email"))

	if subject == "" && plainBody == "" && htmlBody == "" && to == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Draft is empty"})
	}

	var normalizeErr error
	plainBody, htmlBody, normalizeErr = htmlsafe.NormalizeComposeBodies(plainBody, htmlBody)
	if normalizeErr != nil {
		log.Printf("draft: normalize HTML body: %v", normalizeErr)
		return c.Status(400).JSON(fiber.Map{"error": "HTML body is invalid or too large"})
	}

	fromEmail := h.auth.GetSessionEmail(c)
	if accountEmail != "" {
		fromEmail = accountEmail
	}

	mimeOpts := api.MIMEMessageOptions{
		From:       fromEmail,
		To:         to,
		Cc:         cc,
		Subject:    subject,
		InReplyTo:  inReplyTo,
		References: references,
		PlainBody:  plainBody,
		HTMLBody:   htmlBody,
	}
	rawMessage, err := api.BuildMIMEMessage(mimeOpts)
	if err != nil {
		// If body is truly empty, build a minimal skeleton.
		rawMessage = []byte(fmt.Sprintf(
			"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\n",
			fromEmail, to, subject,
		))
	}

	imapClient, _, err := h.messageClientForAccount(c, accountEmail)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to connect to mail server"})
	}
	defer imapClient.Close()

	// Delete the previous version of the draft before saving the new one.
	if oldUID != "" {
		if err := imapClient.DeleteDraft(oldUID); err != nil {
			log.Printf("draft: delete old %s: %v", oldUID, err)
		}
	}

	if err := imapClient.SaveDraft(rawMessage); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Failed to save draft: %v", err)})
	}

	return c.JSON(fiber.Map{"success": true, "message": "Draft saved"})
}

// HandleListDrafts returns draft messages as an email-list partial.
// Route: GET /api/drafts
func (h *EmailHandler) HandleListDrafts(c *fiber.Ctx) error {
	username, _ := c.Locals("username").(string)

	token, err := api.GetSessionToken(c, h.store)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid session"})
	}

	imapClient, err := h.auth.CreateIMAPClient(c)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to connect to mail server"})
	}
	defer imapClient.Close()

	draftsFolder, err := imapClient.DiscoverDraftsFolder()
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "No Drafts folder found"})
	}

	emails, err := imapClient.FetchMessages(draftsFolder, 50)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Failed to fetch drafts: %v", err)})
	}

	threads := h.buildThreads(username, draftsFolder, emails)

	return c.JSON(fiber.Map{
		"Emails":        emails,
		"Threads":       threads,
		"CurrentFolder": draftsFolder,
		"IsDrafts":      true,
		"Token":         token,
	})
}

// HandleAutocomplete returns recipient suggestions for the compose modal.
// Route: GET /api/autocomplete?q=<query>
// Returns JSON array of {email, name} objects.
func (h *EmailHandler) HandleAutocomplete(c *fiber.Ctx) error {
	query := strings.TrimSpace(c.Query("q"))
	username, _ := c.Locals("username").(string)

	const limit = 10

	// Recent recipients from bbolt.
	var results []api.RecipientEntry
	if username != "" {
		dbPath := recipientsStorePath(h.config.Cache.Folder, username)
		if rs, err := api.OpenRecipientsStore(dbPath); err == nil {
			defer rs.Close()
			if res, err := rs.Search(query, limit); err == nil {
				results = res
			}
		}
	}

	// CardDAV contacts (if configured and we haven't hit the limit).
	if len(results) < limit && h.config.CardDAV.Enabled {
		remaining := limit - len(results)
		cardContacts := api.CardDAVContacts(
			h.config.CardDAV.URL,
			h.config.CardDAV.Username,
			h.config.CardDAV.Password,
			query,
			remaining,
		)
		// Deduplicate: skip addresses already in results.
		seen := make(map[string]bool)
		for _, r := range results {
			seen[strings.ToLower(r.Email)] = true
		}
		for _, r := range cardContacts {
			if !seen[strings.ToLower(r.Email)] {
				results = append(results, r)
				seen[strings.ToLower(r.Email)] = true
			}
		}
	}

	// Return simple JSON array.
	type suggestion struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	out := make([]suggestion, 0, len(results))
	for _, r := range results {
		out = append(out, suggestion{Email: r.Email, Name: r.Name})
	}
	return c.JSON(out)
}

// editableDraftHTML returns a sanitized copy of a message's HTML body that is
// safe to place into a browser compose editor. A rich-text editor lives in the
// main app document, which — unlike the sandboxed reading-pane iframe — is not
// script-isolated, so raw attacker-controlled mail HTML would run
// (e.g. <img src=x onerror=...>, <svg onload=...>). We defang it with the shared
// htmlsafe policy (strips script/style/iframe/svg/forms, all on* handlers, and
// javascript:/vbscript:/data: URLs) while keeping benign formatting.
func editableDraftHTML(rawHTML string) string {
	if rawHTML == "" {
		return ""
	}
	return htmlsafe.SanitizeSnippet(rawHTML)
}

// stripHTMLForPlain produces a minimal plain-text version of an HTML string by
// stripping tags and collapsing whitespace. Used to auto-generate the
// text/plain alternative when only HTML body is provided.
func stripHTMLForPlain(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
			b.WriteByte(' ')
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse runs of whitespace.
	return strings.Join(strings.Fields(b.String()), " ")
}

// HandleMarkUnread removes the \Seen flag from a message, marking it as unread.
// Route: PATCH /api/email/:id/unread
// Requires Authorization: Bearer <jwt> and X-Folder (or ?folder=) query param.
func (h *EmailHandler) HandleMarkUnread(c *fiber.Ctx) error {
	emailID := c.Params("id")
	if emailID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Email ID required"})
	}

	folderName := c.Get("X-Folder")
	if folderName == "" {
		folderName = c.Query("folder")
		if folderName == "" {
			folderName = "INBOX"
		}
	}

	client, account, err := h.messageClientForAccount(c, requestAccountEmail(c))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Error connecting to email server"})
	}
	defer client.Close()

	// Remove \Seen flag to mark the message as unread.
	if err := client.SetMessageFlag(folderName, emailID, `\Seen`, false); err != nil {
		log.Printf("HandleMarkUnread: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Failed to mark unread: %v", err)})
	}
	if account.ID != "" {
		if email, dbErr := h.mailDB.GetMessage(c.UserContext(), account.ID, folderName, emailID); dbErr == nil {
			var flags []string
			for _, flag := range email.Flags {
				if flag != `\Seen` {
					flags = append(flags, flag)
				}
			}
			if dbErr := h.mailDB.UpdateFlags(c.UserContext(), account.ID, folderName, emailID, flags); dbErr != nil {
				log.Printf("mail mirror: mark unread %s/%s: %v", folderName, emailID, dbErr)
			} else if dbErr := h.mailDB.UpdateFolderStats(c.UserContext(), account.ID, folderName); dbErr != nil {
				log.Printf("mail mirror: recount after mark unread %s/%s: %v", folderName, emailID, dbErr)
			}
		}
	}

	return c.JSON(fiber.Map{"success": true, "message": "Marked as unread"})
}

// HandleSearch performs an IMAP SEARCH and returns matching messages as an
// email-list partial.
// Route: GET /api/search?q=<query>&folder=<folder>
func (h *EmailHandler) HandleSearch(c *fiber.Ctx) error {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		return c.Status(400).JSON(fiber.Map{"error": "q parameter required"})
	}

	folderName := c.Query("folder")
	if folderName == "" {
		folderName = "INBOX"
	}

	username := c.Locals("username")
	userStr := ""
	if u, ok := username.(string); ok {
		userStr = u
	}

	token, err := api.GetSessionToken(c, h.store)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "Invalid session"})
	}

	var emails []models.Email
	account, hasMirrorAccount := h.currentMirrorAccount(c)
	usedLocal := false
	if hasMirrorAccount {
		var localFolder string
		emails, localFolder, err = h.mailDB.SearchMessages(c.UserContext(), account.ID, folderName, query, 50)
		if err == nil {
			folderName = localFolder
			emails = tagMirrorEmails(emails, account)
			usedLocal = true
		}
	}
	if !usedLocal {
		client, clientErr := h.auth.CreateIMAPClient(c)
		if clientErr != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Error connecting to email server"})
		}
		emails, err = client.SearchMessages(folderName, query, 50)
		_ = client.Close()
	}
	if err != nil {
		log.Printf("HandleSearch: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Search failed: %v", err)})
	}

	threads := h.buildThreads(userStr, folderName+":search:"+query, emails)

	return c.JSON(fiber.Map{
		"Emails":        emails,
		"Threads":       threads,
		"CurrentFolder": folderName,
		"Token":         token,
		"SearchQuery":   query,
	})
}
