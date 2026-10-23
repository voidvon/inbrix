package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestProductPriceDefaultsToCNYWithoutPricingUnit(t *testing.T) {
	s, owner, _ := productFixture(t)
	ctx := context.Background()
	var catalog ProductCatalog
	// An old pricing unit is accepted as an ignored legacy property.
	if err := json.Unmarshal([]byte(`{"variants":[{"attributes":{},"prices":[{"amount":"88.50","unit":"台"}]}]}`), &catalog); err != nil {
		t.Fatal(err)
	}
	p, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "人民币产品", Catalog: &catalog})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.QueryProductPrices(ctx, owner, ProductPriceQuery{ProductID: p.ID})
	if err != nil || r.Status != "matched" || r.Matches[0].Prices[0].Currency != ProductPriceCurrency {
		t.Fatal(r, err)
	}
	encoded, _ := json.Marshal(r.Matches[0].Prices[0])
	var fields map[string]any
	json.Unmarshal(encoded, &fields)
	if _, ok := fields["unit"]; ok {
		t.Fatal("obsolete pricing unit returned", string(encoded))
	}
	// A pre-existing foreign price must never be quoted as renminbi.
	_, err = s.db.ExecContext(ctx, `UPDATE product_variant_prices SET price_json=? WHERE id=?`, `{"amount":"88.50","currency":"USD"}`, r.Matches[0].Prices[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.QueryProductPrices(ctx, owner, ProductPriceQuery{ProductID: p.ID})
	if err != nil || r.Status != "no_price" {
		t.Fatal("foreign price treated as CNY", r, err)
	}
}

func catalogFixture() *ProductCatalog {
	return &ProductCatalog{Dimensions: []ProductDimension{{Key: "connection", Label: "连接方式", Type: "select", Options: []string{"法兰", "螺纹"}}, {Key: "size", Label: "口径", Type: "number", Unit: "mm"}}, Variants: []ProductVariant{
		{SKU: "BV-F50", Attributes: map[string]string{"connection": "法兰", "size": "50"}, Prices: []ProductPrice{{Amount: "1280.00", Currency: "CNY", TaxRate: "0.13", MinQty: "1", MaxQty: "9", Source: "标准价格"}, {Amount: "1200.00", Currency: "CNY", TaxRate: "0.13", MinQty: "10", Source: "标准价格"}}},
		{SKU: "BV-T25", Attributes: map[string]string{"connection": "螺纹", "size": "25"}, Prices: []ProductPrice{}},
	}}
}
func TestProductCatalogQueryAndIntegrity(t *testing.T) {
	s, owner, other := productFixture(t)
	ctx := context.Background()
	p, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "球阀", Model: "BV", Catalog: catalogFixture()})
	if err != nil {
		t.Fatal(err)
	}
	if p.Catalog.Variants[0].ID == "" || p.Catalog.Variants[0].Prices[0].ID == "" {
		t.Fatal("identities missing")
	}
	check := func(q ProductPriceQuery, status string, total int) ProductPriceResult {
		t.Helper()
		q.ProductID = p.ID
		r, err := s.QueryProductPrices(ctx, owner, q)
		if err != nil || r.Status != status || r.Total != total {
			t.Fatalf("query %+v: %+v %v", q, r, err)
		}
		return r
	}
	r := check(ProductPriceQuery{Limit: 1}, "needs_clarification", 2)
	if !r.HasMore || r.NextOffset != 1 || len(r.Missing) != 4 {
		t.Fatalf("pagination hid ambiguity: %+v", r)
	}
	attrs := map[string]string{"connection": "法兰", "size": "50"}
	check(ProductPriceQuery{VariantID: p.Catalog.Variants[0].ID, PriceID: p.Catalog.Variants[0].Prices[1].ID, Qty: "10"}, "matched", 1)
	check(ProductPriceQuery{Attributes: map[string]string{"connection": "法兰", "size": "50.00"}, Qty: "10"}, "matched", 1)

	r = check(ProductPriceQuery{Attributes: attrs, Qty: "10"}, "matched", 1)
	if len(r.Matches[0].Prices) != 1 || r.Matches[0].Prices[0].Amount != "1200.00" {
		t.Fatal(r)
	}
	check(ProductPriceQuery{Attributes: map[string]string{"connection": "螺纹", "size": "25"}}, "no_price", 1)
	check(ProductPriceQuery{Attributes: attrs, Qty: "10", Currency: "USD"}, "no_price", 1)
	check(ProductPriceQuery{Attributes: map[string]string{"connection": "法兰", "size": "25"}}, "no_match", 0)
	if _, err = s.QueryProductPrices(ctx, other, ProductPriceQuery{ProductID: p.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign catalog exposed", err)
	}
	if _, err = s.QueryProductPrices(ctx, owner, ProductPriceQuery{ProductID: p.ID, Attributes: map[string]string{"typo": "50"}}); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("unknown filter accepted", err)
	}
	page, err := s.SearchProducts(ctx, owner, "法兰", "", 20, 0)
	if err != nil || page.Total != 1 {
		t.Fatal("structured attributes not searchable", page, err)
	}
	v, price, err := s.ResolveProductPrice(ctx, owner, p.ID, p.Catalog.Variants[0].ID, p.Catalog.Variants[0].Prices[1].ID, p.Version, "10")
	if err != nil || v.SKU != "BV-F50" || price.Amount != "1200.00" {
		t.Fatal(v, price, err)
	}
	if _, _, err = s.ResolveProductPrice(ctx, owner, p.ID, v.ID, p.Catalog.Variants[0].Prices[0].ID, p.Version, "10"); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("inapplicable quantity accepted", err)
	}
	// An invalid edit rolls back both narrative and catalog.
	p.Catalog.Variants[0].Prices[0].Amount = "not a price"
	_, err = s.SaveProduct(ctx, owner, p.ID, ProductInput{Name: "changed", Version: p.Version, Catalog: p.Catalog})
	if !errors.Is(err, ErrProductInvalid) {
		t.Fatal(err)
	}
	current, _ := s.GetProduct(ctx, owner, p.ID)
	if current.Name != "球阀" || current.Version != p.Version || current.Catalog.Variants[0].Prices[0].Amount != "1280.00" {
		t.Fatal("partial update", current)
	}
	// Old clients omitting catalog preserve configurations and search indexing.
	next, err := s.SaveProduct(ctx, owner, p.ID, ProductInput{Name: p.Name, Version: p.Version})
	if err != nil || len(next.Catalog.Variants) != 2 || next.Catalog.Variants[0].ID != v.ID {
		t.Fatal(next, err)
	}
	if _, _, err = s.ResolveProductPrice(ctx, owner, p.ID, v.ID, price.ID, p.Version, "10"); !errors.Is(err, ErrProductConflict) {
		t.Fatal("stale price accepted", err)
	}
	if err = s.DeleteProduct(ctx, owner, p.ID, next.Version); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM product_variant_prices`).Scan(&n)
	if n != 0 {
		t.Fatal("orphaned prices", n)
	}
}
func TestProductCatalogPriceValidityAndValidation(t *testing.T) {
	s, owner, _ := productFixture(t)
	ctx := context.Background()
	c := EmptyProductCatalog()
	c.Variants = []ProductVariant{{Attributes: map[string]string{}, Prices: []ProductPrice{{Amount: "0", Currency: "CNY", ValidFrom: "2026-01-01", ValidUntil: "2026-12-31"}}}}
	p, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "Simple", Catalog: c})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ date, status string }{{"2025-12-31", "no_price"}, {"2026-01-01", "matched"}, {"2026-12-31", "matched"}, {"2027-01-01", "no_price"}} {
		r, err := s.QueryProductPrices(ctx, owner, ProductPriceQuery{ProductID: p.ID, Date: tt.date})
		if err != nil || r.Status != tt.status {
			t.Fatal(tt, r, err)
		}
	}
	for _, mutate := range []func(*ProductCatalog){
		func(c *ProductCatalog) { c.Variants[0].Prices[0].Amount = "" },
		func(c *ProductCatalog) { c.Variants[0].Prices[0].Amount = "-1" },
		func(c *ProductCatalog) { c.Variants[0].Prices[0].Currency = "USD" },
		func(c *ProductCatalog) { c.Variants[0].Prices[0].TaxRate = "13" },
		func(c *ProductCatalog) { c.Variants = append(c.Variants, c.Variants[0]) },
		func(c *ProductCatalog) { c.Variants[0].ID = "foreign-id" },
	} {
		original, _ := s.GetProduct(ctx, owner, p.ID)
		mutate(original.Catalog)
		_, err = s.SaveProduct(ctx, owner, p.ID, ProductInput{Name: p.Name, Version: p.Version, Catalog: original.Catalog})
		if !errors.Is(err, ErrProductInvalid) {
			t.Fatalf("invalid edit accepted: %v", err)
		}
	}
}
func TestProductLegacyPricesStayPendingUntilLinked(t *testing.T) {
	s, owner, other := productFixture(t)
	ctx := context.Background()
	list, _, err := s.CreatePriceList(ctx, owner, "2026 价格", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SavePriceRows(ctx, owner, list.ID, []PriceRow{{Data: map[string]string{"name": "球阀", "model": "BV", "price": "1200", "currency": "CNY", "unit": "台", "tax": "13%"}}}, nil); err != nil {
		t.Fatal(err)
	}
	pending, total, err := s.PendingProductPrices(ctx, owner, 0, 30)
	if err != nil || total != 1 {
		t.Fatal(pending, total, err)
	}
	if _, n, err := s.PendingProductPrices(ctx, other, 0, 30); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err = s.ImportProductPrice(ctx, other, pending[0].RowID, "", 0, nil); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign import accepted", err)
	}
	r, err := s.QueryProductPrices(ctx, owner, ProductPriceQuery{Query: "BV"})
	if err != nil || r.Status != "no_match" {
		t.Fatal("pending row entered pricing", r, err)
	}
	p, err := s.ImportProductPrice(ctx, owner, pending[0].RowID, "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Catalog.Variants[0].Prices[0].TaxRate != "0.13" || p.Catalog.Variants[0].Prices[0].Source != list.Name {
		t.Fatal(p)
	}
	if _, n, err := s.PendingProductPrices(ctx, owner, 0, 30); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err = s.ImportProductPrice(ctx, owner, pending[0].RowID, "", 0, nil); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("duplicate import accepted", err)
	}
	page, err := s.SearchProducts(ctx, owner, "BV", "", 10, 0)
	if err != nil || page.Total != 1 {
		t.Fatal("retry created duplicate product", page, err)
	}
	rows, _, err := s.ListPriceRows(ctx, owner, list.ID, "", "", "", 30, 0)
	if err != nil || len(rows) != 1 {
		t.Fatal("original source lost", rows, err)
	}
}
