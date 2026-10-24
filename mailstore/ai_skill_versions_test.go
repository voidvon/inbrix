package mailstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAISkillVersionRestoreIsImmutableAndRevisionSurvivesRecreation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	r, err := s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: "follow-up", Name: "Follow up", Kind: AISkillChat, Instructions: "Original instructions", Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r.Instructions = "Updated instructions"
	r, err = s.SaveAISkill(WithAISkillChangeSource(ctx, "api_key:test-id"), r, r.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.GetAISkillVersion(ctx, "alice", r.ID, 1)
	if err != nil || v.Skill.Instructions != "Original instructions" {
		t.Fatal(v, err)
	}
	v2, err := s.GetAISkillVersion(ctx, "alice", r.ID, 2)
	if err != nil || v2.Source != "api_key:test-id" {
		t.Fatal(v2, err)
	}
	if _, err := s.GetAISkillVersion(ctx, "bob", r.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner leaked", err)
	}
	r, err = s.RestoreAISkillVersion(ctx, "alice", r.ID, 1, 2)
	if err != nil || r.Revision != 3 || r.Instructions != v.Skill.Instructions {
		t.Fatal(r, err)
	}
	v3, err := s.GetAISkillVersion(ctx, "alice", r.ID, 3)
	if err != nil || v3.Action != "restore" || v3.RestoredFrom != 1 {
		t.Fatal(v3, err)
	}
	if _, err := s.RestoreAISkillVersion(ctx, "alice", r.ID, 2, 2); !errors.Is(err, ErrAISkillConflict) {
		t.Fatal("stale restore", err)
	}
	if err := s.DeleteAISkill(ctx, "alice", r.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAISkillVersion(ctx, "alice", r.ID, 1); err != nil {
		t.Fatal("delete removed history", err)
	}
	r, err = s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: r.ID, Name: "Recreated", Kind: AISkillChat, Instructions: "New incarnation", Enabled: true}, 0)
	if err != nil || r.Revision != 4 {
		t.Fatal(r, err)
	}
	versions, err := s.ListAISkillVersions(ctx, "alice", r.ID, 4, 2)
	if err != nil || len(versions) != 2 || versions[0].Skill.Revision != 3 || versions[1].Skill.Revision != 2 {
		t.Fatal(versions, err)
	}
}

func TestAISkillVersionRestoreValidatesCurrentGraph(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base, err := s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: "base", Name: "Base", Kind: AISkillChat, Instructions: "Base flow", Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: "dependent", Name: "Dependent", Kind: AISkillChat, Instructions: "Flow", DependsOn: []string{base.ID}, Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r.DependsOn = nil
	r, err = s.SaveAISkill(ctx, r, r.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAISkill(ctx, "alice", base.ID, base.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreAISkillVersion(ctx, "alice", r.ID, 1, 2); !errors.Is(err, ErrAISkillInUse) {
		t.Fatal("dangling restore accepted", err)
	}
	current, _ := s.GetAISkill(ctx, "alice", r.ID)
	versions, _ := s.ListAISkillVersions(ctx, "alice", r.ID, 0, 50)
	if current.Revision != 2 || len(versions) != 2 {
		t.Fatal("failed restore changed history", current, versions)
	}
}

func TestAISkillVersionMigrationBackfillsOnlySurvivingRevision(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	r, err := s.GetAISkill(ctx, "alice", "mail-analysis")
	if err != nil {
		t.Fatal(err)
	}
	r.Instructions += "\nPersisted revision"
	r, err = s.SaveAISkill(ctx, r, r.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "DROP TABLE ai_skill_versions"); err != nil {
		t.Fatal(err)
	}
	other, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	versions, err := other.ListAISkillVersions(ctx, "alice", r.ID, 0, 50)
	if err != nil || len(versions) != 1 || versions[0].Skill.Revision != 2 || versions[0].Action != "migration" || !strings.Contains(versions[0].Skill.Instructions, "Persisted revision") {
		t.Fatal(versions, err)
	}
}
