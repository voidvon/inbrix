package web

import (
	"encoding/json"
	"fmt"
	"strings"
)

func reusableTemplateField(id string) bool {
	return strings.HasPrefix(id, "seller_") || strings.HasPrefix(id, "bank_") ||
		strings.HasPrefix(id, "supplier_") || strings.HasPrefix(id, "beneficiary_") ||
		id == "swift_code" || id == "iban" || id == "payment_method" || id == "payment_memo"
}

// Materialize fields into a copy of the saved canvas. Unknown layout and style
// properties survive; source templates are never mutated.
func materializeBusinessTemplate(body string, card *AIDocumentCard) (string, error) {
	if !strings.HasPrefix(body, canvasDocumentPrefix) {
		if strings.Contains(body, "data-spirax-") {
			return "", nil
		}
		return "", fmt.Errorf("默认模板需要在文档编辑器中保存为结构化模板")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(body, canvasDocumentPrefix)), &payload); err != nil {
		return "", err
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("模板缺少文档内容")
	}
	main, ok := data["main"].([]any)
	if !ok {
		return "", fmt.Errorf("模板缺少正文")
	}
	clone := func(v any) any { raw, _ := json.Marshal(v); var out any; _ = json.Unmarshal(raw, &out); return out }
	var concepts func([]any) []string
	concepts = func(elements []any) []string {
		var out []string
		for _, raw := range elements {
			e, _ := raw.(map[string]any)
			c, _ := e["control"].(map[string]any)
			if id, _ := c["conceptId"].(string); id != "" {
				out = append(out, id)
			}
			for _, key := range []string{"valueList"} {
				if child, ok := e[key].([]any); ok {
					out = append(out, concepts(child)...)
				}
			}
			if rows, ok := e["trList"].([]any); ok {
				for _, r := range rows {
					row, _ := r.(map[string]any)
					cells, _ := row["tdList"].([]any)
					for _, c := range cells {
						cell, _ := c.(map[string]any)
						child, _ := cell["value"].([]any)
						out = append(out, concepts(child)...)
					}
				}
			}
		}
		return out
	}
	values := map[string]string{}
	for k, v := range card.Values {
		values[k] = v
	}
	for i, item := range card.Items {
		fields := map[string]string{"model": item.Model, "description": item.Description, "desc": item.Description, "qty": item.Qty, "price": item.Price, "amount": item.Amount, "picture": item.Picture}
		for k, v := range fields {
			values[fmt.Sprintf("item_%s_%d", k, i)] = v
			if i == 0 {
				values["item_"+k] = v
			}
		}
	}
	itemConcept := func(id string) bool { return strings.HasPrefix(id, "item_") }
	var rewrite func([]any, int)
	rewrite = func(elements []any, index int) {
		for _, raw := range elements {
			e, _ := raw.(map[string]any)
			if e == nil {
				continue
			}
			if c, ok := e["control"].(map[string]any); ok {
				id, _ := c["conceptId"].(string)
				if itemConcept(id) && index >= 0 {
					base := id
					for concept := range itemFieldConcept {
						if id == concept || strings.HasPrefix(id, concept+"_") {
							base = concept
							break
						}
					}
					id = fmt.Sprintf("%s_%d", base, index)
					c["conceptId"] = id
					item := card.Items[index]
					e["extension"] = map[string]any{"productId": item.ProductID, "productVersion": item.ProductVersion, "variantId": item.VariantID, "priceId": item.PriceID, "model": item.Model}
				}
				key := documentFieldKey(id)
				value, exists := values[key]
				if !exists && reusableTemplateField(id) {
					// Template defaults must enter the canonical snapshot, not just
					// survive in controls: the editor renders from card.Values.
					var text strings.Builder
					if runs, ok := c["value"].([]any); ok {
						for _, rawRun := range runs {
							run, _ := rawRun.(map[string]any)
							part, _ := run["value"].(string)
							text.WriteString(part)
						}
					}
					value = strings.TrimSpace(text.String())
					values[key] = value
					if card.Values == nil {
						card.Values = map[string]string{}
					}
					card.Values[key] = value
				}
				run := map[string]any{"value": value}
				if runs, ok := c["value"].([]any); ok && len(runs) > 0 {
					if seed, ok := clone(runs[0]).(map[string]any); ok {
						run = seed
						run["value"] = value
					}
				}
				c["value"] = []any{run}
			}
			if child, ok := e["valueList"].([]any); ok {
				rewrite(child, index)
			}
			if rows, ok := e["trList"].([]any); ok {
				var seed any
				var next []any
				inserted := false
				for _, r := range rows {
					row, _ := r.(map[string]any)
					cells, _ := row["tdList"].([]any)
					isItem := false
					for _, rawCell := range cells {
						cell, _ := rawCell.(map[string]any)
						els, _ := cell["value"].([]any)
						for _, id := range concepts(els) {
							if itemConcept(id) {
								isItem = true
							}
						}
					}
					if isItem {
						if inserted {
							continue
						}
						seed = r
						inserted = true
						for i := range card.Items {
							copyRow, _ := clone(seed).(map[string]any)
							copiedCells, _ := copyRow["tdList"].([]any)
							for _, rawCell := range copiedCells {
								cell, _ := rawCell.(map[string]any)
								els, _ := cell["value"].([]any)
								rewrite(els, i)
								// Replace only product image placeholders in item rows.
								for _, v := range els {
									image, _ := v.(map[string]any)
									if image["type"] == "image" {
										image["value"] = card.Items[i].Picture
									}
								}
							}
							next = append(next, copyRow)
						}
					} else {
						for _, rawCell := range cells {
							cell, _ := rawCell.(map[string]any)
							els, _ := cell["value"].([]any)
							rewrite(els, index)
						}
						next = append(next, r)
					}
				}
				e["trList"] = next
			}
		}
	}
	if len(card.Items) > 0 {
		found := false
		for _, id := range concepts(main) {
			if itemConcept(id) {
				found = true
			}
		}
		if !found {
			return "", fmt.Errorf("默认模板没有产品明细控件，请在模板中添加产品明细")
		}
	}
	rewrite(main, -1)
	// Headers and footers can contain company controls too.
	for _, key := range []string{"header", "footer"} {
		if els, ok := data[key].([]any); ok {
			rewrite(els, -1)
		}
	}
	if err := setCanvasBusiness(payload, card.Values, card.Items); err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	out := canvasDocumentPrefix + string(raw)
	_, rendered, ok := canvasProjectionContent(out)
	if len(card.Items) > 0 {
		if !ok || len(rendered) != len(card.Items) {
			return "", fmt.Errorf("默认模板无法完整展示产品明细，请使用包含产品表格的模板")
		}
		for i, item := range card.Items {
			if rendered[i].Model != item.Model || rendered[i].Qty != item.Qty || rendered[i].Price != item.Price || rendered[i].Amount != item.Amount {
				return "", fmt.Errorf("默认模板缺少型号、数量、单价或金额控件，无法完整填入报价明细")
			}
		}
	}
	return out, nil
}
