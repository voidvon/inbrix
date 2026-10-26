package mailstore

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrAIChatConflict = errors.New("会话已更新或仍在生成，请刷新后重试 / Chat changed or is busy; refresh and retry")
var ErrAIChatAttachmentUnavailable = errors.New("图片不存在、已过期或不属于当前邮箱")
var ErrAIChatAttachmentInUse = errors.New("图片已被聊天记录引用 / Image is used by a chat message")
var ErrAIChatCursor = errors.New("invalid session cursor")

type AIChatRunState struct {
	RequestID     string `json:"requestId"`
	Status        string `json:"status"`
	UserMessageID string `json:"userMessageId"`
	Retryable     bool   `json:"retryable"`
}

type AIChatSession struct {
	CurrentSavedDocument        *DocumentRecord   `json:"-"`
	CurrentSavedDocumentMissing bool              `json:"-"`
	CurrentDocument             json.RawMessage   `json:"currentDocument,omitempty"`
	Memory                      AIChatContext     `json:"-"`
	LastRun                     *AIChatRunState   `json:"lastRun,omitempty"`
	ID                          string            `json:"id"`
	AccountID                   string            `json:"-"`
	Kind                        string            `json:"kind"`
	ConversationID              string            `json:"conversationId,omitempty"`
	Title                       string            `json:"title"`
	DocumentType                string            `json:"documentType"`
	BusinessContext             AIBusinessContext `json:"businessContext"`
	Version                     int64             `json:"version"`
	CreatedAt                   string            `json:"createdAt"`
	UpdatedAt                   string            `json:"updatedAt"`
	Messages                    []json.RawMessage `json:"messages"`
	HasMoreMessages             bool              `json:"hasMoreMessages"`
	NextBefore                  *int64            `json:"nextBefore,omitempty"`
}

type AIChatTurnOptions struct {
	Request json.RawMessage
	// CurrentDocument starts a fresh draft atomically with its user request.
	CurrentDocument json.RawMessage
}

type AIChatTurn struct {
	SessionID, RequestID, Token, UserMessageID string
	Version                                    int64
	Replay                                     json.RawMessage
}

