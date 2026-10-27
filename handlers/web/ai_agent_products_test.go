package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"

	"inbrix/mailstore"
)

func TestEmailAgentProductResearchAndDocumentImage(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var requests []agentTestRequest
	var product mailstore.Product
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, request)
		switch len(requests) {
		case 1:
			return "```tool_call\n" + `{"name":"search_products","arguments":{"query":"P42"}}` + "\n```"
		case 2:
			return fmt.Sprintf("```tool_call\n{\"name\":\"read_product\",\"arguments\":{\"id\":%q,\"version\":1,\"length\":200}}\n```", product.ID)
		case 3:
			return fmt.Sprintf("```tool_call\n{\"name\":\"generate_document\",\"arguments\":{\"type\":\"quotation\",\"items\":[{\"productId\":%q,\"productVersion\":1,\"description\":\"已核对条件的产品\",\"qty\":\"1\"}]}}\n```", product.ID)
		default:
			return fmt.Sprintf("请核对尚缺的工况参数。参考 [P42 产品资料](/products?product=%s)，v1 使用条件。报价草稿已附产品图。", product.ID)
		}
	})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
	var err error
	product, err = h.mailDB.SaveProduct(ctx, account.OwnerID, "", mailstore.ProductInput{Name: "候选产品", Model: "P-42", Specification: "## 使用条件\n适用于指定介质。压力与温度必须按组合条件核对。\n" + strings.Repeat("长规格只应按需读取。", 500), ImageDataURL: &imageURL})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if _, err := h.mailDB.SaveProduct(ctx, account.OwnerID, "", mailstore.ProductInput{Name: fmt.Sprintf("其他产品 %d", i), Specification: "不相关的完整规格内容" + strings.Repeat("其他描述", 500)}); err != nil {
			t.Fatal(err)
		}
	}
	addAgentTestMail(t, h, account, 2, "请判断 P42 是否适用，并起草报价。")
	id := conversationID(account.ID, []string{"peer@example.com"})
	status, result, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, ConversationID: id, Messages: []AIAgentChatMessage{{Role: "user", Content: "核对产品要求，起草报价并带上产品图"}}})
	if status != 200 {
		t.Fatalf("agent: %d %s", status, raw)
	}
	if result.Document == nil || len(result.Document.Items) != 1 || result.Document.Items[0].Picture != product.ImageURL || result.Document.Items[0].Model != "P-42" {
		t.Fatalf("product image not attached: %s", raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 || len(result.ToolCalls) != 3 {
		t.Fatalf("tool loop: %d requests %d calls", len(requests), len(result.ToolCalls))
	}
	if strings.Contains(requests[0].Input, "不相关的完整规格内容") || strings.Contains(requests[0].Input, "长规格只应按需读取") {
		t.Fatal("product library was injected into initial context")
	}
	if !strings.Contains(requests[1].Input, product.ID) || !strings.Contains(requests[2].Input, "压力与温度") {
		t.Fatal("search/read evidence did not reach agent")
	}
	for _, request := range requests {
		if strings.Contains(request.Input, "data:image/") || strings.Contains(request.Input, "不相关的完整规格内容") {
			t.Fatal("binary image or unrelated full specifications entered agent context")
		}
	}
}

func TestDraftOnlyAgentProductResearch(t *testing.T) {
	ctx := context.Background()
	var requests []agentTestRequest
	var product mailstore.Product
	h, app, account, other := agentContextTestApp(t, func(request agentTestRequest) string {
		requests = append(requests, request)
		switch len(requests) {
		case 1:
			return "```tool_call\n" + `{"name":"search_products","arguments":{"query":"TD62LM"}}` + "\n```"
		case 2:
			return fmt.Sprintf("```tool_call\n{\"name\":\"read_product\",\"arguments\":{\"id\":%q,\"version\":1}}\n```", product.ID)
		default:
			return "TD62LM 最低工作压力为 8 barg，草稿中的 3 barg 工况不符合。"
		}
	})
	var err error
	product, err = h.mailDB.SaveProduct(ctx, account.OwnerID, "", mailstore.ProductInput{Name: "TD62LM", Model: "TD62LM", Specification: "## 使用限制\n最低工作压力 8 barg。"})
	if err != nil {
		t.Fatal(err)
	}
	id := conversationID(account.ID, []string{"new@example.com"})
	if _, err := h.mailDB.SaveDraft(ctx, account.OwnerID, mailstore.DraftInput{AccountEmail: account.Email, ConversationID: id, To: "new@example.com", Subject: "数据库旧主题", BodyText: "旧草稿"}); err != nil {
		t.Fatal(err)
	}
	input := AIAgentChatRequest{
		AccountEmail: account.Email, ConversationID: id,
		ActiveDraft: &AIAgentEmailDraft{To: "new@example.com", Cc: "cc@example.com", Subject: "TD62LM 选型确认", BodyText: "请确认 TD62LM 是否适用于入口压力 3 barg。"},
		Messages:    []AIAgentChatMessage{{Role: "user", Content: "核对我正在写的邮件与产品资料"}},
	}
	status, result, raw := postAgentContext(t, app, input)
	if status != 200 || len(result.ToolCalls) != 2 || !strings.Contains(result.Content, "不符合") {
		t.Fatalf("draft product research: %d %s", status, raw)
	}
	for _, want := range []string{`"messageCount":0`, `"status":"unsent"`, `"source":"editor"`, "TD62LM 选型确认", "入口压力 3 barg", "cc@example.com"} {
		if !strings.Contains(requests[0].Input, want) {
			t.Errorf("missing draft context %q", want)
		}
	}
	if strings.Contains(requests[0].Input, "旧草稿") || strings.Contains(requests[0].Input, "最低工作压力 8 barg") || !strings.Contains(requests[2].Input, "最低工作压力 8 barg") {
		t.Fatal("draft freshness or on-demand product evidence failed")
	}
	input.ActiveDraft = nil
	if status, _, raw := postAgentContext(t, app, input); status != 200 {
		t.Fatalf("stored draft: %d %s", status, raw)
	}
	if !strings.Contains(requests[3].Input, `"source":"database"`) || !strings.Contains(requests[3].Input, "旧草稿") {
		t.Fatal("closed editor did not use stored draft")
	}
	input.ActiveDraft = &AIAgentEmailDraft{Subject: "清空正文"}
	if status, _, raw := postAgentContext(t, app, input); status != 200 {
		t.Fatalf("empty live draft: %d %s", status, raw)
	}
	if !strings.Contains(requests[4].Input, `"bodyText":""`) || strings.Contains(requests[4].Input, "旧草稿") {
		t.Fatal("empty live editor was replaced by stale saved content")
	}
	input.ActiveDraft.BodyText = strings.Repeat("长草稿", 10000) + "不应进入上下文的结尾"
	if status, _, raw := postAgentContext(t, app, input); status != 200 {
		t.Fatalf("long draft: %d %s", status, raw)
	}
	if !strings.Contains(requests[5].Input, `"bodyTruncated":true`) || strings.Contains(requests[5].Input, "不应进入上下文的结尾") {
		t.Fatal("long draft was not bounded and marked truncated")
	}
	input.AccountEmail = other.Email
	if status, _, _ := postAgentContext(t, app, input); status != 404 {
		t.Fatalf("foreign draft conversation returned %d", status)
	}
	input.AccountEmail, input.ConversationID = account.Email, ""
	if status, _, _ := postAgentContext(t, app, input); status != 400 {
		t.Fatalf("unscoped active draft returned %d", status)
	}
}

func TestProductToolsRejectForeignAndStaleReferences(t *testing.T) {
	h, _, account, other := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	p, err := h.mailDB.SaveProduct(ctx, account.OwnerID, "", mailstore.ProductInput{Name: "Private", Model: "P42", Specification: "## 范围\n技术依据"})
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []AIToolCall{
		{Name: "read_product", Args: map[string]any{"id": p.ID, "version": 1}},
		{Name: "generate_document", Args: map[string]any{"items": []any{map[string]any{"productId": p.ID, "productVersion": 1}}}},
	} {
		if _, _, err := h.executeTool(ctx, other.OwnerID, other.ID, call, mailstore.AIModelRecord{}, ""); !errors.Is(err, mailstore.ErrNotFound) {
			t.Fatalf("foreign tool reference accepted: %s %v", call.Name, err)
		}
	}
	for _, args := range []map[string]any{{"id": p.ID}, {"id": p.ID, "version": 2}, {"id": p.ID, "version": 1, "length": 4001}} {
		if _, _, err := h.executeProductTool(ctx, account.OwnerID, AIToolCall{Name: "read_product", Args: args}); err == nil {
			t.Fatalf("invalid/stale read accepted: %v", args)
		}
	}
	for _, name := range []string{"search_products", "read_product", "list_product_categories"} {
		if !hasAgentChatTool(name) {
			t.Fatalf("missing product tool %s", name)
		}
	}
}

func TestCanvasProductImageSurvivesStructuredRead(t *testing.T) {
	payload := canvasPayload{}
	payload.Data.Main = []canvasElement{{Type: "table", TrList: []canvasRow{{TdList: []canvasCell{
		{Value: []canvasElement{{Control: &canvasControl{ConceptID: "item_model_0", Value: []canvasElement{{Value: "P42"}}}}}},
		{Value: []canvasElement{{Type: "image", Value: "/api/product-images/pimg_snapshot"}}},
		{Value: []canvasElement{{Control: &canvasControl{ConceptID: "item_qty_0", Value: []canvasElement{{Value: "2"}}}}}},
	}}}}}
	data, _ := json.Marshal(payload)
	_, items, ok := canvasDocumentContent(canonicalTestCanvas(canvasDocumentPrefix + string(data)))
	if !ok || len(items) != 1 || items[0].Picture != "/api/product-images/pimg_snapshot" || items[0].Qty != "2" {
		t.Fatalf("snapshot image missing: %+v", items)
	}
}

func TestProductToolBudgetsRetainContinuation(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	in := mailstore.ProductInput{Name: strings.Repeat("<", 200), Brand: strings.Repeat("<", 100), Model: "COMMON42", Specification: strings.Repeat("# "+strings.Repeat("<", 120)+"\n", 100)}
	p, err := h.mailDB.SaveProduct(ctx, account.OwnerID, "", in)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := h.executeProductTool(ctx, account.OwnerID, AIToolCall{Name: "read_product", Args: map[string]any{"id": p.ID, "version": 1, "length": 4000}})
	if err != nil {
		t.Fatal(err)
	}
	read := result.(mailstore.ProductRead)
	encoded, _ := json.Marshal(result)
	if len(encoded) > 18000 || !read.HasMore || read.NextOffset != len([]rune(read.Content)) || read.NextOffset == 0 {
		t.Fatalf("unbounded or broken read: %d bytes, next %d", len(encoded), read.NextOffset)
	}
	in.Specification = strings.Repeat("<", 500)
	for i := 0; i < 19; i++ {
		if _, err := h.mailDB.SaveProduct(ctx, account.OwnerID, "", in); err != nil {
			t.Fatal(err)
		}
	}
	result, _, err = h.executeProductTool(ctx, account.OwnerID, AIToolCall{Name: "search_products", Args: map[string]any{"query": "common42", "limit": 20}})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(mailstore.ProductSearchPage)
	encoded, _ = json.Marshal(result)
	if len(encoded) > 16000 || !page.HasMore || page.Total != 20 || page.NextOffset != len(page.Products) || len(page.Products) == 0 {
		t.Fatalf("unbounded or broken search: %d bytes, %+v", len(encoded), page)
	}
}
