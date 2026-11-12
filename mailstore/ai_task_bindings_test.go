package mailstore

import (
	"context"
	"errors"
	"testing"
)

func TestAITaskBindingsAreScopedByOwnerAndMailbox(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	alice, err := s.CreateUser(ctx, "alice-bindings@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreateUser(ctx, "bob-bindings@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}

	// The same external mailbox can be connected independently by both users.
	aliceMailbox := testAccount(t, s, alice.ID, "shared@example.com", true)
	bobMailbox := testAccount(t, s, bob.ID, "shared@example.com", true)
	if aliceMailbox.ID == bobMailbox.ID {
		t.Fatal("same mailbox address shared an account ID across owners")
	}
	aliceAgent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: alice.ID, Name: "Alice summary", Prompt: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	bobAgent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: bob.ID, Name: "Bob summary", Prompt: "bob"})
	if err != nil {
		t.Fatal(err)
	}

	aliceBinding, err := s.SaveAITaskBinding(ctx, alice.ID, AITaskBindingRecord{AccountID: aliceMailbox.ID, TaskType: MailSummaryTask, AgentID: aliceAgent.ID})
	if err != nil {
		t.Fatal(err)
	}
	bobBinding, err := s.SaveAITaskBinding(ctx, bob.ID, AITaskBindingRecord{AccountID: bobMailbox.ID, TaskType: MailSummaryTask, AgentID: bobAgent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if aliceBinding.AgentID == bobBinding.AgentID {
		t.Fatal("bindings leaked across users")
	}
	if _, err := s.GetAITaskBinding(ctx, alice.ID, bobMailbox.ID, MailSummaryTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Alice read Bob's mailbox binding: %v", err)
	}
	if _, err := s.SaveAITaskBinding(ctx, alice.ID, AITaskBindingRecord{AccountID: aliceMailbox.ID, TaskType: MailSummaryTask, AgentID: bobAgent.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Alice used Bob's agent: %v", err)
	}
}

func TestMailSummaryMetadataUsesMailboxAgentAndCurrentDefaultModel(t *testing.T) {
	s, firstMailbox, _, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	secondMailbox := testAccount(t, s, firstMailbox.OwnerID, "second@example.com", false)
	agent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: firstMailbox.OwnerID, Name: "Second mailbox summary", Prompt: "second prompt"})
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: firstMailbox.OwnerID, BaseURL: "https://second.example.com/v1", Model: "second-model", EncryptedAPIKey: "encrypted"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAITaskBinding(ctx, firstMailbox.OwnerID, AITaskBindingRecord{AccountID: secondMailbox.ID, TaskType: MailSummaryTask, AgentID: agent.ID}); err != nil {
		t.Fatal(err)
	}

	firstAgent, firstModel, _, err := resolveMailSummaryMetadata(ctx, s, firstMailbox)
	if err != nil {
		t.Fatal(err)
	}
	secondAgent, secondModel, _, err := resolveMailSummaryMetadata(ctx, s, secondMailbox)
	if err != nil {
		t.Fatal(err)
	}
	if firstAgent.ID == secondAgent.ID || firstModel.ID != secondModel.ID {
		t.Fatalf("mailboxes did not keep separate agents with the same default model: first=%s/%s second=%s/%s", firstAgent.ID, firstModel.ID, secondAgent.ID, secondModel.ID)
	}
	if secondAgent.ID != agent.ID || secondModel.ID != firstModel.ID {
		t.Fatalf("second mailbox binding was ignored: agent=%s model=%s", secondAgent.ID, secondModel.ID)
	}
	if err := s.SetDefaultAIModel(ctx, firstMailbox.OwnerID, model.ID); err != nil {
		t.Fatal(err)
	}
	for _, account := range []Account{firstMailbox, secondMailbox} {
		_, current, _, err := resolveMailSummaryMetadata(ctx, s, account)
		if err != nil || current.ID != model.ID {
			t.Fatalf("mailbox %s did not follow the changed default: model=%s err=%v", account.ID, current.ID, err)
		}
	}
}

