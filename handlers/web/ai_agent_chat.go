package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	mailapi "inbrix/handlers/api"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

type AIAgentChatMessage struct {
	Role      string          `json:"role"` // "user", "assistant"
	Content   string          `json:"content"`
	ToolCalls []AIToolCall    `json:"toolCalls,omitempty"`
	Document  *AIDocumentCard `json:"document,omitempty"`
}

type AIToolCall struct {
	ID      string         `json:"id,omitempty"`
	Name    string         `json:"name"`
	Args    map[string]any `json:"args"`
	Result  any            `json:"result,omitempty"`
	Summary string         `json:"summary,omitempty"`
	Status  string         `json:"status,omitempty"` // "success", "error"
}

type AIDocumentCard struct {
	ID           string            `json:"id,omitempty"`
	Type         string            `json:"type"` // "quotation" | "contract"
	Title        string            `json:"title"`
	Counterparty string            `json:"counterparty"`
	Contact      string            `json:"contact,omitempty"`
	Currency     string            `json:"currency,omitempty"`
	Items        []AIDocumentItem  `json:"items,omitempty"`
	Terms        []string          `json:"terms,omitempty"`
	Notes        string            `json:"notes,omitempty"`
	Subtotal     string            `json:"subtotal,omitempty"`
	TaxRate      string            `json:"taxRate,omitempty"`
	TaxAmount    string            `json:"taxAmount,omitempty"`
	Total        string            `json:"total,omitempty"`
	Values       map[string]string `json:"values,omitempty"`
	HTML         string            `json:"html,omitempty"`
}

type AIAgentChatDocContext struct {
	Type         string            `json:"type"`
	Title        string            `json:"title"`
	Counterparty string            `json:"counterparty"`
	Items        []AIDocumentItem  `json:"items,omitempty"`
	Values       map[string]string `json:"values,omitempty"`
}

type AIAgentChatRequest struct {
	AccountEmail   string                 `json:"accountEmail"`
	AgentID        string                 `json:"agentId,omitempty"`
	ModelID        string                 `json:"modelId,omitempty"`
	Messages       []AIAgentChatMessage   `json:"messages"`
	ActiveDocument *AIAgentChatDocContext `json:"activeDocument,omitempty"`
}

type AIAgentChatResponse struct {
	Content   string          `json:"content"`
	ToolCalls []AIToolCall    `json:"toolCalls,omitempty"`
	Document  *AIDocumentCard `json:"document,omitempty"`
}

var (
	toolCallRegex = regexp.MustCompile("(?s)```(?:tool_call|json:tool_call)\\s*(\\{.*?\\})\\s*```")
	documentRegex = regexp.MustCompile("(?s)```(?:document|json:document)\\s*(\\{.*?\\})\\s*```")
)

