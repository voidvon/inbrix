package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func finishLinkedTestDraft(t *testing.T, s *Store, account Account, session AIChatSession) string {
	t.Helper()
	ctx := context.Background()
	turn, err := s.BeginAIChatTurn(ctx, account.OwnerID, account.ID, session.ID, session.ID+string(rune('a'+session.Version)), "hash", session.Version, chatUser(t, "draft"), AIBusinessContext{})
	if err != nil {
		t.Fatal(err)
	}
	var id string
	_, err = s.FinishAIChatTurn(ctx, turn, json.RawMessage(`{"role":"assistant","document":{"type":"quotation","title":"Draft","values":{"payment_terms":"Net30"}}}`), func(message string, version int64) (json.RawMessage, error) {
		id = message
		return json.RawMessage(`{}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAIChatSavedDocumentLinksGuardOwnershipAndConcurrentRuns(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	user, err := s.CreateUser(ctx, "document-owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	owner := user.ID
	account := testAccount(t, s, owner, "links@example.com", true)
	session, err := s.CreateAIChatSession(ctx, owner, account.ID, "mail", "peer", "Mail", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	id := finishLinkedTestDraft(t, s, account, session)
	// Upgrade a previous release's state whose struct JSON field order differs
	// from the map-marshaled message payload.
	if _, err = s.db.Exec(`DELETE FROM ai_chat_current_documents WHERE session_id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE ai_chat_session_state SET current_document='{"type":"quotation","title":"Draft","values":{"payment_terms":"Net30"}}' WHERE session_id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateAIChatSessions(ctx); err != nil {
		t.Fatal(err)
	}
	draft := DocumentRecord{ID: id, OwnerID: owner, Name: "Saved", HTML: "Net30", AIOrigin: &AIChatDocumentOrigin{SessionID: session.ID, MessageID: id}}
	foreign := draft
	foreign.OwnerID = "outsider"
	if _, err = s.SaveDocument(ctx, foreign, 0); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("foreign origin accepted: %v", err)
	}
	wrong := draft
	wrong.AIOrigin = &AIChatDocumentOrigin{SessionID: "different-session", MessageID: id}
	if _, err = s.SaveDocument(ctx, wrong, 0); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("cross-session origin accepted: %v", err)
	}
	wrong = draft
	wrong.ID = "other-id"
	if _, err = s.SaveDocument(ctx, wrong, 0); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("unstable card id accepted: %v", err)
	}
	saved, err := s.SaveDocument(ctx, draft, 0)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.GetAIChatSession(ctx, owner, account.ID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 3 || state.CurrentSavedDocument == nil || state.CurrentSavedDocument.Revision != 1 {
		t.Fatalf("link not visible atomically: %+v", state)
	}
	if !json.Valid(state.Messages[1]) {
		t.Fatal("message projection corrupt")
	}
	var card struct {
		Document struct {
			SavedID string `json:"savedDocumentId"`
		} `json:"document"`
	}
	json.Unmarshal(state.Messages[1], &card)
	if card.Document.SavedID != id {
		t.Fatal("saved origin absent after reload")
	}
	turn, err := s.BeginAIChatTurn(ctx, owner, account.ID, session.ID, "in-flight", "hash", state.Version, chatUser(t, "modify"), AIBusinessContext{}, AIChatTurnOptions{Request: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	// A normal library edit carries no chat metadata. The durable link suffices.
	saved.HTML = "Net60"
	saved.AIOrigin = nil
	updated, err := s.SaveDocument(ctx, *saved, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishAIChatTurn(ctx, turn, json.RawMessage(`{"role":"assistant","document":{"title":"Stale draft"}}`), func(string, int64) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }); !errors.Is(err, ErrAIChatConflict) {
		t.Fatalf("stale run committed after human save: %v", err)
	}
	if err = s.SaveAIChatTurnMemory(ctx, turn, AIChatContext{HistorySummary: "stale"}); !errors.Is(err, ErrAIChatConflict) {
		t.Fatalf("stale memory committed: %v", err)
	}
	if _, err = s.GetAIChatRetryInput(ctx, owner, account.ID, session.ID, "in-flight"); !errors.Is(err, ErrAIChatConflict) {
		t.Fatalf("outdated execution retry allowed: %v", err)
	}
	state, err = s.GetAIChatSession(ctx, owner, account.ID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSavedDocument.HTML != "Net60" || state.CurrentSavedDocument.Revision != 2 || state.LastRun.Retryable {
		t.Fatal("latest saved content/version not authoritative")
	}
	// A newer generated card replaces the association for this chat, not the old
	// library document. Later saves of the old card must not change that draft.
	newID := finishLinkedTestDraft(t, s, account, state)
	before, _ := s.GetAIChatSession(ctx, owner, account.ID, session.ID)
	updated.HTML = "Net90"
	if _, err = s.SaveDocument(ctx, *updated, updated.Revision); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetAIChatSession(ctx, owner, account.ID, session.ID)
	if before.Version != after.Version || after.CurrentSavedDocument != nil {
		t.Fatalf("saving old card changed newer draft: new=%s %+v", newID, after)
	}
}

func TestAIChatDeletedDocumentLinkAndFreshDraftDetach(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	user, err := s.CreateUser(ctx, "document-owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	owner := user.ID
	account := testAccount(t, s, owner, "detach@example.com", true)
	session, err := s.CreateAIChatSession(ctx, owner, account.ID, "document", "", "Docs", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	id := finishLinkedTestDraft(t, s, account, session)
	draft := DocumentRecord{ID: id, OwnerID: owner, Name: "Saved", HTML: "Net30", AIOrigin: &AIChatDocumentOrigin{SessionID: session.ID, MessageID: id}}
	if _, err = s.SaveDocument(ctx, draft, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteDocumentRevision(ctx, owner, id, 1); err != nil {
		t.Fatal(err)
	}
	state, err := s.GetAIChatSession(ctx, owner, account.ID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.CurrentSavedDocumentMissing || state.CurrentSavedDocument != nil {
		t.Fatal("deleted document not reported")
	}
	if _, err = s.SaveDocument(ctx, draft, 0); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("old card recreated deleted document: %v", err)
	}
	turn, err := s.BeginAIChatTurn(ctx, owner, account.ID, session.ID, "fresh", "hash", state.Version, chatUser(t, "new quote"), AIBusinessContext{}, AIChatTurnOptions{CurrentDocument: json.RawMessage(`{"newDraft":true,"type":"quotation"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FailAIChatTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	// Re-running migration must not reattach an older card over the fresh reset.
	if err = s.migrateAIChatSessions(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = s.GetAIChatSession(ctx, owner, account.ID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSavedDocumentMissing || state.CurrentSavedDocument != nil || string(state.CurrentDocument) != `{"newDraft":true,"type":"quotation"}` {
		t.Fatal("fresh draft reattached to deleted document")
	}
	if err = s.DeleteAIChatSession(ctx, owner, account.ID, session.ID, state.Version, true); err != nil {
		t.Fatal(err)
	}
	var links int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM ai_chat_document_links WHERE message_id=?`, id).Scan(&links); err != nil || links != 0 {
		t.Fatalf("clear retained origin link: %d %v", links, err)
	}
}
