package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const aiAgentColumns = `id, owner_id, name, prompt, purpose, output_labels_json, created_at`

var defaultAIAgentOutputLabels = []string{"客户", "需求", "要求", "问题"}

func NormalizeAIAgentOutputLabels(labels []string) ([]string, error) {
	if labels == nil {
		return append([]string(nil), defaultAIAgentOutputLabels...), nil
	}
	if len(labels) > 12 {
		return nil, errors.New("mailstore: AI agent cannot have more than 12 output labels")
	}
	normalized := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || len([]rune(label)) > 20 || strings.ContainsAny(label, "\r\n：:") {
			return nil, errors.New("mailstore: invalid AI agent output label")
		}
		if _, exists := seen[label]; exists {
			return nil, errors.New("mailstore: duplicate AI agent output label")
		}
		seen[label] = struct{}{}
		normalized = append(normalized, label)
	}
	return normalized, nil
}

func scanAIAgent(scanner interface{ Scan(...any) error }) (AIAgentRecord, error) {
	var agent AIAgentRecord
	var outputLabelsJSON string
	var createdAt int64
	err := scanner.Scan(&agent.ID, &agent.OwnerID, &agent.Name, &agent.Prompt, &agent.Purpose, &outputLabelsJSON, &createdAt)
	if err == nil {
		err = json.Unmarshal([]byte(outputLabelsJSON), &agent.OutputLabels)
	}
	agent.CreatedAt = time.Unix(createdAt, 0)
	return agent, err
}

func (s *Store) ListAIAgents(ctx context.Context, ownerID string) ([]AIAgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+aiAgentColumns+` FROM ai_agents WHERE owner_id = ? ORDER BY created_at, name`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list AI agents: %w", err)
	}
	defer rows.Close()
	agents := []AIAgentRecord{}
	for rows.Next() {
		agent, err := scanAIAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("mailstore: scan AI agent: %w", err)
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func (s *Store) CreateAIAgent(ctx context.Context, agent AIAgentRecord) (AIAgentRecord, error) {
	agent.OwnerID = strings.TrimSpace(agent.OwnerID)
	agent.Name = strings.TrimSpace(agent.Name)
	agent.Prompt = strings.TrimSpace(agent.Prompt)
	var err error
	agent.OutputLabels, err = NormalizeAIAgentOutputLabels(agent.OutputLabels)
	if err != nil {
		return AIAgentRecord{}, err
	}
	if agent.OwnerID == "" || agent.Name == "" || agent.Prompt == "" {
		return AIAgentRecord{}, errors.New("mailstore: AI agent owner, name, and prompt are required")
	}
	if agent.ID == "" {
		var err error
		agent.ID, err = newID("aiagent")
		if err != nil {
			return AIAgentRecord{}, err
		}
	}
	now := time.Now().Unix()
	labelsJSON, _ := json.Marshal(agent.OutputLabels)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO ai_agents(id, owner_id, name, prompt, purpose, output_labels_json, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, agent.ID, agent.OwnerID, agent.Name, agent.Prompt, agent.Purpose, string(labelsJSON), now, now); err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: create AI agent: %w", err)
	}
	agent.CreatedAt = time.Unix(now, 0)
	return agent, nil
}

func (s *Store) UpdateAIAgent(ctx context.Context, agent AIAgentRecord) (AIAgentRecord, error) {
	agent.ID = strings.TrimSpace(agent.ID)
	agent.OwnerID = strings.TrimSpace(agent.OwnerID)
	agent.Name = strings.TrimSpace(agent.Name)
	agent.Prompt = strings.TrimSpace(agent.Prompt)
	var err error
	agent.OutputLabels, err = NormalizeAIAgentOutputLabels(agent.OutputLabels)
	if err != nil {
		return AIAgentRecord{}, err
	}
	if agent.ID == "" || agent.OwnerID == "" || agent.Name == "" || agent.Prompt == "" {
		return AIAgentRecord{}, errors.New("mailstore: AI agent id, owner, name, and prompt are required")
	}
	if len(agent.OutputLabels) == 0 {
		var summaryUses int
		if err := s.db.QueryRowContext(ctx, `
			SELECT (SELECT COUNT(*) FROM ai_task_bindings WHERE agent_id = ? AND task_type = ?)
				 + (SELECT COUNT(*) FROM ai_agents WHERE owner_id = ? AND id = ? AND purpose IN ('mail_summary', 'feishu_inquiry_analysis'))`,
			agent.ID, MailSummaryTask, agent.OwnerID, agent.ID).Scan(&summaryUses); err != nil {
			return AIAgentRecord{}, fmt.Errorf("mailstore: inspect AI agent summary bindings: %w", err)
		}
		if summaryUses > 0 {
			return AIAgentRecord{}, errors.New("mailstore: a mail summary agent must have output labels")
		}
	}
	labelsJSON, _ := json.Marshal(agent.OutputLabels)
	result, err := s.db.ExecContext(ctx, `UPDATE ai_agents SET name = ?, prompt = ?, output_labels_json = ?, updated_at = ? WHERE owner_id = ? AND id = ?`, agent.Name, agent.Prompt, string(labelsJSON), time.Now().Unix(), agent.OwnerID, agent.ID)
	if err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: update AI agent: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return AIAgentRecord{}, ErrNotFound
	}
	updated, err := scanAIAgent(s.db.QueryRowContext(ctx, `SELECT `+aiAgentColumns+` FROM ai_agents WHERE owner_id = ? AND id = ?`, agent.OwnerID, agent.ID))
	if err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: read updated AI agent: %w", err)
	}
	return updated, nil
}

