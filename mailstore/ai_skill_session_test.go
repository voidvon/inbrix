package mailstore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAISkillSessionLoadsOnDemandAndFreezesDependencies(t *testing.T) {
	records := []AISkillRecord{
		{ID: "base", Name: "Base", Kind: AISkillChat, Instructions: "BASE-OLD", Enabled: true, Revision: 1},
		{ID: "follow-up", Name: "Follow up", Description: "Track questions", Kind: AISkillChat, Instructions: "FOLLOW-OLD", DependsOn: []string{"base"}, Enabled: true, Revision: 2},
		{ID: "unrelated", Name: "Unrelated", Kind: AISkillChat, Instructions: "UNRELATED-BODY", Enabled: true, Revision: 1},
		{ID: "disabled", Name: "Disabled", Kind: AISkillChat, Instructions: "DISABLED-BODY", Enabled: false},
	}
	session := NewAISkillSession(records)
	prompt, err := session.Prompt("")
	if err != nil || !strings.Contains(prompt, "follow-up") || strings.Contains(prompt, "FOLLOW-OLD") || strings.Contains(prompt, "UNRELATED-BODY") || strings.Contains(prompt, "disabled") {
		t.Fatal(prompt, err)
	}
	records[0].Instructions = "BASE-NEW"
	records[1].DependsOn[0] = "unrelated"
	result, err := session.Load("follow-up", 0, 2000)
	raw, _ := json.Marshal(result)
	if err != nil || !strings.Contains(string(raw), "BASE-OLD") || strings.Contains(string(raw), "BASE-NEW") || strings.Contains(string(raw), "UNRELATED-BODY") {
		t.Fatal(string(raw), err)
	}
	snapshots := session.Snapshots()
	if len(snapshots) != 1 || len(snapshots[0].Versions) != 2 || snapshots[0].Versions[0].Revision != 1 || snapshots[0].Versions[1].Revision != 2 {
		t.Fatal(snapshots)
	}
	if _, err := session.Load("disabled", 0, 2000); err == nil {
		t.Fatal("disabled loaded")
	}
	if _, err := session.Load("foreign", 0, 2000); err == nil {
		t.Fatal("missing loaded")
	}
	selected := NewAISkillSession(records)
	prompt, err = selected.Prompt("follow-up")
	if err != nil || !strings.Contains(prompt, "FOLLOW-OLD") || strings.Contains(prompt, "BASE-NEW") {
		t.Fatal(prompt, err)
	}
}

func TestAISkillSessionPagesUnicodeAndEnforcesCumulativeBudget(t *testing.T) {
	records := []AISkillRecord{}
	for _, id := range []string{"first", "second", "third", "fourth"} {
		records = append(records, AISkillRecord{ID: id, Name: id, Kind: AISkillChat, Instructions: strings.Repeat("规格\"\n", 1800), Enabled: true, Revision: 1})
	}
	session := NewAISkillSession(records)
	var assembled strings.Builder
	offset := 0
	for {
		result, err := session.Load("first", offset, 2000)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(result)
		if len(raw) > 20000 {
			t.Fatal("tool budget exceeded", len(raw))
		}
		part := result.(map[string]any)
		assembled.WriteString(part["instructions"].(string))
		if !part["hasMore"].(bool) {
			break
		}
		offset = part["nextOffset"].(int)
	}
	if !strings.Contains(assembled.String(), records[0].Instructions) {
		t.Fatal("lost instruction characters")
	}
	if _, err := session.Load("second", 0, 2000); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Load("third", 0, 2000); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Load("fourth", 0, 2000); err == nil {
		t.Fatal("unbounded cumulative loads")
	}
	if _, err := session.Load("first", 0, 2001); err == nil {
		t.Fatal("invalid page accepted")
	}
}

func TestAISkillCatalogStaysBoundedWithManyLargeWorkflows(t *testing.T) {
	records := []AISkillRecord{}
	for i := range 64 {
		id := "flow-" + strings.Repeat("x", 55) + string(rune('a'+i/26)) + string(rune('a'+i%26))
		records = append(records, AISkillRecord{ID: id, Name: strings.Repeat("名称", 50), Description: strings.Repeat("\"目录\n", 100), Kind: AISkillChat, Instructions: strings.Repeat("PRIVATE-WORKFLOW", 900), Enabled: true, Revision: 1})
	}
	session := NewAISkillSession(records)
	prompt, err := session.Prompt("")
	if err != nil || len(prompt) > 13500 || strings.Contains(prompt, "PRIVATE-WORKFLOW") {
		t.Fatal("catalog exceeds independent budget", len(prompt), err)
	}
	for _, r := range records {
		if !strings.Contains(prompt, r.ID) {
			t.Fatal("lost skill discovery", r.ID)
		}
	}
	if len(session.Snapshots()) != 0 {
		t.Fatal("metadata catalog loaded instructions")
	}
}

func TestAISkillRegistryAllowsIndependentFlowsBeyondOldAggregateBudget(t *testing.T) {
	s := openTestStore(t)
	for _, id := range []string{"first-large", "second-large", "third-large"} {
		_, err := s.SaveAISkill(context.Background(), AISkillRecord{OwnerID: "alice", ID: id, Name: id, Kind: AISkillChat, Instructions: strings.Repeat("x", 14000), Enabled: true}, 0)
		if err != nil {
			t.Fatal("independent workflows should load separately", err)
		}
	}
}
