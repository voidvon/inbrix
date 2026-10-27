package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func seedAgentSessionReply(t *testing.T, h *AISettingsHandler, account mailstore.Account, sessionID string, version int64, assistant any) {
	t.Helper()
	ctx := context.Background()
	key := fmt.Sprintf("seed-%d", version)
	turn, err := h.mailDB.BeginAIChatTurn(ctx, account.OwnerID, account.ID, sessionID, key, key, version, json.RawMessage(`{"role":"user","content":"earlier question"}`), mailstore.AIBusinessContext{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(assistant)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.mailDB.FinishAIChatTurn(ctx, turn, raw, func(string, int64) (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentDocumentLongHistoryCompactsAndContinues(t *testing.T) {
	summaries, answers := 0, 0
	h, app, account, _ := agentContextTestApp(t, func(r agentTestRequest) string {
		if strings.HasPrefix(r.Instructions, "Summarize earlier AI discussion") {
			summaries++
			return "用户正在核对报价，未解决交期。"
		}
		answers++
		if !strings.Contains(r.Input, "short follow-up") {
			t.Error("latest question lost")
		}
		return "continued"
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	seedAgentSessionReply(t, h, account, session.ID, 0, map[string]string{"role": "assistant", "content": strings.Repeat("historical text ", 7000)})
	version := int64(2)
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "first", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "short follow-up"}}}
	status, res, raw := postAgentContext(t, app, input)
	if status != 200 || summaries == 0 || answers != 1 {
		t.Fatalf("long history blocked: %d summaries=%d %s", status, summaries, raw)
	}
	saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Memory.HistoryCount == 0 || saved.Memory.HistorySummary == "" || !strings.Contains(string(saved.Messages[1]), strings.Repeat("historical text ", 7000)) {
		t.Fatal("summary not durable or originals were replaced")
	}
	firstSummaries := summaries
	version = res.Version
	input.RequestID = "second"
	status, _, raw = postAgentContext(t, app, input)
	if status != 200 || summaries != firstSummaries || answers != 2 {
		t.Fatalf("summary not reused: %d summaries=%d %s", status, summaries, raw)
	}
	// Invalid new text never consumes a version or pollutes durable history.
	before, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	version = before.Version
	input.RequestID = "oversize"
	input.Messages[0].Content = strings.Repeat("x", maxAgentRecentBytes+1)
	status, _, _ = postAgentContext(t, app, input)
	after, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if status != 413 || after.Version != before.Version || len(after.Messages) != len(before.Messages) {
		t.Fatal("oversized new turn was committed")
	}
}

func TestAgentCurrentDraftSurvivesHistoryPagingAndNewDraftReset(t *testing.T) {
	var instructions []string
	h, app, account, _ := agentContextTestApp(t, func(r agentTestRequest) string {
		instructions = append(instructions, r.Instructions)
		return "needs details"
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	doc := &AIDocumentCard{Type: "quotation", Title: "Server draft", Counterparty: "Latest buyer", Values: map[string]string{"seller_company": "Our company", "buyer_email": "old-buyer@example.com"}, Items: []AIDocumentItem{{Model: "DT-99", Price: "123.45"}}, Terms: []string{"30 days"}}
	seedAgentSessionReply(t, h, account, session.ID, 0, map[string]any{"role": "assistant", "document": doc})
	for i := 1; i <= 51; i++ {
		seedAgentSessionReply(t, h, account, session.ID, int64(i*2), map[string]string{"role": "assistant", "content": "ok"})
	}
	latest, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID, mailstore.AIChatMessagePage{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !latest.HasMoreMessages || !strings.Contains(string(latest.CurrentDocument), "Server draft") {
		t.Fatal("draft lost beyond latest 100 messages")
	}
	for _, m := range latest.Messages {
		if strings.Contains(string(m), "Server draft") {
			t.Fatal("fixture draft unexpectedly on current page")
		}
	}
	version := latest.Version
	input := AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "before-page", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "check draft"}}, ActiveDocument: &AIAgentChatDocContext{Title: "Forged client draft"}}
	status, res, raw := postAgentContext(t, app, input)
	if status != 200 {
		t.Fatalf("draft follow-up: %d %s", status, raw)
	}
	_, err = h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID, mailstore.AIChatMessagePage{Limit: 100, Before: latest.NextBefore})
	if err != nil {
		t.Fatal(err)
	}
	version = res.Version
	input.RequestID = "after-page"
	status, res, raw = postAgentContext(t, app, input)
	if status != 200 {
		t.Fatalf("paged follow-up: %d %s", status, raw)
	}
	if len(instructions) != 2 || instructions[0] != instructions[1] || strings.Contains(instructions[0], "Forged client draft") {
		t.Fatal("client paging or supplied draft changed authoritative context")
	}
	for _, want := range []string{"Server draft", "Latest buyer", "DT-99", "123.45", "30 days", "Our company"} {
		if !strings.Contains(instructions[0], want) {
			t.Fatalf("missing draft fact %q", want)
		}
	}
	version = res.Version
	input.RequestID = "new"
	input.Messages[0].Content = "新建一份报价单"
	status, res, raw = postAgentContext(t, app, input)
	if status != 200 {
		t.Fatalf("new draft: %d %s", status, raw)
	}
	snapshot, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if strings.Contains(string(snapshot.CurrentDocument), "Latest buyer") || strings.Contains(string(snapshot.CurrentDocument), "DT-99") || !strings.Contains(string(snapshot.CurrentDocument), "Our company") {
		t.Fatal("new draft retained old buyer/items or lost seller")
	}
	// Reset remains effective while the assistant asks for more information.
	version = res.Version
	input.RequestID = "new-follow-up"
	input.Messages[0].Content = "客户是另一家公司"
	status, _, raw = postAgentContext(t, app, input)
	if status != 200 {
		t.Fatalf("new follow-up: %d %s", status, raw)
	}
	if strings.Contains(instructions[len(instructions)-1], "Latest buyer") || strings.Contains(instructions[len(instructions)-1], "DT-99") {
		t.Fatal("old draft returned after clarification")
	}
}

func TestReadCurrentDraftPagesLargeSnapshot(t *testing.T) {
	doc := &AIAgentChatDocContext{Type: "contract", Title: "Large", Values: map[string]string{"terms": strings.Repeat("中文条款", 5000)}}
	prompt := buildAgentSystemPrompt(agentModeDocument, "", "", doc)
	if len(prompt) > maxAgentInputBytes-maxAgentHistoryBytes-maxAgentToolResultBytes || !strings.Contains(prompt, "too large to inline") {
		t.Fatal("large draft exhausted model input")
	}
	ctx := context.WithValue(context.Background(), agentDocumentContextKey{}, doc)
	offset := 0
	var parts strings.Builder
	for {
		v, _, err := readCurrentDraft(ctx, map[string]any{"offset": offset, "length": 4000})
		if err != nil {
			t.Fatal(err)
		}
		page := v.(map[string]any)
		parts.WriteString(page["content"].(string))
		if !page["hasMore"].(bool) {
			break
		}
		offset = page["nextOffset"].(int)
	}
	raw, _ := json.Marshal(doc)
	if parts.String() != string(raw) {
		t.Fatal("draft paging lost data")
	}
	if _, _, err := readCurrentDraft(ctx, map[string]any{"offset": -1}); err == nil {
		t.Fatal("invalid draft offset accepted")
	}
}

func TestAgentMailContinuesStructuredDraftWithoutLosingMailMode(t *testing.T) {
	var requests []agentTestRequest
	h, app, account, _ := agentContextTestApp(t, func(r agentTestRequest) string {
		requests = append(requests, r)
		if len(requests) == 1 {
			return "```tool_call\n{\"name\":\"read_current_draft\",\"arguments\":{}}\n```"
		}
		return "continued"
	})
	ctx := context.Background()
	conversation := conversationID(account.ID, []string{"peer@example.com"})
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "mail", conversation, "Mail", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	seedAgentSessionReply(t, h, account, session.ID, 0, map[string]any{"role": "assistant", "document": &AIDocumentCard{Type: "quotation", Title: "Mail quote", Counterparty: "Buyer", Items: []AIDocumentItem{{Model: "UNIQUE-MAIL-SKU", Price: "123.45"}}}})
	version := int64(2)
	status, _, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, ConversationID: conversation, RequestID: "continue", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "把刚才报价数量改为两件"}}})
	if status != 200 || len(requests) != 2 {
		t.Fatalf("mail draft follow-up: %d %s calls=%d", status, raw, len(requests))
	}
	if !strings.Contains(requests[0].Instructions, "inside an email conversation") || !strings.Contains(requests[0].Instructions, "UNIQUE-MAIL-SKU") {
		t.Fatal("lost mail identity or structured draft")
	}
	if !strings.Contains(requests[1].Input, "UNIQUE-MAIL-SKU") || strings.Contains(requests[1].Input, `"available":false`) {
		t.Fatal("read_current_draft did not expose mail draft")
	}
}

