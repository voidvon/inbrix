package web

import (
	"context"
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
