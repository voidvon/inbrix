package maileval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mailapi "inbrix/handlers/api"
	"inbrix/mailstore"
)

func TestConfiguredSourceOnlyReadsSettingsAndKeepsSkillSnapshot(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := mailstore.Open(filepath.Join(dir, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(ctx, "eval-owner", "Eval owner", "unused")
	if err != nil {
		t.Fatal(err)
	}
	const key = "0123456789abcdef0123456789abcdef"
	encrypted, err := mailapi.EncryptJSON("provider-secret", key)
	if err != nil {
		t.Fatal(err)
	}
	model, err := db.CreateAIModel(ctx, mailstore.AIModelRecord{OwnerID: user.ID, Provider: "openai-compatible", BaseURL: "https://model.example.invalid/v1", Model: "test-model", EncryptedAPIKey: encrypted})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateAIAgent(ctx, mailstore.AIAgentRecord{OwnerID: user.ID, Name: "Configured summary", Prompt: "SOURCE-PROMPT", Purpose: "mail_summary"})
	if err != nil {
		t.Fatal(err)
	}
	skill, err := db.GetAISkill(ctx, user.ID, "mail-analysis")
	if err != nil {
		t.Fatal(err)
	}
	skill.Instructions += "\nSOURCE-SKILL"
	if _, err := db.SaveAISkill(ctx, skill, skill.Revision); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[mail_sync]\ndatabase='mail.db'\n[encryption]\nkey='"+key+"'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Any attempted write to settings aborts. Read-only load must still succeed.
	if _, err := db.DB().Exec(`CREATE TRIGGER forbid_eval_model_write BEFORE UPDATE ON ai_models BEGIN SELECT RAISE(ABORT,'unexpected settings write'); END`); err != nil {
		t.Fatal(err)
	}
	cfg, origin, err := LoadConfiguredExecution(ctx, configPath, user.ID, model.ID, "")
	if err != nil || origin != "configured-snapshot" || cfg.APIKey != "provider-secret" || cfg.Model.ID != model.ID || cfg.Skill.Revision != 2 || !strings.Contains(cfg.Skill.Instructions, "SOURCE-SKILL") || cfg.Agent.Prompt != "SOURCE-PROMPT" {
		t.Fatal(origin, err)
	}
	raw, _ := json.Marshal(cfg)
	if strings.Contains(string(raw), "provider-secret") || strings.Contains(string(raw), encrypted) {
		t.Fatal("configuration serializes credentials")
	}
	if _, _, err := LoadConfiguredExecution(ctx, configPath, "wrong-owner", model.ID, ""); err == nil {
		t.Fatal("cross-owner model selection accepted")
	}
	candidatePath := filepath.Join(dir, "candidate.md")
	if err := os.WriteFile(candidatePath, []byte("CANDIDATE-INSTRUCTIONS"), 0600); err != nil {
		t.Fatal(err)
	}
	candidate, err := OverrideSkillFile(cfg, candidatePath)
	if err != nil || candidate.Skill.Revision != 0 || candidate.Skill.Fingerprint == cfg.Skill.Fingerprint {
		t.Fatal(candidate.Skill, err)
	}
	stored, err := db.GetAISkill(ctx, user.ID, "mail-analysis")
	if err != nil || stored.Revision != 2 || strings.Contains(stored.Instructions, "CANDIDATE-INSTRUCTIONS") {
		t.Fatal("candidate mutated active settings", stored, err)
	}
}

func TestConfiguredSourceRejectsAmbiguousOwners(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := mailstore.Open(filepath.Join(dir, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, login := range []string{"first", "second"} {
		user, err := db.CreateUser(ctx, login, login, "unused")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateAIModel(ctx, mailstore.AIModelRecord{OwnerID: user.ID, BaseURL: "https://model.example.invalid", Model: "model", EncryptedAPIKey: "unused"}); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[mail_sync]\ndatabase='mail.db'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadConfiguredExecution(ctx, path, "", "", ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatal(err)
	}
}
