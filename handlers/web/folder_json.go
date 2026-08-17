package web

import (
	"errors"
	"fmt"
	stdhtml "html"
	"lilmail/handlers/api"
	"lilmail/mailstore"
	"lilmail/models"
	"lilmail/storage"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const maxLocalFolderPageSize = 200

var (
	errSpamFolderUnavailable  = errors.New("spam folder unavailable")
	errMessageOutsideSpam     = errors.New("message is outside spam folder")
	errAttachmentCacheCleanup = errors.New("attachment cache cleanup failed")
)

type junkMessageMutationClient interface {
	DiscoverJunkFolder() (string, error)
	MoveMessage(srcFolder, uid, destFolder string) error
	DeleteMessage(folder, uid string) error
}

func applyRemoteJunkMessageMutation(client junkMessageMutationClient, folder, uid string, permanent bool, beforeMutation func() error) error {
	junk, err := client.DiscoverJunkFolder()
	if err != nil || strings.TrimSpace(junk) == "" {
		return errSpamFolderUnavailable
	}
	if !strings.EqualFold(strings.TrimSpace(junk), strings.TrimSpace(folder)) {
		return errMessageOutsideSpam
	}
	if beforeMutation != nil {
		if err := beforeMutation(); err != nil {
			return fmt.Errorf("%w: %v", errAttachmentCacheCleanup, err)
		}
	}
	if permanent {
		if err := client.DeleteMessage(folder, uid); err != nil {
			return fmt.Errorf("permanently delete junk message: %w", err)
		}
		return nil
	}
	if err := client.MoveMessage(folder, uid, "INBOX"); err != nil {
		return fmt.Errorf("restore junk message: %w", err)
	}
	return nil
}

func deleteMessageAttachmentCache(c *fiber.Ctx, client api.MailClient, accountID string, message models.Email) error {
	objectStore, enabled := storage.ObjectStoreFromHeaders(func(key string) string { return c.Get(key) })
	if !enabled {
		return nil
	}
	if !message.AttachmentMetadataCached {
		metadataClient, ok := client.(interface {
			FetchAttachmentMetadata(folderName, uid string) ([]models.Attachment, error)
		})
		if !ok {
			return errors.New("mail client cannot load attachment metadata")
		}
		attachments, err := metadataClient.FetchAttachmentMetadata(message.Folder, message.ID)
		if err != nil {
			return fmt.Errorf("load attachment metadata: %w", err)
		}
		message.Attachments = attachments
	}
	if accountID == "" {
		accountID = "session"
	}
	for _, key := range messageAttachmentCacheKeys(accountID, message) {
		if err := objectStore.Delete(c.UserContext(), key); err != nil {
			return err
		}
	}
	return nil
}

func messageAttachmentCacheKeys(accountID string, message models.Email) []string {
	keys := make(map[string]struct{}, len(message.Attachments))
	for _, attachment := range message.Attachments {
		if id := strings.TrimSpace(attachment.ID); id != "" {
			keys["attachments/"+accountID+"/"+id] = struct{}{}
		}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	return result
}

// FolderMessageSummaryJSON is the lightweight local read model used by folder
// lists. Bodies and attachment arrays are deliberately reserved for the detail
// endpoint so opening a large folder stays cheap.
type FolderMessageSummaryJSON struct {
	ID             string    `json:"id"`
	Folder         string    `json:"folder"`
	From           string    `json:"from"`
	FromName       string    `json:"fromName,omitempty"`
	To             string    `json:"to"`
	Subject        string    `json:"subject"`
	Preview        string    `json:"preview"`
	Date           time.Time `json:"date"`
	HasAttachments bool      `json:"hasAttachments"`
	Flags          []string  `json:"flags,omitempty"`
	AccountEmail   string    `json:"accountEmail,omitempty"`
	AccountLabel   string    `json:"accountLabel,omitempty"`
	AccountColor   string    `json:"accountColor,omitempty"`
}

type LocalFolderMessagesJSON struct {
	Folder       string                     `json:"folder"`
	Messages     []FolderMessageSummaryJSON `json:"messages"`
	Limit        int                        `json:"limit"`
	Offset       int                        `json:"offset"`
	NextOffset   *int                       `json:"nextOffset"`
	SyncComplete bool                       `json:"syncComplete"`
	LastSyncAt   *time.Time                 `json:"lastSyncAt,omitempty"`
	SyncError    string                     `json:"syncError,omitempty"`
}

type FolderMessageDetailJSON struct {
	models.Email
	MailSummary *MailSummaryJSON `json:"mailSummary,omitempty"`
}

func localFolderPageInt(c *fiber.Ctx, key string, fallback int) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func folderMessageSummaryJSON(email models.Email, account mailstore.Account) FolderMessageSummaryJSON {
	return FolderMessageSummaryJSON{
		ID:             email.ID,
		Folder:         email.Folder,
		From:           email.From,
		FromName:       email.FromName,
		To:             email.To,
		Subject:        stdhtml.UnescapeString(email.Subject),
		Preview:        stdhtml.UnescapeString(email.Preview),
		Date:           email.Date,
		HasAttachments: email.HasAttachments || messageHasAttachments(email),
		Flags:          email.Flags,
		AccountEmail:   account.Email,
		AccountLabel:   account.Label,
		AccountColor:   account.Color,
	}
}

func folderMessageDetailJSON(email models.Email, account mailstore.Account, locale string) models.Email {
	email.AccountEmail = account.Email
	email.AccountLabel = account.Label
	email.AccountColor = account.Color
	email.Subject = stdhtml.UnescapeString(email.Subject)
	email.Preview = stdhtml.UnescapeString(email.Preview)
	email.Body = stdhtml.UnescapeString(email.Body)
	email.Attachments = api.MarkInlineAttachmentsFromHTML(email.HTML, email.Attachments)
	if strings.TrimSpace(email.HTML) != "" {
		htmlBody := collapseQuotedHTMLForLocale(email.HTML, locale)
		email.HTML, _ = prepareEmailHTML(rewriteInlineCIDReferences(htmlBody, email.Attachments, account.Email))
	}
	email.HasAttachments = email.HasAttachments || messageHasAttachments(email)
	email.Attachments = regularAttachments(email.Attachments)
	return email
}

func (h *EmailHandler) localFolderAccount(c *fiber.Ctx) (mailstore.Account, error) {
	accountEmail := strings.TrimSpace(c.Query("account_email"))
	if accountEmail != "" {
		if account, ok := h.mirrorAccountForEmail(c, accountEmail); ok {
			return account, nil
		}
		return mailstore.Account{}, mailstore.ErrNotFound
	}
	if account, ok := h.currentMirrorAccount(c); ok {
		return account, nil
	}
	return mailstore.Account{}, mailstore.ErrNotFound
}

// HandleLocalFoldersJSON returns the synchronized folder catalog without an
// IMAP round trip. Newly discovered remote folders appear here after the
// background worker's next folder scan.
func (h *EmailHandler) HandleLocalFoldersJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	account, err := h.localFolderAccount(c)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	folders, err := h.mailDB.ListFolders(c.UserContext(), account.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read local folders"})
	}
	return c.JSON(fiber.Map{"folders": mirrorMailboxInfos(folders)})
}

// HandleLocalFolderMessagesJSON lists one synchronized folder entirely from
// SQLite. IMAP remains the source of truth, but is only contacted by the
// background synchronizer and mutation handlers.
func (h *EmailHandler) HandleLocalFolderMessagesJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	account, err := h.localFolderAccount(c)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	folder := strings.TrimSpace(c.Query("folder"))
	if folder == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Folder is required"})
	}
	state, err := h.mailDB.GetSyncState(c.UserContext(), account.ID, folder)
	if errors.Is(err, mailstore.ErrNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Folder not found in local mirror"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read folder sync state"})
	}
	limit := localFolderPageInt(c, "limit", 100)
	if limit == 0 || limit > maxLocalFolderPageSize {
		limit = maxLocalFolderPageSize
	}
	offset := localFolderPageInt(c, "offset", 0)
	emails, err := h.mailDB.ListMessages(c.UserContext(), account.ID, folder, limit, offset)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read local messages"})
	}
	messages := make([]FolderMessageSummaryJSON, 0, len(emails))
	for _, email := range emails {
		messages = append(messages, folderMessageSummaryJSON(email, account))
	}
	var nextOffset *int
	if len(messages) == limit {
		next := offset + limit
		nextOffset = &next
	}
	response := LocalFolderMessagesJSON{
		Folder:       folder,
		Messages:     messages,
		Limit:        limit,
		Offset:       offset,
		NextOffset:   nextOffset,
		SyncComplete: state.SyncComplete,
		SyncError:    state.LastError,
	}
	if !state.LastSyncAt.IsZero() {
		lastSyncAt := state.LastSyncAt.UTC()
		response.LastSyncAt = &lastSyncAt
	}
	return c.JSON(response)
}

