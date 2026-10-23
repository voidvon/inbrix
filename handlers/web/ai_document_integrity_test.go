package web

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestDocumentUpdatePreservesSavedCanvas(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	body := canvasDocumentPrefix + `{"template":"custom","options":{"watermark":{"data":"WATERMARK"}},"custom":{"unknown":true},"data":{"header":[{"value":"HEADER","bold":true}],"footer":[{"type":"image","value":"STAMP","width":90}],"main":[{"value":"FREE TEXT","italic":true},{"type":"control","control":{"conceptId":"payment_terms","value":[{"value":"Net30","font":"Custom","bold":true}]}},{"type":"control","extension":{"keep":42},"control":{"conceptId":"untouched","value":[{"value":"A","bold":true},{"value":"B","italic":true}]}}]}}`
	ctx := context.Background()
	doc, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "Custom", HTML: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := h.executeUpdateDocument(ctx, account.OwnerID, map[string]any{"id": doc.ID, "values": map[string]any{"payment_terms": "Net60"}}, mailstore.AIModelRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	action := result.(*AIDocumentAction)
	var got, want any
	json.Unmarshal([]byte(strings.TrimPrefix(action.HTML, canvasDocumentPrefix)), &got)
	json.Unmarshal([]byte(strings.TrimPrefix(strings.Replace(body, "Net30", "Net60", 1), canvasDocumentPrefix)), &want)
	if !reflect.DeepEqual(got, want) || action.BaseRevision != doc.Revision {
		t.Fatalf("unrelated content changed: %s", action.HTML)
	}
	saved, _ := h.mailDB.GetDocument(ctx, account.OwnerID, doc.ID)
	if saved.HTML != body {
		t.Fatal("proposal mutated saved document")
	}
}

func TestDocumentPatchPreservesRowsAndResizesItems(t *testing.T) {
	control := func(id, value string) map[string]any {
		return map[string]any{"type": "control", "controlId": id, "control": map[string]any{"conceptId": id, "value": []any{map[string]any{"value": value, "font": "CUSTOM"}}}}
	}
	row := func(i int, label string) map[string]any {
		var els []any
		for key, value := range canvasItemValues(AIDocumentItem{Model: label, Qty: "1", Price: "10.00", Amount: "10.00"}) {
			if key == "item_model" || key == "item_qty" || key == "item_price" || key == "item_amount" {
				els = append(els, control(fmt.Sprintf("%s_%d", key, i), value))
			}
		}
		return map[string]any{"id": "row-" + label, "height": 40 + i, "customRow": label, "tdList": []any{map[string]any{"id": "cell-" + label, "backgroundColor": label, "value": append(els, map[string]any{"value": "ROW FREE TEXT " + label}, map[string]any{"type": "image", "value": "STAMP " + label})}}}
	}
	rows := []any{map[string]any{"tdList": []any{map[string]any{"value": []any{map[string]any{"value": "COLUMN HEADER"}}}}}, row(1, "A"), row(2, "B"), map[string]any{"tdList": []any{map[string]any{"value": []any{map[string]any{"value": "TABLE FOOTER"}}}}}}
	raw, _ := json.Marshal(map[string]any{"data": map[string]any{"main": []any{map[string]any{"type": "table", "colgroup": []any{map[string]any{"width": 240}}, "trList": rows}}}})
	body := canvasDocumentPrefix + string(raw)
	for _, n := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			items := make([]AIDocumentItem, n)
			for i := range items {
				items[i] = AIDocumentItem{Model: fmt.Sprint("NEW", i), Qty: "2", Price: "20.00", Amount: "40.00"}
			}
			out, err := patchStoredCanvasDocument(body, nil, items, true)
			if err != nil {
				t.Fatal(err)
			}
			_, got, _ := canvasDocumentContent(out)
			if len(got) != n {
				t.Fatal(got)
			}
			var payload struct {
				Data struct {
					Main []struct {
						TrList []map[string]any `json:"trList"`
					} `json:"main"`
				} `json:"data"`
			}
			json.Unmarshal([]byte(strings.TrimPrefix(out, canvasDocumentPrefix)), &payload)
			rewritten := payload.Data.Main[0].TrList
			if len(rewritten) != n+2 || rewritten[1]["customRow"] != "A" || (n > 1 && rewritten[2]["customRow"] != "B") {
				t.Fatal("per-row layout lost", out)
			}
			if rewritten[1]["id"] != "row-A" || (n > 2 && rewritten[3]["id"] != nil) {
				t.Fatal("cloned row reused editor identity", out)
			}
			for _, marker := range []string{"COLUMN HEADER", "TABLE FOOTER", "ROW FREE TEXT A", "STAMP A", "CUSTOM"} {
				if !strings.Contains(out, marker) {
					t.Fatal("lost", marker)
				}
			}
		})
	}
	// A flat template cannot safely grow to two lines. Never replace it with a default.
	if _, err := patchStoredCanvasDocument(businessCanvas(map[string]string{"item_model": "A"}), nil, []AIDocumentItem{{Model: "A"}, {Model: "B"}}, true); err == nil {
		t.Fatal("unsupported layout silently accepted")
	}
}

