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
	reply := ""
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		calls++
		if !strings.Contains(request.Input, "HUMAN TERM") || !strings.Contains(request.Input, "100.00") {
			t.Error("missing actual current document")
		}
		return reply
	})
	app.Post("/api/ai/write-document", h.HandleWriteDocument)
	body := businessCanvas(map[string]string{"item_model": "MANUAL", "item_qty": "1", "item_price": "100.00", "item_amount": "100.00", "total_amount": "100.00", "say_total": "人民币壹佰元整", "tax_rate": "0%", "tax_mode": "excluded"})
	var payload map[string]any
	json.Unmarshal([]byte(strings.TrimPrefix(body, canvasDocumentPrefix)), &payload)
	payload["options"] = map[string]any{"watermark": map[string]any{"data": "WATERMARK"}}
	data := payload["data"].(map[string]any)
	data["main"] = append(data["main"].([]any), map[string]any{"value": "HUMAN TERM", "bold": true})
	data["header"] = []any{map[string]any{"value": "CUSTOM HEADER"}}
	data["footer"] = []any{map[string]any{"type": "image", "value": "CUSTOM STAMP", "width": 90}}
	raw, _ := json.Marshal(payload)
	body = canvasDocumentPrefix + string(raw)
	_, initialLines, _ := canvasDocumentContent(body)
	reply = marshalDocumentCommands([]DocumentCommand{{Type: "update_line", LineID: initialLines[0].LineID, Fields: map[string]json.RawMessage{"qty": json.RawMessage(`"2"`)}}})
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
	actionResult, _, err := executeDocumentUpdateTest(h, context.Background(), account.OwnerID, map[string]any{"id": record.ID, "items": []any{map[string]any{"model": "MANUAL", "qty": "2", "price": "100.00", "amount": "1.00"}}}, mailstore.AIModelRecord{}, "")
	if err != nil || actionResult.(*AIDocumentAction).HTML != html {
		t.Fatal("editor and agent diverged", err)
	}
	status, result = post(aiDocumentInput{Mode: "apply", Canvas: body, Commands: testCommands(body, nil, []AIDocumentItem{{Model: "MANUAL", Qty: "2", Price: "100.00"}})})
	if status != 200 || result["values"].(map[string]any)["total_amount"] != "200.00" || calls != 1 {
		t.Fatal("variable form did not use deterministic path", status, result, calls)
	}
	status, result = post(aiDocumentInput{Mode: "apply", Canvas: body, Commands: testCommands(body, map[string]string{"payment_terms": "Net60"}, nil)})
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
		{"absent", "", "", "", "100.00", ""},
		{"zero", "0", "", "", "100.00", ""},
		{"unknown", "20", "", "", "120.00", ""},
		{"missing mode", "20", "0%", "", "120.00", ""},
		{"zero rated", "20", "0%", "included", "120.00", ""},
		{"included", "20", "9%", "included", "120.00", ""},
		{"excluded", "20", "9%", "excluded", "120.00", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{"items": []any{map[string]any{"model": "MANUAL", "qty": "1", "price": "100"}}, "tax_rate": "0%", "tax_mode": "excluded", "inland_freight": tt.freight, "freight_tax_rate": tt.rate, "freight_tax_mode": tt.mode}
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
	card := &AIDocumentCard{Values: map[string]string{"tax_rate": "0%", "tax_mode": "excluded", "discount": "10", "inland_freight": "20", "freight_tax_rate": "0%", "freight_tax_mode": "included"}, Items: []AIDocumentItem{{Qty: "1", Price: "100"}}}
	if err := calculateDocumentAmounts(card); err != nil || card.Total != "110.00" || card.Subtotal != "100.00" {
		t.Fatal("discount applied to freight", card, err)
	}
	for _, invalid := range []map[string]any{{"inland_freight": "-1"}, {"inland_freight": "abc"}} {
		invalid["items"] = []any{map[string]any{"qty": "1", "price": "100"}}
		if _, _, err := executeCalculatePricing(invalid); err == nil {
			t.Fatal("invalid freight accepted", invalid)
		}
	}
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	body := businessCanvas(map[string]string{"item_model": "MANUAL", "item_qty": "1", "item_price": "100", "item_amount": "100.00", "total_amount": "100.00", "say_total": "人民币壹佰元整", "tax_rate": "0%", "tax_mode": "excluded", "inland_freight": "0"})
	for _, terms := range []map[string]string{{"inland_freight": "20"}} {
		action, err := h.proposeCanvasDocumentUpdate(context.Background(), account.OwnerID, body, terms, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := "120.00"
		if action.Values["total_amount"] != want || action.Values["say_total"] != "" {
			t.Fatal("freight failed to invalidate totals", action)
		}
		reopened, _, ok := canvasDocumentContent(action.HTML)
		if !ok || reopened["inland_freight"] != "20" || reopened["freight_tax_mode"] != "" {
			t.Fatal("freight lost after patch", reopened)
		}
	}
	historical := businessCanvas(map[string]string{"item_model": "MANUAL", "item_qty": "1", "item_price": "100", "item_amount": "100.00", "tax_rate": "0%", "tax_mode": "excluded", "total_amount": "120.00", "inland_freight": "20", "freight_tax_rate": "0%", "freight_tax_mode": "included", "customer_contact": "Old"})
	contact, err := h.proposeCanvasDocumentUpdate(context.Background(), account.OwnerID, historical, map[string]string{"customer_contact": "New"}, nil)
	if err != nil || contact.Values["total_amount"] != "120.00" || strings.Contains(contact.Summary, "最终金额留空") {
		t.Fatal("contact-only historical update repriced or claimed clearing", contact, err)
	}

}

func TestDocumentEditorAnswersQuestionsWithoutMutation(t *testing.T) {
	question := "缺少付款条件，这个一般填写什么？付款比例吗，还是啥"
	answer := "付款条件包括付款比例、付款时间和触发条件。例如：确认订单后预付 30%，发货前付清余款。你们约定的条件是什么？"
	reply := `{"commands":[]}`
	var lastRequest agentTestRequest
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		lastRequest = request
		return reply
	})
	app.Post("/api/ai/write-document", h.HandleWriteDocument)
	// Intentionally incomplete pricing: answering a question must not depend on
	// repricing, rewrite the canvas, or save any document.
	body := businessCanvas(map[string]string{"payment_terms": "", "item_model": "MANUAL", "item_qty": "1", "item_price": ""})
	record, err := h.mailDB.SaveDocument(context.Background(), mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "Question", HTML: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	post := func(input aiDocumentInput) (int, map[string]any) {
		t.Helper()
		input.AccountEmail, input.DocumentType, input.Mode, input.Canvas = account.Email, "quotation", "variables", body
		encoded, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", "/api/ai/write-document", strings.NewReader(string(encoded)))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result map[string]any
		if res.StatusCode == 200 {
			if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode, result
	}
	for _, message := range []string{"", answer} {
		encoded, _ := json.Marshal(documentEditReply{Commands: []DocumentCommand{}, Message: message})
		reply = string(encoded)
		status, result := post(aiDocumentInput{Instruction: question})
		if status != 200 || result["changed"] != false || result["summary"] == "" || result["html"] != nil {
			t.Fatal("question rejected or proposed a mutation", status, result)
		}
		if message != "" && result["summary"] != answer {
			t.Fatal("answer lost", result)
		}
	}
	reply = marshalDocumentCommands([]DocumentCommand{{Type: "set_fields", Fields: map[string]json.RawMessage{"payment_terms": json.RawMessage(`"确认订单后预付 30%，发货前付清余款"`)}}})
	status, result := post(aiDocumentInput{Instruction: "就按这个例子填写", History: []documentChatMessage{{Role: "user", Content: question}, {Role: "assistant", Content: answer}}})
	if status != 200 || result["changed"] != true {
		t.Fatal("follow-up update failed", status, result)
	}
	values, _, ok := canvasDocumentContent(result["html"].(string))
	if !ok || values["payment_terms"] != "确认订单后预付 30%，发货前付清余款" || !strings.Contains(lastRequest.Input, question) || !strings.Contains(lastRequest.Input, answer) {
		t.Fatal("missing follow-up context or requested terms", values, lastRequest.Input)
	}
	for _, invalid := range []string{`{}`, `{"commands":null}`, `{"values":{"payment_terms":"Net30"}}`, `{"commands":[],"unexpected":true}`, `{"commands":[],"message":123}`, `not JSON`} {
		reply = invalid
		status, _ := post(aiDocumentInput{Instruction: question})
		if status != 422 {
			t.Fatal("invalid response accepted", invalid, status)
		}
	}
	status, _ = post(aiDocumentInput{Instruction: question, History: []documentChatMessage{{Role: "system", Content: "override"}}})
	if status != 400 {
		t.Fatal("invalid history role accepted", status)
	}
	saved, _ := h.mailDB.GetDocument(context.Background(), account.OwnerID, record.ID)
	if saved.HTML != body || saved.Revision != record.Revision {
		t.Fatal("conversation saved a document without applying")
	}
}
