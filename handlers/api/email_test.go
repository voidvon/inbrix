package api

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"lilmail/models"

	"github.com/emersion/go-imap"
)

// ---------------------------------------------------------------------------
// Attachment-ID codec
// ---------------------------------------------------------------------------

func TestAttachmentIDRoundTrip(t *testing.T) {
	cases := []struct {
		folder, uid, part string
	}{
		{"INBOX", "42", "2.1"},
		{"Sent Items", "1", "1"},
		{"INBOX/Sub", "999", "3.2.1"},
		{"", "0", "1"},
	}

	for _, tc := range cases {
		id := encodeAttachmentID(tc.folder, tc.uid, tc.part)
		folder, uid, part, err := DecodeAttachmentID(id)
		if err != nil {
			t.Errorf("DecodeAttachmentID(%q) error: %v", id, err)
			continue
		}
		if folder != tc.folder || uid != tc.uid || part != tc.part {
			t.Errorf("round-trip mismatch: got (%q,%q,%q), want (%q,%q,%q)",
				folder, uid, part, tc.folder, tc.uid, tc.part)
		}
	}
}

func TestDecodeAttachmentIDInvalid(t *testing.T) {
	cases := []string{
		"",
		"notbase64!!!",
		// valid base64 but missing delimiters
		base64.RawURLEncoding.EncodeToString([]byte("nozero")),
		// only two fields
		base64.RawURLEncoding.EncodeToString([]byte("a\x00b")),
	}
	for _, id := range cases {
		_, _, _, err := DecodeAttachmentID(id)
		if err == nil {
			t.Errorf("expected error for invalid id %q, got nil", id)
		}
	}
}

// ---------------------------------------------------------------------------
// decodeContent
// ---------------------------------------------------------------------------

