package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"inbrix/handlers/web"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

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
		"html":    "<div>web quote</div>",
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
