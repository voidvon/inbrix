package web

import "fmt"

// Commercial inputs must survive templates that do not display a tax breakdown.
// Keep this allowlist aligned with frontend/src/lib/document-business.ts.
var documentBusinessKeys = map[string]bool{
	"inland_freight": true, "freight_tax_rate": true, "freight_tax_mode": true,
	"tax_rate": true, "tax_mode": true, "discount": true, "currency": true,
	"subtotal": true, "tax_amount": true, "total_amount": true, "contract_amount": true, "say_total": true,
	"source_quotation_id": true, "source_quotation_revision": true,
	"source_quotation_name": true, "source_quotation_updated_at": true,
}

type documentBusiness struct {
	Version int               `json:"version"`
	Values  map[string]string `json:"values"`
}

func setCanvasBusiness(payload map[string]any, values map[string]string) error {
	if existing, exists := payload["business"]; exists && existing != nil {
		business, ok := existing.(map[string]any)
		if !ok || business["version"] != float64(1) {
			return fmt.Errorf("文档商业数据版本不受支持，请升级后再修改")
		}
	}
	selected := map[string]string{}
	for key, value := range values {
		if documentBusinessKeys[key] {
			selected[key] = value
		}
	}
	if len(selected) > 0 {
		payload["business"] = documentBusiness{Version: 1, Values: selected}
	} else {
		delete(payload, "business")
	}
	return nil
}