// HandleAgentChat handles multi-turn conversation with an intelligent AI Agent equipped with tool calling.
func (h *AISettingsHandler) HandleAgentChat(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}

	var input AIAgentChatRequest
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	input.AccountEmail = strings.TrimSpace(input.AccountEmail)
	if input.AccountEmail == "" {
		return fiber.NewError(fiber.StatusBadRequest, "accountEmail is required")
	}
	if len(input.Messages) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "messages cannot be empty")
	}

	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, input.AccountEmail)
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "mail account not found")
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}

	// 1. Resolve AI Model
	var model mailstore.AIModelRecord
	if input.ModelID != "" {
		model, _ = h.mailDB.GetAIModel(c.UserContext(), owner, input.ModelID)
	}
	if model.ID == "" {
		binding, bindingErr := h.mailDB.GetAITaskBinding(c.UserContext(), owner, account.ID, mailstore.EmailDraftTask)
		if bindingErr == nil && binding.ModelID != "" {
			model, _ = h.mailDB.GetAIModel(c.UserContext(), owner, binding.ModelID)
		}
	}
	if model.ID == "" {
		model, err = h.mailDB.GetDefaultAIModel(c.UserContext(), owner)
	}
	if errors.Is(err, mailstore.ErrNotFound) || model.ID == "" {
		h.recordError(c.UserContext(), owner, "agent_chat", input.AccountEmail, "", "", errors.New("no AI model configured"))
		return fiber.NewError(fiber.StatusPreconditionRequired, "请先在设置中配置可用 AI 模型及 API Key")
	}
	if err != nil {
		h.recordError(c.UserContext(), owner, "agent_chat", input.AccountEmail, "", "", err)
		return fiber.ErrInternalServerError
	}

	var apiKey string
	if model.EncryptedAPIKey == "" || mailapi.DecryptJSON(model.EncryptedAPIKey, &apiKey, h.config.Encryption.Key) != nil {
		h.recordError(c.UserContext(), owner, "agent_chat", input.AccountEmail, model.Model, "", errors.New("AI model API key is not configured"))
		return fiber.NewError(fiber.StatusPreconditionRequired, "AI 模型 API Key 未配置或已失效")
	}

	// 2. Resolve Agent Persona
	var agentCustomPrompt string
	var agentName string
	if input.AgentID != "" {
		if storedAgent, err := h.mailDB.GetAIAgent(c.UserContext(), owner, input.AgentID); err == nil && storedAgent.ID != "" {
			agentName = storedAgent.Name
			agentCustomPrompt = storedAgent.Prompt
			if len(storedAgent.OutputLabels) > 0 {
				agentCustomPrompt += fmt.Sprintf("\n请重点提取并覆盖以下信息: %s", strings.Join(storedAgent.OutputLabels, "、"))
			}
		}
	}
	if agentName == "" {
		// Check if user has an agent bound to email_draft or named with document
		if binding, err := h.mailDB.GetAITaskBinding(c.UserContext(), owner, account.ID, mailstore.EmailDraftTask); err == nil && binding.AgentID != "" {
			if storedAgent, err := h.mailDB.GetAIAgent(c.UserContext(), owner, binding.AgentID); err == nil && storedAgent.ID != "" {
				agentName = storedAgent.Name
				agentCustomPrompt = storedAgent.Prompt
			}
		}
	}

	// 3. Build Base System Instructions with Tool Calling schema
	instructions := buildAgentSystemPrompt(agentName, agentCustomPrompt, input.ActiveDocument)

	// 4. Run Agent Execution Loop (up to 4 turns of tool calling)
	executedToolCalls := make([]AIToolCall, 0)
	var generatedDocument *AIDocumentCard

	historyPrompt := buildConversationTranscript(input.Messages)
	currentTurnInput := historyPrompt

	maxTurns := 4
	finalReply := ""

	for turn := 0; turn < maxTurns; turn++ {
		rawResponse, err := h.createAIResponseWithInstructions(c.UserContext(), model, apiKey, instructions, currentTurnInput, 4096)
		if err != nil {
			h.recordError(c.UserContext(), owner, "agent_chat", input.AccountEmail, model.Model, agentName, err)
			return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		}

		// Check for Tool Call
		call, hasCall := parseToolCall(rawResponse)
		if !hasCall || turn == maxTurns-1 {
			// No tool call, or reached last turn -> this is the final answer
			finalReply = rawResponse
			break
		}

		// Execute Tool
		call.ID = fmt.Sprintf("call_%d", len(executedToolCalls)+1)
		result, summary, err := h.executeTool(c.UserContext(), account.ID, call)
		if err != nil {
			call.Status = "error"
			call.Result = map[string]string{"error": err.Error()}
			call.Summary = "工具执行出错: " + err.Error()
		} else {
			call.Status = "success"
			call.Result = result
			call.Summary = summary
		}

		if call.Name == "generate_document" && call.Status == "success" {
			if doc, ok := result.(*AIDocumentCard); ok && doc != nil {
				generatedDocument = doc
			}
		}

		executedToolCalls = append(executedToolCalls, call)

		// Feed tool result back to the model for next turn
		resBytes, _ := json.Marshal(call.Result)
		currentTurnInput += fmt.Sprintf("\n\n[Agent Tool Call]: %s(%v)\n[Tool Result (%s)]: %s\n\nBased on the tool results above, continue answering the user or call another tool.", call.Name, call.Args, call.Name, string(resBytes))
	}

	// 5. Parse Final Output (extract Document Card if present)
	cleanedContent, docFromFence := extractDocumentFromResponse(finalReply)
	if docFromFence != nil {
		generatedDocument = docFromFence
	}

	if generatedDocument != nil {
		hydrateDocumentCard(generatedDocument)
	}

	return c.JSON(AIAgentChatResponse{
		Content:   strings.TrimSpace(cleanedContent),
		ToolCalls: executedToolCalls,
		Document:  generatedDocument,
	})
}

