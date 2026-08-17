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
