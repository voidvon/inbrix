package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	stdhtml "html"
	"inbrix/handlers/api"
	"inbrix/i18n"
	"inbrix/mailstore"
	"inbrix/models"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/net/html"
)

// ConversationSummaryJSON is the compact row model consumed by the React
// conversation list. Message bodies are deliberately absent from this type.
type ConversationSummaryJSON struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	PeerEmail      string `json:"peerEmail,omitempty"`
	Subject        string `json:"subject"`
	Preview        string `json:"preview"`
	Date           string `json:"date"`
	Count          int    `json:"count"`
	UnreadCount    int    `json:"unreadCount"`
	HasAttachments bool   `json:"hasAttachments"`
	AccountEmail   string `json:"accountEmail,omitempty"`
	AccountLabel   string `json:"accountLabel,omitempty"`
	AccountColor   string `json:"accountColor,omitempty"`
	Note           string `json:"note,omitempty"`
	Status         string `json:"status"`
}

// ConversationMessageJSON is the detail model for one chat bubble. HTML is
// prepared for a script-free sandbox iframe and has local cid: references
// rewritten to authenticated attachment URLs before it reaches the client.
type ConversationMessageJSON struct {
	SendStatus         string               `json:"sendStatus,omitempty"`
	SendError          string               `json:"sendError,omitempty"`
	SendRetryable      *bool                `json:"sendRetryable,omitempty"`
	ID                 string               `json:"id"`
	Folder             string               `json:"folder,omitempty"`
	From               string               `json:"from"`
	FromName           string               `json:"fromName,omitempty"`
	To                 string               `json:"to"`
	Cc                 string               `json:"cc,omitempty"`
	Subject            string               `json:"subject"`
	Preview            string               `json:"preview"`
	Body               string               `json:"body"`
	HTML               string               `json:"html,omitempty"`
	Date               string               `json:"date"`
	HasAttachments     bool                 `json:"hasAttachments"`
	Flags              []string             `json:"flags,omitempty"`
	Attachments        []models.Attachment  `json:"attachments,omitempty"`
	MessageID          string               `json:"messageId,omitempty"`
	InReplyTo          string               `json:"inReplyTo,omitempty"`
	References         []string             `json:"references,omitempty"`
	Outgoing           bool                 `json:"outgoing"`
	MailSummary        *MailSummaryJSON     `json:"mailSummary,omitempty"`
	SuggestedReply     *SuggestedReplyJSON  `json:"suggestedReply,omitempty"`
	MailTranslation    *MailTranslationJSON `json:"mailTranslation,omitempty"`
	MailTranslationAll *MailTranslationJSON `json:"mailTranslationAll,omitempty"`
}