func buildAgentSystemPrompt(agentName, customPrompt string, activeDoc *AIAgentChatDocContext) string {
	var sb strings.Builder
	sb.WriteString("You are the Inbrix Intelligent Document Assistant & Business Agent (Inbrix 智能文档助手与商业助理).\n")
	sb.WriteString("You help the user search emails, extract quotation/order requirements, compute pricing & taxes accurately, and draft or revise business documents (Quotations / Sales Contracts).\n\n")

	if agentName != "" {
		sb.WriteString(fmt.Sprintf("CURRENT AGENT ROLE: %s\n", agentName))
	}
	if customPrompt != "" {
		sb.WriteString(fmt.Sprintf("SPECIAL ROLE GUIDELINES:\n%s\n\n", customPrompt))
	}

	if activeDoc != nil {
		sb.WriteString(fmt.Sprintf("ACTIVE DOCUMENT IN WORKSPACE:\n- Type: %s\n- Title: %s\n- Counterparty: %s\n- Line Items: %d items\n\n", activeDoc.Type, activeDoc.Title, activeDoc.Counterparty, len(activeDoc.Items)))
	}

	sb.WriteString(`AVAILABLE TOOLS:
1. search_emails:
   Search the user's mailbox for relevant emails (by customer name, product requirements, inquiry, order number, or quotation request).
   Arguments schema: {"query": "keywords to search"}

2. read_email:
   Read the full content and details of a specific email found in search.
   Arguments schema: {"uid": "message uid", "folder": "INBOX"}

3. calculate_pricing:
   Perform deterministic math calculation for product line items, subtotals, tax amounts, and totals with currency formatting.
   Arguments schema: {
     "items": [{"name": "product name", "qty": 10, "unit_price": 280.0}],
     "tax_rate": 0.13,
     "discount": 0
   }

4. generate_document:
   Create or update a structured quotation or sales contract.
   Arguments schema: {
     "type": "quotation" | "contract",
     "title": "document title",
     "counterparty": "customer company name",
     "contact": "contact person",
     "currency": "¥",
     "items": [{"model": "...", "description": "...", "qty": "...", "price": "...", "amount": "..."}],
     "terms": ["payment term", "delivery term", "warranty"],
     "notes": "remarks or invoice notes"
   }

TOOL CALLING PROTOCOL:
If you need to query emails, calculate pricing, or draft a document, output ONLY a tool call code fence like:
` + "```tool_call" + `
{"name": "tool_name", "arguments": {"param": "value"}}
` + "```" + `
Do NOT output commentary before or after the tool_call code fence during a tool call.

FINAL ANSWER:
When you have gathered the required information or when no tool is needed:
- Respond in natural, polite Markdown.
- If you generated or updated a document, you can either call the ` + "`generate_document`" + ` tool OR include a document code fence at the end:
` + "```document" + `
{
  "type": "quotation",
  "title": "...",
  "counterparty": "...",
  "items": [{"model": "...", "description": "...", "qty": "...", "price": "...", "amount": "..."}],
  "terms": ["..."],
  "notes": "..."
}
` + "```" + `
Always use the user's language (default Chinese). Be precise, courteous, and efficient.`)

	return sb.String()
}

func buildConversationTranscript(messages []AIAgentChatMessage) string {
	var sb strings.Builder
	for i, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role == "user" {
			sb.WriteString(fmt.Sprintf("User: %s\n\n", m.Content))
		} else {
			sb.WriteString(fmt.Sprintf("Assistant: %s\n\n", m.Content))
		}
		if i == len(messages)-1 && role == "user" {
			// Prompt assistant to proceed
			sb.WriteString("Assistant: ")
		}
	}
	return strings.TrimSpace(sb.String())
}

