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
	"github.com/gofiber/fiber/v2/middleware/session"
)

type priceListAPI struct {
	app   *fiber.App
	store *mailstore.Store
}

// newPriceListStore opens an in-memory mirror with one user and returns the
// store plus that user's id.
func newPriceListStore(t *testing.T) (*mailstore.Store, string) {
	t.Helper()
	ctx := context.Background()
	store, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	user, err := store.CreateUser(ctx, "owner", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	return store, user.ID
}

func newPriceListApp(t *testing.T, store *mailstore.Store, userID string) *priceListAPI {
	t.Helper()
	sessStore := session.New()
	handler := web.NewPriceListHandler(sessStore, store)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if userID != "" {
			c.Locals("user_id", userID)
		}
		return c.Next()
	})
	app.Get("/api/price-lists", handler.HandleListPriceLists)
	app.Post("/api/price-lists", handler.HandleCreatePriceList)
	app.Post("/api/price-lists/lookup", handler.HandleLookupPrices)
	app.Get("/api/price-lists/:id", handler.HandleGetPriceList)
	app.Put("/api/price-lists/:id", handler.HandleUpdatePriceList)
	app.Delete("/api/price-lists/:id", handler.HandleDeletePriceList)
	app.Put("/api/price-lists/:id/columns", handler.HandleSavePriceColumns)
	app.Get("/api/price-lists/:id/rows", handler.HandleListPriceRows)
	app.Post("/api/price-lists/:id/rows", handler.HandleSavePriceRows)
	app.Post("/api/price-lists/:id/rows/append", handler.HandleAppendPriceRow)
	app.Patch("/api/price-lists/:id/rows/:rowId", handler.HandleUpdatePriceRowCells)
	app.Delete("/api/price-lists/:id/rows/:rowId", handler.HandleDeletePriceRow)
	app.Post("/api/price-lists/:id/import", handler.HandleImportPriceRows)
	app.Get("/api/price-lists/:id/export", handler.HandleExportPriceRows)
	return &priceListAPI{app: app, store: store}
}

func (api *priceListAPI) request(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := api.app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 0)
	buffer := bytes.NewBuffer(payload)
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, buffer.Bytes()
}

func (api *priceListAPI) decode(t *testing.T, method, path string, body any, target any) int {
	t.Helper()
	status, payload := api.request(t, method, path, body)
	if target != nil {
		if err := json.Unmarshal(payload, target); err != nil {
			t.Fatalf("%s %s: %v (%s)", method, path, err, payload)
		}
	}
	return status
}

