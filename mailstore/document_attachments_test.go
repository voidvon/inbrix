package mailstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/url"
	"strings"
	"testing"
)

func documentAttachmentTestImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for i := range img.Pix {
		img.Pix[i] = byte(i*i/257 + i/13)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDocumentImageAttachmentsPreserveSnapshotAndDeduplicate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "image-owner", "Owner", "hash")
	other, _ := s.CreateUser(ctx, "image-other", "Other", "hash")
	data := documentAttachmentTestImage(t)
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><text>x+y</text></svg>`
	payload := map[string]any{
		"business": map[string]any{"version": 2, "values": map[string]string{"customer_company": "Buyer"}},
		"data": map[string]any{
			"main": []any{map[string]any{
				"type": "image", "value": imageURL, "width": 100, "height": 100,
				"extension": map[string]any{"lineTemplate": map[string]any{"value": imageURL}, "unknown": json.Number("9007199254740993")},
			}},
			"header": []any{map[string]any{"value": imageURL}},
			"footer": []any{map[string]any{"value": imageURL}},
		},
		"options": map[string]any{"background": map[string]any{"image": "data:image/svg+xml;charset=utf-8," + url.PathEscape(svg)}},
	}
	raw, _ := json.Marshal(payload)
	body := storedCanvasPrefix + string(raw)
	record, err := s.SaveDocument(ctx, DocumentRecord{ID: "with-images", OwnerID: owner.ID, Type: "quotation", Name: "Quote", HTML: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(record.HTML, "data:image/") || len(record.HTML) > 2000 || !strings.Contains(record.HTML, "9007199254740993") {
		t.Fatalf("snapshot was not compacted losslessly: bytes=%d", len(record.HTML))
	}
	var count int
	_ = s.db.QueryRow(`SELECT count(*) FROM document_attachments WHERE owner_id=?`, owner.ID).Scan(&count)
	if count != 2 {
		t.Fatal("duplicate images in rows/prototypes were stored more than once", count)
	}
	a, err := s.SaveDocumentAttachment(ctx, owner.ID, data)
	if err != nil || !strings.Contains(record.HTML, a.URL) {
		t.Fatal(a, err)
	}
	got, mime, err := s.GetDocumentAttachment(ctx, owner.ID, a.ID)
	if err != nil || mime != "image/png" || !bytes.Equal(got, data) {
		t.Fatal("image bytes changed", mime, err)
	}
	var svgData []byte
	_ = s.db.QueryRow(`SELECT data FROM document_attachments WHERE owner_id=? AND content_type='image/svg+xml'`, owner.ID).Scan(&svgData)
	if string(svgData) != svg {
		t.Fatal("percent-encoded SVG was not preserved")
	}
	if _, _, err := s.GetDocumentAttachment(ctx, other.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign attachment read accepted", err)
	}
	foreign := *record
	foreign.OwnerID, foreign.ID = other.ID, "foreign"
	if _, err := s.SaveDocument(ctx, foreign, 0); !errors.Is(err, ErrDocumentAttachmentInvalid) {
		t.Fatal("foreign attachment reference accepted", err)
	}
	// A failed document write must roll back any new attachment inserts as well.
	record.HTML = strings.Replace(body, "Buyer", "Changed", 1)
	record.HTML = strings.Replace(record.HTML, `"options":{`, `"newImage":"data:image/svg+xml,%3Csvg%3E%3C%2Fsvg%3E","options":{`, 1)
	if _, err := s.SaveDocument(ctx, *record, record.Revision+1); !errors.Is(err, ErrDocumentConflict) {
		t.Fatal(err)
	}
	_ = s.db.QueryRow(`SELECT count(*) FROM document_attachments WHERE owner_id=?`, owner.ID).Scan(&count)
	if count != 2 {
		t.Fatal("failed document save left an attachment behind", count)
	}
	updated, err := s.SaveDocument(ctx, *record, record.Revision)
	if err != nil || strings.Contains(updated.HTML, "data:image/") {
		t.Fatal(err)
	}
	if err := s.DeleteDocuments(ctx, owner.ID, []string{record.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetDocumentAttachment(ctx, owner.ID, a.ID); err != nil {
		t.Fatal("deleting a source broke an immutable image reference", err)
	}
}

func TestLegacyDocumentImagesMigrateWithoutChangingRevision(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "legacy-image-owner", "Owner", "hash")
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(documentAttachmentTestImage(t))
	body := storedCanvasPrefix + `{"data":{"main":[{"type":"image","value":"` + imageURL + `","width":100,"custom":"keep"}]}}`
	_, err := s.db.Exec(`INSERT INTO documents(id,owner_id,type,name,html,revision,created_at,updated_at) VALUES(?,?,'quotation','Legacy',?,7,1000,2000)`, "legacy", owner.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.GetDocument(ctx, owner.ID, "legacy")
	if err != nil || record.Revision != 7 || record.UpdatedAt.UnixMilli() != 2000 || strings.Contains(record.HTML, "data:image/") || !strings.Contains(record.HTML, `"custom":"keep"`) {
		t.Fatal("migration changed document facts", record, err)
	}
	var stored string
	_ = s.db.QueryRow(`SELECT html FROM documents WHERE id='legacy'`).Scan(&stored)
	if stored != record.HTML {
		t.Fatal("migration did not persist compact content")
	}
	// A migration of an earlier read cannot overwrite a newer business edit.
	_, _ = s.db.Exec(`UPDATE documents SET html='newer edit',revision=8 WHERE id='legacy'`)
	if _, err := s.compactStoredDocumentImages(ctx, owner.ID, "legacy", body, "documents", "html"); err != nil {
		t.Fatal(err)
	}
	current, _ := s.GetDocument(ctx, owner.ID, "legacy")
	if current.HTML != "newer edit" || current.Revision != 8 {
		t.Fatal("migration overwrote a newer document")
	}
}

func TestTemplateAndStampImagesRemainAvailableAfterSourceDeletion(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "stamp-image-owner", "Owner", "hash")
	data := documentAttachmentTestImage(t)
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	stamp, err := s.SaveDocumentStamp(ctx, DocumentStampRecord{OwnerID: owner.ID, Value: imageURL, Width: 100, Height: 100})
	if err != nil || !strings.HasPrefix(stamp.Value, DocumentAttachmentURLPrefix) {
		t.Fatal(stamp, err)
	}
	body := storedCanvasPrefix + `{"data":{"main":[{"type":"image","value":"` + imageURL + `"}]}}`
	tpl, err := s.SaveDocumentTemplate(ctx, DocumentTemplateRecord{OwnerID: owner.ID, HTML: body})
	if err != nil || strings.Contains(tpl.HTML, "data:image/") || !strings.Contains(tpl.HTML, stamp.Value) {
		t.Fatal(tpl, err)
	}
	// Simulate records created before attachment storage was introduced.
	_, _ = s.db.Exec(`UPDATE document_stamps SET image_data=? WHERE id=?`, imageURL, stamp.ID)
	_, _ = s.db.Exec(`UPDATE document_templates SET html=? WHERE id=?`, body, tpl.ID)
	stamps, err := s.ListDocumentStamps(ctx, owner.ID)
	if err != nil || len(stamps) != 1 || stamps[0].Value != stamp.Value {
		t.Fatal(stamps, err)
	}
	templates, err := s.ListDocumentTemplates(ctx, owner.ID)
	if err != nil || len(templates) != 1 || templates[0].HTML != tpl.HTML {
		t.Fatal(templates, err)
	}
	_ = s.DeleteDocumentStamp(ctx, owner.ID, stamp.ID)
	_ = s.DeleteDocumentTemplate(ctx, owner.ID, tpl.ID)
	got, _, err := s.GetDocumentAttachment(ctx, owner.ID, strings.TrimPrefix(stamp.Value, DocumentAttachmentURLPrefix))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("source deletion destroyed image", err)
	}
}