func parseToolCall(output string) (AIToolCall, bool) {
	match := toolCallRegex.FindStringSubmatch(output)
	if len(match) > 1 {
		var call struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Args      map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(match[1]), &call); err == nil && call.Name != "" {
			args := call.Arguments
			if args == nil {
				args = call.Args
			}
			if args == nil {
				args = make(map[string]any)
			}
			return AIToolCall{Name: call.Name, Args: args}, true
		}
	}

	// Fallback: check if the entire output is a JSON tool call
	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var call struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Args      map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(trimmed), &call); err == nil && call.Name != "" {
			args := call.Arguments
			if args == nil {
				args = call.Args
			}
			if args == nil {
				args = make(map[string]any)
			}
			return AIToolCall{Name: call.Name, Args: args}, true
		}
	}

	return AIToolCall{}, false
}

func extractDocumentFromResponse(output string) (string, *AIDocumentCard) {
	match := documentRegex.FindStringSubmatch(output)
	if len(match) > 1 {
		var card AIDocumentCard
		if err := json.Unmarshal([]byte(match[1]), &card); err == nil && card.Title != "" {
			cleaned := documentRegex.ReplaceAllString(output, "")
			return strings.TrimSpace(cleaned), &card
		}
	}
	return output, nil
}

func (h *AISettingsHandler) executeTool(ctx context.Context, accountID string, call AIToolCall) (any, string, error) {
	switch call.Name {
	case "search_emails":
		query, _ := call.Args["query"].(string)
		if query == "" {
			if q, ok := call.Args["q"].(string); ok {
				query = q
			}
		}
		return h.executeSearchEmails(ctx, accountID, query)

	case "read_email":
		uid, _ := call.Args["uid"].(string)
		if uid == "" {
			if id, ok := call.Args["email_id"].(string); ok {
				uid = id
			}
		}
		folder, _ := call.Args["folder"].(string)
		return h.executeReadEmail(ctx, accountID, folder, uid)

	case "calculate_pricing":
		return executeCalculatePricing(call.Args)

	case "generate_document":
		return executeGenerateDocument(call.Args)

	default:
		return nil, "", fmt.Errorf("unknown tool: %q", call.Name)
	}
}

func (h *AISettingsHandler) executeSearchEmails(ctx context.Context, accountID, query string) (any, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "搜索关键词不能为空", "搜索关键词为空", nil
	}

	emails, _, err := h.mailDB.SearchMessages(ctx, accountID, "", query, 6)
	if err != nil {
		return fmt.Sprintf("检索邮件出错: %v", err), "检索邮件出错", nil
	}

	if len(emails) == 0 {
		return "未找到包含该关键词的邮件。", fmt.Sprintf("检索【%s】未找到邮件", query), nil
	}

	type emailSummary struct {
		UID     string `json:"uid"`
		Folder  string `json:"folder"`
		Date    string `json:"date"`
		From    string `json:"from"`
		Subject string `json:"subject"`
		Snippet string `json:"snippet"`
	}

	list := make([]emailSummary, 0, len(emails))
	for _, e := range emails {
		snippet := e.Preview
		if snippet == "" && e.Body != "" {
			snippet = e.Body
		}
		snippetRunes := []rune(snippet)
		if len(snippetRunes) > 120 {
			snippet = string(snippetRunes[:120]) + "..."
		}
		list = append(list, emailSummary{
			UID:     e.ID,
			Folder:  e.Folder,
			Date:    e.Date.Format("2006-01-02 15:04"),
			From:    e.From,
			Subject: e.Subject,
			Snippet: snippet,
		})
	}

	summary := fmt.Sprintf("检索到 %d 封相关邮件 (关键词: %s)", len(list), query)
	return list, summary, nil
}

func (h *AISettingsHandler) executeReadEmail(ctx context.Context, accountID, folder, uid string) (any, string, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return "邮件 uid 不能为空", "邮件 uid 为空", nil
	}
	if folder == "" {
		folder = "INBOX"
	}

	msg, err := h.mailDB.GetMessage(ctx, accountID, folder, uid)
	if err != nil {
		// If not in specified folder, try search across folders by UID
		emails, _, searchErr := h.mailDB.SearchMessages(ctx, accountID, "", uid, 1)
		if searchErr == nil && len(emails) > 0 {
			msg = emails[0]
		} else {
			return "未能读取该邮件，可能已被删除或移入其他文件夹", "读取邮件失败", nil
		}
	}

	body := msg.Body
	if body == "" && msg.Preview != "" {
		body = msg.Preview
	}
	bodyRunes := []rune(body)
	if len(bodyRunes) > 1800 {
		body = string(bodyRunes[:1800]) + "\n[...内容过长已截断...]"
	}

	result := map[string]any{
		"uid":     msg.ID,
		"from":    msg.From,
		"to":      msg.To,
		"subject": msg.Subject,
		"date":    msg.Date.Format("2006-01-02 15:04"),
		"body":    body,
	}

	summary := fmt.Sprintf("已读取邮件: %s", msg.Subject)
	return result, summary, nil
}