func TestManualLineEditRecalculatesCanvasTotals(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	record, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "Manual", HTML: businessCanvas(map[string]string{"item_model": "MANUAL", "item_qty": "1", "item_price": "100.00", "item_amount": "100.00", "subtotal": "100.00", "total_amount": "100.00", "contract_amount": "100.00", "tax_amount": "0.00", "tax_rate": "0%"})}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tax := range []string{"0%", "13%", ""} {
		result, _, err := h.executeUpdateDocument(ctx, account.OwnerID, map[string]any{"id": record.ID, "values": map[string]any{"tax_rate": tax}, "items": []any{map[string]any{"model": "MANUAL", "qty": "2", "price": "100.00", "amount": "999999"}}}, mailstore.AIModelRecord{}, "")
		if err != nil {
			t.Fatal(err)
		}
		action := result.(*AIDocumentAction)
		want := map[string]string{"0%": "200.00", "13%": "226.00", "": ""}[tax]
		values, items, ok := canvasDocumentContent(action.HTML)
		if !ok || items[0].Amount != "200.00" || values["subtotal"] != "200.00" || values["total_amount"] != want || values["contract_amount"] != want || action.Values["total_amount"] != want {
			t.Fatalf("stale totals for tax %q: %+v %v", tax, values, items)
		}
	}
	result, _, err := h.executeUpdateDocument(ctx, account.OwnerID, map[string]any{"id": record.ID, "values": map[string]any{"tax_rate": "13%"}}, mailstore.AIModelRecord{}, "")
	if err != nil || result.(*AIDocumentAction).Values["total_amount"] != "113.00" {
		t.Fatalf("tax-only change: %+v %v", result, err)
	}
}

func TestDocumentAmountsAcrossToolsAndTaxModes(t *testing.T) {
	for _, tt := range []struct{ name, rate, mode, discount, total, tax string }{
		{"unknown", "", "", "", "", ""}, {"zero", "0", "", "", "20.00", "0.00"}, {"excluded", "13%", "excluded", "", "22.60", "2.60"}, {"included", "0.13", "included", "", "20.00", "2.30"}, {"discount", "13%", "excluded", "2.00", "20.34", "2.34"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{"items": []any{map[string]any{"model": "A", "qty": "2.5", "price": "8", "amount": "1"}}, "tax_rate": tt.rate, "tax_mode": tt.mode, "discount": tt.discount}
			calc, _, err := executeCalculatePricing(args)
			if err != nil {
				t.Fatal(err)
			}
			generated, _, err := executeGenerateDocument(args)
			if err != nil {
				t.Fatal(err)
			}
			card := generated.(*AIDocumentCard)
			values := calc.(map[string]any)
			if card.Subtotal != "20.00" || card.Items[0].Amount != "20.00" || card.Total != tt.total || card.TaxAmount != tt.tax || values["total"] != tt.total || values["tax_amount"] != tt.tax {
				t.Fatalf("inconsistent math: %+v %+v", card, values)
			}
		})
	}
	for _, bad := range []map[string]any{{"tax_rate": "abc"}, {"tax_rate": "130%"}, {"tax_mode": "guess"}, {"discount": "21"}, {"discount": "-1"}} {
		bad["items"] = []any{map[string]any{"qty": "2", "price": "10"}}
		if _, _, err := executeCalculatePricing(bad); err == nil {
			t.Fatal("invalid money inputs accepted", bad)
		}
	}
	card := &AIDocumentCard{Values: map[string]string{"tax_rate": "0%"}, Items: []AIDocumentItem{{Qty: "1", Price: "0.005"}, {Qty: "1", Price: "0.005"}}}
	if err := calculateDocumentAmounts(card, nil); err != nil || card.Total != "0.02" {
		t.Fatalf("line rounding: %+v %v", card, err)
	}
	card = &AIDocumentCard{Values: map[string]string{"total_amount": "999"}, Items: []AIDocumentItem{{Qty: "1", Price: "20"}, {Qty: "1", Amount: "999"}}}
	if err := calculateDocumentAmounts(card, nil); err != nil || card.Subtotal != "" || card.Total != "" || card.Items[1].Amount != "" {
		t.Fatalf("incomplete line treated as zero: %+v %v", card, err)
	}
	// Catalog and manual lines use their own explicit tax treatment.
	card = &AIDocumentCard{Values: map[string]string{"tax_rate": "0%"}, Items: []AIDocumentItem{{Qty: "1", Price: "100"}, {Qty: "1", Price: "100"}}}
	if err := calculateDocumentAmounts(card, map[int]mailstore.ProductPrice{0: {TaxRate: "0.13", TaxMode: "included"}}); err != nil || card.Total != "200.00" || card.TaxAmount != "11.50" {
		t.Fatalf("mixed lines: %+v %v", card, err)
	}
}

