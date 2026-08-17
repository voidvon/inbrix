package mailstore

import (
	"context"
	"errors"
	"testing"
)

func TestAIAgentsCRUDAndOwnerIsolation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	agent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: "alice", Name: "询价分析", Prompt: "分析询价"})
	if err != nil {
		t.Fatalf("CreateAIAgent: %v", err)
	}
	agent, err = s.UpdateAIAgent(ctx, AIAgentRecord{ID: agent.ID, OwnerID: "alice", Name: "询价分析", Prompt: "更新后的提示词"})
	if err != nil || agent.Prompt != "更新后的提示词" {
		t.Fatalf("UpdateAIAgent: %+v, %v", agent, err)
	}
	if _, err := s.GetAIAgent(ctx, "bob", agent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAIAgent owner isolation: %v", err)
	}
	if err := s.DeleteAIAgent(ctx, "alice", agent.ID); err != nil {
		t.Fatalf("DeleteAIAgent: %v", err)
	}
}

func TestWebhookInquiryAgentUsesFirstDatabaseAgentWithoutDefault(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	agents, err := s.ListAIAgents(ctx, "alice")
	if err != nil || len(agents) != 0 {
		t.Fatalf("default agents: %+v, %v", agents, err)
	}
	if _, err := s.GetWebhookInquiryAgent(ctx, "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing webhook agent: %v", err)
	}
	agent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: "alice", Name: "自定义询价分析", Prompt: "数据库中的自定义提示词"})
	if err != nil {
		t.Fatalf("CreateAIAgent: %v", err)
	}
	again, err := s.GetWebhookInquiryAgent(ctx, "alice")
	if err != nil || again.ID != agent.ID {
		t.Fatalf("first agent was not assigned: %+v, %v", again, err)
	}
	if err := s.migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	persisted, err := s.GetAIAgent(ctx, "alice", agent.ID)
	if err != nil || persisted.Prompt != "数据库中的自定义提示词" {
		t.Fatalf("database prompt was overwritten: %+v, %v", persisted, err)
	}
}

func TestMailSummaryAgentReusesLegacyWebhookAgent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	created, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: "alice", Name: "询价总结", Prompt: "分析询价"})
	if err != nil {
		t.Fatalf("CreateAIAgent: %v", err)
	}
	legacy, err := s.GetWebhookInquiryAgent(ctx, "alice")
	if err != nil {
		t.Fatalf("GetWebhookInquiryAgent: %v", err)
	}
	got, err := s.GetMailSummaryAgent(ctx, "alice")
	if err != nil {
		t.Fatalf("GetMailSummaryAgent: %v", err)
	}
	if got.ID != created.ID || got.ID != legacy.ID {
		t.Fatalf("mail summary agent = %q, want legacy agent %q", got.ID, legacy.ID)
	}
}
