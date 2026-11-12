package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	MailSummaryTask     = "mail_summary"
	EmailDraftTask      = "email_draft"
	ReplySuggestionTask = "reply_suggestion"
	MailTranslationTask = "mail_translation"
)

// ErrAITaskDisabled indicates an intentional mailbox setting, not an AI failure.
var ErrAITaskDisabled = errors.New("this AI function is disabled for the mailbox")

// Remove the former model foreign key so deleting a model cannot delete the
// mailbox's agent selection or its enabled state.
func (s *Store) migrateAITaskBindingsToAgents(ctx context.Context) error {
	var hasModel int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('ai_task_bindings') WHERE name = 'model_id'`).Scan(&hasModel); err != nil {
		return fmt.Errorf("mailstore: inspect AI binding model column: %w", err)
	}
	if hasModel == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range []string{
		`CREATE TABLE ai_task_bindings_agents (
			account_id TEXT NOT NULL,
			task_type TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(account_id, task_type),
			FOREIGN KEY(account_id) REFERENCES mail_accounts(id) ON DELETE CASCADE,
			FOREIGN KEY(agent_id) REFERENCES ai_agents(id) ON DELETE CASCADE
		)`,
		`INSERT INTO ai_task_bindings_agents(account_id, task_type, agent_id, enabled, updated_at)
		 SELECT account_id, task_type, agent_id, enabled, updated_at FROM ai_task_bindings`,
		`DROP TABLE ai_task_bindings`,
		`ALTER TABLE ai_task_bindings_agents RENAME TO ai_task_bindings`,
		`CREATE INDEX idx_ai_task_bindings_agent ON ai_task_bindings(agent_id)`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("mailstore: migrate AI bindings to agents: %w", err)
		}
	}
	return tx.Commit()
}

func scanAITaskBinding(scanner interface{ Scan(...any) error }) (AITaskBindingRecord, error) {
	var binding AITaskBindingRecord
	var updatedAt int64
	var enabled int
	err := scanner.Scan(&binding.AccountID, &binding.TaskType, &binding.AgentID, &enabled, &updatedAt)
	binding.Enabled = intBool(enabled)
	binding.UpdatedAt = time.Unix(updatedAt, 0)
	return binding, err
}

func (s *Store) GetAITaskBinding(ctx context.Context, ownerID, accountID, taskType string) (AITaskBindingRecord, error) {
	binding, err := scanAITaskBinding(s.db.QueryRowContext(ctx, `
		SELECT b.account_id, b.task_type, b.agent_id, b.enabled, b.updated_at
		FROM ai_task_bindings b
		JOIN mail_accounts a ON a.id = b.account_id
		JOIN ai_agents g ON g.id = b.agent_id AND g.owner_id = a.owner_id
		WHERE a.owner_id = ? AND b.account_id = ? AND b.task_type = ?`,
		ownerID, accountID, strings.TrimSpace(taskType)))
	if errors.Is(err, sql.ErrNoRows) {
		return AITaskBindingRecord{}, ErrNotFound
	}
	if err != nil {
		return AITaskBindingRecord{}, fmt.Errorf("mailstore: get AI task binding: %w", err)
	}
	return binding, nil
}

func (s *Store) SaveAITaskBinding(ctx context.Context, ownerID string, binding AITaskBindingRecord) (AITaskBindingRecord, error) {
	if !binding.EnabledSet && !binding.Enabled {
		binding.Enabled = true
	}
	ownerID = strings.TrimSpace(ownerID)
	binding.AccountID = strings.TrimSpace(binding.AccountID)
	binding.TaskType = strings.TrimSpace(binding.TaskType)
	binding.AgentID = strings.TrimSpace(binding.AgentID)
	if ownerID == "" || binding.AccountID == "" || binding.TaskType == "" || binding.AgentID == "" {
		return AITaskBindingRecord{}, errors.New("mailstore: AI task binding owner, account, task, and agent are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AITaskBindingRecord{}, err
	}
	defer tx.Rollback()
	var accountOwner, agentOwner string
	if err := tx.QueryRowContext(ctx, `SELECT owner_id FROM mail_accounts WHERE id = ?`, binding.AccountID).Scan(&accountOwner); errors.Is(err, sql.ErrNoRows) {
		return AITaskBindingRecord{}, ErrNotFound
	} else if err != nil {
		return AITaskBindingRecord{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT owner_id FROM ai_agents WHERE id = ?`, binding.AgentID).Scan(&agentOwner); errors.Is(err, sql.ErrNoRows) {
		return AITaskBindingRecord{}, ErrNotFound
	} else if err != nil {
		return AITaskBindingRecord{}, err
	}
	if accountOwner != ownerID || agentOwner != ownerID {
		return AITaskBindingRecord{}, ErrNotFound
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ai_task_bindings(account_id, task_type, agent_id, enabled, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(account_id, task_type) DO UPDATE SET
			agent_id = excluded.agent_id, enabled = excluded.enabled, updated_at = excluded.updated_at`,
		binding.AccountID, binding.TaskType, binding.AgentID, boolInt(binding.Enabled), now); err != nil {
		return AITaskBindingRecord{}, fmt.Errorf("mailstore: save AI task binding: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return AITaskBindingRecord{}, err
	}
	binding.UpdatedAt = time.Unix(now, 0)
	return binding, nil
}
