package web

import (
	"context"
	"testing"

	"inbrix/config"
	"inbrix/mailstore"
)

func TestCatalogCurrencyValidatedAcrossDocumentEntrypoints(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(ctx, "currency-test", "Currency", "hash")
	if err != nil {
		t.Fatal(err)
	}
	h := NewAISettingsHandler(nil, &config.Config{}, db)
	product, err := db.SaveProduct(ctx, user.ID, "", mailstore.ProductInput{Name: "Valve", Model: "BV", Catalog: &mailstore.ProductCatalog{Dimensions: []mailstore.ProductDimension{}, Variants: []mailstore.ProductVariant{{SKU: "BV", Attributes: map[string]string{}, Prices: []mailstore.ProductPrice{{Amount: "100.00", Currency: "CNY", TaxMode: "included", TaxRate: "0"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	v := product.Catalog.Variants[0]
	price := v.Prices[0]
	items := []AIDocumentItem{{ProductID: product.ID, ProductVersion: product.Version, VariantID: v.ID, PriceID: price.ID, Model: "BV", Qty: "1", Price: "100.00"}}
	for _, currency := range []string{"USD", "EUR"} {
		card := &AIDocumentCard{Type: "quotation", Currency: currency, Values: map[string]string{"currency": currency}, Items: append([]AIDocumentItem{}, items...)}
		if err := h.finishBusinessDocument(ctx, user.ID, card); err == nil {
			t.Fatal("final document fence relabeled CNY", currency)
		}
		args := map[string]any{"currency": currency, "items": []any{map[string]any{"productId": product.ID, "productVersion": product.Version, "variantId": v.ID, "priceId": price.ID, "qty": "1"}}}
		if _, _, err := h.executeTool(ctx, user.ID, "", AIToolCall{Name: "generate_document", Args: args}, mailstore.AIModelRecord{}, ""); err == nil {
			t.Fatal("tool relabeled CNY", currency)
		}
	}
	body := businessCanvas(map[string]string{"item_description": "Valve", "item_model": "BV", "item_product_id": product.ID, "item_product_version": "1", "item_variant_id": v.ID, "item_price_id": price.ID, "item_qty": "1", "item_price": "100.00", "item_amount": "100.00", "total_amount": "100.00", "currency": "CNY"})
	if _, err := h.proposeCanvasDocumentUpdate(ctx, user.ID, body, map[string]string{"currency": "USD"}, nil); err == nil {
		t.Fatal("update relabeled CNY")
	}
	for _, alias := range []string{"cny", "RMB", "人民币", "¥"} {
		action, err := h.proposeCanvasDocumentUpdate(ctx, user.ID, body, map[string]string{"currency": alias}, nil)
		if err != nil || action == nil || action.Values["currency"] != "CNY" {
			t.Fatal("equivalent currency alias rejected", alias, action, err)
		}
	}
	// Omitting refs while retaining the same product cannot turn it into a manual
	// line with a made-up price, or conceal a currency mismatch.
	if _, err := h.proposeCanvasDocumentUpdate(ctx, user.ID, body, nil, []AIDocumentItem{{Model: "BV", Description: "Valve", Qty: "2", Price: "1"}}); err == nil {
		t.Fatal("catalog refs downgraded to manual")
	}
	action, err := h.proposeCanvasDocumentUpdate(ctx, user.ID, body, nil, []AIDocumentItem{{Model: "BV", Description: "Valve", Qty: "2", Price: "100"}})
	if err != nil || action.Items[0].PriceID != price.ID || action.Values["total_amount"] != "200.00" {
		t.Fatal("identity not retained", action, err)
	}
	manual := businessCanvas(map[string]string{"item_model": "MANUAL", "item_qty": "1", "item_price": "100", "currency": "USD"})
	if _, err := h.proposeCanvasDocumentUpdate(ctx, user.ID, manual, map[string]string{"currency": "EUR"}, nil); err == nil {
		t.Fatal("manual currency converted without a rate")
	}
}
