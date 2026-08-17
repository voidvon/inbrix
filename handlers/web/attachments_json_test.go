package web

import (
	"testing"
	"time"

	"lilmail/handlers/api"
	"lilmail/mailstore"
)

func TestAttachmentJSONBuildsDownloadIDAndMessageContext(t *testing.T) {
	record := mailstore.AttachmentRecord{
		FolderName: "Projects/RFQ", UID: "42", PartID: "2.1", Filename: "A &amp; B.pdf",
		ContentType: "application/pdf", Size: 1234, MessageDate: time.Unix(100, 0).UTC(),
		From: "buyer@example.com", FromName: "Buyer", Subject: "Quote &amp; terms",
	}
	got := attachmentJSON(record, "sales@example.com")
	if got.ID == "" || got.Filename != "A & B.pdf" || got.MessageTitle != "Quote & terms" || got.AccountEmail != "sales@example.com" {
		t.Fatalf("attachment JSON: %+v", got)
	}
	folder, uid, part, err := api.DecodeAttachmentID(got.ID)
	if err != nil || folder != record.FolderName || uid != record.UID || part != record.PartID {
		t.Fatalf("download ID decoded as %q/%q/%q, err=%v", folder, uid, part, err)
	}
}

func TestAttachmentJSONDecodesMIMEFilename(t *testing.T) {
	record := mailstore.AttachmentRecord{
		Filename: "=?gbk?B?MkMzQzU0MTJAMEJFREM2MzUuMzg0MDdDNkEwMDAw?=",
	}

	got := attachmentJSON(record, "sales@example.com")
	if got.Filename != "2C3C5412@0BEDC635.38407C6A0000" {
		t.Fatalf("decoded filename = %q", got.Filename)
	}
}
