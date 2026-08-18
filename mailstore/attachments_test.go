package mailstore

import (
	"context"
	"path/filepath"
	"testing"

	"inbrix/models"
)

func TestAttachmentIndexLifecycleAndFilters(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "attachment-index@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, owner.ID, "sales@example.com", true)
	message := models.Email{
		ID: "42", Folder: "INBOX", From: "buyer@acme.example", FromName: "Acme Buyer",
		Subject: "Quarterly price list", HTML: `<p>Regards</p><img src="cid:signature-image">`, AttachmentMetadataCached: true,
		Attachments: []models.Attachment{
			{ID: "pdf-token", PartID: "2", Filename: "quote.pdf", ContentType: "application/pdf", Size: 1200},
			{ID: "sheet-token", PartID: "3", Filename: "prices.xlsx", ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Size: 2400},
			{ID: "logo-token", PartID: "4", Filename: "logo.png", ContentType: "image/png", Size: 300, IsInline: true},
			{ID: "signature-token", PartID: "5", Filename: "52573648@52F74567.1176796A0000", ContentType: "application/octet-stream", Size: 400, ContentID: "signature-image"},
		},
	}
	if err := s.UpsertMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}

	items, total, err := s.ListAttachments(ctx, account.ID, AttachmentListOptions{})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("attachments: total=%d items=%+v err=%v", total, items, err)
	}
	if items[0].UID != "42" || items[0].FolderName != "INBOX" || items[0].FromName != "Acme Buyer" {
		t.Fatalf("message context missing: %+v", items[0])
	}

	items, total, err = s.ListAttachments(ctx, account.ID, AttachmentListOptions{Kind: "pdf"})
	if err != nil || total != 1 || len(items) != 1 || items[0].Filename != "quote.pdf" {
		t.Fatalf("pdf filter: total=%d items=%+v err=%v", total, items, err)
	}
	items, total, err = s.ListAttachments(ctx, account.ID, AttachmentListOptions{Kind: "spreadsheets", Query: "quarterly"})
	if err != nil || total != 1 || len(items) != 1 || items[0].Filename != "prices.xlsx" {
		t.Fatalf("spreadsheet/search filter: total=%d items=%+v err=%v", total, items, err)
	}
	items, total, err = s.ListAttachments(ctx, account.ID, AttachmentListOptions{Query: "%"})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("literal wildcard search: total=%d items=%+v err=%v", total, items, err)
	}

	replacement := []models.Attachment{{ID: "archive-token", PartID: "5", Filename: "drawings.zip", ContentType: "application/zip", Size: 4800}}
	if err := s.UpdateAttachmentMetadata(ctx, account.ID, message.Folder, message.ID, replacement); err != nil {
		t.Fatal(err)
	}
	items, total, err = s.ListAttachments(ctx, account.ID, AttachmentListOptions{})
	if err != nil || total != 1 || len(items) != 1 || items[0].Filename != "drawings.zip" {
		t.Fatalf("replacement index: total=%d items=%+v err=%v", total, items, err)
	}

	if err := s.DeleteMessage(ctx, account.ID, message.Folder, message.ID); err != nil {
		t.Fatal(err)
	}
	items, total, err = s.ListAttachments(ctx, account.ID, AttachmentListOptions{})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("cascade cleanup: total=%d items=%+v err=%v", total, items, err)
	}
}

func TestAttachmentIndexBackfillsExistingMessageJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner, err := s.CreateUser(ctx, "attachment-backfill@example.com", "", "hash")
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount(t, s, owner.ID, "archive@example.com", true)
	message := models.Email{ID: "9", Folder: "Archive", AttachmentMetadataCached: true, Attachments: []models.Attachment{{ID: "old-token", PartID: "2", Filename: "legacy.pdf", ContentType: "application/pdf", Size: 99}}}
	if err := s.UpsertMessages(ctx, account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM message_attachments`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, total, err := s.ListAttachments(ctx, account.ID, AttachmentListOptions{})
	if err != nil || total != 1 || len(items) != 1 || items[0].Filename != "legacy.pdf" {
		t.Fatalf("backfill: total=%d items=%+v err=%v", total, items, err)
	}
}
