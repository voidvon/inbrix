package web

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Patch the saved canvas itself, not a newly materialized default template.
// Generic JSON retains editor options, stamps, free text and unknown properties.
func patchStoredCanvasDocument(body string, values map[string]string, items []AIDocumentItem, replaceItems bool) (string, error) {
	var payload map[string]any
	if !strings.HasPrefix(body, canvasDocumentPrefix) {
		return "", fmt.Errorf("文档不支持结构化修改")
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(body, canvasDocumentPrefix)), &payload); err != nil {
		return "", err
	}
	data, _ := payload["data"].(map[string]any)
	main, ok := data["main"].([]any)
	if !ok {
		return "", fmt.Errorf("文档缺少正文")
	}
	// Existing templates may use unsuffixed, zero-based or one-based item ids.
	slots := map[int]bool{}
	var collect func(any)
	collect = func(node any) {
		switch v := node.(type) {
		case []any:
			for _, child := range v {
				collect(child)
			}
		case map[string]any:
			if c, ok := v["control"].(map[string]any); ok {
				id, _ := c["conceptId"].(string)
				if _, slot, ok := canvasItemConcept(id); ok {
					slots[slot] = true
				}
			}
			for _, key := range []string{"valueList", "trList", "tdList", "value"} {
				if child, ok := v[key].([]any); ok {
					collect(child)
				}
			}
		}
	}
	collect(main)
	indexes := make([]int, 0, len(slots))
	for slot := range slots {
		indexes = append(indexes, slot)
	}
	sort.Ints(indexes)
	positions := map[int]int{}
	for i, slot := range indexes {
		positions[slot] = i
	}
	clone := func(v any) any { raw, _ := json.Marshal(v); var out any; _ = json.Unmarshal(raw, &out); return out }
	var rewrite func([]any, int) error
	rewrite = func(elements []any, rowIndex int) error {
		for _, raw := range elements {
			e, _ := raw.(map[string]any)
			if e == nil {
				continue
			}
			if c, ok := e["control"].(map[string]any); ok {
				id, _ := c["conceptId"].(string)
				value, exists := values[documentFieldKey(id)]
				if retiredDocumentFields[id] {
					value, exists = "", true
				}
				if field, slot, itemControl := canvasItemConcept(id); itemControl {
					exists = false
					if replaceItems {
						i := positions[slot]
						if rowIndex >= 0 {
							i = rowIndex
						}
						if i >= len(items) {
							return fmt.Errorf("当前文档无法删除此明细，请在编辑器调整表格")
						}
						id = fmt.Sprintf("%s_%d", field, i)
						c["conceptId"] = id
						value, exists = canvasItemValues(items[i])[field]
						extension, _ := c["extension"].(map[string]any)
						if extension == nil {
							extension, _ = e["extension"].(map[string]any)
						}
						if extension == nil {
							extension = map[string]any{}
						}
						e["extension"] = extension
						for k, v := range map[string]any{"lineId": items[i].LineID, "model": items[i].Model, "productId": items[i].ProductID, "productVersion": items[i].ProductVersion, "variantId": items[i].VariantID, "priceId": items[i].PriceID} {
							extension[k] = v
						}
						c["extension"] = extension
					}
				}
				if exists {
					runs, _ := c["value"].([]any)
					var text strings.Builder
					for _, r := range runs {
						run, _ := r.(map[string]any)
						s, _ := run["value"].(string)
						text.WriteString(s)
					}
					if text.String() != value {
						run := map[string]any{}
						if len(runs) > 0 {
							if first, ok := clone(runs[0]).(map[string]any); ok {
								run = first
							}
						}
						run["value"] = value
						c["value"] = []any{run}
					}
				}
			}
			if child, ok := e["valueList"].([]any); ok {
				if err := rewrite(child, rowIndex); err != nil {
					return err
				}
			}
			if replaceItems && rowIndex >= 0 && e["type"] == "image" {
				picture, _ := e["value"].(string)
				extension, _ := e["extension"].(map[string]any)
				if strings.HasPrefix(picture, "/api/product-images/") || extension["productId"] != nil || extension["model"] != nil {
					e["value"] = items[rowIndex].Picture
					if extension != nil {
						item := items[rowIndex]
						for k, v := range map[string]any{"model": item.Model, "productId": item.ProductID, "productVersion": item.ProductVersion, "variantId": item.VariantID, "priceId": item.PriceID} {
							extension[k] = v
						}
					}
				}
			}
			if rows, ok := e["trList"].([]any); ok {
				tableExtension, _ := e["extension"].(map[string]any)
				if tableExtension == nil {
					tableExtension = map[string]any{}
					e["extension"] = tableExtension
				}
				// A table keeps a row prototype even when its last commercial line is
				// removed. Re-adding a line never requires reconstructing the document.
				if len(indexes) == 0 && replaceItems && len(items) > 0 && tableExtension["lineTemplate"] != nil {
					rows = append(rows, clone(tableExtension["lineTemplate"]))
					positions[0] = 0
					indexes = []int{0}
				}
				next := make([]any, 0)
				for _, rawRow := range rows {
					row, _ := rawRow.(map[string]any)
					cells, _ := row["tdList"].([]any)
					i := -1
					// Only inspect this row's controls; a nested table owns its rows.
					var find func([]any)
					find = func(els []any) {
						for _, raw := range els {
							el, _ := raw.(map[string]any)
							c, _ := el["control"].(map[string]any)
							id, _ := c["conceptId"].(string)
							if _, slot, ok := canvasItemConcept(id); ok {
								i = positions[slot]
							}
							if child, ok := el["valueList"].([]any); ok {
								find(child)
							}
						}
					}
					for _, rawCell := range cells {
						cell, _ := rawCell.(map[string]any)
						els, _ := cell["value"].([]any)
						find(els)
					}
					writeRow := func(r any, index int) error {
						row, _ := r.(map[string]any)
						cells, _ := row["tdList"].([]any)
						for _, rawCell := range cells {
							cell, _ := rawCell.(map[string]any)
							els, _ := cell["value"].([]any)
							if err := rewrite(els, index); err != nil {
								return err
							}
						}
						next = append(next, r)
						return nil
					}
					if !replaceItems || i < 0 {
						if err := writeRow(rawRow, -1); err != nil {
							return err
						}
						continue
					}
					seed := clone(rawRow)
					tableExtension["lineTemplate"] = clone(seed)
					if i < len(items) {
						if err := writeRow(rawRow, i); err != nil {
							return err
						}
					}
					if i == len(indexes)-1 {
						for j := len(indexes); j < len(items); j++ {
							added := clone(seed)
							clearClonedCanvasIDs(added)
							if err := writeRow(added, j); err != nil {
								return err
							}
						}
					}
				}
				e["trList"] = next
			}
		}
		return nil
	}
	for _, key := range []string{"main", "header", "footer"} {
		if els, ok := data[key].([]any); ok {
			if err := rewrite(els, -1); err != nil {
				return "", err
			}
		}
	}
	if err := setCanvasBusiness(payload, values, items); err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	out := canvasDocumentPrefix + string(raw)
	if replaceItems {
		_, rendered, ok := canvasProjectionContent(out)
		if !ok || len(rendered) != len(items) {
			return "", fmt.Errorf("当前文档无法完整展示修改后的明细，请在编辑器调整表格")
		}
		for i, item := range items {
			if rendered[i].Model != item.Model || rendered[i].Description != item.Description || rendered[i].Qty != item.Qty || rendered[i].Price != item.Price || rendered[i].Amount != item.Amount {
				return "", fmt.Errorf("当前文档缺少产品明细控件，无法安全应用修改")
			}
		}
	}
	return out, nil
}

