package web

import (
	"context"
	"inbrix/config"
	"inbrix/mailstore"
	"strings"
	"testing"
)

func TestParseToolCall(t *testing.T) {
	// Standard markdown tool_call fence
	input := "Let me check emails for you:\n```tool_call\n{\"name\": \"search_emails\", \"arguments\": {\"query\": \"斯派莎克\"}}\n```"
	call, ok := parseToolCall(input)
	if !ok {
		t.Fatalf("expected tool call to be parsed, but got false")
	}
	if call.Name != "search_emails" {
		t.Errorf("expected tool name 'search_emails', got %q", call.Name)
	}
	if q, _ := call.Args["query"].(string); q != "斯派莎克" {
		t.Errorf("expected query '斯派莎克', got %v", call.Args["query"])
	}

	// Raw JSON tool call
	rawJSON := "{\"name\": \"calculate_pricing\", \"args\": {\"items\": [{\"name\": \"valve\", \"qty\": 2, \"unit_price\": 100}]}}"
	call2, ok2 := parseToolCall(rawJSON)
	if !ok2 {
		t.Fatalf("expected raw JSON tool call to be parsed")
	}
	if call2.Name != "calculate_pricing" {
		t.Errorf("expected 'calculate_pricing', got %q", call2.Name)
	}

	// Normal text without tool call
	normalText := "这是为您准备的报价单，请查阅。"
	_, ok3 := parseToolCall(normalText)
	if ok3 {
		t.Errorf("expected ok to be false for normal text")
	}
}

func TestExtractDocumentFromResponse(t *testing.T) {
	input := "我已经为您生成了报价单：\n```document\n{\n  \"type\": \"quotation\",\n  \"title\": \"智能传感器报价单\",\n  \"counterparty\": \"宝钢股份\",\n  \"items\": [{\"model\": \"SN-100\", \"description\": \"温度传感器\", \"qty\": \"10\", \"price\": \"150.00\", \"amount\": \"1500.00\"}]\n}\n```\n请点击下方卡片查看。"
	cleaned, doc := extractDocumentFromResponse(input)
	if doc == nil {
		t.Fatalf("expected document card to be extracted, got nil")
	}
	if doc.Title != "智能传感器报价单" {
		t.Errorf("expected title '智能传感器报价单', got %q", doc.Title)
	}
	if doc.Counterparty != "宝钢股份" {
		t.Errorf("expected counterparty '宝钢股份', got %q", doc.Counterparty)
	}
	if len(doc.Items) != 1 || doc.Items[0].Model != "SN-100" {
		t.Errorf("expected 1 item with model 'SN-100', got %+v", doc.Items)
	}
	if doc.Type != "quotation" {
		t.Errorf("expected type 'quotation', got %q", doc.Type)
	}
	if cleaned == "" || len(cleaned) >= len(input) {
		t.Errorf("expected cleaned content to be stripped of document block, got %q", cleaned)
	}
}

func TestExecuteCalculatePricing(t *testing.T) {
	args := map[string]any{
		"items": []any{
			map[string]any{"name": "减压阀", "qty": 5.0, "unit_price": 1200.0},
			map[string]any{"name": "疏水阀", "qty": 10.0, "price": 350.0},
		},
		"tax_rate": 0.13,
	}
	res, summary, err := executeCalculatePricing(args)
	if err != nil {
		t.Fatalf("calculate_pricing failed: %v", err)
	}
	resMap, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any result")
	}
	// Subtotal: 5 * 1200 = 6000 + 10 * 350 = 3500 => 9500.00
	// Tax: 9500 * 0.13 = 1235.00
	// Total: 10735.00
	if resMap["subtotal"] != "9500.00" {
		t.Errorf("expected subtotal '9500.00', got %v", resMap["subtotal"])
	}
	if resMap["tax_amount"] != "1235.00" {
		t.Errorf("expected tax_amount '1235.00', got %v", resMap["tax_amount"])
	}
	if resMap["total"] != "10735.00" {
		t.Errorf("expected total '10735.00', got %v", resMap["total"])
	}
	if summary == "" {
		t.Errorf("expected non-empty summary")
	}
}