func TestEmailDraftBindingAllowsAgentWithoutOutputLabels(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "draft-bindings@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, owner.ID, "draft@example.com", true)
	agent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: owner.ID, Name: "Draft writer", Prompt: "Write concise replies", OutputLabels: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.OutputLabels) != 0 {
		t.Fatalf("draft agent unexpectedly has output labels: %v", agent.OutputLabels)
	}
	binding, err := s.SaveAITaskBinding(ctx, owner.ID, AITaskBindingRecord{AccountID: account.ID, TaskType: EmailDraftTask, AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if binding.TaskType != EmailDraftTask || binding.AgentID != agent.ID {
		t.Fatalf("unexpected email draft binding: %+v", binding)
	}
}

func TestLegacyAITaskBindingMigrationPreservesAgentAndEnabledState(t *testing.T) {
	s, account, _, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	agent, err := s.GetMailSummaryAgent(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.GetDefaultAIModel(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the old model foreign key, including its deletion cascade.
	for _, query := range []string{
		`DROP TABLE ai_task_bindings`,
		`CREATE TABLE ai_task_bindings (
			account_id TEXT NOT NULL REFERENCES mail_accounts(id) ON DELETE CASCADE,
			task_type TEXT NOT NULL,
			agent_id TEXT NOT NULL REFERENCES ai_agents(id) ON DELETE CASCADE,
			model_id TEXT NOT NULL REFERENCES ai_models(id) ON DELETE CASCADE,
			enabled INTEGER NOT NULL DEFAULT 1,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(account_id, task_type))`,
		`CREATE INDEX idx_ai_task_bindings_agent ON ai_task_bindings(agent_id)`,
		`CREATE INDEX idx_ai_task_bindings_model ON ai_task_bindings(model_id)`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	for i, task := range []string{MailSummaryTask, EmailDraftTask, ReplySuggestionTask} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO ai_task_bindings VALUES(?, ?, ?, ?, ?, 123)`, account.ID, task, agent.ID, model.ID, i%2); err != nil {
			t.Fatal(err)
		}
	}
	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.migrate(ctx); err != nil { // Repeated startup must also work.
		t.Fatal(err)
	}
	if err := s.DeleteAIModel(ctx, account.OwnerID, model.ID); err != nil {
		t.Fatal(err)
	}
	for i, task := range []string{MailSummaryTask, EmailDraftTask, ReplySuggestionTask} {
		binding, err := s.GetAITaskBinding(ctx, account.OwnerID, account.ID, task)
		if err != nil || binding.AgentID != agent.ID || binding.Enabled != (i%2 == 1) || binding.UpdatedAt.Unix() != 123 {
			t.Fatalf("migration/model deletion changed %s binding: %+v, %v", task, binding, err)
		}
	}
	// Agent selection and disabled state can be saved with no models configured.
	if _, err := s.SaveAITaskBinding(ctx, account.OwnerID, AITaskBindingRecord{AccountID: account.ID, TaskType: EmailDraftTask, AgentID: agent.ID, EnabledSet: true}); err != nil {
		t.Fatal(err)
	}
}

func TestMailboxAIConfigsFollowChangedDefaultModel(t *testing.T) {
	s, account, _, key := setupMailSummaryTest(t)
	ctx := context.Background()
	agent, err := s.GetMailSummaryAgent(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.GetDefaultAIModel(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: account.OwnerID, BaseURL: first.BaseURL, Model: "new-default", EncryptedAPIKey: first.EncryptedAPIKey})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{MailSummaryTask, ReplySuggestionTask, MailTranslationTask} {
		if _, err := s.SaveAITaskBinding(ctx, account.OwnerID, AITaskBindingRecord{AccountID: account.ID, TaskType: task, AgentID: agent.ID}); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []AIModelRecord{first, second} {
		if err := s.SetDefaultAIModel(ctx, account.OwnerID, want.ID); err != nil {
			t.Fatal(err)
		}
		summary, err := resolveMailSummaryConfig(ctx, s, key, account)
		if err != nil || summary.model.ID != want.ID || summary.agent.ID != agent.ID {
			t.Fatalf("summary did not follow default %s: %+v, %v", want.ID, summary, err)
		}
		reply, err := resolveReplySuggestionConfig(ctx, s, key, account)
		if err != nil || reply.model.ID != want.ID || reply.agent.ID != agent.ID {
			t.Fatalf("reply did not follow default %s: %+v, %v", want.ID, reply, err)
		}
		translation, err := resolveMailTranslationConfig(ctx, s, key, account, false)
		if err != nil || translation.model.ID != want.ID || translation.agent.ID != agent.ID {
			t.Fatalf("translation did not follow default %s: %+v, %v", want.ID, translation, err)
		}
	}
}
