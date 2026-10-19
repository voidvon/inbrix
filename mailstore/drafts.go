package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// A draft is an unsent compose window that outlives the browser tab. It is kept
// on the server rather than in browser storage for one reason: the same account
// signs in from several computers, and a half-written reply must follow the
// user between them.
//
// Identity: a draft belongs to exactly one (owner, account, conversation) when
// it answers a conversation, so clicking Reply again reopens the draft that is
// already there instead of starting a second one. A brand new message has no
// conversation to key on, so it keeps its own id and the client sends that id
// back with every autosave.

// Draft modes. The mode is descriptive: it records which button opened the
// compose window, and is NOT part of the draft's identity.
const (
	DraftModeNew      = "new"
	DraftModeReply    = "reply"
	DraftModeReplyAll = "reply_all"
)

// Bounds. The HTML bound matches htmlsafe's compose-body bound, so a draft can
// never hold more than a message would be allowed to send. The rest are
// generous limits on the small header fields.
const (
	maxDraftHTMLBytes    = 2 * 1024 * 1024
	maxDraftTextBytes    = 2 * 1024 * 1024
	maxDraftSubjectRunes = 1000
	maxDraftAddressBytes = 8 * 1024
	maxDraftTokenBytes   = 4 * 1024
)

var draftModes = map[string]bool{
	DraftModeNew: true, DraftModeReply: true, DraftModeReplyAll: true,
}

// Draft is one stored compose window. Bodies are stored as the rich HTML the
// editor produced, plus the plain-text alternative derived at save time.
type Draft struct {
	ID             string    `json:"id"`
	OwnerID        string    `json:"ownerId,omitempty"`
	AccountEmail   string    `json:"accountEmail"`
	ConversationID string    `json:"conversationId"`
	Mode           string    `json:"mode"`
	InReplyTo      string    `json:"inReplyTo"`
	References     string    `json:"references"`
	To             string    `json:"to"`
	Cc             string    `json:"cc"`
	Subject        string    `json:"subject"`
	BodyHTML       string    `json:"bodyHtml"`
	BodyText       string    `json:"bodyText"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// DraftInput is the caller-supplied state of one compose window. ID is empty
// for a first save; the store fills it in and returns it so the client can send
// it back on the next autosave.
type DraftInput struct {
	ID             string
	AccountEmail   string
	ConversationID string
	Mode           string
	InReplyTo      string
	References     string
	To             string
	Cc             string
	Subject        string
	BodyHTML       string
	BodyText       string
}

// draftInputError marks a caller mistake so handlers can answer 400 rather than
// 500 without pattern-matching on message text.
type draftInputError struct{ message string }

func (e draftInputError) Error() string { return "mailstore: " + e.message }

// IsDraftInputError reports whether err describes invalid draft input rather
// than a storage failure.
func IsDraftInputError(err error) bool {
	var target draftInputError
	return errors.As(err, &target)
}

func normalizeDraftInput(in DraftInput) (DraftInput, error) {
	in.ID = strings.TrimSpace(in.ID)
	in.AccountEmail = strings.TrimSpace(in.AccountEmail)
	in.ConversationID = strings.TrimSpace(in.ConversationID)
	in.Mode = strings.TrimSpace(in.Mode)
	in.InReplyTo = strings.TrimSpace(in.InReplyTo)
	in.References = strings.TrimSpace(in.References)
	in.To = strings.TrimSpace(in.To)
	in.Cc = strings.TrimSpace(in.Cc)
	in.Subject = strings.TrimSpace(in.Subject)

	if in.Mode == "" {
		in.Mode = DraftModeNew
	}
	if !draftModes[in.Mode] {
		return DraftInput{}, draftInputError{message: "unknown draft mode"}
	}
	if utf8.RuneCountInString(in.Subject) > maxDraftSubjectRunes {
		return DraftInput{}, draftInputError{message: "draft subject is too long"}
	}
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"draft recipients", in.To, maxDraftAddressBytes},
		{"draft copy recipients", in.Cc, maxDraftAddressBytes},
		{"draft references", in.References, maxDraftTokenBytes},
		{"draft reply target", in.InReplyTo, maxDraftTokenBytes},
	} {
		if len(field.value) > field.limit {
			return DraftInput{}, draftInputError{message: field.name + " is too long"}
		}
	}
	if len(in.BodyHTML) > maxDraftHTMLBytes {
		return DraftInput{}, draftInputError{message: "draft body is too large"}
	}
	if len(in.BodyText) > maxDraftTextBytes {
		return DraftInput{}, draftInputError{message: "draft body is too large"}
	}
	return in, nil
}

// IsEmptyDraft reports whether a draft carries nothing worth keeping. The
// compose window calls this before creating a row, so opening Reply and
// immediately closing it does not leave an empty draft behind.
func IsEmptyDraft(in DraftInput) bool {
	return strings.TrimSpace(in.To) == "" &&
		strings.TrimSpace(in.Cc) == "" &&
		strings.TrimSpace(in.Subject) == "" &&
		strings.TrimSpace(in.BodyText) == "" &&
		strings.TrimSpace(in.BodyHTML) == ""
}

// SaveDraft creates or updates one draft and returns the stored row. The
// returned ID is the handle the client uses for every later autosave.
func (s *Store) SaveDraft(ctx context.Context, ownerID string, in DraftInput) (Draft, error) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()

	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return Draft{}, draftInputError{message: "owner_id is required"}
	}
	normalized, err := normalizeDraftInput(in)
	if err != nil {
		return Draft{}, err
	}

	now := time.Now().Unix()
	id, createdAt, err := s.resolveDraftIdentity(ctx, ownerID, normalized, now)
	if err != nil {
		return Draft{}, err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO drafts(id, owner_id, account_email, conversation_id, mode, in_reply_to, refs,
			to_addrs, cc_addrs, subject, body_html, body_text, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_email = excluded.account_email,
			conversation_id = excluded.conversation_id,
			mode = excluded.mode,
			in_reply_to = excluded.in_reply_to,
			refs = excluded.refs,
			to_addrs = excluded.to_addrs,
			cc_addrs = excluded.cc_addrs,
			subject = excluded.subject,
			body_html = excluded.body_html,
			body_text = excluded.body_text,
			updated_at = excluded.updated_at
		WHERE drafts.owner_id = excluded.owner_id`,
		id, ownerID, normalized.AccountEmail, normalized.ConversationID, normalized.Mode,
		normalized.InReplyTo, normalized.References, normalized.To, normalized.Cc,
		normalized.Subject, normalized.BodyHTML, normalized.BodyText, createdAt, now)
	if err != nil {
		return Draft{}, fmt.Errorf("mailstore: save draft: %w", err)
	}
	return s.GetDraft(ctx, ownerID, id)
}

