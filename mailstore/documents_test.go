package mailstore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
		DataURL:     "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
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

func TestServerTemplateDefaultsSeedOnceAndKeepEditsAndDeletions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "template-owner", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, "template-other", "Other", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, false); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListDocumentTemplates(ctx, owner.ID)
	if err != nil || len(first) != 5 || first[0].Name != "英文报价单模板" || first[1].Name != "中文报价单模板" || first[2].Type != "contract" {
		t.Fatal(first, err)
	}
	first[0].Name, first[0].HTML = "My template", "<p>Edited on server</p>"
	if _, err := s.SaveDocumentTemplate(ctx, first[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDocumentTemplate(ctx, owner.ID, first[2].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDefaultDocumentTemplates(ctx, other.ID, true); err != nil {
		t.Fatal(err)
	}
	// A restart and another read cannot restore a deleted default or its old body.
	reopened, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.EnsureDefaultDocumentTemplates(ctx, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	current, err := reopened.ListDocumentTemplates(ctx, owner.ID)
	if err != nil || len(current) != 4 || current[0].HTML != first[0].HTML || current[0].Name != "My template" {
		t.Fatal(current, err)
	}
	foreign, err := reopened.ListDocumentTemplates(ctx, other.ID)
	if err != nil || len(foreign) != 5 || foreign[0].Name != "English quotation" || foreign[1].Name != "Chinese quotation" || foreign[0].ID == first[0].ID {
		t.Fatal(foreign, err)
	}
}

func TestStampInsertWidthPersistsOnServerAndMigratesOldSchema(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "stamp-width-owner", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := s.SaveDocumentStamp(ctx, DocumentStampRecord{OwnerID: owner.ID, Name: "Seal", Value: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", Width: 100, Height: 100, InsertWidth: 61})
	if err != nil || stamp.InsertWidth != 61 {
		t.Fatal(stamp, err)
	}
	list, err := s.ListDocumentStamps(ctx, owner.ID)
	if err != nil || len(list) != 1 || list[0].InsertWidth != 61 {
		t.Fatal(list, err)
	}
	// Recreate the previous schema, which did not persist insertion width.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE document_stamps DROP COLUMN insert_width`); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	list, err = reopened.ListDocumentStamps(ctx, owner.ID)
	if err != nil || len(list) != 1 || list[0].InsertWidth != 40 || list[0].Value != stamp.Value {
		t.Fatal(list, err)
	}
}

func TestChineseContractTemplateSeedsForExistingOwnerOnce(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "cn-contract", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an account that already initialized the older built-ins.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO document_template_initializations(owner_id) VALUES(?)`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, false); err != nil {
		t.Fatal(err)
	}
	templates, err := s.ListDocumentTemplates(ctx, owner.ID)
	if err != nil || len(templates) != 3 {
		t.Fatal(templates, err)
	}
	var chineseTemplate DocumentTemplateRecord
	for _, template := range templates {
		if template.ID == "chinese-sales-contract-"+owner.ID {
			chineseTemplate = template
		}
	}
	if chineseTemplate.Name != "中文购销合同模板" || !strings.Contains(chineseTemplate.HTML, `"version": 2`) || !strings.Contains(chineseTemplate.HTML, "item_price_0") {
		t.Fatal("not a structured commercial template")
	}
	if err := s.DeleteDocumentTemplate(ctx, owner.ID, chineseTemplate.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	templates, err = s.ListDocumentTemplates(ctx, owner.ID)
	if err != nil || len(templates) != 2 {
		t.Fatal("deleted template was recreated", templates, err)
	}
	for _, template := range templates {
		if template.ID == chineseTemplate.ID {
			t.Fatal("deleted template was recreated", template.ID)
		}
	}
}

func TestQuotationTemplatesUpgradeExistingOwnersOnce(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		legacyID         bool
	}{
		{"标准报价单模板", englishQuotationTemplateBody, "英文报价单模板", false},
		{"Standard quotation", englishQuotationTemplateBody, "英文报价单模板", true},
		{"我的报价模板", englishQuotationTemplateBody, "我的报价模板", false},
		{"标准报价单模板", "<p>Custom quotation</p>", "标准报价单模板", false},
		{"", "", "", false}, // The owner deleted the old built-in.
	} {
		t.Run(tt.name+tt.body, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			owner, err := s.CreateUser(ctx, "quotation-owner", "Owner", "hash")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO document_template_initializations(owner_id) VALUES(?)`, owner.ID); err != nil {
				t.Fatal(err)
			}
			englishID := "default-quotation-" + owner.ID
			if tt.legacyID {
				englishID = "default-quotation"
			}
			if tt.name != "" {
				if _, err := s.SaveDocumentTemplate(ctx, DocumentTemplateRecord{ID: englishID, OwnerID: owner.ID, Type: "quotation", Name: tt.name, HTML: tt.body}); err != nil {
					t.Fatal(err)
				}
			}
			read := func() map[string]DocumentTemplateRecord {
				t.Helper()
				templates, err := s.ListDocumentTemplates(ctx, owner.ID)
				if err != nil {
					t.Fatal(err)
				}
				quotations := map[string]DocumentTemplateRecord{}
				for _, template := range templates {
					if template.Type == "quotation" {
						quotations[template.ID] = template
					}
				}
				return quotations
			}
			if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, false); err != nil {
				t.Fatal(err)
			}
			chineseID := "chinese-quotation-" + owner.ID
			quotations := read()
			if got := quotations[englishID]; got.Name != tt.want || got.HTML != tt.body {
				t.Fatalf("unexpected English quotation: %+v", got)
			}
			if got := quotations[chineseID]; got.Name != "中文报价单模板" || got.HTML != chineseQuotationTemplateBody {
				t.Fatalf("unexpected Chinese quotation: %+v", got)
			}
			if err := s.DeleteDocumentTemplate(ctx, owner.ID, chineseID); err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, true); err != nil {
				t.Fatal(err)
			}
			if got := read(); got[chineseID].ID != "" || got[englishID].Name != tt.want {
				t.Fatalf("repeat seed changed quotations: %+v", got)
			}
		})
	}
}

