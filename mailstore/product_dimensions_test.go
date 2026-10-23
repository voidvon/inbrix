package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSharedProductDimensionsAndCanonicalReferences(t *testing.T) {
	s, owner, other := productFixture(t)
	ctx := context.Background()
	d, err := s.SaveProductDimension(ctx, owner, "", ProductDimensionRecord{ProductDimension: ProductDimension{Label: "连接方式", Type: "select", Options: []string{"法兰", "螺纹"}}})
	if err != nil {
		t.Fatal(err)
	}
	makeProduct := func(name string) Product {
		t.Helper()
		p, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: name, Catalog: &ProductCatalog{Dimensions: []ProductDimension{{Key: d.Key}}, Variants: []ProductVariant{{Attributes: map[string]string{d.Key: "法兰"}, Prices: []ProductPrice{{Amount: "10", Currency: "CNY"}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b := makeProduct("A"), makeProduct("B")
	if a.Catalog.Dimensions[0].Label != "连接方式" || a.Catalog.Dimensions[0].Key != b.Catalog.Dimensions[0].Key {
		t.Fatal(a, b)
	}
	list, err := s.ListProductDimensions(ctx, owner)
	if err != nil || len(list) != 1 || list[0].ProductCount != 2 {
		t.Fatal(list, err)
	}
	foreign, err := s.ListProductDimensions(ctx, other)
	if err != nil || len(foreign) != 0 {
		t.Fatal(foreign, err)
	}
	if _, err = s.SaveProductDimension(ctx, other, d.Key, d); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign update", err)
	}
	if err = s.DeleteProductDimension(ctx, other, d.Key, d.Version); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign delete", err)
	}
	if err = s.DeleteProductDimension(ctx, owner, d.Key, d.Version); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("used definition deleted", err)
	}
	invalid := d
	invalid.Unit = "mm"
	if _, err = s.SaveProductDimension(ctx, owner, d.Key, invalid); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("unit change accepted", err)
	}
	invalid = d
	invalid.Type = "text"
	if _, err = s.SaveProductDimension(ctx, owner, d.Key, invalid); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("type change accepted", err)
	}
	invalid = d
	invalid.Options = []string{"螺纹"}
	if _, err = s.SaveProductDimension(ctx, owner, d.Key, invalid); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("used option removed", err)
	}
	// Renames and additive options propagate, update search, and invalidate stale AI evidence.
	updated := d
	updated.Label = "接口形式"
	updated.Options = append(updated.Options, "焊接")
	updated, err = s.SaveProductDimension(ctx, owner, d.Key, updated)
	if err != nil || updated.Version != 2 || updated.ProductCount != 2 {
		t.Fatal(updated, err)
	}
	current, err := s.GetProduct(ctx, owner, a.ID)
	if err != nil || current.Version != a.Version+1 || current.Catalog.Dimensions[0].Label != "接口形式" || len(current.Catalog.Dimensions[0].Options) != 3 {
		t.Fatal(current, err)
	}
	page, err := s.SearchProducts(ctx, owner, "接口形式", "", 20, 0)
	if err != nil || page.Total != 2 {
		t.Fatal("search metadata not refreshed", page, err)
	}
	if _, err = s.QueryProductPrices(ctx, owner, ProductPriceQuery{ProductID: a.ID, Version: a.Version}); !errors.Is(err, ErrProductConflict) {
		t.Fatal("stale evidence accepted", err)
	}
	if _, err = s.SaveProductDimension(ctx, owner, d.Key, d); !errors.Is(err, ErrProductConflict) {
		t.Fatal("stale schema save accepted", err)
	}
	// Product editors cannot override shared definitions through copied label/unit fields.
	current.Catalog.Dimensions[0].Label = "local override"
	current.Catalog.Dimensions[0].Unit = "wrong"
	saved, err := s.SaveProduct(ctx, owner, current.ID, ProductInput{Name: current.Name, Version: current.Version, Catalog: current.Catalog})
	if err != nil || saved.Catalog.Dimensions[0].Label != "接口形式" || saved.Catalog.Dimensions[0].Unit != "" {
		t.Fatal(saved, err)
	}
}
func TestSharedDimensionDeduplicationAndDeletion(t *testing.T) {
	s, owner, _ := productFixture(t)
	ctx := context.Background()
	a, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "A", Catalog: &ProductCatalog{Dimensions: []ProductDimension{{Key: "old_a", Label: "材质", Type: "text"}}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "B", Catalog: &ProductCatalog{Dimensions: []ProductDimension{{Key: "old_b", Label: "材质", Type: "text"}}, Variants: []ProductVariant{{Attributes: map[string]string{"old_b": "304"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Catalog.Dimensions[0].Key != b.Catalog.Dimensions[0].Key || b.Catalog.Variants[0].Attributes["old_a"] != "304" {
		t.Fatal("definitions not shared", a, b)
	}
	list, _ := s.ListProductDimensions(ctx, owner)
	if len(list) != 1 {
		t.Fatal(list)
	}
	if _, err = s.SaveProductDimension(ctx, owner, "", ProductDimensionRecord{ProductDimension: ProductDimension{Label: "材质", Type: "text"}}); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("duplicate shared definition", err)
	}
	unused, err := s.SaveProductDimension(ctx, owner, "", ProductDimensionRecord{ProductDimension: ProductDimension{Label: "功率", Type: "number", Unit: "kW"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteProductDimension(ctx, owner, unused.Key, unused.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveProduct(ctx, owner, "", ProductInput{Name: "stale draft", Catalog: &ProductCatalog{Dimensions: []ProductDimension{unused.ProductDimension}}}); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("deleted definition resurrected", err)
	}
	// Unit case remains significant (MPa and mPa are different units).
	for _, unit := range []string{"MPa", "mPa"} {
		if _, err = s.SaveProductDimension(ctx, owner, "", ProductDimensionRecord{ProductDimension: ProductDimension{Label: "压力", Type: "number", Unit: unit}}); err != nil {
			t.Fatal(unit, err)
		}
	}
}
func TestLegacyDimensionsMigrateWithoutLosingValues(t *testing.T) {
	s, owner, _ := productFixture(t)
	ctx := context.Background()
	a, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "legacy A", Catalog: EmptyProductCatalog()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "legacy B", Catalog: EmptyProductCatalog()})
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce a database written before the shared dictionary existed.
	for i, p := range []Product{a, b} {
		key := []string{"local_a", "local_b"}[i]
		option := []string{"法兰", "螺纹"}[i]
		d, _ := json.Marshal([]ProductDimension{{Key: key, Label: "连接方式", Type: "select", Options: []string{option}}})
		if _, err = s.db.Exec(`UPDATE product_catalogs SET dimensions_json=? WHERE product_id=?`, string(d), p.ID); err != nil {
			t.Fatal(err)
		}
		attrs, _ := json.Marshal(map[string]string{key: option})
		if _, err = s.db.Exec(`INSERT INTO product_variants(id,product_id,position,sku,attributes_json) VALUES(?,?,0,'',?)`, key, p.ID, string(attrs)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.db.Exec(`DELETE FROM product_catalog_migrations WHERE name='global_dimensions_v1'`); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateGlobalProductDimensions(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListProductDimensions(ctx, owner)
	if err != nil || len(list) != 1 || list[0].ProductCount != 2 || len(list[0].Options) != 2 {
		t.Fatal(list, err)
	}
	for i, p := range []Product{a, b} {
		current, err := s.GetProduct(ctx, owner, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		key := list[0].Key
		if current.Catalog.Dimensions[0].Key != key || current.Catalog.Variants[0].Attributes[key] != []string{"法兰", "螺纹"}[i] || len(current.Catalog.Dimensions[0].Options) != 2 {
			t.Fatal(current)
		}
	}
	if err = s.migrateGlobalProductDimensions(ctx); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	current, _ := s.GetProduct(ctx, owner, a.ID)
	if current.Version != a.Version+1 {
		t.Fatal("migration repeated", current.Version)
	}
}

func TestSharedDimensionRenameCannotInvalidateProductLabels(t *testing.T) {
	s, owner, _ := productFixture(t)
	ctx := context.Background()
	a, err := s.SaveProductDimension(ctx, owner, "", ProductDimensionRecord{ProductDimension: ProductDimension{Label: "口径", Type: "number", Unit: "mm"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveProductDimension(ctx, owner, "", ProductDimensionRecord{ProductDimension: ProductDimension{Label: "连接方式", Type: "text"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "Shared", Catalog: &ProductCatalog{Dimensions: []ProductDimension{{Key: a.Key}, {Key: b.Key}}}})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged, err := s.SaveProductDimension(ctx, owner, b.Key, b); err != nil || unchanged.Version != b.Version {
		t.Fatal("no-op edit advanced schema", unchanged, err)
	}
	current, _ := s.GetProduct(ctx, owner, p.ID)
	if current.Version != p.Version {
		t.Fatal("no-op edit invalidated product")
	}
	b.Label = "口径"
	if _, err = s.SaveProductDimension(ctx, owner, b.Key, b); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("duplicate product dimension label accepted", err)
	}
	current, _ = s.GetProduct(ctx, owner, p.ID)
	if current.Catalog.Dimensions[1].Label != "连接方式" || current.Version != p.Version {
		t.Fatal("failed global change partially committed", current)
	}
}
