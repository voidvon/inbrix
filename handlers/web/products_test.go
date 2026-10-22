package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"inbrix/mailstore"
)

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
