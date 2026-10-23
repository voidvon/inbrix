package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestMailboxBrandsOwnerIsolationAndUpdates(t *testing.T) {
	h, app, account, other := outboxTestApp(t)
	handler := NewAccountsHandler(h.store, h.config, nil, nil)
	handler.SetMailMirror(h.mailDB, nil)
	app.Get("/api/accounts/:id/brands", handler.HandleAccountBrands)
	app.Put("/api/accounts/:id/brands", handler.HandleAccountBrands)
	request := func(method, id, body string, status int) mailstore.AccountBrands {
		t.Helper()
		req := httptest.NewRequest(method, "/api/accounts/"+id+"/brands", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("%s %s status=%d want %d", method, id, res.StatusCode, status)
		}
		var out mailstore.AccountBrands
		if status == http.StatusOK {
			if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	if got := request("GET", account.ID, "", 200); got.BrandMode != "prefer" || len(got.Brands) != 0 {
		t.Fatal(got)
	}
	got := request("PUT", account.ID, `{"brands":[" A ","A","B"],"brandMode":"restrict"}`, 200)
	if len(got.Brands) != 2 {
		t.Fatal(got)
	}
	request("PUT", account.ID, `{"brands":[],"brandMode":"restrict"}`, 400)
	request("PUT", account.ID, `{"brands":["B"],"brandMode":"unknown"}`, 400)
	request("GET", other.ID, "", 404)
	request("PUT", other.ID, `{"brands":["B"]}`, 404)
	saved := request("GET", account.ID, "", 200)
	if saved.BrandMode != "restrict" || len(saved.Brands) != 2 {
		t.Fatal(saved)
	}
	// Explicit conversation selections can narrow the mailbox scope only, even with no agent selected.
	ctx := context.Background()
	if err := h.mailDB.SaveAIBusinessContext(ctx, account.OwnerID, account.ID, "test", mailstore.AIBusinessContext{Brands: []string{"C"}}); err == nil {
		t.Fatal("scope widening accepted")
	}
	if err := h.mailDB.SaveAIBusinessContext(ctx, other.OwnerID, account.ID, "test", mailstore.AIBusinessContext{}); err == nil {
		t.Fatal("foreign context accepted")
	}
	request("PUT", account.ID, `{"brands":[],"brandMode":"prefer"}`, 200)
	if err := h.mailDB.SaveAIBusinessContext(ctx, account.OwnerID, account.ID, "test", mailstore.AIBusinessContext{Brands: []string{"C"}}); err != nil {
		t.Fatal(err)
	}
}
