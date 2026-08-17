package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultMessageSummaryType = "mail_summary"

const messageSummaryColumns = `account_id, folder_name, uid, summary_type, summary_text, status, source_hash, config_hash, model_id, model_name, agent_id, pipeline_version, generation_token, lease_until, error_message, created_at, updated_at`

func scanMessageSummary(scanner interface{ Scan(...any) error }) (MessageSummaryRecord, error) {
	var record MessageSummaryRecord
	var uid, leaseUntil, createdAt, updatedAt int64
	err := scanner.Scan(&record.AccountID, &record.FolderName, &uid, &record.SummaryType, &record.Summary, &record.Status, &record.SourceHash, &record.ConfigHash, &record.ModelID, &record.ModelName, &record.AgentID, &record.PipelineVersion, &record.GenerationToken, &leaseUntil, &record.ErrorMessage, &createdAt, &updatedAt)
	record.UID = fmt.Sprintf("%d", uid)
	record.LeaseUntil = timeFromUnix(leaseUntil)
	record.CreatedAt = timeFromUnix(createdAt)
	record.UpdatedAt = timeFromUnix(updatedAt)
	return record, err
}

func normalizeMessageSummaryKey(key MessageSummaryKey) (MessageSummaryKey, int64, error) {
	key.AccountID = strings.TrimSpace(key.AccountID)
	key.FolderName = strings.TrimSpace(key.FolderName)
	uid, err := parseUIDString(key.UID)
	if err != nil {
		return MessageSummaryKey{}, 0, err
	}
	if key.AccountID == "" || key.FolderName == "" {
		return MessageSummaryKey{}, 0, errors.New("mailstore: message summary account and folder are required")
	}
	key.UID = fmt.Sprintf("%d", uid)
	return key, uid, nil
}

func (s *Store) GetMessageSummary(ctx context.Context, key MessageSummaryKey) (MessageSummaryRecord, error) {
	key, uid, err := normalizeMessageSummaryKey(key)
	if err != nil {
		return MessageSummaryRecord{}, err
	}
	record, err := scanMessageSummary(s.db.QueryRowContext(ctx, `SELECT `+messageSummaryColumns+` FROM message_summaries WHERE account_id = ? AND folder_name = ? AND uid = ? AND summary_type = ?`, key.AccountID, key.FolderName, uid, defaultMessageSummaryType))
	if errors.Is(err, sql.ErrNoRows) {
		return MessageSummaryRecord{}, ErrNotFound
	}
	if err != nil {
		return MessageSummaryRecord{}, fmt.Errorf("mailstore: get message summary: %w", err)
	}
	return record, nil
}

// GetReadyMessageSummaryByMessageID finds a completed summary for a message
// across all folders in one account. The same RFC Message-ID can appear in an
// archive and another mailbox, so a ready result is preferred over its folder.
func (s *Store) GetReadyMessageSummaryByMessageID(ctx context.Context, accountID, messageID string) (MessageSummaryRecord, error) {
	accountID = strings.TrimSpace(accountID)
	messageID = strings.TrimSpace(messageID)
	if accountID == "" || messageID == "" {
		return MessageSummaryRecord{}, ErrNotFound
	}
	record, err := scanMessageSummary(s.db.QueryRowContext(ctx, `
		SELECT `+prefixedMessageSummaryColumns("s")+`
		FROM message_summaries s
		JOIN messages m ON m.account_id = s.account_id AND m.folder_name = s.folder_name AND m.uid = s.uid
		WHERE m.account_id = ? AND m.message_id = ? AND s.summary_type = ? AND s.status = 'ready'
		ORDER BY s.updated_at DESC LIMIT 1`, accountID, messageID, defaultMessageSummaryType))
	if errors.Is(err, sql.ErrNoRows) {
		return MessageSummaryRecord{}, ErrNotFound
	}
	if err != nil {
		return MessageSummaryRecord{}, fmt.Errorf("mailstore: get message summary by Message-ID: %w", err)
	}
	return record, nil
}

func prefixedMessageSummaryColumns(prefix string) string {
	columns := strings.Split(messageSummaryColumns, ", ")
	for i := range columns {
		columns[i] = prefix + "." + columns[i]
	}
	return strings.Join(columns, ", ")
}

func MessageSummaryLookupKey(folderName, uid string) string {
	return folderName + "\x00" + uid
}