func TestAgentQuotationConversionKeepsSnapshotThroughClarification(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(r agentTestRequest) string {
		for _, want := range []string{"Buyer", "DT-99", "100.00", "Net30"} {
			if !strings.Contains(r.Instructions, want) {
				t.Errorf("missing conversion fact %s", want)
			}
		}
		return "请确认合同交期"
	})
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "Quote", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	seedAgentSessionReply(t, h, account, session.ID, 0, map[string]any{"role": "assistant", "document": &AIDocumentCard{Type: "quotation", Title: "Quotation", Counterparty: "Buyer", Items: []AIDocumentItem{{Model: "DT-99", Price: "100.00"}}, Values: map[string]string{"payment_terms": "Net30"}}})
	version := int64(2)
	status, _, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "convert", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Content: "根据刚才的报价制作合同"}}})
	if status != 200 {
		t.Fatalf("conversion: %d %s", status, raw)
	}
	saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved.CurrentDocument), "DT-99") || strings.Contains(string(saved.CurrentDocument), `"newDraft":true`) {
		t.Fatal("clarification destructively reset the quotation")
	}
}

func TestDocumentIntentOnlyClearsExplicitFreshDrafts(t *testing.T) {
	session := mailstore.AIChatSession{Kind: "document", DocumentType: "quotation", CurrentDocument: json.RawMessage(`{"type":"quotation","counterparty":"Buyer","items":[{"model":"DT-99"}],"values":{"seller_company":"Seller"}}`)}
	for _, question := range []string{"根据刚才的报价制作合同", "制作合同", "重新生成合同", "不要新建报价单", "新建合同需要什么资料", "新建一份报价单有什么条件？", "请修改合同", "Create a new contract from the quotation", "新建合同，沿用刚才的明细"} {
		doc, err := sessionDocumentContext(session, question)
		if err != nil || doc.NewDraft || len(doc.Items) != 1 {
			t.Errorf("ambiguous request reset draft: %s %+v %v", question, doc, err)
		}
	}
	for _, question := range []string{"新建一份报价单", "请帮我新建一份合同", "Please create a new contract", "Start a blank quotation"} {
		doc, err := sessionDocumentContext(session, question)
		if err != nil || !doc.NewDraft || len(doc.Items) != 0 || doc.Counterparty != "" || doc.Values["seller_company"] != "Seller" {
			t.Errorf("explicit new draft not isolated: %s %+v %v", question, doc, err)
		}
	}
}

