package mailstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func newDraftFixture(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	user, err := store.CreateUser(ctx, "drafter", "Drafter", "hash")
	if err != nil {
		t.Fatal(err)
	}
	return store, user.ID
}

// A conversation owns exactly one draft: clicking Reply again must reopen the
// draft that is already there, not start a second one.
func TestSaveDraftReusesConversationDraft(t *testing.T) {
	ctx := context.Background()
	store, owner := newDraftFixture(t)

	first, err := store.SaveDraft(ctx, owner, DraftInput{
		AccountEmail:   "me@example.com",
		ConversationID: "conv-1",
		Mode:           DraftModeReply,
		To:             "peer@example.com",
		Subject:        "Re: quote",
		BodyHTML:       "<p>first</p>",
		BodyText:       "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !validDraftID(first.ID) {
		t.Fatalf("minted id = %q", first.ID)
	}

	// A second Reply click, this time with the richer reply-all recipient set.
	second, err := store.SaveDraft(ctx, owner, DraftInput{
		AccountEmail:   "me@example.com",
		ConversationID: "conv-1",
		Mode:           DraftModeReplyAll,
		To:             "peer@example.com",
		Cc:             "boss@example.com",
		Subject:        "Re: quote",
		BodyHTML:       "<p>second</p>",
		BodyText:       "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("conversation draft id changed: %q -> %q", first.ID, second.ID)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at moved: %v -> %v", first.CreatedAt, second.CreatedAt)
	}
	if second.BodyHTML != "<p>second</p>" || second.Cc != "boss@example.com" {
		t.Fatalf("draft not updated: %+v", second)
	}
	if second.Mode != DraftModeReplyAll {
		t.Fatalf("mode = %q", second.Mode)
	}

	drafts, err := store.ListDrafts(ctx, owner, "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 {
		t.Fatalf("drafts for conversation = %d, want 1", len(drafts))
	}
}

// A brand new message has no conversation, so the client's handle is the
// identity and must survive every autosave.
func TestSaveDraftNewMessageKeepsClientHandle(t *testing.T) {
	ctx := context.Background()
	store, owner := newDraftFixture(t)

	created, err := store.SaveDraft(ctx, owner, DraftInput{
		AccountEmail: "me@example.com",
		Mode:         DraftModeNew,
		To:           "peer@example.com",
		Subject:      "hello",
		BodyHTML:     "<p>hi</p>",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.SaveDraft(ctx, owner, DraftInput{
		ID:           created.ID,
		AccountEmail: "me@example.com",
		Mode:         DraftModeNew,
		To:           "peer@example.com",
		Subject:      "hello",
		BodyHTML:     "<p>hi again</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID {
		t.Fatalf("handle changed: %q -> %q", created.ID, updated.ID)
	}
	if updated.BodyHTML != "<p>hi again</p>" {
		t.Fatalf("body = %q", updated.BodyHTML)
	}

	// A second new message without a handle is a separate draft.
	other, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "me@example.com", Subject: "another"})
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == created.ID {
		t.Fatal("a fresh new message reused an existing draft")
	}
	all, err := store.ListDrafts(ctx, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("drafts = %d, want 2", len(all))
	}
}

// A draft deleted on another computer must not wedge autosave: the save still
// succeeds and hands back a usable handle.
func TestSaveDraftRecoversFromDeletedHandle(t *testing.T) {
	ctx := context.Background()
	store, owner := newDraftFixture(t)

	created, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "me@example.com", Subject: "s", BodyHTML: "<p>x</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDraft(ctx, owner, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDraft(ctx, owner, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted draft = %v", err)
	}

	revived, err := store.SaveDraft(ctx, owner, DraftInput{ID: created.ID, AccountEmail: "me@example.com", Subject: "s", BodyHTML: "<p>y</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if !validDraftID(revived.ID) || revived.BodyHTML != "<p>y</p>" {
		t.Fatalf("revived draft = %+v", revived)
	}
	// The handle it hands back is stable from then on.
	again, err := store.SaveDraft(ctx, owner, DraftInput{ID: revived.ID, AccountEmail: "me@example.com", Subject: "s", BodyHTML: "<p>z</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != revived.ID {
		t.Fatalf("handle was not stable: %q -> %q", revived.ID, again.ID)
	}
}

// Draft ids are unguessable and owner-scoped: another account sees not-found,
// never someone else's half-written mail.
func TestDraftsAreOwnerScoped(t *testing.T) {
	ctx := context.Background()
	store, owner := newDraftFixture(t)
	other, err := store.CreateUser(ctx, "intruder", "Intruder", "hash")
	if err != nil {
		t.Fatal(err)
	}

	created, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "me@example.com", ConversationID: "conv-1", Subject: "secret", BodyHTML: "<p>secret</p>"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetDraft(ctx, other.ID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner get = %v", err)
	}
	// Saving with someone else's handle must not overwrite it.
	if _, err := store.SaveDraft(ctx, other.ID, DraftInput{ID: created.ID, Subject: "overwritten"}); err != nil {
		t.Fatalf("cross-owner save: %v", err)
	}
	untouched, err := store.GetDraft(ctx, owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if untouched.Subject != "secret" || untouched.BodyHTML != "<p>secret</p>" {
		t.Fatalf("owner draft was modified: %+v", untouched)
	}

	// The intruder got their own row instead, and sees only that.
	theirs, err := store.ListDrafts(ctx, other.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(theirs) != 1 || theirs[0].ID == created.ID {
		t.Fatalf("intruder drafts = %+v", theirs)
	}
	if err := store.DeleteDraft(ctx, other.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDraft(ctx, owner, created.ID); err != nil {
		t.Fatalf("cross-owner delete removed the owner's draft: %v", err)
	}
}

// One conversation draft per (owner, account): two accounts on the same
// conversation each keep their own reply.
func TestSaveDraftSeparatesAccounts(t *testing.T) {
	ctx := context.Background()
	store, owner := newDraftFixture(t)

	first, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "a@example.com", ConversationID: "conv-1", Subject: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "b@example.com", ConversationID: "conv-1", Subject: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("two accounts shared one conversation draft")
	}
	drafts, err := store.ListDrafts(ctx, owner, "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("drafts = %d, want 2", len(drafts))
	}
}

func TestSaveDraftRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	store, owner := newDraftFixture(t)

	cases := []struct {
		name  string
		owner string
		input DraftInput
	}{
		{"missing owner", "", DraftInput{Subject: "s"}},
		{"unknown mode", owner, DraftInput{Mode: "sneaky"}},
		{"subject too long", owner, DraftInput{Subject: strings.Repeat("s", maxDraftSubjectRunes+1)}},
		{"body too large", owner, DraftInput{BodyHTML: strings.Repeat("x", maxDraftHTMLBytes+1)}},
		{"recipients too long", owner, DraftInput{To: strings.Repeat("a@b.co,", maxDraftAddressBytes)}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := store.SaveDraft(ctx, testCase.owner, testCase.input)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !IsDraftInputError(err) {
				t.Fatalf("error is not classified as input: %v", err)
			}
		})
	}

	// The oversized inputs must not have been stored.
	drafts, err := store.ListDrafts(ctx, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 0 {
		t.Fatalf("rejected drafts were persisted: %+v", drafts)
	}
}

func TestIsEmptyDraft(t *testing.T) {
	if !IsEmptyDraft(DraftInput{Mode: DraftModeReply}) {
		t.Fatal("a blank compose window is not empty")
	}
	if !IsEmptyDraft(DraftInput{To: "  ", Subject: "\n", BodyHTML: " "}) {
		t.Fatal("whitespace-only content counts as empty")
	}
	if IsEmptyDraft(DraftInput{Subject: "x"}) {
		t.Fatal("a subject counts as content")
	}
	if IsEmptyDraft(DraftInput{BodyHTML: "<p>hi</p>"}) {
		t.Fatal("a body counts as content")
	}
}

func TestListDraftsOrdersNewestFirst(t *testing.T) {
	ctx := context.Background()
	store, owner := newDraftFixture(t)

	older, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "me@example.com", Subject: "older"})
	if err != nil {
		t.Fatal(err)
	}
	// updated_at has one-second resolution, so nudge the clock deterministically.
	if _, err := store.db.ExecContext(ctx, `UPDATE drafts SET updated_at = ? WHERE id = ?`, time.Now().Unix()-60, older.ID); err != nil {
		t.Fatal(err)
	}
	newer, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "me@example.com", Subject: "newer"})
	if err != nil {
		t.Fatal(err)
	}

	drafts, err := store.ListDrafts(ctx, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 || drafts[0].ID != newer.ID || drafts[1].ID != older.ID {
		t.Fatalf("order = %+v", drafts)
	}
	if drafts[0].UpdatedAt.IsZero() || drafts[0].CreatedAt.IsZero() {
		t.Fatal("timestamps were not restored")
	}
}

func TestDeleteDraftsForConversation(t *testing.T) {
	ctx := context.Background()
	store, owner := newDraftFixture(t)

	if _, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "me@example.com", ConversationID: "conv-1", Subject: "reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "other@example.com", ConversationID: "conv-1", Subject: "other account"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveDraft(ctx, owner, DraftInput{AccountEmail: "me@example.com", ConversationID: "conv-2", Subject: "keep"}); err != nil {
		t.Fatal(err)
	}

	removed, err := store.DeleteDraftsForConversation(ctx, owner, "me@example.com", "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	remaining, err := store.ListDrafts(ctx, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining = %+v", remaining)
	}

	// An empty conversation id is a no-op rather than a table wipe.
	if removed, err := store.DeleteDraftsForConversation(ctx, owner, "me@example.com", ""); err != nil || removed != 0 {
		t.Fatalf("empty conversation delete = %d, %v", removed, err)
	}
	if all, err := store.ListDrafts(ctx, owner, ""); err != nil || len(all) != 2 {
		t.Fatalf("drafts after no-op delete = %+v, %v", all, err)
	}
}
