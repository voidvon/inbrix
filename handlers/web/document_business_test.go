package web

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func withDocumentBusiness(t *testing.T, body string, values map[string]string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(body, canvasDocumentPrefix)), &payload); err != nil {
		t.Fatal(err)
	}
	if err := setCanvasBusiness(payload, values); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return canvasDocumentPrefix + string(raw)
}

func TestDocumentCommercialRoundtrip(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	fields := map[string]string{"customer_company": "Buyer", "item_model": "MANUAL", "item_description": "Manual valve", "item_qty": "1", "item_price": "100.00", "item_amount": "100.00", "total_amount": "90.00"}
	terms := map[string]string{"tax_rate": "13%", "tax_mode": "included", "discount": "10.00", "subtotal": "100.00", "tax_amount": "10.35", "total_amount": "90.00", "currency": "CNY"}
	samples := []struct{ Type, HTML string }{{"quotation", withDocumentBusiness(t, businessCanvas(fields), terms)}, {"contract", withDocumentBusiness(t, businessCanvas(fields), terms)}}
	// The browser check exports real default canvases after first save, editor
	// save and reload. Feed those exact records through the real update tool too.
	if path := os.Getenv("INBRIX_ROUNDTRIP_RECORDS"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &samples); err != nil {
			t.Fatal(err)
		}
		if len(samples) < 2 {
			t.Fatal("missing browser records")
		}
	}
	for _, sample := range samples {
		t.Run(sample.Type, func(t *testing.T) {
			record, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: sample.Type, Name: "Roundtrip", HTML: sample.HTML}, 0)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				result, _, err := h.executeUpdateDocument(ctx, account.OwnerID, map[string]any{"id": record.ID, "items": []any{map[string]any{"model": "MANUAL", "description": "Manual valve", "qty": "2", "price": "100.00"}}}, mailstore.AIModelRecord{}, "")
				if err != nil {
					t.Fatal(err)
				}
				action := result.(*AIDocumentAction)
				values, items, ok := canvasDocumentContent(action.HTML)
				if !ok || len(items) != 1 || items[0].Amount != "200.00" || values["total_amount"] != "190.00" || values["discount"] != "10.00" || values["tax_mode"] != "included" || values["tax_rate"] != "13%" {
					t.Fatalf("lost commercial basis: %+v %+v", values, items)
				}
				record.HTML = action.HTML
				record, err = h.mailDB.SaveDocument(ctx, *record, record.Revision)
				if err != nil {
					t.Fatal(err)
				}
			}
			if sample.Type == "quotation" {
				card := &AIDocumentCard{Type: "contract", SourceQuotationID: record.ID, Values: map[string]string{"discount": "999"}}
				if err := h.applyQuotationSnapshot(ctx, account.OwnerID, card); err != nil {
					t.Fatal(err)
				}
				if card.Values["discount"] != "10.00" || card.Values["tax_mode"] != "included" || card.Values["source_quotation_id"] != record.ID || card.Total != "190.00" {
					t.Fatalf("lost quotation terms: %+v", card)
				}
			}
		})
	}
}

func TestBusinessMetadataControlPrecedenceAndVersions(t *testing.T) {
	body := withDocumentBusiness(t, businessCanvas(map[string]string{"tax_rate": "", "discount": ""}), map[string]string{"tax_rate": "13%", "tax_mode": "included", "discount": "10.00", "payment_terms": "Net60"})
	values, _, ok := canvasDocumentContent(body)
	if !ok || values["tax_rate"] != "" || values["discount"] != "" || values["tax_mode"] != "included" {
		t.Fatalf("empty controls did not win: %+v", values)
	}
	if _, exists := values["payment_terms"]; exists {
		t.Fatal("arbitrary invisible fields allowed")
	}
	for _, malformed := range []string{`null`, `{"version":2,"values":{}}`, `{"version":1}`, `{"version":1,"values":{"tax_rate":13}}`} {
		bad := canvasDocumentPrefix + `{"business":` + malformed + `,"data":{"main":[{"value":"x"}]}}`
		if malformed == "null" {
			continue
		} // null is treated as absent for legacy compatibility.
		if _, _, ok := canvasDocumentContent(bad); ok {
			t.Fatalf("unsupported metadata accepted: %s", bad)
		}
	}
	legacy := businessCanvas(map[string]string{"total_amount": "90.00"})
	values, _, ok = canvasDocumentContent(legacy)
	if !ok || values["tax_rate"] != "" || values["discount"] != "" {
		t.Fatal("legacy conditions inferred")
	}
	card := &AIDocumentCard{Values: map[string]string{"tax_rate": "13%", "tax_mode": "included", "discount": "10.00"}}
	custom, err := materializeBusinessTemplate(businessCanvas(map[string]string{"customer_company": "Old"}), card)
	if err != nil {
		t.Fatal(err)
	}
	values, _, _ = canvasDocumentContent(custom)
	if values["tax_mode"] != "included" || values["discount"] != "10.00" {
		t.Fatal("custom template lost terms")
	}
}

