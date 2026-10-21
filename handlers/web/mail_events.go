package web

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"inbrix/events"
)

// MailMutationEvents publishes a generic invalidation event after any
// successful mail-related HTTP mutation. This is the safety net that keeps
// multiple tabs in step without teaching every handler about SSE: the handler
// writes the database, returns 2xx, and the middleware tells all tabs for the
// user to refetch.
//
// The per-user revision is bumped *before* the handler runs. A list read that
// races the response must not see the old revision with new data, otherwise
// the revision fast path could incorrectly answer "unchanged".
//
// High-frequency autosave endpoints (/api/drafts, /api/conversations/:id/ai-chat)
// are deliberately excluded. They do not change the mailbox list, and emitting
// on every autosave would turn a debounced keystroke into an invalidation
// storm.
func MailMutationEvents(hub *NotificationHub) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if hub == nil || !isMailMutationRequest(c) {
			return c.Next()
		}
		userID, _ := c.Locals("user_id").(string)
		if userID != "" {
			hub.BumpRevision(userID)
		}
		if err := c.Next(); err != nil {
			return err
		}
		if userID == "" || c.Response().StatusCode() >= fiber.StatusBadRequest {
			return nil
		}
		username, _ := c.Locals("username").(string)
		origin := strings.TrimSpace(c.Get("X-Client-Id"))
		if len(origin) > 128 {
			origin = origin[:128]
		}
		hub.Publish(events.Event{
			UserID:   userID,
			Username: username,
			Kind:     events.KindMailChanged,
			Origin:   origin,
		})
		return nil
	}
}

// isMailMutationRequest decides whether a request can change mail state. It
// intentionally does not inspect the response status: the revision bump must
// happen before the handler runs, while the event is only published after a
// successful response.
func isMailMutationRequest(c *fiber.Ctx) bool {
	switch c.Method() {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return false
	}
	path := c.Path()
	switch {
	case strings.HasPrefix(path, "/api/conversations"):
		// AI chat is a per-conversation autosave; it does not affect the
		// mailbox list and is written on every couple of keystrokes.
		return !strings.HasSuffix(path, "/ai-chat")
	case strings.HasPrefix(path, "/api/mail"):
		return true
	case strings.HasPrefix(path, "/api/compose"):
		return true
	case strings.HasPrefix(path, "/api/outbox"):
		return true
	case strings.HasPrefix(path, "/api/accounts"):
		return true
	case strings.HasPrefix(path, "/api/ai/mail-"):
		return true
	default:
		return false
	}
}
