package mailstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"inbrix/models"
)

func TestMailAnalysisRetrievalInvalidatesLateHistoryButNotFutureFlagsOrOtherOwners(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	message.Date = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if err := s.UpsertMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	client := &analysisScriptClient{reply: func(_ int, _ string) string { return standaloneAnalysisOutput }}
	first, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Record.Analysis.Retrieval.Version != 1 {
		t.Fatal("retrieval not recorded")
	}
	check := func(stale bool) {
		t.Helper()
		result, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
		if err != nil || !result.Cached || result.Stale != stale || len(client.inputs) != 1 {
			t.Fatal(result, err, len(client.inputs))
		}
	}
	message.Flags = []string{"\\Seen"}
	if err := s.UpsertMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	check(false)
	future := message
	future.ID = "43"
	future.Date = message.Date.Add(time.Hour)
	if err := s.UpsertMessages(ctx, account.ID, future.Folder, []models.Email{future}); err != nil {
		t.Fatal(err)
	}
	check(false)
	// A same-folder higher UID is outside the boundary even if its Date is old.
	future.Date = message.Date.Add(-time.Hour)
	if err := s.UpsertMessages(ctx, account.ID, future.Folder, []models.Email{future}); err != nil {
		t.Fatal(err)
	}
	check(false)
	other := testAccount(t, s, account.OwnerID, "other@example.com", false)
	old := message
	old.ID = "12"
	old.Date = message.Date.Add(-time.Hour)
	if err := s.UpsertMessages(ctx, other.ID, old.Folder, []models.Email{old}); err != nil {
		t.Fatal(err)
	}
	check(false)
	unrelated := old
	unrelated.ID = "13"
	unrelated.From = "stranger@example.com"
	if err := s.UpsertMessages(ctx, account.ID, unrelated.Folder, []models.Email{unrelated}); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := s.UpsertMessages(ctx, account.ID, old.Folder, []models.Email{old}); err != nil {
		t.Fatal(err)
	}
	check(true)
}

func TestMailAnalysisRetrievalTracksMatchesOutsideReturnedSearchPage(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	message.Date = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	firstHit := models.Email{ID: "11", Folder: "Sent", From: "supplier@example.com", To: "unrelated@example.com", Date: message.Date.Add(-time.Hour), Subject: "SPEC-Z9", Body: "Catalog entry", BodyCached: true}
	for _, mail := range []models.Email{message, firstHit} {
		if err := s.UpsertMessages(ctx, account.ID, mail.Folder, []models.Email{mail}); err != nil {
			t.Fatal(err)
		}
	}
	client := &analysisScriptClient{reply: func(turn int, _ string) string {
		if turn == 1 {
			return `{"name":"search_emails","arguments":{"scope":"mailbox","query":"SPEC-Z9","limit":1}}`
		}
		return standaloneAnalysisOutput
	}}
	result, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Record.Analysis.Retrieval.Queries) != 1 {
		t.Fatal("search dependency missing", result.Record.Analysis)
	}
	late := firstHit
	late.ID = "10"
	late.Date = firstHit.Date.Add(-time.Hour)
	if err := s.UpsertMessages(ctx, account.ID, late.Folder, []models.Email{late}); err != nil {
		t.Fatal(err)
	}
	result, err = GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil || !result.Cached || !result.Stale || len(client.inputs) != 2 {
		t.Fatal(result, err, len(client.inputs))
	}
}

func TestMailAnalysisRejectsRetrievalChangesDuringRun(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	client := &analysisScriptClient{reply: func(_ int, _ string) string {
		late := message
		late.ID = "11"
		late.Body = "Late synchronized history"
		if err := s.UpsertMessages(ctx, account.ID, late.Folder, []models.Email{late}); err != nil {
			t.Fatal(err)
		}
		return standaloneAnalysisOutput
	}}
	_, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err == nil || !strings.Contains(err.Error(), "scope changed") {
		t.Fatal("mixed snapshot accepted", err)
	}
	record, err := s.GetMessageSummary(ctx, MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID})
	if err != nil || record.Status != "failed" {
		t.Fatal(record, err)
	}
}
