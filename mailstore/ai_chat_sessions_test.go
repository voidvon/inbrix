package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func chatUser(t *testing.T, text string, attachments ...AIChatAttachment) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"role": "user", "content": text, "attachments": attachments})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAIChatTurnAtomicRetryAndOwnership(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a := testAccount(t, s, "owner", "a@example.com", true)
	b := testAccount(t, s, "owner", "b@example.com", false)
	session, err := s.CreateAIChatSession(ctx, "owner", a.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	image, err := s.CreateAIChatAttachment(ctx, "owner", a.ID, "img", "image/png", []byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	user := chatUser(t, "read image", image)
	if _, err = s.BeginAIChatTurn(ctx, "stranger", a.ID, session.ID, "req", "hash", 0, user, AIBusinessContext{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign owner: %v", err)
	}
	if _, err = s.BeginAIChatTurn(ctx, "owner", b.ID, session.ID, "req", "hash", 0, user, AIBusinessContext{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign mailbox: %v", err)
	}
	otherImage, _ := s.CreateAIChatAttachment(ctx, "owner", b.ID, "foreign", "image/png", []byte("image"))
	if _, err = s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, "bad", "bad", 0, chatUser(t, "foreign", otherImage), AIBusinessContext{}); err == nil {
		t.Fatal("adopted foreign image")
	}
	if session.BusinessContext.Brands == nil {
		t.Fatal("new sessions must return [] brands, not null")
	}
	before, _ := s.GetAIChatSession(ctx, "owner", a.ID, session.ID)
	if before.Version != 0 || len(before.Messages) != 0 {
		t.Fatal("failed adoption was not rolled back")
	}
	turn, err := s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, "req", "hash", 0, user, AIBusinessContext{})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetAIChatSession(ctx, "owner", a.ID, session.ID)
	if len(after.Messages) != 1 || after.Version != 1 || after.LastRun.Status != "pending" {
		t.Fatalf("user not persisted: %+v", after)
	}
	if err = s.DeleteAIChatAttachment(ctx, "owner", a.ID, image.ID); !errors.Is(err, ErrAIChatAttachmentInUse) {
		t.Fatalf("deleted sent image: %v", err)
	}
	if _, err = s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, "req", "hash", 0, user, AIBusinessContext{}); !errors.Is(err, ErrAIChatConflict) {
		t.Fatalf("pending retry: %v", err)
	}
	if err = s.UpdateAIChatBusiness(ctx, "owner", a.ID, session.ID, 1, AIBusinessContext{}); !errors.Is(err, ErrAIChatConflict) {
		t.Fatalf("changed pending selection: %v", err)
	}
	if err = s.DeleteAIChatSession(ctx, "owner", a.ID, session.ID, 1, false); !errors.Is(err, ErrAIChatConflict) {
		t.Fatalf("deleted pending session: %v", err)
	}
	if err = s.FailAIChatTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	retry, err := s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, "req", "hash", 0, user, AIBusinessContext{})
	if err != nil {
		t.Fatal(err)
	}
	if retry.UserMessageID != turn.UserMessageID || retry.Token == turn.Token {
		t.Fatal("retry did not reuse user with a fresh lease token")
	}
	finish := func(id string, version int64) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"content": "done", "assistantMessageId": id, "version": version})
	}
	if _, err = s.FinishAIChatTurn(ctx, turn, json.RawMessage(`{"role":"assistant","content":"stale"}`), finish); !errors.Is(err, ErrAIChatConflict) {
		t.Fatal("stale execution committed", err)
	}
	response, err := s.FinishAIChatTurn(ctx, retry, json.RawMessage(`{"role":"assistant","content":"done"}`), finish)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, "req", "hash", 0, user, AIBusinessContext{})
	if err != nil || string(replay.Replay) != string(response) {
		t.Fatalf("lost completed response: %s %v", replay.Replay, err)
	}
	if _, err = s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, "req", "different", 2, user, AIBusinessContext{}); !errors.Is(err, ErrAIChatConflict) {
		t.Fatal("reused ID with different input", err)
	}
	after, _ = s.GetAIChatSession(ctx, "owner", a.ID, session.ID)
	if len(after.Messages) != 2 || after.Version != 2 {
		t.Fatalf("duplicate messages: %+v", after)
	}
	if err = s.DeleteAIChatSession(ctx, "owner", a.ID, session.ID, 0, false); !errors.Is(err, ErrAIChatConflict) {
		t.Fatal("stale delete", err)
	}
	if err = s.DeleteAIChatSession(ctx, "owner", a.ID, session.ID, 2, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetAIChatAttachment(ctx, "owner", a.ID, image.ID); err != nil {
		t.Fatal("grace period was not retained", err)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE ai_chat_attachment_lifecycle SET expires_at=0`)
	if err = s.GCAIChatAttachments(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetAIChatAttachment(ctx, "owner", a.ID, image.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("released image not collected", err)
	}
}

func TestAIChatConcurrentWritersAndLeaseRecovery(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a := testAccount(t, s, "owner", "a@example.com", true)
	session, _ := s.CreateAIChatSession(ctx, "owner", a.ID, "document", "", "New", "quotation")
	user := chatUser(t, "hello")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	turns := make(chan AIChatTurn, 2)
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			turn, err := s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, id, id, 0, user, AIBusinessContext{})
			results <- err
			if err == nil {
				turns <- turn
			}
		}(id)
	}
	wg.Wait()
	close(results)
	won, conflicted := 0, 0
	for err := range results {
		if err == nil {
			won++
		} else if errors.Is(err, ErrAIChatConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if won != 1 || conflicted != 1 {
		t.Fatal("concurrent writers were not serialized")
	}
	turn := <-turns
	_, _ = s.db.ExecContext(ctx, `UPDATE ai_chat_runs SET lease_until=?`, time.Now().Add(-time.Minute).Unix())
	recovered, err := s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, turn.RequestID, turn.RequestID, 0, user, AIBusinessContext{})
	if err != nil {
		t.Fatal("expired run did not recover", err)
	}
	if recovered.UserMessageID != turn.UserMessageID {
		t.Fatal("recovery duplicated user")
	}
}

func TestAIChatLegacyImportAndAttachmentGC(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a := testAccount(t, s, "owner", "a@example.com", true)
	// Pre-lifecycle upload simulates images referenced only by an old browser.
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_chat_attachments VALUES('legacy',?,'old.png','image/png',3,x'010203')`, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.GCAIChatAttachments(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetAIChatAttachment(ctx, "owner", a.ID, "legacy"); err != nil {
		t.Fatal("legacy upload collected", err)
	}
	raw := `[null,42,{"id":"old","role":"user","content":"historic","attachments":[{"id":"legacy"}]}]`
	if err = s.SaveConversationAIChat(ctx, a.ID, "conv", raw); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAIBusinessContext(ctx, "owner", a.ID, "conv", AIBusinessContext{SkillID: "quotation"}); err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateAIChatSession(ctx, "owner", a.ID, "mail", "conv", "Mail", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := s.CreateAIChatSession(ctx, "owner", a.ID, "mail", "conv", "Mail", "quotation")
	if err != nil || twice.ID != session.ID || len(twice.Messages) != 1 || twice.BusinessContext.SkillID != "quotation" {
		t.Fatalf("legacy import: %+v %v", twice, err)
	}
	if err = s.SaveConversationAIChat(ctx, a.ID, "conv", "[]"); !errors.Is(err, ErrAIChatConflict) {
		t.Fatal("old client overwrote session", err)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE ai_chat_attachment_lifecycle SET expires_at=0`)
	if err = s.GCAIChatAttachments(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetAIChatAttachment(ctx, "owner", a.ID, "legacy"); err != nil {
		t.Fatal("referenced image collected", err)
	}
	if err = s.DeleteAIChatSession(ctx, "owner", a.ID, session.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	blank, _ := s.GetAIChatSession(ctx, "owner", a.ID, session.ID)
	if len(blank.Messages) != 0 || blank.BusinessContext.SkillID != "quotation" {
		t.Fatal("clear removed preferences or retained messages")
	}
}

func TestAIChatSharedReferencesSurviveReopenAndMailboxDeletion(t *testing.T) {
	path := t.TempDir() + "/chat.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a := testAccount(t, s, "owner", "a@example.com", true)
	image, err := s.CreateAIChatAttachment(ctx, "owner", a.ID, "shared.png", "image/png", []byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, key := range []string{"one", "two"} {
		session, e := s.CreateAIChatSession(ctx, "owner", a.ID, "document", "", key, "quotation")
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, session.ID)
		turn, e := s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, key, key, 0, chatUser(t, "image", image), AIBusinessContext{})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.FailAIChatTurn(ctx, turn); e != nil {
			t.Fatal(e)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	saved, err := s.GetAIChatSession(ctx, "owner", a.ID, ids[1])
	if err != nil || len(saved.Messages) != 1 || saved.Version != 1 {
		t.Fatalf("session did not survive reopen: %+v %v", saved, err)
	}
	if err = s.DeleteAIChatSession(ctx, "owner", a.ID, ids[0], 1, false); err != nil {
		t.Fatal(err)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE ai_chat_attachment_lifecycle SET expires_at=0`)
	if err = s.GCAIChatAttachments(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetAIChatAttachment(ctx, "owner", a.ID, image.ID); err != nil {
		t.Fatal("other session's reference was lost", err)
	}
	if err = s.DeleteAccount(ctx, "owner", a.ID); err != nil {
		t.Fatal("mailbox cascade failed", err)
	}
	if _, err = s.GetAIChatAttachment(ctx, "owner", a.ID, image.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("mailbox data retained", err)
	}
}

func TestAIChatSummaryAndMessagePagination(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a := testAccount(t, s, "owner", "a@example.com", true)
	ids := map[string]bool{}
	for i := 0; i < 5; i++ {
		session, err := s.CreateAIChatSession(ctx, "owner", a.ID, "document", "", "Page", "quotation")
		if err != nil {
			t.Fatal(err)
		}
		ids[session.ID] = true
	}
	// Equal timestamps must still yield every session exactly once.
	if _, err := s.db.ExecContext(ctx, `UPDATE ai_chat_sessions SET updated_at=1000`); err != nil {
		t.Fatal(err)
	}
	cursor := ""
	seen := map[string]bool{}
	for {
		page, err := s.ListAIChatSessionPage(ctx, "owner", a.ID, 2, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, session := range page.Sessions {
			if seen[session.ID] || len(session.Messages) > 0 {
				t.Fatal("duplicate or non-summary list row")
			}
			seen[session.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != len(ids) {
		t.Fatal("cursor skipped tied timestamps")
	}
	foreign, err := s.ListAIChatSessionPage(ctx, "stranger", a.ID, 2, "")
	if err != nil || len(foreign.Sessions) != 0 {
		t.Fatal("list leaked owner")
	}
	var id string
	for key := range ids {
		id = key
		break
	}
	for i := 0; i < 5; i++ {
		turn, err := s.BeginAIChatTurn(ctx, "owner", a.ID, id, string(rune('a'+i)), "hash", int64(i), chatUser(t, string(rune('a'+i))), AIBusinessContext{})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.FailAIChatTurn(ctx, turn); err != nil {
			t.Fatal(err)
		}
	}
	var before *int64
	var all []json.RawMessage
	for {
		page, err := s.GetAIChatSession(ctx, "owner", a.ID, id, AIChatMessagePage{Limit: 2, Before: before})
		if err != nil {
			t.Fatal(err)
		}
		all = append(page.Messages, all...)
		before = page.NextBefore
		if !page.HasMoreMessages {
			break
		}
	}
	full, err := s.GetAIChatSession(ctx, "owner", a.ID, id)
	if err != nil || len(all) != len(full.Messages) {
		t.Fatal("page lost messages")
	}
	for i := range all {
		if string(all[i]) != string(full.Messages[i]) {
			t.Fatal("page reordered messages")
		}
	}
}

func TestAIChatRetrySnapshotSurvivesRestartAndHonorsVersion(t *testing.T) {
	path := t.TempDir() + "/retry.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a := testAccount(t, s, "owner", "a@example.com", true)
	session, err := s.CreateAIChatSession(ctx, "owner", a.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := json.RawMessage(`{"messages":[{"role":"user","content":"original"}],"activeDocument":{"title":"Original"}}`)
	turn, err := s.BeginAIChatTurn(ctx, "owner", a.ID, session.ID, "run", "hash", 0, chatUser(t, "original"), AIBusinessContext{}, AIChatTurnOptions{Request: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FailAIChatTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loaded, err := s.GetAIChatRetryInput(ctx, "owner", a.ID, session.ID, "run")
	if err != nil || string(loaded) != string(snapshot) {
		t.Fatal("snapshot lost across restart", err)
	}
	if _, err = s.GetAIChatRetryInput(ctx, "stranger", a.ID, session.ID, "run"); !errors.Is(err, ErrNotFound) {
		t.Fatal("snapshot leaked owner", err)
	}
	if err = s.UpdateAIChatBusiness(ctx, "owner", a.ID, session.ID, 1, AIBusinessContext{}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetAIChatRetryInput(ctx, "owner", a.ID, session.ID, "run"); !errors.Is(err, ErrAIChatConflict) {
		t.Fatal("retry ignored changed business context", err)
	}
	page, err := s.GetAIChatSession(ctx, "owner", a.ID, session.ID)
	if err != nil || page.LastRun.Retryable {
		t.Fatal("stale run advertised as retryable")
	}
}