func TestEnglishCommercialInvoiceSeedsForExistingOwnerOnce(t *testing.T) {
	for _, english := range []bool{false, true} {
		t.Run(fmt.Sprint(english), func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			owner, err := s.CreateUser(ctx, "commercial-invoice-owner", "Owner", "hash")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO document_template_initializations(owner_id) VALUES(?)`, owner.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, english); err != nil {
				t.Fatal(err)
			}
			id := "english-commercial-invoice-" + owner.ID
			read := func() *DocumentTemplateRecord {
				t.Helper()
				templates, err := s.ListDocumentTemplates(ctx, owner.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, template := range templates {
					if template.ID == id {
						return &template
					}
				}
				return nil
			}
			name := "英文正式发票模板"
			if english {
				name = "English commercial invoice"
			}
			template := read()
			if template == nil || template.Type != "contract" || template.Name != name || template.HTML != `<div data-spirax-invoice="reference-v1"></div>` {
				t.Fatalf("commercial invoice template not seeded: %+v", template)
			}
			template.Name, template.HTML = "Custom invoice", "<p>Custom invoice layout</p>"
			if _, err := s.SaveDocumentTemplate(ctx, *template); err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, !english); err != nil {
				t.Fatal(err)
			}
			if got := read(); got == nil || got.Name != template.Name || got.HTML != template.HTML {
				t.Fatalf("custom invoice overwritten: %+v", got)
			}
			if err := s.DeleteDocumentTemplate(ctx, owner.ID, id); err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, english); err != nil {
				t.Fatal(err)
			}
			if read() != nil {
				t.Fatal("deleted commercial invoice was recreated")
			}
		})
	}
}

func TestEnglishContractTemplateUpgradePreservesCustomizations(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"标准合同模板", `<div data-spirax-contract="reference-v1"></div>`, "英文形式发票模板"},
		{"Standard contract", `<div data-spirax-contract="reference-v1"></div>`, "英文形式发票模板"},
		{"英文合同模板", `<div data-spirax-contract="reference-v1"></div>`, "英文形式发票模板"},
		{"English sales contract", `<div data-spirax-contract="reference-v1"></div>`, "英文形式发票模板"},
		{"我的合同", `<div data-spirax-contract="reference-v1"></div>`, "我的合同"},
		{"标准合同模板", "CUSTOM CONTRACT CONTENT", "标准合同模板"},
	} {
		t.Run(tt.name+tt.body, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			owner, err := s.CreateUser(ctx, "english-template-owner", "Owner", "hash")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO document_template_initializations(owner_id) VALUES(?)`, owner.ID); err != nil {
				t.Fatal(err)
			}
			id := "default-contract-" + owner.ID
			if _, err := s.SaveDocumentTemplate(ctx, DocumentTemplateRecord{ID: id, OwnerID: owner.ID, Type: "contract", Name: tt.name, HTML: tt.body}); err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, false); err != nil {
				t.Fatal(err)
			}
			read := func() DocumentTemplateRecord {
				t.Helper()
				templates, err := s.ListDocumentTemplates(ctx, owner.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, template := range templates {
					if template.ID == id {
						return template
					}
				}
				t.Fatal("contract template missing")
				return DocumentTemplateRecord{}
			}
			got := read()
			if got.Name != tt.want || got.HTML != tt.body {
				t.Fatalf("upgrade lost template data: %+v", got)
			}
			got.Name = "Standard contract"
			if _, err := s.SaveDocumentTemplate(ctx, got); err != nil {
				t.Fatal(err)
			}
			if err := s.EnsureDefaultDocumentTemplates(ctx, owner.ID, true); err != nil {
				t.Fatal(err)
			}
			if got := read(); got.Name != "Standard contract" || got.HTML != tt.body {
				t.Fatal("later reads overwrote user's template edit", got)
			}
		})
	}
}
