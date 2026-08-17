package mailstore

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type AttachmentRecord struct {
	AccountID    string
	FolderName   string
	UID          string
	AttachmentID string
	PartID       string
	Filename     string
	ContentType  string
	Size         int
	IsInline     bool
	ContentID    string
	MessageDate  time.Time
	From         string
	FromName     string
	Subject      string
}

type AttachmentListOptions struct {
	Query  string
	Kind   string
	Limit  int
	Offset int
}

func escapeAttachmentLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func attachmentKindClause(kind string) (string, []any) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "images":
		return ` AND lower(a.content_type) LIKE 'image/%'`, nil
	case "pdf":
		return ` AND (lower(a.content_type) = 'application/pdf' OR lower(a.filename) LIKE '%.pdf')`, nil
	case "documents":
		return ` AND (lower(a.content_type) LIKE 'application/msword%' OR lower(a.content_type) LIKE 'application/vnd.openxmlformats-officedocument.wordprocessingml%' OR lower(a.filename) LIKE '%.doc' OR lower(a.filename) LIKE '%.docx' OR lower(a.filename) LIKE '%.txt' OR lower(a.filename) LIKE '%.rtf')`, nil
	case "spreadsheets":
		return ` AND (lower(a.content_type) LIKE 'application/vnd.ms-excel%' OR lower(a.content_type) LIKE 'application/vnd.openxmlformats-officedocument.spreadsheetml%' OR lower(a.filename) LIKE '%.xls' OR lower(a.filename) LIKE '%.xlsx' OR lower(a.filename) LIKE '%.csv')`, nil
	case "archives":
		return ` AND (lower(a.content_type) IN ('application/zip', 'application/x-rar-compressed', 'application/x-7z-compressed', 'application/gzip') OR lower(a.filename) LIKE '%.zip' OR lower(a.filename) LIKE '%.rar' OR lower(a.filename) LIKE '%.7z' OR lower(a.filename) LIKE '%.gz')`, nil
	default:
		return "", nil
	}
}

func (s *Store) ListAttachments(ctx context.Context, accountID string, options AttachmentListOptions) ([]AttachmentRecord, int, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, 0, fmt.Errorf("mailstore: attachment account is required")
	}
	if options.Limit <= 0 || options.Limit > 200 {
		options.Limit = 100
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	// Some clients label embedded CID images as regular attachments. Treat the
	// HTML body as authoritative so stale or BODYSTRUCTURE-only metadata cannot
	// surface signatures and other in-message resources in attachment search.
	where := ` WHERE a.account_id = ? AND a.is_inline = 0
		AND (trim(a.content_id) = '' OR instr(lower(m.html), 'cid:' || lower(trim(trim(a.content_id), '<>'))) = 0)`
	args := []any{accountID}
	if query := strings.ToLower(strings.TrimSpace(options.Query)); query != "" {
		where += ` AND (lower(a.filename) LIKE ? ESCAPE '\' OR lower(m.subject) LIKE ? ESCAPE '\' OR lower(m.from_addr) LIKE ? ESCAPE '\' OR lower(m.from_name) LIKE ? ESCAPE '\')`
		pattern := "%" + escapeAttachmentLike(query) + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	kindClause, kindArgs := attachmentKindClause(options.Kind)
	where += kindClause
	args = append(args, kindArgs...)
	join := ` FROM message_attachments a JOIN messages m ON m.account_id = a.account_id AND m.folder_name = a.folder_name AND m.uid = a.uid`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+join+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("mailstore: count attachments: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.account_id, a.folder_name, a.uid, a.attachment_id, a.part_id, a.filename, a.content_type, a.size_bytes, a.is_inline, a.content_id, m.date_unix, m.from_addr, m.from_name, m.subject`+join+where+` ORDER BY m.date_unix DESC, m.uid DESC, a.filename LIMIT ? OFFSET ?`, append(args, options.Limit, options.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("mailstore: list attachments: %w", err)
	}
	defer rows.Close()
	records := make([]AttachmentRecord, 0)
	for rows.Next() {
		var record AttachmentRecord
		var uid, dateUnix int64
		var inline int
		if err := rows.Scan(&record.AccountID, &record.FolderName, &uid, &record.AttachmentID, &record.PartID, &record.Filename, &record.ContentType, &record.Size, &inline, &record.ContentID, &dateUnix, &record.From, &record.FromName, &record.Subject); err != nil {
			return nil, 0, fmt.Errorf("mailstore: scan attachment: %w", err)
		}
		record.UID = fmt.Sprintf("%d", uid)
		record.IsInline = intBool(inline)
		record.MessageDate = timeFromUnix(dateUnix)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("mailstore: iterate attachments: %w", err)
	}
	return records, total, nil
}