type MailSummaryJSON struct {
	Text      string `json:"text"`
	Status    string `json:"status"`
	Stale     bool   `json:"stale"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type MailTranslationJSON struct {
	Text      string `json:"text"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type SuggestedReplyJSON struct {
	Text      string `json:"text"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func mailSummaryJSON(record mailstore.MessageSummaryRecord, stale bool) *MailSummaryJSON {
	if record.Status != "ready" || strings.TrimSpace(record.Summary) == "" {
		return nil
	}
	return &MailSummaryJSON{Text: record.Summary, Status: record.Status, Stale: stale, UpdatedAt: record.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
}

func mailTranslationJSON(record mailstore.MessageSummaryRecord) *MailTranslationJSON {
	if record.Status != "ready" || strings.TrimSpace(record.Summary) == "" {
		return nil
	}
	return &MailTranslationJSON{Text: mailstore.CleanTranslationText(record.Summary), Status: record.Status, UpdatedAt: record.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
}

type ConversationDetailJSON struct {
	ID           string                    `json:"id"`
	Title        string                    `json:"title"`
	PeerEmail    string                    `json:"peerEmail,omitempty"`
	Subject      string                    `json:"subject"`
	Note         string                    `json:"note"`
	Count        int                       `json:"count"`
	AccountEmail string                    `json:"accountEmail,omitempty"`
	AccountLabel string                    `json:"accountLabel,omitempty"`
	AccountColor string                    `json:"accountColor,omitempty"`
	Messages     []ConversationMessageJSON `json:"messages"`
}

type ConversationAccountErrorJSON struct {
	AccountEmail string `json:"accountEmail"`
	Message      string `json:"message"`
}

type ConversationAccountJSON struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Label    string `json:"label"`
	Color    string `json:"color,omitempty"`
	IsActive bool   `json:"isActive"`
}

type ConversationListJSON struct {
	Conversations    []ConversationSummaryJSON      `json:"conversations"`
	Folders          []*api.MailboxInfo             `json:"folders"`
	Accounts         []ConversationAccountJSON      `json:"accounts"`
	AccountEmail     string                         `json:"accountEmail"`
	AccountErrors    []ConversationAccountErrorJSON `json:"accountErrors,omitempty"`
	Locale           string                         `json:"locale"`
	Unified          bool                           `json:"unified"`
	UnifiedAvailable bool                           `json:"unifiedAvailable"`
	// Revision increments whenever the realtime pipeline publishes a change
	// for this user. Clients echo it on reconnect so an unchanged mailbox can
	// be answered without rebuilding the conversation list.
	Revision uint64 `json:"revision"`
	// Unchanged is true only for the compact response used by the revision
	// fast path; all other fields are empty in that case.
	Unchanged bool `json:"unchanged,omitempty"`
	// NextCursor is an opaque cursor for the next page. It is empty when this
	// is the last page or when the caller asked for the legacy unpaginated list.
	NextCursor string `json:"nextCursor,omitempty"`
	HasMore    bool   `json:"hasMore"`
	// Total is the number of conversations matching the query before paging.
	Total int `json:"total"`
	// UnreadCount is the mailbox-wide unread message count, not just the rows
	// on this page. The sidebar badge and tab title depend on it.
	UnreadCount int `json:"unreadCount"`
}

const conversationMaxPageSize = 100

var errInvalidConversationCursor = errors.New("invalid conversation cursor")

type conversationCursor struct {
	Date time.Time
	ID   string
}

func encodeConversationCursor(conversation Conversation) string {
	raw := strconv.FormatInt(conversation.Latest.Date.UnixNano(), 10) + "\x00" + conversation.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeConversationCursor(value string) (conversationCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return conversationCursor{}, errInvalidConversationCursor
	}
	parts := strings.SplitN(string(decoded), "\x00", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return conversationCursor{}, errInvalidConversationCursor
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return conversationCursor{}, errInvalidConversationCursor
	}
	return conversationCursor{Date: time.Unix(0, nanos), ID: parts[1]}, nil
}

// conversationBeforeOrAtCursor reports whether c is at or above the cursor in
// the list order (latest first, then conversation ID ascending as a stable
// tie). Those rows were already returned by the previous page.
func conversationBeforeOrAtCursor(conversation Conversation, cursor conversationCursor) bool {
	if conversation.Latest.Date.After(cursor.Date) {
		return true
	}
	if conversation.Latest.Date.Before(cursor.Date) {
		return false
	}
	return conversation.ID <= cursor.ID
}

func conversationListLimit(c *fiber.Ctx) int {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		// Legacy callers that do not pass limit still receive the complete list.
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0
	}
	if limit > conversationMaxPageSize {
		return conversationMaxPageSize
	}
	return limit
}

func conversationSummaryJSON(conversation Conversation) ConversationSummaryJSON {
	return ConversationSummaryJSON{
		ID:             conversation.ID,
		Title:          conversation.Title,
		PeerEmail:      conversation.PeerEmail,
		Subject:        conversation.Subject,
		Preview:        conversation.Preview,
		Date:           conversation.Latest.Date.UTC().Format("2006-01-02T15:04:05.999Z07:00"),
		Count:          conversation.Count,
		UnreadCount:    conversation.UnreadCount,
		HasAttachments: conversation.HasAttachments,
		AccountEmail:   conversation.AccountEmail,
		AccountLabel:   conversation.AccountLabel,
		AccountColor:   conversation.AccountColor,
		Note:           conversation.Note,
		Status:         conversation.Status,
	}
}

func conversationDetailJSON(conversation Conversation) ConversationDetailJSON {
	return conversationDetailJSONForLocale(conversation, i18n.LocaleEnglish)
}

func conversationDetailJSONForLocale(conversation Conversation, locale string) ConversationDetailJSON {
	result := ConversationDetailJSON{
		ID:           conversation.ID,
		Title:        conversation.Title,
		PeerEmail:    conversation.PeerEmail,
		Subject:      conversation.Subject,
		Note:         conversation.Note,
		Count:        conversation.Count,
		AccountEmail: conversation.AccountEmail,
		AccountLabel: conversation.AccountLabel,
		AccountColor: conversation.AccountColor,
		Messages:     make([]ConversationMessageJSON, 0, len(conversation.Messages)),
	}
	for _, message := range conversation.Messages {
		email := message.Email
		email.Attachments = api.MarkInlineAttachmentsFromHTML(email.HTML, email.Attachments)
		body := email.Body
		if strings.TrimSpace(body) == "" {
			body = email.Preview
		}
		body = stdhtml.UnescapeString(body)
		preparedHTML := ""
		if strings.TrimSpace(email.HTML) != "" {
			htmlBody := collapseQuotedHTMLForLocale(email.HTML, locale)
			preparedHTML, _ = prepareEmailHTML(rewriteInlineCIDReferences(htmlBody, email.Attachments, conversation.AccountEmail))
		}
		var retryable *bool
		if email.Folder == "Outbox" {
			value := email.HasSendPayload
			retryable = &value
		}
		attachments := regularAttachments(email.Attachments)
		result.Messages = append(result.Messages, ConversationMessageJSON{
			SendStatus:     email.SendStatus,
			SendError:      email.SendError,
			SendRetryable:  retryable,
			ID:             email.ID,
			Folder:         email.Folder,
			From:           email.From,
			FromName:       email.FromName,
			To:             email.To,
			Cc:             email.Cc,
			Subject:        stdhtml.UnescapeString(email.Subject),
			Preview:        stdhtml.UnescapeString(email.Preview),
			Body:           body,
			HTML:           preparedHTML,
			Date:           email.Date.UTC().Format("2006-01-02T15:04:05.999Z07:00"),
			HasAttachments: messageHasAttachments(email),
			Flags:          email.Flags,
			Attachments:    attachments,
			MessageID:      email.MessageID,
			InReplyTo:      email.InReplyTo,
			References:     email.References,
			Outgoing:       message.Outgoing,
		})
	}
	return result
}

func collapseQuotedHTML(raw string) string {
	return collapseQuotedHTMLForLocale(raw, i18n.LocaleEnglish)
}

func collapseQuotedHTMLForLocale(raw, locale string) string {
	document, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	collapseQuotedNodes(document, locale)
	var output strings.Builder
	if err := html.Render(&output, document); err != nil {
		return raw
	}
	return output.String()
}

func collapseQuotedNodes(node *html.Node, locale string) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if isQuotedHTMLNode(child) {
			wrapQuotedNodes(node, child, child, locale)
		} else {
			collapseQuotedNodes(child, locale)
		}
		child = next
	}
	// Some clients emit only a separator such as
	// "------------------ Original ------------------" and do not provide a
	// quoted-message container. From that separator onward, the siblings are
	// the old message and should be collapsed together.
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if isOriginalSeparatorNode(child) {
			wrapQuotedNodes(node, child, nil, locale)
			break
		}
	}
}

func wrapQuotedNodes(parent, first, last *html.Node, locale string) {
	details := &html.Node{Type: html.ElementNode, Data: "details", Attr: []html.Attribute{{Key: "class", Val: "inbrix-quoted"}}}
	summary := &html.Node{Type: html.ElementNode, Data: "summary"}
	summary.AppendChild(&html.Node{Type: html.TextNode, Data: i18n.Translate(locale, "Show quoted message")})
	parent.InsertBefore(details, first)
	for child := first; child != nil; {
		next := child.NextSibling
		parent.RemoveChild(child)
		details.AppendChild(child)
		if child == last {
			break
		}
		child = next
	}
	details.InsertBefore(summary, details.FirstChild)
}

var originalSeparatorRe = regexp.MustCompile(`(?i)^[-_\s]*original(?:\s+message)?[-_\s]*$`)

func isOriginalSeparatorNode(node *html.Node) bool {
	if node.Type != html.ElementNode || isQuotedHTMLNode(node) {
		return false
	}
	text := strings.Join(strings.Fields(nodeText(node)), " ")
	return originalSeparatorRe.MatchString(text) && strings.Contains(strings.ToLower(text), "original")
}

func nodeText(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	var text strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(nodeText(child))
	}
	return text.String()
}

func isQuotedHTMLNode(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	// Tencent/QQ Mail and several Outlook integrations use includetail as
	// the wrapper around the previous message instead of blockquote.
	if node.Data == "blockquote" || node.Data == "includetail" {
		return true
	}
	for _, attribute := range node.Attr {
		if attribute.Key != "class" && attribute.Key != "id" {
			continue
		}
		value := strings.ToLower(attribute.Val)
		if strings.Contains(value, "gmail_quote") || strings.Contains(value, "yahoo_quoted") ||
			strings.Contains(value, "moz-cite-prefix") || strings.Contains(value, "protonmail_quote") ||
			strings.Contains(value, "outlook_quote") || strings.Contains(value, "quoted-text") ||
			strings.Contains(value, "quotedcontent") || strings.Contains(value, "original-message") {
			return true
		}
	}
	return false
}

var cidReferenceRe = regexp.MustCompile(`(?i)cid:([^\s"'<>]+)`)

func rewriteInlineCIDReferences(raw string, attachments []models.Attachment, accountEmail string) string {
	byContentID := make(map[string]models.Attachment)
	for _, attachment := range attachments {
		if attachment.IsInline && strings.TrimSpace(attachment.ContentID) != "" {
			byContentID[strings.ToLower(strings.Trim(strings.TrimSpace(attachment.ContentID), "<>"))] = attachment
		}
	}
	if len(byContentID) == 0 {
		return raw
	}
	return cidReferenceRe.ReplaceAllStringFunc(raw, func(reference string) string {
		contentID := reference[len("cid:"):]
		if decoded, err := url.PathUnescape(contentID); err == nil {
			contentID = decoded
		}
		attachment, ok := byContentID[strings.ToLower(strings.Trim(contentID, "<>"))]
		if !ok {
			return reference
		}
		downloadURL := "/api/attachment/" + url.PathEscape(attachment.ID) + "?inline=true"
		if strings.HasPrefix(attachment.ID, "outbox_") {
			downloadURL = outgoingAttachmentURL(attachment.ID)
		}
		if accountEmail != "" && !strings.HasPrefix(attachment.ID, "outbox_") {
			downloadURL += "&amp;account_email=" + url.QueryEscape(accountEmail)
		}
		return downloadURL
	})
}

func regularAttachments(attachments []models.Attachment) []models.Attachment {
	regular := make([]models.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if !attachment.IsInline {
			regular = append(regular, attachment)
		}
	}
	return regular
}

func filterConversationJSON(conversations []Conversation, query string) []Conversation {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return conversations
	}
	filtered := make([]Conversation, 0, len(conversations))
	for _, conversation := range conversations {
		matched := strings.Contains(strings.ToLower(conversation.Title), query) ||
			strings.Contains(strings.ToLower(conversation.PeerEmail), query) ||
			strings.Contains(strings.ToLower(conversation.Subject), query) ||
			strings.Contains(strings.ToLower(conversation.Preview), query) ||
			strings.Contains(strings.ToLower(conversation.Note), query)
		if !matched {
			for _, message := range conversation.Messages {
				if strings.Contains(strings.ToLower(message.Email.From), query) ||
					strings.Contains(strings.ToLower(message.Email.To), query) ||
					strings.Contains(strings.ToLower(message.Email.Body), query) {
					matched = true
					break
				}
			}
		}
		if matched {
			filtered = append(filtered, conversation)
		}
	}
	return filtered
}

func (h *EmailHandler) conversationListJSON(c *fiber.Ctx, query string, filter string, limit int, cursor string) (ConversationListJSON, error) {
	// Revision fast path: a reconnect resync with no changes should not rebuild
	// every conversation. It applies to the first page only; a cursor request
	// must receive the rows after that cursor (or the client would keep an
	// empty page). A search query must always be evaluated.
	revision := uint64(0)
	if h.revisionSource != nil {
		if ownerID := requestOwnerID(c, h.store); ownerID != "" {
			revision = h.revisionSource(ownerID)
		}
	}
	if strings.TrimSpace(query) == "" && strings.TrimSpace(cursor) == "" && revision > 0 {
		if requested := strings.TrimSpace(c.Query("rev")); requested != "" && requested == strconv.FormatUint(revision, 10) {
			return ConversationListJSON{Revision: revision, Unchanged: true}, nil
		}
	}

	// Conversation search may match a body, so only that legacy path asks for
	// full rows. The normal inbox list stays on the header projection.
	includeBodies := strings.TrimSpace(query) != ""
	data, err := h.conversationPageData(c, includeBodies)
	if err != nil {
		return ConversationListJSON{}, err
	}
	conversations := data["Conversations"].([]Conversation)
	unreadCount := 0
	for _, conversation := range conversations {
		unreadCount += conversation.UnreadCount
	}
	all := filterConversationJSON(conversations, query)
	if strings.EqualFold(strings.TrimSpace(filter), "open") {
		open := make([]Conversation, 0, len(all))
		for _, conversation := range all {
			if conversation.Status == "unanswered" || conversation.Status == "draft" {
				open = append(open, conversation)
			}
		}
		all = open
	}
	total := len(all)

	start := 0
	if limit > 0 && strings.TrimSpace(cursor) != "" {
		parsed, err := decodeConversationCursor(cursor)
		if err != nil {
			return ConversationListJSON{}, err
		}
		for start < len(all) && conversationBeforeOrAtCursor(all[start], parsed) {
			start++
		}
	}
	end := len(all)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	page := all[start:end]
	hasMore := end < len(all)
	nextCursor := ""
	if hasMore && len(page) > 0 {
		nextCursor = encodeConversationCursor(page[len(page)-1])
	}

	response := ConversationListJSON{
		Conversations:    make([]ConversationSummaryJSON, 0, len(page)),
		Folders:          data["Folders"].([]*api.MailboxInfo),
		Accounts:         make([]ConversationAccountJSON, 0),
		AccountEmail:     data["Email"].(string),
		Locale:           CurrentLocale(c),
		Unified:          data["Unified"].(bool),
		UnifiedAvailable: data["UnifiedAvailable"].(bool),
		Revision:         revision,
		NextCursor:       nextCursor,
		HasMore:          hasMore,
		Total:            total,
		UnreadCount:      unreadCount,
	}
	for _, account := range data["MailAccounts"].([]mailAccountOption) {
		response.Accounts = append(response.Accounts, ConversationAccountJSON{
			ID:       account.ID,
			Email:    account.Email,
			Label:    account.Label,
			Color:    account.Color,
			IsActive: account.IsActive,
		})
	}
	for _, conversation := range page {
		response.Conversations = append(response.Conversations, conversationSummaryJSON(conversation))
	}
	for _, account := range data["AccountErrors"].([]AccountFetchResult) {
		if account.Err != nil {
			response.AccountErrors = append(response.AccountErrors, ConversationAccountErrorJSON{
				AccountEmail: account.AccountEmail,
				Message:      account.Err.Error(),
			})
		}
	}
	return response, nil
}

func (h *EmailHandler) HandleConversationListJSON(c *fiber.Ctx) error {
	response, err := h.conversationListJSON(c, c.Query("q"), c.Query("filter"), conversationListLimit(c), c.Query("cursor"))
	if errors.Is(err, errInvalidConversationCursor) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid conversation cursor"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversations"})
	}
	return c.JSON(response)
}