func TestPriceListAPIRoundTrip(t *testing.T) {
	store, ownerID := newPriceListStore(t)
	api := newPriceListApp(t, store, ownerID)

	var created1 struct {
		PriceList mailstore.PriceList     `json:"priceList"`
		Columns   []mailstore.PriceColumn `json:"columns"`
	}
	if status := api.decode(t, http.MethodPost, "/api/price-lists", map[string]any{
		"name":        "2026 阀门价目表",
		"description": "标准价",
		"columns": []map[string]any{
			{"key": "model", "label": "型号", "type": "text", "role": "model", "width": 180},
			{"key": "price", "label": "单价", "type": "number", "role": "price", "width": 120},
			{"key": "brand", "label": "品牌", "type": "text", "width": 120},
		},
	}, &created1); status != http.StatusCreated {
		t.Fatalf("create status = %d", status)
	}
	listID := created1.PriceList.ID
	if listID == "" || len(created1.Columns) != 3 {
		t.Fatalf("created = %+v", created1)
	}

	var listed struct {
		PriceLists []mailstore.PriceList `json:"priceLists"`
	}
	if status := api.decode(t, http.MethodGet, "/api/price-lists", nil, &listed); status != http.StatusOK || len(listed.PriceLists) != 1 {
		t.Fatalf("list status = %d payload = %+v", status, listed)
	}

	// Save rows: a new row and a numeric cell written the way a spreadsheet
	// user would type it.
	if status := api.decode(t, http.MethodPost, "/api/price-lists/"+listID+"/rows", map[string]any{
		"rows": []map[string]any{
			{"data": map[string]string{"model": "DN50-BV", "price": "1,280.00", "brand": "Acme"}},
			{"data": map[string]string{"model": "DN80-BV", "price": "1980", "brand": "Acme"}},
		},
	}, nil); status != http.StatusOK {
		t.Fatalf("save rows status = %d", status)
	}

	var page struct {
		Rows  []mailstore.PriceRow `json:"rows"`
		Total int                  `json:"total"`
	}
	if status := api.decode(t, http.MethodGet, "/api/price-lists/"+listID+"/rows", nil, &page); status != http.StatusOK || page.Total != 2 {
		t.Fatalf("rows status = %d page = %+v", status, page)
	}
	if page.Rows[0].Data["price"] != "1280.00" {
		t.Fatalf("numeric cell not normalized: %+v", page.Rows[0].Data)
	}

	// Search and numeric sort.
	if status := api.decode(t, http.MethodGet, "/api/price-lists/"+listID+"/rows?q=DN80", nil, &page); status != http.StatusOK || page.Total != 1 {
		t.Fatalf("search status = %d total = %d", status, page.Total)
	}
	if status := api.decode(t, http.MethodGet, "/api/price-lists/"+listID+"/rows?sort=price&dir=asc", nil, &page); status != http.StatusOK || page.Rows[0].Data["model"] != "DN50-BV" {
		t.Fatalf("sort status = %d rows = %+v", status, page.Rows)
	}

	// Column rename keeps row data; a new column can be added in the same call.
	var columns struct {
		Columns []mailstore.PriceColumn `json:"columns"`
	}
	if status := api.decode(t, http.MethodPut, "/api/price-lists/"+listID+"/columns", map[string]any{
		"columns": []map[string]any{
			{"key": "model", "label": "产品型号", "type": "text", "role": "model", "width": 200},
			{"key": "price", "label": "含税单价", "type": "number", "role": "price", "width": 120},
			{"key": "brand", "label": "品牌", "type": "text", "width": 120},
			{"key": "lead_time", "label": "交期", "type": "number", "role": "lead_time", "width": 100},
		},
	}, &columns); status != http.StatusOK || len(columns.Columns) != 4 {
		t.Fatalf("columns status = %d columns = %+v", status, columns)
	}
	if status := api.decode(t, http.MethodGet, "/api/price-lists/"+listID+"/rows", nil, &page); status != http.StatusOK || page.Rows[0].Data["brand"] != "Acme" {
		t.Fatalf("row data lost after column edit: %+v", page.Rows)
	}

	// Lookup resolves the authoritative price through the roles.
	var lookup struct {
		Entries []mailstore.PriceLookupEntry `json:"entries"`
	}
	if status := api.decode(t, http.MethodPost, "/api/price-lists/lookup", map[string]any{"query": "DN50-BV"}, &lookup); status != http.StatusOK {
		t.Fatalf("lookup status = %d", status)
	}
	if len(lookup.Entries) != 1 || lookup.Entries[0].Price != 1280 || lookup.Entries[0].Model != "DN50-BV" {
		t.Fatalf("lookup = %+v", lookup.Entries)
	}

	// Import: dry run reports, then the real run writes, then upsert updates.
	var preview struct {
		Result mailstore.PriceImportResult `json:"result"`
	}
	if status := api.decode(t, http.MethodPost, "/api/price-lists/"+listID+"/import", map[string]any{
		"content": "产品型号,含税单价,品牌\nDN25-BV,¥9,Acme\n",
		"dryRun":  true,
	}, &preview); status != http.StatusOK || preview.Result.Added != 1 {
		t.Fatalf("dry run status = %d result = %+v", status, preview.Result)
	}
	if status := api.decode(t, http.MethodGet, "/api/price-lists/"+listID+"/rows", nil, &page); status != http.StatusOK || page.Total != 2 {
		t.Fatalf("dry run wrote rows: %d", page.Total)
	}
	if status := api.decode(t, http.MethodPost, "/api/price-lists/"+listID+"/import", map[string]any{
		"content": "产品型号,含税单价,品牌\nDN25-BV,9,Acme\n",
		"mode":    "upsert",
	}, &preview); status != http.StatusOK || preview.Result.Added != 1 {
		t.Fatalf("import status = %d result = %+v", status, preview.Result)
	}
	if status := api.decode(t, http.MethodPost, "/api/price-lists/"+listID+"/import", map[string]any{
		"content": "产品型号,含税单价\nDN25-BV,11\n",
		"mode":    "upsert",
	}, &preview); status != http.StatusOK || preview.Result.Updated != 1 {
		t.Fatalf("upsert status = %d result = %+v", status, preview.Result)
	}
	if status := api.decode(t, http.MethodPost, "/api/price-lists/lookup", map[string]any{"query": "DN25-BV"}, &lookup); status != http.StatusOK || len(lookup.Entries) != 1 || lookup.Entries[0].Price != 11 {
		t.Fatalf("lookup after upsert = %+v", lookup.Entries)
	}

	// Export is CSV with a BOM and the localized headers.
	status, payload := api.request(t, http.MethodGet, "/api/price-lists/"+listID+"/export", nil)
	if status != http.StatusOK || !strings.HasPrefix(string(payload), "\ufeff") || !strings.Contains(string(payload), "产品型号") {
		t.Fatalf("export status = %d payload = %q", status, string(payload[:min(len(payload), 60)]))
	}

	// Delete removes the table.
	if status := api.decode(t, http.MethodDelete, "/api/price-lists/"+listID, nil, nil); status != http.StatusOK {
		t.Fatalf("delete status = %d", status)
	}
	if status := api.decode(t, http.MethodGet, "/api/price-lists/"+listID, nil, nil); status != http.StatusNotFound {
		t.Fatalf("get after delete status = %d", status)
	}
}

