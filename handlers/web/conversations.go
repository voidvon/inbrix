package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	stdhtml "html"
	"lilmail/handlers/api"
	"lilmail/mailstore"
	"lilmail/models"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"
)

const conversationViewFolder = "INBOX"

// Conversation is the UI model for one mail conversation. It deliberately
// contains both received and sent messages so the chat transcript is complete.
type Conversation struct {
	ID             string
	Title          string
	PeerEmail      string
	Subject        string
	Preview        string
	Latest         models.Email
	Messages       []ConversationMessage
	Count          int
	UnreadCount    int
	HasAttachments bool
	AccountEmail   string
	AccountLabel   string
	AccountColor   string
}

type ConversationMessage struct {
	Email    models.Email
	Outgoing bool
}

func isSentMailbox(folder mailstore.Folder) bool {
	name := strings.ToLower(strings.TrimSpace(folder.Name))
	if name == "sent" || name == "sent items" || name == "sent mail" || name == "sent messages" ||
		strings.HasSuffix(name, "/sent") || strings.HasSuffix(name, "/sent messages") {
		return true
	}
	for _, attribute := range folder.Attributes {
		if strings.EqualFold(strings.TrimSpace(attribute), `\Sent`) {
			return true
		}
	}
	return false
}

func isConversationMailboxName(name string) bool {
	if strings.EqualFold(strings.TrimSpace(name), conversationViewFolder) {
		return true
	}
	return isSentMailbox(mailstore.Folder{Name: name})
}

func isSentMailboxName(name string) bool {
	return isSentMailbox(mailstore.Folder{Name: name})
}

func conversationFolderNames(folders []mailstore.Folder) []string {
	names := make([]string, 0, 2)
	hasInbox := false
	for _, folder := range folders {
		if strings.EqualFold(folder.Name, conversationViewFolder) {
			hasInbox = true
			names = append(names, folder.Name)
		}
	}
	if !hasInbox {
		names = append(names, conversationViewFolder)
	}
	for _, folder := range folders {
		if isSentMailbox(folder) {
			duplicate := false
			for _, name := range names {
				if name == folder.Name {
					duplicate = true
					break
				}
			}
			if !duplicate {
				names = append(names, folder.Name)
			}
		}
	}
	return names
}

func conversationNavigationFolders(folders []*api.MailboxInfo) []*api.MailboxInfo {
	out := make([]*api.MailboxInfo, 0, len(folders))
	for _, folder := range folders {
		if folder == nil || isConversationMailboxName(folder.Name) {
			continue
		}
		out = append(out, folder)
	}
	return out
}

func sameMailboxAddress(value, mailbox string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	mailbox = strings.TrimSpace(strings.ToLower(mailbox))
	if start := strings.LastIndex(value, "<"); start >= 0 {
		if end := strings.Index(value[start:], ">"); end > 0 {
			value = strings.TrimSpace(value[start+1 : start+end])
		}
	}
	return value != "" && value == mailbox
}

func firstMailboxAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if comma := strings.IndexAny(value, ",;"); comma >= 0 {
		value = value[:comma]
	}
	if start := strings.LastIndex(value, "<"); start >= 0 {
		if end := strings.Index(value[start:], ">"); end > 0 {
			return strings.TrimSpace(value[start+1 : start+end])
		}
	}
	return strings.TrimSpace(value)
}

func displayAddress(name, address string) string {
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return firstMailboxAddress(address)
}

func isUnread(email models.Email) bool {
	for _, flag := range email.Flags {
		if flag == `\Seen` {
			return false
		}
	}
	return true
}

// messageHasAttachments is deliberately derived from both fields. Older
// mirror rows can contain attachment metadata while their boolean marker was
// reset by a later lightweight list refresh; the metadata is enough to render
// the attachment links and is the more useful source of truth here.
func messageHasAttachments(email models.Email) bool {
	for _, attachment := range email.Attachments {
		if !attachment.IsInline {
			return true
		}
	}
	return false
}

