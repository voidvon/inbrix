package api

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// AutomationHandler discovers the running server's routes rather than keeping
// a second, potentially stale list. Reference hints come from the same embedded
// API documentation shipped with the application; they are not JSON schemas.
type AutomationHandler struct {
	store     *session.Store
	reference string
	hints     map[string]automationReference
	sections  map[string]string
}

type automationReference struct {
	Section string `json:"section"`
	Header  string `json:"tableHeader"`
	Row     string `json:"tableRow"`
}

type automationOperation struct {
	Method         string               `json:"method"`
	Path           string               `json:"path"`
	PathParameters []string             `json:"pathParameters"`
	ReadOnly       bool                 `json:"readOnly"`
	Reference      *automationReference `json:"reference,omitempty"`
}

var referenceOperationRE = regexp.MustCompile("^\\|\\s*`?(GET|POST|PUT|PATCH|DELETE)`?\\s*\\|\\s*`?([^` |?]+)`?\\s*\\|")

func NewAutomationHandler(store *session.Store, reference string) *AutomationHandler {
	h := &AutomationHandler{store: store, reference: reference, hints: make(map[string]automationReference), sections: make(map[string]string)}
	lines := strings.Split(reference, "\n")
	section, header := "", ""
	type heading struct {
		name         string
		index, level int
	}
	var headings []heading
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if strings.HasPrefix(line, "#") {
			level := len(line) - len(strings.TrimLeft(line, "#"))
			section = strings.TrimSpace(strings.TrimLeft(line, "#"))
			headings = append(headings, heading{section, i, level})
		}
		if !strings.HasPrefix(line, "|") {
			header = ""
		}
		if strings.HasPrefix(line, "| Method | Path |") {
			header = line
		}
		if match := referenceOperationRE.FindStringSubmatch(line); match != nil && header != "" {
			key := match[1] + " " + match[2]
			if _, exists := h.hints[key]; !exists {
				h.hints[key] = automationReference{Section: section, Header: header, Row: line}
			}
		}
	}
	for i, heading := range headings {
		end := len(lines)
		for _, next := range headings[i+1:] {
			if next.level <= heading.level {
				end = next.index
				break
			}
		}
		h.sections[heading.name] = strings.Join(lines[heading.index:end], "\n")
	}
	return h
}

// Register must be called on an authenticated group, alongside the other APIs.
func (h *AutomationHandler) Register(router fiber.Router) {
	router.Get("/automation", h.HandleCatalog)
	router.Get("/automation/reference", h.HandleReference)
}

func catalogPageInt(c *fiber.Ctx, name string, fallback int) (int, error) {
	value := c.Query(name)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || (name == "limit" && (n == 0 || n > 100)) {
		return 0, fiber.NewError(fiber.StatusBadRequest, "Invalid "+name)
	}
	return n, nil
}

func (h *AutomationHandler) HandleCatalog(c *fiber.Ctx) error {
	sess, err := ValidateSession(c, h.store)
	if err != nil {
		return fiber.ErrUnauthorized
	}
	c.Set(fiber.HeaderCacheControl, "private, no-store, max-age=0")
	limit, err := catalogPageInt(c, "limit", 50)
	if err != nil {
		return err
	}
	offset, err := catalogPageInt(c, "offset", 0)
	if err != nil {
		return err
	}
	method, path, query := strings.ToUpper(c.Query("method")), c.Query("path"), strings.ToLower(c.Query("q"))
	operations := make([]automationOperation, 0)
	seen := make(map[string]bool)
	for _, route := range c.App().GetRoutes(true) {
		if route.Method == fiber.MethodHead || route.Method == fiber.MethodOptions || (!strings.HasPrefix(route.Path, "/api/") && !strings.HasPrefix(route.Path, "/v1/")) {
			continue
		}
		key := route.Method + " " + route.Path
		if seen[key] || (method != "" && method != route.Method) || (path != "" && path != route.Path) {
			continue
		}
		seen[key] = true
		op := automationOperation{Method: route.Method, Path: route.Path, PathParameters: append([]string{}, route.Params...), ReadOnly: route.Method == fiber.MethodGet}
		search := key
		if hint, ok := h.hints[key]; ok {
			op.Reference = &hint
			search += " " + hint.Section + " " + hint.Row
		}
		if query != "" && !strings.Contains(strings.ToLower(search), query) {
			continue
		}
		operations = append(operations, op)
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Path != operations[j].Path {
			return operations[i].Path < operations[j].Path
		}
		return operations[i].Method < operations[j].Method
	})
	total := len(operations)
	start := min(offset, total)
	end := start + min(limit, total-start)
	var nextOffset *int
	if end < total {
		nextOffset = &end
	}
	return c.JSON(fiber.Map{
		"version":        1,
		"authentication": fiber.Map{"headers": []string{"Authorization: Bearer <API_KEY>", "X-API-Key: <API_KEY>"}, "permissions": "Current owner permissions; discovery does not grant access. Existing role and ownership checks still apply."},
		"mailboxes": fiber.Map{
			"listEndpoint": "/api/accounts", "selectionHeader": "X-Account-ID", "selectedAccountId": sess.Get("account_id"), "selectedEmail": sess.Get("email"),
			"instructions": "List application mailboxes via /api/accounts. Send X-Account-ID on EVERY mailbox request, including pagination, attachments and mutations. Without it each request uses the default mailbox. Selection is request-local; /api/accounts/:id/switch does not persist for API keys. Identify mail by (accountId, folder, uid). To search all application mailboxes, enumerate /api/accounts and search each explicitly. /v1/accounts and /v1/unified use a separate legacy connected-account store, not the application's mailbox list.",
		},
		"capabilitiesEndpoint": "/api/capabilities", "referenceEndpoint": "/api/automation/reference",
		"instructions": "Filter this catalog by q, method or exact route template path; page using limit (1-100) and offset. Path parameters use Fiber :name syntax and must be URL-encoded when substituted. Reference table rows are documentation hints, not OpenAPI schemas. For documented operations fetch /api/automation/reference?section=<reference.section> for details and examples. An absent reference means the parameter contract is not documented here; do not infer it from the path. Read the reference before mutations. A registered route may still return 403 for insufficient role, or 501/503 when its backing feature is unavailable.",
		"total":        total, "limit": limit, "offset": offset, "nextOffset": nextOffset, "operations": operations[start:end],
	})
}

func (h *AutomationHandler) HandleReference(c *fiber.Ctx) error {
	if _, err := ValidateSession(c, h.store); err != nil {
		return fiber.ErrUnauthorized
	}
	c.Set(fiber.HeaderCacheControl, "private, no-store, max-age=0")
	reference := h.reference
	if section := c.Query("section"); section != "" {
		var ok bool
		reference, ok = h.sections[section]
		if !ok {
			return fiber.ErrNotFound
		}
	}
	c.Set(fiber.HeaderContentType, "text/markdown; charset=utf-8")
	return c.SendString(reference)
}