// HandleLocalFolderMessageJSON returns a cached full message. A 409 response
// tells the client that the header exists but its body is still synchronizing.
func (h *EmailHandler) HandleLocalFolderMessageJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	account, err := h.localFolderAccount(c)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	folder := strings.TrimSpace(c.Query("folder"))
	if folder == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Folder is required"})
	}
	if _, err := h.mailDB.GetSyncState(c.UserContext(), account.ID, folder); errors.Is(err, mailstore.ErrNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Folder not found in local mirror"})
	} else if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read folder sync state"})
	}
	email, err := h.mailDB.GetMessage(c.UserContext(), account.ID, folder, strings.TrimSpace(c.Params("uid")))
	if errors.Is(err, mailstore.ErrNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Message not found"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read local message"})
	}
	if !emailBodyCached(email) {
		return mailBodyNotReady(c)
	}
	response := FolderMessageDetailJSON{Email: folderMessageDetailJSON(email, account, CurrentLocale(c))}
	if record, summaryErr := h.mailDB.GetMessageSummary(c.UserContext(), mailstore.MessageSummaryKey{AccountID: account.ID, FolderName: folder, UID: email.ID}); summaryErr == nil {
		configHash, _ := mailstore.CurrentMailSummaryConfigHash(c.UserContext(), h.mailDB, account.OwnerID)
		sourceHash, sourceErr := mailstore.CurrentMailSummarySourceHash(c.UserContext(), h.mailDB, account, email)
		stale := sourceErr != nil || record.SourceHash != sourceHash || (configHash != "" && record.ConfigHash != configHash)
		response.MailSummary = mailSummaryJSON(record, stale)
	} else if !errors.Is(summaryErr, mailstore.ErrNotFound) {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read saved mail summary"})
	}
	return c.JSON(response)
}