func (s *Store) DeleteAIAgent(ctx context.Context, ownerID, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ai_agents WHERE owner_id = ? AND id = ?`, ownerID, id)
	if err != nil {
		return fmt.Errorf("mailstore: delete AI agent: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetAIAgent(ctx context.Context, ownerID, id string) (AIAgentRecord, error) {
	agent, err := scanAIAgent(s.db.QueryRowContext(ctx, `SELECT `+aiAgentColumns+` FROM ai_agents WHERE owner_id = ? AND id = ?`, ownerID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AIAgentRecord{}, ErrNotFound
	}
	if err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: get AI agent: %w", err)
	}
	return agent, nil
}

func (s *Store) GetAIAgentByName(ctx context.Context, ownerID, name string) (AIAgentRecord, error) {
	agent, err := scanAIAgent(s.db.QueryRowContext(ctx, `SELECT `+aiAgentColumns+` FROM ai_agents WHERE owner_id = ? AND name = ?`, ownerID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return AIAgentRecord{}, ErrNotFound
	}
	if err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: get AI agent by name: %w", err)
	}
	return agent, nil
}

func (s *Store) GetWebhookInquiryAgent(ctx context.Context, ownerID string) (AIAgentRecord, error) {
	agent, err := scanAIAgent(s.db.QueryRowContext(ctx, `SELECT `+aiAgentColumns+` FROM ai_agents WHERE owner_id = ? AND purpose = 'feishu_inquiry_analysis' LIMIT 1`, ownerID))
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AIAgentRecord{}, fmt.Errorf("mailstore: get webhook inquiry agent: %w", err)
	}
	agent, err = scanAIAgent(s.db.QueryRowContext(ctx, `SELECT `+aiAgentColumns+` FROM ai_agents WHERE owner_id = ? ORDER BY created_at, name LIMIT 1`, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return AIAgentRecord{}, ErrNotFound
	}
	if err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: get first AI agent: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE ai_agents SET purpose = 'feishu_inquiry_analysis', updated_at = ? WHERE owner_id = ? AND id = ?`, time.Now().Unix(), ownerID, agent.ID); err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: assign webhook inquiry agent: %w", err)
	}
	agent.Purpose = "feishu_inquiry_analysis"
	return agent, nil
}

// GetMailSummaryAgent returns the agent used by the reusable per-message mail
// summary feature. The legacy webhook purpose remains a valid source so
// existing installations keep using the prompt they configured previously.
func (s *Store) GetMailSummaryAgent(ctx context.Context, ownerID string) (AIAgentRecord, error) {
	agent, err := scanAIAgent(s.db.QueryRowContext(ctx, `SELECT `+aiAgentColumns+` FROM ai_agents WHERE owner_id = ? AND purpose IN ('mail_summary', 'feishu_inquiry_analysis') ORDER BY CASE purpose WHEN 'mail_summary' THEN 0 ELSE 1 END LIMIT 1`, ownerID))
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AIAgentRecord{}, fmt.Errorf("mailstore: get mail summary agent: %w", err)
	}
	agent, err = scanAIAgent(s.db.QueryRowContext(ctx, `SELECT `+aiAgentColumns+` FROM ai_agents WHERE owner_id = ? ORDER BY created_at, name LIMIT 1`, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return AIAgentRecord{}, ErrNotFound
	}
	if err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: get first AI agent: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE ai_agents SET purpose = 'mail_summary', updated_at = ? WHERE owner_id = ? AND id = ?`, time.Now().Unix(), ownerID, agent.ID); err != nil {
		return AIAgentRecord{}, fmt.Errorf("mailstore: assign mail summary agent: %w", err)
	}
	agent.Purpose = "mail_summary"
	return agent, nil
}