func TestAgentUsesHumanSavedCanvasRevision(t *testing.T) {
	var instructions string
	h, app, account, _ := agentContextTestApp(t, func(r agentTestRequest) string { instructions = r.Instructions; return "continued" })
	ctx := context.Background()
	session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "Quote", "quotation")
	if err != nil {
		t.Fatal(err)
	}
	seedAgentSessionReply(t, h, account, session.ID, 0, map[string]any{"role": "assistant", "document": &AIDocumentCard{Type: "quotation", Title: "Old quote", Counterparty: "Buyer", Items: []AIDocumentItem{{Model: "REMOVED-SKU"}}, Terms: []string{"Net30"}, Values: map[string]string{"payment_terms": "Net30"}}})
	state, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	var message struct {
		ID string `json:"id"`
	}
	json.Unmarshal(state.Messages[1], &message)
	controller := NewDocumentsHandler(h.sessions, h.mailDB)
	app.Post("/api/documents", controller.HandleSaveDocument)
	for revision, term := range []string{"Net30", "Net60"} {
		input := saveDocumentInput{ID: message.ID, Revision: int64(revision), Type: "quotation", Name: "Human edited", Company: "Buyer", HTML: canvasDocumentPrefix + `{"data":{"main":[{"type":"control","control":{"conceptId":"payment_terms","value":[{"value":"` + term + `"}]}}]}}`}
		if revision == 0 {
			input.AIOrigin = &mailstore.AIChatDocumentOrigin{SessionID: session.ID, MessageID: message.ID}
		}
		input.HTML = canonicalTestCanvas(input.HTML)
		raw, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", "/api/documents", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		res, e := app.Test(req)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("save human revision: %d %s", res.StatusCode, body)
		}
	}
	state, _ = h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
	app.Get("/api/ai/chat-sessions/:id", h.HandleAIChatSession)
	detailRequest := httptest.NewRequest("GET", "/api/ai/chat-sessions/"+session.ID+"?accountEmail="+account.Email, nil)
	detail, e := app.Test(detailRequest)
	if e != nil {
		t.Fatal(e)
	}
	detailBody, _ := io.ReadAll(detail.Body)
	detail.Body.Close()
	var projected struct {
		CurrentDocument AIAgentChatDocContext `json:"currentDocument"`
	}
	if e = json.Unmarshal(detailBody, &projected); e != nil {
		t.Fatal(e)
	}
	if detail.StatusCode != 200 || projected.CurrentDocument.Revision != 2 || projected.CurrentDocument.Values["payment_terms"] != "Net60" || len(projected.CurrentDocument.Items) != 0 {
		t.Fatalf("detail projection stale: %s", detailBody)
	}
	status, _, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "after-save", Version: &state.Version, Messages: []AIAgentChatMessage{{Role: "user", Content: "核对当前付款条款"}}})
	if status != 200 {
		t.Fatalf("follow-up: %d %s", status, raw)
	}
	if !strings.Contains(instructions, "Net60") || !strings.Contains(instructions, `"revision":2`) || strings.Contains(instructions, "Net30") || strings.Contains(instructions, "REMOVED-SKU") {
		t.Fatalf("saved content did not replace generated draft: %s", instructions)
	}
}

