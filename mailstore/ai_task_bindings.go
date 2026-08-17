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
	MailSummaryTask = "mail_summary"
	EmailDraftTask  = "email_draft"
)

func scanAITaskBinding(scanner interface{ Scan(...any) error }) (AITaskBindingRecord, error) {
	var binding AITaskBindingRecord
	var updatedAt int64
	err := scanner.Scan(&binding.AccountID, &binding.TaskType, &binding.AgentID, &binding.ModelID, &updatedAt)
	binding.UpdatedAt = time.Unix(updatedAt, 0)
	return binding, err
}

func (s *Store) GetAITaskBinding(ctx context.Context, ownerID, accountID, taskType string) (AITaskBindingRecord, error) {
	binding, err := scanAITaskBinding(s.db.QueryRowContext(ctx, `
		SELECT b.account_id, b.task_type, b.agent_id, b.model_id, b.updated_at
		FROM ai_task_bindings b
		JOIN mail_accounts a ON a.id = b.account_id
		JOIN ai_agents g ON g.id = b.agent_id AND g.owner_id = a.owner_id
		JOIN ai_models m ON m.id = b.model_id AND m.owner_id = a.owner_id
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
	ownerID = strings.TrimSpace(ownerID)
	binding.AccountID = strings.TrimSpace(binding.AccountID)
	binding.TaskType = strings.TrimSpace(binding.TaskType)
	binding.AgentID = strings.TrimSpace(binding.AgentID)
	binding.ModelID = strings.TrimSpace(binding.ModelID)
	if ownerID == "" || binding.AccountID == "" || binding.TaskType == "" || binding.AgentID == "" || binding.ModelID == "" {
		return AITaskBindingRecord{}, errors.New("mailstore: AI task binding owner, account, task, agent, and model are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AITaskBindingRecord{}, err
	}
	defer tx.Rollback()
	var accountOwner, agentOwner, modelOwner string
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
	if err := tx.QueryRowContext(ctx, `SELECT owner_id FROM ai_models WHERE id = ?`, binding.ModelID).Scan(&modelOwner); errors.Is(err, sql.ErrNoRows) {
		return AITaskBindingRecord{}, ErrNotFound
	} else if err != nil {
		return AITaskBindingRecord{}, err
	}
	if accountOwner != ownerID || agentOwner != ownerID || modelOwner != ownerID {
		return AITaskBindingRecord{}, ErrNotFound
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ai_task_bindings(account_id, task_type, agent_id, model_id, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(account_id, task_type) DO UPDATE SET
			agent_id = excluded.agent_id, model_id = excluded.model_id, updated_at = excluded.updated_at`,
		binding.AccountID, binding.TaskType, binding.AgentID, binding.ModelID, now); err != nil {
		return AITaskBindingRecord{}, fmt.Errorf("mailstore: save AI task binding: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return AITaskBindingRecord{}, err
	}
	binding.UpdatedAt = time.Unix(now, 0)
	return binding, nil
}