func TestExecuteGenerateDocument(t *testing.T) {
	args := map[string]any{
		"type":         "quotation",
		"title":        "工业阀门采购报价",
		"counterparty": "中石化物资装备部",
		"contact":      "张经理",
		"items": []any{
			map[string]any{
				"model":       "DP27",
				"description": "蒸汽减压阀 DN50",
				"qty":         "2",
				"price":       "4500.00",
			},
		},
		"terms": []any{
			"付款方式: 合同生效后预付30%，发货前付清70%",
			"交货期: 现货，款到后3个工作日内发出",
			"质保期: 调试合格后12个月",
		},
	}

	res, summary, err := executeGenerateDocument(args)
	if err != nil {
		t.Fatalf("generate_document failed: %v", err)
	}
	doc, ok := res.(*AIDocumentCard)
	if !ok || doc == nil {
		t.Fatalf("expected *AIDocumentCard")
	}
	if doc.Title != "工业阀门采购报价" {
		t.Errorf("expected title '工业阀门采购报价', got %q", doc.Title)
	}
	if doc.Values["buyer_company"] != "中石化物资装备部" {
		t.Errorf("expected buyer_company '中石化物资装备部', got %q", doc.Values["buyer_company"])
	}
	if doc.Values["payment_terms"] == "" {
		t.Errorf("expected payment_terms to be populated in values")
	}
	if doc.Values["delivery_date"] == "" {
		t.Errorf("expected delivery_date to be populated in values")
	}
	if len(doc.Items) != 1 || doc.Items[0].Amount != "9000.00" {
		t.Errorf("expected item amount to be computed '9000.00', got %+v", doc.Items)
	}
	if summary == "" {
		t.Errorf("expected non-empty summary")
	}
}

func TestExecuteSearchEmailsEmptyQuery(t *testing.T) {
	h := &AISettingsHandler{}
	res, summary, err := h.executeSearchEmails(context.Background(), "acc1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "搜索关键词为空" {
		t.Errorf("expected '搜索关键词为空', got %q", summary)
	}
	if res != "搜索关键词不能为空" {
		t.Errorf("expected '搜索关键词不能为空', got %v", res)
	}
}

func TestParseMultipleToolCalls(t *testing.T) {
	// The exact user report case: multiple tool_call fences in one turn
	input := "```tool_call\n{\"name\": \"search_emails\", \"arguments\": {\"query\": \"pickup confirmation\"}}\n```\n\n```tool_call\n{\"name\": \"read_email\", \"arguments\": {\"uid\": \"386\", \"folder\": \"Sent Messages\"}}\n```"
	calls := parseToolCalls(input)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls to be parsed, got %d", len(calls))
	}
	if calls[0].Name != "search_emails" {
		t.Errorf("expected first tool 'search_emails', got %q", calls[0].Name)
	}
	if q, _ := calls[0].Args["query"].(string); q != "pickup confirmation" {
		t.Errorf("expected query 'pickup confirmation', got %v", calls[0].Args["query"])
	}
	if calls[1].Name != "read_email" {
		t.Errorf("expected second tool 'read_email', got %q", calls[1].Name)
	}
	if uid, _ := calls[1].Args["uid"].(string); uid != "386" {
		t.Errorf("expected uid '386', got %v", calls[1].Args["uid"])
	}
	if folder, _ := calls[1].Args["folder"].(string); folder != "Sent Messages" {
		t.Errorf("expected folder 'Sent Messages', got %v", calls[1].Args["folder"])
	}
}

func TestStripResidualToolCalls(t *testing.T) {
	// Mixed text and tool_call fence
	mixed := "根据您的需求：\n```tool_call\n{\"name\": \"search_emails\", \"arguments\": {\"query\": \"test\"}}\n```\n我已为您找到相关信息。"
	cleaned := stripResidualToolCalls(mixed)
	if cleaned != "根据您的需求：\n\n我已为您找到相关信息。" {
		t.Errorf("unexpected stripped output: %q", cleaned)
	}

	// Only tool call fence
	onlyTool := "```tool_call\n{\"name\": \"search_emails\", \"arguments\": {\"query\": \"pickup confirmation\"}}\n```"
	cleanedOnly := stripResidualToolCalls(onlyTool)
	if cleanedOnly != "" {
		t.Errorf("expected empty string for pure tool call, got %q", cleanedOnly)
	}

	// Pure JSON tool call
	jsonOnly := "{\"name\": \"read_email\", \"arguments\": {\"uid\": \"123\"}}"
	cleanedJSON := stripResidualToolCalls(jsonOnly)
	if cleanedJSON != "" {
		t.Errorf("expected empty string for pure json tool call, got %q", cleanedJSON)
	}
}

