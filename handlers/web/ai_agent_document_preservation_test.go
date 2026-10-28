package web

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func preservationDraft() *AIDocumentCard {
	return &AIDocumentCard{
		Type: "quotation", Title: "原报价单", Counterparty: "原客户", Contact: "张经理", Currency: "CNY",
		Notes: "原备注", Terms: []string{"付款：货到30天"},
		Values: map[string]string{"seller_company": "原供应商", "customer_email": "buyer@example.com", "customer_address": "原地址", "lead_time": "两周", "quote_number": "KEEP-001", "discount": "10", "inland_freight": "20"},
		Items: []AIDocumentItem{
			{LineID: "line-a", Model: "SAME", Description: "第一种规格", Qty: "1", Price: "100", Picture: "https://example.com/a.png"},
			{LineID: "line-b", Model: "SAME", Description: "第二种规格", Qty: "2", Price: "50", Picture: "https://example.com/b.png"},
		},
	}
}

func TestAgentDraftContinuationPreservesUnmentionedData(t *testing.T) {
	calls := 0
	h, app, account, _ := agentContextTestApp(t, func(r agentTestRequest) string {
		calls++
		switch calls {
		case 1:
			// Reproduce a model trying to replace the quotation with this turn's
			// partial information. The tool must reject it and allow recovery.
			return "```tool_call\n{\"name\":\"generate_document\",\"arguments\":{\"type\":\"quotation\",\"items\":[{\"model\":\"SAME\",\"qty\":\"3\"}]}}\n```"
		case 2:
			if !strings.Contains(r.Input, "commands") {
				t.Error("replacement failure did not guide the model to atomic commands")
			}
			return "```tool_call\n{\"name\":\"generate_document\",\"arguments\":{\"type\":\"quotation\",\"commands\":[{\"type\":\"update_line\",\"lineId\":\"line-b\",\"fields\":{\"qty\":\"3\"}}]}}\n```"
		case 3:
			return "```tool_call\n{\"name\":\"generate_document\",\"arguments\":{\"type\":\"quotation\",\"commands\":[{\"type\":\"set_fields\",\"fields\":{\"lead_time\":\"三周\"}}]}}\n```"
		default:
			// A second partial snapshot in the final answer must not overwrite
			// the authoritative tool result either.
			return "已修改数量。\n```document\n{\"type\":\"quotation\",\"title\":\"丢失信息的报价\",\"items\":[]}\n```"
		}
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "Quote", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	original := preservationDraft()
	seedAgentSessionReply(t, h, account, session.ID, 0, map[string]any{"role": "assistant", "document": original})
	version := int64(2)
	status, res, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "preserve", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "第二种规格的数量改成3，交期改成三周，重新生成报价单"}}})
	if status != 200 || res.Document == nil {
		t.Fatalf("continuation failed: %d %s", status, raw)
	}
	doc := res.Document
	if calls != 4 || doc.Title != original.Title || doc.Counterparty != original.Counterparty || doc.Contact != original.Contact || doc.Notes != original.Notes || doc.Total != "260.00" {
		t.Fatalf("lost headline data or incorrect total: %+v", doc)
	}
	for key, value := range original.Values {
		if key == "lead_time" {
			value = "三周"
		}
		if doc.Values[key] != value {
			t.Errorf("lost %s: want %q, got %q", key, value, doc.Values[key])
		}
	}
	if doc.Values["payment_terms"] != "付款：货到30天" || len(doc.Items) != 2 {
		t.Fatalf("lost terms or lines: %+v", doc)
	}
	for i := range doc.Items {
		got, want := doc.Items[i], original.Items[i]
		got.Amount, want.Amount = "", ""
		if i == 1 {
			want.Qty = "3"
		}
		if got != want {
			t.Fatalf("unrequested line changes: got %+v, want %+v", got, want)
		}
	}
	state, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	var saved AIDocumentCard
	if err != nil || json.Unmarshal(state.CurrentDocument, &saved) != nil || !reflect.DeepEqual(saved.Values, doc.Values) || !reflect.DeepEqual(saved.Items, doc.Items) {
		t.Fatalf("complete snapshot not persisted: %+v %v", state, err)
	}
}

func TestAgentPartialDocumentFenceCannotReplaceCurrentDraft(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string {
		return "```document\n{\"type\":\"quotation\",\"title\":\"Partial\",\"values\":{\"lead_time\":\"三周\"}}\n```"
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "Quote", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	seedAgentSessionReply(t, h, account, session.ID, 0, map[string]any{"role": "assistant", "document": preservationDraft()})
	before, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	version := before.Version
	status, _, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "partial-fence", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "交期改成三周"}}})
	if status != 422 {
		t.Fatalf("partial replacement accepted: %d %s", status, raw)
	}
	after, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if string(before.CurrentDocument) != string(after.CurrentDocument) {
		t.Fatal("failed generation erased the current draft")
	}
}

func TestAgentDocumentContinuationUsesCommandsAndSavedRevision(t *testing.T) {
	h, _, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	raw, _ := json.Marshal(preservationDraft())
	var doc AIAgentChatDocContext
	_ = json.Unmarshal(raw, &doc)
	ctx = context.WithValue(ctx, agentDocumentContextKey{}, &doc)
	ctx = context.WithValue(ctx, businessRuntimeKey{}, businessRuntime{Profile: mailstore.AIBusinessProfile{Company: "另一个默认供应商"}})
	for _, args := range []map[string]any{
		{"type": "quotation", "items": []any{}},
		{"type": "quotation", "values": map[string]any{"lead_time": "三周"}},
		{"type": "quotation", "commands": []any{}},
	} {
		if _, _, err := h.executeTool(ctx, account.OwnerID, account.ID, AIToolCall{Name: "generate_document", Args: args}, mailstore.AIModelRecord{}, ""); err == nil {
			t.Fatalf("replacement accepted: %+v", args)
		}
	}
	// Explicit commands support intentional clearing/removal and additions,
	// while keeping all other data. Old Terms must not overwrite the edit.
	args := map[string]any{"type": "quotation", "commands": []any{
		map[string]any{"type": "set_fields", "fields": map[string]any{"payment_terms": "付款：预付50%", "notes": ""}},
		map[string]any{"type": "remove_line", "lineId": "line-a"},
		map[string]any{"type": "add_line", "fields": map[string]any{"model": "NEW", "qty": "1", "price": "20"}},
	}}
	result, _, err := h.executeTool(ctx, account.OwnerID, account.ID, AIToolCall{Name: "generate_document", Args: args}, mailstore.AIModelRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	card := result.(*AIDocumentCard)
	if card.Values["seller_company"] != "原供应商" || card.Values["payment_terms"] != "付款：预付50%" || card.Notes != "" || len(card.Items) != 2 || card.Items[0].LineID != "line-b" || card.Items[1].LineID == "" || card.Total != "130.00" {
		t.Fatalf("explicit commands lost: %+v", card)
	}
	if doc.Notes != "原备注" || len(doc.Items) != 2 || doc.Items[0].LineID != "line-a" {
		t.Fatal("proposal mutated its source snapshot")
	}
	doc.ID, doc.Revision = "saved-quote", 7
	if _, _, err := h.executeTool(ctx, account.OwnerID, account.ID, AIToolCall{Name: "generate_document", Args: args}, mailstore.AIModelRecord{}, ""); err == nil || !strings.Contains(err.Error(), fmt.Sprint(doc.Revision)) || !strings.Contains(err.Error(), "update_document") {
		t.Fatalf("saved canvas replacement was not blocked: %v", err)
	}
}