func (s *Store) ListMessageSummaries(ctx context.Context, accountID string, keys []MessageSummaryKey) (map[string]MessageSummaryRecord, error) {
	result := make(map[string]MessageSummaryRecord)
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || len(keys) == 0 {
		return result, nil
	}
	clauses := make([]string, 0, len(keys))
	args := make([]any, 0, 2+len(keys)*2)
	args = append(args, accountID, defaultMessageSummaryType)
	for _, key := range keys {
		key.AccountID = accountID
		normalized, uid, err := normalizeMessageSummaryKey(key)
		if err != nil {
			continue
		}
		clauses = append(clauses, `(folder_name = ? AND uid = ?)`)
		args = append(args, normalized.FolderName, uid)
	}
	if len(clauses) == 0 {
		return result, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+messageSummaryColumns+` FROM message_summaries WHERE account_id = ? AND summary_type = ? AND (`+strings.Join(clauses, " OR ")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list message summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanMessageSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("mailstore: scan message summary: %w", err)
		}
		result[MessageSummaryLookupKey(record.FolderName, record.UID)] = record
	}
	return result, rows.Err()
}

// ClaimMessageSummaryGeneration atomically acquires a short generation lease.
// A ready result is immutable unless regenerate is explicitly requested.
func (s *Store) ClaimMessageSummaryGeneration(ctx context.Context, record MessageSummaryRecord, regenerate bool, lease time.Duration) (MessageSummaryRecord, bool, error) {
	key, uid, err := normalizeMessageSummaryKey(record.MessageSummaryKey)
	if err != nil {
		return MessageSummaryRecord{}, false, err
	}
	token, err := newID("summary")
	if err != nil {
		return MessageSummaryRecord{}, false, err
	}
	now := time.Now().Unix()
	leaseUntil := time.Now().Add(lease).Unix()
	readyGuard := "status <> 'ready' AND (status <> 'generating' OR lease_until <= ?)"
	if regenerate {
		readyGuard = "status <> 'generating' OR lease_until <= ?"
	}
	query := `INSERT INTO message_summaries(` + messageSummaryColumns + `) VALUES(?, ?, ?, ?, '', 'generating', ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)
		ON CONFLICT(account_id, folder_name, uid, summary_type) DO UPDATE SET
			status = 'generating', generation_token = excluded.generation_token,
			lease_until = excluded.lease_until, error_message = '', updated_at = excluded.updated_at
		WHERE ` + readyGuard
	result, err := s.db.ExecContext(ctx, query,
		key.AccountID, key.FolderName, uid, defaultMessageSummaryType,
		record.SourceHash, record.ConfigHash, record.ModelID, record.ModelName, record.AgentID,
		record.PipelineVersion, token, leaseUntil, now, now, now,
	)
	if err != nil {
		return MessageSummaryRecord{}, false, fmt.Errorf("mailstore: claim message summary generation: %w", err)
	}
	claimed, _ := result.RowsAffected()
	current, err := s.GetMessageSummary(ctx, key)
	if err != nil {
		return MessageSummaryRecord{}, false, err
	}
	return current, claimed > 0 && current.GenerationToken == token, nil
}

func (s *Store) CompleteMessageSummaryGeneration(ctx context.Context, record MessageSummaryRecord, token, summary string) (MessageSummaryRecord, error) {
	key, uid, err := normalizeMessageSummaryKey(record.MessageSummaryKey)
	if err != nil {
		return MessageSummaryRecord{}, err
	}
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE message_summaries SET summary_text = ?, status = 'ready', source_hash = ?, config_hash = ?, model_id = ?, model_name = ?, agent_id = ?, pipeline_version = ?, generation_token = '', lease_until = 0, error_message = '', updated_at = ? WHERE account_id = ? AND folder_name = ? AND uid = ? AND summary_type = ? AND generation_token = ?`, summary, record.SourceHash, record.ConfigHash, record.ModelID, record.ModelName, record.AgentID, record.PipelineVersion, now, key.AccountID, key.FolderName, uid, defaultMessageSummaryType, token)
	if err != nil {
		return MessageSummaryRecord{}, fmt.Errorf("mailstore: complete message summary generation: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return MessageSummaryRecord{}, errors.New("mailstore: message summary generation lease was lost")
	}
	return s.GetMessageSummary(ctx, key)
}

func (s *Store) FailMessageSummaryGeneration(ctx context.Context, key MessageSummaryKey, token string, generationErr error) error {
	key, uid, err := normalizeMessageSummaryKey(key)
	if err != nil {
		return err
	}
	message := "summary generation failed"
	if generationErr != nil {
		message = generationErr.Error()
	}
	_, err = s.db.ExecContext(ctx, `UPDATE message_summaries SET status = CASE WHEN summary_text <> '' THEN 'ready' ELSE 'failed' END, generation_token = '', lease_until = 0, error_message = ?, updated_at = ? WHERE account_id = ? AND folder_name = ? AND uid = ? AND summary_type = ? AND generation_token = ?`, message, time.Now().Unix(), key.AccountID, key.FolderName, uid, defaultMessageSummaryType, token)
	if err != nil {
		return fmt.Errorf("mailstore: fail message summary generation: %w", err)
	}
	return nil
}