func (s *Store) migrateAIChatSessions(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS ai_chat_sessions(id TEXT PRIMARY KEY, account_id TEXT NOT NULL REFERENCES mail_accounts(id) ON DELETE CASCADE, kind TEXT NOT NULL CHECK(kind IN ('mail','document')), conversation_id TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', document_type TEXT NOT NULL DEFAULT 'quotation', business_context TEXT NOT NULL DEFAULT '{}', version INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ai_chat_mail_session ON ai_chat_sessions(account_id,conversation_id) WHERE kind='mail'`,
		`CREATE INDEX IF NOT EXISTS ai_chat_sessions_account ON ai_chat_sessions(account_id,kind,updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS ai_chat_messages(id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES ai_chat_sessions(id) ON DELETE CASCADE, position INTEGER NOT NULL, payload TEXT NOT NULL, UNIQUE(session_id,position))`,
		`CREATE TABLE IF NOT EXISTS ai_chat_runs(session_id TEXT NOT NULL REFERENCES ai_chat_sessions(id) ON DELETE CASCADE, request_id TEXT NOT NULL, request_hash TEXT NOT NULL, token TEXT NOT NULL, status TEXT NOT NULL, lease_until INTEGER NOT NULL, user_message_id TEXT NOT NULL, user_version INTEGER NOT NULL, response TEXT NOT NULL DEFAULT '', PRIMARY KEY(session_id,request_id))`,
		`CREATE TABLE IF NOT EXISTS ai_chat_attachment_refs(message_id TEXT NOT NULL REFERENCES ai_chat_messages(id) ON DELETE CASCADE, attachment_id TEXT NOT NULL REFERENCES ai_chat_attachments(id) ON DELETE RESTRICT, PRIMARY KEY(message_id,attachment_id))`,
		// Existing uploads lack a lifecycle row and stay protected until adopted. Their
		// unscoped browser histories cannot be safely inferred or expired server-side.
		`CREATE TABLE IF NOT EXISTS ai_chat_attachment_lifecycle(attachment_id TEXT PRIMARY KEY REFERENCES ai_chat_attachments(id) ON DELETE CASCADE, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ai_chat_run_inputs(session_id TEXT NOT NULL, request_id TEXT NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(session_id,request_id), FOREIGN KEY(session_id,request_id) REFERENCES ai_chat_runs(session_id,request_id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS ai_chat_session_state(session_id TEXT PRIMARY KEY REFERENCES ai_chat_sessions(id) ON DELETE CASCADE, memory TEXT NOT NULL DEFAULT '{}', current_document TEXT NOT NULL DEFAULT 'null')`,
		`CREATE TABLE IF NOT EXISTS ai_chat_current_documents(session_id TEXT PRIMARY KEY REFERENCES ai_chat_sessions(id) ON DELETE CASCADE, message_id TEXT NOT NULL REFERENCES ai_chat_messages(id) ON DELETE CASCADE)`,
		// Retain links when a saved document is deleted so an old card cannot recreate it.
		`CREATE TABLE IF NOT EXISTS ai_chat_document_links(message_id TEXT PRIMARY KEY REFERENCES ai_chat_messages(id) ON DELETE CASCADE, document_id TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS ai_chat_document_links_document ON ai_chat_document_links(document_id)`,
		`INSERT OR IGNORE INTO ai_chat_session_state(session_id,current_document) SELECT s.id,COALESCE((SELECT json_extract(m.payload,'$.document') FROM ai_chat_messages m WHERE m.session_id=s.id AND json_type(m.payload,'$.document')='object' ORDER BY m.position DESC LIMIT 1),'null') FROM ai_chat_sessions s WHERE NOT EXISTS(SELECT 1 FROM ai_chat_session_state state WHERE state.session_id=s.id)`,
		`CREATE INDEX IF NOT EXISTS ai_chat_runs_version ON ai_chat_runs(session_id,user_version DESC)`,
		`CREATE INDEX IF NOT EXISTS ai_chat_runs_pending ON ai_chat_runs(session_id,lease_until) WHERE status='pending'`,
		`CREATE INDEX IF NOT EXISTS ai_chat_refs_attachment ON ai_chat_attachment_refs(attachment_id)`,
		`CREATE INDEX IF NOT EXISTS ai_chat_lifecycle_expiry ON ai_chat_attachment_lifecycle(expires_at)`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("chat migration: %w", err)
		}
	}
	if err := s.backfillAIChatCurrentDocuments(ctx); err != nil {
		return err
	}
	return s.GCAIChatAttachments(ctx)
}

