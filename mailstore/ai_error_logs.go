package mailstore

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AIErrorLogRecord records an execution failure during an AI task (summary, drafting, testing, etc.).
type AIErrorLogRecord struct {
	ID           string    `json:"id"`
	OwnerID      string    `json:"ownerId"`
	TaskType     string    `json:"taskType"`
	AccountEmail string    `json:"accountEmail"`
	ModelName    string    `json:"modelName"`
	AgentName    string    `json:"agentName"`
	ErrorMessage string    `json:"errorMessage"`
	CreatedAt    time.Time `json:"createdAt"`
}

// RecordAIError inserts a new error record for the user and trims old records if needed.
func (s *Store) RecordAIError(ctx context.Context, record AIErrorLogRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	ownerID := strings.TrimSpace(record.OwnerID)
	if ownerID == "" {
		return nil
	}
	id, err := newID("aierr")
	if err != nil {
		return err
	}
	now := time.Now()
	errMsg := strings.TrimSpace(record.ErrorMessage)
	if len(errMsg) > 2000 {
		errMsg = errMsg[:2000] + "..."
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO ai_error_logs (id, owner_id, task_type, account_email, model_name, agent_name, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, ownerID, strings.TrimSpace(record.TaskType), strings.TrimSpace(record.AccountEmail),
		strings.TrimSpace(record.ModelName), strings.TrimSpace(record.AgentName), errMsg, now.UnixMilli())
	if err != nil {
		return fmt.Errorf("mailstore: record AI error log: %w", err)
	}

	go func() {
		_ = s.cleanupOldAIErrorLogs(context.Background(), ownerID, 500)
	}()

	return nil
}

func (s *Store) cleanupOldAIErrorLogs(ctx context.Context, ownerID string, keep int) error {
	if s == nil || s.db == nil || keep <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM ai_error_logs
		WHERE owner_id = ? AND id NOT IN (
			SELECT id FROM ai_error_logs
			WHERE owner_id = ?
			ORDER BY created_at DESC, rowid DESC
			LIMIT ?
		)
	`, ownerID, ownerID, keep)
	return err
}

// ListAIErrorLogs returns recent error logs for an owner ordered by creation time descending.
func (s *Store) ListAIErrorLogs(ctx context.Context, ownerID string, limit int) ([]AIErrorLogRecord, error) {
	if s == nil || s.db == nil {
		return []AIErrorLogRecord{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_id, task_type, account_email, model_name, agent_name, error_message, created_at
		FROM ai_error_logs
		WHERE owner_id = ?
		ORDER BY created_at DESC, rowid DESC
		LIMIT ?
	`, strings.TrimSpace(ownerID), limit)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list AI error logs: %w", err)
	}
	defer rows.Close()

	logs := make([]AIErrorLogRecord, 0)
	for rows.Next() {
		var r AIErrorLogRecord
		var createdAt int64
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.TaskType, &r.AccountEmail, &r.ModelName, &r.AgentName, &r.ErrorMessage, &createdAt); err != nil {
			return nil, fmt.Errorf("mailstore: scan AI error log: %w", err)
		}
		r.CreatedAt = time.UnixMilli(createdAt)
		logs = append(logs, r)
	}
	return logs, rows.Err()
}

// ClearAIErrorLogs removes all AI error logs for an owner.
func (s *Store) ClearAIErrorLogs(ctx context.Context, ownerID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM ai_error_logs WHERE owner_id = ?`, strings.TrimSpace(ownerID))
	if err != nil {
		return fmt.Errorf("mailstore: clear AI error logs: %w", err)
	}
	return nil
}
