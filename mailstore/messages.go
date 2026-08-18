package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"inbrix/handlers/api"
	"inbrix/models"
	"strings"
	"time"
)

const messageColumns = `account_id, folder_name, uid, date_unix, from_addr, from_name, to_addrs, to_names_json, cc, subject, preview, body, html, flags_json, attachments_json, message_id, in_reply_to, references_json, has_attachments, body_cached, attachment_metadata_cached, auth_json, unsubscribe_json, invite_json, brand_json`

// UpsertMessages stores list metadata and preserves a previously cached full
// body when the incoming record only came from the lightweight IMAP list fetch.
func (s *Store) UpsertMessages(ctx context.Context, accountID, folderName string, emails []models.Email) error {
	if accountID == "" || folderName == "" {
		return fmt.Errorf("mailstore: message account and folder are required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mailstore: begin message upsert: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	query := `
		INSERT INTO messages(` + messageColumns + `, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, folder_name, uid) DO UPDATE SET
			date_unix=excluded.date_unix, from_addr=excluded.from_addr, from_name=excluded.from_name,
			to_addrs=excluded.to_addrs, to_names_json=excluded.to_names_json, cc=excluded.cc,
			subject=excluded.subject, preview=excluded.preview, flags_json=excluded.flags_json,
			-- The lightweight IMAP list fetch does not include BODYSTRUCTURE, so
			-- it necessarily writes an empty attachment list and has_attachments=0.
			-- Do not let that metadata refresh erase attachment information learned
			-- by the full MIME fetch. A full MIME fetch is authoritative for both
			-- fields and can also intentionally clear stale metadata.
			attachments_json=CASE WHEN excluded.attachment_metadata_cached = 1 THEN excluded.attachments_json ELSE messages.attachments_json END,
			has_attachments=CASE WHEN excluded.attachment_metadata_cached = 1 THEN excluded.has_attachments ELSE messages.has_attachments END, message_id=excluded.message_id,
			in_reply_to=excluded.in_reply_to, references_json=excluded.references_json,
			auth_json=CASE WHEN excluded.auth_json <> '' THEN excluded.auth_json ELSE messages.auth_json END,
			unsubscribe_json=CASE WHEN excluded.unsubscribe_json <> '' THEN excluded.unsubscribe_json ELSE messages.unsubscribe_json END,
			invite_json=CASE WHEN excluded.invite_json <> '' THEN excluded.invite_json ELSE messages.invite_json END,
			brand_json=CASE WHEN excluded.brand_json <> '' THEN excluded.brand_json ELSE messages.brand_json END,
			body=CASE WHEN excluded.body_cached = 1 THEN excluded.body ELSE messages.body END,
			html=CASE WHEN excluded.body_cached = 1 THEN excluded.html ELSE messages.html END,
			body_cached=MAX(messages.body_cached, excluded.body_cached),
			attachment_metadata_cached=MAX(messages.attachment_metadata_cached, excluded.attachment_metadata_cached), updated_at=excluded.updated_at`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("mailstore: prepare message upsert: %w", err)
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, email := range emails {
		uid, err := parseUIDString(email.ID)
		if err != nil {
			continue
		}
		email.Attachments = api.MarkInlineAttachmentsFromHTML(email.HTML, email.Attachments)
		bodyCached := email.BodyCached || email.Body != "" || email.HTML != ""
		attachmentMetadataCached := email.AttachmentMetadataCached || len(email.Attachments) > 0
		hasAttachments := email.HasAttachments || hasRegularAttachment(email.Attachments)
		_, err = stmt.ExecContext(ctx,
			accountID, folderName, uid, unixOrZero(email.Date), email.From, email.FromName,
			email.To, marshalJSON(email.ToNames, "[]"), email.Cc, email.Subject, email.Preview,
			email.Body, email.HTML, marshalJSON(email.Flags, "[]"), marshalJSON(email.Attachments, "[]"),
			email.MessageID, email.InReplyTo, marshalJSON(email.References, "[]"), boolInt(hasAttachments),
			boolInt(bodyCached), boolInt(attachmentMetadataCached), optionalJSON(email.Auth), optionalJSON(email.Unsubscribe), optionalJSON(email.Invite),
			optionalJSON(email.Brand), now,
		)
		if err != nil {
			return fmt.Errorf("mailstore: upsert message %s/%s/%s: %w", accountID, folderName, email.ID, err)
		}
		if attachmentMetadataCached {
			if err := replaceMessageAttachments(ctx, tx, accountID, folderName, uid, email.Attachments); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mailstore: commit message upsert: %w", err)
	}
	return nil
}

// UpdateAttachmentMetadata updates only MIME attachment metadata. This is
// intentionally separate from UpsertMessages because a BODYSTRUCTURE-only
// IMAP fetch does not contain the message headers or body and must not erase
// either from the local mirror.
func (s *Store) UpdateAttachmentMetadata(ctx context.Context, accountID, folderName, uid string, attachments []models.Attachment) error {
	if accountID == "" || folderName == "" {
		return fmt.Errorf("mailstore: message account and folder are required")
	}
	n, err := parseUIDString(uid)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mailstore: begin attachment metadata update: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	var htmlBody string
	if err := tx.QueryRowContext(ctx, `SELECT html FROM messages WHERE account_id = ? AND folder_name = ? AND uid = ?`, accountID, folderName, n).Scan(&htmlBody); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("mailstore: read message HTML for attachment metadata %s/%s/%s: %w", accountID, folderName, uid, err)
	}
	attachments = api.MarkInlineAttachmentsFromHTML(htmlBody, attachments)
	_, err = tx.ExecContext(ctx, `
		UPDATE messages
		SET attachments_json = ?, has_attachments = ?, attachment_metadata_cached = 1, updated_at = ?
		WHERE account_id = ? AND folder_name = ? AND uid = ?`,
		marshalJSON(attachments, "[]"), boolInt(hasRegularAttachment(attachments)), time.Now().Unix(), accountID, folderName, n)
	if err != nil {
		return fmt.Errorf("mailstore: update attachment metadata %s/%s/%s: %w", accountID, folderName, uid, err)
	}
	if err := replaceMessageAttachments(ctx, tx, accountID, folderName, n, attachments); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mailstore: commit attachment metadata update: %w", err)
	}
	return nil
}

type messageAttachmentExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func replaceMessageAttachments(ctx context.Context, execer messageAttachmentExecer, accountID, folderName string, uid int64, attachments []models.Attachment) error {
	if _, err := execer.ExecContext(ctx, `DELETE FROM message_attachments WHERE account_id = ? AND folder_name = ? AND uid = ?`, accountID, folderName, uid); err != nil {
		return fmt.Errorf("mailstore: clear attachment index %s/%s/%d: %w", accountID, folderName, uid, err)
	}
	for index, attachment := range attachments {
		key := strings.TrimSpace(attachment.PartID)
		if key == "" {
			key = strings.TrimSpace(attachment.ID)
		}
		if key == "" {
			key = fmt.Sprintf("index-%d", index)
		}
		if _, err := execer.ExecContext(ctx, `INSERT INTO message_attachments(account_id, folder_name, uid, attachment_key, attachment_id, part_id, filename, content_type, size_bytes, is_inline, content_id) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			accountID, folderName, uid, key, attachment.ID, attachment.PartID, attachment.Filename, attachment.ContentType, attachment.Size, boolInt(attachment.IsInline), attachment.ContentID); err != nil {
			return fmt.Errorf("mailstore: index attachment %s/%s/%d/%s: %w", accountID, folderName, uid, key, err)
		}
	}
	return nil
}

