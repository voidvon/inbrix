package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"inbrix/models"
)

var ErrOutboxBusy = errors.New("mailstore: message is already sending or sent")

type OutgoingMessage struct {
	ReplacesID                                                            string
	ID, AccountID, ClientKey, ConversationID, Status, LastError, DraftUID string
	Message                                                               models.Email
	Recipients                                                            []string
	RawMIME                                                               []byte
	HasPayload                                                            bool
	Attempts                                                              int
}

// Tencent can prepend 16 hex digits and '+' to our original Message-ID when
// storing the Sent copy. Match only this exact transformation for Tencent SMTP
// accounts, preserving the complete original ID and account boundary.
const outgoingMirrorMatch = `m.account_id = o.account_id AND (
 m.message_id = o.message_id OR (
  EXISTS(SELECT 1 FROM mail_accounts a WHERE a.id = o.account_id AND lower(a.smtp_server) IN ('smtp.exmail.qq.com', 'smtp.qq.com'))
  AND substr(o.message_id, 1, 1) = '<'
  AND length(m.message_id) = length(o.message_id) + 17
  AND substr(m.message_id, 1, 1) = '<' AND substr(m.message_id, 18, 1) = '+'
  AND substr(m.message_id, 2, 16) NOT GLOB '*[^0123456789abcdefABCDEF]*'
  AND substr(m.message_id, 19) = substr(o.message_id, 2)
 ))`

// OutgoingMirrorMessageID applies the same provider-scoped rule used by SQL
// deduplication and retry protection to thread metadata lookup.
func OutgoingMirrorMessageID(server, id string) string {
	if !strings.EqualFold(server, "smtp.exmail.qq.com") && !strings.EqualFold(server, "smtp.qq.com") {
		return id
	}
	if len(id) < 20 || id[0] != '<' || id[17] != '+' {
		return id
	}
	for _, c := range id[1:17] {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'F' || c >= 'a' && c <= 'f') {
			return id
		}
	}
	return "<" + id[18:]
}

const outgoingColumns = `id, account_id, client_key, conversation_id, status, last_error, draft_uid, message_json, recipients_json, length(raw_mime) > 0, attempt_count`

func scanOutgoing(scanner interface{ Scan(...any) error }, raw bool) (OutgoingMessage, error) {
	var record OutgoingMessage
	var messageJSON, recipientsJSON string
	values := []any{&record.ID, &record.AccountID, &record.ClientKey, &record.ConversationID, &record.Status, &record.LastError, &record.DraftUID, &messageJSON, &recipientsJSON, &record.HasPayload, &record.Attempts}
	if raw {
		values = append(values, &record.RawMIME)
	}
	if err := scanner.Scan(values...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return record, ErrNotFound
		}
		return record, err
	}
	if err := json.Unmarshal([]byte(messageJSON), &record.Message); err != nil {
		return record, err
	}
	if err := json.Unmarshal([]byte(recipientsJSON), &record.Recipients); err != nil {
		return record, err
	}
	record.Message.ConversationID = record.ConversationID
	record.Message.BodyCached = true
	record.Message.AttachmentMetadataCached = true
	for i := range record.Message.Attachments {
		record.Message.Attachments[i].ID = record.ID + "." + strconv.Itoa(i)
		record.Message.Attachments[i].PartID = strconv.Itoa(i)
	}
	record.Message.ID = record.ID
	record.Message.Folder = "Outbox"
	record.Message.SendStatus = record.Status
	if record.Status == "queued" {
		record.Message.SendStatus = "sending"
	}
	record.Message.SendError = record.LastError
	record.Message.HasSendPayload = record.HasPayload
	return record, nil
}

// CreateOutgoing commits the entire MIME before any SMTP submission. A caller
// key makes a repeated upload/import return the same record, without resending.
func (s *Store) CreateOutgoing(ctx context.Context, record OutgoingMessage) (OutgoingMessage, error) {
	id, err := newID("outbox")
	if err != nil {
		return record, err
	}
	if record.ClientKey == "" {
		record.ClientKey = id
	}
	if record.Status == "" {
		record.Status = "queued"
	}
	if record.RawMIME == nil {
		record.RawMIME = []byte{}
	}
	messageJSON, err := json.Marshal(record.Message)
	if err != nil {
		return record, err
	}
	recipientsJSON, err := json.Marshal(record.Recipients)
	if err != nil {
		return record, err
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return record, err
	}
	defer tx.Rollback()
	inserted, err := tx.ExecContext(ctx, `INSERT INTO outgoing_messages(id, account_id, client_key, conversation_id, message_id, message_json, recipients_json, raw_mime, draft_uid, status, last_error, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(account_id, client_key) DO NOTHING`,
		id, record.AccountID, record.ClientKey, record.ConversationID, record.Message.MessageID, string(messageJSON), string(recipientsJSON), record.RawMIME, record.DraftUID, record.Status, record.LastError, now, now)
	if err != nil {
		return record, fmt.Errorf("mailstore: persist outgoing message: %w", err)
	}
	count, err := inserted.RowsAffected()
	if err != nil {
		return record, err
	}
	if count > 0 && record.ReplacesID != "" {
		result, err := tx.ExecContext(ctx, `UPDATE outgoing_messages SET status = 'superseded', updated_at = ? WHERE account_id = ? AND id = ? AND status IN ('failed', 'unknown')`, now, record.AccountID, record.ReplacesID)
		if err != nil {
			return record, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return record, err
		}
		if count != 1 {
			return record, ErrOutboxBusy
		}
	}
	saved, err := scanOutgoing(tx.QueryRowContext(ctx, `SELECT `+outgoingColumns+` FROM outgoing_messages WHERE account_id = ? AND client_key = ?`, record.AccountID, record.ClientKey), false)
	if err != nil {
		return saved, err
	}
	return saved, tx.Commit()
}

