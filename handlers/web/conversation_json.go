package web

import (
	stdhtml "html"
	"lilmail/handlers/api"
	"lilmail/models"
	"net/url"
	"regexp"
	"strings"

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
}

// ConversationMessageJSON is the detail model for one chat bubble. HTML is
// prepared for a script-free sandbox iframe and has local cid: references
// rewritten to authenticated attachment URLs before it reaches the client.
type ConversationMessageJSON struct {
	ID             string              `json:"id"`
	Folder         string              `json:"folder,omitempty"`
	From           string              `json:"from"`
	FromName       string              `json:"fromName,omitempty"`
	To             string              `json:"to"`
	Cc             string              `json:"cc,omitempty"`
	Subject        string              `json:"subject"`
	Preview        string              `json:"preview"`
	Body           string              `json:"body"`
	HTML           string              `json:"html,omitempty"`
	Date           string              `json:"date"`
	HasAttachments bool                `json:"hasAttachments"`
	Flags          []string            `json:"flags,omitempty"`
	Attachments    []models.Attachment `json:"attachments,omitempty"`
	MessageID      string              `json:"messageId,omitempty"`
	InReplyTo      string              `json:"inReplyTo,omitempty"`
	References     []string            `json:"references,omitempty"`
	Outgoing       bool                `json:"outgoing"`
}

type ConversationDetailJSON struct {
	ID           string                    `json:"id"`
	Title        string                    `json:"title"`
	PeerEmail    string                    `json:"peerEmail,omitempty"`
	Subject      string                    `json:"subject"`
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
	}
}

func conversationDetailJSON(conversation Conversation) ConversationDetailJSON {
	result := ConversationDetailJSON{
		ID:           conversation.ID,
		Title:        conversation.Title,
		PeerEmail:    conversation.PeerEmail,
		Subject:      conversation.Subject,
		Count:        conversation.Count,
		AccountEmail: conversation.AccountEmail,
		AccountLabel: conversation.AccountLabel,
		AccountColor: conversation.AccountColor,
		Messages:     make([]ConversationMessageJSON, 0, len(conversation.Messages)),
	}
	for _, message := range conversation.Messages {
		email := message.Email
		body := email.Body
		if strings.TrimSpace(body) == "" {
			body = email.Preview
		}
		body = stdhtml.UnescapeString(body)
		preparedHTML := ""
		if strings.TrimSpace(email.HTML) != "" {
			htmlBody := collapseQuotedHTML(email.HTML)
			preparedHTML, _ = prepareEmailHTML(rewriteInlineCIDReferences(htmlBody, email.Attachments, conversation.AccountEmail))
		}
		attachments := regularAttachments(email.Attachments)
		result.Messages = append(result.Messages, ConversationMessageJSON{
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
	document, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	collapseQuotedNodes(document)
	var output strings.Builder
	if err := html.Render(&output, document); err != nil {
		return raw
	}
	return output.String()
}

func collapseQuotedNodes(node *html.Node) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if isQuotedHTMLNode(child) {
			wrapQuotedNodes(node, child, child)
		} else {
			collapseQuotedNodes(child)
		}
		child = next
	}
	// Some clients emit only a separator such as
	// "------------------ Original ------------------" and do not provide a
	// quoted-message container. From that separator onward, the siblings are
	// the old message and should be collapsed together.
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if isOriginalSeparatorNode(child) {
			wrapQuotedNodes(node, child, nil)
			break
		}
	}
}

func wrapQuotedNodes(parent, first, last *html.Node) {
	details := &html.Node{Type: html.ElementNode, Data: "details", Attr: []html.Attribute{{Key: "class", Val: "lilmail-quoted"}}}
	summary := &html.Node{Type: html.ElementNode, Data: "summary"}
	summary.AppendChild(&html.Node{Type: html.TextNode, Data: "Show quoted message"})
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
		if accountEmail != "" {
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
			strings.Contains(strings.ToLower(conversation.Preview), query)
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

func (h *EmailHandler) conversationListJSON(c *fiber.Ctx, query string) (ConversationListJSON, error) {
	data, err := h.conversationPageData(c)
	if err != nil {
		return ConversationListJSON{}, err
	}
	conversations := filterConversationJSON(data["Conversations"].([]Conversation), query)
	response := ConversationListJSON{
		Conversations:    make([]ConversationSummaryJSON, 0, len(conversations)),
		Folders:          data["Folders"].([]*api.MailboxInfo),
		Accounts:         make([]ConversationAccountJSON, 0),
		AccountEmail:     data["Email"].(string),
		Locale:           CurrentLocale(c),
		Unified:          data["Unified"].(bool),
		UnifiedAvailable: data["UnifiedAvailable"].(bool),
	}
	for _, account := range data["MailAccounts"].([]mailAccountOption) {
		response.Accounts = append(response.Accounts, ConversationAccountJSON{
			Email:    account.Email,
			Label:    account.Label,
			Color:    account.Color,
			IsActive: account.IsActive,
		})
	}
	for _, conversation := range conversations {
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
	response, err := h.conversationListJSON(c, c.Query("q"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversations"})
	}
	return c.JSON(response)
}

func (h *EmailHandler) HandleConversationViewJSON(c *fiber.Ctx) error {
	data, err := h.conversationPageData(c)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), c.Params("id"))
	if selected == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Conversation not found"})
	}
	return c.JSON(fiber.Map{"conversation": conversationDetailJSON(*selected)})
}
