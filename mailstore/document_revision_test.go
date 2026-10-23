package mailstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDocumentRevisionProtectsConcurrentEditsAndDeletedDocuments(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "documents.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user, err := s.CreateUser(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	owner := user.ID
	doc := DocumentRecord{ID: "guarded", OwnerID: owner, Type: "quotation", Name: "Quote", HTML: "original"}
	initial, err := s.SaveDocument(ctx, doc, 0)
	if err != nil || initial.Revision != 1 {
		t.Fatalf("create: %+v %v", initial, err)
	}
	doc.HTML = "human changed price"
	changed, err := s.SaveDocument(ctx, doc, initial.Revision)
	if err != nil || changed.Revision != 2 {
		t.Fatalf("edit: %+v %v", changed, err)
	}
	doc.HTML = "old AI proposal"
	if _, err = s.SaveDocument(ctx, doc, initial.Revision); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("stale update: %v", err)
	}
	if err = s.DeleteDocumentRevision(ctx, owner, doc.ID, initial.Revision); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	other := doc
	other.OwnerID = "other"
	if _, err = s.SaveDocument(ctx, other, changed.Revision); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("foreign update: %v", err)
	}
	if err = s.DeleteDocumentRevision(ctx, "other", doc.ID, changed.Revision); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("foreign delete: %v", err)
	}
	if _, err = s.SaveDocument(ctx, doc, 0); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("create over existing: %v", err)
	}
	got, err := s.GetDocument(ctx, owner, doc.ID)
	if err != nil || got.HTML != "human changed price" || got.Revision != 2 {
		t.Fatalf("human edit lost: %+v %v", got, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.GetDocument(ctx, owner, doc.ID)
	if err != nil || got.Revision != 2 {
		t.Fatalf("revision lost after restart: %+v %v", got, err)
	}
	if err = s.DeleteDocumentRevision(ctx, owner, doc.ID, got.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveDocument(ctx, doc, got.Revision); !errors.Is(err, ErrDocumentConflict) {
		t.Fatalf("stale update recreated deleted document: %v", err)
	}
}

func TestDocumentRevisionMigrationPreservesExistingContent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	// Recreate the schema used by older releases, then run the upgrade twice.
	_, err := s.db.Exec(`DROP TABLE documents; CREATE TABLE documents(id TEXT PRIMARY KEY,owner_id TEXT NOT NULL,type TEXT NOT NULL,name TEXT NOT NULL,company TEXT NOT NULL,html TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL); INSERT INTO documents VALUES('old','owner','quotation','Old','','kept',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.migrateDocumentRevision(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetDocument(ctx, "owner", "old")
	if err != nil || got.HTML != "kept" || got.Revision != 1 {
		t.Fatalf("migration: %+v %v", got, err)
	}
}
