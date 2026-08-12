package web

import (
	stdhtml "html"
	"lilmail/handlers/api"
	"lilmail/models"
	"strings"

	"github.com/gofiber/fiber/v2"
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
// intentionally excluded for now: the chat UI renders safe plain text. A
// future rich-mail viewer must sanitize HTML before adding it to this DTO.
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
			Date:           email.Date.UTC().Format("2006-01-02T15:04:05.999Z07:00"),
			HasAttachments: email.HasAttachments,
			Flags:          email.Flags,
			Attachments:    email.Attachments,
			MessageID:      email.MessageID,
			InReplyTo:      email.InReplyTo,
			References:     email.References,
			Outgoing:       message.Outgoing,
		})
	}
	return result
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
