package web

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"inbrix/mailstore"
)

// Resolve referenced catalog items before generating totals. Only the stored
// amount and applicable quantity are used, never a model-supplied subtotal.
func (h *AISettingsHandler) prepareCatalogDocumentArgs(ctx context.Context, owner string, args map[string]any) error {
	raw, _ := args["items"].([]any)
	hasCatalog := false
	for _, r := range raw {
		item, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if err := h.checkBusinessProduct(ctx, owner, parseStringArg(item, "productId")); err != nil {
			return err
		}
		if parseStringArg(item, "variantId") == "" && parseStringArg(item, "priceId") == "" {
			continue
		}
		v, p, err := h.mailDB.ResolveProductPrice(ctx, owner, parseStringArg(item, "productId"), parseStringArg(item, "variantId"), parseStringArg(item, "priceId"), productVersionArg(item), parseStringArg(item, "qty", "quantity"))
		if err != nil {
			return err
		}
		hasCatalog = true
		if supplied := parseStringArg(item, "price", "unit_price"); supplied != "" && !sameCatalogNumber(supplied, p.Amount) {
			return fmt.Errorf("catalog price mismatch")
		}
		item["price"] = p.Amount
		item["qty"] = parseStringArg(item, "qty", "quantity")
		item["amount"] = catalogAmount(item["qty"].(string), p.Amount)
		if v.SKU != "" {
			item["model"] = v.SKU
		}
	}
	if hasCatalog {
		given := parseStringArg(args, "currency")
		if given == "¥" || given == "￥" || given == "人民币" || strings.EqualFold(given, "RMB") {
			given = mailstore.ProductPriceCurrency
		}
		if given != "" && !strings.EqualFold(given, mailstore.ProductPriceCurrency) {
			return fmt.Errorf("catalog documents must use CNY")
		}
		args["currency"] = mailstore.ProductPriceCurrency
	}
	return nil
}
func sameCatalogNumber(a, b string) bool {
	x, ok := new(big.Rat).SetString(a)
	if !ok {
		return false
	}
	y, ok := new(big.Rat).SetString(b)
	return ok && x.Cmp(y) == 0
}
func catalogAmount(qty, amount string) string {
	q, ok := new(big.Rat).SetString(qty)
	if !ok {
		return ""
	}
	p, ok := new(big.Rat).SetString(amount)
	if !ok {
		return ""
	}
	return new(big.Rat).Mul(q, p).FloatString(2)
}

// finalizeDocumentAmounts resolves live catalog references only for new or
// commercially changed documents. Contact-only edits retain historical prices.
func (h *AISettingsHandler) finalizeDocumentAmounts(ctx context.Context, owner string, card *AIDocumentCard, trusted ...[]AIDocumentItem) error {
	if card == nil {
		return nil
	}
	if err := validateDocumentCurrency(card); err != nil {
		return err
	}
	ensureDocumentLineIDs(card.Items)
	previous := map[string]AIDocumentItem{}
	if len(trusted) > 0 {
		for _, line := range trusted[0] {
			previous[line.LineID] = line
		}
	}
	for i := range card.Items {
		item := &card.Items[i]
		if old, exists := previous[item.LineID]; exists && old.PriceID != "" && (item.PriceID == "" || item.ProductID == "" || item.VariantID == "") {
			return fmt.Errorf("catalog line cannot discard its price source; remove it and explicitly add a new manual line")
		}
		if item.PriceID == "" {
			continue
		}
		if old, ok := previous[item.LineID]; ok && old.ProductID == item.ProductID && old.ProductVersion == item.ProductVersion && old.VariantID == item.VariantID && old.PriceID == item.PriceID && old.Price == item.Price && old.Qty == item.Qty {
			continue
		}
		_, price, err := h.mailDB.ResolveProductPrice(ctx, owner, item.ProductID, item.VariantID, item.PriceID, item.ProductVersion, item.Qty)
		if err != nil {
			return err
		}
		if item.Price != "" && !sameCatalogNumber(item.Price, price.Amount) {
			return fmt.Errorf("catalog price mismatch")
		}
		item.Price = price.Amount
	}
	return calculateDocumentAmounts(card)
}
