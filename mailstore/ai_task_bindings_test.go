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
	aliceModel, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: alice.ID, BaseURL: "https://alice.example.com/v1", Model: "alice-model", EncryptedAPIKey: "encrypted"})
	if err != nil {
		t.Fatal(err)
	}
	bobModel, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: bob.ID, BaseURL: "https://bob.example.com/v1", Model: "bob-model", EncryptedAPIKey: "encrypted"})
	if err != nil {
		t.Fatal(err)
	}

	aliceBinding, err := s.SaveAITaskBinding(ctx, alice.ID, AITaskBindingRecord{AccountID: aliceMailbox.ID, TaskType: MailSummaryTask, AgentID: aliceAgent.ID, ModelID: aliceModel.ID})
	if err != nil {
		t.Fatal(err)
	}
	bobBinding, err := s.SaveAITaskBinding(ctx, bob.ID, AITaskBindingRecord{AccountID: bobMailbox.ID, TaskType: MailSummaryTask, AgentID: bobAgent.ID, ModelID: bobModel.ID})
	if err != nil {
		t.Fatal(err)
	}
	if aliceBinding.AgentID == bobBinding.AgentID || aliceBinding.ModelID == bobBinding.ModelID {
		t.Fatal("bindings leaked across users")
	}
	if _, err := s.GetAITaskBinding(ctx, alice.ID, bobMailbox.ID, MailSummaryTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Alice read Bob's mailbox binding: %v", err)
	}
	if _, err := s.SaveAITaskBinding(ctx, alice.ID, AITaskBindingRecord{AccountID: aliceMailbox.ID, TaskType: MailSummaryTask, AgentID: bobAgent.ID, ModelID: aliceModel.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Alice used Bob's agent: %v", err)
	}
}

func TestMailSummaryMetadataUsesMailboxBinding(t *testing.T) {
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
	if _, err := s.SaveAITaskBinding(ctx, firstMailbox.OwnerID, AITaskBindingRecord{AccountID: secondMailbox.ID, TaskType: MailSummaryTask, AgentID: agent.ID, ModelID: model.ID}); err != nil {
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
	if firstAgent.ID == secondAgent.ID || firstModel.ID == secondModel.ID {
		t.Fatalf("mailboxes resolved the same configuration: first=%s/%s second=%s/%s", firstAgent.ID, firstModel.ID, secondAgent.ID, secondModel.ID)
	}
	if secondAgent.ID != agent.ID || secondModel.ID != model.ID {
		t.Fatalf("second mailbox binding was ignored: agent=%s model=%s", secondAgent.ID, secondModel.ID)
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
	model, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: owner.ID, BaseURL: "https://draft.example.com/v1", Model: "draft-model", EncryptedAPIKey: "encrypted"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := s.SaveAITaskBinding(ctx, owner.ID, AITaskBindingRecord{AccountID: account.ID, TaskType: EmailDraftTask, AgentID: agent.ID, ModelID: model.ID})
	if err != nil {
		t.Fatal(err)
	}
	if binding.TaskType != EmailDraftTask || binding.AgentID != agent.ID || binding.ModelID != model.ID {
		t.Fatalf("unexpected email draft binding: %+v", binding)
	}
}
