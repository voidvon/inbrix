package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	mailapi "inbrix/handlers/api"
	"inbrix/mailstore"
	"inbrix/models"

	"github.com/gofiber/fiber/v2"
)

type agentTestRequest struct {
	Input        string `json:"input"`
	Instructions string `json:"instructions"`
}

func agentContextTestApp(t *testing.T, respond func(agentTestRequest) string) (*AISettingsHandler, *fiber.App, mailstore.Account, mailstore.Account) {
	t.Helper()
	emailHandler, app, account, other := outboxTestApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request agentTestRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Input)+len(request.Instructions) > maxAgentInputBytes {
			t.Errorf("provider input exceeded budget: %d", len(request.Input)+len(request.Instructions))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"output_text": respond(request)})
	}))
	t.Cleanup(provider.Close)
	emailHandler.config.Encryption.Key = "0123456789abcdef0123456789abcdef"
	key, err := mailapi.EncryptJSON("fake-test-key", emailHandler.config.Encryption.Key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = emailHandler.mailDB.CreateAIModel(context.Background(), mailstore.AIModelRecord{OwnerID: account.OwnerID, Provider: "openai", BaseURL: provider.URL + "/v1", Model: "mock-model", EncryptedAPIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	h := NewAISettingsHandler(emailHandler.store, emailHandler.config, emailHandler.mailDB)
	h.client = provider.Client()
	app.Post("/api/ai/agent-chat", h.HandleAgentChat)
	return h, app, account, other
}

func postAgentContext(t *testing.T, app *fiber.App, request AIAgentChatRequest) (int, AIAgentChatResponse, string) {
	t.Helper()
	body, _ := json.Marshal(request)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent-chat", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	response, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var result AIAgentChatResponse
	if response.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, result, string(raw)
}

func addAgentTestMail(t *testing.T, h *AISettingsHandler, account mailstore.Account, uid int, body string) {
	t.Helper()
	email := models.Email{ID: strconv.Itoa(uid), From: "peer@example.com", To: account.Email, Subject: fmt.Sprintf("交期更新 %d", uid), Preview: fmt.Sprintf("预览 %d", uid), Body: body, Date: time.Date(2026, 10, 20, 10, uid, 0, 0, time.UTC)}
	if err := h.mailDB.UpsertMessages(context.Background(), account.ID, "INBOX", []models.Email{email}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentContextRefreshesNotesAndNewMail(t *testing.T) {
	var mu sync.Mutex
	var requests []agentTestRequest
	answers := []string{
		"第一次回答",
		"```tool_call\n{\"name\":\"search_emails\",\"arguments\":{\"query\":\"交期更新 5\"}}\n```",
		"```tool_call\n{\"name\":\"read_email\",\"arguments\":{\"folder\":\"INBOX\",\"uid\":\"5\",\"length\":4}}\n```",
		"```tool_call\n{\"name\":\"read_email\",\"arguments\":{\"folder\":\"INBOX\",\"uid\":\"5\",\"offset\":4}}\n```",
		"客户最新交期为两周。来源：INBOX/5。",
	}
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, request)
		if len(requests) > len(answers) {
			t.Error("unexpected extra provider call")
			return "unexpected"
		}
		return answers[len(requests)-1]
	})
	addAgentTestMail(t, h, account, 2, "旧邮件二")
	addAgentTestMail(t, h, account, 3, "旧邮件三")
	ctx := context.Background()
	id := conversationID(account.ID, []string{"peer@example.com"})
	if err := h.mailDB.SetConversationNote(ctx, account.ID, id, "最初备注"); err != nil {
		t.Fatal(err)
	}
	input := AIAgentChatRequest{AccountEmail: account.Email, ConversationID: id, Messages: []AIAgentChatMessage{{Role: "user", Content: "请看一下"}}}
	status, _, raw := postAgentContext(t, app, input)
	if status != http.StatusOK {
		t.Fatalf("first request: %d %s", status, raw)
	}
	addAgentTestMail(t, h, account, 4, "邮件四")
	addAgentTestMail(t, h, account, 5, "最新交期调整为两周。")
	if err := h.mailDB.SetConversationNote(ctx, account.ID, id, "本轮最新备注"); err != nil {
		t.Fatal(err)
	}
	input.Messages = append(input.Messages, AIAgentChatMessage{Role: "assistant", Content: "第一次回答"}, AIAgentChatMessage{Role: "user", Content: "最新交期呢？"})
	input.FocusedMessages = []AgentMailReference{{UID: "5", Folder: "INBOX"}}
	status, result, raw := postAgentContext(t, app, input)
	if status != http.StatusOK || !strings.Contains(result.Content, "两周") {
		t.Fatalf("continued discussion: %d %s", status, raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(requests[0].Input, `"messageCount":3`) || !strings.Contains(requests[0].Input, "最初备注") {
		t.Fatalf("initial context missing: %s", requests[0].Input)
	}
	for _, want := range []string{`"messageCount":5`, `"newMessageCountSinceLastDiscussion":2`, "本轮最新备注", `"focusedMailPreviews":[{"uid":"5"`} {
		if !strings.Contains(requests[1].Input, want) {
			t.Errorf("fresh context missing %q", want)
		}
	}
	if strings.Contains(requests[1].Input, "最初备注") || strings.Contains(requests[1].Input, "最新交期调整为两周") {
		t.Fatal("bootstrap reused an old note or included a full mail body")
	}
	if !strings.Contains(requests[3].Input, `"nextOffset":4`) || !strings.Contains(requests[4].Input, "调整为两周") {
		t.Fatal("tool continuation did not reach the rest of the message")
	}
	if len(result.Sources) != 1 || result.Sources[0].UID != "5" || len(result.ToolCalls) != 3 {
		t.Fatalf("source refs/tools were not retained: %+v", result)
	}
}

func TestAgentHundredMailContextStaysSmallAndFindsOldMail(t *testing.T) {
	var mu sync.Mutex
	var requests []agentTestRequest
	answers := []string{
		"```tool_call\n{\"name\":\"search_emails\",\"arguments\":{\"query\":\"历史付款条件\"}}\n```",
		"```tool_call\n{\"name\":\"read_email\",\"arguments\":{\"folder\":\"INBOX\",\"uid\":\"2\",\"offset\":3000}}\n```",
		"历史付款条件为预付30%。来源 INBOX/2。",
	}
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, request)
		if len(requests) > len(answers) {
			return "unexpected"
		}
		return answers[len(requests)-1]
	})
	for uid := 2; uid <= 100; uid++ {
		body := strings.Repeat("长邮件正文", 600)
		if uid == 2 {
			body += "历史付款条件：预付30%。"
		}
		addAgentTestMail(t, h, account, uid, body)
	}
	id := conversationID(account.ID, []string{"peer@example.com"})
	status, result, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, ConversationID: id, Messages: []AIAgentChatMessage{{Role: "user", Content: "之前的付款条件是什么？"}}})
	if status != http.StatusOK {
		t.Fatalf("request: %d %s", status, raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 || !strings.Contains(requests[0].Input, `"messageCount":100`) || len(requests[0].Input) > 6000 || strings.Contains(requests[0].Input, "长邮件正文") {
		t.Fatalf("hundred-mail bootstrap was not compact: %d calls, %d bytes", len(requests), len(requests[0].Input))
	}
	if !strings.Contains(requests[1].Input, "历史付款条件") || !strings.Contains(requests[2].Input, "预付30%") || len(result.Sources) != 1 || result.Sources[0].UID != "2" {
		t.Fatal("failed to retrieve old mail outside the frontend's recent window")
	}
}

func TestAgentHistoricalFT14QuoteFromHundredEmails(t *testing.T) {
	for _, withCurrentPrice := range []bool{false, true} {
		t.Run(fmt.Sprintf("currentPriceList=%t", withCurrentPrice), func(t *testing.T) {
			var mu sync.Mutex
			var requests []agentTestRequest
			answers := []string{
				"```tool_call\n{\"name\":\"search_emails\",\"arguments\":{\"query\":\"ft14\"}}\n```",
				"```tool_call\n{\"name\":\"read_email\",\"arguments\":{\"folder\":\"INBOX\",\"uid\":\"2\"}}\n```",
				"上次邮件中斯派莎克 FT14 疏水阀单价为 1400元。这是历史报价，来源：2026-10-20《交期更新 2》，INBOX/2。",
			}
			searchResultIndex := 1
			readResultIndex := 2
			if withCurrentPrice {
				// Exercise the case where a current price is already known before
				// retrieving the historical source; it must not override that source.
				answers = append([]string{"```tool_call\n{\"name\":\"lookup_price\",\"arguments\":{\"query\":\"ft14\"}}\n```"}, answers...)
				searchResultIndex++
				readResultIndex++
			}
			h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, request)
				if len(requests) > len(answers) {
					return "unexpected"
				}
				return answers[len(requests)-1]
			})
			ctx := context.Background()
			if withCurrentPrice {
				_, err := h.mailDB.SaveProduct(ctx, account.OwnerID, "", mailstore.ProductInput{Name: "FT14", Model: "FT14", Catalog: &mailstore.ProductCatalog{Dimensions: []mailstore.ProductDimension{}, Variants: []mailstore.ProductVariant{{Attributes: map[string]string{}, Prices: []mailstore.ProductPrice{{Amount: "1600", Currency: "CNY", Source: "当前价格表"}}}}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			for uid := 2; uid <= 100; uid++ {
				body := "这是一封无关的往来邮件。"
				if uid == 2 {
					body = "斯派莎克FT14 疏水阀 单价 1400元"
				}
				addAgentTestMail(t, h, account, uid, body)
			}
			id := conversationID(account.ID, []string{"peer@example.com"})
			status, result, raw := postAgentContext(t, app, AIAgentChatRequest{
				AccountEmail: account.Email, ConversationID: id,
				Messages: []AIAgentChatMessage{{Role: "user", Content: "上次ft14报价多少"}},
			})
			if status != http.StatusOK {
				t.Fatalf("request: %d %s", status, raw)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != len(answers) {
				t.Fatalf("expected %d model calls, got %d", len(answers), len(requests))
			}
			if !strings.Contains(requests[0].Input, `"messageCount":100`) || strings.Contains(requests[0].Input, "1400") {
				t.Fatal("historical mail should be retrieved on demand, not embedded in bootstrap")
			}
			marker := "[Tool Result (search_emails)]: "
			_, searchJSON, ok := strings.Cut(requests[searchResultIndex].Input, marker)
			if !ok {
				t.Fatal("search result missing")
			}
			var page agentMailSearchResult
			if err := json.NewDecoder(strings.NewReader(searchJSON)).Decode(&page); err != nil {
				t.Fatal(err)
			}
			if len(page.Messages) != 1 || page.Messages[0].UID != "2" || !strings.Contains(page.Messages[0].Snippet, "1400元") {
				t.Fatalf("lowercase search did not find uppercase model in old body: %+v", page)
			}
			if !strings.Contains(requests[readResultIndex].Input, `"body":"斯派莎克FT14 疏水阀 单价 1400元"`) || len(result.Sources) != 1 || result.Sources[0].UID != "2" {
				t.Fatal("full historical source did not reach the model/response")
			}
			if withCurrentPrice && !strings.Contains(requests[1].Input, `"unit_price":1600`) {
				t.Fatal("conflicting current price did not reach model before historical retrieval")
			}
			if strings.Contains(requests[0].Input, "1600") || strings.Contains(requests[0].Input, "【价格表匹配】") {
				t.Fatal("runtime injected price-list results without a model tool call")
			}
			for _, call := range result.ToolCalls {
				if call.ID == "call_price_prefetch" {
					t.Fatal("runtime made a business-specific tool choice on the model's behalf")
				}
			}
		})
	}
}

func TestAgentMailReadsAreScopedAndUnicodeSafe(t *testing.T) {
	emailHandler, _, account, _ := outboxTestApp(t)
	h := &AISettingsHandler{mailDB: emailHandler.mailDB}
	ctx := context.Background()
	body := strings.Repeat("中文😀abc", 900) + "邮件尾部重要条款"
	email := models.Email{ID: "20", Folder: "INBOX", From: "peer@example.com", To: account.Email, Body: body, BodyCached: true}
	if err := h.mailDB.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{email}); err != nil {
		t.Fatal(err)
	}
	sent := models.Email{ID: "20", Folder: "Sent", From: account.Email, To: "other-peer@example.com", HTML: "<p>这是另一个文件夹的同UID邮件</p>"}
	if err := h.mailDB.UpsertMessages(ctx, account.ID, "Sent", []models.Email{sent}); err != nil {
		t.Fatal(err)
	}
	scope := &agentMailContext{conversation: &Conversation{ID: "test", Messages: []ConversationMessage{{Email: email}}}, byKey: map[string]models.Email{agentMailKey("INBOX", "20"): email}}
	var read strings.Builder
	for offset := 0; ; {
		result, _, err := h.readAgentEmail(ctx, account.ID, scope, map[string]any{"uid": "20", "folder": "INBOX", "offset": offset, "length": 2400})
		if err != nil {
			t.Fatal(err)
		}
		part := result.(map[string]any)
		text := part["body"].(string)
		if !utf8.ValidString(text) {
			t.Fatal("split a Unicode character")
		}
		read.WriteString(text)
		if !part["hasMore"].(bool) {
			break
		}
		offset = part["nextOffset"].(int)
	}
	if read.String() != body {
		t.Fatal("lost or repeated text across read segments")
	}
	if _, _, err := h.readAgentEmail(ctx, account.ID, scope, map[string]any{"uid": "20", "folder": "Sent"}); err == nil {
		t.Fatal("read unrelated conversation by guessing the same UID")
	}
	result, _, err := h.readAgentEmail(ctx, account.ID, scope, map[string]any{"uid": "20", "folder": "Sent", "scope": "mailbox"})
	if err != nil || !strings.Contains(result.(map[string]any)["body"].(string), "另一个文件夹") {
		t.Fatalf("HTML-only read did not use exact folder: %v, %v", result, err)
	}
	for _, args := range []map[string]any{
		{"uid": "20"}, {"uid": "20", "folder": "INBOX", "offset": -1}, {"uid": "20", "folder": "INBOX", "offset": 999999}, {"uid": "20", "folder": "INBOX", "length": 4001},
	} {
		if _, _, err := h.readAgentEmail(ctx, account.ID, scope, args); err == nil {
			t.Fatalf("accepted invalid read arguments: %+v", args)
		}
	}
}

func TestAgentConversationRejectsForeignAccountAndFocus(t *testing.T) {
	_, app, account, other := agentContextTestApp(t, func(agentTestRequest) string {
		t.Error("invalid context reached the provider")
		return "unexpected"
	})
	input := AIAgentChatRequest{AccountEmail: account.Email, ConversationID: conversationID(other.ID, []string{"peer@example.com"}), Messages: []AIAgentChatMessage{{Role: "user", Content: "hi"}}}
	if status, _, _ := postAgentContext(t, app, input); status != http.StatusNotFound {
		t.Fatalf("foreign conversation returned %d", status)
	}
	input.ConversationID = conversationID(account.ID, []string{"peer@example.com"})
	input.FocusedMessages = []AgentMailReference{{UID: "999", Folder: "INBOX"}}
	if status, _, _ := postAgentContext(t, app, input); status != http.StatusBadRequest {
		t.Fatalf("foreign focused mail returned %d", status)
	}
	input.FocusedMessages = nil
	input.AccountEmail = other.Email
	if status, _, _ := postAgentContext(t, app, input); status != http.StatusNotFound {
		t.Fatalf("foreign account returned %d", status)
	}
}

func TestAgentDiscussionSummaryCacheAndClear(t *testing.T) {
	var mu sync.Mutex
	summaryCalls := 0
	h, _, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		mu.Lock()
		defer mu.Unlock()
		summaryCalls++
		if !strings.Contains(request.Instructions, "Preserve the user's goals") {
			t.Error("missing summary contract")
		}
		return "用户要求含税报价；来源 INBOX/1，需重新读取核实。"
	})
	ctx := context.Background()
	messages := []AIAgentChatMessage{}
	for i := 0; i < 24; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, AIAgentChatMessage{Role: role, Content: fmt.Sprintf("讨论%d：", i) + strings.Repeat("这是历史讨论", 130)})
	}
	messages = append(messages, AIAgentChatMessage{Role: "user", Content: "最新问题必须保留"})
	model, err := h.mailDB.GetDefaultAIModel(ctx, account.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	scope := &agentMailContext{}
	history, err := h.agentHistory(ctx, messages, scope, model, "fake-test-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) > maxAgentHistoryBytes || !strings.Contains(history, "最新问题必须保留") || scope.memory.HistoryCount == 0 || !strings.Contains(history, "来源 INBOX/1") {
		t.Fatal("summary did not retain recent discussion and source references")
	}
	mu.Lock()
	firstCalls := summaryCalls
	mu.Unlock()
	if firstCalls == 0 {
		t.Fatal("large history was not summarized")
	}
	id := conversationID(account.ID, []string{"peer@example.com"})
	if err := h.mailDB.SaveAIChatContext(ctx, account.ID, id, scope.memory); err != nil {
		t.Fatal(err)
	}
	scope.memory, err = h.mailDB.GetAIChatContext(ctx, account.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.agentHistory(ctx, messages, scope, model, "fake-test-key"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if summaryCalls != firstCalls {
		t.Error("unchanged history generated another summary")
	}
	mu.Unlock()
	// Growing history should extend the existing memory, rather than summarize
	// the entire prefix again on every question.
	beforeExtension := scope.memory.HistoryCount
	messages = append(messages, AIAgentChatMessage{Role: "assistant", Content: strings.Repeat("新增讨论", 600)}, AIAgentChatMessage{Role: "user", Content: "继续讨论"})
	if _, err := h.agentHistory(ctx, messages, scope, model, "fake-test-key"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if scope.memory.HistoryCount <= beforeExtension || summaryCalls != firstCalls+1 {
		t.Errorf("history extension did not reuse the cached prefix: count=%d calls=%d", scope.memory.HistoryCount, summaryCalls)
	}
	firstCalls = summaryCalls
	mu.Unlock()
	messages[0].Content = "用户修改了历史约定" + messages[0].Content
	if _, err := h.agentHistory(ctx, messages, scope, model, "fake-test-key"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if summaryCalls <= firstCalls {
		t.Error("edited history reused a stale summary")
	}
	mu.Unlock()
	if err := h.mailDB.DeleteConversationAIChat(ctx, account.ID, id); err != nil {
		t.Fatal(err)
	}
	record, err := h.mailDB.GetAIChatContext(ctx, account.ID, id)
	if err != nil || record.HistoryCount != 0 || record.HistorySummary != "" || record.MailKeys != "" {
		t.Fatalf("clearing chat retained derived memory: %+v %v", record, err)
	}
}

func TestAgentContextKeepsNotesWithoutMail(t *testing.T) {
	var input string
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		input = request.Input
		return "已参考备注。"
	})
	ctx := context.Background()
	id := conversationID(account.ID, []string{"new-customer@example.com"})
	_, err := h.mailDB.SaveDraft(ctx, account.OwnerID, mailstore.DraftInput{AccountEmail: account.Email, ConversationID: id, Mode: mailstore.DraftModeNew, To: "new-customer@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.mailDB.SetConversationNote(ctx, account.ID, id, "这个客户需要英文沟通"); err != nil {
		t.Fatal(err)
	}
	status, _, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, ConversationID: id, Messages: []AIAgentChatMessage{{Role: "user", Content: "帮我准备开场白"}}})
	if status != http.StatusOK || !strings.Contains(input, "这个客户需要英文沟通") || !strings.Contains(input, `"messageCount":0`) {
		t.Fatalf("note-only discussion: %d %s", status, raw)
	}
}

func TestAgentToolBudgetStopsWithoutClaimingCompleteCoverage(t *testing.T) {
	var mu sync.Mutex
	providerCalls := 0
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		mu.Lock()
		defer mu.Unlock()
		providerCalls++
		if strings.Contains(request.Input, "No more tools are available") {
			if !strings.Contains(request.Input, "incomplete coverage") {
				t.Error("budget stop did not require disclosing incomplete coverage")
			}
			return "本次只读取了部分内容，尚未完成全部邮件的核对。"
		}
		return "```tool_call\n{\"name\":\"read_email\",\"arguments\":{\"folder\":\"INBOX\",\"uid\":\"2\",\"length\":4000}}\n```"
	})
	addAgentTestMail(t, h, account, 2, strings.Repeat("长正文", 4000))
	id := conversationID(account.ID, []string{"peer@example.com"})
	status, result, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, ConversationID: id, Messages: []AIAgentChatMessage{{Role: "user", Content: "检查全部内容"}}})
	if status != http.StatusOK || !strings.Contains(result.Content, "尚未完成") {
		t.Fatalf("budget exhaustion: %d %s", status, raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if providerCalls > 12 || len(result.ToolCalls) > 10 || len(result.Sources) != 1 {
		t.Fatalf("unbounded/repeated source work: calls=%d tools=%d sources=%d", providerCalls, len(result.ToolCalls), len(result.Sources))
	}
}

func TestAgentSearchFollowsEveryPageInCurrentConversation(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]bool)
	h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
		mu.Lock()
		defer mu.Unlock()
		marker := "[Tool Result (search_emails)]: "
		index := strings.LastIndex(request.Input, marker)
		args := map[string]any{"limit": 20}
		if index >= 0 {
			var page agentMailSearchResult
			if err := json.NewDecoder(strings.NewReader(request.Input[index+len(marker):])).Decode(&page); err != nil {
				t.Error(err)
				return "bad page"
			}
			for _, message := range page.Messages {
				key := message.Folder + "/" + message.UID
				if seen[key] || message.UID == "999" {
					t.Errorf("duplicate/unrelated message: %s", key)
				}
				seen[key] = true
			}
			if !page.HasMore {
				return "已列出会话全部邮件目录，正文尚待按需读取。"
			}
			args["cursor"] = page.NextCursor
		}
		call, _ := json.Marshal(map[string]any{"name": "search_emails", "arguments": args})
		return "```tool_call\n" + string(call) + "\n```"
	})
	for uid := 2; uid <= 100; uid++ {
		addAgentTestMail(t, h, account, uid, "邮件正文")
	}
	if err := h.mailDB.UpsertMessages(context.Background(), account.ID, "INBOX", []models.Email{{ID: "999", From: "another-customer@example.com", To: account.Email, Subject: "其他会话", Date: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	id := conversationID(account.ID, []string{"peer@example.com"})
	status, result, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, ConversationID: id, Messages: []AIAgentChatMessage{{Role: "user", Content: "先列出本会话全部邮件目录"}}})
	if status != http.StatusOK || len(result.ToolCalls) != 5 {
		t.Fatalf("paged search: %d %s", status, raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 100 {
		t.Fatalf("did not enumerate all 100 messages: %d", len(seen))
	}
}