func optionalJSON(v any) string {
	if v == nil {
		return ""
	}
	return marshalJSON(v, "")
}

func decodeMessage(
	accountID, folderName string,
	uid int64,
	dateUnix int64,
	fromAddr, fromName, toAddrs, toNamesJSON, cc, subject, preview, body, html, flagsJSON, attachmentsJSON,
	messageID, inReplyTo, referencesJSON string,
	hasAttachments, bodyCached, attachmentMetadataCached int,
	authJSON, unsubscribeJSON, inviteJSON, brandJSON string,
) models.Email {
	email := models.Email{
		ID:                       fmt.Sprintf("%d", uid),
		Folder:                   folderName,
		From:                     fromAddr,
		FromName:                 api.DecodeMIMEHeader(fromName),
		To:                       toAddrs,
		Cc:                       cc,
		Subject:                  subject,
		Preview:                  preview,
		Body:                     body,
		HTML:                     html,
		BodyCached:               intBool(bodyCached),
		AttachmentMetadataCached: intBool(attachmentMetadataCached),
		Date:                     timeFromUnix(dateUnix),
		HasAttachments:           intBool(hasAttachments),
		MessageID:                messageID,
		InReplyTo:                inReplyTo,
	}
	unmarshalJSON(toNamesJSON, "[]", &email.ToNames)
	for i := range email.ToNames {
		email.ToNames[i] = api.DecodeMIMEHeader(email.ToNames[i])
	}
	unmarshalJSON(flagsJSON, "[]", &email.Flags)
	unmarshalJSON(attachmentsJSON, "[]", &email.Attachments)
	unmarshalJSON(referencesJSON, "[]", &email.References)
	// Older mirror rows may have retained attachments_json while a later
	// lightweight refresh reset has_attachments. Keep the model self-consistent
	// when those rows are read, even before they are rewritten by a new sync.
	email.HasAttachments = email.HasAttachments || hasRegularAttachment(email.Attachments)
	if authJSON != "" {
		var v models.AuthResults
		if err := unmarshalOptional(authJSON, &v); err == nil {
			email.Auth = &v
		}
	}
	if unsubscribeJSON != "" {
		var v models.Unsubscribe
		if err := unmarshalOptional(unsubscribeJSON, &v); err == nil {
			email.Unsubscribe = &v
		}
	}
	if inviteJSON != "" {
		var v models.CalendarInvite
		if err := unmarshalOptional(inviteJSON, &v); err == nil {
			email.Invite = &v
		}
	}
	if brandJSON != "" {
		var v models.BrandIndicator
		if err := unmarshalOptional(brandJSON, &v); err == nil {
			email.Brand = &v
		}
	}
	return email
}

