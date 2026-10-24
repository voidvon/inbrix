package mailstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSkillHistoryRemovalPreservesCurrentConfiguration(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	current, err := s.GetAISkill(ctx, "alice", "mail-analysis")
	if err != nil {
		t.Fatal(err)
	}
	current.Instructions = "Current instructions that must survive migration"
	current, err = s.SaveAISkill(ctx, current, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: "custom-summary", Name: "Custom", Instructions: "Use the current dependency", Kind: AISkillMailSummary, DependsOn: []string{current.ID}, Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMailSummarySkill(ctx, "alice", root.ID, root.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAISkill(ctx, "alice", "contract", 1); err != nil {
		t.Fatal(err)
	}
	// Simulate the former archive in an existing database, including an old
	// snapshot that must never replace the active content during upgrade.
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE ai_skill_versions (
		owner_id TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL,
		snapshot_json TEXT NOT NULL, action TEXT NOT NULL, source TEXT NOT NULL,
		restored_from INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
		PRIMARY KEY(owner_id,id,revision))`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO ai_skill_versions VALUES('alice','mail-analysis',1,'{"instructions":"OLD"}','save','system',0,1)`); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var count int
	if err := upgraded.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='ai_skill_versions'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("archive still exists: count=%d err=%v", count, err)
	}
	got, err := upgraded.GetAISkill(ctx, "alice", current.ID)
	if err != nil || got.Instructions != current.Instructions || got.Revision != current.Revision {
		t.Fatalf("current content changed: %+v %v", got, err)
	}
	execution, err := upgraded.ResolveMailSummarySkill(ctx, "alice")
	if err != nil || execution.ID != root.ID || len(execution.Versions) != 2 || !strings.Contains(execution.Instructions, current.Instructions) {
		t.Fatalf("binding or dependencies changed: %+v %v", execution, err)
	}
	if _, err := upgraded.GetAISkill(ctx, "alice", "contract"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted default reappeared", err)
	}
	// Subsequent writes work without an archive; deletion removes the only
	// stored definition, and recreation does not recover previous content.
	got.Instructions = "Updated after migration"
	got, err = upgraded.SaveAISkill(ctx, got, got.Revision)
	if err != nil || got.Revision != current.Revision+1 {
		t.Fatal(got, err)
	}
	if err := upgraded.DeleteAISkill(ctx, "alice", root.ID, root.Revision); !errors.Is(err, ErrAISkillInUse) {
		t.Fatal("active summary lost protection", err)
	}
	if err := upgraded.SetMailSummarySkill(ctx, "alice", current.ID, got.Revision); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.DeleteAISkill(ctx, "alice", root.ID, root.Revision); err != nil {
		t.Fatal(err)
	}
	root.Instructions = "Recreated content"
	root, err = upgraded.SaveAISkill(ctx, root, 0)
	if err != nil || root.Revision != 1 || root.Instructions != "Recreated content" {
		t.Fatal(root, err)
	}
}