func executeCalculatePricing(args map[string]any) (any, string, error) {
	taxRate := 0.13
	if tr, ok := args["tax_rate"].(float64); ok {
		taxRate = tr
	} else if trStr, ok := args["tax_rate"].(string); ok {
		if trVal, err := strconv.ParseFloat(strings.TrimSuffix(trStr, "%"), 64); err == nil {
			if strings.HasSuffix(trStr, "%") {
				taxRate = trVal / 100.0
			} else {
				taxRate = trVal
			}
		}
	}

	discount := 0.0
	if d, ok := args["discount"].(float64); ok {
		discount = d
	}

	type calcItem struct {
		Name      string `json:"name"`
		Qty       string `json:"qty"`
		UnitPrice string `json:"unitPrice"`
		Amount    string `json:"amount"`
	}

	subtotal := 0.0
	var calcItems []calcItem

	if rawItems, ok := args["items"].([]any); ok {
		for _, raw := range rawItems {
			itemMap, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := itemMap["name"].(string)
			if name == "" {
				name, _ = itemMap["description"].(string)
			}
			if name == "" {
				name, _ = itemMap["model"].(string)
			}

			qty := 1.0
			if q, ok := itemMap["qty"].(float64); ok {
				qty = q
			} else if qStr, ok := itemMap["qty"].(string); ok {
				qty, _ = strconv.ParseFloat(strings.TrimSpace(qStr), 64)
			}

			price := 0.0
			if p, ok := itemMap["unit_price"].(float64); ok {
				price = p
			} else if p, ok := itemMap["price"].(float64); ok {
				price = p
			} else if pStr, ok := itemMap["price"].(string); ok {
				price, _ = strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(pStr), ",", ""), 64)
			} else if pStr, ok := itemMap["unit_price"].(string); ok {
				price, _ = strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(pStr), ",", ""), 64)
			}

			lineAmount := qty * price
			subtotal += lineAmount

			calcItems = append(calcItems, calcItem{
				Name:      name,
				Qty:       fmt.Sprintf("%.0f", qty),
				UnitPrice: fmt.Sprintf("%.2f", price),
				Amount:    fmt.Sprintf("%.2f", lineAmount),
			})
		}
	}

	taxableAmount := subtotal - discount
	if taxableAmount < 0 {
		taxableAmount = 0
	}
	taxAmount := taxableAmount * taxRate
	total := taxableAmount + taxAmount

	result := map[string]any{
		"subtotal":   fmt.Sprintf("%.2f", subtotal),
		"tax_rate":   fmt.Sprintf("%.0f%%", taxRate*100),
		"tax_amount": fmt.Sprintf("%.2f", taxAmount),
		"discount":   fmt.Sprintf("%.2f", discount),
		"total":      fmt.Sprintf("%.2f", total),
		"items":      calcItems,
	}

	summary := fmt.Sprintf("精确核算完成: 含税总额 ¥%.2f (税率 %.0f%%)", total, taxRate*100)
	return result, summary, nil
}

