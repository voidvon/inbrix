package web

import (
	"context"
	"encoding/json"
	"fmt"
	"inbrix/mailstore"
	"strings"
)

type DocumentValidation struct {
	Status string                 `json:"status"` // draft | ready
	Issues []DocumentPricingIssue `json:"issues"`
}

// No model or UI has a privileged write path. Normalize the canonical inputs,
// render the same snapshot, and derive delivery readiness from that result.
func normalizeCommercialDocument(ctx context.Context, db *mailstore.Store, owner, body, kind string, previous *mailstore.DocumentRecord) (string, *DocumentValidation, error) {
	if kind != "quotation" && kind != "contract" {
		return "", nil, fmt.Errorf("文档类型无效")
	}
	values, items, ok := canvasDocumentContent(body)
	if !ok {
		return "", nil, fmt.Errorf("需要 version=2 商业快照，请重新创建文档")
	}
	trusted := []AIDocumentItem{}
	var trustedValues map[string]string
	if previous != nil {
		trustedValues, trusted, _ = canvasDocumentContent(previous.HTML)
		for key, value := range trustedValues {
			if strings.HasPrefix(key, "source_quotation_") {
				values[key] = value
			}
		}
		for key := range values {
			if strings.HasPrefix(key, "source_quotation_") {
				if _, exists := trustedValues[key]; !exists {
					delete(values, key)
				}
			}
		}
	} else if id := values["source_quotation_id"]; id != "" {
		source, err := db.GetDocument(ctx, owner, id)
		if err != nil || source == nil || source.Type != "quotation" || fmt.Sprint(source.Revision) != values["source_quotation_revision"] {
			return "", nil, fmt.Errorf("来源报价单版本不匹配，请重新生成合同")
		}
		trustedValues, trusted, _ = canvasDocumentContent(source.HTML)
	}
	if trustedValues != nil {
		if err := validateDocumentCurrencyChange(trustedValues, values, items); err != nil {
			return "", nil, err
		}
	}
	card := &AIDocumentCard{Type: kind, Values: values, Items: items}
	calculator := &AISettingsHandler{mailDB: db}
	if err := calculator.finalizeDocumentAmounts(ctx, owner, card, trusted); err != nil {
		return "", nil, err
	}
	if trustedValues != nil {
		clearStaleDocumentAmountWords(card.Values, trustedValues["total_amount"])
		if documentCurrency(trustedValues["currency"]) != documentCurrency(card.Values["currency"]) {
			card.Values["say_total"] = ""
		}
	}
	normalized, err := patchStoredCanvasDocument(body, card.Values, card.Items, true)
	if err != nil {
		return "", nil, err
	}
	validation := validateCommercialDocument(kind, normalized, card)
	return normalized, validation, nil
}

func validateCommercialDocument(kind, body string, card *AIDocumentCard) *DocumentValidation {
	result := &DocumentValidation{Status: "ready", Issues: []DocumentPricingIssue{}}
	add := func(code, field, message string) {
		result.Status = "draft"
		result.Issues = append(result.Issues, DocumentPricingIssue{Code: code, Field: field, Message: message})
	}
	if card.PricingValidation != nil {
		for _, issue := range card.PricingValidation.Issues {
			result.Status = "draft"
			result.Issues = append(result.Issues, issue)
		}
	}
	required := []struct {
		keys  []string
		label string
	}{
		{[]string{"customer_company", "buyer_company"}, "买方公司"},
		{[]string{"seller_company"}, "卖方公司"},
		{[]string{"currency"}, "结算币种"},
		{[]string{"payment_terms"}, "付款条款"},
		{[]string{"lead_time"}, "交货期"},
	}
	if kind == "contract" {
		required = append(required, struct {
			keys  []string
			label string
		}{[]string{"contract_number"}, "合同编号"}, struct {
			keys  []string
			label string
		}{[]string{"contract_date"}, "合同日期"})
	} else {
		required = append(required, struct {
			keys  []string
			label string
		}{[]string{"quote_number"}, "报价单号"}, struct {
			keys  []string
			label string
		}{[]string{"issue_date"}, "报价日期"}, struct {
			keys  []string
			label string
		}{[]string{"validity", "commercial_validity"}, "报价有效期"})
	}
	visible, rendered, _ := canvasProjectionContent(body)
	for _, rule := range required {
		filled := false
		displayed := false
		for _, key := range rule.keys {
			if strings.TrimSpace(card.Values[key]) != "" {
				filled = true
				if func() bool {
					for shown, value := range visible {
						if documentFieldKey(shown) == key && value == card.Values[key] {
							return true
						}
					}
					return false
				}() {
					displayed = true
				}
			}
		}
		if !filled {
			add("missing_field", rule.keys[0], "缺少"+rule.label)
		} else if !displayed {
			add("missing_control", rule.keys[0], rule.label+"未显示在文档中")
		}
	}
	if _, hasTotal := visible["total_amount"]; !hasTotal {
		if _, hasContract := visible["contract_amount"]; !hasContract {
			add("missing_total_display", "total_amount", "模板缺少总价控件")
		}
	}

	if len(rendered) != len(card.Items) {
		add("missing_lines", "items", "文档没有完整显示产品明细")
	}
	for i, item := range card.Items {
		if strings.TrimSpace(item.Model) == "" && strings.TrimSpace(item.Description) == "" {
			result.Status = "draft"
			result.Issues = append(result.Issues, DocumentPricingIssue{Code: "missing_product", Field: "model", Line: i + 1, Message: fmt.Sprintf("第 %d 项缺少产品名称或型号", i+1)})
		}
	}
	return result
}

func marshalDocumentCommands(commands []DocumentCommand) string {
	raw, _ := json.Marshal(map[string]any{"commands": commands})
	return string(raw)
}
