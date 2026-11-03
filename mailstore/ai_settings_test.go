package mailstore

import (
	"context"
	"errors"
	"testing"
)

func TestAIModelsCRUDDefaultsAndOwnerIsolation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	first, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: "alice", Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-first", ReasoningEffort: "low", EncryptedAPIKey: "encrypted-1"})
	if err != nil {
		t.Fatalf("CreateAIModel first: %v", err)
	}
	if !first.IsDefault {
		t.Fatal("first model should become default")
	}
	if first.ReasoningEffort != "low" {
		t.Fatalf("reasoning effort: got %q", first.ReasoningEffort)
	}
	first, err = s.UpdateAIModel(ctx, AIModelRecord{ID: first.ID, OwnerID: "alice", BaseURL: "https://gateway.example/v1", Model: "gpt-first-edited", ReasoningEffort: "medium"})
	if err != nil {
		t.Fatalf("UpdateAIModel: %v", err)
	}
	if first.Model != "gpt-first-edited" || first.ReasoningEffort != "medium" || first.EncryptedAPIKey != "encrypted-1" || !first.IsDefault {
		t.Fatalf("updated model did not preserve key/default: %+v", first)
	}
	if _, err := s.GetAIModel(ctx, "bob", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAIModel owner isolation: %v", err)
	}
	second, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: "alice", Provider: "openai", BaseURL: "https://gateway.example/v1", Model: "gpt-second", EncryptedAPIKey: "encrypted-2"})
	if err != nil {
		t.Fatalf("CreateAIModel second: %v", err)
	}
	if second.IsDefault {
		t.Fatal("second model unexpectedly became default")
	}
	if err := s.SetDefaultAIModel(ctx, "alice", second.ID); err != nil {
		t.Fatalf("SetDefaultAIModel: %v", err)
	}
	got, err := s.GetDefaultAIModel(ctx, "alice")
	if err != nil || got.ID != second.ID {
		t.Fatalf("default model: %+v, err=%v", got, err)
	}
	if got.ReasoningEffort != "medium" {
		t.Fatalf("default reasoning effort: got %q", got.ReasoningEffort)
	}
	if models, err := s.ListAIModels(ctx, "bob"); err != nil || len(models) != 0 {
		t.Fatalf("bob can see alice models: %+v, err=%v", models, err)
	}
	if err := s.DeleteAIModel(ctx, "alice", second.ID); err != nil {
		t.Fatalf("DeleteAIModel: %v", err)
	}
	got, err = s.GetDefaultAIModel(ctx, "alice")
	if err != nil || got.ID != first.ID || !got.IsDefault {
		t.Fatalf("remaining model was not promoted: %+v, err=%v", got, err)
	}
}