func conversationID(accountID string, thread models.Thread) string {
	h := sha256.New()
	h.Write([]byte(accountID))
	for _, email := range thread.Messages {
		folder := email.Folder
		if folder == "" {
			folder = conversationViewFolder
		}
		fmt.Fprintf(h, "\x00%s\x00%s\x00%s", folder, email.ID, email.MessageID)
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

func conversationTitle(messages []models.Email, mailbox string) (string, string) {
	type participant struct {
		name  string
		email string
	}
	participants := make([]participant, 0, 2)
	seen := make(map[string]struct{})
	appendParticipant := func(name, address string) {
		address = firstMailboxAddress(address)
		key := strings.ToLower(address)
		if address == "" || sameMailboxAddress(address, mailbox) {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		participants = append(participants, participant{name: displayAddress(name, address), email: address})
	}

	for _, email := range messages {
		outgoing := isSentMailboxName(email.Folder) || sameMailboxAddress(email.From, mailbox)
		if outgoing {
			name := ""
			if len(email.ToNames) > 0 {
				name = email.ToNames[0]
			}
			appendParticipant(name, email.To)
		} else {
			appendParticipant(email.FromName, email.From)
		}
	}
	if len(participants) == 0 {
		return "Conversation", ""
	}
	names := make([]string, 0, len(participants))
	emails := make([]string, 0, len(participants))
	for _, p := range participants {
		names = append(names, p.name)
		emails = append(emails, p.email)
	}
	return strings.Join(names, ", "), strings.Join(emails, ", ")
}

func buildConversations(account mailstore.Account, emails []models.Email) []Conversation {
	if len(emails) == 0 {
		return nil
	}
	for i := range emails {
		if emails[i].Folder == "" {
			emails[i].Folder = conversationViewFolder
		}
		emails[i].AccountEmail = account.Email
		emails[i].AccountLabel = account.Label
		emails[i].AccountColor = account.Color
	}

	threads := api.ThreadMessages(emails)
	conversations := make([]Conversation, 0, len(threads))
	for _, thread := range threads {
		if len(thread.Messages) == 0 {
			continue
		}
		title, peerEmail := conversationTitle(thread.Messages, account.Email)
		subject := stdhtml.UnescapeString(strings.TrimSpace(thread.Root.Subject))
		if subject == "" {
			subject = "(no subject)"
		}
		latest := thread.Latest
		preview := stdhtml.UnescapeString(strings.TrimSpace(latest.Preview))
		if preview == "" {
			preview = stdhtml.UnescapeString(strings.TrimSpace(latest.Body))
		}
		if preview == "" {
			preview = subject
		}
		conversation := Conversation{
			ID:           conversationID(account.ID, thread),
			Title:        title,
			PeerEmail:    peerEmail,
			Subject:      subject,
			Preview:      preview,
			Latest:       latest,
			Count:        len(thread.Messages),
			AccountEmail: account.Email,
			AccountLabel: account.Label,
			AccountColor: account.Color,
		}
		for _, email := range thread.Messages {
			outgoing := isSentMailboxName(email.Folder) || sameMailboxAddress(email.From, account.Email)
			conversation.Messages = append(conversation.Messages, ConversationMessage{Email: email, Outgoing: outgoing})
			if !outgoing && isUnread(email) {
				conversation.UnreadCount++
			}
			if messageHasAttachments(email) {
				conversation.HasAttachments = true
			}
		}
		conversations = append(conversations, conversation)
	}
	sort.SliceStable(conversations, func(i, j int) bool {
		return conversations[i].Latest.Date.After(conversations[j].Latest.Date)
	})
	return conversations
}

func (h *EmailHandler) localConversations(c *fiber.Ctx, accounts []mailstore.Account) ([]Conversation, []AccountFetchResult, error) {
	if h.mailDB == nil {
		return nil, nil, fmt.Errorf("mail mirror is not configured")
	}
	conversations := make([]Conversation, 0)
	results := make([]AccountFetchResult, 0, len(accounts))
	for _, account := range accounts {
		result := AccountFetchResult{AccountEmail: account.Email, AccountLabel: account.Label, AccountColor: account.Color}
		folders, err := h.mailDB.ListFolders(c.UserContext(), account.ID)
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}
		names := conversationFolderNames(folders)
		emails, err := h.mailDB.ListMessagesForFolders(c.UserContext(), account.ID, names)
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}
		result.Emails = tagMirrorEmails(emails, account)
		conversations = append(conversations, buildConversations(account, result.Emails)...)
		results = append(results, result)
	}
	sort.SliceStable(conversations, func(i, j int) bool {
		return conversations[i].Latest.Date.After(conversations[j].Latest.Date)
	})
	return conversations, results, nil
}

func findConversation(conversations []Conversation, id string) *Conversation {
	for i := range conversations {
		if conversations[i].ID == id {
			return &conversations[i]
		}
	}
	return nil
}

func (h *EmailHandler) conversationPageData(c *fiber.Ctx) (fiber.Map, error) {
	username, _ := c.Locals("username").(string)
	if username == "" {
		return nil, fiber.ErrUnauthorized
	}
	active, ok := h.currentMirrorAccount(c)
	if !ok {
		return nil, fmt.Errorf("mailbox is not configured")
	}
	all := c.Query("unified") == "1"
	accounts := []mailstore.Account{active}
	if all {
		if mirrorAccounts, _, err := h.mirrorAccounts(c); err == nil && len(mirrorAccounts) > 0 {
			accounts = mirrorAccounts
		}
	}
	conversations, accountErrors, err := h.localConversations(c, accounts)
	if err != nil {
		return nil, err
	}
	token, err := api.GetSessionToken(c, h.store)
	if err != nil {
		return nil, err
	}
	selectedID := c.Query("conversation")
	if selectedID == "" && len(conversations) > 0 {
		selectedID = conversations[0].ID
	}
	selected := findConversation(conversations, selectedID)
	folders, err := h.mailDB.ListFolders(c.UserContext(), active.ID)
	if err != nil {
		return nil, err
	}
	navigation := conversationNavigationFolders(mirrorMailboxInfos(folders))
	unread := 0
	for _, conversation := range conversations {
		if conversation.UnreadCount > 0 {
			unread++
		}
	}
	return fiber.Map{
		"Username":                username,
		"Email":                   active.Email,
		"MailAccounts":            h.mailAccountOptions(c),
		"Folders":                 mirrorMailboxInfos(folders),
		"NavigationFolders":       navigation,
		"Conversations":           conversations,
		"Conversation":            selected,
		"SelectedConversationID":  selectedID,
		"ConversationUnreadCount": unread,
		"AccountErrors":           accountErrors,
		"Unified":                 all && len(accounts) > 1,
		"UnifiedAvailable":        len(accounts) > 1,
		"Token":                   token,
		"CurrentFolder":           conversationViewFolder,
		"CurrentView":             "conversations",
		"ConversationMode":        true,
	}, nil
}

func (h *EmailHandler) HandleConversationList(c *fiber.Ctx) error {
	data, err := h.conversationPageData(c)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Error loading local conversations"})
	}
	return Render(c, "conversation-list", data, "")
}

