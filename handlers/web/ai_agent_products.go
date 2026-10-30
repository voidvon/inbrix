package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"inbrix/mailstore"
)

func productToolInt(args map[string]any, key string, fallback, minValue, maxValue int) (int, error) {
	v, ok := args[key]
	if !ok {
		return fallback, nil
	}
	n, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil || n < minValue || n > maxValue {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minValue, maxValue)
	}
	return n, nil
}

func (h *AISettingsHandler) executeProductTool(ctx context.Context, owner string, call AIToolCall) (any, string, error) {
	switch call.Name {
	case "search_products":
		limit, err := productToolInt(call.Args, "limit", 5, 1, 20)
		if err != nil {
			return nil, "", err
		}
		offset, err := productToolInt(call.Args, "offset", 0, 0, 100000)
		if err != nil {
			return nil, "", err
		}
		filter := mailstore.ProductSearchFilter{Brands: currentBusiness(ctx).Brands}
		if _, ok := call.Args["brand"]; ok {
			brand := parseStringArg(call.Args, "brand")
			filter.Brand = &brand
		}
		page, err := h.mailDB.SearchProducts(ctx, owner, parseStringArg(call.Args, "query"), parseStringArg(call.Args, "categoryId"), limit, offset, filter)
		// Fit even unusually long names/excerpts into the existing tool budget.
		for len(page.Products) > 1 {
			encoded, _ := json.Marshal(page)
			if len(encoded) <= 16_000 {
				break
			}
			page.Products = page.Products[:len(page.Products)-1]
			page.HasMore = true
			page.NextOffset = offset + len(page.Products)
		}
		return page, fmt.Sprintf("已检索产品资料，返回 %d / %d 个产品", len(page.Products), page.Total), err
	case "read_product":
		if err := h.checkBusinessProduct(ctx, owner, parseStringArg(call.Args, "id")); err != nil {
			return nil, "", err
		}
		version, err := productToolInt(call.Args, "version", 0, 1, 1<<31-1)
		if err != nil || version == 0 {
			return nil, "", fmt.Errorf("version from search_products is required")
		}
		offset, err := productToolInt(call.Args, "offset", 0, 0, mailstore.MaxProductSpecificationRunes)
		if err != nil {
			return nil, "", err
		}
		length, err := productToolInt(call.Args, "length", 2400, 1, 4000)
		if err != nil {
			return nil, "", err
		}
		result, err := h.mailDB.ReadProduct(ctx, owner, parseStringArg(call.Args, "id"), int64(version), offset, length)
		for err == nil {
			encoded, _ := json.Marshal(result)
			if len(encoded) <= 18_000 {
				break
			}
			if len(result.Sections) > 1 {
				result.Sections = result.Sections[:len(result.Sections)-1]
				result.HasMoreSections = true
			} else {
				content := []rune(result.Content)
				if len(content) <= 1 {
					break
				}
				result.Content = string(content[:len(content)/2])
				result.NextOffset = result.Offset + len(content)/2
				result.HasMore = result.NextOffset < result.TotalLength
			}
		}
		return result, fmt.Sprintf("已读取产品《%s》v%d，位置 %d–%d", result.Product.Name, result.Product.Version, result.Offset, result.NextOffset), err
	case "list_product_categories":
		limit, err := productToolInt(call.Args, "limit", 30, 1, 50)
		if err != nil {
			return nil, "", err
		}
		offset, err := productToolInt(call.Args, "offset", 0, 0, 100000)
		if err != nil {
			return nil, "", err
		}
		items, err := h.mailDB.ListProductCategories(ctx, owner)
		if err != nil {
			return nil, "", err
		}
		start := min(offset, len(items))
		end := min(start+limit, len(items))
		return map[string]any{"categories": items[start:end], "total": len(items), "hasMore": end < len(items), "nextOffset": end}, "已读取产品分类", nil
	}
	return nil, "", fmt.Errorf("unknown product tool")
}

// Product image URLs are immutable, owner-scoped references. Existing document
// snapshots survive product edits/deletion without putting binary images in an
// LLM context or giving the LLM arbitrary image URLs to insert.
func (h *AISettingsHandler) resolveProductPictures(ctx context.Context, owner string, items []AIDocumentItem, trusted ...[]AIDocumentItem) error {
	if err := h.checkBusinessItems(ctx, owner, items); err != nil {
		return err
	}
	previous := map[string]AIDocumentItem{}
	if len(trusted) > 0 {
		for _, item := range trusted[0] {
			previous[item.LineID] = item
		}
	}
	for index := range items {
		item := &items[index]
		// Existing snapshots retain their images and authored descriptions.
		// Quantity/price changes are validated separately by the amount resolver.
		if old, ok := previous[item.LineID]; ok && old.ProductID == item.ProductID && old.ProductVersion == item.ProductVersion && old.VariantID == item.VariantID && old.PriceID == item.PriceID && old.Model == item.Model && old.Picture == item.Picture {
			continue
		}
		if item.VariantID != "" || item.PriceID != "" {
			v, price, err := h.mailDB.ResolveProductPrice(ctx, owner, item.ProductID, item.VariantID, item.PriceID, item.ProductVersion, item.Qty)
			if err != nil {
				return documentProductLookupError(index, *item, err)
			}
			if item.Price != "" && !sameCatalogNumber(item.Price, price.Amount) {
				return fmt.Errorf("catalog price mismatch; use the retrieved price unchanged")
			}
			item.Price = price.Amount
			item.Amount = catalogAmount(item.Qty, price.Amount)
			if v.SKU != "" {
				item.Model = v.SKU
			}
			p, err := h.mailDB.GetProduct(ctx, owner, item.ProductID)
			if err != nil {
				return documentProductLookupError(index, *item, err)
			}
			specs := []string{p.Name}
			for _, d := range p.Catalog.Dimensions {
				specs = append(specs, d.Label+": "+v.Attributes[d.Key]+d.Unit)
			}
			item.Description = strings.Join(specs, " / ")
		}

		if item.ProductID != "" {
			p, err := h.mailDB.GetProduct(ctx, owner, item.ProductID)
			if err != nil {
				return documentProductLookupError(index, *item, err)
			}
			if item.ProductVersion == 0 || item.ProductVersion != p.Version {
				return fmt.Errorf("product %s: %w", p.Model, mailstore.ErrProductConflict)
			}
			item.Picture = p.ImageURL
			if item.Model == "" {
				item.Model = p.Model
			}
		} else if item.Picture != "" {
			const prefix = "/api/product-images/"
			if !strings.HasPrefix(item.Picture, prefix) {
				item.Picture = ""
				continue
			}
			if _, _, err := h.mailDB.GetProductImage(ctx, owner, strings.TrimPrefix(item.Picture, prefix)); err != nil {
				return documentProductLookupError(index, *item, err)
			}
		}
	}
	return nil
}

func productVersionArg(args map[string]any) int64 {
	n, _ := productToolInt(args, "productVersion", 0, 0, 1<<31-1)
	return int64(n)
}
