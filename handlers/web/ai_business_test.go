package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func businessTestProduct(t *testing.T, h *AISettingsHandler, owner, brand string) mailstore.Product {
	t.Helper()
	p, e := h.mailDB.SaveProduct(context.Background(), owner, "", mailstore.ProductInput{Name: brand + "球阀", Brand: brand, Model: "BV-" + brand, Specification: "法兰球阀", Catalog: &mailstore.ProductCatalog{Dimensions: []mailstore.ProductDimension{}, Variants: []mailstore.ProductVariant{{SKU: "BV-" + brand, Attributes: map[string]string{}, Prices: []mailstore.ProductPrice{{Amount: "100", Currency: "CNY"}}}}}})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestBusinessSkillsPersistAndRejectWidening(t *testing.T) {
	var prompts []string
	h, app, account, other := agentContextTestApp(t, func(r agentTestRequest) string {
		prompts = append(prompts, r.Instructions)
		return "已分析客户需求"
	})
	ctx := context.Background()
	a, e := h.mailDB.CreateAIAgent(ctx, mailstore.AIAgentRecord{OwnerID: account.OwnerID, Name: "品牌A销售", Prompt: "使用简洁的中文", BusinessProfile: mailstore.AIBusinessProfile{Company: "销售公司", BrandMode: "restrict", Brands: []string{"C"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.mailDB.SaveAccountBrands(ctx, account.OwnerID, account.ID, mailstore.AccountBrands{Brands: []string{"A", "B"}, BrandMode: "restrict"}); e != nil {
		t.Fatal(e)
	}
	conv := conversationID(account.ID, []string{"peer@example.com"})
	request := AIAgentChatRequest{AccountEmail: account.Email, ConversationID: conv, BusinessContext: &mailstore.AIBusinessContext{AgentID: a.ID, SkillID: "quotation", Brands: []string{"A"}}, Messages: []AIAgentChatMessage{{Role: "user", Content: "请分析并报价"}}}
	status, result, raw := postAgentContext(t, app, request)
	if status != http.StatusOK {
		t.Fatalf("%d %s", status, raw)
	}
	if result.BusinessContext.AgentID != a.ID || result.BusinessContext.SkillID != "quotation" {
		t.Fatal(result.BusinessContext)
	}
	request.BusinessContext = nil
	status, _, raw = postAgentContext(t, app, request)
	if status != http.StatusOK {
		t.Fatalf("%d %s", status, raw)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "Active task: quotation") || !strings.Contains(prompts[1], "Skill email_analysis:") || !strings.Contains(prompts[1], `Enforced catalog brand scope: ["A"]`) || !strings.Contains(prompts[1], "销售公司") {
		t.Fatal(prompts)
	}
	saved, e := h.mailDB.GetAIBusinessContext(ctx, account.ID, conv)
	if e != nil || saved.AgentID != a.ID || len(saved.Brands) != 1 {
		t.Fatalf("%+v %v", saved, e)
	}
	request.BusinessContext = &mailstore.AIBusinessContext{AgentID: a.ID, Brands: []string{"C"}}
	status, _, _ = postAgentContext(t, app, request)
	if status != http.StatusBadRequest {
		t.Fatal("scope widening accepted", status)
	}
	request.BusinessContext = &mailstore.AIBusinessContext{AgentID: a.ID, SkillID: "unknown"}
	status, _, _ = postAgentContext(t, app, request)
	if status != http.StatusBadRequest {
		t.Fatal("unknown skill accepted", status)
	}
	if e = h.mailDB.SaveAIBusinessContext(ctx, other.OwnerID, account.ID, conv, saved); e == nil {
		t.Fatal("foreign account context write accepted")
	}
	request.BusinessContext = &mailstore.AIBusinessContext{AgentID: "missing"}
	status, _, _ = postAgentContext(t, app, request)
	if status != http.StatusBadRequest {
		t.Fatal("invalid agent silently fell back", status)
	}
}

func TestBusinessBrandScopeAcrossCatalogPaths(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "ok" })
	a := businessTestProduct(t, h, account.OwnerID, "A")
	b := businessTestProduct(t, h, account.OwnerID, "B")
	runtime, e := resolveBusinessRuntime(mailstore.AIBusinessProfile{}, mailstore.AccountBrands{BrandMode: "restrict", Brands: []string{"A"}}, mailstore.AIBusinessContext{})
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.WithValue(context.Background(), businessRuntimeKey{}, runtime)
	result, _, e := h.executeTool(ctx, account.OwnerID, account.ID, AIToolCall{Name: "search_products", Args: map[string]any{"limit": float64(1)}}, mailstore.AIModelRecord{}, "")
	page := result.(mailstore.ProductSearchPage)
	if e != nil || page.Total != 1 || page.Products[0].ID != a.ID || page.HasMore {
		t.Fatalf("scope applied after pagination: %+v %v", page, e)
	}
	for _, call := range []AIToolCall{
		{Name: "read_product", Args: map[string]any{"id": b.ID, "version": float64(b.Version)}},
		{Name: "query_product_prices", Args: map[string]any{"productId": b.ID}},
		{Name: "generate_document", Args: map[string]any{"items": []any{map[string]any{"productId": b.ID, "productVersion": float64(b.Version), "qty": "1"}}}},
		{Name: "generate_document", Args: map[string]any{"items": []any{map[string]any{"model": "B", "qty": "1", "price": "100"}}}},
	} {
		if _, _, e = h.executeTool(ctx, account.OwnerID, account.ID, call, mailstore.AIModelRecord{}, ""); e == nil {
			t.Fatalf("bypass accepted: %+v", call)
		}
	}
	result, _, e = h.executeTool(ctx, account.OwnerID, account.ID, AIToolCall{Name: "query_product_prices", Args: map[string]any{"query": "球阀"}}, mailstore.AIModelRecord{}, "")
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), b.ID) || !strings.Contains(string(encoded), a.ID) {
		t.Fatalf("price search leaked brand: %s", encoded)
	}
	// Preferences allow explicit other brands; selecting a brand narrows either mode.
	prefer, _ := resolveBusinessRuntime(mailstore.AIBusinessProfile{}, mailstore.AccountBrands{BrandMode: "prefer", Brands: []string{"A"}}, mailstore.AIBusinessContext{})
	if prefer.Brands != nil {
		t.Fatal("preferred brands became a restriction")
	}
	selected, _ := resolveBusinessRuntime(mailstore.AIBusinessProfile{}, mailstore.AccountBrands{BrandMode: "prefer", Brands: []string{"A"}}, mailstore.AIBusinessContext{Brands: []string{"B"}})
	if len(selected.Brands) != 1 || selected.Brands[0] != "B" {
		t.Fatal(selected)
	}
}

func TestBusinessFinalDocumentFenceCannotBypassBrandAndTaxValidation(t *testing.T) {
	var reply string
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return reply })
	a := businessTestProduct(t, h, account.OwnerID, "A")
	b := businessTestProduct(t, h, account.OwnerID, "B")
	agent, e := h.mailDB.CreateAIAgent(context.Background(), mailstore.AIAgentRecord{OwnerID: account.OwnerID, Name: "A", Prompt: "sales", BusinessProfile: mailstore.AIBusinessProfile{BrandMode: "restrict", Brands: []string{"A"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.mailDB.SaveAccountBrands(context.Background(), account.OwnerID, account.ID, mailstore.AccountBrands{Brands: []string{"A"}, BrandMode: "restrict"}); e != nil {
		t.Fatal(e)
	}
	req := AIAgentChatRequest{AccountEmail: account.Email, AgentID: agent.ID, Messages: []AIAgentChatMessage{{Role: "user", Content: "报价"}}}
	reply = fmt.Sprintf("```document\n{\"type\":\"quotation\",\"items\":[{\"productId\":%q,\"productVersion\":1,\"qty\":\"2\"}]}\n```", b.ID)
	status, _, _ := postAgentContext(t, app, req)
	if status == http.StatusOK {
		t.Fatal("cross-brand final fence accepted")
	}
	v := a.Catalog.Variants[0]
	reply = fmt.Sprintf("```document\n{\"type\":\"quotation\",\"total\":\"99999\",\"items\":[{\"productId\":%q,\"productVersion\":1,\"variantId\":%q,\"priceId\":%q,\"qty\":\"2\",\"price\":\"100\",\"amount\":\"1\"}]}\n```", a.ID, v.ID, v.Prices[0].ID)
	status, result, raw := postAgentContext(t, app, req)
	if status != http.StatusOK || result.Document.Subtotal != "200.00" || result.Document.Total != "200.00" {
		t.Fatalf("fenced amount not verified: %d %s", status, raw)
	}
}

func businessCanvas(values map[string]string) string {
	var main []any
	for k, v := range values {
		main = append(main, map[string]any{"type": "control", "control": map[string]any{"conceptId": k, "value": []any{map[string]any{"value": v}}}})
	}
	payload := map[string]any{"template": "spirax-quotation", "data": map[string]any{"main": main}}
	seedCanonicalTestSnapshot(payload, values, canvasDocumentItems(values))
	raw, _ := json.Marshal(payload)
	return canvasDocumentPrefix + string(raw)
}
func TestContractQuotationSnapshotDoesNotReprice(t *testing.T) {
	h, _, account, other := agentContextTestApp(t, func(agentTestRequest) string { return "ok" })
	p := businessTestProduct(t, h, account.OwnerID, "A")
	source, e := h.mailDB.SaveDocument(context.Background(), mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "客户确认报价", Company: "采购公司", HTML: businessCanvas(map[string]string{"customer_company": "采购公司", "currency": "CNY", "total_amount": "88.00", "subtotal": "88.00", "payment_terms": "预付", "seller_company": "来源卖方", "item_model_0": "旧型号", "item_product_id_0": p.ID, "item_product_version_0": "1", "item_variant_id_0": p.Catalog.Variants[0].ID, "item_price_id_0": p.Catalog.Variants[0].Prices[0].ID, "item_qty_0": "2", "item_price_0": "44.00", "item_amount_0": "88.00"})})
	if e != nil {
		t.Fatal(e)
	}
	// Price differs from the current catalog even before a new catalog version.
	result, _, e := h.executeTool(context.Background(), account.OwnerID, account.ID, AIToolCall{Name: "generate_document", Args: map[string]any{"type": "contract", "sourceQuotationId": source.ID, "values": map[string]any{"contract_amount": "1", "tax_rate": "13%", "discount": "999", "say_total": "Guessed words"}}}, mailstore.AIModelRecord{}, "")
	if e != nil {
		t.Fatal(e)
	}
	card := result.(*AIDocumentCard)
	if card.Items[0].Price != "44.00" || card.Total != "88.00" || card.Counterparty != "采购公司" || card.Values["contract_amount"] != "88.00" || card.Values["source_quotation_id"] != source.ID {
		t.Fatalf("snapshot lost: %+v", card)
	}
	if card.Values["tax_rate"] != "" || card.Values["discount"] != "" || card.Values["say_total"] != "" {
		t.Fatal("guesses filled missing source conditions", card.Values)
	}
	if _, _, e = h.executeTool(context.Background(), other.OwnerID, other.ID, AIToolCall{Name: "generate_document", Args: map[string]any{"type": "contract", "sourceQuotationId": source.ID}}, mailstore.AIModelRecord{}, ""); e == nil {
		t.Fatal("foreign quotation reused")
	}
}

func TestBusinessTemplateMaterializationPreservesLayoutAndClearsBuyer(t *testing.T) {
	body := canvasDocumentPrefix + `{"template":"spirax-quotation","options":{"watermark":{"data":"CUSTOM"}},"data":{"main":[{"type":"control","control":{"conceptId":"customer_company","value":[{"value":"旧买方"}]}},{"type":"control","control":{"conceptId":"seller_company","value":[{"value":"模板卖方"}]}},{"type":"table","colgroup":[{"width":123}],"trList":[{"tdList":[{"value":[{"value":"CUSTOM HEADER"}]}]},{"tdList":[{"value":[{"type":"control","control":{"conceptId":"item_model_1","value":[{"value":"OLD MODEL"}]}},{"type":"control","control":{"conceptId":"item_price_1","value":[{"value":"999"}]}},{"type":"control","control":{"conceptId":"item_qty_1","value":[]}},{"type":"control","control":{"conceptId":"item_amount_1","value":[]}}]}]}]}]}}`
	card := &AIDocumentCard{Type: "quotation", Values: map[string]string{"seller_company": "业务卖方"}, Items: []AIDocumentItem{{ProductID: "one", Model: "A", Price: "10"}, {ProductID: "two", Model: "B", Price: "20"}}}
	html, e := materializeBusinessTemplate(body, card)
	if e != nil {
		t.Fatal(e)
	}
	vals, items, ok := canvasDocumentContent(html)
	if !ok || len(items) != 2 || items[0].Model != "A" || items[1].Price != "20" || items[0].ProductID != "one" || vals["seller_company"] != "业务卖方" || vals["customer_company"] != "" {
		t.Fatalf("template fields lost: %s", html)
	}
	if !strings.Contains(html, "CUSTOM HEADER") || !strings.Contains(html, `"width":123`) || !strings.Contains(html, "CUSTOM") || strings.Contains(html, "OLD MODEL") || strings.Contains(html, "旧买方") {
		t.Fatalf("template layout or stale data: %s", html)
	}
}

func TestBusinessConfiguredTemplateAppliedToFinalDocument(t *testing.T) {
	var reply string
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return reply })
	ctx := context.Background()
	p := businessTestProduct(t, h, account.OwnerID, "A")
	templateHTML := canvasDocumentPrefix + `{"template":"spirax-quotation","options":{"watermark":{"data":"CUSTOM LAYOUT"}},"data":{"main":[{"type":"control","control":{"conceptId":"customer_company","value":[{"value":"OLD BUYER"}]}},{"type":"control","control":{"conceptId":"seller_company","value":[]}},{"type":"table","trList":[{"tdList":[{"value":[{"type":"control","control":{"conceptId":"item_model_0","value":[]}},{"type":"control","control":{"conceptId":"item_qty_0","value":[]}},{"type":"control","control":{"conceptId":"item_price_0","value":[]}},{"type":"control","control":{"conceptId":"item_amount_0","value":[]}}]}]}]}]}}`
	template, err := h.mailDB.SaveDocumentTemplate(ctx, mailstore.DocumentTemplateRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "A template", HTML: templateHTML})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := h.mailDB.CreateAIAgent(ctx, mailstore.AIAgentRecord{OwnerID: account.OwnerID, Name: "A sales", Prompt: "sales", BusinessProfile: mailstore.AIBusinessProfile{Company: "SELLER", QuotationTemplateID: template.ID}})
	if err != nil {
		t.Fatal(err)
	}
	variant := p.Catalog.Variants[0]
	reply = fmt.Sprintf("```document\n{\"type\":\"quotation\",\"counterparty\":\"NEW BUYER\",\"items\":[{\"productId\":%q,\"productVersion\":1,\"variantId\":%q,\"priceId\":%q,\"qty\":\"2\"}]}\n```", p.ID, variant.ID, variant.Prices[0].ID)
	request := AIAgentChatRequest{AccountEmail: account.Email, AgentID: agent.ID, Messages: []AIAgentChatMessage{{Role: "user", Content: "报价"}}}
	status, result, raw := postAgentContext(t, app, request)
	if status != http.StatusOK {
		t.Fatalf("%d %s", status, raw)
	}
	values, items, ok := canvasDocumentContent(result.Document.HTML)
	if !ok || len(items) != 1 || items[0].Amount != "200.00" || values["customer_company"] != "NEW BUYER" || values["seller_company"] != "SELLER" || result.Document.TemplateID != template.ID || !strings.Contains(result.Document.HTML, "CUSTOM LAYOUT") || strings.Contains(result.Document.HTML, "OLD BUYER") {
		t.Fatalf("configured template not applied: %+v", result.Document)
	}
	if err = h.mailDB.DeleteDocumentTemplate(ctx, account.OwnerID, template.ID); err != nil {
		t.Fatal(err)
	}
	status, _, _ = postAgentContext(t, app, request)
	if status == http.StatusOK {
		t.Fatal("deleted default silently substituted")
	}
}