// resolveDraftIdentity decides which row a save targets: the draft already
// parked on this conversation, an existing draft named by id, or a new row.
func (s *Store) resolveDraftIdentity(ctx context.Context, ownerID string, in DraftInput, now int64) (string, int64, error) {
	// A conversation draft is identified by its conversation, so clicking Reply
	// a second time lands on the row that is already there.
	if in.ConversationID != "" {
		var id string
		var createdAt int64
		err := s.db.QueryRowContext(ctx, `
			SELECT id, created_at FROM drafts
			WHERE owner_id = ? AND account_email = ? AND conversation_id = ?`,
			ownerID, in.AccountEmail, in.ConversationID).Scan(&id, &createdAt)
		switch {
		case err == nil:
			return id, createdAt, nil
		case !errors.Is(err, sql.ErrNoRows):
			return "", 0, fmt.Errorf("mailstore: look up draft: %w", err)
		}
	}

	// Otherwise a handle the client already holds wins, as long as it is one of
	// ours. Ids are minted here, so an unrecognised shape is treated as "no
	// handle" rather than trusted as a key.
	//
	// Anything else — unknown shape, unknown id, someone else's id, or a handle
	// that belongs to a different conversation — mints a fresh id. Those cases
	// are deliberately indistinguishable: a caller never learns whether an id it
	// does not own exists. The client always adopts the id from the response, so
	// a lost handle costs nothing.
	if validDraftID(in.ID) {
		existing, err := s.GetDraft(ctx, ownerID, in.ID)
		switch {
		case err == nil && (existing.ConversationID == "" || existing.ConversationID == in.ConversationID):
			return existing.ID, existing.CreatedAt.Unix(), nil
		case err == nil, errors.Is(err, ErrNotFound):
			// Fall through and mint a new id.
		default:
			return "", 0, err
		}
	}

	id, err := newID("draft")
	if err != nil {
		return "", 0, err
	}
	return id, now, nil
}

