package web

import (
	"bytes"
	"mime/multipart"
	"testing"
)

func inlineTestForm(t *testing.T, field string, data []byte) *multipart.Form {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, "image.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(bytes.NewReader(body.Bytes()), writer.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form
}

func TestParseComposeInlineAttachments(t *testing.T) {
	form := inlineTestForm(t, "inline_image_0", []byte("\x89PNG\r\n\x1a\nimage"))
	got, err := parseComposeInlineAttachments(`[{"field":"inline_image_0","contentId":"image123@lilmail"}]`, form.File)
	if err != nil {
		t.Fatal(err)
	}
	if got["inline_image_0"].ContentID != "image123@lilmail" {
		t.Fatalf("unexpected manifest: %+v", got)
	}
	if contentType, err := sniffComposeInlineImage(form.File["inline_image_0"][0]); err != nil || contentType != "image/png" {
		t.Fatalf("sniffComposeInlineImage() = %q, %v", contentType, err)
	}
}

func TestParseComposeInlineAttachmentsRejectsInvalidManifest(t *testing.T) {
	form := inlineTestForm(t, "inline_image_0", []byte("not an image"))
	tests := []string{
		`not-json`,
		`[{"field":"missing","contentId":"image123"}]`,
		`[{"field":"inline_image_0","contentId":"bad\r\nid"}]`,
		`[{"field":"inline_image_0","contentId":"same"},{"field":"inline_image_0","contentId":"other"}]`,
		`[{"field":"inline_image_0","contentId":"same"},{"field":"other","contentId":"same"}]`,
		`[{"field":"inline_image_0","contentId":"image123","extra":true}]`,
	}
	for _, raw := range tests {
		if _, err := parseComposeInlineAttachments(raw, form.File); err == nil {
			t.Errorf("parseComposeInlineAttachments(%q) unexpectedly succeeded", raw)
		}
	}
	if _, err := sniffComposeInlineImage(form.File["inline_image_0"][0]); err == nil {
		t.Fatal("non-image upload unexpectedly passed content sniffing")
	}
}
