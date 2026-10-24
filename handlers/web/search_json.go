package web

import (
	stdhtml "html"
	"inbrix/mailstore"
	"inbrix/models"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
)

const (
	defaultMailSearchLimit = 50
	maxMailSearchLimit     = 200
	// mailSearchScanFactor widens the SQL scan beyond the requested page so the
	// plain-text verification below can discard matches that only hit HTML
	// markup without starving the page of real results.
	mailSearchScanFactor = 4
	minMailSearchScan    = 200
	// Snippet geometry: a little context before the hit, more after it.
	mailSearchSnippetLead = 40
	mailSearchSnippetTail = 160
)

// MailSearchResultJSON is one matching message. It carries the conversation
// identity and the message's folder/UID so the client can open the
// conversation and scroll straight to the matching bubble.
type MailSearchResultJSON struct {
	ConversationID    string `json:"conversationId"`
	MessageID         string `json:"messageId"`
	Folder            string `json:"folder"`
	Subject           string `json:"subject"`
	From              string `json:"from"`
	FromName          string `json:"fromName,omitempty"`
	To                string `json:"to"`
	Date              string `json:"date"`
	Snippet           string `json:"snippet"`
	ConversationTitle string `json:"conversationTitle"`
	PeerEmail         string `json:"peerEmail,omitempty"`
	AccountEmail      string `json:"accountEmail,omitempty"`
	AccountLabel      string `json:"accountLabel,omitempty"`
	AccountColor      string `json:"accountColor,omitempty"`
	Outgoing          bool   `json:"outgoing"`
	MatchedSubject    bool   `json:"matchedSubject"`
	MatchedBody       bool   `json:"matchedBody"`
}

// MailSearchJSON is the message-level search response. Terms is the normalized
// term list the server actually matched, so the client highlights exactly what
// the query meant instead of re-parsing the search grammar.
type MailSearchJSON struct {
	Query   string                 `json:"query"`
	Terms   []string               `json:"terms"`
	Results []MailSearchResultJSON `json:"results"`
	HasMore bool                   `json:"hasMore"`
}

func mailSearchLimit(c *fiber.Ctx) int {
	limit := localFolderPageInt(c, "limit", defaultMailSearchLimit)
	if limit <= 0 {
		return defaultMailSearchLimit
	}
	if limit > maxMailSearchLimit {
		return maxMailSearchLimit
	}
	return limit
}

// messagePlainText is the text a reader sees for a message. The app renders the
// HTML part whenever a message has one, so that projection wins; it also keeps
// markup out of snippets for senders whose text/plain alternative embeds VML or
// HTML fragments. Text-only mail falls back to its body, then to the preview.
func messagePlainText(email models.Email) string {
	return mailstore.AgentMailText(email)
}

