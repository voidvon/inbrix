package web

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"inbrix/mailstore"
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

func documentTax(rate, mode string) (*big.Rat, string, error) {
	if mode != "" && mode != "included" && mode != "excluded" {
		return nil, "", fmt.Errorf("tax_mode 必须是 included 或 excluded")
	}
	rate = strings.TrimSpace(rate)
	if rate == "" {
		return nil, mode, nil
	}
	percent := strings.HasSuffix(rate, "%")
	n, err := documentNumber(strings.TrimSuffix(rate, "%"))
	if err != nil {
		return nil, "", err
	}
	if percent {
		n.Quo(n, big.NewRat(100, 1))
	}
	if n.Cmp(big.NewRat(1, 1)) > 0 {
		return nil, "", fmt.Errorf("税率必须在 0 到 1 之间，或使用百分号")
	}
	// Legacy explicit tax_rate means tax added to the unit price. No rate is
	// ever inferred when absent; tax_mode can explicitly request included tax.
	if mode == "" {
		mode = "excluded"
	}
	return n, mode, nil
}

func documentPricingChanged(values map[string]string) bool {
	for _, key := range []string{"tax_rate", "tax_mode", "discount", "subtotal", "tax_amount", "total_amount", "contract_amount", "inland_freight", "freight_tax_rate", "freight_tax_mode"} {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

// One deterministic decimal calculation for tools, manual lines and catalog
// lines. Round each line to cents before summing. Never trust supplied totals.
func calculateDocumentAmounts(card *AIDocumentCard, prices map[int]mailstore.ProductPrice) error {
	if card.Values == nil {
		card.Values = map[string]string{}
	}
	previousTotal := card.Values["total_amount"]
	if previousTotal == "" {
		previousTotal = card.Values["contract_amount"]
	}
	rateText := card.Values["tax_rate"]
	if rateText == "" {
		rateText = card.TaxRate
	}
	card.Values["tax_rate"] = rateText
	rate, mode, err := documentTax(rateText, card.Values["tax_mode"])
	if err != nil {
		return err
	}
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
	freightRate, freightMode, err := documentTax(card.Values["freight_tax_rate"], card.Values["freight_tax_mode"])
	if err != nil {
		return fmt.Errorf("运费: %w", err)
	}
	if card.Values["freight_tax_mode"] == "" {
		freightMode = ""
	}
	subtotal := new(big.Rat)
	amounts := make([]*big.Rat, len(card.Items))
	complete := len(card.Items) > 0
	for i := range card.Items {
		item := &card.Items[i]
		item.Amount = ""
		if strings.TrimSpace(item.Qty) == "" || strings.TrimSpace(item.Price) == "" {
			complete = false
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
		amounts[i] = roundedMoney(new(big.Rat).Mul(q, p))
		item.Amount = amounts[i].FloatString(2)
		subtotal.Add(subtotal, amounts[i])
	}
	card.Subtotal, card.Total, card.TaxAmount, card.TaxRate = "", "", "", ""
	if complete {
		if discount.Cmp(subtotal) > 0 {
			return fmt.Errorf("折扣金额不能超过小计")
		}
		card.Subtotal = subtotal.FloatString(2)
		tax, extra := new(big.Rat), new(big.Rat)
		known, sameRate := true, true
		label := ""
		for i, amount := range amounts {
			r, m := rate, mode
			if price, catalog := prices[i]; catalog {
				if price.TaxMode == "" || price.TaxRate == "" {
					known = false
					continue
				}
				r, m, err = documentTax(price.TaxRate, price.TaxMode)
				if err != nil {
					return err
				}
			}
			if r == nil {
				known = false
				continue
			}
			currentLabel := new(big.Rat).Mul(r, big.NewRat(100, 1)).FloatString(2) + "%"
			if label != "" && label != currentLabel {
				sameRate = false
			}
			label = currentLabel
			base := new(big.Rat).Set(amount)
			if subtotal.Sign() > 0 {
				base.Sub(base, new(big.Rat).Quo(new(big.Rat).Mul(discount, amount), subtotal))
			}
			lineTax := new(big.Rat).Mul(base, r)
			if m == "included" {
				lineTax.Quo(lineTax, new(big.Rat).Add(big.NewRat(1, 1), r))
			}
			lineTax = roundedMoney(lineTax)
			tax.Add(tax, lineTax)
			if m == "excluded" {
				extra.Add(extra, lineTax)
			}
		}
		if freight.Sign() > 0 {
			if freightRate == nil || freightMode == "" {
				known = false
			} else {
				freightTax := new(big.Rat).Mul(freight, freightRate)
				if freightMode == "included" {
					freightTax.Quo(freightTax, new(big.Rat).Add(big.NewRat(1, 1), freightRate))
				}
				freightTax = roundedMoney(freightTax)
				tax.Add(tax, freightTax)
				if freightMode == "excluded" {
					extra.Add(extra, freightTax)
				}
			}
		}
		if known {
			card.TaxAmount = tax.FloatString(2)
			card.Total = new(big.Rat).Add(new(big.Rat).Add(new(big.Rat).Sub(subtotal, discount), freight), extra).FloatString(2)
			if sameRate {
				card.TaxRate = label
			}
		}
	}
	for key, value := range map[string]string{"subtotal": card.Subtotal, "tax_amount": card.TaxAmount, "total_amount": card.Total, "contract_amount": card.Total} {
		card.Values[key] = value
	}
	// Keep the explicit manual rate as an input on mixed or incomplete drafts.
	// Catalog-only rate labels are derived from their stored prices.
	if len(prices) == len(card.Items) {
		card.Values["tax_rate"] = card.TaxRate
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
