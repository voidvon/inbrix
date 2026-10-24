package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestAPIKeyCanManageSkillsAndCannotCrossOwners(t *testing.T) {
	f := newAPIKeyFixture(t)
	res, body := keyRequest(t, f, "POST", "/api/settings/ai/skills", f.key.Key, `{"id":"rfq-follow-up","name":"RFQ follow-up","description":"Track open requirements","instructions":"Read the customer's latest request and related emails.","kind":"chat","dependsOn":["email_analysis"],"enabled":true}`, nil)
	if res.StatusCode != 201 {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var created mailstore.AISkillRecord
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.BuiltIn {
		t.Fatal(created)
	}
	res, body = keyRequest(t, f, "GET", "/api/settings/ai/skills/"+created.ID, f.otherKey.Key, "", nil)
	if res.StatusCode != 404 {
		t.Fatalf("foreign read: %d %s", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "PATCH", "/api/settings/ai/skills/"+created.ID, "", `{"instructions":"UPDATED-BY-API-KEY","revision":1}`, map[string]string{"Authorization": "Bearer " + f.key.Key})
	if res.StatusCode != 200 || !strings.Contains(body, "UPDATED-BY-API-KEY") || !strings.Contains(body, `"revision":2`) {
		t.Fatalf("patch: %d %s", res.StatusCode, body)
	}
	stored, err := f.db.GetAISkill(context.Background(), f.user.ID, created.ID)
	if err != nil || stored.Name != created.Name || len(stored.DependsOn) != 1 || stored.Instructions != "UPDATED-BY-API-KEY" {
		t.Fatalf("patch erased fields: %+v %v", stored, err)
	}
	res, body = keyRequest(t, f, "PATCH", "/api/settings/ai/skills/"+created.ID, f.key.Key, `{"instructions":"stale","revision":1}`, nil)
	if res.StatusCode != 409 {
		t.Fatalf("stale update: %d %s", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "DELETE", "/api/settings/ai/skills/"+created.ID, f.otherKey.Key, "", nil)
	if res.StatusCode != 404 {
		t.Fatalf("foreign delete: %d %s", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "DELETE", "/api/settings/ai/skills/"+created.ID+"?revision=2", f.key.Key, "", nil)
	if res.StatusCode != 200 {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "GET", "/api/settings/ai/skills", f.key.Key, "", nil)
	if res.StatusCode != 200 || strings.Contains(body, created.ID) || !strings.Contains(body, `"mailSummarySkillId":"mail-analysis"`) {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	for _, invalid := range []string{`{"name":"Empty","instructions":""}`, `{"id":"bad/slug","name":"Bad","instructions":"text"}`, `{"name":"Bad","instructions":"text","dependsOn":["foreign-skill"]}`, `{"name":"Bad","instructions":"text","unknown":true}`} {
		res, body = keyRequest(t, f, "POST", "/api/settings/ai/skills", f.key.Key, invalid, nil)
		if res.StatusCode < 400 || res.StatusCode >= 500 {
			t.Fatalf("invalid input: %d %s", res.StatusCode, body)
		}
	}
}

func TestAPIKeyCanSwitchSummarySkillAndInUseSkillsAreProtected(t *testing.T) {
	f := newAPIKeyFixture(t)
	res, body := keyRequest(t, f, "DELETE", "/api/settings/ai/skills/mail-analysis", f.key.Key, "", nil)
	if res.StatusCode != 409 {
		t.Fatalf("active summary deleted: %d %s", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "POST", "/api/settings/ai/skills", f.key.Key, `{"id":"new-summary","name":"New summary","kind":"mail_summary","instructions":"Focus on changes and next steps."}`, nil)
	if res.StatusCode != 201 {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "POST", "/api/settings/ai/skills/new-summary/use-for-mail-summary", f.otherKey.Key, `{"revision":1}`, nil)
	if res.StatusCode != 404 {
		t.Fatalf("foreign binding: %d %s", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "POST", "/api/settings/ai/skills/new-summary/use-for-mail-summary", f.key.Key, `{"revision":1}`, nil)
	if res.StatusCode != 200 {
		t.Fatalf("bind: %d %s", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "DELETE", "/api/settings/ai/skills/mail-analysis", f.key.Key, "", nil)
	if res.StatusCode != 200 {
		t.Fatalf("unused default: %d %s", res.StatusCode, body)
	}
	execution, err := f.db.ResolveMailSummarySkill(context.Background(), f.user.ID)
	if err != nil || execution.ID != "new-summary" || !strings.Contains(execution.Instructions, "Focus on changes") {
		t.Fatal(execution, err)
	}
	res, body = keyRequest(t, f, "PATCH", "/api/settings/ai/skills/new-summary", f.key.Key, `{"enabled":false}`, nil)
	if res.StatusCode != 409 {
		t.Fatalf("active summary disabled: %d %s", res.StatusCode, body)
	}
	res, _ = keyRequest(t, f, http.MethodGet, "/api/settings/ai/skills", "", "", nil)
	if res.StatusCode < 400 {
		t.Fatal("unauthenticated skill access accepted")
	}
}

func TestRemovedSkillHistoryEndpointsAreUnavailable(t *testing.T) {
	f := newAPIKeyFixture(t)
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/settings/ai/skills/mail-analysis/versions"},
		{"GET", "/api/settings/ai/skills/mail-analysis/versions/1"},
		{"POST", "/api/settings/ai/skills/mail-analysis/restore"},
	} {
		res, body := keyRequest(t, f, endpoint.method, endpoint.path, f.key.Key, "", nil)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("removed endpoint %s %s: %d %s", endpoint.method, endpoint.path, res.StatusCode, body)
		}
	}
}