// GetOutgoing scopes every read to an account already authorized by the caller.
func (s *Store) GetOutgoing(ctx context.Context, accountID, id string) (OutgoingMessage, error) {
	return scanOutgoing(s.db.QueryRowContext(ctx, `SELECT `+outgoingColumns+`, raw_mime FROM outgoing_messages WHERE account_id = ? AND id = ?`, accountID, id), true)
}

func (s *Store) ListOutgoing(ctx context.Context, accountID string) ([]OutgoingMessage, error) {
	// The IMAP copy replaces the outbox bubble by exact Message-ID. The audit
	// record is retained; successful mail never appears twice in the transcript.
	rows, err := s.db.QueryContext(ctx, `SELECT `+outgoingColumns+` FROM outgoing_messages o WHERE account_id = ?
		AND status != 'superseded' AND NOT EXISTS(SELECT 1 FROM messages m WHERE `+outgoingMirrorMatch+`)
		ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []OutgoingMessage
	for rows.Next() {
		record, err := scanOutgoing(rows, false)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// ClaimOutgoing is atomic across workers. Only queued records are automatically
// submitted; failures and uncertain results require an explicit user retry.
func (s *Store) ClaimOutgoing(ctx context.Context) (OutgoingMessage, error) {
	return scanOutgoing(s.db.QueryRowContext(ctx, `UPDATE outgoing_messages SET status = 'sending', last_error = '', attempt_count = attempt_count + 1, updated_at = ?
		WHERE id = (SELECT id FROM outgoing_messages WHERE status = 'queued' ORDER BY created_at LIMIT 1) AND status = 'queued'
		RETURNING `+outgoingColumns+`, raw_mime`, time.Now().UnixMilli()), true)
}

func (s *Store) FinishOutgoing(ctx context.Context, accountID, id, status, lastError string) error {
	if status != "failed" && status != "unknown" && status != "sent" {
		return fmt.Errorf("mailstore: invalid send result")
	}
	record, err := s.GetOutgoing(ctx, accountID, id)
	if err != nil {
		return err
	}
	if status == "sent" {
		record.Message.Date = time.Now().UTC()
	}
	payload, err := json.Marshal(record.Message)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE outgoing_messages SET status = ?, last_error = ?, message_json = ?, updated_at = ? WHERE account_id = ? AND id = ? AND status = 'sending'`, status, lastError, string(payload), time.Now().UnixMilli(), accountID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrOutboxBusy
	}
	return err
}

func (s *Store) RetryOutgoing(ctx context.Context, accountID, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE outgoing_messages AS o SET status = 'queued', last_error = '', updated_at = ?
		WHERE account_id = ? AND id = ? AND status IN ('failed', 'unknown') AND length(raw_mime) > 0
		AND NOT EXISTS(SELECT 1 FROM messages m WHERE `+outgoingMirrorMatch+`)`, time.Now().UnixMilli(), accountID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrOutboxBusy
	}
	return nil
}

func (s *Store) DeleteOutgoing(ctx context.Context, accountID, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM outgoing_messages WHERE account_id = ? AND id = ? AND status != 'sending'`, accountID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrOutboxBusy
	}
	return nil
}

func (s *Store) RecoverOutgoing(ctx context.Context) error {
	// A crash during SMTP DATA can lose the acceptance response. Never silently
	// re-fire an in-flight message on restart and risk delivering it twice.
	_, err := s.db.ExecContext(ctx, `UPDATE outgoing_messages SET status = 'unknown', last_error = 'Server restarted before the send result was recorded', updated_at = ? WHERE status = 'sending'`, time.Now().UnixMilli())
	return err
}

// OutgoingThreadMetadata keeps the server thread and acceptance time stable
// when the sent message is replaced by its IMAP copy.
func (s *Store) OutgoingThreadMetadata(ctx context.Context, accountID string) (map[string]models.Email, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+outgoingColumns+` FROM outgoing_messages WHERE account_id = ? AND status != 'superseded'`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]models.Email)
	for rows.Next() {
		record, err := scanOutgoing(rows, false)
		if err != nil {
			return nil, err
		}
		result[record.Message.MessageID] = record.Message
	}
	return result, rows.Err()
}
