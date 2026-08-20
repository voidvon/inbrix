package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const replySuggestionSummaryType = "reply_suggestion"

func (s *Store) GetMessageReplySuggestion(ctx context.Context, key MessageSummaryKey) (MessageSummaryRecord, error) {
	key, uid, err := normalizeMessageSummaryKey(key)
	if err != nil {
		return MessageSummaryRecord{}, err
	}
	record, err := scanMessageSummary(s.db.QueryRowContext(ctx, `SELECT `+messageSummaryColumns+` FROM message_summaries WHERE account_id = ? AND folder_name = ? AND uid = ? AND summary_type = ?`, key.AccountID, key.FolderName, uid, replySuggestionSummaryType))
	if errors.Is(err, sql.ErrNoRows) {
		return MessageSummaryRecord{}, ErrNotFound
	}
	if err != nil {
		return MessageSummaryRecord{}, fmt.Errorf("mailstore: get message reply suggestion: %w", err)
	}
	return record, nil
}

func (s *Store) ListMessageReplySuggestions(ctx context.Context, accountID string, keys []MessageSummaryKey) (map[string]MessageSummaryRecord, error) {
	result := make(map[string]MessageSummaryRecord)
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || len(keys) == 0 {
		return result, nil
	}
	clauses := make([]string, 0, len(keys))
	args := []any{accountID, replySuggestionSummaryType}
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
		return nil, fmt.Errorf("mailstore: list message reply suggestions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanMessageSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("mailstore: scan message reply suggestion: %w", err)
		}
		result[MessageSummaryLookupKey(record.FolderName, record.UID)] = record
	}
	return result, rows.Err()
}

func (s *Store) ClaimMessageReplySuggestionGeneration(ctx context.Context, record MessageSummaryRecord, regenerate bool, lease time.Duration) (MessageSummaryRecord, bool, error) {
	key, uid, err := normalizeMessageSummaryKey(record.MessageSummaryKey)
	if err != nil {
		return MessageSummaryRecord{}, false, err
	}
	token, err := newID("reply")
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
		key.AccountID, key.FolderName, uid, replySuggestionSummaryType,
		record.SourceHash, record.ConfigHash, record.ModelID, record.ModelName, record.AgentID,
		record.PipelineVersion, token, leaseUntil, now, now, now,
	)
	if err != nil {
		return MessageSummaryRecord{}, false, fmt.Errorf("mailstore: claim message reply suggestion: %w", err)
	}
	claimed, _ := result.RowsAffected()
	current, err := s.GetMessageReplySuggestion(ctx, key)
	if err != nil {
		return MessageSummaryRecord{}, false, err
	}
	return current, claimed > 0 && current.GenerationToken == token, nil
}

func (s *Store) CompleteMessageReplySuggestionGeneration(ctx context.Context, record MessageSummaryRecord, token, suggestion string) (MessageSummaryRecord, error) {
	key, uid, err := normalizeMessageSummaryKey(record.MessageSummaryKey)
	if err != nil {
		return MessageSummaryRecord{}, err
	}
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, `UPDATE message_summaries SET summary_text = ?, status = 'ready', source_hash = ?, config_hash = ?, model_id = ?, model_name = ?, agent_id = ?, pipeline_version = ?, generation_token = '', lease_until = 0, error_message = '', updated_at = ? WHERE account_id = ? AND folder_name = ? AND uid = ? AND summary_type = ? AND generation_token = ?`, suggestion, record.SourceHash, record.ConfigHash, record.ModelID, record.ModelName, record.AgentID, record.PipelineVersion, now, key.AccountID, key.FolderName, uid, replySuggestionSummaryType, token)
	if err != nil {
		return MessageSummaryRecord{}, fmt.Errorf("mailstore: complete message reply suggestion: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return MessageSummaryRecord{}, errors.New("mailstore: message reply suggestion generation lease was lost")
	}
	return s.GetMessageReplySuggestion(ctx, key)
}

func (s *Store) FailMessageReplySuggestionGeneration(ctx context.Context, key MessageSummaryKey, token string, generationErr error) error {
	key, uid, err := normalizeMessageSummaryKey(key)
	if err != nil {
		return err
	}
	message := "reply suggestion generation failed"
	if generationErr != nil {
		message = generationErr.Error()
	}
	_, err = s.db.ExecContext(ctx, `UPDATE message_summaries SET status = CASE WHEN summary_text <> '' THEN 'ready' ELSE 'failed' END, generation_token = '', lease_until = 0, error_message = ?, updated_at = ? WHERE account_id = ? AND folder_name = ? AND uid = ? AND summary_type = ? AND generation_token = ?`, message, time.Now().Unix(), key.AccountID, key.FolderName, uid, replySuggestionSummaryType, token)
	return err
}