func TestGenerateFallbackResponse(t *testing.T) {
	toolCalls := []AIToolCall{
		{Name: "search_emails", Summary: "检索到 4 封相关邮件 (关键词: FedEx pickup confirmation number C)"},
		{Name: "search_emails", Summary: "检索【pickup confirmation code C number FedEx remote pickup】未找到邮件"},
	}
	fallback := generateFallbackResponse(toolCalls, nil)
	if fallback == "" {
		t.Fatalf("expected non-empty fallback")
	}
	if !strings.Contains(fallback, "检索到 4 封相关邮件") {
		t.Errorf("expected fallback to contain tool summary, got %q", fallback)
	}
}

func TestParseStringArg(t *testing.T) {
	args := map[string]any{
		"query":     "test query",
		"uid_int":   float64(386),
		"folder":    "Sent Messages",
		"empty_str": "   ",
	}

	if s := parseStringArg(args, "query"); s != "test query" {
		t.Errorf("expected 'test query', got %q", s)
	}
	if s := parseStringArg(args, "non_existent", "folder"); s != "Sent Messages" {
		t.Errorf("expected fallback to 'folder', got %q", s)
	}
	if s := parseStringArg(args, "uid_int"); s != "386" {
		t.Errorf("expected float64 386 converted to string '386', got %q", s)
	}
	if s := parseStringArg(args, "empty_str", "query"); s != "test query" {
		t.Errorf("expected empty string to be skipped and fallback to query, got %q", s)
	}
}

