// handlers/jsonapi/contacts_frequent.go — "frequently contacted" surfacing.
//
//	GET /v1/contacts/frequent?limit=  → { contacts: [{email, name, count, lastUsed}] }
//
// This exposes the existing per-account recent-recipient store that the
// send path already populates after every send. It is READ-ONLY and PER-ACCOUNT
// isolated by construction: the store file is keyed by the request's own
// sanitized username under config.Cache.Folder — the exact same path the send
// path writes to — so a caller only ever reads the addresses THEY sent to. When
// there is no local store (e.g. a brokered account whose sends were never
// recorded locally, or Cache.Folder unset) it degrades to an empty list.
package jsonapi

import (
	"strings"
	"time"

	"inbrix/handlers/api"

	"github.com/gofiber/fiber/v2"
)

// maxFrequentContacts caps how many frequently-contacted rows are returned.
const maxFrequentContacts = 50

// frequentContact is one "frequently contacted" row for the client.
type frequentContact struct {
	Email    string    `json:"email"`
	Name     string    `json:"name,omitempty"`
	Count    int       `json:"count"`
	LastUsed time.Time `json:"lastUsed"`
}

// handleFrequentContacts returns the account's most-contacted recipients, ordered
// by (count desc, lastUsed desc) — the ordering the store already applies.
func (h *Handler) handleFrequentContacts(c *fiber.Ctx) error {
	limit := int(uintQuery(c, "limit", 12))
	if limit <= 0 || limit > maxFrequentContacts {
		limit = maxFrequentContacts
	}

	scope := h.recipientsStoreScope(c)
	if scope == "" || h.kv == nil {
		return c.JSON(fiber.Map{"contacts": []frequentContact{}})
	}
	rs := api.NewRecipientsStore(h.kv, scope)

	entries, err := rs.Search("", limit)
	if err != nil {
		return fail(c, fiber.StatusInternalServerError, "could not read frequent contacts")
	}
	out := make([]frequentContact, 0, len(entries))
	for _, e := range entries {
		if strings.TrimSpace(e.Email) == "" {
			continue
		}
		out = append(out, frequentContact{
			Email:    e.Email,
			Name:     e.Name,
			Count:    e.Count,
			LastUsed: e.LastUsed,
		})
	}
	return c.JSON(fiber.Map{"contacts": out})
}

func (h *Handler) recipientsStoreScope(c *fiber.Ctx) string {
	username, _ := c.Locals("username").(string)
	if strings.TrimSpace(username) == "" {
		return ""
	}
	return api.SanitizeUsername(username)
}
