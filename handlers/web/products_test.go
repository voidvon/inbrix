package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestProductNavigationAPI(t *testing.T) {
	f := newAPIKeyFixture(t)
	ctx := context.Background()
	root, err := f.db.SaveProductCategory(ctx, f.user.ID, "", "疏水阀")
	if err != nil {
		t.Fatal(err)
	}
	res, body := keyRequest(t, f, "POST", "/api/product-categories", f.key.Key, fmt.Sprintf(`{"name":"浮球式","parentId":%q}`, root.ID), nil)
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, body)
	}
	var created struct {
		Category mailstore.ProductCategory `json:"category"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	child := created.Category
	if child.ParentID != root.ID {
		t.Fatal("parent not persisted", child)
	}
	for _, brand := range []string{"斯派莎克", "", "其他"} {
		if _, err := f.db.SaveProduct(ctx, f.user.ID, "", mailstore.ProductInput{Name: "浮球式疏水阀", Brand: brand, CategoryID: child.ID}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		key, path, contains string
		status              int
	}{
		{"", "/api/product-navigation", "", 401},
		{f.key.Key, "/api/product-navigation", `"total":3`, 200},
		{f.otherKey.Key, "/api/product-navigation", `"total":0`, 200},
		{f.key.Key, "/api/products?categoryId=" + root.ID, `"total":3`, 200},
		{f.key.Key, "/api/products?categoryId=" + root.ID + "&brand=" + url.QueryEscape("斯派莎克"), `"total":1`, 200},
		{f.key.Key, "/api/products?categoryId=" + root.ID + "&brand=", `"total":1`, 200},
		{f.key.Key, "/api/products?uncategorized=true", `"total":0`, 200},
		{f.otherKey.Key, "/api/products?categoryId=" + root.ID, `"total":0`, 200},
	} {
		res, body := keyRequest(t, f, "GET", tc.path, tc.key, "", nil)
		if res.StatusCode != tc.status || !strings.Contains(body, tc.contains) {
			t.Fatalf("%+v: %d %s", tc, res.StatusCode, body)
		}
		if tc.status == 200 && res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("navigation must not be cached")
		}
	}
	res, _ = keyRequest(t, f, "PUT", "/api/product-categories/"+root.ID, f.key.Key, fmt.Sprintf(`{"name":"疏水阀","parentId":%q}`, child.ID), nil)
	if res.StatusCode != 400 {
		t.Fatal("cycle accepted", res.StatusCode)
	}
	res, _ = keyRequest(t, f, "DELETE", "/api/product-categories/"+root.ID, f.key.Key, "", nil)
	if res.StatusCode != 400 {
		t.Fatal("parent deletion accepted", res.StatusCode)
	}
}

func TestProductLibraryAPIKeyPermissionsAndDiscovery(t *testing.T) {
	f := newAPIKeyFixture(t)
	res, _ := keyRequest(t, f, "GET", "/api/products", "", "", nil)
	if res.StatusCode != 401 {
		t.Fatal("product library exposed without auth")
	}
	res, body := keyRequest(t, f, "POST", "/api/product-categories", f.key.Key, `{"name":"测试分类"}`, nil)
	if res.StatusCode != 200 {
		t.Fatalf("create category: %d %s", res.StatusCode, body)
	}
	var category struct {
		Category mailstore.ProductCategory `json:"category"`
	}
	if err := json.Unmarshal([]byte(body), &category); err != nil {
		t.Fatal(err)
	}
	res, body = keyRequest(t, f, "POST", "/api/products", f.key.Key, fmt.Sprintf(`{"name":"测试产品","model":"TEST-42","categoryId":%q,"specification":"## 技术规格\n支持电动执行器。"}`, category.Category.ID), nil)
	if res.StatusCode != 201 {
		t.Fatalf("create product: %d %s", res.StatusCode, body)
	}
	var created struct {
		Product mailstore.Product `json:"product"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	id := created.Product.ID
	for _, path := range []string{"/api/products?q=test42", "/api/products/search?q=电动", "/api/products/" + id, "/api/products/" + id + "/read?version=1&length=6"} {
		res, body = keyRequest(t, f, "GET", path, f.key.Key, "", nil)
		if res.StatusCode != 200 || !strings.Contains(body, id) || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("read product: %s %d %s", path, res.StatusCode, body)
		}
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		res, body = keyRequest(t, f, method, "/api/products/"+id+"?version=1", f.otherKey.Key, `{"name":"overwrite","version":1}`, nil)
		if res.StatusCode != 404 {
			t.Fatalf("foreign %s: %d %s", method, res.StatusCode, body)
		}
	}
	res, body = keyRequest(t, f, "GET", "/api/products?q=test42", f.otherKey.Key, "", nil)
	if res.StatusCode != 200 || !strings.Contains(body, `"total":0`) {
		t.Fatalf("foreign search: %d %s", res.StatusCode, body)
	}
	for _, path := range []string{"/api/products?limit=200", "/api/products?offset=-1", "/api/products/" + id + "/read?length=4001", "/api/products/" + id + "/read?offset=9999"} {
		res, _ = keyRequest(t, f, "GET", path, f.key.Key, "", nil)
		if res.StatusCode != 400 {
			t.Fatalf("invalid pagination accepted: %s %d", path, res.StatusCode)
		}
	}
	res, _ = keyRequest(t, f, "PUT", "/api/products/"+id, f.key.Key, `{"name":"stale","version":0}`, nil)
	if res.StatusCode != 409 {
		t.Fatalf("stale write accepted: %d", res.StatusCode)
	}
	res, body = keyRequest(t, f, "GET", "/api/automation?path=/api/products/:id/read&method=GET", f.key.Key, "", nil)
	if res.StatusCode != 200 || !strings.Contains(body, `"total":1`) || !strings.Contains(body, "version") {
		t.Fatalf("product API missing from discovery: %d %s", res.StatusCode, body)
	}
	// A user-owned library is accessible from another owned mailbox and when
	// the user has no attached mailbox at all.
	second, err := f.db.UpsertAccount(context.Background(), mailstore.Account{OwnerID: f.user.ID, Email: "second@example.com", Username: "second", IMAPServer: "imap.example.com", SMTPServer: "smtp.example.com", EncryptedPassword: "encrypted"})
	if err != nil {
		t.Fatal(err)
	}
	res, body = keyRequest(t, f, "GET", "/api/products", f.key.Key, "", map[string]string{"X-Account-ID": second.ID})
	if res.StatusCode != 200 || !strings.Contains(body, id) {
		t.Fatalf("second mailbox library: %d %s", res.StatusCode, body)
	}
	for _, account := range []mailstore.Account{f.account, second} {
		if err := f.db.DeleteAccount(context.Background(), f.user.ID, account.ID); err != nil {
			t.Fatal(err)
		}
	}
	res, body = keyRequest(t, f, "GET", "/api/products", f.key.Key, "", nil)
	if res.StatusCode != 200 || !strings.Contains(body, id) {
		t.Fatalf("library required a mailbox: %d %s", res.StatusCode, body)
	}
}