// HandleStartConversationJSON opens the account's peer conversation or creates
// an initial draft so a conversation without sent messages survives reloads.
func (h *EmailHandler) HandleStartConversationJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "Mail mirror is unavailable")
	}
	ownerID := requestOwnerID(c, h.store)
	if ownerID == "" {
		return fiber.ErrUnauthorized
	}
	var input struct {
		To           string `json:"to"`
		AccountEmail string `json:"accountEmail"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request body")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(input.To))
	if err != nil || !strings.Contains(address.Address, "@") {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid recipient email address")
	}
	account, ok := h.mirrorAccountForEmail(c, input.AccountEmail)
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "Mail account not found")
	}
	to := strings.ToLower(address.Address)
	id := conversationID(account.ID, messageParticipants(models.Email{From: account.Email, To: to}, account.Email))
	conversations, _, err := h.localConversations(c, []mailstore.Account{account}, false)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Error loading local conversations")
	}
	if existing := findConversation(conversations, id); existing != nil {
		if err := h.hydrateConversation(c, account.ID, existing); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "Error loading conversation messages")
		}
		return c.JSON(fiber.Map{"conversation": conversationDetailJSONForLocale(*existing, CurrentLocale(c))})
	}
	draft, err := h.mailDB.SaveDraft(c.UserContext(), ownerID, mailstore.DraftInput{
		AccountEmail: account.Email, ConversationID: id, Mode: mailstore.DraftModeNew, To: to,
	})
	if err != nil {
		return draftError(err)
	}
	conversation := Conversation{
		ID: id, Title: to, PeerEmail: to, Latest: models.Email{Date: draft.UpdatedAt},
		AccountEmail: account.Email, AccountLabel: account.Label, AccountColor: account.Color, Status: "no_action",
	}
	return c.JSON(fiber.Map{"conversation": conversationDetailJSON(conversation)})
}

func (h *EmailHandler) HandleConversationViewJSON(c *fiber.Ctx) error {
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), c.Params("id"))
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}
	account, accountOK := h.mirrorAccountForEmail(c, selected.AccountEmail)
	if !accountOK {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	if err := h.hydrateConversation(c, account.ID, selected); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	response := conversationDetailJSONForLocale(*selected, CurrentLocale(c))
	keys := make([]mailstore.MessageSummaryKey, 0, len(selected.Messages))
	for _, message := range selected.Messages {
		if message.Email.Folder == "Outbox" {
			continue
		}
		keys = append(keys, mailstore.MessageSummaryKey{FolderName: message.Email.Folder, UID: message.Email.ID})
	}
	summaries, summaryErr := h.mailDB.ListMessageSummaries(c.UserContext(), account.ID, keys)
	if summaryErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading saved mail summaries"})
	}
	replySuggestions, suggestionErr := h.mailDB.ListMessageReplySuggestions(c.UserContext(), account.ID, keys)
	if suggestionErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading saved reply suggestions"})
	}
	translations, translationErr := h.mailDB.ListMessageTranslations(c.UserContext(), account.ID, keys)
	if translationErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading saved mail translations"})
	}
	translationsAll, translationAllErr := h.mailDB.ListMessageTranslationsAll(c.UserContext(), account.ID, keys)
	if translationAllErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading saved mail translations"})
	}
	configHash, _ := mailstore.CurrentMailSummaryConfigHash(c.UserContext(), h.mailDB, account)
	for index, message := range selected.Messages {
		lookupKey := mailstore.MessageSummaryLookupKey(message.Email.Folder, message.Email.ID)
		if record, exists := summaries[lookupKey]; exists {
			sourceHash, sourceErr := mailstore.CurrentMailSummarySourceHash(c.UserContext(), h.mailDB, account, message.Email)
			stale := sourceErr != nil || record.SourceHash != sourceHash || (configHash != "" && record.ConfigHash != configHash)
			response.Messages[index].MailSummary = mailSummaryJSON(record, stale)
		}
		if record, exists := replySuggestions[lookupKey]; exists && record.Status == "ready" && strings.TrimSpace(record.Summary) != "" {
			response.Messages[index].SuggestedReply = &SuggestedReplyJSON{Text: record.Summary, Status: record.Status, UpdatedAt: record.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
		}
		if record, exists := translations[lookupKey]; exists {
			response.Messages[index].MailTranslation = mailTranslationJSON(record)
		}
		if record, exists := translationsAll[lookupKey]; exists {
			response.Messages[index].MailTranslationAll = mailTranslationJSON(record)
		}
	}
	return c.JSON(fiber.Map{"conversation": response})
}

func (h *EmailHandler) HandleConversationNoteJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	conversationID := strings.TrimSpace(c.Params("id"))
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), conversationID)
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	body.Note = strings.TrimSpace(body.Note)
	if len([]rune(body.Note)) > 200 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Conversation note must not exceed 200 characters"})
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not save conversation note"})
	}
	ownerID, _ := sess.Get("user_id").(string)
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), ownerID, selected.AccountEmail)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	if err := h.mailDB.SetConversationNote(c.UserContext(), account.ID, conversationID, body.Note); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not save conversation note"})
	}
	return c.JSON(fiber.Map{"ok": true, "note": body.Note})
}

func (h *EmailHandler) HandleGetConversationAIChatJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	conversationID := strings.TrimSpace(c.Params("id"))
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), conversationID)
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not get user session"})
	}
	ownerID, _ := sess.Get("user_id").(string)
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), ownerID, selected.AccountEmail)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	rawMessages, err := h.mailDB.GetConversationAIChat(c.UserContext(), account.ID, conversationID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not load conversation AI chat"})
	}
	var messages []any
	if err := json.Unmarshal([]byte(rawMessages), &messages); err != nil {
		messages = []any{}
	}
	return c.JSON(fiber.Map{"ok": true, "messages": messages})
}

func (h *EmailHandler) HandleSaveConversationAIChatJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	conversationID := strings.TrimSpace(c.Params("id"))
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), conversationID)
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}
	var body struct {
		Messages any `json:"messages"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	bytes, err := json.Marshal(body.Messages)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid messages payload"})
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not save conversation AI chat"})
	}
	ownerID, _ := sess.Get("user_id").(string)
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), ownerID, selected.AccountEmail)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	if err := h.mailDB.SaveConversationAIChat(c.UserContext(), account.ID, conversationID, string(bytes)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not save conversation AI chat"})
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *EmailHandler) HandleDeleteConversationAIChatJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	conversationID := strings.TrimSpace(c.Params("id"))
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), conversationID)
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not delete conversation AI chat"})
	}
	ownerID, _ := sess.Get("user_id").(string)
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), ownerID, selected.AccountEmail)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	if err := h.mailDB.DeleteConversationAIChat(c.UserContext(), account.ID, conversationID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not delete conversation AI chat"})
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *EmailHandler) HandleConversationStatusJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	conversationID := strings.TrimSpace(c.Params("id"))
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), conversationID)
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if body.Status != "answered" && body.Status != "unanswered" && body.Status != "no_action" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid conversation status"})
	}
	sess, err := h.store.Get(c)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not save conversation status"})
	}
	ownerID, _ := sess.Get("user_id").(string)
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), ownerID, selected.AccountEmail)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	if err := h.mailDB.SetConversationStatus(c.UserContext(), account.ID, conversationID, body.Status, selected.StatusMessageKey); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not save conversation status"})
	}
	return c.JSON(fiber.Map{"ok": true, "status": body.Status})
}