func hasRegularAttachment(attachments []models.Attachment) bool {
	for _, attachment := range attachments {
		if !attachment.IsInline {
			return true
		}
	}
	return false
}

func unmarshalOptional(raw string, dst any) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("empty JSON")
	}
	return jsonUnmarshal([]byte(raw), dst)
}

// jsonUnmarshal is a small indirection that keeps the hot row conversion free
// of a second JSON implementation while making malformed legacy rows harmless.
func jsonUnmarshal(raw []byte, dst any) error {
	return json.Unmarshal(raw, dst)
}

func scanMessage(scanner interface{ Scan(...any) error }) (models.Email, error) {
	var accountID, folderName string
	var uid, dateUnix int64
	var fromAddr, fromName, toAddrs, toNamesJSON, cc, subject, preview, body, html, flagsJSON, attachmentsJSON string
	var messageID, inReplyTo, referencesJSON string
	var hasAttachments, bodyCached, attachmentMetadataCached int
	var authJSON, unsubscribeJSON, inviteJSON, brandJSON string
	err := scanner.Scan(&accountID, &folderName, &uid, &dateUnix, &fromAddr, &fromName, &toAddrs, &toNamesJSON, &cc, &subject, &preview, &body, &html, &flagsJSON, &attachmentsJSON, &messageID, &inReplyTo, &referencesJSON, &hasAttachments, &bodyCached, &attachmentMetadataCached, &authJSON, &unsubscribeJSON, &inviteJSON, &brandJSON)
	if err != nil {
		return models.Email{}, err
	}
	return decodeMessage(accountID, folderName, uid, dateUnix, fromAddr, fromName, toAddrs, toNamesJSON, cc, subject, preview, body, html, flagsJSON, attachmentsJSON, messageID, inReplyTo, referencesJSON, hasAttachments, bodyCached, attachmentMetadataCached, authJSON, unsubscribeJSON, inviteJSON, brandJSON), nil
}

func (s *Store) ListMessages(ctx context.Context, accountID, folderName string, limit, offset int) ([]models.Email, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE account_id = ? AND folder_name = ? ORDER BY date_unix DESC, uid DESC LIMIT ? OFFSET ?`, accountID, folderName, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list messages: %w", err)
	}
	defer rows.Close()
	var out []models.Email
	for rows.Next() {
		email, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("mailstore: scan message: %w", err)
		}
		out = append(out, email)
	}
	return out, rows.Err()
}

// ListMessagesForFolders returns the complete local message mirror for a set
// of mailboxes. It is used by conversation mode, where INBOX and the Sent
// mailbox must be threaded together before the page is rendered.
func (s *Store) ListMessagesForFolders(ctx context.Context, accountID string, folderNames []string) ([]models.Email, error) {
	if strings.TrimSpace(accountID) == "" || len(folderNames) == 0 {
		return nil, nil
	}

	unique := make([]string, 0, len(folderNames))
	seen := make(map[string]struct{}, len(folderNames))
	for _, name := range folderNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}
	if len(unique) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(unique))
	args := make([]any, 0, len(unique)+1)
	args = append(args, accountID)
	for i, name := range unique {
		placeholders[i] = "?"
		args = append(args, name)
	}
	query := `SELECT ` + messageColumns + ` FROM messages WHERE account_id = ? AND folder_name IN (` + strings.Join(placeholders, ",") + `) ORDER BY date_unix ASC, uid ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list messages for folders: %w", err)
	}
	defer rows.Close()

	out := make([]models.Email, 0)
	for rows.Next() {
		email, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("mailstore: scan conversation message: %w", err)
		}
		out = append(out, email)
	}
	return out, rows.Err()
}

