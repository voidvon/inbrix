package mailstore

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"inbrix/models"
)

func TestMessageSearchIndexPreservesSubstringQueries(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	user, err := store.CreateUser(ctx, "search-index", "Search", "password")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, store, user.ID, "me@example.com", true)
	other := testAccount(t, store, user.ID, "other@example.com", false)
	emails := []models.Email{
		{ID: "1", From: "alice@example.com", To: account.Email, Subject: "Quarterly REPORT", Body: `rare-needle 华南项目 a"bc abcXbcdYcde`, Flags: []string{`\Seen`}},
		{ID: "2", From: "bob@example.com", To: account.Email, Subject: "Other", HTML: "<p>Widgets available</p>"},
		{ID: "3", From: "carol@example.com", To: account.Email, Cc: "logistics@example.com", Subject: "Unicode", Body: "Éclair café 飞跃华南项目团队"},
	}
	for i := range emails {
		emails[i].Date = time.Unix(int64(i+1), 0)
		emails[i].BodyCached = true
	}
	for _, target := range []struct{ account, folder string }{
		{account.ID, "INBOX"}, {account.ID, "Archive"}, {other.ID, "INBOX"},
	} {
		if err := store.UpsertMessages(ctx, target.account, target.folder, emails); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		query string
		ids   []string
	}{
		{"are-need", []string{"1"}},
		{"RARE-NEEDLE", []string{"1"}},
		{"absent-needle", nil},
		{"华南项目", []string{"3", "1"}},
		{"华南", []string{"3", "1"}},
		{"Widgets", []string{"2"}},
		{"alice@example.com", []string{"1"}},
		{"cc:logistics", []string{"3"}},
		{"subject:report", []string{"1"}},
		{"subject:Widgets", nil},
		{"from:alice 华南项目", []string{"1"}},
		{"华南项目 -from:alice", []string{"3"}},
		{"-rare-needle", []string{"3", "2"}},
		{"is:unread 华南项目", []string{"3"}},
		{"in:Archive rare-needle", []string{"1"}},
		{"r%needle", []string{"1"}},
		{"rare_needle", []string{"1"}},
		{"abcde", nil}, // All trigrams exist, but the complete substring does not.
		{`a"bc`, []string{"1"}},
		{"café", []string{"3"}},
		{"cafe", nil},
		{"Éclair", []string{"3"}},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			messages, _, err := store.SearchMessages(ctx, account.ID, "INBOX", tc.query, 50)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, message := range messages {
				ids = append(ids, message.ID)
			}
			if !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("matching IDs = %v, want %v", ids, tc.ids)
			}
		})
	}
}

func TestMessageSearchIndexMigrationAndLifecycle(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mail.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	user, err := store.CreateUser(ctx, "old-search-index", "Search", "password")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, store, user.ID, "me@example.com", true)
	// Recreate the pre-index schema to verify upgrading an existing mirror.
	for _, sql := range []string{
		"DROP TRIGGER messages_search_insert", "DROP TRIGGER messages_search_update",
		"DROP TRIGGER messages_search_delete", "DROP TABLE messages_search",
	} {
		if _, err := store.db.ExecContext(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	email := models.Email{ID: "1", Subject: "Original", Body: "old-needle", BodyCached: true}
	if err := store.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{email}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertCount := func(query string, want int) {
		t.Helper()
		messages, _, err := store.SearchMessages(ctx, account.ID, "INBOX", query, 50)
		if err != nil || len(messages) != want {
			t.Fatalf("search %q: %d messages, error=%v; want %d", query, len(messages), err, want)
		}
	}
	assertCount("old-needle", 1)
	// Header/flag refreshes must preserve indexed cached bodies.
	if err := store.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "1", Subject: "Renamed", Flags: []string{`\Seen`}}}); err != nil {
		t.Fatal(err)
	}
	assertCount("old-needle", 1)
	assertCount("Original", 0)
	assertCount("Renamed", 1)
	email.Subject, email.Body = "Renamed", "new-needle"
	if err := store.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{email}); err != nil {
		t.Fatal(err)
	}
	assertCount("old-needle", 0)
	assertCount("new-needle", 1)
	if err := store.DeleteMessage(ctx, account.ID, "INBOX", "1"); err != nil {
		t.Fatal(err)
	}
	assertCount("new-needle", 0)
	// Re-running migration neither duplicates hits nor drops content.
	if err := store.migrateMessageSearch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO messages_search(messages_search, rank) VALUES('integrity-check', 1)`); err != nil {
		t.Fatalf("search index integrity: %v", err)
	}
}
