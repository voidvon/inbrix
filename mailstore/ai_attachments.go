package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"
)

// AIChatAttachment stores image bytes separately from lightweight chat history.
// Every read is scoped to both the authenticated owner and the selected mailbox.
type AIChatAttachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Data        []byte `json:"-"`
}

const MaxAIImageBytes = 10 << 20

func (s *Store) CreateAIChatAttachment(ctx context.Context, owner, accountID, filename, contentType string, data []byte) (AIChatAttachment, error) {
	out := AIChatAttachment{Filename: filename, ContentType: contentType, Size: len(data)}
	if len(data) == 0 || len(data) > MaxAIImageBytes {
		return out, errors.New("图片大小必须在 1 字节到 10 MB 之间")
	}
	if err := s.GCAIChatAttachments(ctx); err != nil {
		return out, err
	}
	id, err := newID("aimg")
	if err != nil {
		return out, err
	}
	// A single atomic statement applies ownership and a 256 MiB mailbox quota,
	// including concurrent uploads. Images stay available across chat reloads.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO ai_chat_attachments(id,account_id,filename,content_type,size,data)
 SELECT ?,id,?,?,?,? FROM mail_accounts WHERE id=? AND owner_id=?
 AND COALESCE((SELECT SUM(size) FROM ai_chat_attachments WHERE account_id=?),0)+? <= 268435456`,
		id, filename, contentType, len(data), data, accountID, owner, accountID, len(data))
	if err != nil {
		return out, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return out, err
	}
	if n == 0 {
		return out, errors.New("邮箱不存在或 AI 图片附件已达到 256 MB 存储上限，请移除不需要的图片")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_attachment_lifecycle(attachment_id,created_at,expires_at) VALUES(?,?,?)`, id, time.Now().Unix(), time.Now().Add(24*time.Hour).Unix()); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out.ID = id
	return out, nil
}

func (s *Store) GetAIChatAttachment(ctx context.Context, owner, accountID, id string) (AIChatAttachment, error) {
	var out AIChatAttachment
	err := s.db.QueryRowContext(ctx, `SELECT i.id,i.filename,i.content_type,i.size,i.data FROM ai_chat_attachments i
 JOIN mail_accounts a ON a.id=i.account_id WHERE i.id=? AND a.id=? AND a.owner_id=?`, id, accountID, owner).
		Scan(&out.ID, &out.Filename, &out.ContentType, &out.Size, &out.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s *Store) DeleteAIChatAttachment(ctx context.Context, owner, accountID, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ai_chat_attachments WHERE id=? AND account_id IN (SELECT id FROM mail_accounts WHERE id=? AND owner_id=?) AND NOT EXISTS(SELECT 1 FROM ai_chat_attachment_refs WHERE attachment_id=ai_chat_attachments.id) AND NOT EXISTS(SELECT 1 FROM conversation_ai_chats c,json_each(CASE WHEN json_valid(c.messages) THEN c.messages ELSE '[]' END) m,json_each(m.value,'$.attachments') a WHERE c.account_id=ai_chat_attachments.account_id AND json_extract(a.value,'$.id')=ai_chat_attachments.id)`, id, accountID, owner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		if _, e := s.GetAIChatAttachment(ctx, owner, accountID, id); e == nil {
			return ErrAIChatAttachmentInUse
		}
		return ErrNotFound
	}
	return err
}

func (s *Store) GCAIChatAttachments(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ai_chat_attachments WHERE id IN(SELECT attachment_id FROM ai_chat_attachment_lifecycle WHERE expires_at<=? AND NOT EXISTS(SELECT 1 FROM ai_chat_attachment_refs WHERE attachment_id=ai_chat_attachment_lifecycle.attachment_id) ORDER BY expires_at LIMIT 100)`, time.Now().Unix())
	return err
}

// StartAIChatAttachmentGC stops and joins its worker before Store.Close.
func (s *Store) StartAIChatAttachmentGC() func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleanup, stop := context.WithTimeout(ctx, 30*time.Second)
				if err := s.GCAIChatAttachments(cleanup); err != nil && ctx.Err() == nil {
					log.Printf("AI attachment cleanup: %v", err)
				}
				stop()
			}
		}
	}()
	return func() { cancel(); <-done }
}