// The editor assigns fresh row/cell/control identities when opening an added
// row. Reusing the seed's identities would couple selection and control edits.
func clearClonedCanvasIDs(node any) {
	switch v := node.(type) {
	case []any:
		for _, child := range v {
			clearClonedCanvasIDs(child)
		}
	case map[string]any:
		for _, key := range []string{"id", "controlId", "tdId", "trId", "tableId"} {
			delete(v, key)
		}
		for _, key := range []string{"valueList", "trList", "tdList", "value"} {
			if child, ok := v[key].([]any); ok {
				clearClonedCanvasIDs(child)
			}
		}
		if control, ok := v["control"].(map[string]any); ok {
			clearClonedCanvasIDs(control["value"])
		}
	}
}

func canvasItemConcept(id string) (string, int, bool) {
	if _, ok := itemFieldConcept[id]; ok {
		return id, -1, true
	}
	pos := strings.LastIndex(id, "_")
	if pos < 0 {
		return "", 0, false
	}
	base := id[:pos]
	index, err := strconv.Atoi(id[pos+1:])
	_, known := itemFieldConcept[base]
	return base, index, known && err == nil && index >= 0
}

func canvasItemValues(item AIDocumentItem) map[string]string {
	return map[string]string{
		"item_model": item.Model, "item_description": item.Description, "item_desc": item.Description,
		"item_qty": item.Qty, "item_price": item.Price, "item_amount": item.Amount, "item_picture": item.Picture,
		"item_product_id": item.ProductID, "item_product_version": strconv.FormatInt(item.ProductVersion, 10),
		"item_variant_id": item.VariantID, "item_price_id": item.PriceID,
	}
}
