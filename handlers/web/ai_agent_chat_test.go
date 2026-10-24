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
	// The quotation prints "Lead Time" and "Total", the contract prints a subtotal:
	// the derived values have to reach the ids those templates actually read.
	if doc.Values["lead_time"] != "交货期: 现货，款到后3个工作日内发出" {
		t.Errorf("expected lead_time to mirror the delivery term, got %q", doc.Values["lead_time"])
	}
	if doc.Values["subtotal"] == "" {
		t.Errorf("expected subtotal to be populated in values")
	}
	if doc.Values["currency"] != "¥" {
		t.Errorf("expected currency in values, got %q", doc.Values["currency"])
	}
	if len(doc.Items) != 1 || doc.Items[0].Amount != "9000.00" {
		t.Errorf("expected item amount to be computed '9000.00', got %+v", doc.Items)
	}
	if summary == "" {
		t.Errorf("expected non-empty summary")
	}
}

func TestExecuteGenerateDocumentFillsBuyerDetailsAndValues(t *testing.T) {
	args := map[string]any{
		"type":         "contract",
		"title":        "SALES CONTRACT SPX-2026-0815",
		"counterparty": "上海某某机械有限公司",
		"contact":      "王工",
		"email":        "wang@example.cn",
		"phone":        "+86 138 0000 0000",
		"address":      "上海市浦东新区某某路 88 号",
		"currency":     "USD",
		"items": []any{
			map[string]any{"model": "DN25", "description": "疏水阀", "qty": "10", "price": "1200"},
			map[string]any{"model": "DN40", "description": "过滤器", "qty": "5", "price": "2300"},
		},
		"terms": []any{"付款方式: 30% 预付，发货前付清", "交货期: 15 天"},
		"values": map[string]any{
			"customer_po":    "PO-7788",
			"lead_time":      "15 天",
			"validity":       "30 天",
			"customer_phone": "+86 21 5555 0000",
		},
	}

	res, _, err := executeGenerateDocument(args)
	if err != nil {
		t.Fatalf("generate_document failed: %v", err)
	}
	doc, ok := res.(*AIDocumentCard)
	if !ok || doc == nil {
		t.Fatalf("expected *AIDocumentCard")
	}
	// The buyer's coordinates reach both spellings the two canvases read.
	for key, want := range map[string]string{
		"buyer_company":    "上海某某机械有限公司",
		"buyer_contact":    "王工",
		"buyer_email":      "wang@example.cn",
		"customer_email":   "wang@example.cn",
		"buyer_address":    "上海市浦东新区某某路 88 号",
		"customer_address": "上海市浦东新区某某路 88 号",
		"customer_po":      "PO-7788",
		"validity":         "30 天",
		"customer_phone":   "+86 21 5555 0000",
	} {
		if got := doc.Values[key]; got != want {
			t.Errorf("values[%q] = %q, want %q", key, got, want)
		}
	}
	if doc.Values["buyer_phone"] != "+86 138 0000 0000" {
		t.Errorf("buyer_phone = %q", doc.Values["buyer_phone"])
	}
	if len(doc.Items) != 2 {
		t.Fatalf("expected both product rows, got %+v", doc.Items)
	}
	if doc.Items[0].Amount != "12000.00" || doc.Items[1].Amount != "11500.00" {
		t.Errorf("expected computed amounts, got %+v", doc.Items)
	}
}

func TestExecuteGenerateDocumentExplicitValuesWin(t *testing.T) {
	// The agent sometimes learns the buyer's registered name from the thread while
	// the headline "counterparty" argument carries a short form. The explicit map
	// is the more careful reading, so it must survive hydration.
	res, _, err := executeGenerateDocument(map[string]any{
		"type":         "quotation",
		"title":        "报价单",
		"counterparty": "某某机械",
		"values":       map[string]any{"buyer_company": "上海某某机械有限公司", "currency": "USD"},
	})
	if err != nil {
		t.Fatalf("generate_document failed: %v", err)
	}
	doc := res.(*AIDocumentCard)
	if doc.Values["buyer_company"] != "上海某某机械有限公司" {
		t.Errorf("explicit buyer_company was overwritten: %q", doc.Values["buyer_company"])
	}
	if doc.Values["customer_company"] != "某某机械" {
		t.Errorf("derived customer_company should stay as the counterparty: %q", doc.Values["customer_company"])
	}
}