func flagsWithSeen(flags []string, seen bool) ([]string, bool) {
	next := make([]string, 0, len(flags)+1)
	found := false
	for _, flag := range flags {
		if strings.EqualFold(flag, `\Seen`) {
			found = true
			if !seen {
				continue
			}
			flag = `\Seen`
		}
		next = append(next, flag)
	}
	if seen && !found {
		next = append(next, `\Seen`)
	}
	return next, found != seen
}

func (h *EmailHandler) handleConversationSeenJSON(c *fiber.Ctx, seen bool) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), strings.TrimSpace(c.Params("id")))
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}

	keys := make([]mailstore.MessageFlagKey, 0, len(selected.Messages))
	for _, message := range selected.Messages {
		if message.Outgoing {
			continue
		}
		keys = append(keys, mailstore.MessageFlagKey{FolderName: message.Email.Folder, UID: message.Email.ID})
	}
	if len(keys) == 0 {
		return c.JSON(fiber.Map{"ok": true, "updated": 0, "sync": "idle"})
	}

	account, ok := h.mirrorAccountForEmail(c, selected.AccountEmail)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	updated, err := h.mailDB.QueueSeenUpdates(c.UserContext(), account.ID, keys, seen)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not update the local email read status"})
	}
	if updated > 0 && h.auth != nil && h.auth.syncer != nil {
		h.auth.syncer.Trigger(account.ID)
	}
	syncState := "idle"
	if updated > 0 {
		syncState = "pending"
	}
	return c.JSON(fiber.Map{"ok": true, "updated": updated, "sync": syncState})
}