func TestAgentDocumentProposalsUseRevisionAndHTTPRejectsStaleConfirmation(t *testing.T) {
	h, app, account, other := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	record, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{ID: "quote", OwnerID: account.OwnerID, Type: "quotation", Name: "Quote", HTML: businessCanvas(map[string]string{"customer_company": "Buyer"})}, 0)
	if err != nil {
		t.Fatal(err)
	}
	update, _, err := executeDocumentUpdateTest(h, ctx, account.OwnerID, map[string]any{"id": record.ID, "values": map[string]any{"customer_company": "AI buyer"}}, mailstore.AIModelRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	proposal, ok := update.(*AIDocumentAction)
	if !ok || proposal.BaseRevision != record.Revision {
		t.Fatalf("proposal lacks base version: %#v", update)
	}
	deletion, _, err := h.executeDeleteDocument(ctx, account.OwnerID, map[string]any{"id": record.ID})
	if err != nil || deletion.(*AIDocumentAction).BaseRevision != record.Revision {
		t.Fatal("delete proposal lacks base revision")
	}
	handler := NewDocumentsHandler(h.sessions, h.mailDB)
	app.Post("/api/documents", handler.HandleSaveDocument)
	app.Delete("/api/documents/:id", handler.HandleDeleteDocument)
	request := func(method, path string, revision int64, foreign bool) int {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"id": record.ID, "revision": revision, "type": "quotation", "name": "Quote", "html": businessCanvas(map[string]string{"customer_company": "Human buyer"})})
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		if foreign {
			req.Header.Set("X-Test-Outsider", other.Email)
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if status := request(http.MethodPost, "/api/documents", 1, false); status != 200 {
		t.Fatalf("human edit %d", status)
	}
	if status := request(http.MethodPost, "/api/documents", proposal.BaseRevision, false); status != 409 {
		t.Fatalf("stale apply %d", status)
	}
	if status := request(http.MethodDelete, "/api/documents/quote?revision=1", 0, false); status != 409 {
		t.Fatalf("stale delete %d", status)
	}
	if status := request(http.MethodPost, "/api/documents", 2, true); status != 409 {
		t.Fatalf("foreign save %d", status)
	}
	if status := request(http.MethodDelete, "/api/documents/quote?revision=2", 0, false); status != 200 {
		t.Fatalf("current delete %d", status)
	}
	if status := request(http.MethodPost, "/api/documents", 2, false); status != 409 {
		t.Fatalf("deleted document recreated %d", status)
	}
}