func TestLegacyDeepSeekModelMigrationOnOpen(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	account := testAccount(t, s, "alice", "sales@example.com", true)
	agent, err := s.CreateAIAgent(ctx, AIAgentRecord{OwnerID: "alice", Name: "Writer", Prompt: "Write replies"})
	if err != nil {
		t.Fatal(err)
	}
	images := false
	model, err := s.CreateAIModel(ctx, AIModelRecord{
		OwnerID: "alice", Provider: "openai-compatible", BaseURL: "https://api.deepseek.com/v1",
		Model: "deepseek-reasoner", ReasoningEffort: "high", ExtraParams: `{"temperature":0.5}`,
		EncryptedAPIKey: "encrypted-legacy-key", SupportsImages: &images,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := s.SaveAITaskBinding(ctx, "alice", AITaskBindingRecord{AccountID: account.ID, TaskType: EmailDraftTask, AgentID: agent.ID, ModelID: model.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce an existing installation rather than using the current API.
	if _, err := s.db.ExecContext(ctx, `UPDATE ai_models SET provider = 'deepseek', updated_at = 123 WHERE id = ?`, model.ID); err != nil {
		t.Fatal(err)
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
	// The regular startup migration is repeatable, including on the next restart.
	if err := s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var provider string
	var updatedAt int64
	if err := s.db.QueryRowContext(ctx, `SELECT provider, updated_at FROM ai_models WHERE id = ?`, model.ID).Scan(&provider, &updatedAt); err != nil || provider != "openai-compatible" || updatedAt != 123 {
		t.Fatalf("persisted migration: provider=%q, updated_at=%d, err=%v", provider, updatedAt, err)
	}
	get, err := s.GetAIModel(ctx, "alice", model.ID)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAIModels(ctx, "alice")
	if err != nil || len(list) != 1 {
		t.Fatalf("legacy list: count=%d, err=%v", len(list), err)
	}
	defaultModel, err := s.GetDefaultAIModel(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []AIModelRecord{get, list[0], defaultModel} {
		if got.Provider != "openai-compatible" || got.ID != model.ID || got.BaseURL != model.BaseURL || got.Model != model.Model || got.EncryptedAPIKey != model.EncryptedAPIKey || got.ReasoningEffort != model.ReasoningEffort || got.ExtraParams != model.ExtraParams || !got.IsDefault || got.SupportsImageInput() {
			t.Errorf("migration changed configuration: %+v", got)
		}
	}
	migratedBinding, err := s.GetAITaskBinding(ctx, "alice", account.ID, EmailDraftTask)
	if err != nil || migratedBinding != binding {
		t.Fatalf("migration changed task binding: %+v, err=%v", migratedBinding, err)
	}
}

func TestLegacyDeepSeekModelMigrationConflictIsAtomic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"legacy-first", "legacy-conflict", "current"} {
		provider, modelName := "deepseek", "duplicate-model"
		if id == "legacy-first" {
			modelName = "other-model"
		}
		if id == "current" {
			provider = "openai-compatible"
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO ai_models(id, owner_id, provider, base_url, model, encrypted_api_key, created_at, updated_at) VALUES(?, 'alice', ?, 'https://api.deepseek.com', ?, 'encrypted', 1, 1)`, id, provider, modelName); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.migrateLegacyAIModelProviders(ctx); err == nil {
		t.Fatal("expected a migration error for conflicting model configurations")
	}
	var oldCount, newCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_models WHERE provider = 'deepseek'`).Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_models WHERE provider = 'openai-compatible'`).Scan(&newCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 2 || newCount != 1 {
		t.Fatalf("conflict partially migrated or removed configurations: old=%d, new=%d", oldCount, newCount)
	}
}

func TestAIModelImageSupportMigrationAndOverrides(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	models := []AIModelRecord{
		{Provider: "openai", Model: "existing-responses"},
		{Provider: "openai-compatible", Model: "existing-chat"},
		{Provider: "gemini", Model: "existing-gemini"},
	}
	for i, model := range models {
		model.OwnerID, model.BaseURL, model.EncryptedAPIKey = "alice", "https://example.com/v1", "encrypted"
		var err error
		models[i], err = s.CreateAIModel(ctx, model)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Reproduce an existing installation and verify the migration is repeatable.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE ai_models DROP COLUMN supports_images`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.ensureAIModelSupportsImagesColumn(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, model := range models {
		stored, err := s.GetAIModel(ctx, "alice", model.ID)
		if err != nil || stored.SupportsImages != nil || !stored.SupportsImageInput() {
			t.Fatalf("legacy model %s: %+v %v", model.Model, stored, err)
		}
		// Explicit settings override the protocol default.
		for _, enabled := range []bool{false, true} {
			stored.SupportsImages = &enabled
			if _, err := s.UpdateAIModel(ctx, stored); err != nil {
				t.Fatal(err)
			}
			// Older clients omitting the field must not reset the saved choice.
			stored.SupportsImages = nil
			stored, err = s.UpdateAIModel(ctx, stored)
			if err != nil || stored.SupportsImages == nil || stored.SupportsImageInput() != enabled || stored.EncryptedAPIKey != "encrypted" {
				t.Fatalf("saved capability for %s: %+v %v", model.Model, stored, err)
			}
		}
	}
}
