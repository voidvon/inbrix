package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"inbrix/mailstore"
)

var documentProductReference = regexp.MustCompile(`\bprod_[A-Za-z0-9_-]+\b`)

// Pasted IDs can come from another deployment. They are source material until
// checked against the current owner, never a reason to import or invent data.
func (h *AISettingsHandler) documentCatalogReferenceContext(ctx context.Context, owner string, input aiDocumentInput) (string, error) {
	text := input.Instruction
	for _, message := range input.History {
		text += "\n" + message.Content
	}
	type reference struct {
		ID        string `json:"id"`
		Available bool   `json:"available"`
		Version   int64  `json:"currentVersion,omitempty"`
	}
	refs := []reference{}
	seen := map[string]bool{}
	for _, id := range documentProductReference.FindAllString(text, -1) {
		if seen[id] {
			continue
		}
		seen[id] = true
		p, err := h.mailDB.GetProduct(ctx, owner, id)
		if err != nil && !errors.Is(err, mailstore.ErrNotFound) {
			return "", err
		}
		refs = append(refs, reference{ID: id, Available: err == nil, Version: p.Version})
	}
	if len(refs) == 0 {
		return "", nil
	}
	raw, _ := json.Marshal(refs)
	policy := "Manual lines are allowed. For a product explicitly unavailable here, use the user's supplied model, description, quantity and unit price as a manual line with no productId/productVersion/variantId/priceId. Explain in message that pasted catalog references could not be verified locally. Do not attach unavailable IDs or convert CNY reference prices into USD. Use explicit user-supplied USD prices unchanged."
	if currentBusiness(ctx).Brands != nil {
		policy = "Manual lines are not allowed by the current brand scope. If the requested product is unavailable here, return commands:[] and ask the user to select an available product or adjust the mailbox brand scope."
	}
	return "\n\nCURRENT CATALOG REFERENCE CHECKS (verified by server for this owner):\n" + string(raw) + "\n" + policy + " Existing catalog-bound lines must retain their verified identity and price source; never downgrade them to manual lines. If that prevents the edit, return commands:[] and explain the conflict.\n", nil
}

func documentProductLookupError(index int, item AIDocumentItem, err error) error {
	if errors.Is(err, mailstore.ErrNotFound) {
		name := strings.TrimSpace(item.Model)
		if name == "" {
			name = item.ProductID
		}
		return fmt.Errorf("第 %d 项（%s）的产品或图片引用在当前账号产品库中不存在，请重新选择当前产品；允许手工报价时可按已提供的型号和单价填写：%w", index+1, name, err)
	}
	return err
}
