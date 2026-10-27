package web

import (
	"fmt"
	"github.com/google/uuid"
	"strings"
)

// Inputs and calculated outputs are separate. Canvas controls are projections,
// never a fallback source of commercial facts.
var documentBusinessKeys = map[string]bool{
	"inland_freight": true, "freight_tax_rate": true, "freight_tax_mode": true,
	"tax_rate": true, "tax_mode": true, "discount": true, "currency": true,
	"subtotal": true, "tax_amount": true, "total_amount": true, "contract_amount": true, "say_total": true,
	"source_quotation_id": true, "source_quotation_revision": true,
	"source_quotation_name": true, "source_quotation_updated_at": true,
}
var documentCalculatedKeys = map[string]bool{"subtotal": true, "tax_amount": true, "total_amount": true, "contract_amount": true}

// Template labels can vary; party data has exactly one canonical spelling.
func documentFieldKey(key string) string {
	switch key {
	case "buyer_company":
		return "customer_company"
	case "buyer_contact":
		return "customer_contact"
	case "buyer_email":
		return "customer_email"
	case "buyer_phone":
		return "customer_phone"
	case "buyer_address":
		return "customer_address"
	}
	return key
}

type documentBusiness struct {
	Version    int               `json:"version"`
	Values     map[string]string `json:"values"`
	Calculated map[string]string `json:"calculated"`
	Items      []AIDocumentItem  `json:"items"`
}

func (b *documentBusiness) validate() error {
	if b == nil || b.Version != 2 || b.Values == nil || b.Calculated == nil || b.Items == nil {
		return fmt.Errorf("需要 version=2 的完整商业快照，请重新创建文档")
	}
	if len(b.Items) > 500 {
		return fmt.Errorf("产品明细不能超过 500 行")
	}
	seen := map[string]bool{}
	for _, item := range b.Items {
		if strings.TrimSpace(item.LineID) == "" || seen[item.LineID] {
			return fmt.Errorf("产品明细 lineId 缺失或重复")
		}
		seen[item.LineID] = true
	}
	for key := range b.Values {
		if strings.HasPrefix(key, "item_") || documentCalculatedKeys[key] || documentFieldKey(key) != key {
			return fmt.Errorf("商业输入字段 %s 无效", key)
		}
	}
	for key := range b.Calculated {
		if !documentCalculatedKeys[key] {
			return fmt.Errorf("派生金额字段 %s 无效", key)
		}
	}
	return nil
}

func ensureDocumentLineIDs(items []AIDocumentItem) {
	for i := range items {
		if items[i].LineID == "" {
			items[i].LineID = uuid.NewString()
		}
	}
}

func setCanvasBusiness(payload map[string]any, values map[string]string, items []AIDocumentItem) error {
	inputs, calculated := map[string]string{}, map[string]string{}
	for key, value := range values {
		if strings.HasPrefix(key, "item_") {
			continue
		}
		if documentCalculatedKeys[key] {
			calculated[key] = value
		} else {
			canonical := documentFieldKey(key)
			if previous, exists := inputs[canonical]; exists && previous != value {
				return fmt.Errorf("字段 %s 与其模板别名冲突", canonical)
			}
			inputs[canonical] = value
		}
	}
	if items == nil {
		items = []AIDocumentItem{}
	}
	ensureDocumentLineIDs(items)
	snapshot := documentBusiness{Version: 2, Values: inputs, Calculated: calculated, Items: items}
	if err := snapshot.validate(); err != nil {
		return err
	}
	payload["business"] = snapshot
	return nil
}
