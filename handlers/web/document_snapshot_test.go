package web

import (
	"context"
	"encoding/json"
	"inbrix/mailstore"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func readyCommercialTestDocument() string {
	return businessCanvas(map[string]string{
		"customer_company": "Buyer", "seller_company": "Seller", "currency": "CNY", "payment_terms": "Net30", "lead_time": "2 weeks",
		"quote_number": "Q-1", "issue_date": "2026-10-27", "validity": "30 days",
		"item_model_0": "A", "item_description_0": "Valve", "item_qty_0": "2", "item_price_0": "100", "item_amount_0": "999",
		"tax_rate": "13%", "tax_mode": "excluded", "subtotal": "999", "tax_amount": "999", "total_amount": "999",
	})
}

func TestCanonicalSnapshotIgnoresProjectionAndRecalculatesOutputs(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	body := readyCommercialTestDocument()
	// A layout edit cannot silently change business facts on the server.
	body = strings.Replace(body, `"value":"100"`, `"value":"1"`, 1)
	values, items, ok := canvasDocumentContent(body)
	if !ok || items[0].Price != "100" {
		t.Fatal("projection changed canonical price", values, items)
	}
	normalized, report, err := normalizeCommercialDocument(context.Background(), h.mailDB, account.OwnerID, body, "quotation", nil)
	if err != nil || report.Status != "ready" {
		t.Fatal(report, err)
	}
	actual, lines, _ := canvasDocumentContent(normalized)
	visible, _, _ := canvasProjectionContent(normalized)
	if actual["total_amount"] != "226.00" || visible["total_amount"] != "226.00" || lines[0].Amount != "200.00" || lines[0].LineID != items[0].LineID {
		t.Fatal(actual, visible, lines)
	}
	// The same explicit rate without included/excluded mode is still incomplete.
	incomplete := withDocumentBusiness(t, normalized, map[string]string{"tax_mode": ""})
	_, report, err = normalizeCommercialDocument(context.Background(), h.mailDB, account.OwnerID, incomplete, "quotation", nil)
	if err != nil || report.Status != "draft" {
		t.Fatal(report, err)
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Code == "missing_tax" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing tax mode was not explained", report)
	}
	for _, invalid := range []string{
		canvasDocumentPrefix + `{"data":{"main":[{"value":"x"}]}}`,
		canvasDocumentPrefix + `{"business":{"version":1,"values":{}},"data":{"main":[{"value":"x"}]}}`,
	} {
		if _, _, ok := canvasDocumentContent(invalid); ok {
			t.Fatal("accepted legacy snapshot")
		}
	}
}

func TestCommandsDeleteAndAddWithoutReconstructingLayout(t *testing.T) {
	body := canvasBody(t, "spirax-quotation", []map[string]any{tableElement([][]map[string]any{{controlElement("item_model_0", "A"), controlElement("item_qty_0", "1"), controlElement("item_price_0", "10"), controlElement("item_amount_0", "10")}})})
	_, lines, _ := canvasDocumentContent(body)
	updates, items, err := applyDocumentCommands(body, []DocumentCommand{{Type: "remove_line", LineID: lines[0].LineID}})
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	empty, err := patchStoredCanvasDocument(body, updates, items, true)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]json.RawMessage{"model": json.RawMessage(`"B"`), "qty": json.RawMessage(`"2"`), "price": json.RawMessage(`"20"`)}
	updates, items, err = applyDocumentCommands(empty, []DocumentCommand{{Type: "add_line", Fields: fields}})
	if err != nil || len(items) != 1 || items[0].LineID == lines[0].LineID {
		t.Fatal(items, err)
	}
	if _, err := patchStoredCanvasDocument(empty, updates, items, true); err != nil {
		t.Fatal("could not reuse line table prototype", err)
	}
}

func TestSaveAndDeliveryValidationUseSameCurrentSnapshot(t *testing.T) {
	h, app, _, other := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	controller := NewDocumentsHandler(h.sessions, h.mailDB)
	app.Post("/api/documents", controller.HandleSaveDocument)
	app.Post("/api/documents/validate", controller.HandleValidateDocument)
	post := func(path string, input any, foreign bool) (int, map[string]json.RawMessage) {
		raw, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		if foreign {
			req.Header.Set("X-Test-Outsider", other.Email)
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		result := map[string]json.RawMessage{}
		if res.StatusCode == 200 {
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode, result
	}
	status, saved := post("/api/documents", saveDocumentInput{Type: "quotation", Name: "Q", HTML: readyCommercialTestDocument()}, false)
	if status != 200 {
		t.Fatal(status)
	}
	var doc mailstore.DocumentRecord
	_ = json.Unmarshal(saved["document"], &doc)
	var report DocumentValidation
	_ = json.Unmarshal(saved["validation"], &report)
	if report.Status != "ready" {
		t.Fatal(report)
	}
	values, _, _ := canvasDocumentContent(doc.HTML)
	if values["total_amount"] != "226.00" {
		t.Fatal("save trusted fake total", values)
	}
	missing := withDocumentBusiness(t, doc.HTML, map[string]string{"tax_mode": ""})
	status, checked := post("/api/documents/validate", saveDocumentInput{ID: doc.ID, Revision: doc.Revision, Type: doc.Type, HTML: missing}, false)
	_ = json.Unmarshal(checked["validation"], &report)
	if status != 200 || report.Status != "draft" {
		t.Fatal(status, report)
	}
	if status, _ := post("/api/documents/validate", saveDocumentInput{ID: doc.ID, Revision: doc.Revision + 1, Type: doc.Type, HTML: doc.HTML}, false); status != 409 {
		t.Fatal("stale delivery check", status)
	}
	if status, _ := post("/api/documents/validate", saveDocumentInput{ID: doc.ID, Revision: doc.Revision, Type: doc.Type, HTML: doc.HTML}, true); status != 404 {
		t.Fatal("foreign delivery check", status)
	}
	// An incomplete document is still a valid saved draft.
	status, saved = post("/api/documents", saveDocumentInput{ID: doc.ID, Revision: doc.Revision, Type: doc.Type, Name: doc.Name, HTML: missing}, false)
	_ = json.Unmarshal(saved["validation"], &report)
	if status != 200 || report.Status != "draft" {
		t.Fatal(status, report)
	}
}
