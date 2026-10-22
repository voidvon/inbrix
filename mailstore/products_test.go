package mailstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
)

func productFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "products.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := s.CreateUser(context.Background(), "products-alice", "Alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateUser(context.Background(), "products-bob", "Bob", "hash")
	if err != nil {
		t.Fatal(err)
	}
	return s, a.ID, b.ID
}

func TestProductKnowledgeSearchAndUpdates(t *testing.T) {
	s, owner, other := productFixture(t)
	ctx := context.Background()
	cat, err := s.SaveProductCategory(ctx, owner, "", "控制阀")
	if err != nil {
		t.Fatal(err)
	}
	in := ProductInput{Name: "控制阀", Brand: "示例品牌", Model: "KE-43", CategoryID: cat.ID, Specification: "# 产品说明\n适用于蒸汽和其他介质。\n\n# 压力温度限制\n允许压力应结合工作温度核对。\n\n# 执行器选配\n支持电动执行器，须核对关断压差。"}
	p, err := s.SaveProduct(ctx, owner, "", in)
	if err != nil {
		t.Fatal(err)
	}
	if renamed, err := s.SaveProductCategory(ctx, owner, cat.ID, "调节阀"); err != nil || renamed.ProductCount != 1 {
		t.Fatalf("category count after rename: %+v %v", renamed, err)
	}
	if _, err = s.SaveProduct(ctx, other, "", ProductInput{Name: "Private", Model: "KE43", Specification: "电动执行器"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"ke43", "KE-43", "电动", "执行器", "压力温度", "蒸汽", "KE43 电动"} {
		page, err := s.SearchProducts(ctx, owner, q, "", 10, 0)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if page.Total != 1 || len(page.Products) != 1 || page.Products[0].ID != p.ID || page.Products[0].Specification != "" {
			t.Fatalf("search %q: %+v", q, page)
		}
	}
	for _, q := range []string{`" OR *`, "无匹配词", "KE63"} {
		page, err := s.SearchProducts(ctx, owner, q, "", 10, 0)
		if err != nil || page.Total != 0 {
			t.Fatalf("literal search %q: %+v %v", q, page, err)
		}
	}
	page, err := s.SearchProducts(ctx, owner, "", "wrong-category", 10, 0)
	if err != nil || page.Total != 0 {
		t.Fatalf("category filter: %+v %v", page, err)
	}
	read, err := s.ReadProduct(ctx, owner, p.ID, p.Version, 0, 12)
	if err != nil {
		t.Fatal(err)
	}
	if !read.HasMore || read.NextOffset != 12 || len([]rune(read.Content)) != 12 || len(read.Sections) != 3 {
		t.Fatalf("read: %+v", read)
	}
	var all strings.Builder
	all.WriteString(read.Content)
	for read.HasMore {
		read, err = s.ReadProduct(ctx, owner, p.ID, p.Version, read.NextOffset, 12)
		if err != nil {
			t.Fatal(err)
		}
		all.WriteString(read.Content)
	}
	if all.String() != in.Specification {
		t.Fatal("segmented read changed unicode text")
	}
	in.Specification = "# 技术规格\n仅支持气动执行器。"
	in.Version = p.Version
	updated, err := s.SaveProduct(ctx, owner, p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatal("version did not advance")
	}
	if _, err = s.ReadProduct(ctx, owner, p.ID, p.Version, 0, 100); !errors.Is(err, ErrProductConflict) {
		t.Fatalf("stale read accepted: %v", err)
	}
	if _, err = s.SaveProduct(ctx, owner, p.ID, in); !errors.Is(err, ErrProductConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	page, err = s.SearchProducts(ctx, owner, "电动", "", 10, 0)
	if err != nil || page.Total != 0 {
		t.Fatalf("stale FTS content: %+v %v", page, err)
	}
	page, err = s.SearchProducts(ctx, owner, "气动", "", 10, 0)
	if err != nil || page.Total != 1 {
		t.Fatalf("new FTS content missing: %+v %v", page, err)
	}
	if _, err = s.GetProduct(ctx, other, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign product exposed: %v", err)
	}
	if _, err = s.SaveProduct(ctx, other, p.ID, ProductInput{Name: "overwrite", Version: 2}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign update accepted: %v", err)
	}
	if _, err = s.SaveProduct(ctx, other, "", ProductInput{Name: "bad category", CategoryID: cat.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign category accepted: %v", err)
	}
	if err = s.DeleteProduct(ctx, other, p.ID, 2); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign deletion accepted")
	}
	if err = s.DeleteProductCategory(ctx, owner, cat.ID); err != nil {
		t.Fatal(err)
	}
	p, err = s.GetProduct(ctx, owner, p.ID)
	if err != nil || p.CategoryID != "" || p.Version != 3 {
		t.Fatalf("category deletion: %+v %v", p, err)
	}
	if err = s.DeleteProduct(ctx, owner, p.ID, 2); !errors.Is(err, ErrProductConflict) {
		t.Fatal("stale delete accepted")
	}
	if err = s.DeleteProduct(ctx, owner, p.ID, 3); err != nil {
		t.Fatal(err)
	}
	page, err = s.SearchProducts(ctx, owner, "气动", "", 10, 0)
	if err != nil || page.Total != 0 {
		t.Fatalf("deleted FTS content: %+v %v", page, err)
	}
}

func TestProductPaginationAndLargeSpecifications(t *testing.T) {
	s, owner, _ := productFixture(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "候选产品", Specification: strings.Repeat("重复说明。\n", 500) + "# 技术规格\n材料：不锈钢"})
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for offset := 0; ; {
		page, err := s.SearchProducts(ctx, owner, "不锈钢", "", 2, offset)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 || len(page.Products) != 2 {
			t.Fatalf("pagination: %+v", page)
		}
		for _, p := range page.Products {
			if seen[p.ID] {
				t.Fatal("duplicate product across pages")
			}
			seen[p.ID] = true
			if len([]rune(p.Snippet)) > 400 {
				t.Fatal("unbounded snippet")
			}
		}
		if !page.HasMore {
			break
		}
		if page.NextOffset <= offset {
			t.Fatal("pagination did not advance")
		}
		offset = page.NextOffset
	}
	if len(seen) != 4 {
		t.Fatal("missing products")
	}
}

func TestProductImagesAreOwnedImmutableSnapshots(t *testing.T) {
	s, owner, other := productFixture(t)
	ctx := context.Background()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	p, err := s.SaveProduct(ctx, owner, "", ProductInput{Name: "有图产品", ImageDataURL: &url})
	if err != nil {
		t.Fatal(err)
	}
	data, mime, err := s.GetProductImage(ctx, owner, p.ImageID)
	if err != nil || mime != "image/png" || !bytes.Equal(data, buf.Bytes()) {
		t.Fatalf("image mismatch: %s %v", mime, err)
	}
	if _, _, err = s.GetProductImage(ctx, other, p.ImageID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign image exposed")
	}
	empty := ""
	updated, err := s.SaveProduct(ctx, owner, p.ID, ProductInput{Name: p.Name, Version: p.Version, ImageDataURL: &empty})
	if err != nil || updated.ImageID != "" {
		t.Fatalf("remove image: %+v %v", updated, err)
	}
	if err = s.DeleteProduct(ctx, owner, p.ID, updated.Version); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.GetProductImage(ctx, owner, p.ImageID); err != nil {
		t.Fatalf("existing document image reference broken: %v", err)
	}
	for _, bad := range []string{"https://example.com/image.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,YWJj", strings.Replace(url, "image/png", "image/jpeg", 1)} {
		if _, err = s.SaveProduct(ctx, owner, "", ProductInput{Name: "bad", ImageDataURL: &bad}); !errors.Is(err, ErrProductInvalid) {
			t.Fatalf("invalid image accepted: %v", err)
		}
	}
}