func TestDocumentUpdateRejectsMissingScalarAndWritesNestedControls(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	body := canvasDocumentPrefix + `{"data":{"main":[{"value":"TEXT"},{"valueList":[{"type":"control","control":{"conceptId":"nested","value":[]}}]}],"header":[{"type":"control","control":{"conceptId":"header","value":[]}}],"footer":[{"type":"control","control":{"conceptId":"footer","value":[]}}]}}`
	record, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "Nested", HTML: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := h.executeUpdateDocument(ctx, account.OwnerID, map[string]any{"id": record.ID, "values": map[string]any{"header": "Changed", "payment_terms": "Net60"}}, mailstore.AIModelRecord{}, "")
	if err == nil || result != nil || !strings.Contains(err.Error(), "payment_terms") {
		t.Fatal("missing field silently accepted", result, err)
	}
	result, _, err = h.executeUpdateDocument(ctx, account.OwnerID, map[string]any{"id": record.ID, "values": map[string]any{"nested": "N", "header": "H", "footer": "F"}}, mailstore.AIModelRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	action := result.(*AIDocumentAction)
	for key, want := range map[string]string{"nested": "N", "header": "H", "footer": "F"} {
		if action.Values[key] != want {
			t.Fatalf("field not applied: %s %+v", key, action)
		}
	}
	saved, _ := h.mailDB.GetDocument(ctx, account.OwnerID, record.ID)
	if saved.HTML != body {
		t.Fatal("proposal was not atomic/read-only")
	}
}

func TestDocumentAmountWordsInvalidated(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	fields := map[string]string{"customer_company": "Buyer", "item_model": "MANUAL", "item_qty": "1", "item_price": "100.00", "item_amount": "100.00", "subtotal": "100.00", "total_amount": "100.00", "tax_rate": "0%", "say_total": "人民币壹佰元整", "currency": "CNY"}
	record, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "contract", Name: "Words", HTML: businessCanvas(fields)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		args map[string]any
		keep bool
	}{
		{"contact", map[string]any{"values": map[string]any{"customer_company": "New buyer"}}, true},
		{"same total", map[string]any{"items": []any{map[string]any{"model": "MANUAL", "qty": "2", "price": "50"}}}, true},
		{"changed total", map[string]any{"items": []any{map[string]any{"model": "MANUAL", "qty": "2", "price": "100"}}}, false},
		{"unknown total", map[string]any{"values": map[string]any{"tax_rate": ""}}, false},
		{"guessed totals cannot validate words", map[string]any{"values": map[string]any{"total_amount": "200.00"}, "items": []any{map[string]any{"model": "MANUAL", "qty": "2", "price": "100"}}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.args["id"] = record.ID
			result, _, err := h.executeUpdateDocument(ctx, account.OwnerID, tt.args, mailstore.AIModelRecord{}, "")
			if err != nil {
				t.Fatal(err)
			}
			action := result.(*AIDocumentAction)
			values, _, _ := canvasDocumentContent(action.HTML)
			if (values["say_total"] != "") != tt.keep || (!tt.keep && !strings.Contains(action.Summary, "请核对后补填")) {
				t.Fatalf("stale words or missing warning: %+v", action)
			}
		})
	}
}
