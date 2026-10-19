package web

import (
	"errors"
	"strings"

	"inbrix/handlers/htmlsafe"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// DraftHandler stores unsent compose windows server-side so a half-written
// reply follows the account to another computer. Drafts are owner-scoped: a
// caller only ever reads and writes its own, and another account's draft id
// answers exactly like a missing one.
type DraftHandler struct {
	store  *session.Store
	mailDB *mailstore.Store
}

func NewDraftHandler(store *session.Store, mailDB *mailstore.Store) *DraftHandler {
	return &DraftHandler{store: store, mailDB: mailDB}
}

// requestOwnerID resolves the authenticated user id for a request. The
// session middleware records it in Locals; the session itself is the fallback
// for callers that run outside that middleware.
func requestOwnerID(c *fiber.Ctx, sessions *session.Store) string {
	if userID, ok := c.Locals("user_id").(string); ok && strings.TrimSpace(userID) != "" {
		return strings.TrimSpace(userID)
	}
	if sessions != nil {
		if sess, err := sessions.Get(c); err == nil {
			if owner, _ := sess.Get("user_id").(string); strings.TrimSpace(owner) != "" {
				return strings.TrimSpace(owner)
			}
		}
	}
	return ""
}

func (h *DraftHandler) owner(c *fiber.Ctx) string {
	if h == nil {
		return ""
	}
	return requestOwnerID(c, h.store)
}

func (h *DraftHandler) ready(c *fiber.Ctx) (string, error) {
	if h == nil || h.mailDB == nil {
		return "", fiber.NewError(fiber.StatusNotImplemented, "Local database unavailable")
	}
	owner := h.owner(c)
	if owner == "" {
		return "", fiber.ErrUnauthorized
	}
	return owner, nil
}

// draftError maps store errors onto status codes. Invalid input is the caller's
// mistake (400); anything else is a server-side storage failure.
func draftError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "Draft not found")
	}
	if mailstore.IsDraftInputError(err) {
		message := strings.TrimPrefix(err.Error(), "mailstore: ")
		return fiber.NewError(fiber.StatusBadRequest, message)
	}
	return fiber.NewError(fiber.StatusInternalServerError, "Failed to access drafts")
}

type draftSaveInput struct {
	ID             string `json:"id"`
	AccountEmail   string `json:"accountEmail"`
	ConversationID string `json:"conversationId"`
	Mode           string `json:"mode"`
	InReplyTo      string `json:"inReplyTo"`
	References     string `json:"references"`
	To             string `json:"to"`
	Cc             string `json:"cc"`
	Subject        string `json:"subject"`
	BodyHTML       string `json:"bodyHtml"`
	BodyText       string `json:"bodyText"`
}

// HandleListDrafts returns the owner's drafts, newest first.
// Route: GET /api/drafts?conversation_id=
func (h *DraftHandler) HandleListDrafts(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	drafts, err := h.mailDB.ListDrafts(c.UserContext(), owner, c.Query("conversation_id"))
	if err != nil {
		return draftError(err)
	}
	return c.JSON(fiber.Map{"drafts": drafts})
}

// HandleGetDraft returns one draft.
// Route: GET /api/drafts/:id
func (h *DraftHandler) HandleGetDraft(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	draft, err := h.mailDB.GetDraft(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return draftError(err)
	}
	return c.JSON(fiber.Map{"draft": draft})
}

// HandleSaveDraft creates or updates a draft and returns the stored row,
// including the id the client must send back with its next autosave.
//
// The body is normalized through the same active-content policy the send path
// applies. That is not merely tidy: the stored HTML is handed back to the React
// editor, which assigns it to a contenteditable in the MAIN document, so raw
// mail HTML here would be a stored-XSS sink. Sanitizing on write means nothing
// dangerous is ever persisted, whatever a client sends.
//
// Route: POST /api/drafts
func (h *DraftHandler) HandleSaveDraft(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input draftSaveInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	plain, normalizedHTML, normalizeErr := htmlsafe.NormalizeComposeBodies(input.BodyText, input.BodyHTML)
	if normalizeErr != nil {
		return fiber.NewError(fiber.StatusBadRequest, "HTML body is invalid or too large")
	}

	draft, err := h.mailDB.SaveDraft(c.UserContext(), owner, mailstore.DraftInput{
		ID:             input.ID,
		AccountEmail:   input.AccountEmail,
		ConversationID: input.ConversationID,
		Mode:           input.Mode,
		InReplyTo:      input.InReplyTo,
		References:     input.References,
		To:             input.To,
		Cc:             input.Cc,
		Subject:        input.Subject,
		BodyHTML:       normalizedHTML,
		BodyText:       plain,
	})
	if err != nil {
		return draftError(err)
	}
	return c.JSON(fiber.Map{"draft": draft})
}

// HandleDeleteDraft discards a draft. Discarding an already-absent draft is
// success, so a double-click or a retry is never an error the UI must explain.
// Route: DELETE /api/drafts/:id
func (h *DraftHandler) HandleDeleteDraft(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	id := c.Params("id")
	if err := h.mailDB.DeleteDraft(c.UserContext(), owner, id); err != nil {
		return draftError(err)
	}
	return c.JSON(fiber.Map{"ok": true, "id": id})
}
