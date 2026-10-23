package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

// AIChatDocumentOrigin binds a generated card to its saved library document.
// The message id also supplies a stable document id across refreshes.
type AIChatDocumentOrigin struct {
	SessionID string `json:"sessionId"`
	MessageID string `json:"messageId"`
}

func (s *Store) backfillAIChatCurrentDocuments(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE ai_chat_current_documents SET message_id=message_id WHERE 0`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT state.session_id,m.id,state.current_document,json_extract(m.payload,'$.document') FROM ai_chat_session_state state JOIN ai_chat_messages m ON m.id=(SELECT id FROM ai_chat_messages WHERE session_id=state.session_id AND json_type(payload,'$.document')='object' ORDER BY position DESC LIMIT 1) WHERE NOT EXISTS(SELECT 1 FROM ai_chat_current_documents c WHERE c.session_id=state.session_id)`)
	if err != nil {
		return err
	}
	var matches []AIChatDocumentOrigin
	for rows.Next() {
		var origin AIChatDocumentOrigin
		var current, original string
		if err = rows.Scan(&origin.SessionID, &origin.MessageID, &current, &original); err != nil {
			rows.Close()
			return err
		}
		// The original snapshot and message were marshaled through different Go
		// types. Compare structure, not JSON object key order, and never reattach
		// an explicit fresh-draft reset to its preceding card.
		var a, b any
		if json.Unmarshal([]byte(current), &a) == nil && json.Unmarshal([]byte(original), &b) == nil && reflect.DeepEqual(a, b) {
			matches = append(matches, origin)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, origin := range matches {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ai_chat_current_documents(session_id,message_id) VALUES(?,?)`, origin.SessionID, origin.MessageID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateAIChatDocumentOrigin(ctx context.Context, tx *sql.Tx, owner, id string, origin AIChatDocumentOrigin, updating bool) error {
	var message string
	err := tx.QueryRowContext(ctx, `SELECT m.id FROM ai_chat_messages m JOIN ai_chat_sessions s ON s.id=m.session_id JOIN mail_accounts a ON a.id=s.account_id WHERE m.id=? AND s.id=? AND a.owner_id=? AND json_extract(m.payload,'$.role')='assistant' AND json_type(m.payload,'$.document')='object'`, origin.MessageID, origin.SessionID, owner).Scan(&message)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDocumentConflict
	}
	if err != nil {
		return err
	}
	if id != message {
		return ErrDocumentConflict
	}
	var linked string
	err = tx.QueryRowContext(ctx, `SELECT document_id FROM ai_chat_document_links WHERE message_id=?`, message).Scan(&linked)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if linked != "" && (linked != id || !updating) {
		return ErrDocumentConflict
	}
	return nil
}

// Saving or deleting the current linked document advances the chat version in
// the same transaction. A model reading an older revision cannot commit later.
func invalidateAIChatDocumentReaders(ctx context.Context, tx *sql.Tx, owner string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	marks := make([]string, len(ids))
	args := []any{owner}
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	affected := `SELECT c.session_id FROM ai_chat_current_documents c JOIN ai_chat_document_links l ON l.message_id=c.message_id JOIN ai_chat_sessions s ON s.id=c.session_id JOIN mail_accounts a ON a.id=s.account_id WHERE a.owner_id=? AND l.document_id IN (` + strings.Join(marks, ",") + `)`
	if _, err := tx.ExecContext(ctx, `UPDATE ai_chat_runs SET status='failed' WHERE status='pending' AND session_id IN (`+affected+`)`, args...); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE ai_chat_sessions SET version=version+1,updated_at=? WHERE id IN (`+affected+`)`, append([]any{time.Now().UnixMilli()}, args...)...)
	return err
}