// MaxMessageUID returns the highest locally observed UID for a folder. IMAP
// UIDs are monotonically increasing within a mailbox, so this is the cursor
// used by the background worker for incremental discovery.
func (s *Store) MaxMessageUID(ctx context.Context, accountID, folderName string) (uint32, error) {
	var max sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(uid) FROM messages WHERE account_id = ? AND folder_name = ?`, accountID, folderName).Scan(&max); err != nil {
		return 0, fmt.Errorf("mailstore: max message UID: %w", err)
	}
	if !max.Valid || max.Int64 <= 0 {
		return 0, nil
	}
	if max.Int64 > int64(^uint32(0)) {
		return 0, fmt.Errorf("mailstore: message UID exceeds uint32: %d", max.Int64)
	}
	return uint32(max.Int64), nil
}

// ListMessageUIDsMissingAttachmentMetadata returns locally mirrored messages
// whose MIME structure has not been inspected yet. The lightweight header
// sync deliberately leaves these rows pending, so the background worker can
// repair old mailboxes as well as newly discovered messages.
func (s *Store) ListMessageUIDsMissingAttachmentMetadata(ctx context.Context, accountID, folderName string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uid FROM messages WHERE account_id = ? AND folder_name = ? AND attachment_metadata_cached = 0 ORDER BY uid ASC`, accountID, folderName)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list messages missing attachment metadata: %w", err)
	}
	defer rows.Close()
	var uids []string
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("mailstore: scan message UID missing attachment metadata: %w", err)
		}
		uids = append(uids, fmt.Sprintf("%d", uid))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mailstore: list messages missing attachment metadata: %w", err)
	}
	return uids, nil
}

// ResetAttachmentMetadata marks all mirrored messages for an account as
// pending. It is used by the explicit repair action so a user can request a
// fresh MIME-structure scan even when the previous attempt was interrupted.
func (s *Store) ResetAttachmentMetadata(ctx context.Context, accountID string) error {
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("mailstore: account is required to reset attachment metadata")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET attachment_metadata_cached = 0, updated_at = ? WHERE account_id = ?`, time.Now().Unix(), accountID)
	if err != nil {
		return fmt.Errorf("mailstore: reset attachment metadata: %w", err)
	}
	return nil
}

// UpdateFolderStats derives counts from the local mirror after a sync. The
// JSON flags are intentionally queried with instr rather than a LIKE pattern,
// because the IMAP backslash in \\Seen is easy to mis-escape in SQL literals.
func (s *Store) UpdateFolderStats(ctx context.Context, accountID, folderName string) error {
	var messageCount, unreadCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN instr(flags_json, ?) = 0 THEN 1 ELSE 0 END), 0)
		FROM messages WHERE account_id = ? AND folder_name = ?`, `\Seen`, accountID, folderName).Scan(&messageCount, &unreadCount)
	if err != nil {
		return fmt.Errorf("mailstore: folder stats: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE folders SET message_count = ?, unread_count = ? WHERE account_id = ? AND name = ?`, messageCount, unreadCount, accountID, folderName)
	return err
}

func (s *Store) GetMessage(ctx context.Context, accountID, folderName, uid string) (models.Email, error) {
	n, err := parseUIDString(uid)
	if err != nil {
		return models.Email{}, err
	}
	email, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE account_id = ? AND folder_name = ? AND uid = ?`, accountID, folderName, n))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Email{}, ErrNotFound
	}
	if err != nil {
		return models.Email{}, fmt.Errorf("mailstore: get message: %w", err)
	}
	return email, nil
}

func (s *Store) UpdateFlags(ctx context.Context, accountID, folderName, uid string, flags []string) error {
	n, err := parseUIDString(uid)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE messages SET flags_json = ?, updated_at = ? WHERE account_id = ? AND folder_name = ? AND uid = ?`, marshalJSON(flags, "[]"), time.Now().Unix(), accountID, folderName, n)
	return err
}

func (s *Store) DeleteMessage(ctx context.Context, accountID, folderName, uid string) error {
	n, err := parseUIDString(uid)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM messages WHERE account_id = ? AND folder_name = ? AND uid = ?`, accountID, folderName, n)
	return err
}

// PruneFolder removes records not observed during a complete IMAP scan. It is
// only called after the worker has reached the end of a folder, never when a
// configured per-folder cap truncated the scan.
func (s *Store) PruneFolder(ctx context.Context, accountID, folderName string, seen []uint32) error {
	seenSet := make(map[int64]struct{}, len(seen))
	for _, uid := range seen {
		seenSet[int64(uid)] = struct{}{}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT uid FROM messages WHERE account_id = ? AND folder_name = ?`, accountID, folderName)
	if err != nil {
		return err
	}
	var stale []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return err
		}
		if _, ok := seenSet[uid]; !ok {
			stale = append(stale, uid)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(stale) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM messages WHERE account_id = ? AND folder_name = ? AND uid = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, uid := range stale {
		if _, err := stmt.ExecContext(ctx, accountID, folderName, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
