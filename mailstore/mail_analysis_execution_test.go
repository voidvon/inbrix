package mailstore

import (
	"context"
	"errors"
	"testing"
)

func TestAnalyzeMailWithConfigUsesFrozenSkillWithoutCacheOrQueuedDelivery(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	cfg, err := resolveMailSummaryConfig(ctx, s, key, account)
	if err != nil {
		t.Fatal(err)
	}
	cfg.agent.OutputLabels = []string{}
	frozen := MailAnalysisExecutionConfig{Model: cfg.model, APIKey: cfg.apiKey, Agent: cfg.agent, Skill: cfg.skill}
	client := &analysisScriptClient{reply: func(_ int, _ string) string { return standaloneAnalysisOutput }}
	first, err := AnalyzeMailWithConfig(ctx, client, s, account, message, frozen)
	if err != nil || first.Analysis == nil || first.Analysis.SkillSnapshot.Revision != 1 {
		t.Fatal(first, err)
	}
	skill, err := s.GetAISkill(ctx, account.OwnerID, "mail-analysis")
	if err != nil {
		t.Fatal(err)
	}
	skill.Instructions += "\nEdited while replay snapshot is frozen."
	if _, err := s.SaveAISkill(ctx, skill, skill.Revision); err != nil {
		t.Fatal(err)
	}
	second, err := AnalyzeMailWithConfig(ctx, client, s, account, message, frozen)
	if err != nil || second.Analysis.SkillSnapshot.Revision != 1 || second.ConfigHash != first.ConfigHash || len(client.inputs) != 2 {
		t.Fatal(second, err, len(client.inputs))
	}
	if _, err := s.GetMessageSummary(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatal("replay wrote a cached summary", err)
	}
	tasks, err := s.ListMailTasks(ctx, account.ID, message.Folder, message.ID, 100)
	if err != nil || len(tasks) != 0 {
		t.Fatal("replay queued delivery or AI tasks", tasks, err)
	}
}
