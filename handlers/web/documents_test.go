package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/handlers/web"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

func TestDocumentListOmitsContentAndDetailKeepsIt(t *testing.T) {
	f := newAPIKeyFixture(t)
	ctx := context.Background()
	largeHTML := "__INBRIX_CANVAS_DOCUMENT__:" + strings.Repeat("EMBEDDED-IMAGE-AND-CANVAS-DATA", 10000)
	for _, doc := range []mailstore.DocumentRecord{
		{ID: "quote", OwnerID: f.user.ID, Type: "quotation", Name: "Quote", Company: "Buyer", HTML: largeHTML},
		{ID: "contract", OwnerID: f.user.ID, Type: "contract", Name: "Contract", Company: "Buyer", HTML: largeHTML},
		{ID: "foreign", OwnerID: f.other.ID, Type: "quotation", Name: "Foreign private quote", HTML: largeHTML},
	} {
		if _, err := f.db.SaveDocument(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	for _, filter := range []string{"", "?type=quotation", "?type=contract"} {
		res, body := keyRequest(t, f, "GET", "/api/documents"+filter, f.key.Key, "", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatal(res.StatusCode, body)
		}
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("document lists must not be cached")
		}
		var result struct {
			Documents []map[string]json.RawMessage `json:"documents"`
		}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatal(err)
		}
		expectedCount := 1
		if filter == "" {
			expectedCount = 2
		}
		if len(result.Documents) != expectedCount || len(body) > 1500 || strings.Contains(body, "EMBEDDED-IMAGE") || strings.Contains(body, "Foreign private") {
			t.Fatalf("list contains excess content: count=%d bytes=%d", len(result.Documents), len(body))
		}
		for _, doc := range result.Documents {
			for _, field := range []string{"id", "revision", "name", "company", "type", "createdAt", "updatedAt"} {
				if _, ok := doc[field]; !ok {
					t.Fatalf("missing list field %s", field)
				}
			}
			if len(doc) != 7 {
				t.Fatalf("unexpected list fields: %v", doc)
			}
			if filter != "" && string(doc["type"]) != `"`+strings.TrimPrefix(filter, "?type=")+`"` {
				t.Fatalf("type filter ignored: %s", doc["type"])
			}
		}
	}
	res, body := keyRequest(t, f, "GET", "/api/documents/quote", f.key.Key, "", nil)
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("document details must not be cached")
	}
	var detail struct {
		Document mailstore.DocumentRecord `json:"document"`
	}
	if err := json.Unmarshal([]byte(body), &detail); err != nil || res.StatusCode != http.StatusOK || detail.Document.HTML != largeHTML || detail.Document.Revision != 1 {
		t.Fatalf("detail content changed: status=%d err=%v", res.StatusCode, err)
	}
	res, _ = keyRequest(t, f, "GET", "/api/documents/quote", f.otherKey.Key, "", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatal("foreign detail access accepted", res.StatusCode)
	}
}

func TestDocumentsHandlerEndpoints(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	user, err := db.CreateUser(ctx, "doc-web-user@example.com", "Web User", "hash")
	if err != nil {
		t.Fatal(err)
	}

	h := web.NewDocumentsHandler(nil, db)

	app := fiber.New()
	// Middleware injecting user_id to simulate authenticated session
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", user.ID)
		return c.Next()
	})

	app.Get("/api/documents", h.HandleListDocuments)
	app.Post("/api/documents", h.HandleSaveDocument)
	app.Get("/api/documents/:id", h.HandleGetDocument)
	app.Delete("/api/documents/:id", h.HandleDeleteDocument)
	app.Post("/api/documents/batch-delete", h.HandleBatchDeleteDocuments)

	app.Get("/api/document-templates", h.HandleListTemplates)
	app.Post("/api/document-templates", h.HandleSaveTemplate)
	app.Delete("/api/document-templates/:id", h.HandleDeleteTemplate)

	app.Get("/api/document-stamps", h.HandleListStamps)
	app.Post("/api/document-stamps", h.HandleSaveStamp)
	app.Delete("/api/document-stamps/:id", h.HandleDeleteStamp)

	// 1. Save document
	savePayload := map[string]string{
		"id":      "doc-web-1",
		"type":    "quotation",
		"name":    "Web 报价单",
		"company": "测试企业",
		"html":    commercialAPITestCanvas(),
	}
	payloadBytes, _ := json.Marshal(savePayload)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/documents failed: code=%d err=%v", resp.StatusCode, err)
	}

	// 2. Get document
	req = httptest.NewRequest(http.MethodGet, "/api/documents/doc-web-1", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/documents/:id failed: code=%d err=%v", resp.StatusCode, err)
	}

	// 3. List documents
	req = httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/documents failed: code=%d err=%v", resp.StatusCode, err)
	}
	var listResp struct {
		Documents []mailstore.DocumentRecord `json:"documents"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listResp)
	if len(listResp.Documents) != 1 || listResp.Documents[0].Name != "Web 报价单" {
		t.Errorf("list documents response mismatch: %+v", listResp)
	}

	// 4. Batch delete
	batchDelPayload := map[string][]string{
		"ids": {"doc-web-1"},
	}
	delBytes, _ := json.Marshal(batchDelPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/documents/batch-delete", bytes.NewReader(delBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/documents/batch-delete failed: code=%d err=%v", resp.StatusCode, err)
	}

	// Verify empty list
	req = httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	resp, _ = app.Test(req)
	var emptyResp struct {
		Documents []mailstore.DocumentRecord `json:"documents"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&emptyResp)
	if len(emptyResp.Documents) != 0 {
		t.Errorf("expected 0 documents after delete, got %d", len(emptyResp.Documents))
	}

	// 5. Template CRUD
	tplPayload := map[string]string{
		"id":   "tpl-web-1",
		"type": "contract",
		"name": "专属模板",
		"html": "<div>custom tpl</div>",
	}
	tplBytes, _ := json.Marshal(tplPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/document-templates", bytes.NewReader(tplBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/document-templates failed: code=%d", resp.StatusCode)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/document-templates", nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/document-templates failed: code=%d", resp.StatusCode)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/document-templates/tpl-web-1", nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/document-templates/:id failed: code=%d", resp.StatusCode)
	}

	// 6. Stamp CRUD
	stampPayload := map[string]any{
		"id":          "stamp-web-1",
		"name":        "印章测试",
		"dataUrl":     "data:image/png;base64,123",
		"width":       100,
		"height":      100,
		"aspectRatio": 1.0,
	}
	stampBytes, _ := json.Marshal(stampPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/document-stamps", bytes.NewReader(stampBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = app.Test(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/document-stamps failed: code=%d", resp.StatusCode)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/document-stamps", nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/document-stamps failed: code=%d", resp.StatusCode)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/document-stamps/stamp-web-1", nil)
	resp, _ = app.Test(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/document-stamps/:id failed: code=%d", resp.StatusCode)
	}
}

func commercialAPITestCanvas() string {
	return `__INBRIX_CANVAS_DOCUMENT__:{"template":"document","business":{"version":2,"values":{},"calculated":{},"items":[]},"data":{"main":[{"value":"API document"}]}}`
}