func (h *EmailHandler) HandleConversationReadJSON(c *fiber.Ctx) error {
	return h.handleConversationSeenJSON(c, true)
}

func (h *EmailHandler) HandleConversationUnreadJSON(c *fiber.Ctx) error {
	return h.handleConversationSeenJSON(c, false)
}

func (h *EmailHandler) HandleConversationDeleteJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), strings.TrimSpace(c.Params("id")))
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}

	account, ok := h.mirrorAccountForEmail(c, selected.AccountEmail)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}

	trash, _ := h.mailDB.ResolveTrashFolder(c.UserContext(), account.ID)

	var moveKeys []mailstore.MessageMoveKey
	var deleteKeys []mailstore.MessageMoveKey
	for _, message := range selected.Messages {
		email := message.Email
		if email.Folder == "Outbox" {
			if err := h.mailDB.DeleteOutgoing(c.UserContext(), account.ID, email.ID); err != nil {
				return c.Status(409).JSON(fiber.Map{"error": "Cannot delete an email while sending"})
			}
			continue
		}
		_ = deleteMessageAttachmentCache(c, nil, account.ID, email)
		if trash != "" && strings.EqualFold(strings.TrimSpace(email.Folder), strings.TrimSpace(trash)) {
			deleteKeys = append(deleteKeys, mailstore.MessageMoveKey{
				FolderName: email.Folder,
				UID:        email.ID,
			})
			continue
		}
		moveKeys = append(moveKeys, mailstore.MessageMoveKey{
			FolderName: email.Folder,
			UID:        email.ID,
		})
	}

	queuedMoves, err := h.mailDB.QueueMessageMoves(c.UserContext(), account.ID, moveKeys, trash)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not delete conversation locally"})
	}
	queuedDeletes, err := h.mailDB.QueueMessageDeletions(c.UserContext(), account.ID, deleteKeys)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not delete conversation locally"})
	}
	drafts, err := h.mailDB.ListDrafts(c.UserContext(), account.OwnerID, selected.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "Could not delete conversation draft")
	}
	for _, draft := range drafts {
		if strings.EqualFold(draft.AccountEmail, account.Email) {
			if err := h.mailDB.DeleteDraft(c.UserContext(), account.OwnerID, draft.ID); err != nil {
				return draftError(err)
			}
		}
	}

	if (queuedMoves > 0 || queuedDeletes > 0) && h.auth != nil && h.auth.syncer != nil {
		h.auth.syncer.Trigger(account.ID)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *EmailHandler) HandleConversationMessageDeleteJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}

	conversationID := strings.TrimSpace(c.Params("id"))
	uid := strings.TrimSpace(c.Params("uid"))
	var body struct {
		Folder string `json:"folder"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	body.Folder = strings.TrimSpace(body.Folder)
	if uid == "" || body.Folder == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Message folder and UID are required"})
	}

	data, err := h.conversationPageData(c, false)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), conversationID)
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}
	var targetEmail models.Email
	for _, message := range selected.Messages {
		if message.Email.ID == uid && message.Email.Folder == body.Folder {
			targetEmail = message.Email
			break
		}
	}
	if targetEmail.ID == "" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Message not found in conversation"})
	}

	account, ok := h.mirrorAccountForEmail(c, selected.AccountEmail)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}

	if targetEmail.Folder == "Outbox" {
		if err := h.mailDB.DeleteOutgoing(c.UserContext(), account.ID, uid); err != nil {
			return c.SendStatus(409)
		}
		return c.JSON(fiber.Map{"ok": true})
	}
	trash, _ := h.mailDB.ResolveTrashFolder(c.UserContext(), account.ID)
	if trash != "" && strings.EqualFold(strings.TrimSpace(trash), body.Folder) {
		_ = deleteMessageAttachmentCache(c, nil, account.ID, targetEmail)
		queued, err := h.mailDB.QueueMessageDeletions(c.UserContext(), account.ID, []mailstore.MessageMoveKey{
			{FolderName: body.Folder, UID: uid},
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not delete email locally"})
		}
		if queued > 0 && h.auth != nil && h.auth.syncer != nil {
			h.auth.syncer.Trigger(account.ID)
		}
		return c.JSON(fiber.Map{"ok": true})
	}

	_ = deleteMessageAttachmentCache(c, nil, account.ID, targetEmail)

	queued, err := h.mailDB.QueueMessageMoves(c.UserContext(), account.ID, []mailstore.MessageMoveKey{
		{FolderName: body.Folder, UID: uid},
	}, trash)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not delete email locally"})
	}

	if queued > 0 && h.auth != nil && h.auth.syncer != nil {
		h.auth.syncer.Trigger(account.ID)
	}

	return c.JSON(fiber.Map{"ok": true})
}