func executeGenerateDocument(args map[string]any) (any, string, error) {
	docType, _ := args["type"].(string)
	if docType != "contract" {
		docType = "quotation"
	}
	title, _ := args["title"].(string)
	if title == "" {
		if docType == "contract" {
			title = "标准销售合同"
		} else {
			title = "商业报价单"
		}
	}
	counterparty, _ := args["counterparty"].(string)
	contact, _ := args["contact"].(string)
	currency, _ := args["currency"].(string)
	if currency == "" {
		currency = "¥"
	}
	notes, _ := args["notes"].(string)

	var items []AIDocumentItem
	subtotalVal := 0.0

	if rawItems, ok := args["items"].([]any); ok {
		for _, raw := range rawItems {
			if itemMap, ok := raw.(map[string]any); ok {
				model, _ := itemMap["model"].(string)
				desc, _ := itemMap["description"].(string)
				if desc == "" {
					desc, _ = itemMap["name"].(string)
				}
				qty, _ := itemMap["qty"].(string)
				if qty == "" {
					if qf, ok := itemMap["qty"].(float64); ok {
						qty = fmt.Sprintf("%.0f", qf)
					}
				}
				price, _ := itemMap["price"].(string)
				if price == "" {
					if pf, ok := itemMap["price"].(float64); ok {
						price = fmt.Sprintf("%.2f", pf)
					}
				}
				amount, _ := itemMap["amount"].(string)
				if amount == "" {
					q, _ := strconv.ParseFloat(qty, 64)
					p, _ := strconv.ParseFloat(strings.ReplaceAll(price, ",", ""), 64)
					if q > 0 && p > 0 {
						amount = fmt.Sprintf("%.2f", q*p)
					}
				}

				if a, err := strconv.ParseFloat(strings.ReplaceAll(amount, ",", ""), 64); err == nil {
					subtotalVal += a
				}

				items = append(items, AIDocumentItem{
					Model:       model,
					Description: desc,
					Qty:         qty,
					Price:       price,
					Amount:      amount,
				})
			}
		}
	}

	var terms []string
	if rawTerms, ok := args["terms"].([]any); ok {
		for _, t := range rawTerms {
			if ts, ok := t.(string); ok && strings.TrimSpace(ts) != "" {
				terms = append(terms, strings.TrimSpace(ts))
			}
		}
	}

	card := &AIDocumentCard{
		ID:           fmt.Sprintf("doc_%d", time.Now().UnixMilli()),
		Type:         docType,
		Title:        title,
		Counterparty: counterparty,
		Contact:      contact,
		Currency:     currency,
		Items:        items,
		Terms:        terms,
		Notes:        notes,
		Subtotal:     fmt.Sprintf("%.2f", subtotalVal),
		TaxRate:      "13%",
		TaxAmount:    fmt.Sprintf("%.2f", subtotalVal*0.13),
		Total:        fmt.Sprintf("%.2f", subtotalVal*1.13),
	}

	hydrateDocumentCard(card)

	docName := "报价单"
	if docType == "contract" {
		docName = "合同"
	}
	summary := fmt.Sprintf("已生成%s: %s (买方: %s)", docName, title, counterparty)
	return card, summary, nil
}

func hydrateDocumentCard(card *AIDocumentCard) {
	if card == nil {
		return
	}
	if card.Values == nil {
		card.Values = make(map[string]string)
	}

	isContract := card.Type == "contract"
	dateStr := time.Now().Format("2006年01月02日")

	if card.Counterparty != "" {
		card.Values["buyer_company"] = card.Counterparty
		card.Values["customer_company"] = card.Counterparty
	}
	if card.Contact != "" {
		card.Values["buyer_contact"] = card.Contact
		card.Values["customer_contact"] = card.Contact
	}
	if card.Total != "" {
		card.Values["total_amount"] = card.Total
		card.Values["contract_amount"] = card.Total
	}
	if card.Notes != "" {
		card.Values["order_note_01"] = card.Notes
	}

	if isContract {
		card.Values["contract_date"] = dateStr
		if card.Values["contract_number"] == "" {
			card.Values["contract_number"] = fmt.Sprintf("SC-%s", time.Now().Format("200601021504"))
		}
	} else {
		card.Values["issue_date"] = dateStr
		if card.Values["quote_number"] == "" {
			card.Values["quote_number"] = fmt.Sprintf("SQ-%s", time.Now().Format("200601021504"))
		}
	}

	if len(card.Terms) > 0 {
		for i, term := range card.Terms {
			if strings.Contains(term, "付款") || strings.Contains(term, "预付") {
				card.Values["payment_terms"] = term
			} else if strings.Contains(term, "交货") || strings.Contains(term, "交付") {
				card.Values["delivery_date"] = term
				card.Values["delivery_lead_time"] = term
			} else if strings.Contains(term, "质保") || strings.Contains(term, "保修") {
				card.Values["warranty_period"] = term
			} else {
				card.Values[fmt.Sprintf("special_terms_%d", i+1)] = term
			}
		}
	}
}
