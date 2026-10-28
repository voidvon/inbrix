package web

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestQuotationTotalWithoutTaxFields(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	body := businessCanvas(map[string]string{
		"quote_number": "SP-20261028-F85E992F", "currency": "USD",
		"item_qty_0": "1", "item_price_0": "3731.00", "item_amount_0": "3731.00", "total_amount": "",
	})
	normalized, report, err := normalizeCommercialDocument(context.Background(), h.mailDB, account.OwnerID, body, "quotation", nil)
	if err != nil {
		t.Fatal(err)
	}
	values, items, _ := canvasDocumentContent(normalized)
	visible, _, _ := canvasProjectionContent(normalized)
	if values["total_amount"] != "3731.00" || visible["total_amount"] != "3731.00" || items[0].Amount != "3731.00" {
		t.Fatal("missing quotation total", values, visible, items)
	}
	for _, issue := range report.Issues {
		if retiredDocumentFields[issue.Field] {
			t.Fatal("retired tax field blocked total", issue)
		}
	}
	for key := range retiredDocumentFields {
		if _, exists := values[key]; exists {
			t.Fatal("retired field survived", key)
		}
	}
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
	if actual["total_amount"] != "200.00" || visible["total_amount"] != "200.00" || lines[0].Amount != "200.00" || lines[0].LineID != items[0].LineID {
		t.Fatal(actual, visible, lines)
	}
	// Missing commercial terms still keep an otherwise priced document in draft.
	incomplete := withDocumentBusiness(t, normalized, map[string]string{"payment_terms": ""})
	_, report, err = normalizeCommercialDocument(context.Background(), h.mailDB, account.OwnerID, incomplete, "quotation", nil)
	if err != nil || report.Status != "draft" {
		t.Fatal(report, err)
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Code == "missing_field" && issue.Field == "payment_terms" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing payment terms were not explained", report)
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
	if values["total_amount"] != "200.00" {
		t.Fatal("save trusted fake total", values)
	}
	missing := withDocumentBusiness(t, doc.HTML, map[string]string{"payment_terms": ""})
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

func TestBlankDocumentLinesCanBeSavedAndValidated(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	controller := NewDocumentsHandler(h.sessions, h.mailDB)
	app.Post("/api/documents", controller.HandleSaveDocument)
	app.Post("/api/documents/validate", controller.HandleValidateDocument)
	for _, kind := range []string{"quotation", "contract"} {
		for _, lines := range [][]AIDocumentItem{
			{{LineID: "blank"}},
			{{LineID: "blank"}, {LineID: "filled", Model: "VALVE", Qty: "1", Price: "100"}},
			{{LineID: "filled", Model: "VALVE", Qty: "1", Price: "100"}, {LineID: "blank"}},
		} {
			for _, numbering := range []int{-1, 0, 1} {
				if numbering == -1 && len(lines) > 1 {
					continue
				}
				var rows [][]map[string]any
				for i, line := range lines {
					var cells []map[string]any
					for _, field := range []string{"item_model", "item_description", "item_qty", "item_price", "item_amount"} {
						id := field
						if numbering >= 0 {
							id = fmt.Sprintf("%s_%d", field, i+numbering)
						}
						cells = append(cells, controlElement(id, canvasItemValues(line)[field]))
					}
					rows = append(rows, cells)
				}
				payload := map[string]any{
					"template": "spirax-" + kind,
					"options":  map[string]any{"watermark": map[string]any{"data": "KEEP WATERMARK"}},
					"data":     map[string]any{"main": []any{map[string]any{"value": "KEEP HUMAN TEXT"}, tableElement(rows)}},
				}
				if err := setCanvasBusiness(payload, map[string]string{}, lines); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(payload)
				body := canvasDocumentPrefix + string(raw)
				for _, path := range []string{"/api/documents/validate", "/api/documents"} {
					encoded, _ := json.Marshal(saveDocumentInput{Type: kind, Name: "Blank draft", HTML: body})
					req := httptest.NewRequest("POST", path, strings.NewReader(string(encoded)))
					req.Header.Set("Content-Type", "application/json")
					res, err := app.Test(req)
					if err != nil {
						t.Fatal(err)
					}
					result, _ := io.ReadAll(res.Body)
					res.Body.Close()
					if res.StatusCode != 200 {
						t.Fatalf("%s %s numbering=%d lines=%+v: %d %s", kind, path, numbering, lines, res.StatusCode, result)
					}
					var reply struct {
						HTML       string                   `json:"html"`
						Document   mailstore.DocumentRecord `json:"document"`
						Validation DocumentValidation       `json:"validation"`
					}
					if err := json.Unmarshal(result, &reply); err != nil {
						t.Fatal(err)
					}
					if reply.Document.ID != "" {
						reopened, err := h.mailDB.GetDocument(context.Background(), account.OwnerID, reply.Document.ID)
						if err != nil {
							t.Fatal(err)
						}
						reply.HTML = reopened.HTML
					}
					_, stored, ok := canvasDocumentContent(reply.HTML)
					_, visible, _ := canvasProjectionContent(reply.HTML)
					if !ok || len(stored) != len(lines) || len(visible) != len(lines) || reply.Validation.Status != "draft" {
						t.Fatalf("blank draft lost its lines: %+v", reply)
					}
					for i, line := range lines {
						if stored[i].LineID != line.LineID || stored[i].Model != line.Model || stored[i].Qty != line.Qty || stored[i].Price != line.Price || visible[i].Model != line.Model {
							t.Fatalf("line %d was removed, shifted or changed: %+v %+v", i, stored, visible)
						}
					}
					for _, marker := range []string{"KEEP HUMAN TEXT", "KEEP WATERMARK"} {
						if !strings.Contains(reply.HTML, marker) {
							t.Fatal("lost existing document content", marker)
						}
					}
				}
			}
		}
	}
}
