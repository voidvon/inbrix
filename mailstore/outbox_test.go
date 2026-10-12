package mailstore

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"inbrix/models"
)

func outgoingFixture(t *testing.T, s *Store) (Account, OutgoingMessage) {
	t.Helper()
	user, err := s.CreateUser(context.Background(), "outbox-user", "User", "hash")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, user.ID, "me@example.com", true)
	record := OutgoingMessage{AccountID: account.ID, ClientKey: "upload-1", ConversationID: "thread-1", Message: models.Email{From: account.Email, To: "peer@example.com", Body: "hello", MessageID: "<outgoing@example.com>", Date: time.Now().Add(-time.Hour)}, Recipients: []string{"peer@example.com", "hidden@example.com"}, RawMIME: []byte("From: me@example.com\r\n\r\nhello\x00attachment")}
	return account, record
}

func TestOutgoingDurableIdempotentAndAccountScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	account, record := outgoingFixture(t, s)
	saved, err := s.CreateOutgoing(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.CreateOutgoing(context.Background(), record)
	if err != nil || repeated.ID != saved.ID {
		t.Fatalf("idempotency: %v %+v", err, repeated)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loaded, err := s.GetOutgoing(context.Background(), account.ID, saved.ID)
	if err != nil || !bytes.Equal(loaded.RawMIME, record.RawMIME) || len(loaded.Recipients) != 2 || loaded.Recipients[1] != "hidden@example.com" {
		t.Fatalf("lost MIME/envelope: %v %+v", err, loaded)
	}
	if _, err := s.GetOutgoing(context.Background(), "other-account", saved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account read: %v", err)
	}
	if err := s.RetryOutgoing(context.Background(), "other-account", saved.ID); !errors.Is(err, ErrOutboxBusy) {
		t.Fatalf("cross-account retry: %v", err)
	}
	if err := s.DeleteOutgoing(context.Background(), "other-account", saved.ID); !errors.Is(err, ErrOutboxBusy) {
		t.Fatalf("cross-account delete: %v", err)
	}
}

func TestOutgoingClaimRetryAndRecovery(t *testing.T) {
	s := openTestStore(t)
	account, record := outgoingFixture(t, s)
	ctx := context.Background()
	saved, err := s.CreateOutgoing(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var claims atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ClaimOutgoing(ctx)
			if err == nil {
				claims.Add(1)
			} else if !errors.Is(err, ErrNotFound) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("SMTP claimed %d times", claims.Load())
	}
	if err := s.DeleteOutgoing(ctx, account.ID, saved.ID); !errors.Is(err, ErrOutboxBusy) {
		t.Fatalf("delete in progress: %v", err)
	}
	if err := s.RecoverOutgoing(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, _ := s.GetOutgoing(ctx, account.ID, saved.ID)
	if recovered.Status != "unknown" {
		t.Fatalf("restart state = %s", recovered.Status)
	}
	if _, err := s.ClaimOutgoing(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("auto resent uncertain message: %v", err)
	}
	claims.Store(0)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.RetryOutgoing(ctx, account.ID, saved.ID)
			if err == nil {
				claims.Add(1)
			} else if !errors.Is(err, ErrOutboxBusy) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("retry queued %d times", claims.Load())
	}
	next, err := s.ClaimOutgoing(ctx)
	if err != nil || next.Attempts != 2 || !bytes.Equal(next.RawMIME, record.RawMIME) {
		t.Fatalf("retry payload: %v %+v", err, next)
	}
	if err := s.FinishOutgoing(ctx, account.ID, saved.ID, "failed", "TLS EOF"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := s.GetOutgoing(ctx, account.ID, saved.ID)
	if loaded.LastError != "TLS EOF" || loaded.Status != "failed" {
		t.Fatalf("failure not persisted: %+v", loaded)
	}
	if _, err := s.ClaimOutgoing(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("auto resent failure: %v", err)
	}
}

func TestOutgoingExactMirrorDedupAndSupersede(t *testing.T) {
	s := openTestStore(t)
	account, record := outgoingFixture(t, s)
	ctx := context.Background()
	record.Status = "failed"
	saved, err := s.CreateOutgoing(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFolder(ctx, Folder{AccountID: account.ID, Name: "Sent"}); err != nil {
		t.Fatal(err)
	}
	similar := record.Message
	similar.ID = "1"
	similar.MessageID = "<different@example.com>"
	if err := s.UpsertMessages(ctx, account.ID, "Sent", []models.Email{similar}); err != nil {
		t.Fatal(err)
	}
	visible, err := s.ListOutgoing(ctx, account.ID)
	if err != nil || len(visible) != 1 {
		t.Fatalf("similar content hid outgoing: %v %d", err, len(visible))
	}
	similar.ID = "2"
	similar.MessageID = record.Message.MessageID
	if err := s.UpsertMessages(ctx, account.ID, "Sent", []models.Email{similar}); err != nil {
		t.Fatal(err)
	}
	visible, err = s.ListOutgoing(ctx, account.ID)
	if err != nil || len(visible) != 0 {
		t.Fatalf("duplicate mirror bubble: %v %d", err, len(visible))
	}
	if err := s.RetryOutgoing(ctx, account.ID, saved.ID); !errors.Is(err, ErrOutboxBusy) {
		t.Fatalf("resent mirrored mail: %v", err)
	}
	record.ClientKey = "legacy-missing"
	record.Message.MessageID = "<missing@example.com>"
	record.RawMIME = nil
	missing, err := s.CreateOutgoing(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RetryOutgoing(ctx, account.ID, missing.ID); !errors.Is(err, ErrOutboxBusy) {
		t.Fatal("retried without attachments")
	}
	record.ClientKey = "replacement"
	record.Message.MessageID = "<replacement@example.com>"
	record.RawMIME = []byte("reattached")
	record.Status = ""
	record.ReplacesID = missing.ID
	replacement, err := s.CreateOutgoing(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := s.GetOutgoing(ctx, account.ID, missing.ID)
	if old.Status != "superseded" || replacement.Status != "queued" {
		t.Fatalf("replace was not atomic: %s %s", old.Status, replacement.Status)
	}
	repeated, err := s.CreateOutgoing(ctx, record)
	if err != nil || repeated.ID != replacement.ID {
		t.Fatalf("replacement idempotency: %v", err)
	}
	record.ClientKey = "invalid-replace"
	record.ReplacesID = replacement.ID
	if _, err := s.CreateOutgoing(ctx, record); !errors.Is(err, ErrOutboxBusy) {
		t.Fatalf("replaced active send: %v", err)
	}
	visible, _ = s.ListOutgoing(ctx, account.ID)
	if len(visible) != 1 || visible[0].ID != replacement.ID {
		t.Fatalf("failed transaction leaked bubble: %+v", visible)
	}
}

func TestTencentSentMessageIDDedupAndRetryProtection(t *testing.T) {
	original := "<1791802215791647170.1822253.4016009473503969760@spiraxsteam.com>"
	for _, tc := range []struct {
		name, server, mirrorID string
		otherAccount, hidden   bool
	}{
		{name: "exact ID", server: "smtp.example.com", mirrorID: original, hidden: true},
		{name: "observed Tencent ID", server: "smtp.exmail.qq.com", mirrorID: "<07892A00BBB2336A+" + original[1:], hidden: true},
		{name: "lowercase prefix", server: "SMTP.QQ.COM", mirrorID: "<07892a00bbb2336a+" + original[1:], hidden: true},
		{name: "different provider", server: "smtp.example.com", mirrorID: "<07892A00BBB2336A+" + original[1:]},
		{name: "non hex prefix", server: "smtp.exmail.qq.com", mirrorID: "<07892A00BBB2336Z+" + original[1:]},
		{name: "short prefix", server: "smtp.exmail.qq.com", mirrorID: "<07892A+" + original[1:]},
		{name: "different original", server: "smtp.exmail.qq.com", mirrorID: "<07892A00BBB2336A+other@spiraxsteam.com>"},
		{name: "other account", server: "smtp.exmail.qq.com", mirrorID: "<07892A00BBB2336A+" + original[1:], otherAccount: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			account, record := outgoingFixture(t, s)
			account.SMTPServer = tc.server
			if _, err := s.UpsertAccount(ctx, account); err != nil {
				t.Fatal(err)
			}
			record.Message.MessageID = original
			record.Status = "unknown"
			saved, err := s.CreateOutgoing(ctx, record)
			if err != nil {
				t.Fatal(err)
			}
			mirrorAccount := account
			if tc.otherAccount {
				mirrorAccount = testAccount(t, s, account.OwnerID, "other@example.com", false)
			}
			if err := s.UpsertFolder(ctx, Folder{AccountID: mirrorAccount.ID, Name: "Sent"}); err != nil {
				t.Fatal(err)
			}
			mirrored := record.Message
			mirrored.ID = "376"
			mirrored.MessageID = tc.mirrorID
			if err := s.UpsertMessages(ctx, mirrorAccount.ID, "Sent", []models.Email{mirrored}); err != nil {
				t.Fatal(err)
			}
			visible, err := s.ListOutgoing(ctx, account.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (len(visible) == 0) != tc.hidden {
				t.Fatalf("hidden = %v, want %v", len(visible) == 0, tc.hidden)
			}
			err = s.RetryOutgoing(ctx, account.ID, saved.ID)
			if tc.hidden && !errors.Is(err, ErrOutboxBusy) {
				t.Fatalf("allowed resend of mirrored email: %v", err)
			}
			if !tc.hidden && err != nil {
				t.Fatalf("unrelated mirror blocked retry: %v", err)
			}
			canonical := OutgoingMirrorMessageID(tc.server, tc.mirrorID)
			if !tc.otherAccount && (canonical == original) != tc.hidden {
				t.Fatalf("metadata canonical ID disagrees with dedup: %s", canonical)
			}
		})
	}
}