func (s *Store) CreateAIChatSession(ctx context.Context, owner, accountID, kind, conversationID, title, documentType string, page ...AIChatMessagePage) (AIChatSession, error) {
	if kind == "mail" {
		var existing string
		e := s.db.QueryRowContext(ctx, `SELECT s.id FROM ai_chat_sessions s JOIN mail_accounts a ON a.id=s.account_id WHERE a.owner_id=? AND a.id=? AND s.kind='mail' AND s.conversation_id=?`, owner, accountID, conversationID).Scan(&existing)
		if e == nil {
			return s.GetAIChatSession(ctx, owner, accountID, existing, page...)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return AIChatSession{}, e
		}
	}
	id, err := newID("chat")
	if err != nil {
		return AIChatSession{}, err
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIChatSession{}, err
	}
	defer tx.Rollback()
	// INSERT is the first statement, serializing concurrent lazy mail imports.
	res, err := tx.ExecContext(ctx, `INSERT INTO ai_chat_sessions(id,account_id,kind,conversation_id,title,document_type,created_at,updated_at) SELECT ?,id,?,?,?,?,?,? FROM mail_accounts WHERE id=? AND owner_id=? ON CONFLICT DO NOTHING`, id, kind, conversationID, title, documentType, now, now, accountID, owner)
	if err != nil {
		return AIChatSession{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if kind != "mail" {
			return AIChatSession{}, ErrNotFound
		}
		if err = tx.QueryRowContext(ctx, `SELECT s.id FROM ai_chat_sessions s JOIN mail_accounts a ON a.id=s.account_id WHERE s.account_id=? AND a.owner_id=? AND kind='mail' AND conversation_id=?`, accountID, owner, conversationID).Scan(&id); err != nil {
			return AIChatSession{}, ErrNotFound
		}
	} else if kind == "mail" {
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT messages FROM conversation_ai_chats WHERE account_id=? AND conversation_id=?`, accountID, conversationID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return AIChatSession{}, err
		}
		var messages []json.RawMessage
		if json.Unmarshal([]byte(raw), &messages) == nil {
			for _, m := range messages {
				if _, err = insertAIChatMessage(ctx, tx, id, accountID, m, false); err != nil {
					return AIChatSession{}, err
				}
			}
		}
		var business string
		if err = tx.QueryRowContext(ctx, `SELECT context_json FROM conversation_ai_business WHERE account_id=? AND conversation_id=?`, accountID, conversationID).Scan(&business); err == nil {
			if _, err = tx.ExecContext(ctx, `UPDATE ai_chat_sessions SET business_context=? WHERE id=?`, business, id); err != nil {
				return AIChatSession{}, err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return AIChatSession{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ai_chat_session_state(session_id,current_document) SELECT id,COALESCE((SELECT json_extract(payload,'$.document') FROM ai_chat_messages WHERE session_id=? AND json_type(payload,'$.document')='object' ORDER BY position DESC LIMIT 1),'null') FROM ai_chat_sessions WHERE id=?`, id, id); err != nil {
		return AIChatSession{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ai_chat_current_documents(session_id,message_id) SELECT ?,id FROM ai_chat_messages WHERE session_id=? AND json_type(payload,'$.document')='object' ORDER BY position DESC LIMIT 1`, id, id); err != nil {
		return AIChatSession{}, err
	}

	if err = tx.Commit(); err != nil {
		return AIChatSession{}, err
	}
	return s.GetAIChatSession(ctx, owner, accountID, id, page...)
}

type AIChatMessagePage struct {
	Limit  int
	Before *int64
}

func (s *Store) GetAIChatSession(ctx context.Context, owner, accountID, id string, page ...AIChatMessagePage) (AIChatSession, error) {
	var out AIChatSession
	var business string
	var created, updated int64
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT s.id,s.account_id,s.kind,s.conversation_id,s.title,s.document_type,s.business_context,s.version,s.created_at,s.updated_at FROM ai_chat_sessions s JOIN mail_accounts a ON a.id=s.account_id WHERE s.id=? AND a.id=? AND a.owner_id=?`, id, accountID, owner).Scan(&out.ID, &out.AccountID, &out.Kind, &out.ConversationID, &out.Title, &out.DocumentType, &business, &out.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	var memory, document string
	if err = tx.QueryRowContext(ctx, `SELECT memory,current_document FROM ai_chat_session_state WHERE session_id=?`, id).Scan(&memory, &document); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if memory != "" {
		if err = json.Unmarshal([]byte(memory), &out.Memory); err != nil {
			return out, err
		}
	}
	if document != "" && document != "null" {
		out.CurrentDocument = json.RawMessage(document)
	}
	var savedID string
	err = tx.QueryRowContext(ctx, `SELECT l.document_id FROM ai_chat_current_documents c JOIN ai_chat_document_links l ON l.message_id=c.message_id WHERE c.session_id=?`, id).Scan(&savedID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if savedID != "" {
		saved := &DocumentRecord{}
		var savedCreated, savedUpdated int64
		err = tx.QueryRowContext(ctx, `SELECT id,type,name,company,html,revision,created_at,updated_at FROM documents WHERE id=? AND owner_id=?`, savedID, owner).Scan(&saved.ID, &saved.Type, &saved.Name, &saved.Company, &saved.HTML, &saved.Revision, &savedCreated, &savedUpdated)
		if errors.Is(err, sql.ErrNoRows) {
			out.CurrentSavedDocumentMissing = true
		} else if err != nil {
			return out, err
		} else {
			saved.OwnerID = owner
			saved.CreatedAt = time.UnixMilli(savedCreated)
			saved.UpdatedAt = time.UnixMilli(savedUpdated)
			out.CurrentSavedDocument = saved
		}
	}

	_ = json.Unmarshal([]byte(business), &out.BusinessContext)
	if out.BusinessContext.Brands == nil {
		out.BusinessContext.Brands = []string{}
	}
	out.CreatedAt = time.UnixMilli(created).UTC().Format(time.RFC3339Nano)
	out.UpdatedAt = time.UnixMilli(updated).UTC().Format(time.RFC3339Nano)
	var last AIChatRunState
	var lease int64
	err = tx.QueryRowContext(ctx, `SELECT r.request_id,r.status,r.user_message_id,r.lease_until, r.user_version=? AND EXISTS(SELECT 1 FROM ai_chat_run_inputs i WHERE i.session_id=r.session_id AND i.request_id=r.request_id) FROM ai_chat_runs r WHERE r.session_id=? ORDER BY r.user_version DESC LIMIT 1`, out.Version, id).Scan(&last.RequestID, &last.Status, &last.UserMessageID, &lease, &last.Retryable)
	if err == nil {
		if last.Status == "pending" && lease <= time.Now().Unix() {
			last.Status = "failed"
		}
		last.Retryable = last.Retryable && last.Status == "failed"
		out.LastRun = &last
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out.Messages = []json.RawMessage{}
	// Read messages and version from one SQLite snapshot. Internal execution can
	// request the complete authoritative history; UI reads use bounded pages.
	query := `SELECT m.position,m.payload,COALESCE(l.document_id,'') FROM ai_chat_messages m LEFT JOIN ai_chat_document_links l ON l.message_id=m.id WHERE m.session_id=? ORDER BY m.position`
	args := []any{id}
	limit := 0
	if len(page) > 0 {
		limit = page[0].Limit
		if limit < 1 || limit > 100 {
			limit = 100
		}
		before := int64(1<<63 - 1)
		if page[0].Before != nil {
			before = *page[0].Before
		}
		query = `SELECT m.position,m.payload,COALESCE(l.document_id,'') FROM ai_chat_messages m LEFT JOIN ai_chat_document_links l ON l.message_id=m.id WHERE m.session_id=? AND m.position<? ORDER BY m.position DESC LIMIT ?`
		args = append(args, before, limit+1)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var first int64
	for rows.Next() {
		var position int64
		var p string
		var linked string
		if err = rows.Scan(&position, &p, &linked); err != nil {
			return out, err
		}
		if linked != "" {
			var message map[string]any
			if err = json.Unmarshal([]byte(p), &message); err != nil {
				return out, err
			}
			if card, ok := message["document"].(map[string]any); ok {
				card["savedDocumentId"] = linked
			}
			raw, e := json.Marshal(message)
			if e != nil {
				return out, e
			}
			p = string(raw)
		}
		if limit > 0 && len(out.Messages) == limit {
			out.HasMoreMessages = true
			break
		}
		first = position
		out.Messages = append(out.Messages, json.RawMessage(p))
	}
	if limit > 0 {
		for i, j := 0, len(out.Messages)-1; i < j; i, j = i+1, j-1 {
			out.Messages[i], out.Messages[j] = out.Messages[j], out.Messages[i]
		}
		if out.HasMoreMessages {
			out.NextBefore = &first
		}
	}
	return out, rows.Err()
}

type AIChatSessionPage struct {
	Sessions   []AIChatSession `json:"sessions"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

func (s *Store) ListAIChatSessions(ctx context.Context, owner, accountID string) ([]AIChatSession, error) {
	page, err := s.ListAIChatSessionPage(ctx, owner, accountID, 50, "")
	return page.Sessions, err
}

// Summary rows never read message payloads. The cursor also orders equal timestamps.
func (s *Store) ListAIChatSessionPage(ctx context.Context, owner, accountID string, limit int, cursor string) (AIChatSessionPage, error) {
	out := AIChatSessionPage{Sessions: []AIChatSession{}}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	var after struct {
		Updated int64  `json:"updated"`
		ID      string `json:"id"`
	}
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(raw, &after) != nil || after.ID == "" {
			return out, ErrAIChatCursor
		}
	}
	query := `SELECT s.id,s.kind,s.title,s.document_type,s.business_context,s.version,s.created_at,s.updated_at,
 COALESCE(r.request_id,''),COALESCE(r.status,''),COALESCE(r.user_message_id,''),COALESCE(r.lease_until,0),
 r.user_version=s.version AND EXISTS(SELECT 1 FROM ai_chat_run_inputs i WHERE i.session_id=r.session_id AND i.request_id=r.request_id)
 FROM ai_chat_sessions s JOIN mail_accounts a ON a.id=s.account_id
 LEFT JOIN ai_chat_runs r ON r.session_id=s.id AND r.user_version=(SELECT MAX(user_version) FROM ai_chat_runs WHERE session_id=s.id)
 WHERE a.owner_id=? AND a.id=? AND s.kind='document'`
	args := []any{owner, accountID}
	if cursor != "" {
		query += ` AND (s.updated_at<? OR (s.updated_at=? AND s.id>?))`
		args = append(args, after.Updated, after.Updated, after.ID)
	}
	query += ` ORDER BY s.updated_at DESC,s.id LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AIChatSession
		var business string
		var created, updated, lease int64
		var last AIChatRunState
		var retry sql.NullBool
		if err = rows.Scan(&v.ID, &v.Kind, &v.Title, &v.DocumentType, &business, &v.Version, &created, &updated, &last.RequestID, &last.Status, &last.UserMessageID, &lease, &retry); err != nil {
			return out, err
		}
		if len(out.Sessions) == limit {
			raw, _ := json.Marshal(after)
			out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			break
		}
		v.AccountID = accountID
		v.Messages = []json.RawMessage{}
		_ = json.Unmarshal([]byte(business), &v.BusinessContext)
		if v.BusinessContext.Brands == nil {
			v.BusinessContext.Brands = []string{}
		}
		v.CreatedAt = time.UnixMilli(created).UTC().Format(time.RFC3339Nano)
		v.UpdatedAt = time.UnixMilli(updated).UTC().Format(time.RFC3339Nano)
		if last.RequestID != "" {
			if last.Status == "pending" && lease <= time.Now().Unix() {
				last.Status = "failed"
			}
			last.Retryable = retry.Bool && last.Status == "failed"
			v.LastRun = &last
		}
		out.Sessions = append(out.Sessions, v)
		after.Updated = updated
		after.ID = v.ID
	}
	return out, rows.Err()
}

// insertAIChatMessage assigns server IDs and adopts attachments in the same transaction.
func insertAIChatMessage(ctx context.Context, tx *sql.Tx, sessionID, accountID string, payload json.RawMessage, strict bool) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil || m == nil {
		if !strict {
			return "", nil
		} // Preserve malformed legacy data in its source table.
		return "", errors.New("invalid chat message")
	}
	id, err := newID("msg")
	if err != nil {
		return "", err
	}
	m["id"] = id
	if _, ok := m["createdAt"]; !ok {
		m["createdAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_messages(id,session_id,position,payload) VALUES(?,?,COALESCE((SELECT MAX(position)+1 FROM ai_chat_messages WHERE session_id=?),0),?)`, id, sessionID, sessionID, string(raw)); err != nil {
		return "", err
	}
	var refs struct {
		Attachments []AIChatAttachment `json:"attachments"`
	}
	_ = json.Unmarshal(payload, &refs)
	for _, ref := range refs.Attachments {
		res, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ai_chat_attachment_refs(message_id,attachment_id) SELECT ?,i.id FROM ai_chat_attachments i LEFT JOIN ai_chat_attachment_lifecycle l ON l.attachment_id=i.id WHERE i.id=? AND i.account_id=? AND (l.attachment_id IS NULL OR l.expires_at>? OR EXISTS(SELECT 1 FROM ai_chat_attachment_refs r WHERE r.attachment_id=i.id))`, id, ref.ID, accountID, time.Now().Unix())
		if e != nil {
			return "", e
		}
		n, _ := res.RowsAffected()
		if strict && n == 0 { // Duplicate refs are harmless; missing/cross-account refs are not.
			var exists int
			e = tx.QueryRowContext(ctx, `SELECT 1 FROM ai_chat_attachment_refs WHERE message_id=? AND attachment_id=?`, id, ref.ID).Scan(&exists)
			if e != nil {
				return "", ErrAIChatAttachmentUnavailable
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_attachment_lifecycle(attachment_id,created_at,expires_at) SELECT i.id,?,? FROM ai_chat_attachments i WHERE i.id=? AND i.account_id=? ON CONFLICT DO NOTHING`, time.Now().Unix(), time.Now().Add(24*time.Hour).Unix(), ref.ID, accountID); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (s *Store) BeginAIChatTurn(ctx context.Context, owner, accountID, sessionID, requestID, hash string, version int64, user json.RawMessage, business AIBusinessContext, options ...AIChatTurnOptions) (AIChatTurn, error) {
	out := AIChatTurn{SessionID: sessionID, RequestID: requestID}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE ai_chat_sessions SET version=version WHERE id=? AND account_id IN(SELECT id FROM mail_accounts WHERE id=? AND owner_id=?)`, sessionID, accountID, owner)
	if err != nil {
		return out, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return out, ErrNotFound
	}
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM ai_chat_sessions WHERE id=?`, sessionID).Scan(&current); err != nil {
		return out, err
	}
	var savedHash, status, response string
	var lease, userVersion int64
	err = tx.QueryRowContext(ctx, `SELECT request_hash,status,lease_until,user_message_id,user_version,response FROM ai_chat_runs WHERE session_id=? AND request_id=?`, sessionID, requestID).Scan(&savedHash, &status, &lease, &out.UserMessageID, &userVersion, &response)
	retry := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if retry {
		if savedHash != hash {
			return out, ErrAIChatConflict
		}
		if status == "complete" {
			out.Replay = json.RawMessage(response)
			return out, nil
		}
		if status == "pending" && lease > time.Now().Unix() {
			return out, ErrAIChatConflict
		}
		if current != userVersion {
			return out, ErrAIChatConflict
		}
	} else if current != version {
		return out, ErrAIChatConflict
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_chat_runs WHERE session_id=? AND status='pending' AND lease_until>?`, sessionID, time.Now().Unix()).Scan(&active); err != nil {
		return out, err
	}
	if active > 0 {
		return out, ErrAIChatConflict
	}
	out.Token, err = newID("run")
	if err != nil {
		return out, err
	}
	if !retry {
		out.UserMessageID, err = insertAIChatMessage(ctx, tx, sessionID, accountID, user, true)
		if err != nil {
			return out, err
		}
		if len(options) > 0 && len(options[0].CurrentDocument) > 0 {
			if !json.Valid(options[0].CurrentDocument) {
				return out, fmt.Errorf("invalid draft snapshot")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_session_state(session_id,current_document) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET current_document=excluded.current_document`, sessionID, string(options[0].CurrentDocument)); err != nil {
				return out, err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM ai_chat_current_documents WHERE session_id=?`, sessionID); err != nil {
				return out, err
			}
		}

		current++
	}
	out.Version = current
	b, _ := json.Marshal(business)
	// Set the title from the first real user question, including image-only turns.
	var msg struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(user, &msg)
	title := []rune(strings.TrimSpace(msg.Content))
	if len(title) > 32 {
		title = title[:32]
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_chat_sessions SET version=?,business_context=?,updated_at=?,title=CASE WHEN ?=1 AND ?<>'' THEN ? ELSE title END WHERE id=?`, current, string(b), time.Now().UnixMilli(), current, string(title), string(title), sessionID); err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_runs(session_id,request_id,request_hash,token,status,lease_until,user_message_id,user_version) VALUES(?,?,?,?,'pending',?,?,?) ON CONFLICT(session_id,request_id) DO UPDATE SET token=excluded.token,status='pending',lease_until=excluded.lease_until`, sessionID, requestID, hash, out.Token, time.Now().Add(11*time.Minute).Unix(), out.UserMessageID, current)
	if err != nil {
		return out, err
	}
	if !retry && len(options) > 0 && len(options[0].Request) > 0 {
		if !json.Valid(options[0].Request) {
			return out, errors.New("invalid execution input")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_run_inputs(session_id,request_id,payload) VALUES(?,?,?)`, sessionID, requestID, string(options[0].Request)); err != nil {
			return out, err
		}
	}
	return out, tx.Commit()
}

// Retry input is never exposed as a client-controlled replacement for a stored turn.
// Ownership, lease and version are checked again by BeginAIChatTurn after preflight.
func (s *Store) GetAIChatRetryInput(ctx context.Context, owner, accountID, sessionID, requestID string) (json.RawMessage, error) {
	var payload, status string
	var lease, version, current int64
	err := s.db.QueryRowContext(ctx, `SELECT i.payload,r.status,r.lease_until,r.user_version,s.version FROM ai_chat_run_inputs i JOIN ai_chat_runs r USING(session_id,request_id) JOIN ai_chat_sessions s ON s.id=i.session_id JOIN mail_accounts a ON a.id=s.account_id WHERE a.owner_id=? AND a.id=? AND s.id=? AND i.request_id=?`, owner, accountID, sessionID, requestID).Scan(&payload, &status, &lease, &version, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "complete" && (current != version || status == "pending" && lease > time.Now().Unix()) {
		return nil, ErrAIChatConflict
	}
	return json.RawMessage(payload), nil
}

func (s *Store) FinishAIChatTurn(ctx context.Context, turn AIChatTurn, assistant json.RawMessage, response func(string, int64) (json.RawMessage, error)) (json.RawMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE ai_chat_runs SET status='complete' WHERE session_id=? AND request_id=? AND token=? AND status='pending' AND lease_until>?`, turn.SessionID, turn.RequestID, turn.Token, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrAIChatConflict
	}
	var account string
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT account_id,version FROM ai_chat_sessions WHERE id=?`, turn.SessionID).Scan(&account, &version); err != nil {
		return nil, err
	}
	if version != turn.Version {
		return nil, ErrAIChatConflict
	}
	id, err := insertAIChatMessage(ctx, tx, turn.SessionID, account, assistant, true)
	if err != nil {
		return nil, err
	}
	var visible struct {
		Document json.RawMessage `json:"document"`
	}
	if err = json.Unmarshal(assistant, &visible); err != nil {
		return nil, err
	}
	if len(visible.Document) > 0 && string(visible.Document) != "null" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_session_state(session_id,current_document) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET current_document=excluded.current_document`, turn.SessionID, string(visible.Document)); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_current_documents(session_id,message_id) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET message_id=excluded.message_id`, turn.SessionID, id); err != nil {
			return nil, err
		}
	}

	raw, err := response(id, version+1)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_chat_sessions SET version=version+1,updated_at=? WHERE id=?`, time.Now().UnixMilli(), turn.SessionID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_chat_runs SET response=? WHERE session_id=? AND request_id=?`, string(raw), turn.SessionID, turn.RequestID); err != nil {
		return nil, err
	}
	return raw, tx.Commit()
}

