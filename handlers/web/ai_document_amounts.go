package web

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var documentDecimal = regexp.MustCompile(`^\d+(?:\.\d+)?$`)

func documentNumber(text string) (*big.Rat, error) {
	text = strings.ReplaceAll(strings.TrimSpace(text), ",", "")
	if !documentDecimal.MatchString(text) {
		return nil, fmt.Errorf("无效的非负金额或数量：%q", text)
	}
	n, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("无效的数字：%q", text)
	}
	return n, nil
}

func roundedMoney(n *big.Rat) *big.Rat {
	r, _ := new(big.Rat).SetString(n.FloatString(2))
	return r
}

func documentPricingChanged(values map[string]string) bool {
	for _, key := range []string{"discount", "subtotal", "total_amount", "contract_amount", "inland_freight"} {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

// One deterministic decimal calculation for tools, manual lines and catalog
// lines. Round each line to cents before summing. Never trust supplied totals.
func calculateDocumentAmounts(card *AIDocumentCard) error {
	if card.Values == nil {
		card.Values = map[string]string{}
	}
	ensureDocumentLineIDs(card.Items)
	card.PricingValidation = &DocumentPricingValidation{Status: "complete", Issues: []DocumentPricingIssue{}}
	previousTotal := card.Values["total_amount"]
	if previousTotal == "" {
		previousTotal = card.Values["contract_amount"]
	}
	// Discard legacy document tax fields. Unit prices are the amounts to quote.
	for key := range retiredDocumentFields {
		delete(card.Values, key)
	}
	card.TaxRate, card.TaxAmount = "", ""
	var err error
	discount := new(big.Rat)
	if text := card.Values["discount"]; text != "" {
		discount, err = documentNumber(text)
		if err != nil {
			return err
		}
		discount = roundedMoney(discount)
	}
	freight := new(big.Rat)
	if text := strings.TrimSpace(card.Values["inland_freight"]); text != "" {
		freight, err = documentNumber(text)
		if err != nil {
			return fmt.Errorf("运费: %w", err)
		}
		freight = roundedMoney(freight)
	}
	subtotal := new(big.Rat)
	complete := len(card.Items) > 0
	if !complete {
		card.PricingValidation.add("missing_items", "items", 0, "尚未填写产品明细")
	}
	for i := range card.Items {
		item := &card.Items[i]
		item.Amount = ""
		item.TaxRate, item.TaxMode = "", ""
		if strings.TrimSpace(item.Qty) == "" || strings.TrimSpace(item.Price) == "" {
			complete = false
			if strings.TrimSpace(item.Qty) == "" {
				card.PricingValidation.add("missing_quantity", "qty", i+1, fmt.Sprintf("第 %d 项缺少数量", i+1))
			}
			if strings.TrimSpace(item.Price) == "" {
				card.PricingValidation.add("missing_price", "price", i+1, fmt.Sprintf("第 %d 项缺少单价", i+1))
			}
			continue
		}
		q, err := documentNumber(item.Qty)
		if err != nil {
			return fmt.Errorf("明细 %d: %w", i+1, err)
		}
		p, err := documentNumber(item.Price)
		if err != nil {
			return fmt.Errorf("明细 %d: %w", i+1, err)
		}
		if q.Sign() <= 0 {
			return fmt.Errorf("明细 %d 数量必须大于 0", i+1)
		}
		amount := roundedMoney(new(big.Rat).Mul(q, p))
		item.Amount = amount.FloatString(2)
		subtotal.Add(subtotal, amount)
	}
	card.Subtotal, card.Total = "", ""
	if complete {
		if discount.Cmp(subtotal) > 0 {
			return fmt.Errorf("折扣金额不能超过小计")
		}
		card.Subtotal = subtotal.FloatString(2)
		card.Total = new(big.Rat).Add(new(big.Rat).Sub(subtotal, discount), freight).FloatString(2)
	}
	for key, value := range map[string]string{"subtotal": card.Subtotal, "total_amount": card.Total, "contract_amount": card.Total} {
		card.Values[key] = value
	}
	clearStaleDocumentAmountWords(card.Values, previousTotal)
	return nil
}

// Words are user-authored and may use any language/currency. Invalidate them
// when their numerical basis changes rather than inventing a translation.
func clearStaleDocumentAmountWords(values map[string]string, previousTotal string) {
	if values["say_total"] == "" {
		return
	}
	previous, oldErr := documentNumber(previousTotal)
	current, newErr := documentNumber(values["total_amount"])
	if oldErr != nil || newErr != nil || previous.Cmp(current) != 0 {
		values["say_total"] = ""
	}
}