func TestContractSourceSnapshotStableThroughFinalReply(t *testing.T) {
	var sourceID string
	var changeSource func()
	calls := 0
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string {
		calls++
		if calls == 1 {
			return fmt.Sprintf("```tool_call\n{\"name\":\"generate_document\",\"arguments\":{\"type\":\"contract\",\"sourceQuotationId\":%q,\"sourceQuotationRevision\":1}}\n```", sourceID)
		}
		changeSource()
		return fmt.Sprintf("已按确认的报价生成合同，总额 100.00。\n```document\n{\"type\":\"contract\",\"sourceQuotationId\":%q,\"total\":\"200.00\"}\n```", sourceID)
	})
	ctx := context.Background()
	fields := map[string]string{"customer_company": "Buyer", "item_model": "MANUAL", "item_qty": "1", "item_price": "100.00", "item_amount": "100.00", "subtotal": "100.00", "total_amount": "100.00", "currency": "CNY"}
	record, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "Source", HTML: businessCanvas(fields)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	sourceID = record.ID
	changeSource = func() {
		fields["item_price"] = "200.00"
		fields["item_amount"] = "200.00"
		fields["subtotal"] = "200.00"
		fields["total_amount"] = "200.00"
		record.HTML = businessCanvas(fields)
		if _, e := h.mailDB.SaveDocument(ctx, *record, record.Revision); e != nil {
			t.Error(e)
		}
	}
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "Contract", "contract")
	if err != nil {
		t.Fatal(err)
	}
	status, res, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "source-drift", Version: &session.Version, Messages: []AIAgentChatMessage{{Role: "user", Content: "按已确认报价制作合同"}}})
	if status != 200 || res.Document == nil || res.Document.Total != "100.00" || res.Document.Items[0].Price != "100.00" || res.Document.SourceQuotationRevision != 1 || res.Document.Values["source_quotation_revision"] != "1" {
		t.Fatalf("source drifted: %d %s", status, raw)
	}
	state, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if err != nil || !strings.Contains(string(state.CurrentDocument), `"total":"100.00"`) {
		t.Fatalf("committed wrong snapshot: %+v %v", state, err)
	}
	// A fresh invocation cannot pretend that the old revision is still current.
	_, _, err = h.executeTool(ctx, account.OwnerID, account.ID, AIToolCall{Name: "generate_document", Args: map[string]any{"type": "contract", "sourceQuotationId": sourceID, "sourceQuotationRevision": 1}}, mailstore.AIModelRecord{}, "")
	if err == nil || !strings.Contains(err.Error(), "版本已变化") {
		t.Fatalf("stale source accepted: %v", err)
	}
	read, _, err := h.executeReadDocument(ctx, account.OwnerID, map[string]any{"id": sourceID})
	if err != nil || read.(map[string]any)["revision"] != int64(2) {
		t.Fatal("source revision unavailable", read, err)
	}
}