// validDraftID accepts only ids this package mints, so a client cannot pin an
// arbitrary string as a primary key.
func validDraftID(id string) bool {
	if len(id) != len("draft_")+32 || !strings.HasPrefix(id, "draft_") {
		return false
	}
	for _, r := range id[len("draft_"):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// ListDrafts returns the owner's drafts, newest first. A non-empty
// conversationID narrows the result to that conversation's draft, which is what
// the compose window asks for when it opens on a reply.
func (s *Store) ListDrafts(ctx context.Context, ownerID, conversationID string) ([]Draft, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, draftInputError{message: "owner_id is required"}
	}
	query := `
		SELECT id, owner_id, account_email, conversation_id, mode, in_reply_to, refs,
			to_addrs, cc_addrs, subject, body_html, body_text, created_at, updated_at
		FROM drafts WHERE owner_id = ?`
	args := []any{ownerID}
	if trimmed := strings.TrimSpace(conversationID); trimmed != "" {
		query += ` AND conversation_id = ?`
		args = append(args, trimmed)
	}
	query += ` ORDER BY updated_at DESC, id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list drafts: %w", err)
	}
	defer rows.Close()

	drafts := make([]Draft, 0, 8)
	for rows.Next() {
		draft, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mailstore: list drafts: %w", err)
	}
	return drafts, nil
}

// GetDraft returns one draft the owner owns, or ErrNotFound. Another owner's id
// is indistinguishable from a missing id, so draft ids do not leak existence.
func (s *Store) GetDraft(ctx context.Context, ownerID, id string) (Draft, error) {
	ownerID = strings.TrimSpace(ownerID)
	id = strings.TrimSpace(id)
	if ownerID == "" {
		return Draft{}, draftInputError{message: "owner_id is required"}
	}
	if id == "" {
		return Draft{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_id, account_email, conversation_id, mode, in_reply_to, refs,
			to_addrs, cc_addrs, subject, body_html, body_text, created_at, updated_at
		FROM drafts WHERE owner_id = ? AND id = ?`, ownerID, id)
	draft, err := scanDraft(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	return draft, err
}

// DeleteDraft removes one draft. Deleting an already-absent draft is not an
// error: the send path calls this after the message is queued, and a draft that
// never reached the server must not fail that call.
func (s *Store) DeleteDraft(ctx context.Context, ownerID, id string) error {
	ownerID = strings.TrimSpace(ownerID)
	id = strings.TrimSpace(id)
	if ownerID == "" {
		return draftInputError{message: "owner_id is required"}
	}
	if id == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM drafts WHERE owner_id = ? AND id = ?`, ownerID, id); err != nil {
		return fmt.Errorf("mailstore: delete draft: %w", err)
	}
	return nil
}

// DeleteDraftsForConversation drops every draft parked on a conversation. It
// runs after a reply is queued so a sent reply cannot leave its draft behind,
// even when the browser never got the chance to clean up.
func (s *Store) DeleteDraftsForConversation(ctx context.Context, ownerID, accountEmail, conversationID string) (int64, error) {
	ownerID = strings.TrimSpace(ownerID)
	conversationID = strings.TrimSpace(conversationID)
	if ownerID == "" {
		return 0, draftInputError{message: "owner_id is required"}
	}
	if conversationID == "" {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM drafts
		WHERE owner_id = ? AND conversation_id = ? AND (? = '' OR account_email = ?)`,
		ownerID, conversationID, strings.TrimSpace(accountEmail), strings.TrimSpace(accountEmail))
	if err != nil {
		return 0, fmt.Errorf("mailstore: delete conversation drafts: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return removed, nil
}

type draftScanner interface {
	Scan(dest ...any) error
}

func scanDraft(row draftScanner) (Draft, error) {
	var draft Draft
	var createdAt, updatedAt int64
	if err := row.Scan(
		&draft.ID, &draft.OwnerID, &draft.AccountEmail, &draft.ConversationID, &draft.Mode,
		&draft.InReplyTo, &draft.References, &draft.To, &draft.Cc, &draft.Subject,
		&draft.BodyHTML, &draft.BodyText, &createdAt, &updatedAt,
	); err != nil {
		return Draft{}, err
	}
	draft.CreatedAt = time.Unix(createdAt, 0)
	draft.UpdatedAt = time.Unix(updatedAt, 0)
	return draft, nil
}
