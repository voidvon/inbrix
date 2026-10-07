package mailstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDocumentsCRUD(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	user, err := store.CreateUser(ctx, "doc-user@example.com", "Doc User", "hash")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	ownerID := user.ID

	// 1. Save and Get Document
	doc := DocumentRecord{
		ID:      "doc-1",
		OwnerID: ownerID,
		Type:    "quotation",
		Name:    "报价单测试",
		Company: "测试公司",
		HTML:    "<div>test quotation</div>",
	}
	saved, err := store.SaveDocument(ctx, doc)
	if err != nil {
		t.Fatalf("SaveDocument failed: %v", err)
	}
	if saved.ID != "doc-1" || saved.Name != "报价单测试" || saved.Company != "测试公司" {
		t.Errorf("saved doc mismatch: %+v", saved)
	}

	got, err := store.GetDocument(ctx, ownerID, "doc-1")
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if got.HTML != "<div>test quotation</div>" {
		t.Errorf("got HTML mismatch: %s", got.HTML)
	}

	// 2. Update Document
	doc.Name = "报价单更新"
	doc.HTML = "<div>updated quotation</div>"
	updated, err := store.SaveDocument(ctx, doc)
	if err != nil {
		t.Fatalf("update SaveDocument failed: %v", err)
	}
	if updated.Name != "报价单更新" || updated.HTML != "<div>updated quotation</div>" {
		t.Errorf("updated mismatch: %+v", updated)
	}

	// 3. List Documents
	list, err := store.ListDocuments(ctx, ownerID, "all")
	if err != nil {
		t.Fatalf("ListDocuments failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 document, got %d", len(list))
	}

	// 4. Delete Document
	if err := store.DeleteDocuments(ctx, ownerID, []string{"doc-1"}); err != nil {
		t.Fatalf("DeleteDocuments failed: %v", err)
	}
	listAfterDelete, err := store.ListDocuments(ctx, ownerID, "all")
	if err != nil {
		t.Fatalf("ListDocuments after delete failed: %v", err)
	}
	if len(listAfterDelete) != 0 {
		t.Errorf("expected 0 documents after delete, got %d", len(listAfterDelete))
	}
}

func TestTemplatesAndStampsCRUD(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	user, err := store.CreateUser(ctx, "tpl-user@example.com", "Tpl User", "hash")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	ownerID := user.ID

	// Template
	tpl := DocumentTemplateRecord{
		ID:      "tpl-1",
		OwnerID: ownerID,
		Type:    "contract",
		Name:    "专属销售合同模板",
		HTML:    "<div>contract template</div>",
	}
	if _, err := store.SaveDocumentTemplate(ctx, tpl); err != nil {
		t.Fatalf("SaveDocumentTemplate failed: %v", err)
	}
	templates, err := store.ListDocumentTemplates(ctx, ownerID)
	if err != nil {
		t.Fatalf("ListDocumentTemplates failed: %v", err)
	}
	if len(templates) != 1 || templates[0].Name != "专属销售合同模板" {
		t.Errorf("expected 1 template, got %+v", templates)
	}
	if err := store.DeleteDocumentTemplate(ctx, ownerID, "tpl-1"); err != nil {
		t.Fatalf("DeleteDocumentTemplate failed: %v", err)
	}

	// Stamp
	stamp := DocumentStampRecord{
		ID:          "stamp-1",
		OwnerID:     ownerID,
		Name:        "合同章",
		DataURL:     "data:image/png;base64,abc",
		Width:       120,
		Height:      120,
		AspectRatio: 1.0,
	}
	if _, err := store.SaveDocumentStamp(ctx, stamp); err != nil {
		t.Fatalf("SaveDocumentStamp failed: %v", err)
	}
	stamps, err := store.ListDocumentStamps(ctx, ownerID)
	if err != nil {
		t.Fatalf("ListDocumentStamps failed: %v", err)
	}
	if len(stamps) != 1 || stamps[0].Name != "合同章" {
		t.Errorf("expected 1 stamp, got %+v", stamps)
	}
	if err := store.DeleteDocumentStamp(ctx, ownerID, "stamp-1"); err != nil {
		t.Fatalf("DeleteDocumentStamp failed: %v", err)
	}
	stampsAfter, err := store.ListDocumentStamps(ctx, ownerID)
	if err != nil {
		t.Fatalf("ListDocumentStamps failed: %v", err)
	}
	if len(stampsAfter) != 0 {
		t.Errorf("expected 0 stamps after delete, got %d", len(stampsAfter))
	}
}
