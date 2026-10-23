package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestAIChatStatePersistsWithLeaseAndClearsWithHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	account := testAccount(t, s, "owner", "state@example.com", true)
	session, err := s.CreateAIChatSession(ctx, "owner", account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	draft := json.RawMessage(`{"type":"quotation","title":"Draft","values":{"seller_company":"Seller"}}`)
	turn, err := s.BeginAIChatTurn(ctx, "owner", account.ID, session.ID, "first", "hash", 0, chatUser(t, "new quote"), AIBusinessContext{}, AIChatTurnOptions{Request: json.RawMessage(`{}`), CurrentDocument: draft})
	if err != nil {
		t.Fatal(err)
	}
	memory := AIChatContext{HistoryHash: "prefix", HistoryCount: 1, HistorySummary: "user constraints"}
	if err = s.SaveAIChatTurnMemory(ctx, turn, memory); err != nil {
		t.Fatal(err)
	}
	if err = s.FailAIChatTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAIChatTurnMemory(ctx, turn, AIChatContext{HistorySummary: "stale"}); !errors.Is(err, ErrAIChatConflict) {
		t.Fatalf("expired run updated memory: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetAIChatSession(ctx, "owner", account.ID, session.ID, AIChatMessagePage{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Memory != memory || string(got.CurrentDocument) != string(draft) {
		t.Fatalf("state lost after restart: %+v", got)
	}
	if _, err = s.BeginAIChatTurn(ctx, "owner", account.ID, session.ID, "stale", "hash", 0, chatUser(t, "stale"), AIBusinessContext{}, AIChatTurnOptions{CurrentDocument: json.RawMessage(`{"title":"wrong"}`)}); !errors.Is(err, ErrAIChatConflict) {
		t.Fatalf("stale turn accepted: %v", err)
	}
	if err = s.DeleteAIChatSession(ctx, "owner", account.ID, session.ID, got.Version, true); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetAIChatSession(ctx, "owner", account.ID, session.ID)
	if err != nil || len(got.CurrentDocument) != 0 || got.Memory.HistorySummary != "" {
		t.Fatalf("clear retained derived state: %+v %v", got, err)
	}
}

func TestAIChatStateBackfillsLatestLegacyDocument(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	account := testAccount(t, s, "owner", "backfill@example.com", true)
	session, err := s.CreateAIChatSession(ctx, "owner", account.ID, "document", "", "Old", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	// Emulate a pre-upgrade session whose latest draft is outside the UI page.
	for i, raw := range []string{`{"role":"assistant","document":{"title":"older"}}`, `{"role":"assistant","document":{"title":"latest"}}`, `{"role":"assistant","content":"follow-up"}`} {
		_, err = s.db.Exec(`INSERT INTO ai_chat_messages(id,session_id,position,payload) VALUES(?,?,?,?)`, session.ID+string(rune('a'+i)), session.ID, i, raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.db.Exec(`DELETE FROM ai_chat_session_state WHERE session_id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateAIChatSessions(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAIChatSession(ctx, "owner", account.ID, session.ID, AIChatMessagePage{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.CurrentDocument) != `{"title":"latest"}` || len(got.Messages) != 1 {
		t.Fatalf("legacy draft migration failed: %+v", got)
	}
}
