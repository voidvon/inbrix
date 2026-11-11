package web

import (
	"context"
	"encoding/json"
	"testing"

	"inbrix/mailstore"
)

func TestGeneratedDocumentRoutesFreightToExistingField(t *testing.T) {
	for _, label := range []string{"运费", "Freight", "Shipping charges", "内陆运费"} {
		t.Run(label, func(t *testing.T) {
			args := map[string]any{
				"type": "contract", "currency": "USD",
				"items": []any{
					map[string]any{"model": "VALVE", "qty": "2", "price": "50"},
					map[string]any{"name": label, "price": "20.25", "amount": "999"},
				},
				"values": map[string]any{"discount": "5"},
			}
			generated, _, err := executeGenerateDocument(args)
			if err != nil {
				t.Fatal(err)
			}
			card := generated.(*AIDocumentCard)
			if len(card.Items) != 1 || card.Items[0].Model != "VALVE" || card.Subtotal != "100.00" || card.Total != "115.25" || card.Values["inland_freight"] != "20.25" || card.Values["say_total"] != "SAY TOTAL USD ONE HUNDRED FIFTEEN AND 25/100 ONLY" {
				t.Fatalf("freight or amount words incorrect: %+v", card)
			}
		})
	}
}

func TestFreightNormalizationDoesNotGuessOrDoubleCharge(t *testing.T) {
	for _, tt := range []struct {
		name, scalar, price, qty, want string
		fail                           bool
	}{
		{"same scalar is not added again", "20", "20", "1", "20.00", false},
		{"conflicting scalar", "30", "20", "1", "", true},
		{"unknown amount", "", "", "1", "", true},
		{"negative amount", "", "-20", "1", "", true},
		{"invalid quantity", "", "20", "abc", "", true},
		{"zero quantity", "", "20", "0", "", true},
		{"line decimal rounding", "", "0.005", "1", "0.01", false},
		{"quantity times price", "", "10", "2", "20.00", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			card := &AIDocumentCard{Values: map[string]string{"inland_freight": tt.scalar}, Items: []AIDocumentItem{{Model: "Freight", Price: tt.price, Qty: tt.qty}}}
			err := normalizeDocumentFreightLines(card)
			if (err != nil) != tt.fail || (!tt.fail && (card.Values["inland_freight"] != tt.want || len(card.Items) != 0)) {
				t.Fatalf("unexpected freight normalization: %+v, %v", card, err)
			}
		})
	}
	for _, item := range []AIDocumentItem{
		{Model: "Freight valve"},
		{Model: "VALVE", Description: "Freight"},
		{Model: "Freight", ProductID: "catalog-product"},
		{Model: "Freight", PriceID: "catalog-price"},
	} {
		if isDocumentFreightLine(item) {
			t.Fatal("product incorrectly treated as freight", item)
		}
	}
}

func TestFreightCommandAndWordsSurviveCanvasSave(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	body := businessCanvas(map[string]string{"item_model": "VALVE", "item_qty": "2", "item_price": "50", "item_amount": "100.00", "total_amount": "100.00", "currency": "USD", "inland_freight": "10", "say_total": ""})
	_, initial, _ := canvasDocumentContent(body)
	commands := []DocumentCommand{{Type: "add_line", Fields: map[string]json.RawMessage{"description": json.RawMessage(`"运费"`), "price": json.RawMessage(`"20.25"`)}}}
	action, err := h.proposeCanvasDocumentCommands(ctx, account.OwnerID, aiDocumentInput{Canvas: body, DocumentType: "contract"}, commands)
	if err != nil {
		t.Fatal(err)
	}
	if len(action.Items) != 1 || action.Items[0].LineID != initial[0].LineID || action.Values["inland_freight"] != "20.25" || action.Values["subtotal"] != "100.00" || action.Values["total_amount"] != "120.25" || action.Values["say_total"] != "SAY TOTAL USD ONE HUNDRED TWENTY AND 25/100 ONLY" {
		t.Fatalf("freight created a product or stale words: %+v", action)
	}
	previous := &mailstore.DocumentRecord{HTML: body}
	normalized, _, err := normalizeCommercialDocument(ctx, h.mailDB, account.OwnerID, action.HTML, "contract", previous)
	if err != nil {
		t.Fatal(err)
	}
	values, items, _ := canvasDocumentContent(normalized)
	visible, _, _ := canvasProjectionContent(normalized)
	if len(items) != 1 || items[0].LineID != initial[0].LineID || values["say_total"] != action.Values["say_total"] || visible["say_total"] != values["say_total"] || visible["inland_freight"] != "20.25" {
		t.Fatal("save lost freight, words or stable identity", values, items, visible)
	}
	// Clearing freight uses the same field and removes it from both total forms.
	cleared, err := h.proposeCanvasDocumentCommands(ctx, account.OwnerID, aiDocumentInput{Canvas: normalized, DocumentType: "contract"}, []DocumentCommand{{Type: "set_fields", Fields: map[string]json.RawMessage{"inland_freight": json.RawMessage(`""`)}}})
	if err != nil || cleared.Values["total_amount"] != "100.00" || cleared.Values["say_total"] != "SAY TOTAL USD ONE HUNDRED ONLY" {
		t.Fatal("clearing freight did not update amount words", cleared, err)
	}
}

func TestDocumentAmountWordsDecimalAndScale(t *testing.T) {
	for _, tt := range []struct{ amount, words string }{
		{"0", "ZERO"},
		{"0.005", "ZERO AND 01/100"},
		{"19.99", "NINETEEN AND 99/100"},
		{"1001.10", "ONE THOUSAND ONE AND 10/100"},
		{"1234567.89", "ONE MILLION TWO HUNDRED THIRTY FOUR THOUSAND FIVE HUNDRED SIXTY SEVEN AND 89/100"},
		{"999.999", "ONE THOUSAND"},
		{"1000000000000000000000", "ONE SEXTILLION"},
	} {
		values := map[string]string{"total_amount": tt.amount, "currency": "RMB"}
		refreshDocumentAmountWords(values, "")
		if want := "SAY TOTAL CNY " + tt.words + " ONLY"; values["say_total"] != want {
			t.Fatalf("%s: got %q, want %q", tt.amount, values["say_total"], want)
		}
	}
	values := map[string]string{"currency": "USD", "say_total": "stale words"}
	refreshDocumentAmountWords(values, "100")
	if values["say_total"] != "" {
		t.Fatal("unknown amount retained words", values)
	}
}

func TestFinalChatDocumentRoutesFreight(t *testing.T) {
	_, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string {
		return "```document\n" + `{"type":"contract","currency":"USD","items":[{"model":"VALVE","qty":"1","price":"100"},{"description":"Freight","qty":"1","price":"20"}]}` + "\n```"
	})
	status, result, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, Messages: []AIAgentChatMessage{{Role: "user", Content: "制作形式发票，阀门100美元，运费20美元"}}})
	if status != 200 || result.Document == nil {
		t.Fatal(status, raw)
	}
	card := result.Document
	if len(card.Items) != 1 || card.Total != "120.00" || card.Values["inland_freight"] != "20.00" || card.Values["say_total"] != "SAY TOTAL USD ONE HUNDRED TWENTY ONLY" {
		t.Fatal("final chat document bypassed freight normalization", card)
	}
}
