package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// canvasBody builds a stored document body the way the client serializes it.
func canvasBody(t *testing.T, template string, main []map[string]any) string {
	t.Helper()
	payload := map[string]any{"template": template, "data": map[string]any{"main": main}}
	values := map[string]string{}
	raw, _ := json.Marshal(main)
	var elements []canvasElement
	_ = json.Unmarshal(raw, &elements)
	collectCanvasControls(elements, values)
	collectCanvasProductImages(elements, values)
	seedCanonicalTestSnapshot(payload, values, canvasDocumentItems(values))
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return canvasDocumentPrefix + string(encoded)
}

func controlElement(conceptID, value string) map[string]any {
	return map[string]any{
		"type": "control",
		"control": map[string]any{
			"type":      "text",
			"conceptId": conceptID,
			"value":     []map[string]any{{"value": value}},
		},
	}
}

func tableElement(rows [][]map[string]any) map[string]any {
	trList := make([]map[string]any, 0, len(rows))
	for _, cells := range rows {
		tdList := make([]map[string]any, 0, len(cells))
		for _, cell := range cells {
			tdList = append(tdList, map[string]any{"value": []map[string]any{cell}})
		}
		trList = append(trList, map[string]any{"tdList": tdList})
	}
	return map[string]any{"type": "table", "trList": trList}
}

func TestCanvasProductIdentitySurvivesEditorControlSerialization(t *testing.T) {
	for _, location := range []string{"element", "control", "run"} {
		t.Run(location, func(t *testing.T) {
			description := controlElement("item_description_0", "Valve")
			control := description["control"].(map[string]any)
			extension := map[string]any{"model": "M1", "productId": "product", "productVersion": 2, "variantId": "variant", "priceId": "price"}
			switch location {
			case "element":
				description["extension"] = extension
			case "control":
				control["extension"] = extension
			case "run":
				control["value"].([]map[string]any)[0]["extension"] = extension
			}
			body := canvasBody(t, "spirax-contract", []map[string]any{tableElement([][]map[string]any{{description, controlElement("item_qty_0", "1"), controlElement("item_price_0", "100"), controlElement("item_amount_0", "100")}})})
			_, items, ok := canvasDocumentContent(body)
			if !ok || len(items) != 1 || items[0].Model != "M1" || items[0].ProductID != "product" || items[0].ProductVersion != 2 || items[0].VariantID != "variant" || items[0].PriceID != "price" {
				t.Fatal("lost catalog identity", items)
			}
			if err := validateDocumentCurrency(&AIDocumentCard{Currency: "USD", Items: items}); err == nil {
				t.Fatal("serialized catalog price bypassed currency validation")
			}
			patched, err := patchStoredCanvasDocument(body, nil, items, true)
			if err != nil {
				t.Fatal(err)
			}
			payload, _ := parseStoredCanvasDocument(patched)
			saved := payload.Data.Main[0].TrList[0].TdList[0].Value[0].Control.Extension
			if saved == nil || saved.PriceID != "price" {
				t.Fatal("patch did not store metadata on serializable control", saved)
			}
		})
	}
}

func TestCanvasDocumentContentReadsFieldsAndItems(t *testing.T) {
	body := canvasBody(t, "spirax-quotation", []map[string]any{
		controlElement("quote_number", "SQ-202610"),
		controlElement("buyer_company", "上海宝钢"),
		controlElement("buyer_contact", "王工"),
		controlElement("total_amount", "14,464.00"),
		// Empty controls remain discoverable so AI can fill or explicitly clear them.
		controlElement("seller_bank", ""),
		tableElement([][]map[string]any{
			{
				controlElement("item_model_0", "DN50-BV"),
				controlElement("item_description_0", "DN50 球阀"),
				controlElement("item_qty_0", "10"),
				controlElement("item_price_0", "1280.00"),
				controlElement("item_amount_0", "12,800.00"),
			},
			{
				controlElement("item_model_1", "TD42"),
				controlElement("item_description_1", "疏水阀"),
				controlElement("item_qty_1", "10"),
				controlElement("item_price_1", "380.00"),
				controlElement("item_amount_1", "3,800.00"),
			},
		}),
	})

	values, items, ok := canvasDocumentContent(body)
	if !ok {
		t.Fatal("canvas document not recognised")
	}
	if values["customer_company"] != "上海宝钢" || values["quote_number"] != "SQ-202610" {
		t.Fatalf("fields = %+v", values)
	}
	if value, present := values["seller_bank"]; !present || value != "" {
		t.Fatalf("empty editable control was lost: %+v", values)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Model != "DN50-BV" || items[0].Qty != "10" || items[0].Price != "1280.00" {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[1].Model != "TD42" || items[1].Amount != "3,800.00" {
		t.Fatalf("second item = %+v", items[1])
	}
	// Ordered by index, not by map iteration.
	if items[0].Model != "DN50-BV" || items[1].Model != "TD42" {
		t.Fatalf("items out of order: %+v", items)
	}
}

func TestCanvasDocumentContentSingleItemAndLegacyHTML(t *testing.T) {
	body := canvasBody(t, "spirax-contract", []map[string]any{
		controlElement("contract_number", "SC-202610"),
		tableElement([][]map[string]any{{
			controlElement("item_model", "DP27"),
			controlElement("item_qty", "5"),
		}}),
	})
	values, items, ok := canvasDocumentContent(body)
	if !ok || values["contract_number"] != "SC-202610" {
		t.Fatalf("values = %+v ok=%v", values, ok)
	}
	if len(items) != 1 || items[0].Model != "DP27" || items[0].Qty != "5" {
		t.Fatalf("items = %+v", items)
	}

	// A legacy plain-HTML document is reported as unreadable rather than empty.
	if _, _, ok := canvasDocumentContent("<div>plain html quotation</div>"); ok {
		t.Fatal("legacy HTML was treated as a canvas document")
	}
	if _, _, ok := canvasDocumentContent(canvasDocumentPrefix + "{not json"); ok {
		t.Fatal("malformed canvas payload was accepted")
	}
}

func TestDocumentValueKeysAreStable(t *testing.T) {
	keys := documentValueKeys(map[string]string{"zeta": "1", "alpha": "2", "mid": "3"})
	if strings.Join(keys, ",") != "alpha,mid,zeta" {
		t.Fatalf("keys = %v", keys)
	}
}
