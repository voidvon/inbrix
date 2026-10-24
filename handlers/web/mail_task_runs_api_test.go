package web_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"inbrix/models"
)

func TestAPIKeyMailTaskRunHistoryIsOwnerScopedAndOmitsFencingTokens(t *testing.T) {
	f := newAPIKeyFixture(t)
	ctx := context.Background()
	if err := f.db.UpsertIncomingMessages(ctx, f.account.ID, "INBOX", []models.Email{{ID: "21", From: "buyer@example.com", To: f.account.Email, Body: "PRIVATE-MAIL-BODY", BodyCached: true}}); err != nil {
		t.Fatal(err)
	}
	task, claimed, err := f.db.ClaimMailTask(ctx, f.account.ID, time.Now())
	if err != nil || !claimed {
		t.Fatal(task, claimed, err)
	}
	res, body := keyRequest(t, f, "GET", "/api/settings/ai/mail-tasks/runs?accountEmail="+f.account.Email+"&folder=INBOX&messageId=21&taskType=analysis", f.key.Key, "", nil)
	if res.StatusCode != 200 || !strings.Contains(body, task.RunID) || strings.Contains(body, task.Token) || strings.Contains(body, "PRIVATE-MAIL-BODY") || strings.Contains(body, "lease_token") || strings.Contains(body, "payload") {
		t.Fatal(res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "GET", "/api/settings/ai/mail-tasks/runs?accountEmail="+f.account.Email, f.otherKey.Key, "", nil)
	if res.StatusCode != 404 {
		t.Fatal("cross owner run access", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "GET", "/api/settings/ai/mail-tasks/runs?accountEmail="+f.account.Email, "", "", nil)
	if res.StatusCode != 401 {
		t.Fatal("anonymous access", res.StatusCode, body)
	}
	for _, query := range []string{"&limit=0", "&limit=101", "&messageId=oops", "&taskType=unknown"} {
		res, body = keyRequest(t, f, "GET", "/api/settings/ai/mail-tasks/runs?accountEmail="+f.account.Email+query, f.key.Key, "", nil)
		if res.StatusCode != 400 {
			t.Fatal("bad query accepted", query, res.StatusCode, body)
		}
	}
}
