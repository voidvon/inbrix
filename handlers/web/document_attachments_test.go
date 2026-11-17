package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestDocumentAttachmentUploadOwnershipCachingAndTypeValidation(t *testing.T) {
	f := newAPIKeyFixture(t)
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10" fill="red"/></svg>`
	upload := func(data, key string) (*http.Response, string) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "image.svg")
		_, _ = part.Write([]byte(data))
		_ = writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/document-attachments", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		res, err := f.app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res, string(raw)
	}
	res, body := upload(svg, "")
	if res.StatusCode != 401 {
		t.Fatal("unauthenticated upload accepted", res.StatusCode, body)
	}
	res, body = upload("<html>not an image</html>", f.key.Key)
	if res.StatusCode != 422 {
		t.Fatal("non-image upload accepted", res.StatusCode, body)
	}
	res, body = upload(svg, f.key.Key)
	var result struct {
		Attachment mailstore.DocumentAttachment `json:"attachment"`
	}
	if res.StatusCode != 200 || json.Unmarshal([]byte(body), &result) != nil || result.Attachment.ContentType != "image/svg+xml" {
		t.Fatal(res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "GET", result.Attachment.URL, f.key.Key, "", nil)
	if res.StatusCode != 200 || body != svg || res.Header.Get("Cache-Control") != "private, no-cache" || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("image response lost bytes or cache/security headers", res.StatusCode, body)
	}
	etag := res.Header.Get("ETag")
	res, body = keyRequest(t, f, "GET", result.Attachment.URL, f.key.Key, "", map[string]string{"If-None-Match": etag})
	if res.StatusCode != 304 || body != "" {
		t.Fatal("conditional image request transferred bytes", res.StatusCode, body)
	}
	res, _ = keyRequest(t, f, "GET", result.Attachment.URL, f.otherKey.Key, "", map[string]string{"If-None-Match": etag})
	if res.StatusCode != 404 {
		t.Fatal("conditional request bypassed ownership check", res.StatusCode)
	}
	// Existing snapshots can reference attachments; foreign URLs cannot be saved.
	body = strings.Replace(commercialAPITestCanvas(), `"data":{`, `"data":{"header":[{"type":"image","value":"`+result.Attachment.URL+`"}],`, 1)
	payload, _ := json.Marshal(map[string]any{"type": "quotation", "name": "With image", "html": body})
	res, body = keyRequest(t, f, "POST", "/api/documents", f.key.Key, string(payload), nil)
	if res.StatusCode != 200 || strings.Contains(body, "data:image/") {
		t.Fatal(res.StatusCode, body)
	}
	res, _ = keyRequest(t, f, "POST", "/api/documents", f.otherKey.Key, string(payload), nil)
	if res.StatusCode != 422 {
		t.Fatal("foreign image reference accepted by document save", res.StatusCode)
	}
	_, _, err := f.db.GetDocumentAttachment(context.Background(), f.user.ID, result.Attachment.ID)
	if err != nil {
		t.Fatal(err)
	}
}