func TestAgentPromptUsesNativeToolsAndSkillWorkflows(t *testing.T) {
	prompt := buildAgentSystemPrompt(agentModeConversation, "", "", nil)
	if strings.Contains(prompt, "ONE ROW PER PRODUCT") || strings.Contains(prompt, "```tool_call") {
		t.Fatal("business workflow or text calling protocol leaked into the generic prompt")
	}
	if !strings.Contains(prompt, "Do not invent facts") || !strings.Contains(prompt, "native tools") {
		t.Fatal("generic execution rules missing")
	}
	session := mailstore.NewAISkillSession(mailstore.DefaultAIChatSkills())
	workflow, err := session.Prompt("quotation")
	if err != nil || !strings.Contains(workflow, "ONE ROW PER PRODUCT") || !strings.Contains(workflow, "seller_company") {
		t.Fatal("quotation completeness workflow missing from Skill", err)
	}
	for _, tool := range agentChatTools() {
		if tool.Name == "generate_document" {
			if tool.Parameters["properties"] == nil {
				t.Fatal("document arguments missing from native schema")
			}
			return
		}
	}
	t.Fatal("native document tool not registered")
}

func TestExecuteSearchEmailsEmptyQueryListsRecentMail(t *testing.T) {
	emailHandler, _, account, _ := outboxTestApp(t)
	h := &AISettingsHandler{mailDB: emailHandler.mailDB}
	res, _, err := h.searchAgentEmails(context.Background(), account.ID, nil, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	page := res.(agentMailSearchResult)
	if len(page.Messages) != 1 || page.Messages[0].UID != "1" || page.HasMore {
		t.Errorf("expected recent mail page, got %+v", page)
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
	_, err = db.SaveProduct(ctx, user.ID, "", mailstore.ProductInput{Name: "DN50 球阀", Model: "DN50-BV", Catalog: &mailstore.ProductCatalog{Dimensions: []mailstore.ProductDimension{}, Variants: []mailstore.ProductVariant{{SKU: "DN50-BV", Attributes: map[string]string{}, Prices: []mailstore.ProductPrice{{Amount: "1280", Currency: "CNY", TaxRate: "0.13", LeadTime: "15", Source: "阀门价目表"}}}}}})
	if err != nil {
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
	if matches[0].SKU != "DN50-BV" || matches[0].Currency != "CNY" || matches[0].LeadTime != "15" {
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
	if payload, ok := result.(map[string]any); !ok || payload["status"] != "no_match" {
		t.Fatalf("miss result = %#v", result)
	}

	// An empty query is rejected rather than matching everything.
	if _, _, err := handler.executeLookupPrice(ctx, user.ID, nil); err == nil {
		t.Fatal("empty query accepted")
	}
}

func TestAgentPromptExposesToolsWithoutQuestionSpecificRouting(t *testing.T) {
	conversation := buildAgentSystemPrompt(agentModeConversation, "销售助手", "", nil)
	if !strings.Contains(conversation, "email conversation") {
		t.Fatal("conversation mode is not framed as an email conversation")
	}
	for _, tool := range []string{"search_emails", "read_email", "lookup_price", "calculate_pricing"} {
		if !hasAgentChatTool(tool) {
			t.Fatalf("tool %s is not exposed", tool)
		}
	}
	for _, forbidden := range []string{"FT14", "ft14", "斯派莎克", "PRICING RULES", "WHEN TO LOOK UP PRICES", "【价格表匹配】", "HISTORICAL MAIL PRICES"} {
		if strings.Contains(conversation, forbidden) {
			t.Errorf("global prompt contains question-specific routing: %s", forbidden)
		}
	}
	document := buildAgentSystemPrompt(agentModeDocument, "", "", &AIAgentChatDocContext{Type: "quotation", Title: "报价单"})
	if !strings.Contains(document, "ACTIVE DOCUMENT IN WORKSPACE") || strings.Contains(document, "email conversation (邮件会话)") {
		t.Fatal("document mode framing is wrong")
	}
}

// documentToolsFixture stores one canvas quotation for the document tool tests.
func documentToolsFixture(t *testing.T) (*mailstore.Store, string, string) {
	t.Helper()
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	user, err := db.CreateUser(ctx, "docs", "Docs", "hash")
	if err != nil {
		t.Fatal(err)
	}
	body := canvasBody(t, "spirax-quotation", []map[string]any{
		controlElement("quote_number", "SQ-202610"),
		controlElement("buyer_company", "上海宝钢"),
		controlElement("customer_contact", "王经理"),
		controlElement("delivery_time", "30 天"),
		controlElement("total_amount", "12,800.00"),
		tableElement([][]map[string]any{{
			controlElement("item_model", "DN50-BV"),
			controlElement("item_description", "DN50 球阀"),
			controlElement("item_qty", "10"),
			controlElement("item_price", "1280.00"),
			controlElement("item_amount", "12800.00"),
		}}),
	})
	record, err := db.SaveDocument(ctx, mailstore.DocumentRecord{
		OwnerID: user.ID, Type: "quotation", Name: "宝钢报价单", Company: "上海宝钢", HTML: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, user.ID, record.ID
}

func TestExecuteListAndReadDocument(t *testing.T) {
	db, owner, documentID := documentToolsFixture(t)
	handler := NewAISettingsHandler(nil, &config.Config{}, db)
	ctx := context.Background()

	result, summary, err := handler.executeListDocuments(ctx, owner, map[string]any{"query": "宝钢"})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := result.([]aiDocumentSummary)
	if !ok || len(list) != 1 || list[0].ID != documentID {
		t.Fatalf("list = %#v (%s)", result, summary)
	}

	// A query that matches nothing reports it instead of returning everything.
	if _, summary, _ := handler.executeListDocuments(ctx, owner, map[string]any{"query": "不存在的客户"}); !strings.Contains(summary, "未找到") {
		t.Fatalf("unexpected summary: %s", summary)
	}

	read, summary, err := handler.executeReadDocument(ctx, owner, map[string]any{"id": documentID})
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := read.(map[string]any)
	if !ok {
		t.Fatalf("read = %#v", read)
	}
	fields, ok := payload["fields"].(map[string]string)
	if !ok || fields["buyer_company"] != "上海宝钢" {
		t.Fatalf("fields = %#v", payload["fields"])
	}
	items, ok := payload["items"].([]AIDocumentItem)
	if !ok || len(items) != 1 || items[0].Model != "DN50-BV" {
		t.Fatalf("items = %#v", payload["items"])
	}
	if !strings.Contains(summary, "宝钢报价单") {
		t.Fatalf("summary = %s", summary)
	}

	// Unknown ids are reported, not silently accepted.
	if _, summary, _ := handler.executeReadDocument(ctx, owner, map[string]any{"id": "doc_missing"}); !strings.Contains(summary, "未找到") {
		t.Fatalf("summary = %s", summary)
	}
}

func TestExecuteDeleteDocumentProposesOnly(t *testing.T) {
	db, owner, documentID := documentToolsFixture(t)
	handler := NewAISettingsHandler(nil, &config.Config{}, db)
	ctx := context.Background()

	result, summary, err := handler.executeDeleteDocument(ctx, owner, map[string]any{"id": documentID})
	if err != nil {
		t.Fatal(err)
	}
	action, ok := result.(*AIDocumentAction)
	if !ok || action.Kind != "delete" || action.DocumentID != documentID {
		t.Fatalf("action = %#v", result)
	}
	if !strings.Contains(summary, "等待确认") {
		t.Fatalf("summary = %s", summary)
	}
	// The document must still exist: deletion is the user's call.
	if _, err := db.GetDocument(ctx, owner, documentID); err != nil {
		t.Fatalf("document was removed by the proposal: %v", err)
	}
}

func TestExecuteUpdateDocumentRejectsUnstructured(t *testing.T) {
	ctx := context.Background()
	db, err := mailstore.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(ctx, "legacy", "Legacy", "hash")
	if err != nil {
		t.Fatal(err)
	}
	record, err := db.SaveDocument(ctx, mailstore.DocumentRecord{
		OwnerID: user.ID, Type: "quotation", Name: "旧格式报价单", HTML: "<div>plain html</div>",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAISettingsHandler(nil, &config.Config{}, db)

	result, summary, err := handler.executeUpdateDocument(ctx, user.ID, map[string]any{"id": record.ID, "instruction": "改单价"}, mailstore.AIModelRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, isAction := result.(*AIDocumentAction); isAction {
		t.Fatalf("unstructured document produced an action: %#v", result)
	}
	if !strings.Contains(summary, "不支持结构化修改") {
		t.Fatalf("summary = %s", summary)
	}

	// A missing instruction is reported before any model work happens.
	if _, summary, _ := handler.executeUpdateDocument(ctx, user.ID, map[string]any{"id": record.ID}, mailstore.AIModelRecord{}, ""); !strings.Contains(summary, "缺少修改要求") {
		t.Fatalf("summary = %s", summary)
	}
}

func TestAgentPromptCoversDocumentTools(t *testing.T) {
	prompt := buildAgentSystemPrompt(agentModeConversation, "", "", nil)
	for _, tool := range []string{"list_documents", "read_document", "update_document", "delete_document"} {
		if !hasAgentChatTool(tool) {
			t.Fatalf("prompt does not advertise %s", tool)
		}
	}
	if !strings.Contains(prompt, "UI confirmation") {
		t.Fatal("prompt does not require confirmation for document changes")
	}
}

func TestExecuteUpdateDocumentAppliesExplicitValues(t *testing.T) {
	db, owner, documentID := documentToolsFixture(t)
	handler := NewAISettingsHandler(nil, &config.Config{}, db)
	ctx := context.Background()

	// The agent read the document, so it can name the fields directly; no model
	// call is needed and the merge must keep every untouched field.
	result, summary, err := handler.executeUpdateDocument(ctx, owner, map[string]any{
		"id":          documentID,
		"instruction": "改交期与联系人",
		"values":      map[string]any{"delivery_time": "35 天", "customer_contact": "李经理"},
		"items": []any{
			map[string]any{"model": "DN50-BV", "description": "DN50 球阀", "qty": "10", "price": "1280.00", "amount": "12800.00"},
			map[string]any{"model": "TD42", "qty": "5", "price": "380", "amount": "1900"},
		},
	}, mailstore.AIModelRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	action, ok := result.(*AIDocumentAction)
	if !ok || action.Kind != "update" {
		t.Fatalf("action = %#v", result)
	}
	if action.Values["delivery_time"] != "35 天" || action.Values["customer_contact"] != "李经理" {
		t.Fatalf("values = %#v", action.Values)
	}
	// Untouched fields survive the merge.
	if action.Values["buyer_company"] != "上海宝钢" || action.Values["quote_number"] != "SQ-202610" {
		t.Fatalf("merge dropped fields: %#v", action.Values)
	}
	if len(action.Items) != 2 || action.Items[1].Model != "TD42" || action.Items[1].Qty != "5" {
		t.Fatalf("items = %#v", action.Items)
	}
	if !strings.Contains(action.Summary, "delivery_time") || !strings.Contains(summary, "修改方案") {
		t.Fatalf("summary = %q / %q", action.Summary, summary)
	}
	// The proposal alone must not touch the stored document.
	record, err := db.GetDocument(ctx, owner, documentID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(record.HTML, "35 天") {
		t.Fatal("proposal wrote to the document")
	}
}

func hasAgentChatTool(name string) bool {
	for _, spec := range agentChatTools() {
		if spec.Name == name {
			return true
		}
	}
	return false
}
