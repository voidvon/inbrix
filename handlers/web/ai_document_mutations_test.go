package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestEditorAndAgentShareValidatedDocumentMutation(t *testing.T) {
	calls := 0
	reply := `{"values":{},"items":[{"model":"MANUAL","qty":"2","price":"100.00","amount":"1.00"}]}`
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		calls++
		if !strings.Contains(request.Input, "HUMAN TERM") || !strings.Contains(request.Input, "100.00") {
			t.Error("missing actual current document")
		}
		return reply
	})
	app.Post("/api/ai/write-document", h.HandleWriteDocument)
	body := businessCanvas(map[string]string{"item_model": "MANUAL", "item_qty": "1", "item_price": "100.00", "item_amount": "100.00", "total_amount": "100.00", "say_total": "人民币壹佰元整", "tax_rate": "0%"})
	var payload map[string]any
	json.Unmarshal([]byte(strings.TrimPrefix(body, canvasDocumentPrefix)), &payload)
	payload["options"] = map[string]any{"watermark": map[string]any{"data": "WATERMARK"}}
	data := payload["data"].(map[string]any)
	data["main"] = append(data["main"].([]any), map[string]any{"value": "HUMAN TERM", "bold": true})
	data["header"] = []any{map[string]any{"value": "CUSTOM HEADER"}}
	data["footer"] = []any{map[string]any{"type": "image", "value": "CUSTOM STAMP", "width": 90}}
	raw, _ := json.Marshal(payload)
	body = canvasDocumentPrefix + string(raw)
	post := func(input aiDocumentInput) (int, map[string]any) {
		t.Helper()
		input.AccountEmail = account.Email
		input.DocumentType = "contract"
		encoded, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", "/api/ai/write-document", strings.NewReader(string(encoded)))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		bytes, _ := io.ReadAll(res.Body)
		var result map[string]any
		if res.StatusCode == 200 {
			if err := json.Unmarshal(bytes, &result); err != nil {
				t.Fatal(err)
			}
		} else {
			result = map[string]any{"error": string(bytes)}
		}
		return res.StatusCode, result
	}
	status, result := post(aiDocumentInput{Mode: "variables", Canvas: body, Instruction: "数量改成 2"})
	if status != 200 {
		t.Fatal(status, result)
	}
	html := result["html"].(string)
	values, items, ok := canvasDocumentContent(html)
	if !ok || items[0].Amount != "200.00" || values["total_amount"] != "200.00" || values["say_total"] != "" {
		t.Fatal("unvalidated amounts", result)
	}
	for _, marker := range []string{"HUMAN TERM", "CUSTOM HEADER", "CUSTOM STAMP", "WATERMARK"} {
		if !strings.Contains(html, marker) {
			t.Fatal("lost", marker)
		}
	}
	if !strings.Contains(result["summary"].(string), "请核对后补填") {
		t.Fatal("missing amount words notice")
	}
	record, err := h.mailDB.SaveDocument(context.Background(), mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "contract", Name: "Shared", HTML: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	actionResult, _, err := h.executeUpdateDocument(context.Background(), account.OwnerID, map[string]any{"id": record.ID, "items": []any{map[string]any{"model": "MANUAL", "qty": "2", "price": "100.00", "amount": "1.00"}}}, mailstore.AIModelRecord{}, "")
	if err != nil || actionResult.(*AIDocumentAction).HTML != html {
		t.Fatal("editor and agent diverged", err)
	}
	status, result = post(aiDocumentInput{Mode: "apply", Canvas: body, Values: map[string]string{"total_amount": "999"}, Items: []AIDocumentItem{{Model: "MANUAL", Qty: "2", Price: "100", Amount: "1"}}})
	if status != 200 || result["values"].(map[string]any)["total_amount"] != "200.00" || calls != 1 {
		t.Fatal("variable form did not use deterministic path", status, result, calls)
	}
	status, result = post(aiDocumentInput{Mode: "apply", Canvas: body, Values: map[string]string{"payment_terms": "Net60"}})
	if status != 422 {
		t.Fatal("missing controls accepted", status, result)
	}
	status, _ = post(aiDocumentInput{Mode: "variables", Variables: []aiVariableItem{{ConceptID: "total_amount"}}, Instruction: "change"})
	if status != 422 || calls != 1 {
		t.Fatal("legacy unvalidated mutation still allowed", status, calls)
	}
	if err := h.mailDB.SaveAccountBrands(context.Background(), account.OwnerID, account.ID, mailstore.AccountBrands{BrandMode: "restrict", Brands: []string{"A"}}); err != nil {
		t.Fatal(err)
	}
	status, _ = post(aiDocumentInput{Mode: "apply", Canvas: body, Values: map[string]string{"total_amount": "999"}})
	if status != 422 {
		t.Fatal("editor bypassed mailbox scope", status)
	}
	saved, _ := h.mailDB.GetDocument(context.Background(), account.OwnerID, record.ID)
	if saved.HTML != body {
		t.Fatal("proposal saved without applying")
	}
}