func TestExecuteLookupPrice(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(ctx, "agent", "Agent", "hash")
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := db.CreatePriceList(ctx, user.ID, "阀门价目表", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SavePriceRows(ctx, user.ID, list.ID, []mailstore.PriceRow{
		{Data: map[string]string{"model": "DN50-BV", "name": "DN50 球阀", "price": "1280", "currency": "CNY", "unit": "台", "tax": "13%", "lead_time": "15"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	handler := NewAISettingsHandler(nil, &config.Config{}, db)

	result, summary, err := handler.executeLookupPrice(ctx, user.ID, map[string]any{"query": "DN50-BV", "qty": "10"})
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	matches, ok := payload["matches"].([]aiPriceHit)
	if !ok {
		t.Fatalf("matches type = %T", payload["matches"])
	}
	if len(matches) != 1 || matches[0].UnitPrice != 1280 || matches[0].TaxRate != 0.13 {
		t.Fatalf("matches = %+v", matches)
	}
	if matches[0].SKU != "DN50-BV" || matches[0].Unit != "台" || matches[0].LeadTime != "15" {
		t.Fatalf("roled fields = %+v", matches[0])
	}
	if payload["requested_qty"] != "10" {
		t.Fatalf("requested_qty = %v", payload["requested_qty"])
	}
	if !strings.Contains(summary, "1") {
		t.Fatalf("summary = %q", summary)
	}

	// A miss must be explicit so the model has no room to invent a price.
	result, summary, err = handler.executeLookupPrice(ctx, user.ID, map[string]any{"query": "不存在的型号"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "未找到") {
		t.Fatalf("miss summary = %q", summary)
	}
	if text, ok := result.(string); !ok || !strings.Contains(text, "不要推测价格") {
		t.Fatalf("miss result = %#v", result)
	}

	// An empty query is rejected rather than matching everything.
	if _, summary, _ := handler.executeLookupPrice(ctx, user.ID, nil); !strings.Contains(summary, "为空") {
		t.Fatalf("empty query summary = %q", summary)
	}
}

func TestPriceLookupCandidates(t *testing.T) {
	texts := []string{
		"客户问 DN50-BV 和 SN-100 的交期，另外 A-1 也报一下",
		"请参考 “不锈钢球阀” 的价格",
		"订单号 20260119 与电话 13800138000，日期 2026-10-19",
	}
	candidates := priceLookupCandidates(texts, 10)
	joined := strings.Join(candidates, ",")
	for _, want := range []string{"DN50-BV", "SN-100", "A-1", "不锈钢球阀"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("candidate %q missing from %v", want, candidates)
		}
	}
	// Pure numbers and pure words are not model numbers.
	if strings.Contains(joined, "13800138000") || strings.Contains(joined, "2026-10-19") {
		t.Fatalf("non-model tokens leaked in: %v", candidates)
	}
	if len(candidates) > 10 {
		t.Fatalf("limit not applied: %d", len(candidates))
	}
	if got := priceLookupCandidates(nil, 5); len(got) != 0 {
		t.Fatalf("empty input produced %v", got)
	}
	// Duplicates collapse regardless of case.
	deduped := priceLookupCandidates([]string{"dn50-bv DN50-BV"}, 5)
	if len(deduped) != 1 {
		t.Fatalf("dedupe failed: %v", deduped)
	}
}

func TestFormatPriceContext(t *testing.T) {
	block := formatPriceContext([]mailstore.PriceLookupEntry{
		{Model: "DN50-BV", Name: "DN50 球阀", PriceText: "1280", Currency: "CNY", Unit: "台", TaxRate: 0.13, MOQ: "1", LeadTime: "15", ListName: "阀门价目表"},
	})
	if !strings.Contains(block, "【价格表匹配】") || !strings.Contains(block, "不得改写或另编") {
		t.Fatalf("block lacks provenance wording: %q", block)
	}
	for _, want := range []string{"型号 DN50-BV", "单价 1280", "币种 CNY", "单位 台", "税率 0.13", "起订量 1", "交期 15", "来源 阀门价目表"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q: %q", want, block)
		}
	}
}

func TestAgentPromptCoversPriceLookup(t *testing.T) {
	conversation := buildAgentSystemPrompt(agentModeConversation, "销售助手", "", nil)
	if !strings.Contains(conversation, "email conversation") {
		t.Fatal("conversation mode is not framed as an email conversation")
	}
	if !strings.Contains(conversation, "lookup_price") {
		t.Fatal("conversation prompt does not advertise lookup_price")
	}
	if !strings.Contains(conversation, "【价格表匹配】") {
		t.Fatal("conversation prompt does not explain the prefetched price block")
	}
	if !strings.Contains(conversation, "NEVER invent") {
		t.Fatal("conversation prompt lacks the no-invention rule")
	}
	document := buildAgentSystemPrompt(agentModeDocument, "", "", &AIAgentChatDocContext{Type: "quotation", Title: "报价单"})
	if !strings.Contains(document, "ACTIVE DOCUMENT IN WORKSPACE") || strings.Contains(document, "email conversation (邮件会话)") {
		t.Fatal("document mode framing is wrong")
	}
}

func TestPrefetchPriceContext(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(ctx, "prefetch", "Prefetch", "hash")
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := db.CreatePriceList(ctx, user.ID, "阀门价目表", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SavePriceRows(ctx, user.ID, list.ID, []mailstore.PriceRow{
		{Data: map[string]string{"model": "DN50-BV", "name": "DN50 球阀", "price": "1280", "currency": "CNY", "unit": "台", "lead_time": "15"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	handler := NewAISettingsHandler(nil, &config.Config{}, db)

	// A thread that names a model gets its authoritative price up front.
	block, entries := handler.prefetchPriceContext(ctx, user.ID, []string{"客户询价：DN50-BV 要 10 台，请报价"}, 8)
	if len(entries) != 1 || entries[0].Model != "DN50-BV" {
		t.Fatalf("entries = %+v", entries)
	}
	if !strings.Contains(block, "单价 1280") || !strings.Contains(block, "DN50-BV") {
		t.Fatalf("block = %q", block)
	}

	// A thread with nothing model-shaped injects nothing.
	block, entries = handler.prefetchPriceContext(ctx, user.ID, []string{"你好，请问在吗？"}, 8)
	if block != "" || len(entries) != 0 {
		t.Fatalf("unexpected prefetch: %q %+v", block, entries)
	}

	// Another user's price list is never visible.
	other, err := db.CreateUser(ctx, "other-prefetch", "Other", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if block, entries := handler.prefetchPriceContext(ctx, other.ID, []string{"DN50-BV"}, 8); block != "" || len(entries) != 0 {
		t.Fatalf("cross-owner prefetch leaked: %q %+v", block, entries)
	}
}
