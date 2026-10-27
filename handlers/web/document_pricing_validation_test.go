package web

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestAtomicDocumentCommandsPreserveIdentity(t *testing.T) {
	body := businessCanvas(map[string]string{"item_model_0": "A", "item_qty_0": "1", "item_price_0": "10", "item_amount_0": "10", "item_model_1": "B", "item_qty_1": "2", "item_price_1": "20", "item_amount_1": "40", "tax_rate": "0%"})
	_, before, _ := canvasDocumentContent(body)
	command := DocumentCommand{Type: "update_line", LineID: before[1].LineID, Fields: map[string]json.RawMessage{"qty": json.RawMessage(`"3"`)}}
	values, items, err := applyDocumentCommands(body, []DocumentCommand{command})
	if err != nil || len(items) != 2 || items[0].LineID != before[0].LineID || items[0].Price != "10" || items[1].Price != "20" || items[1].Qty != "3" {
		t.Fatal(items, err)
	}
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	action, err := h.proposeCanvasDocumentUpdate(context.Background(), account.OwnerID, body, values, items)
	if err != nil || action.Values["total_amount"] != "70.00" {
		t.Fatal(action, err)
	}
	for _, bad := range []string{`{"values":{}}`, `{"itemUpdates":[]}`, `{"commands":[{"type":"update_line","index":0,"fields":{"qty":"3"}}]}`} {
		if _, err := decodeDocumentCommands(bad); err == nil {
			t.Fatal("accepted old or invalid protocol", bad)
		}
	}
	command.LineID = "unknown"
	if _, _, err := applyDocumentCommands(body, []DocumentCommand{command}); err == nil {
		t.Fatal("accepted unknown lineId")
	}
}

func TestPricingValidationExplainsIncompleteTotalsAndResets(t *testing.T) {
	card := &AIDocumentCard{Items: []AIDocumentItem{{Model: "A", Qty: "1"}}, Values: map[string]string{"inland_freight": "20"}}
	if err := calculateDocumentAmounts(card, nil); err != nil {
		t.Fatal(err)
	}
	issues := map[string]bool{}
	for _, issue := range card.PricingValidation.Issues {
		issues[issue.Code] = true
	}
	if card.Total != "" || card.PricingValidation.Status != "incomplete" || !issues["missing_price"] || !issues["missing_tax"] || !issues["missing_freight_tax"] {
		t.Fatal("missing actionable issues", card.PricingValidation)
	}
	card.Items[0].Price = "100"
	card.Values["tax_mode"] = "excluded"
	card.Values["tax_rate"], card.Values["freight_tax_rate"], card.Values["freight_tax_mode"] = "0%", "0%", "included"
	if err := calculateDocumentAmounts(card, nil); err != nil || card.Total != "120.00" || card.PricingValidation.Status != "complete" || len(card.PricingValidation.Issues) != 0 {
		t.Fatal("stale validation after completion", card, err)
	}
	catalog := &AIDocumentCard{Items: []AIDocumentItem{{Qty: "1", Price: "10"}}, Values: map[string]string{"tax_rate": "0%"}}
	if err := calculateDocumentAmounts(catalog, map[int]mailstore.ProductPrice{0: {}}); err != nil || catalog.PricingValidation.Issues[0].Code != "missing_catalog_tax" {
		t.Fatal("missing catalog-specific explanation", catalog, err)
	}
}

func TestAgentSparseUpdatesRequireCurrentRevisionAndExplainMissingTotal(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	body := businessCanvas(map[string]string{"item_model": "A", "item_qty": "1", "item_price": "10", "item_amount": "10.00", "total_amount": "10.00"})
	record, err := h.mailDB.SaveDocument(context.Background(), mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "Test", HTML: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, lines, _ := canvasDocumentContent(body)
	args := map[string]any{"id": record.ID, "commands": []DocumentCommand{{Type: "update_line", LineID: lines[0].LineID, Fields: map[string]json.RawMessage{"qty": json.RawMessage(`"2"`)}}}}
	if _, _, err := h.executeUpdateDocument(context.Background(), account.OwnerID, args, mailstore.AIModelRecord{}, ""); err == nil {
		t.Fatal("unguarded sparse update accepted")
	}
	args["baseRevision"] = record.Revision
	result, _, err := h.executeUpdateDocument(context.Background(), account.OwnerID, args, mailstore.AIModelRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	action := result.(*AIDocumentAction)
	if action.Items[0].Price != "10" || action.Values["subtotal"] != "20.00" || action.Values["total_amount"] != "" || action.PricingValidation.Status != "incomplete" || !strings.Contains(action.Summary, "税率待确认") {
		t.Fatal("missing actionable mutation result", action)
	}
}