func (s *Store) FailAIChatTurn(ctx context.Context, turn AIChatTurn) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ai_chat_runs SET status='failed' WHERE session_id=? AND request_id=? AND token=? AND status='pending'`, turn.SessionID, turn.RequestID, turn.Token)
	return err
}

// CancelAIChatRequest fences late completion and removes retry input for an explicit stop.
func (s *Store) CancelAIChatRequest(ctx context.Context, owner, accountID, sessionID, requestID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE ai_chat_runs SET status='failed' WHERE session_id=? AND request_id=? AND status='pending' AND session_id IN (SELECT s.id FROM ai_chat_sessions s JOIN mail_accounts a ON a.id=s.account_id WHERE a.id=? AND a.owner_id=?)`, sessionID, requestID, accountID, owner)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM ai_chat_run_inputs WHERE session_id=? AND request_id=? AND session_id IN (SELECT r.session_id FROM ai_chat_runs r JOIN ai_chat_sessions s ON s.id=r.session_id JOIN mail_accounts a ON a.id=s.account_id WHERE r.request_id=? AND r.status='failed' AND a.id=? AND a.owner_id=?)`, sessionID, requestID, requestID, accountID, owner)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateAIChatBusiness(ctx context.Context, owner, accountID, id string, version int64, business AIBusinessContext) error {
	raw, _ := json.Marshal(business)
	res, err := s.db.ExecContext(ctx, `UPDATE ai_chat_sessions SET business_context=?,version=version+1,updated_at=? WHERE id=? AND version=? AND account_id IN(SELECT id FROM mail_accounts WHERE id=? AND owner_id=?) AND NOT EXISTS(SELECT 1 FROM ai_chat_runs WHERE session_id=? AND status='pending' AND lease_until>?)`, string(raw), time.Now().UnixMilli(), id, version, accountID, owner, id, time.Now().Unix())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrAIChatConflict
	}
	return nil
}

// Clear/delete release references, retaining bytes for a 24-hour recovery grace.
func (s *Store) DeleteAIChatSession(ctx context.Context, owner, accountID, id string, version int64, clear bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE ai_chat_sessions SET version=version+1,updated_at=? WHERE id=? AND version=? AND account_id IN(SELECT id FROM mail_accounts WHERE id=? AND owner_id=?) AND NOT EXISTS(SELECT 1 FROM ai_chat_runs WHERE session_id=? AND status='pending' AND lease_until>?)`, time.Now().UnixMilli(), id, version, accountID, owner, id, time.Now().Unix())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrAIChatConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_chat_attachment_lifecycle SET expires_at=? WHERE attachment_id IN(SELECT r.attachment_id FROM ai_chat_attachment_refs r JOIN ai_chat_messages m ON m.id=r.message_id WHERE m.session_id=?)`, time.Now().Add(24*time.Hour).Unix(), id); err != nil {
		return err
	}
	var conversation string
	_ = tx.QueryRowContext(ctx, `SELECT conversation_id FROM ai_chat_sessions WHERE id=?`, id).Scan(&conversation)
	if conversation != "" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM conversation_ai_chats WHERE account_id=? AND conversation_id=?`, accountID, conversation); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM conversation_ai_context WHERE account_id=? AND conversation_id=?`, accountID, conversation); err != nil {
			return err
		}
	}
	if clear {
		if _, err = tx.ExecContext(ctx, `DELETE FROM ai_chat_session_state WHERE session_id=?`, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM ai_chat_runs WHERE session_id=?`, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM ai_chat_messages WHERE session_id=?`, id)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM ai_chat_sessions WHERE id=?`, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReplayAIChatTurn(ctx context.Context, owner, accountID, sessionID, requestID, hash string) (json.RawMessage, error) {
	var savedHash, status, response string
	err := s.db.QueryRowContext(ctx, `SELECT r.request_hash,r.status,r.response FROM ai_chat_runs r JOIN ai_chat_sessions s ON s.id=r.session_id JOIN mail_accounts a ON a.id=s.account_id WHERE s.id=? AND a.id=? AND a.owner_id=? AND r.request_id=?`, sessionID, accountID, owner, requestID).Scan(&savedHash, &status, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if savedHash != hash {
		return nil, ErrAIChatConflict
	}
	if status == "complete" {
		return json.RawMessage(response), nil
	}
	return nil, nil
}

// SaveAIChatTurnMemory caches derived memory only while this run owns the lease.
// It can be reused after a failed model request without committing an assistant reply.
func (s *Store) SaveAIChatTurnMemory(ctx context.Context, turn AIChatTurn, memory AIChatContext) error {
	raw, err := json.Marshal(memory)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO ai_chat_session_state(session_id,memory) SELECT s.id,? FROM ai_chat_sessions s JOIN ai_chat_runs r ON r.session_id=s.id WHERE s.id=? AND s.version=? AND r.request_id=? AND r.token=? AND r.status='pending' AND r.lease_until>? ON CONFLICT(session_id) DO UPDATE SET memory=excluded.memory`, string(raw), turn.SessionID, turn.Version, turn.RequestID, turn.Token, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrAIChatConflict
	}
	return nil
}
