package mailstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestAISkillsPersistEditsDeletionsAndOwnerIsolation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	skills, err := s.ListAISkills(ctx, "alice")
	if err != nil || len(skills) != 4 {
		t.Fatalf("defaults=%+v err=%v", skills, err)
	}
	created, err := s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: "follow-up", Name: "跟进", Kind: AISkillChat, Instructions: "Track the actual customer's outstanding requests.", Enabled: true}, 0)
	if err != nil || created.Revision != 1 {
		t.Fatal(created, err)
	}
	if _, err := s.GetAISkill(ctx, "bob", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner read: %v", err)
	}
	if err := s.DeleteAISkill(ctx, "bob", created.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner delete: %v", err)
	}
	created.Instructions = "Updated workflow"
	updated, err := s.SaveAISkill(ctx, created, created.Revision)
	if err != nil || updated.Revision != 2 {
		t.Fatal(updated, err)
	}
	if _, err := s.SaveAISkill(ctx, created, created.Revision); !errors.Is(err, ErrAISkillConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	if err := s.DeleteAISkill(ctx, "alice", created.ID, 1); !errors.Is(err, ErrAISkillConflict) {
		t.Fatalf("stale delete accepted: %v", err)
	}
	if err := s.DeleteAISkill(ctx, "alice", created.ID, updated.Revision); err != nil {
		t.Fatal(err)
	}
	// Built-in records are editable and deletable, and do not reappear on boot.
	if err := s.DeleteAISkill(ctx, "alice", "contract", 1); err != nil {
		t.Fatal(err)
	}
	other, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.GetAISkill(ctx, "alice", "contract"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted default resurrected: %v", err)
	}
	bob, err := other.GetAISkill(ctx, "bob", "contract")
	if err != nil || bob.Revision != 1 {
		t.Fatalf("other owner's default changed: %+v %v", bob, err)
	}
}

func TestAISkillDependenciesBindingAndConcurrentUpdates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.DeleteAISkill(ctx, "alice", "email_analysis", 1); !errors.Is(err, ErrAISkillInUse) {
		t.Fatalf("dangling dependency accepted: %v", err)
	}
	base, err := s.GetAISkill(ctx, "alice", "email_analysis")
	if err != nil {
		t.Fatal(err)
	}
	base.Enabled = false
	if _, err := s.SaveAISkill(ctx, base, base.Revision); !errors.Is(err, ErrAISkillInUse) {
		t.Fatalf("disabled dependency accepted: %v", err)
	}
	a, err := s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: "first", Name: "First", Instructions: "First workflow", Kind: AISkillChat, Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: "second", Name: "Second", Instructions: "Second workflow", Kind: AISkillChat, DependsOn: []string{a.ID}, Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	a.DependsOn = []string{b.ID}
	if _, err := s.SaveAISkill(ctx, a, a.Revision); !errors.Is(err, ErrInvalidAISkill) {
		t.Fatalf("cycle accepted: %v", err)
	}
	if err := s.DeleteAISkill(ctx, "alice", "mail-analysis", 1); !errors.Is(err, ErrAISkillInUse) {
		t.Fatalf("active summary deleted: %v", err)
	}
	custom, err := s.SaveAISkill(ctx, AISkillRecord{OwnerID: "alice", ID: "custom-summary", Name: "Custom", Instructions: "Summarize changes and open questions.", Kind: AISkillMailSummary, Enabled: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMailSummarySkill(ctx, "alice", custom.ID, custom.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAISkill(ctx, "alice", "mail-analysis", 1); err != nil {
		t.Fatal(err)
	}
	resolved, err := s.ResolveMailSummarySkill(ctx, "alice")
	if err != nil || resolved.ID != custom.ID || !strings.Contains(resolved.Instructions, custom.Instructions) {
		t.Fatal(resolved, err)
	}
	if err := s.SetMailSummarySkill(ctx, "bob", custom.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign summary bound: %v", err)
	}
	// Both writers start from the same revision. Exactly one may win.
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() { defer group.Done(); _, err := s.SaveAISkill(ctx, custom, custom.Revision); results <- err }()
	}
	group.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrAISkillConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}

func TestManagedMailSkillUpdatesPromptAndInvalidatesCache(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	client := &analysisScriptClient{reply: func(_ int, _ string) string { return standaloneAnalysisOutput }}
	first, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := s.GetAISkill(ctx, account.OwnerID, "mail-analysis")
	if err != nil {
		t.Fatal(err)
	}
	skill.Instructions += "\nMANAGED-SKILL-UPDATED: emphasize exact requested quantities."
	skill, err = s.SaveAISkill(ctx, skill, skill.Revision)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil || !cached.Cached || !cached.Stale || len(client.inputs) != 1 {
		t.Fatalf("skill update did not stale cache: %+v %v", cached, err)
	}
	cfg, err := resolveMailSummaryConfig(ctx, s, key, account)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mailSummaryInstructions(cfg.agent, cfg.skill.Instructions), "MANAGED-SKILL-UPDATED") {
		t.Fatal("runtime ignored saved instructions")
	}
	second, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, true)
	if err != nil || second.Record.Analysis.Skill != "mail-analysis/v2" || second.Record.ConfigHash == first.Record.ConfigHash {
		t.Fatalf("regeneration did not adopt revision: %+v %v", second, err)
	}
}