func (h *EmailHandler) localJunkMessageMutation(c *fiber.Ctx, permanent bool) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	account, err := h.localFolderAccount(c)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	folder := strings.TrimSpace(c.Query("folder"))
	uid := strings.TrimSpace(c.Params("uid"))
	if folder == "" || uid == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Message folder and UID are required"})
	}
	email, err := h.mailDB.GetMessage(c.UserContext(), account.ID, folder, uid)
	if errors.Is(err, mailstore.ErrNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Message not found"})
	} else if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read local message"})
	}

	client, _, err := h.messageClientForAccount(c, account.Email)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Could not connect to the mail server"})
	}
	defer client.Close()
	err = applyRemoteJunkMessageMutation(client, folder, uid, permanent, func() error {
		return deleteMessageAttachmentCache(c, client, account.ID, email)
	})
	if errors.Is(err, errSpamFolderUnavailable) {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Spam folder could not be identified"})
	}
	if errors.Is(err, errMessageOutsideSpam) {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "This action is only available in the spam folder"})
	}
	if errors.Is(err, errAttachmentCacheCleanup) {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Could not clear the attachment cache; the email was not changed"})
	}
	if err != nil {
		message := "Could not move the email to Inbox"
		if permanent {
			message = "Could not permanently delete the email"
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": message})
	}
	if err := h.mailDB.DeleteMessage(c.UserContext(), account.ID, folder, uid); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The email was changed on the mail server, but the local mailbox could not be updated"})
	}
	if err := h.mailDB.UpdateFolderStats(c.UserContext(), account.ID, folder); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The email was changed, but the local folder count could not be updated"})
	}
	response := fiber.Map{"ok": true}
	if !permanent {
		response["folder"] = "INBOX"
	}
	return c.JSON(response)
}

// HandleLocalJunkMessageRestoreJSON marks a mirrored Junk message as not spam
// by moving it back to the account's standard INBOX mailbox.
func (h *EmailHandler) HandleLocalJunkMessageRestoreJSON(c *fiber.Ctx) error {
	return h.localJunkMessageMutation(c, false)
}

// HandleLocalJunkMessageDeleteJSON permanently expunges a message. The handler
// deliberately rejects messages outside the server-discovered Junk mailbox.
func (h *EmailHandler) HandleLocalJunkMessageDeleteJSON(c *fiber.Ctx) error {
	return h.localJunkMessageMutation(c, true)
}
