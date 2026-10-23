package web

import (
	"fmt"
	"strings"

	"inbrix/mailstore"
)

func documentCurrency(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case "¥", "￥", "人民币", "RMB":
		return "CNY"
	}
	return value
}

// Currency labels are not conversions. Catalog prices currently have a single
// currency; never turn a CNY price into a foreign price by changing its label.
func validateDocumentCurrency(card *AIDocumentCard) error {
	if card.Values == nil {
		card.Values = map[string]string{}
	}
	currency, field := documentCurrency(card.Currency), documentCurrency(card.Values["currency"])
	if currency != "" && field != "" && currency != field {
		return fmt.Errorf("文档币种不一致，请确认结算币种")
	}
	if field != "" {
		currency = field
	}
	for _, item := range card.Items {
		if item.PriceID != "" || item.VariantID != "" {
			if currency != "" && currency != mailstore.ProductPriceCurrency {
				return fmt.Errorf("产品库价格为 CNY，缺少经确认的汇率，不支持直接改为其他币种")
			}
			currency = mailstore.ProductPriceCurrency
		}
	}
	card.Currency = currency
	if currency != "" {
		card.Values["currency"] = currency
	}
	return nil
}

func validateDocumentCurrencyChange(previous map[string]string, values map[string]string, items []AIDocumentItem) error {
	if documentCurrency(previous["currency"]) == documentCurrency(values["currency"]) {
		return nil
	}
	for _, item := range items {
		if item.Price != "" {
			return fmt.Errorf("已有金额的币种不能直接更改；请先确认原币种和汇率，再重新提供换算后的价格")
		}
	}
	return nil
}
