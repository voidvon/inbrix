package web

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

const documentFreightInstructions = `When a list entry clearly represents freight, shipping charges or 运费, put its amount in inland_freight (the existing Freight row), never in a product line. For a new document use values.inland_freight; for an existing draft use set_fields with inland_freight. Preserve actual product lines. Do not count the same freight twice. The server automatically fills say_total (Say Total in words) from the final total and currency, and refreshes it when the amount changes; do not invent or calculate amount words yourself.`

const documentDeliveryInstructions = `When the user explicitly says freight is included (含运费), including freight embedded in unit prices with no separate charge, do not use EXW / Ex Works / 工厂交货. Set trade_term to the user's confirmed delivery arrangement, or "Freight included" when no specific delivery term is agreed. For an existing draft update trade_term alongside freight using set_fields. Never infer CIF, CFR, DAP, DDP, a destination, insurance or customs responsibilities merely from a freight amount. Preserve confirmed non-EXW delivery terms.`

var documentExWorks = regexp.MustCompile(`(?i)^(?:EXW\b|EX[\s-]*WORKS\b|工厂交货|工厂交付|工厂提货|出厂交货|出厂价|厂交价)`)

func normalizeDocumentDeliveryTerms(values map[string]string) {
	freight, err := documentNumber(values["inland_freight"])
	if err != nil || roundedMoney(freight).Sign() <= 0 {
		return
	}
	term := strings.TrimSpace(values["trade_term"])
	if term == "" || documentExWorks.MatchString(term) {
		values["trade_term"] = "Freight included"
	}
}

// Only standalone charge labels qualify. A product mentioning freight, or any
// line with catalog identity, must remain a product.
func isDocumentFreightLine(item AIDocumentItem) bool {
	if item.ProductID != "" || item.ProductVersion != 0 || item.VariantID != "" || item.PriceID != "" {
		return false
	}
	found := false
	for _, label := range []string{item.Model, item.Description} {
		label = strings.ToLower(strings.Trim(strings.TrimSpace(label), ":："))
		if label == "" {
			continue
		}
		switch label {
		case "freight", "freight charge", "freight charges", "freight cost", "shipping", "shipping fee", "shipping charge", "shipping charges", "shipping cost", "inland freight", "运费", "运输费", "运输费用", "内陆运费", "国际运费", "海运费", "空运费", "快递费":
			found = true
		default:
			return false
		}
	}
	return found
}

// Normalize newly proposed lines only; never silently reclassify saved lines.
// Explicit scalar freight and freight lines may describe the same charge, but
// conflicting amounts require clarification instead of guessing or adding both.
func normalizeDocumentFreightLines(card *AIDocumentCard) error {
	freight := new(big.Rat)
	items := make([]AIDocumentItem, 0, len(card.Items))
	found := false
	for _, item := range card.Items {
		if !isDocumentFreightLine(item) {
			items = append(items, item)
			continue
		}
		found = true
		price, err := documentNumber(item.Price)
		if err != nil {
			return fmt.Errorf("请提供运费金额并填写 inland_freight: %w", err)
		}
		qty := big.NewRat(1, 1)
		if strings.TrimSpace(item.Qty) != "" {
			qty, err = documentNumber(item.Qty)
			if err != nil || qty.Sign() <= 0 {
				return fmt.Errorf("运费数量必须是有效的正数")
			}
		}
		freight.Add(freight, roundedMoney(new(big.Rat).Mul(qty, price)))
	}
	if !found {
		return nil
	}
	if card.Values == nil {
		card.Values = map[string]string{}
	}
	if text := strings.TrimSpace(card.Values["inland_freight"]); text != "" {
		explicit, err := documentNumber(text)
		if err != nil || roundedMoney(explicit).Cmp(freight) != 0 {
			return fmt.Errorf("运费明细与 inland_freight 金额不一致，请确认运费后只填写 Freight 字段")
		}
	}
	card.Values["inland_freight"] = freight.FloatString(2)
	card.Items = items
	return nil
}
