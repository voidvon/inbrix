package web

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"inbrix/handlers/api"
	"inbrix/mailstore"
	"inbrix/models"

	"github.com/gofiber/fiber/v2"
)

type indexedMessage struct {
	ID       string
	Folder   string
	Date     time.Time
	Outgoing bool
}

func (h *EmailHandler) ensureConversationIndex(c *fiber.Ctx, accountID string) error {
	ctx := c.UserContext()
	if _, current, err := h.mailDB.ConversationIndexRevision(ctx, accountID); err != nil || current {
		return err
	}

	// Only rebuild on a source change. Requests for a current index do not
	// contend with a rebuild, and a concurrent source write invalidates it.
	h.conversationIndexMu.Lock()
	defer h.conversationIndexMu.Unlock()
	for range 2 {
		revision, current, err := h.mailDB.ConversationIndexRevision(ctx, accountID)
		if err != nil || current {
			return err
		}
		account, err := h.mailDB.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		conversations, results, err := h.localConversations(c, []mailstore.Account{account}, false)
		if err != nil {
			return err
		}
		if results[0].Err != nil {
			return results[0].Err
		}
		rows := make([]mailstore.ConversationIndexRow, 0, len(conversations))
		for _, conversation := range conversations {
			summary, err := json.Marshal(conversationSummaryJSON(conversation))
			if err != nil {
				return err
			}
			members := make([]indexedMessage, 0, len(conversation.Messages))
			for _, message := range conversation.Messages {
				members = append(members, indexedMessage{
					ID: message.Email.ID, Folder: message.Email.Folder, Date: message.Email.Date, Outgoing: message.Outgoing,
				})
			}
			messages, err := json.Marshal(members)
			if err != nil {
				return err
			}
			rows = append(rows, mailstore.ConversationIndexRow{
				ID: conversation.ID, Date: conversation.Latest.Date, Status: conversation.Status,
				UnreadCount: conversation.UnreadCount, Summary: summary, Messages: messages,
			})
		}
		saved, err := h.mailDB.ReplaceConversationIndex(ctx, accountID, revision, rows)
		if err != nil || saved {
			return err
		}
	}
	return mailstore.ErrConversationIndexStale
}

func (h *EmailHandler) indexedConversationListJSON(c *fiber.Ctx, filter string, limit int, cursor string, revision uint64) (ConversationListJSON, error) {
	var boundary conversationCursor
	if limit > 0 && strings.TrimSpace(cursor) != "" {
		var err error
		boundary, err = decodeConversationCursor(cursor)
		if err != nil {
			return ConversationListJSON{}, err
		}
	}
	active, accounts, err := h.conversationAccounts(c)
	if err != nil {
		return ConversationListJSON{}, err
	}
	if _, err := api.GetSessionToken(c, h.store); err != nil {
		return ConversationListJSON{}, err
	}
	accountIDs := make([]string, 0, len(accounts))
	for _, account := range accounts {
		if err := h.ensureConversationIndex(c, account.ID); err != nil {
			return ConversationListJSON{}, err
		}
		accountIDs = append(accountIDs, account.ID)
	}
	folders, err := h.mailDB.ListFolders(c.UserContext(), active.ID)
	if err != nil {
		return ConversationListJSON{}, err
	}
	page, err := h.mailDB.ReadConversationIndexPage(c.UserContext(), accountIDs,
		strings.EqualFold(strings.TrimSpace(filter), "open"), strings.TrimSpace(c.Query("tag")), boundary.Date, boundary.ID, limit)
	if err != nil {
		return ConversationListJSON{}, err
	}
	response := ConversationListJSON{
		Tags: page.Tags, Conversations: make([]ConversationSummaryJSON, 0, len(page.Summaries)),
		Folders: mirrorMailboxInfos(folders), Accounts: make([]ConversationAccountJSON, 0),
		AccountEmail: active.Email, Locale: CurrentLocale(c),
		Unified: c.Query("unified") == "1" && len(accounts) > 1, UnifiedAvailable: len(accounts) > 1,
		Revision: revision, HasMore: page.HasMore, Total: page.Total, UnreadCount: page.UnreadCount,
	}
	if page.HasMore {
		response.NextCursor = encodeConversationCursor(Conversation{ID: page.NextID, Latest: models.Email{Date: page.NextDate}})
	}
	for _, account := range h.mailAccountOptions(c) {
		response.Accounts = append(response.Accounts, ConversationAccountJSON(account))
	}
	for _, stored := range page.Summaries {
		var summary ConversationSummaryJSON
		if err := json.Unmarshal(stored, &summary); err != nil {
			return ConversationListJSON{}, err
		}
		response.Conversations = append(response.Conversations, summary)
	}
	return response, nil
}

func (h *EmailHandler) indexedConversation(c *fiber.Ctx, conversationID string) (*Conversation, int64, error) {
	_, accounts, err := h.conversationAccounts(c)
	if err != nil {
		return nil, 0, err
	}
	if _, err := api.GetSessionToken(c, h.store); err != nil {
		return nil, 0, err
	}
	ctx := c.UserContext()
	for _, account := range accounts {
		if err := h.ensureConversationIndex(c, account.ID); err != nil {
			return nil, 0, err
		}
		summaryJSON, messagesJSON, revision, err := h.mailDB.ReadConversationIndexDetail(ctx, account.ID, conversationID)
		if errors.Is(err, mailstore.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		var summary ConversationSummaryJSON
		if err := json.Unmarshal(summaryJSON, &summary); err != nil {
			return nil, 0, err
		}
		var members []indexedMessage
		if err := json.Unmarshal(messagesJSON, &members); err != nil {
			return nil, 0, err
		}
		selected := &Conversation{
			ID: summary.ID, Title: summary.Title, PeerEmail: summary.PeerEmail, Subject: summary.Subject,
			Tags: summary.Tags, Note: summary.Note, Count: summary.Count,
			AccountEmail: summary.AccountEmail, AccountLabel: summary.AccountLabel, AccountColor: summary.AccountColor,
			Messages: make([]ConversationMessage, 0, len(members)),
		}
		for _, member := range members {
			selected.Messages = append(selected.Messages, ConversationMessage{
				Email: models.Email{ID: member.ID, Folder: member.Folder, Date: member.Date}, Outgoing: member.Outgoing,
			})
		}
		return selected, revision, nil
	}
	return nil, 0, nil
}

func (h *EmailHandler) hydrateIndexedOutbox(c *fiber.Ctx, accountID string, conversation *Conversation) error {
	for i := range conversation.Messages {
		message := &conversation.Messages[i]
		if message.Email.Folder != "Outbox" {
			continue
		}
		record, err := h.mailDB.GetOutgoingWithoutMIME(c.UserContext(), accountID, message.Email.ID)
		if err != nil {
			return err
		}
		message.Email = record.Message
		decodeConversationMessageText(&message.Email)
	}
	return nil
}