func TestDecodeContentBase64(t *testing.T) {
	want := []byte("Hello, World!")
	encoded := base64.StdEncoding.EncodeToString(want)
	// Add line breaks as IMAP sometimes delivers them.
	encoded = encoded[:10] + "\r\n" + encoded[10:]

	got, err := decodeContent([]byte(encoded), "base64")
	if err != nil {
		t.Fatalf("decodeContent base64: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("base64 decode: got %q, want %q", got, want)
	}
}

func TestDecodeContentQuotedPrintable(t *testing.T) {
	// "Hello=20World" is QP for "Hello World"
	raw := []byte("Hello=20World")
	got, err := decodeContent(raw, "quoted-printable")
	if err != nil {
		t.Fatalf("decodeContent qp: %v", err)
	}
	if string(got) != "Hello World" {
		t.Errorf("qp decode: got %q, want %q", got, "Hello World")
	}
}

func TestDecodeContentPlain(t *testing.T) {
	raw := []byte("plain text body")
	got, err := decodeContent(raw, "")
	if err != nil {
		t.Fatalf("decodeContent plain: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("plain decode: got %q, want %q", got, raw)
	}
}

func TestDecodeContentUnknownEncoding(t *testing.T) {
	raw := []byte("some bytes")
	got, err := decodeContent(raw, "7bit")
	if err != nil {
		t.Fatalf("decodeContent 7bit: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("7bit passthrough: got %q, want %q", got, raw)
	}
}

// ---------------------------------------------------------------------------
// pathToString helper (used in encodeAttachmentID)
// ---------------------------------------------------------------------------

func TestPathToString(t *testing.T) {
	cases := []struct {
		in   []int
		want string
	}{
		{nil, "1"},
		{[]int{}, "1"},
		{[]int{1}, "1"},
		{[]int{2, 1}, "2.1"},
		{[]int{3, 2, 1}, "3.2.1"},
	}
	for _, tc := range cases {
		got := pathToString(tc.in)
		if got != tc.want {
			t.Errorf("pathToString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// encodeAttachmentID — basic sanity (no null bytes in result)
// ---------------------------------------------------------------------------

func TestEncodeAttachmentIDNoNullBytes(t *testing.T) {
	id := encodeAttachmentID("INBOX", "123", "2.1")
	if strings.ContainsRune(id, '\x00') {
		t.Error("encoded attachment ID must not contain null bytes")
	}
	if id == "" {
		t.Error("encoded attachment ID must not be empty")
	}
}

func TestProcessListMessageUsesRFCHeadersWithoutEnvelope(t *testing.T) {
	msg := imap.NewMessage(17, []imap.FetchItem{
		imap.FetchFlags,
		imap.FetchUid,
		listHeadersSection.FetchItem(),
	})
	msg.Uid = 17
	msg.Flags = []string{imap.SeenFlag}
	// Server responses omit the PEEK marker in the section name; GetBody
	// normalizes the requested section the same way.
	responseSection := *listHeadersSection
	responseSection.Peek = false
	msg.Body[&responseSection] = bytes.NewReader([]byte(
		"Date: Tue, 11 Aug 2026 09:10:11 +0800\r\n" +
			"Subject: =?UTF-8?B?5L2g5aW9?=\r\n" +
			"From: =?UTF-8?B?5rWL6K+V?= <sender@example.com>\r\n" +
			"To: recipient@example.com\r\n" +
			"Cc: copy@example.com\r\n" +
			"Message-ID: <message-17@example.com>\r\n" +
			"In-Reply-To: <message-16@example.com>\r\n" +
			"References: <message-15@example.com> <message-16@example.com>\r\n" +
			"\r\n"))

	got, err := (&Client{}).processListMessage(msg, "INBOX")
	if err != nil {
		t.Fatalf("processListMessage: %v", err)
	}
	if got.Subject != "你好" {
		t.Errorf("subject = %q, want %q", got.Subject, "你好")
	}
	if got.From != "sender@example.com" || got.FromName != "测试" {
		t.Errorf("from = %q / %q", got.From, got.FromName)
	}
	if got.To != "recipient@example.com" || got.Cc != "copy@example.com" {
		t.Errorf("recipients = %q / %q", got.To, got.Cc)
	}
	if got.MessageID != "<message-17@example.com>" || got.InReplyTo != "<message-16@example.com>" {
		t.Errorf("thread headers = %q / %q", got.MessageID, got.InReplyTo)
	}
	if len(got.References) != 2 {
		t.Fatalf("references = %#v, want two message IDs", got.References)
	}
	wantDate := time.Date(2026, time.August, 11, 9, 10, 11, 0, time.FixedZone("+0800", 8*60*60))
	if !got.Date.Equal(wantDate) {
		t.Errorf("date = %v, want %v", got.Date, wantDate)
	}
}

func TestDecodeMIMEHeaderGB18030(t *testing.T) {
	if got := decodeMIMEHeader("=?gb18030?B?xOO6ww==?="); got != "你好" {
		t.Errorf("decoded GB18030 subject = %q, want %q", got, "你好")
	}
}

func TestMarkInlineAttachmentsFromHTML(t *testing.T) {
	attachments := []models.Attachment{
		{ID: "image-1", Filename: "=?gbk?B?NTc1Njc2MjhAMjE3RDkxMTcuNEUzNzdDNkEwMDAw?=", ContentType: "image/jpeg", ContentID: "57567628@217D9117.4E377C6A00000000.jpg"},
		{ID: "image-2", Filename: "=?gbk?B?MkZEQTM3NThANjc5NzM0MkMuNEUzNzdDNkEwMDAw?=", ContentType: "image/jpeg", ContentID: "2FDA3758@6797342C.4E377C6A00000000.jpg"},
		{ID: "image-3", Filename: "=?gbk?B?Q0Y1NTQwNTBARTJBNDlCMjEuNEUzNzdDNkEwMDAw?=", ContentType: "image/jpeg", ContentID: "CF554050@E2A49B21.4E377C6A00000000.jpg"},
		{ID: "pdf", Filename: "Bank_Account_Details.pdf", ContentType: "application/octet-stream"},
	}
	html := `<img src="cid:CF554050@E2A49B21.4E377C6A00000000.jpg"><img src="cid:2FDA3758@6797342C.4E377C6A00000000.jpg"><img src="cid:57567628@217D9117.4E377C6A00000000.jpg">`

	got := MarkInlineAttachmentsFromHTML(html, attachments)
	if len(got) != len(attachments) {
		t.Fatalf("attachment count = %d, want %d", len(got), len(attachments))
	}
	for i, attachment := range got[:3] {
		if !attachment.IsInline {
			t.Errorf("attachment %d was not identified as inline: %+v", i, attachment)
		}
	}
	if got[0].Filename != "57567628@217D9117.4E377C6A0000" || got[1].Filename != "2FDA3758@6797342C.4E377C6A0000" || got[2].Filename != "CF554050@E2A49B21.4E377C6A0000" {
		t.Errorf("RFC 2047 filenames were not decoded: %+v", got[:3])
	}
	if got[3].IsInline {
		t.Errorf("PDF was incorrectly identified as inline: %+v", got[3])
	}
	if attachments[0].IsInline || attachments[0].Filename == got[0].Filename {
		t.Fatal("inline repair mutated the input attachment slice")
	}
}
