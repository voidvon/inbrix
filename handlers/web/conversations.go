package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	stdhtml "html"
	"inbrix/handlers/api"
	"inbrix/mailstore"
	"inbrix/models"
	"net/mail"
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
	Note           string
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

func conversationID(accountID string, participants []string) string {
	h := sha256.New()
	h.Write([]byte(accountID))
	ordered := append([]string(nil), participants...)
	sort.Strings(ordered)
	for _, participant := range ordered {
		fmt.Fprintf(h, "\x00%s", participant)
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

func parseMailboxAddresses(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if addresses, err := mail.ParseAddressList(value); err == nil {
		result := make([]string, 0, len(addresses))
		for _, address := range addresses {
			if normalized := strings.ToLower(strings.TrimSpace(address.Address)); normalized != "" {
				result = append(result, normalized)
			}
		}
		return result
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if normalized := strings.ToLower(firstMailboxAddress(part)); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func messageParticipants(email models.Email, mailbox string) []string {
	mailbox = strings.ToLower(strings.TrimSpace(mailbox))
	seen := make(map[string]struct{})
	for _, value := range []string{email.From, email.To, email.Cc} {
		for _, address := range parseMailboxAddresses(value) {
			if address != mailbox {
				seen[address] = struct{}{}
			}
		}
	}
	participants := make([]string, 0, len(seen))
	for address := range seen {
		participants = append(participants, address)
	}
	sort.Strings(participants)
	return participants
}

func conversationTitle(messages []models.Email, mailbox string, participants []string) (string, string) {
	names := make(map[string]string, len(participants))
	for _, email := range messages {
		from := strings.ToLower(firstMailboxAddress(email.From))
		if from != "" && !sameMailboxAddress(from, mailbox) && strings.TrimSpace(email.FromName) != "" {
			names[from] = strings.TrimSpace(email.FromName)
		}
		for index, address := range parseMailboxAddresses(email.To) {
			if sameMailboxAddress(address, mailbox) || names[address] != "" || index >= len(email.ToNames) {
				continue
			}
			if name := strings.TrimSpace(email.ToNames[index]); name != "" {
				names[address] = name
			}
		}
	}
	if len(participants) == 0 {
		return "Conversation", ""
	}
	labels := make([]string, 0, len(participants))
	for _, address := range participants {
		if names[address] != "" {
			labels = append(labels, names[address])
		} else {
			labels = append(labels, address)
		}
	}
	return strings.Join(labels, ", "), strings.Join(participants, ", ")
}

func buildConversations(account mailstore.Account, emails []models.Email) []Conversation {
	if len(emails) == 0 {
		return nil
	}
	for i := range emails {
		if emails[i].Folder == "" {
			emails[i].Folder = conversationViewFolder
		}
		emails[i].Subject = string(api.DecodeLegacyText([]byte(emails[i].Subject)))
		emails[i].Preview = string(api.DecodeLegacyText([]byte(emails[i].Preview)))
		emails[i].Body = string(api.DecodeLegacyText([]byte(emails[i].Body)))
		emails[i].HTML = string(api.DecodeLegacyText([]byte(emails[i].HTML)))
		emails[i].AccountEmail = account.Email
		emails[i].AccountLabel = account.Label
		emails[i].AccountColor = account.Color
	}

	type conversationGroup struct {
		participants []string
		messages     []models.Email
	}
	groups := make(map[string]*conversationGroup)
	for _, email := range emails {
		participants := messageParticipants(email, account.Email)
		key := strings.Join(participants, "\x00")
		group := groups[key]
		if group == nil {
			group = &conversationGroup{participants: participants}
			groups[key] = group
		}
		group.messages = append(group.messages, email)
	}

	conversations := make([]Conversation, 0, len(groups))
	for _, group := range groups {
		sort.SliceStable(group.messages, func(i, j int) bool {
			if group.messages[i].Date.Equal(group.messages[j].Date) {
				return group.messages[i].ID < group.messages[j].ID
			}
			return group.messages[i].Date.Before(group.messages[j].Date)
		})
		title, peerEmail := conversationTitle(group.messages, account.Email, group.participants)
		subject := stdhtml.UnescapeString(strings.TrimSpace(group.messages[0].Subject))
		if subject == "" {
			subject = "(no subject)"
		}
		latest := group.messages[len(group.messages)-1]
		preview := stdhtml.UnescapeString(strings.TrimSpace(latest.Preview))
		if preview == "" {
			preview = stdhtml.UnescapeString(strings.TrimSpace(latest.Body))
		}
		if preview == "" {
			preview = subject
		}
		conversation := Conversation{
			ID:           conversationID(account.ID, group.participants),
			Title:        title,
			PeerEmail:    peerEmail,
			Subject:      subject,
			Preview:      preview,
			Latest:       latest,
			Count:        len(group.messages),
			AccountEmail: account.Email,
			AccountLabel: account.Label,
			AccountColor: account.Color,
		}
		for _, email := range group.messages {
			// Older mirror rows may have marked cid-referenced images as regular
			// attachments. Reclassify them from the cached HTML before building both
			// the conversation summary and its message bubbles.
			email.Attachments = api.MarkInlineAttachmentsFromHTML(email.HTML, email.Attachments)
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
		accountConversations := buildConversations(account, result.Emails)
		notes, err := h.mailDB.ListConversationNotes(c.UserContext(), account.ID)
		if err != nil {
			result.Err = err
			results = append(results, result)
			continue
		}
		for i := range accountConversations {
			accountConversations[i].Note = notes[accountConversations[i].ID]
		}
		conversations = append(conversations, accountConversations...)
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
	return c.JSON(data)
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
	return c.JSON(data)
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
	return c.JSON(data)
}

func (h *EmailHandler) HandleConversations(c *fiber.Ctx) error {
	data, err := h.conversationPageData(c)
	if err != nil {
		if h.localUserWithoutMailbox(c) {
			return c.Redirect("/settings?setup=1")
		}
		return c.Status(500).SendString("Error loading local conversations")
	}
	return c.JSON(data)
}