func TestFreightAmountsAcrossToolsAndUpdates(t *testing.T) {
	for _, tt := range []struct{ name, freight, rate, mode, total, tax string }{
		{"absent", "", "", "", "100.00", "0.00"},
		{"zero", "0", "", "", "100.00", "0.00"},
		{"unknown", "20", "", "", "", ""},
		{"missing mode", "20", "0%", "", "", ""},
		{"zero rated", "20", "0%", "included", "120.00", "0.00"},
		{"included", "20", "9%", "included", "120.00", "1.65"},
		{"excluded", "20", "9%", "excluded", "121.80", "1.80"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{"items": []any{map[string]any{"model": "MANUAL", "qty": "1", "price": "100"}}, "tax_rate": "0%", "inland_freight": tt.freight, "freight_tax_rate": tt.rate, "freight_tax_mode": tt.mode}
			computed, _, err := executeCalculatePricing(args)
			if err != nil {
				t.Fatal(err)
			}
			generated, _, err := executeGenerateDocument(args)
			if err != nil {
				t.Fatal(err)
			}
			card := generated.(*AIDocumentCard)
			if card.Total != tt.total || card.TaxAmount != tt.tax || computed.(map[string]any)["total"] != tt.total {
				t.Fatal(card, computed)
			}
		})
	}
	card := &AIDocumentCard{Values: map[string]string{"tax_rate": "0%", "discount": "10", "inland_freight": "20", "freight_tax_rate": "0%", "freight_tax_mode": "included"}, Items: []AIDocumentItem{{Qty: "1", Price: "100"}}}
	if err := calculateDocumentAmounts(card, nil); err != nil || card.Total != "110.00" || card.Subtotal != "100.00" {
		t.Fatal("discount applied to freight", card, err)
	}
	for _, invalid := range []map[string]any{{"inland_freight": "-1"}, {"inland_freight": "abc"}, {"freight_tax_rate": "150%"}, {"freight_tax_mode": "guess"}} {
		invalid["items"] = []any{map[string]any{"qty": "1", "price": "100"}}
		if _, _, err := executeCalculatePricing(invalid); err == nil {
			t.Fatal("invalid freight accepted", invalid)
		}
	}
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	body := businessCanvas(map[string]string{"item_model": "MANUAL", "item_qty": "1", "item_price": "100", "item_amount": "100.00", "total_amount": "100.00", "say_total": "人民币壹佰元整", "tax_rate": "0%", "inland_freight": "0"})
	for _, terms := range []map[string]string{{"inland_freight": "20"}, {"inland_freight": "20", "freight_tax_rate": "0%", "freight_tax_mode": "included"}} {
		action, err := h.proposeCanvasDocumentUpdate(context.Background(), account.OwnerID, body, terms, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		if terms["freight_tax_mode"] != "" {
			want = "120.00"
		}
		if action.Values["total_amount"] != want || action.Values["say_total"] != "" {
			t.Fatal("freight failed to invalidate totals", action)
		}
		reopened, _, ok := canvasDocumentContent(action.HTML)
		if !ok || reopened["inland_freight"] != "20" || reopened["freight_tax_mode"] != terms["freight_tax_mode"] {
			t.Fatal("freight lost after patch", reopened)
		}
	}
	historical := businessCanvas(map[string]string{"item_model": "MANUAL", "item_qty": "1", "item_price": "100", "total_amount": "120.00", "inland_freight": "20", "customer_contact": "Old"})
	contact, err := h.proposeCanvasDocumentUpdate(context.Background(), account.OwnerID, historical, map[string]string{"customer_contact": "New"}, nil)
	if err != nil || contact.Values["total_amount"] != "120.00" || strings.Contains(contact.Summary, "最终金额留空") {
		t.Fatal("contact-only historical update repriced or claimed clearing", contact, err)
	}

}