func TestProductCatalogAPIQueryAndLegacyImport(t *testing.T) {
	f := newAPIKeyFixture(t)
	ctx := context.Background()
	input := mailstore.ProductInput{Name: "API 球阀", Model: "API-BV", Catalog: &mailstore.ProductCatalog{Dimensions: []mailstore.ProductDimension{{Key: "material", Label: "材质", Type: "text"}}, Variants: []mailstore.ProductVariant{{Attributes: map[string]string{"material": "304"}, Prices: []mailstore.ProductPrice{{Amount: "88.50", Currency: "CNY"}}}}}}
	body, _ := json.Marshal(input)
	res, raw := keyRequest(t, f, "POST", "/api/products", f.key.Key, string(body), nil)
	if res.StatusCode != 201 {
		t.Fatal(res.StatusCode, raw)
	}
	var created struct {
		Product mailstore.Product `json:"product"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatal(err)
	}
	p := created.Product
	for _, tt := range []struct {
		key, body string
		status    int
		contains  string
	}{
		{f.key.Key, fmt.Sprintf(`{"productId":%q}`, p.ID), 200, `"status":"needs_clarification"`},
		{f.key.Key, fmt.Sprintf(`{"productId":%q,"attributes":{"material":"304"},"qty":"2"}`, p.ID), 200, `"status":"matched"`},
		{f.otherKey.Key, fmt.Sprintf(`{"productId":%q}`, p.ID), 404, ""},
		{f.key.Key, fmt.Sprintf(`{"productId":%q,"version":99}`, p.ID), 409, ""},
		{f.key.Key, fmt.Sprintf(`{"productId":%q,"attributes":{"typo":"304"}}`, p.ID), 400, ""},
		{f.key.Key, fmt.Sprintf(`{"productId":%q,"qty":"-1"}`, p.ID), 400, ""},
		{"", fmt.Sprintf(`{"productId":%q}`, p.ID), 401, ""},
	} {
		res, raw = keyRequest(t, f, "POST", "/api/products/query-prices", tt.key, tt.body, nil)
		if res.StatusCode != tt.status || !strings.Contains(raw, tt.contains) {
			t.Fatal(tt, res.StatusCode, raw)
		}
	}
	list, _, err := f.db.CreatePriceList(ctx, f.user.ID, "旧价格", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.SavePriceRows(ctx, f.user.ID, list.ID, []mailstore.PriceRow{{Data: map[string]string{"name": "旧产品", "model": "OLD", "price": "10", "currency": "CNY", "unit": "台"}}}, nil); err != nil {
		t.Fatal(err)
	}
	res, raw = keyRequest(t, f, "GET", "/api/product-price-imports", f.key.Key, "", nil)
	if res.StatusCode != 200 {
		t.Fatal(raw)
	}
	var pending struct {
		Rows  []mailstore.PendingProductPrice `json:"rows"`
		Total int                             `json:"total"`
	}
	if err = json.Unmarshal([]byte(raw), &pending); err != nil || pending.Total != 1 {
		t.Fatal(raw, err)
	}
	path := "/api/product-price-imports/" + pending.Rows[0].RowID
	res, raw = keyRequest(t, f, "POST", path, f.otherKey.Key, `{}`, nil)
	if res.StatusCode != 404 {
		t.Fatal(res.StatusCode, raw)
	}
	res, raw = keyRequest(t, f, "POST", path, f.key.Key, `{}`, nil)
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, raw)
	}
	res, raw = keyRequest(t, f, "POST", path, f.key.Key, `{}`, nil)
	if res.StatusCode != 400 {
		t.Fatal("duplicate import", res.StatusCode, raw)
	}
	res, raw = keyRequest(t, f, "GET", "/api/product-price-imports", f.key.Key, "", nil)
	if res.StatusCode != 200 || !strings.Contains(raw, `"total":0`) {
		t.Fatal(raw)
	}
}

func TestSharedProductDimensionAPI(t *testing.T) {
	f := newAPIKeyFixture(t)
	path := "/api/product-dimensions"
	res, body := keyRequest(t, f, "POST", path, f.key.Key, `{"label":"材质","type":"select","options":["304","316"]}`, nil)
	if res.StatusCode != 201 {
		t.Fatal(res.StatusCode, body)
	}
	var created struct {
		Dimension mailstore.ProductDimensionRecord `json:"dimension"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	d := created.Dimension
	res, body = keyRequest(t, f, "GET", path, f.key.Key, "", nil)
	if res.StatusCode != 200 || !strings.Contains(body, d.Key) || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "GET", path, f.otherKey.Key, "", nil)
	if res.StatusCode != 200 || strings.Contains(body, d.Key) {
		t.Fatal("foreign dimension exposed", body)
	}
	res, body = keyRequest(t, f, "PUT", path+"/"+d.Key, f.otherKey.Key, `{"label":"overwrite","type":"text","version":1}`, nil)
	if res.StatusCode != 404 {
		t.Fatal(res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "POST", "/api/products", f.key.Key, fmt.Sprintf(`{"name":"球阀","catalog":{"dimensions":[{"key":%q}],"variants":[{"attributes":{%q:"304"}}]}}`, d.Key, d.Key), nil)
	if res.StatusCode != 201 {
		t.Fatal(res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "DELETE", path+"/"+d.Key+"?version=1", f.key.Key, "", nil)
	if res.StatusCode != 400 {
		t.Fatal("used dimension deleted", res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "PUT", path+"/"+d.Key, f.key.Key, `{"label":"阀体材质","type":"select","options":["304","316","铸铁"],"version":1}`, nil)
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "PUT", path+"/"+d.Key, f.key.Key, `{"label":"stale","type":"text","version":1}`, nil)
	if res.StatusCode != 409 {
		t.Fatal(res.StatusCode, body)
	}
	res, _ = keyRequest(t, f, "GET", path, "", "", nil)
	if res.StatusCode != 401 {
		t.Fatal("public dictionary", res.StatusCode)
	}
}
