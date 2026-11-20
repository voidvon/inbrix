package mailstore

import (
	"context"
	"time"

	"inbrix/models"
)

// ListSentMessages merges the local send history and the synchronized Sent
// folder. Select the page before reading previews or decoding attachment JSON.
func (s *Store) ListSentMessages(ctx context.Context, accountID, folder string, limit, offset int) ([]models.Email, error) {
	rows, err := s.db.QueryContext(ctx, `WITH mirrored AS (
		SELECT CAST(m.uid AS TEXT) AS id, m.date_unix * 1000 AS date_ms, m.uid AS sort_uid, 0 AS local
		FROM messages m WHERE m.account_id = ?1 AND m.folder_name = ?2
		ORDER BY m.date_unix DESC, m.uid DESC LIMIT (?3 + ?4)
	), sent AS (
		SELECT o.id, o.updated_at AS date_ms, 0 AS sort_uid, 1 AS local FROM outgoing_messages o
		WHERE o.account_id = ?1 AND o.status = 'sent'
			AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.folder_name = ?2 AND `+outgoingMirrorMatch+`)
		ORDER BY o.updated_at DESC, o.id DESC LIMIT (?3 + ?4)
	), page AS (
		SELECT id, date_ms, sort_uid, local FROM mirrored
		UNION ALL
		SELECT id, date_ms, sort_uid, local FROM sent
		ORDER BY date_ms DESC, sort_uid DESC, id DESC LIMIT ?3 OFFSET ?4
	)
	SELECT p.id, p.date_ms, COALESCE(m.from_addr, json_extract(o.message_json, '$.from'), ''),
		COALESCE(m.from_name, json_extract(o.message_json, '$.fromName'), ''),
		COALESCE(m.to_addrs, json_extract(o.message_json, '$.to'), ''),
		COALESCE(m.subject, json_extract(o.message_json, '$.subject'), ''),
		COALESCE(m.preview, json_extract(o.message_json, '$.preview'), ''),
		COALESCE(m.flags_json, json_extract(o.message_json, '$.flags'), '[]'),
		COALESCE(m.attachments_json, json_extract(o.message_json, '$.attachments'), '[]'),
		COALESCE(m.has_attachments, 0)
	FROM page p
	LEFT JOIN messages m ON p.local = 0 AND m.account_id = ?1 AND m.folder_name = ?2 AND m.uid = p.sort_uid
	LEFT JOIN outgoing_messages o ON p.local = 1 AND o.id = p.id
	ORDER BY p.date_ms DESC, p.sort_uid DESC, p.id DESC`, accountID, folder, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []models.Email
	for rows.Next() {
		email := models.Email{Folder: folder}
		var dateMS int64
		var flags, attachments string
		var hasAttachments int
		if err := rows.Scan(&email.ID, &dateMS, &email.From, &email.FromName, &email.To, &email.Subject, &email.Preview, &flags, &attachments, &hasAttachments); err != nil {
			return nil, err
		}
		email.Date = time.UnixMilli(dateMS).UTC()
		unmarshalJSON(flags, "[]", &email.Flags)
		unmarshalJSON(attachments, "[]", &email.Attachments)
		email.HasAttachments = intBool(hasAttachments) || hasRegularAttachment(email.Attachments)
		messages = append(messages, email)
	}
	return messages, rows.Err()
}
