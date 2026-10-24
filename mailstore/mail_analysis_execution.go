package mailstore

import (
	"context"
	"errors"
	"strings"

	"inbrix/models"
)

// MailAnalysisExecutionConfig is an already resolved, frozen execution input.
// Explicit configuration lets isolated replays use the production analysis
// path without mutating active settings or creating notification tasks.
type MailAnalysisExecutionConfig struct {
	Model  AIModelRecord    `json:"-"`
	APIKey string           `json:"-"`
	Agent  AIAgentRecord    `json:"-"`
	Skill  AISkillExecution `json:"-"`
}

type MailAnalysisExecutionResult struct {
	Analysis        *MailAnalysis `json:"analysis"`
	Summary         string        `json:"summary"`
	ConfigHash      string        `json:"configHash"`
	PipelineVersion int           `json:"pipelineVersion"`
}

// ResolveAISkillSnapshot shares the exact dependency and size checks used by
// live settings. It never changes the supplied registry or storage.
func ResolveAISkillSnapshot(records []AISkillRecord, id, kind string) (AISkillExecution, error) {
	return resolveAISkillRecords(records, id, kind)
}

func DefaultMailAnalysisSkillSnapshot() (AISkillExecution, error) {
	return resolveAISkillRecords(defaultAISkills(), "mail-analysis", AISkillMailSummary)
}

func AnalyzeMailWithConfig(ctx context.Context, client HTTPClient, store *Store, account Account, message models.Email, input MailAnalysisExecutionConfig) (MailAnalysisExecutionResult, error) {
	if store == nil || client == nil || strings.TrimSpace(input.APIKey) == "" || input.Model.Model == "" || input.Skill.Instructions == "" {
		return MailAnalysisExecutionResult{}, errors.New("incomplete mail analysis execution configuration")
	}
	effort := input.Model.ReasoningEffort
	if effort == "" {
		effort = "medium"
	}
	cfg := mailSummaryConfig{model: input.Model, apiKey: input.APIKey, agent: input.Agent, skill: input.Skill, effort: effort, configHash: mailSummaryConfigHash(input.Agent, input.Model, effort, input.Skill)}
	summary, analysis, err := generateMailSummary(ctx, client, store, account, message, cfg)
	return MailAnalysisExecutionResult{Analysis: analysis, Summary: summary, ConfigHash: cfg.configHash, PipelineVersion: mailSummaryPipelineVersion}, err
}