func TestPriceListAPIRequiresSession(t *testing.T) {
	store, _ := newPriceListStore(t)
	api := newPriceListApp(t, store, "")
	if status := api.decode(t, http.MethodGet, "/api/price-lists", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
}

func TestPriceListAPIRejectsOtherOwners(t *testing.T) {
	ctx := context.Background()
	store, firstID := newPriceListStore(t)
	second, err := store.CreateUser(ctx, "second", "Second", "hash")
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := store.CreatePriceList(ctx, firstID, "私有价目", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	api := newPriceListApp(t, store, second.ID)
	if status := api.decode(t, http.MethodGet, "/api/price-lists/"+list.ID, nil, nil); status != http.StatusNotFound {
		t.Fatalf("cross-owner read status = %d, want 404", status)
	}
	var listed struct {
		PriceLists []mailstore.PriceList `json:"priceLists"`
	}
	if status := api.decode(t, http.MethodGet, "/api/price-lists", nil, &listed); status != http.StatusOK || len(listed.PriceLists) != 0 {
		t.Fatalf("cross-owner list status = %d payload = %+v", status, listed)
	}
}

func TestPriceListAPIValidatesInput(t *testing.T) {
	store, _ := newPriceListStore(t)
	api := newPriceListApp(t, store, "usr_missing")
	// A missing owner must not be able to create anything.
	if status := api.decode(t, http.MethodPost, "/api/price-lists", map[string]any{"name": "x"}, nil); status == http.StatusCreated {
		t.Fatal("created a price list for a non-existent owner")
	}
	// An empty name is rejected.
	if status := api.decode(t, http.MethodPost, "/api/price-lists", map[string]any{"name": "   "}, nil); status != http.StatusBadRequest {
		t.Fatalf("blank name status = %d, want 400", status)
	}
	// An unknown id is a 404, not a 500.
	if status := api.decode(t, http.MethodGet, "/api/price-lists/plist_missing/rows", nil, nil); status != http.StatusNotFound {
		t.Fatalf("unknown list status = %d, want 404", status)
	}
}

func TestPriceListAPICellAutosave(t *testing.T) {
	store, ownerID := newPriceListStore(t)
	api := newPriceListApp(t, store, ownerID)

	var created struct {
		PriceList mailstore.PriceList `json:"priceList"`
	}
	if status := api.decode(t, http.MethodPost, "/api/price-lists", map[string]any{"name": "自动保存"}, &created); status != http.StatusCreated {
		t.Fatalf("create status = %d", status)
	}
	listID := created.PriceList.ID

	// The grid appends a row once, then only sends the cells it changed.
	var appended struct {
		Row mailstore.PriceRow `json:"row"`
	}
	if status := api.decode(t, http.MethodPost, "/api/price-lists/"+listID+"/rows/append", map[string]any{
		"data": map[string]string{"model": "DN50-BV", "price": "¥1,280.00"},
	}, &appended); status != http.StatusCreated {
		t.Fatalf("append status = %d", status)
	}
	rowID := appended.Row.ID
	if rowID == "" || appended.Row.Data["price"] != "1280.00" {
		t.Fatalf("appended row = %+v", appended.Row)
	}

	var patched struct {
		Row mailstore.PriceRow `json:"row"`
	}
	if status := api.decode(t, http.MethodPatch, "/api/price-lists/"+listID+"/rows/"+rowID, map[string]any{
		"cells": map[string]string{"lead_time": "15"},
	}, &patched); status != http.StatusOK {
		t.Fatalf("patch status = %d", status)
	}
	if patched.Row.Data["lead_time"] != "15" || patched.Row.Data["model"] != "DN50-BV" || patched.Row.Data["price"] != "1280.00" {
		t.Fatalf("cell patch clobbered the row: %+v", patched.Row.Data)
	}

	// An empty patch is a no-op rather than an error.
	if status := api.decode(t, http.MethodPatch, "/api/price-lists/"+listID+"/rows/"+rowID, map[string]any{"cells": map[string]string{}}, nil); status != http.StatusOK {
		t.Fatalf("empty patch status = %d", status)
	}
	// Patching a row that no longer exists is a 404 so the client can re-create it.
	if status := api.decode(t, http.MethodPatch, "/api/price-lists/"+listID+"/rows/prow_missing", map[string]any{
		"cells": map[string]string{"model": "x"},
	}, nil); status != http.StatusNotFound {
		t.Fatalf("missing row patch status = %d", status)
	}

	// Deleting one row is its own small request.
	if status := api.decode(t, http.MethodDelete, "/api/price-lists/"+listID+"/rows/"+rowID, nil, nil); status != http.StatusOK {
		t.Fatalf("delete status = %d", status)
	}
	if status := api.decode(t, http.MethodDelete, "/api/price-lists/"+listID+"/rows/"+rowID, nil, nil); status != http.StatusNotFound {
		t.Fatalf("second delete status = %d", status)
	}

	var page struct {
		Total int `json:"total"`
	}
	if status := api.decode(t, http.MethodGet, "/api/price-lists/"+listID+"/rows", nil, &page); status != http.StatusOK || page.Total != 0 {
		t.Fatalf("rows after delete: status=%d total=%d", status, page.Total)
	}
}
