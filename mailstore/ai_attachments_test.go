package mailstore

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAIImagePersistsAcrossStoreReopenAndEnforcesMailbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "images.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	account := testAccount(t, store, "owner", "sales@example.com", true)
	second := testAccount(t, store, "owner", "other@example.com", false)
	data := []byte("validated image bytes")
	attachment, err := store.CreateAIChatAttachment(ctx, "owner", account.ID, "需求.png", "image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetAIChatAttachment(ctx, "owner", account.ID, attachment.ID)
	if err != nil || !bytes.Equal(got.Data, data) || got.Filename != "需求.png" {
		t.Fatalf("after reopen: %+v %v", got, err)
	}
	for _, scope := range [][2]string{{"stranger", account.ID}, {"owner", second.ID}} {
		_, err = store.GetAIChatAttachment(ctx, scope[0], scope[1], attachment.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign read: %v", err)
		}
		err = store.DeleteAIChatAttachment(ctx, scope[0], scope[1], attachment.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign delete: %v", err)
		}
	}
	if err = store.DeleteAIChatAttachment(ctx, "owner", account.ID, attachment.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetAIChatAttachment(ctx, "owner", account.ID, attachment.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted image remains", err)
	}
}
