package mailstore

import (
	"context"
	"errors"
	"testing"
)

func TestProductCategoryTreeAndBrandNavigation(t *testing.T) {
	s, owner, other := productFixture(t)
	ctx := context.Background()
	root, err := s.SaveProductCategory(ctx, owner, "", "疏水阀")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.SaveProductCategory(ctx, owner, "", "浮球式疏水阀", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := s.SaveProductCategory(ctx, owner, "", "立式浮球", child.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.SaveProductCategory(ctx, other, "", "私有分类")
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{root.ID, child.ID, leaf.ID} {
		if _, err := s.SaveProductCategory(ctx, owner, root.ID, root.Name, parent); !errors.Is(err, ErrProductInvalid) {
			t.Fatalf("cycle accepted under %s: %v", parent, err)
		}
	}
	if _, err := s.SaveProductCategory(ctx, owner, "", "跨用户", foreign.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign parent accepted", err)
	}
	if _, err := s.SaveProductCategory(ctx, other, child.ID, "越权"); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign category changed", err)
	}
	if _, err := s.SaveProductCategory(ctx, owner, child.ID, "浮球式"); err != nil {
		t.Fatal(err)
	}
	categories, err := s.ListProductCategories(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range categories {
		if category.ID == child.ID && category.ParentID != root.ID {
			t.Fatal("legacy rename detached the parent")
		}
	}
	brandA, brandB, blank := "斯派莎克", "另一品牌", ""
	for _, input := range []ProductInput{
		{Name: "浮球阀甲", Brand: brandA, CategoryID: child.ID},
		{Name: "浮球阀乙", Brand: brandA, CategoryID: leaf.ID},
		{Name: "浮球阀丙", Brand: brandB, CategoryID: child.ID},
		{Name: "浮球阀丁", Brand: brandA, CategoryID: root.ID},
		{Name: "浮球阀未分类", Brand: brandA},
		{Name: "浮球阀无品牌", CategoryID: child.ID},
	} {
		if _, err := s.SaveProduct(ctx, owner, "", input); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SaveProduct(ctx, other, "", ProductInput{Name: "私有", Brand: brandA, CategoryID: foreign.ID}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		category, query string
		filter          ProductSearchFilter
		total           int
	}{
		{root.ID, "", ProductSearchFilter{}, 5},
		{root.ID, "浮球", ProductSearchFilter{Brand: &brandA}, 3},
		{child.ID, "", ProductSearchFilter{Brand: &brandA}, 2},
		{root.ID, "", ProductSearchFilter{Brand: &brandB}, 1},
		{root.ID, "", ProductSearchFilter{Brand: &blank}, 1},
		{"", "", ProductSearchFilter{Brand: &brandA, Uncategorized: true}, 1},
		{foreign.ID, "", ProductSearchFilter{}, 0},
	} {
		page, err := s.SearchProducts(ctx, owner, tc.query, tc.category, 1, 0, tc.filter)
		if err != nil || page.Total != tc.total || len(page.Products) != min(1, tc.total) || page.HasMore != (tc.total > 1) {
			t.Fatalf("filter %+v: %+v %v", tc, page, err)
		}
	}
	nav, err := s.ProductNavigation(ctx, owner)
	if err != nil || nav.Total != 6 || len(nav.Brands) != 3 {
		t.Fatalf("navigation: %+v %v", nav, err)
	}
	for _, brand := range nav.Brands {
		if brand.Name == brandA && (brand.ProductCount != 4 || len(brand.Categories) != 4) {
			t.Fatalf("bad brand counts: %+v", brand)
		}
	}
	if err := s.DeleteProductCategory(ctx, owner, root.ID); !errors.Is(err, ErrProductInvalid) {
		t.Fatal("deleted category with children", err)
	}
	if _, err := s.SaveProductCategory(ctx, owner, child.ID, "浮球式", ""); err != nil {
		t.Fatal(err)
	}
	page, err := s.SearchProducts(ctx, owner, "", root.ID, 20, 0)
	if err != nil || page.Total != 1 {
		t.Fatal("moving a subtree did not change parent results", page, err)
	}
	if err := s.DeleteProductCategory(ctx, owner, root.ID); err != nil {
		t.Fatal(err)
	}
	nav, err = s.ProductNavigation(ctx, owner)
	if err != nil || nav.Total != 6 {
		t.Fatal("category deletion removed products", nav, err)
	}
}

func TestProductCategoryTreeMigrationPreservesExistingCategories(t *testing.T) {
	s, owner, _ := productFixture(t)
	ctx := context.Background()
	category, err := s.SaveProductCategory(ctx, owner, "", "已有分类")
	if err != nil {
		t.Fatal(err)
	}
	product, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "已有产品", CategoryID: category.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `DROP INDEX idx_product_categories_parent`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `ALTER TABLE product_categories DROP COLUMN parent_id`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = s.migrateProductCategoryTree(ctx); err != nil {
			t.Fatal(err)
		}
	}
	categories, err := s.ListProductCategories(ctx, owner)
	if err != nil || len(categories) != 1 || categories[0].ID != category.ID || categories[0].ParentID != "" || categories[0].ProductCount != 1 {
		t.Fatal(categories, err)
	}
	stored, err := s.GetProduct(ctx, owner, product.ID)
	if err != nil || stored.CategoryID != category.ID || stored.Version != product.Version {
		t.Fatal("migration changed product references", stored, err)
	}
}
