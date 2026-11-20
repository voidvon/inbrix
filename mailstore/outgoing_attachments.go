package mailstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"inbrix/models"
)

func (s *Store) outgoingAttachmentDir(id string) (string, error) {
	if !strings.HasPrefix(id, "outbox_") || !filepath.IsLocal(id) || filepath.Base(id) != id {
		return "", fmt.Errorf("mailstore: invalid outgoing message ID")
	}
	if s.path == ":memory:" {
		return "", fmt.Errorf("mailstore: durable attachment storage requires a database file")
	}
	return filepath.Join(s.path+".attachments", id), nil
}

// ArchiveOutgoingAttachments publishes the files before releasing the MIME.
// Holding the SQLite writer prevents another process from deleting the message
// while files are written. A restart can repeat the same writes safely.
func (s *Store) ArchiveOutgoingAttachments(ctx context.Context, accountID, id string, files []models.Attachment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	record, err := scanOutgoing(tx.QueryRowContext(ctx, `SELECT `+outgoingColumns+`
		FROM outgoing_messages WHERE account_id = ? AND id = ? AND status = 'sent'`, accountID, id), false)
	if err != nil {
		return err
	}
	if !record.HasPayload {
		return nil
	}
	if len(record.Message.Attachments) != len(files) {
		return fmt.Errorf("mailstore: sent attachment count differs from MIME")
	}
	if len(files) > 0 {
		dir, err := s.outgoingAttachmentDir(id)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		for index, file := range files {
			metadata := record.Message.Attachments[index]
			if file.Filename != metadata.Filename || file.ContentID != metadata.ContentID || len(file.Content) != metadata.Size {
				return fmt.Errorf("mailstore: sent attachment %d differs from MIME metadata", index)
			}
			if err := writeOutgoingAttachment(filepath.Join(dir, strconv.Itoa(index)), file.Content); err != nil {
				return err
			}
		}
		root := filepath.Dir(dir)
		for _, path := range []string{dir, root, filepath.Dir(root)} {
			entry, err := os.Open(path)
			if err != nil {
				return err
			}
			err = entry.Sync()
			entry.Close()
			if err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE outgoing_messages SET raw_mime = X'' WHERE account_id = ? AND id = ?`, accountID, id); err != nil {
		return err
	}
	return tx.Commit()
}

func writeOutgoingAttachment(path string, data []byte) error {
	// Downloads switch to this directory only after every part is written and
	// the transaction commits. An interrupted write is replaced by the retry.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func (s *Store) OpenOutgoingAttachment(record OutgoingMessage, index int) (*os.File, error) {
	if record.Status != "sent" || record.HasPayload || index < 0 || index >= len(record.Message.Attachments) {
		return nil, ErrNotFound
	}
	dir, err := s.outgoingAttachmentDir(record.ID)
	if err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(dir, strconv.Itoa(index)))
}

type SentMIMEKey struct {
	AccountID string
	ID        string
}

func (s *Store) ListSentMIME(ctx context.Context, after string, limit int) ([]SentMIMEKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account_id, id FROM outgoing_messages
		WHERE status = 'sent' AND length(raw_mime) > 0 AND id > ? ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []SentMIMEKey
	for rows.Next() {
		var key SentMIMEKey
		if err := rows.Scan(&key.AccountID, &key.ID); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) CollectOutgoingAttachments(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM outgoing_attachment_gc ORDER BY id LIMIT 100`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if s.path != ":memory:" {
			dir, err := s.outgoingAttachmentDir(id)
			if err != nil {
				return err
			}
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM outgoing_attachment_gc WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}