// mailMessageMatchesTerms reports whether every term appears in the fields the
// search covers. The SQL pass also probes the raw HTML of body-less messages,
// so this second pass keeps markup-only hits (a style rule mentioning "color",
// for example) out of the result list.
func mailMessageMatchesTerms(email models.Email, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		email.Subject,
		email.From,
		email.FromName,
		email.To,
		email.Cc,
		email.Preview,
		messagePlainText(email),
	}, "\n"))
	for _, term := range terms {
		if !strings.Contains(haystack, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

func searchContainsTerm(haystack string, terms []string) bool {
	if haystack == "" {
		return false
	}
	lower := strings.ToLower(haystack)
	for _, term := range terms {
		if term != "" && strings.Contains(lower, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

// searchSnippet returns an excerpt of text around the earliest matching term.
// The second result reports whether a term was actually found, which lets the
// caller fall back to the head of the message when only the subject matched.
func searchSnippet(text string, terms []string) (string, bool) {
	normalized := strings.Join(strings.Fields(text), " ")
	if normalized == "" {
		return "", false
	}
	lower := strings.ToLower(normalized)
	match := -1
	for _, term := range terms {
		if term == "" {
			continue
		}
		if at := strings.Index(lower, strings.ToLower(term)); at >= 0 && (match < 0 || at < match) {
			match = at
		}
	}
	if match < 0 {
		return clipRunes(normalized, mailSearchSnippetLead+mailSearchSnippetTail), false
	}
	start := utf8.RuneCountInString(lower[:match]) - mailSearchSnippetLead
	if start < 0 {
		start = 0
	}
	runes := []rune(normalized)
	end := start + mailSearchSnippetLead + mailSearchSnippetTail
	if end > len(runes) {
		end = len(runes)
	}
	if start > len(runes) {
		start = len(runes)
	}
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet, true
}

// clipRunes trims text to at most limit runes, appending an ellipsis when it
// was cut.
func clipRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func mailSearchResultJSON(conversation Conversation, message ConversationMessage, terms []string) MailSearchResultJSON {
	email := message.Email
	plain := messagePlainText(email)
	// When only the headers or subject matched, searchSnippet still returns the
	// head of the mail so the row has context.
	snippet, matchedBody := searchSnippet(plain, terms)
	return MailSearchResultJSON{
		ConversationID:    conversation.ID,
		MessageID:         email.ID,
		Folder:            email.Folder,
		Subject:           stdhtml.UnescapeString(email.Subject),
		From:              email.From,
		FromName:          email.FromName,
		To:                email.To,
		Date:              email.Date.UTC().Format("2006-01-02T15:04:05.999Z07:00"),
		Snippet:           snippet,
		ConversationTitle: conversation.Title,
		PeerEmail:         conversation.PeerEmail,
		AccountEmail:      conversation.AccountEmail,
		AccountLabel:      conversation.AccountLabel,
		AccountColor:      conversation.AccountColor,
		Outgoing:          message.Outgoing,
		MatchedSubject:    searchContainsTerm(stdhtml.UnescapeString(email.Subject), terms),
		MatchedBody:       matchedBody,
	}
}

// HandleMailSearchJSON searches the local mail mirror for messages matching a
// query and reports message-level hits across the folders the conversation
// view is built from, so every result can be opened at its exact position.
// Route: GET /api/mail/search?q=<query>&limit=<n>
func (h *EmailHandler) HandleMailSearchJSON(c *fiber.Ctx) error {
	if h.mailDB == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Mail mirror is unavailable"})
	}
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "q parameter required"})
	}
	account, ok := h.currentMirrorAccount(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Mail account not found"})
	}
	limit := mailSearchLimit(c)
	scan := limit * mailSearchScanFactor
	if scan < minMailSearchScan {
		scan = minMailSearchScan
	}
	terms := mailstore.SearchTextTerms(query)
	if terms == nil {
		terms = []string{}
	}

	folders, err := h.mailDB.ListFolders(c.UserContext(), account.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not read local folders"})
	}

	seen := make(map[string]struct{})
	candidates := make([]models.Email, 0, scan)
	// The conversation view is built from the inbox and sent folders, so the
	// search is scoped to the same set. A hit from an unrelated folder could
	// not be opened in its conversation anyway.
	for _, name := range conversationFolderNames(folders) {
		emails, _, searchErr := h.mailDB.SearchMessages(c.UserContext(), account.ID, name, query, scan)
		if searchErr != nil {
			// A query built only from operators is not a server error; it
			// simply has nothing to match here.
			continue
		}
		for _, email := range emails {
			key := email.Folder + "\x00" + email.ID
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, email)
		}
	}
	// Pending and draft sends are part of the transcript as well.
	if outgoing, outgoingErr := h.mailDB.ListOutgoing(c.UserContext(), account.ID); outgoingErr == nil {
		for _, record := range outgoing {
			email := record.Message
			key := email.Folder + "\x00" + email.ID
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			if !mailMessageMatchesTerms(email, terms) {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, email)
		}
	}

	matches := make([]models.Email, 0, len(candidates))
	for _, email := range candidates {
		if mailMessageMatchesTerms(email, terms) {
			matches = append(matches, email)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Date.Equal(matches[j].Date) {
			return matches[i].ID > matches[j].ID
		}
		return matches[i].Date.After(matches[j].Date)
	})
	hasMore := len(matches) > limit
	if hasMore {
		matches = matches[:limit]
	}

	// Resolve each hit to the same conversation the inbox renders: saved
	// outbox thread metadata wins over the participant-derived identity.
	if metadata, metaErr := h.mailDB.OutgoingThreadMetadata(c.UserContext(), account.ID); metaErr == nil {
		for i := range matches {
			if saved, exists := metadata[mailstore.OutgoingMirrorMessageID(account.SMTPServer, matches[i].MessageID)]; exists {
				matches[i].ConversationID = saved.ConversationID
				if saved.SendStatus == "sent" {
					matches[i].Date = saved.Date
				}
			}
		}
	}

	results := make([]MailSearchResultJSON, 0, len(matches))
	for _, conversation := range buildConversations(account, matches) {
		for _, message := range conversation.Messages {
			results = append(results, mailSearchResultJSON(conversation, message, terms))
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Date > results[j].Date
	})

	return c.JSON(MailSearchJSON{
		Query:   query,
		Terms:   terms,
		Results: results,
		HasMore: hasMore,
	})
}
