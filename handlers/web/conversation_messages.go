package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
)

const conversationMessagePageSize = 3

var errInvalidMessageCursor = errors.New("invalid message cursor")
var errMessageTargetNotFound = errors.New("message not found in conversation")

// Folder breaks ties between identical UIDs in different IMAP mailboxes.
// Including the conversation prevents accidentally using another thread's cursor.
type conversationMessageCursor struct {
	Conversation string    `json:"conversation"`
	Date         time.Time `json:"date"`
	ID           string    `json:"id"`
	Folder       string    `json:"folder"`
}

type conversationMessagePage struct {
	HasMore      bool   `json:"hasMore"`
	HasNewer     bool   `json:"hasNewer"`
	OldestCursor string `json:"oldestCursor,omitempty"`
	NewestCursor string `json:"newestCursor,omitempty"`
}

func messageCursor(conversationID string, message ConversationMessage) conversationMessageCursor {
	return conversationMessageCursor{Conversation: conversationID, Date: message.Email.Date, ID: message.Email.ID, Folder: message.Email.Folder}
}

func encodeMessageCursor(conversationID string, message ConversationMessage) string {
	raw, _ := json.Marshal(messageCursor(conversationID, message))
	return base64.RawURLEncoding.EncodeToString(raw)
}

func messageAtOrAfterCursor(message ConversationMessage, cursor conversationMessageCursor) bool {
	if !message.Email.Date.Equal(cursor.Date) {
		return message.Email.Date.After(cursor.Date)
	}
	if message.Email.ID != cursor.ID {
		return message.Email.ID > cursor.ID
	}
	return message.Email.Folder >= cursor.Folder
}

// Select headers before hydrating bodies/HTML/AI output. Search opens a bounded
// window around a message; either boundary can then be extended independently.
// through+until refreshes that window without pulling in the gap to latest mail.
func pageConversationMessages(c *fiber.Ctx, conversation *Conversation) (conversationMessagePage, error) {
	page := conversationMessagePage{}
	limit := conversationMessagePageSize
	if value, err := strconv.Atoi(c.Query("limit")); err == nil && value > 0 {
		limit = min(value, conversationMaxPageSize)
	}
	before, after, through, until, around := c.Query("cursor"), c.Query("after"), c.Query("through"), c.Query("until"), c.Query("around")
	modes := 0
	for _, value := range []string{before, after, through, around} {
		if value != "" {
			modes++
		}
	}
	if modes > 1 || (until != "" && through == "") {
		return page, errInvalidMessageCursor
	}
	boundary := func(raw string, inclusive bool) (int, error) {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		var cursor conversationMessageCursor
		if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Conversation != conversation.ID || cursor.ID == "" || cursor.Folder == "" {
			return 0, errInvalidMessageCursor
		}
		return sort.Search(len(conversation.Messages), func(i int) bool {
			message := conversation.Messages[i]
			equal := message.Email.Date.Equal(cursor.Date) && message.Email.ID == cursor.ID && message.Email.Folder == cursor.Folder
			return messageAtOrAfterCursor(message, cursor) && (inclusive || !equal)
		}), nil
	}
	total := len(conversation.Messages)
	end := total
	start := max(0, end-limit)
	if around != "" {
		folder := c.Query("folder", "INBOX")
		target := -1
		for i, message := range conversation.Messages {
			if message.Email.ID == around && message.Email.Folder == folder {
				target = i
				break
			}
		}
		if target < 0 {
			return page, errMessageTargetNotFound
		}
		start = max(0, min(target-limit/2, total-limit))
		end = min(total, start+limit)
	} else if before != "" {
		var err error
		end, err = boundary(before, true)
		if err != nil {
			return page, err
		}
		start = max(0, end-limit)
	} else if after != "" {
		var err error
		start, err = boundary(after, false)
		if err != nil {
			return page, err
		}
		end = min(total, start+limit)
	} else if through != "" {
		var err error
		start, err = boundary(through, true)
		if err != nil {
			return page, err
		}
		if until != "" {
			end, err = boundary(until, false)
			if err != nil || start > end {
				return page, errInvalidMessageCursor
			}
		} else {
			start = min(start, max(0, end-limit))
		}
	}
	page.HasMore, page.HasNewer = start > 0, end < total
	conversation.Messages = conversation.Messages[start:end]
	if len(conversation.Messages) > 0 {
		page.OldestCursor = encodeMessageCursor(conversation.ID, conversation.Messages[0])
		page.NewestCursor = encodeMessageCursor(conversation.ID, conversation.Messages[len(conversation.Messages)-1])
	} else if through != "" && until != "" {
		// A whole loaded window can be deleted. Retain its ordering boundaries
		// so the client can still navigate to surviving mail on either side.
		page.OldestCursor, page.NewestCursor = through, until
	}
	return page, nil
}
