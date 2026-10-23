package web

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// One validated, lossless mutation path for saved documents and editor snapshots.
// This produces a proposal only; callers decide when to apply/save it.
func (h *AISettingsHandler) proposeCanvasDocumentUpdate(ctx context.Context, owner, body string, updates map[string]string, updatedItems []AIDocumentItem) (*AIDocumentAction, error) {
	values, items, ok := canvasDocumentContent(body)
	if !ok {
		return nil, fmt.Errorf("文档不支持结构化修改或商业数据版本不受支持，请检查模板")
	}
	changed := make([]string, 0, len(updates))
	merged := make(map[string]string, len(values)+len(updates))
	for key, value := range values {
		merged[key] = value
	}
	for key, value := range updates {
		if strings.HasPrefix(key, "item_") {
			return nil, fmt.Errorf("请使用 items 修改产品明细，以便验证规格与价格")
		}
		if _, exists := values[key]; !exists && !documentBusinessKeys[key] {
			return nil, fmt.Errorf("文档缺少字段 %s 的控件，无法应用修改；请先在模板中添加对应控件", key)
		}
		if strings.TrimSpace(merged[key]) != strings.TrimSpace(value) {
			changed = append(changed, key)
		}
		merged[key] = value
	}
	sort.Strings(changed)
	if len(updatedItems) > 0 {
		// Retain identity when an update only changes the quantity/price of an
		// existing line. Omitting refs must not downgrade catalog prices to manual.
		for i := range updatedItems {
			if i >= len(items) {
				continue
			}
			old, next := items[i], &updatedItems[i]
			sameProduct := (next.ProductID != "" && next.ProductID == old.ProductID) ||
				(next.ProductID == "" && (next.Model == old.Model || (next.Model == "" && next.Description == old.Description)))
			if !sameProduct {
				continue
			}
			if next.Model == "" {
				next.Model = old.Model
			}
			if next.Picture == "" {
				next.Picture = old.Picture
			}
			if next.ProductID == "" {
				next.ProductID = old.ProductID
			}
			if next.ProductVersion == 0 {
				next.ProductVersion = old.ProductVersion
			}
			if next.VariantID == "" {
				next.VariantID = old.VariantID
			}
			if next.PriceID == "" {
				next.PriceID = old.PriceID
			}
		}
		if err := h.resolveProductPictures(ctx, owner, updatedItems); err != nil {
			return nil, err
		}
		items = updatedItems
		changed = append(changed, fmt.Sprintf("items(%d)", len(updatedItems)))
	}
	if len(changed) == 0 {
		return nil, nil
	}
	if err := h.checkBusinessItems(ctx, owner, items); err != nil {
		return nil, err
	}
	action := &AIDocumentAction{
		Kind:    "update",
		Values:  merged,
		Items:   items,
		Summary: "将修改：" + strings.Join(changed, ", "),
	}
	if _, exists := updates["currency"]; exists {
		card := &AIDocumentCard{Values: action.Values, Items: action.Items}
		if err := validateDocumentCurrency(card); err != nil {
			return nil, err
		}
		if err := validateDocumentCurrencyChange(values, action.Values, items); err != nil {
			return nil, err
		}
	}
	if currency, exists := updates["currency"]; exists && documentCurrency(currency) != documentCurrency(values["currency"]) {
		action.Values["say_total"] = ""
	}
	recalculate := len(updatedItems) > 0 || documentPricingChanged(updates)
	if recalculate {
		card := &AIDocumentCard{Items: action.Items, Values: action.Values}
		if err := h.finalizeDocumentAmounts(ctx, owner, card); err != nil {
			return nil, err
		}
		previousTotal := values["total_amount"]
		if previousTotal == "" {
			previousTotal = values["contract_amount"]
		}
		clearStaleDocumentAmountWords(action.Values, previousTotal)
	}
	patched, err := patchStoredCanvasDocument(body, action.Values, action.Items, recalculate)
	if err != nil {
		return nil, err
	}
	action.HTML = patched
	// Verify the rendered result, then expose only fields actually stored.
	patchedValues, patchedItems, ok := canvasDocumentContent(action.HTML)
	if !ok {
		return nil, fmt.Errorf("修改后的文档无法读取")
	}
	for key := range updates {
		if got, exists := patchedValues[key]; !exists || strings.TrimSpace(got) != strings.TrimSpace(action.Values[key]) {
			return nil, fmt.Errorf("字段 %s 未完整写入文档，请检查模板控件", key)
		}
	}
	if values["say_total"] != "" && patchedValues["say_total"] == "" {
		action.Summary += "；金额或币种变化，已清空金额大写，请核对后补填"
	}
	if freight, err := documentNumber(patchedValues["inland_freight"]); err == nil && recalculate && patchedValues["total_amount"] == "" && freight.Sign() > 0 && (patchedValues["freight_tax_rate"] == "" || patchedValues["freight_tax_mode"] == "") {
		action.Summary += "；运费已单列，税率或含税口径待确认，最终金额留空"
	}
	action.Values, action.Items = patchedValues, patchedItems

	return action, nil
}
func (h *AISettingsHandler) respondCanvasDocumentUpdate(c *fiber.Ctx, owner string, input aiDocumentInput, values map[string]string, items []AIDocumentItem) error {
	action, err := h.proposeCanvasDocumentUpdate(c.UserContext(), owner, input.Canvas, values, items)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	if action == nil {
		return c.JSON(fiber.Map{"mode": "variables", "html": input.Canvas, "summary": "没有需要应用的变更"})
	}
	return c.JSON(fiber.Map{"mode": "variables", "html": action.HTML, "values": action.Values, "items": action.Items, "summary": action.Summary})
}