func (h *EmailHandler) HandleConversationView(c *fiber.Ctx) error {
	data, err := h.conversationPageData(c)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Error loading local conversation"})
	}
	selected := findConversation(data["Conversations"].([]Conversation), c.Params("id"))
	if selected == nil {
		return c.Status(404).JSON(fiber.Map{"error": "Conversation not found"})
	}
	data["Conversation"] = selected
	return Render(c, "conversation-chat", data, "")
}

func (h *EmailHandler) HandleConversationSearch(c *fiber.Ctx) error {
	data, err := h.conversationPageData(c)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Error searching local conversations"})
	}
	query := strings.ToLower(strings.TrimSpace(c.Query("q")))
	if query != "" {
		all := data["Conversations"].([]Conversation)
		filtered := make([]Conversation, 0, len(all))
		for _, conversation := range all {
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
		data["Conversations"] = filtered
		data["SelectedConversationID"] = ""
	}
	return Render(c, "conversation-list", data, "")
}

func (h *EmailHandler) HandleConversations(c *fiber.Ctx) error {
	data, err := h.conversationPageData(c)
	if err != nil {
		if h.localUserWithoutMailbox(c) {
			return c.Redirect("/settings?setup=1")
		}
		return c.Status(500).SendString("Error loading local conversations")
	}
	return Render(c, "conversations", data)
}
