package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"inbrix/config"
	"inbrix/mailstore"
)

func TestCatalogDocumentReferencesAndTax(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(ctx, "catalog", "Catalog", "hash")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAISettingsHandler(nil, &config.Config{}, db)
	for _, tt := range []struct{ mode, rate, total, tax string }{{"excluded", "0.13", "22.60", "2.60"}, {"included", "0.13", "20.00", "2.30"}, {"", "0.13", "", ""}, {"excluded", "0", "20.00", "0.00"}} {
		t.Run(tt.mode+tt.rate, func(t *testing.T) {
			product, err := db.SaveProduct(ctx, user.ID, "", mailstore.ProductInput{Name: "阀门", Model: "BV", Catalog: &mailstore.ProductCatalog{Dimensions: []mailstore.ProductDimension{{Key: "connection", Label: "连接方式", Type: "text"}}, Variants: []mailstore.ProductVariant{{SKU: "BV-F", Attributes: map[string]string{"connection": "法兰"}, Prices: []mailstore.ProductPrice{{Amount: "10.00", TaxMode: tt.mode, TaxRate: tt.rate, MinQty: "2", MaxQty: "10"}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			variant := product.Catalog.Variants[0]
			price := variant.Prices[0]
			item := map[string]any{"productId": product.ID, "productVersion": float64(product.Version), "variantId": variant.ID, "priceId": price.ID, "qty": "2"}
			args := map[string]any{"items": []any{item}}
			result, _, err := handler.executeTool(ctx, user.ID, "", AIToolCall{Name: "generate_document", Args: args}, mailstore.AIModelRecord{}, "")
			if err != nil {
				t.Fatal(err)
			}
			card := result.(*AIDocumentCard)
			if card.Items[0].Price != "10.00" || card.Items[0].Amount != "20.00" || card.Items[0].Model != "BV-F" || !strings.Contains(card.Items[0].Description, "法兰") {
				t.Fatal(card.Items)
			}
			if card.Currency != "CNY" || card.Subtotal != "20.00" || card.Total != tt.total || card.TaxAmount != tt.tax || card.Values["total_amount"] != tt.total {
				t.Fatalf("wrong tax result: %+v", card)
			}
			item["qty"] = "1"
			if _, _, err = handler.executeTool(ctx, user.ID, "", AIToolCall{Name: "generate_document", Args: args}, mailstore.AIModelRecord{}, ""); err == nil {
				t.Fatal("wrong quantity accepted")
			}
			item["qty"] = "2"
			item["price"] = "9.99"
			if _, _, err = handler.executeTool(ctx, user.ID, "", AIToolCall{Name: "generate_document", Args: args}, mailstore.AIModelRecord{}, ""); err == nil {
				t.Fatal("invented price accepted")
			}
			item["price"] = "10"
			item["priceId"] = "foreign-price"
			if _, _, err = handler.executeTool(ctx, user.ID, "", AIToolCall{Name: "generate_document", Args: args}, mailstore.AIModelRecord{}, ""); err == nil {
				t.Fatal("foreign price accepted")
			}
		})
	}
}

func TestEmailAgentStructuredConfigurationPricing(t *testing.T) {
	var product mailstore.Product
	var requests []agentTestRequest
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		requests = append(requests, request)
		switch len(requests) {
		case 1:
			return "```tool_call\n" + `{"name":"search_products","arguments":{"query":"BV-QA"}}` + "\n```"
		case 2:
			return fmt.Sprintf("```tool_call\n{\"name\":\"query_product_prices\",\"arguments\":{\"productId\":%q,\"version\":1}}\n```", product.ID)
		case 3:
			return fmt.Sprintf("```tool_call\n{\"name\":\"query_product_prices\",\"arguments\":{\"productId\":%q,\"version\":1,\"attributes\":{\"connection\":\"法兰\"},\"qty\":\"10\"}}\n```", product.ID)
		case 4:
			v := product.Catalog.Variants[0]
			return fmt.Sprintf("```tool_call\n{\"name\":\"generate_document\",\"arguments\":{\"items\":[{\"productId\":%q,\"productVersion\":1,\"variantId\":%q,\"priceId\":%q,\"qty\":\"10\"}]}}\n```", product.ID, v.ID, v.Prices[0].ID)
		default:
			return "已匹配法兰规格，10 台单价 100 元，报价草稿已生成。"
		}
	})
	ctx := context.Background()
	var err error
	product, err = h.mailDB.SaveProduct(ctx, account.OwnerID, "", mailstore.ProductInput{Name: "球阀", Model: "BV-QA", Catalog: &mailstore.ProductCatalog{Dimensions: []mailstore.ProductDimension{{Key: "connection", Label: "连接方式", Type: "select", Options: []string{"法兰", "螺纹"}}}, Variants: []mailstore.ProductVariant{
		{SKU: "BV-F", Attributes: map[string]string{"connection": "法兰"}, Prices: []mailstore.ProductPrice{{Amount: "100.00", Currency: "CNY", MinQty: "10", TaxRate: "0.13", TaxMode: "included"}}},
		{SKU: "BV-T", Attributes: map[string]string{"connection": "螺纹"}, Prices: []mailstore.ProductPrice{{Amount: "80.00", Currency: "CNY", MinQty: "10"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	addAgentTestMail(t, h, account, 2, "请报 BV-QA 法兰连接，10 台。")
	status, result, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, ConversationID: conversationID(account.ID, []string{"peer@example.com"}), Messages: []AIAgentChatMessage{{Role: "user", Content: "按产品库规格和价格起草报价"}}})
	if status != 200 || len(result.ToolCalls) != 4 || result.Document == nil {
		t.Fatal(status, raw)
	}
	if result.Document.Items[0].VariantID != product.Catalog.Variants[0].ID || result.Document.Items[0].Price != "100.00" || result.Document.Total != "1000.00" {
		t.Fatal(raw)
	}
	if !strings.Contains(requests[2].Input, "needs_clarification") || !strings.Contains(requests[3].Input, `\"status\":\"matched\"`) && !strings.Contains(requests[3].Input, `"status":"matched"`) {
		t.Fatal("configuration matching evidence missing")
	}
}

func TestCanvasCatalogReferenceWithoutImage(t *testing.T) {
	payload := map[string]any{"data": map[string]any{"main": []any{map[string]any{"control": map[string]any{"conceptId": "item_model", "value": []any{map[string]any{"value": "BV-F"}}}, "extension": map[string]any{"productId": "p", "productVersion": 3, "variantId": "v", "priceId": "price"}}}}}
	b, _ := json.Marshal(payload)
	_, items, ok := canvasDocumentContent(canonicalTestCanvas(canvasDocumentPrefix + string(b)))
	if !ok || len(items) != 1 || items[0].VariantID != "v" || items[0].PriceID != "price" || items[0].ProductVersion != 3 {
		t.Fatal(items, ok)
	}
}

func TestSavedCatalogQuoteKeepsSnapshotForContactChanges(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(ctx, "snapshot", "Snapshot", "hash")
	if err != nil {
		t.Fatal(err)
	}
	control := func(id, value string) map[string]any {
		return map[string]any{"control": map[string]any{"conceptId": id, "value": []any{map[string]any{"value": value}}}}
	}
	model := control("item_model", "BV-F")
	model["extension"] = map[string]any{"productId": "deleted-product", "productVersion": 1, "variantId": "old-variant", "priceId": "old-price"}
	raw, _ := json.Marshal(map[string]any{"data": map[string]any{"main": []any{model, control("item_qty", "2"), control("item_price", "10.00"), control("item_amount", "20.00"), control("customer_contact", "Old contact")}}})
	doc, err := db.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: user.ID, Name: "Saved quote", HTML: canonicalTestCanvas(canvasDocumentPrefix + string(raw))})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAISettingsHandler(nil, &config.Config{}, db)
	result, _, err := handler.executeTool(ctx, user.ID, "", AIToolCall{Name: "update_document", Args: map[string]any{"id": doc.ID, "baseRevision": doc.Revision, "commands": testCommands(doc.HTML, map[string]string{"customer_contact": "New contact"}, nil)}}, mailstore.AIModelRecord{}, "")
	if err != nil {
		t.Fatal("unrelated change revalidated historical price", err)
	}
	action := result.(*AIDocumentAction)
	if action.Values["customer_contact"] != "New contact" || len(action.Items) != 1 || action.Items[0].Price != "10.00" || action.Items[0].PriceID != "old-price" {
		t.Fatal(action)
	}
}

func TestCatalogSnapshotCannotSilentlyBecomeManual(t *testing.T) {
	old := AIDocumentItem{LineID: "stable", ProductID: "product", VariantID: "variant", PriceID: "price", Qty: "2", Price: "10", TaxRate: "0.13", TaxMode: "excluded"}
	h := &AISettingsHandler{}
	card := &AIDocumentCard{Values: map[string]string{"currency": "CNY"}, Items: []AIDocumentItem{old}}
	if err := h.finalizeDocumentAmounts(context.Background(), "owner", card, []AIDocumentItem{old}); err != nil {
		t.Fatal(err)
	}
	if card.Values["total_amount"] != "22.60" {
		t.Fatal(card.Values)
	}
	card.Items[0].PriceID = ""
	if err := h.finalizeDocumentAmounts(context.Background(), "owner", card, []AIDocumentItem{old}); err == nil {
		t.Fatal("catalog source was discarded")
	}
}

func TestContractIndexedCatalogDescriptionAndSKU(t *testing.T) {
	body := canvasDocumentPrefix + `{"data":{"main":[{"control":{"conceptId":"item_desc_0","value":[{"value":"连接方式: 法兰"}]},"extension":{"model":"BV-F","productId":"p","productVersion":2,"variantId":"v","priceId":"price"}},{"control":{"conceptId":"item_qty_0","value":[{"value":"10"}]}}]}}`
	_, items, ok := canvasDocumentContent(canonicalTestCanvas(body))
	if !ok || len(items) != 1 || items[0].Model != "BV-F" || items[0].Description != "连接方式: 法兰" || items[0].PriceID != "price" {
		t.Fatal(items, ok)
	}
}
