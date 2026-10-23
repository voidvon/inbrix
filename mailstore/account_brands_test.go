package mailstore

import (
	"context"
	"testing"
)

func TestMailboxBrandsMigrationIsOneTimeAndIndependentOfAgent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	user, err := s.CreateUser(ctx, "mailbox-brands", "Sales", "unused")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, user.ID, "sales@example.com", true)
	agent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: user.ID, Name: "Legacy sales", Prompt: "sales", BusinessProfile: AIBusinessProfile{Brands: []string{"A"}, BrandMode: "restrict"}})
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: user.ID, Provider: "openai", BaseURL: "https://api.example.com/v1", Model: "test", EncryptedAPIKey: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveAITaskBinding(ctx, user.ID, AITaskBindingRecord{AccountID: account.ID, TaskType: EmailDraftTask, AgentID: agent.ID, ModelID: model.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `DROP TABLE account_brand_settings`); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateAccountBrands(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAccountBrands(ctx, user.ID, account.ID)
	if err != nil || got.BrandMode != "restrict" || len(got.Brands) != 1 || got.Brands[0] != "A" {
		t.Fatalf("%+v %v", got, err)
	}
	if err = s.SaveAccountBrands(ctx, user.ID, account.ID, AccountBrands{BrandMode: "prefer"}); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateAccountBrands(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetAccountBrands(ctx, user.ID, account.ID)
	if err != nil || len(got.Brands) != 0 || got.BrandMode != "prefer" {
		t.Fatalf("migration overwrote mailbox config: %+v %v", got, err)
	}
	if _, err = s.GetAccountBrands(ctx, "outsider", account.ID); err == nil {
		t.Fatal("foreign binding readable")
	}
	if err = s.SaveAccountBrands(ctx, "outsider", account.ID, AccountBrands{}); err == nil {
		t.Fatal("foreign binding writable")
	}
	legacy, err := s.GetAIAgent(ctx, user.ID, agent.ID)
	if err != nil || len(legacy.BusinessProfile.Brands) != 1 {
		t.Fatal("legacy data lost", err)
	}
}
