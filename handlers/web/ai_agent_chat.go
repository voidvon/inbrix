package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
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
	// DocumentAction is a change to a stored document that the user confirms in
	// the UI. The agent never deletes or overwrites a saved document itself.
	DocumentAction *AIDocumentAction `json:"documentAction,omitempty"`
}

// AIDocumentAction is a proposed update or deletion of a stored quotation or
// contract. Values and Items are the complete field set to write, so applying it
// is deterministic and reviewable.
type AIDocumentAction struct {
	Kind       string            `json:"kind"` // "update" | "delete"
	DocumentID string            `json:"documentId"`
	Type       string            `json:"type,omitempty"`
	Name       string            `json:"name,omitempty"`
	Company    string            `json:"company,omitempty"`
	Values     map[string]string `json:"values,omitempty"`
	Items      []AIDocumentItem  `json:"items,omitempty"`
	Summary    string            `json:"summary,omitempty"`
}

var (
	toolCallRegex = regexp.MustCompile("(?s)```(?:tool_call|json:tool_call|toolCall)\\s*(.*?)\\s*```")
	documentRegex = regexp.MustCompile("(?s)```(?:document|json:document)\\s*(.*?)\\s*```")
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
	promptMode := agentModeConversation
	if input.ActiveDocument != nil {
		promptMode = agentModeDocument
	}
	instructions := buildAgentSystemPrompt(promptMode, agentName, agentCustomPrompt, input.ActiveDocument)

	// 4. Run Agent Execution Loop (up to 5 rounds of tool calling)
	executedToolCalls := make([]AIToolCall, 0)
	var generatedDocument *AIDocumentCard
	var documentAction *AIDocumentAction

	historyPrompt := buildConversationTranscript(input.Messages)
	currentTurnInput := historyPrompt

	// Resolve anything model-shaped the user (or the open document) mentions
	// before the first model call, so price questions are answered from the
	// user's own price list even when the model would not call the tool itself.
	prefetchTexts := make([]string, 0, len(input.Messages)+2)
	for _, message := range input.Messages {
		prefetchTexts = append(prefetchTexts, message.Content)
	}
	if input.ActiveDocument != nil {
		for _, item := range input.ActiveDocument.Items {
			prefetchTexts = append(prefetchTexts, item.Model, item.Description)
		}
	}
	priceContext, priceEntries := h.prefetchPriceContext(c.UserContext(), owner, prefetchTexts, maxPrefetchedPrices)
	if priceContext != "" {
		currentTurnInput = priceContext + "\n\n" + currentTurnInput
		executedToolCalls = append(executedToolCalls, AIToolCall{
			ID:      "call_price_prefetch",
			Name:    "lookup_price",
			Args:    map[string]any{"auto": true},
			Status:  "success",
			Summary: fmt.Sprintf("价格表预匹配 %d 条", len(priceEntries)),
		})
	}

	maxToolTurns := 5
	finalReply := ""

	for turn := 0; turn < maxToolTurns; turn++ {
		rawResponse, err := h.createAIResponseWithInstructions(c.UserContext(), model, apiKey, instructions, currentTurnInput, 4096)
		if err != nil {
			h.recordError(c.UserContext(), owner, "agent_chat", input.AccountEmail, model.Model, agentName, err)
			return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		}

		calls := parseToolCalls(rawResponse)
		if len(calls) == 0 {
			// No tool call requested; model gave its natural language response
			finalReply = rawResponse
			break
		}

		// Execute all parsed tool calls in this turn
		for _, call := range calls {
			call.ID = fmt.Sprintf("call_%d", len(executedToolCalls)+1)
			result, summary, err := h.executeTool(c.UserContext(), owner, account.ID, call, model, apiKey)
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

			if call.Status == "success" {
				if action, ok := result.(*AIDocumentAction); ok && action != nil {
					documentAction = action
				}
			}

			executedToolCalls = append(executedToolCalls, call)

			// Feed tool result back to the model for next turn
			resBytes, _ := json.Marshal(call.Result)
			currentTurnInput += fmt.Sprintf("\n\n[Agent Tool Call]: %s(%v)\n[Tool Result (%s)]: %s", call.Name, call.Args, call.Name, string(resBytes))
		}

		// If this was the last tool turn allowed, ask model for final synthesis
		if turn == maxToolTurns-1 {
			currentTurnInput += "\n\n[System]: You have completed the maximum number of tool executions. You must NOT call any more tools or output any tool_call code blocks. Based on all the gathered information and tool results above, provide your final, comprehensive, and polite response to the user now in Markdown."
			synthesisResponse, synErr := h.createAIResponseWithInstructions(c.UserContext(), model, apiKey, instructions, currentTurnInput, 4096)
			if synErr == nil && strings.TrimSpace(synthesisResponse) != "" {
				finalReply = synthesisResponse
			}
			break
		} else {
			currentTurnInput += "\n\nBased on the tool results above, continue answering the user or call another tool if needed."
		}
	}

	// 5. Parse Final Output (extract Document Card if present & sanitize residual tool blocks)
	cleanedContent, docFromFence := extractDocumentFromResponse(finalReply)
	if docFromFence != nil {
		generatedDocument = docFromFence
	}

	cleanedContent = stripResidualToolCalls(cleanedContent)

	if cleanedContent == "" {
		cleanedContent = generateFallbackResponse(executedToolCalls, generatedDocument)
	}

	if generatedDocument != nil {
		hydrateDocumentCard(generatedDocument)
	}

	return c.JSON(AIAgentChatResponse{
		Content:        strings.TrimSpace(cleanedContent),
		ToolCalls:      executedToolCalls,
		Document:       generatedDocument,
		DocumentAction: documentAction,
	})
}

// Agent prompt modes. The tool set is shared; what differs is how the agent is
// framed, because the same agent is used from the document workspace and from
// the email conversation sidebar.
const (
	agentModeDocument     = "document"
	agentModeConversation = "conversation"
)

func buildAgentSystemPrompt(mode, agentName, customPrompt string, activeDoc *AIAgentChatDocContext) string {
	var sb strings.Builder
	sb.WriteString("You are the Inbrix Intelligent Business Agent (Inbrix 智能商业助理).\n")
	if mode == agentModeConversation {
		sb.WriteString("You are assisting the user inside an email conversation (邮件会话). The email thread below is your context: the user is asking about it, and may ask you to price something, prepare a quotation, or draft a reply.\n")
		sb.WriteString("You can search the mailbox, read a message in full, look up the user's own price list, compute pricing and taxes, and draft or revise business documents (Quotations / Sales Contracts).\n\n")
	} else {
		sb.WriteString("You help the user search emails, extract quotation/order requirements, compute pricing & taxes accurately, and draft or revise business documents (Quotations / Sales Contracts).\n\n")
	}

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
   Create a structured quotation or sales contract, filled as completely as the thread allows.
   Arguments schema: {
     "type": "quotation" | "contract",
     "title": "document title",
     "counterparty": "buyer company, exactly as the thread writes it",
     "contact": "buyer contact person",
     "email": "buyer contact email",
     "phone": "buyer phone",
     "address": "buyer address",
     "currency": "USD",
     "items": [{"model": "...", "description": "...", "qty": "...", "price": "...", "amount": "..."}],
     "terms": ["payment term", "delivery term", "warranty"],
     "notes": "remarks or invoice notes",
     "values": {"customer_po": "...", "validity": "...", "lead_time": "...", "payment_terms": "...",
                "trade_term": "...", "origin": "...", "port_of_loading": "...", "remarks": "...",
                "seller_company": "...", "seller_contact": "...", "seller_email": "...",
                "seller_phone": "...", "seller_address": "...", "bank_account_name": "...",
                "bank_account_no": "...", "bank_swift": "...", "bank_name": "...",
                "bank_address": "...", "bank_country": "...", "payment_method": "...",
                "order_note_1": "...", "order_note_2": "...", "order_note_3": "...", "order_note_4": "..."}
   }

5. list_documents:
   Find the user's saved quotations and contracts (by name, customer, or type). Call this before reading, updating, or deleting a document, so you use a real document id.
   Arguments schema: {"type": "quotation" | "contract" (optional), "query": "name or customer keyword" (optional)}

6. read_document:
   Read one saved document's structured fields (customer, amounts, terms) and line items.
   Arguments schema: {"id": "document id from list_documents"}

7. update_document:
   Propose a change to a saved document. This only proposes: the user confirms it in the UI. Read the document first with read_document, then send the fields you are changing as values; send items only when the product lines change. Include just the fields you change, and give concrete values.
   Arguments schema: {
     "id": "document id",
     "instruction": "one-line description of the change, shown to the user",
     "values": {"delivery_time": "35 天", "customer_contact": "李经理", "notes": "含运费"},
     "items": [{"model": "...", "description": "...", "qty": "...", "price": "...", "amount": "..."}]
   }

8. delete_document:
   Propose deleting a saved document. This only proposes: the user confirms it in the UI.
   Arguments schema: {"id": "document id"}

9. lookup_price:
   Look up the user's own price list for authoritative product prices, units, currencies, tax rates, minimum order quantities, and lead times.
   Arguments schema: {"query": "model / SKU / product name keywords", "qty": 10}
   Returns: [{"sku", "name", "unit", "currency", "unit_price", "price_text", "tax_rate", "moq", "lead_time", "list_name"}]

PRICING RULES (ABSOLUTE, NEVER VIOLATE):
- Every price, tax rate, unit, MOQ, or lead time you output MUST come from a lookup_price result, or from a ` + "`【价格表匹配】`" + ` block supplied with the request.
- NEVER invent, guess, or recall a price from memory. When lookup_price returns no match, say plainly that the price list has no entry for that model, ask the user to confirm the model number or add it to the price list, and do NOT propose a number.
- Treat unit_price / tax_rate / currency / lead_time from those sources as authoritative and quote them unchanged.
- Use calculate_pricing for arithmetic (line totals, tax, discounts) on prices that lookup_price returned.
- A ` + "`【价格表匹配】`" + ` block, when present, is the user's own price list already resolved for this request. Use it directly and do not re-look-up the same models.

WHEN TO LOOK UP PRICES:
- Any question about price, cost, quote, quotation, discount, lead time, MOQ, or availability.
- Whenever the user asks you to draft a reply, an offer, or a quotation that will contain numbers, even if they did not say the word "price".
- Before quoting, check the email thread for model numbers or product names and look those up; the mailbox context is where the customer's request lives.

DOCUMENT RULES (QUOTATIONS & CONTRACTS):
- The user's saved quotations and contracts live in the document library. Use list_documents to find one and read_document to see its current fields and line items.
- generate_document produces a draft card that the user saves. To change an ALREADY SAVED document, call update_document with its id and a concrete instruction; never regenerate the document and call it an update.
- update_document and delete_document only PROPOSE the change: the user confirms it in the UI. After calling one, tell the user exactly what you proposed and that they need to confirm it. Never claim a document was changed or deleted by you.
- Read a document before updating it when you are not certain of its current values, and put concrete values (names, quantities, prices, dates, terms) in the instruction.
- Deleting is irreversible and needs the user's explicit intent. If the request is ambiguous, ask which document first.
- Every number written into a quotation or contract follows the pricing rules below.

DOCUMENT COMPLETENESS (QUOTATIONS & CONTRACTS):
- A document is filled from the WHOLE thread, not from the user's last sentence. Re-read the conversation before calling generate_document and collect: the buyer's full company name, contact person, email, phone and address; the customer's PO or reference number; the currency; every product model, description and quantity discussed; payment terms, lead time, validity and any delivery or packing note.
- Buyer details come from the thread itself. The sender's signature usually carries the company name, phone and address, and the From/Cc headers carry the email address. Copy the company name exactly as it is written. Never leave the buyer blank when anyone in the thread names the company.
- Line items: ONE ROW PER PRODUCT. If the mail lists or tabulates products, reproduce every row in the same order with model, description, quantity and unit price, and let the amount be quantity x price. Never collapse several products into one summary row, and never drop a row the thread discusses.
- Prices still follow the pricing rules: when lookup_price has no entry for a model, leave its price and amount empty and say so. Quantities, descriptions, buyer details and terms are never left empty when the thread has them.
- Everything without a dedicated argument goes into "values", keyed by these concept ids: customer_po, validity, commercial_validity, lead_time, payment_terms, trade_term, origin, port_of_loading, remarks, notes, item_model, contract_title, seller_company, seller_contact, seller_email, seller_phone, seller_address, bank_account_name, bank_account_no, bank_swift, bank_name, bank_address, bank_country, payment_method, order_note_1, order_note_2, order_note_3, order_note_4. Fill seller and bank details only when the thread or the user's own signature states them; never invent a bank account.
- A field the document needs but the thread does not contain stays empty AND is named in your reply, so the user can supply it. Do not silently hand over a half-empty document.

TOOL CALLING PROTOCOL:
If you need to query emails, read email details, calculate pricing, or draft a document, output one or more tool call code fences like:
` + "```tool_call" + `
{"name": "tool_name", "arguments": {"param": "value"}}
` + "```" + `
You may output multiple ` + "```tool_call```" + ` blocks in a single turn if multiple actions are needed (e.g. read an email and search for another query).
Do NOT output commentary before or after the tool_call code fence during a tool call.

EFFICIENCY & TOOL SELECTION GUIDELINES:
- When search_emails returns candidate emails, review their subject and snippet. If any candidate appears relevant, call read_email with its uid to read the full body rather than repeatedly searching with slight keyword variations.
- Avoid looping on search_emails with redundant queries.

FINAL ANSWER:
When you have gathered the required information or when no further tool is needed:
- Respond in natural, polite, and helpful Markdown.
- Synthesize all findings from your tool calls clearly for the user.
- Do NOT output any ` + "```tool_call```" + ` blocks when providing your final answer.
- If you generated or updated a document, you can either call the ` + "`generate_document`" + ` tool OR include a document code fence at the end:
` + "```document" + `
{
  "type": "quotation",
  "title": "...",
  "counterparty": "...",
  "contact": "...",
  "email": "...",
  "phone": "...",
  "address": "...",
  "currency": "...",
  "items": [{"model": "...", "description": "...", "qty": "...", "price": "...", "amount": "..."}],
  "terms": ["..."],
  "notes": "...",
  "values": {"customer_po": "...", "lead_time": "...", "payment_terms": "..."}
}
` + "```" + `
Always use the user's language (default Chinese). Be precise, courteous, and efficient.`)

	return sb.String()
}

func buildConversationTranscript(messages []AIAgentChatMessage) string {
	var sb strings.Builder
	for i, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		content := stripResidualToolCalls(m.Content)
		if role == "user" {
			sb.WriteString(fmt.Sprintf("User: %s\n\n", m.Content))
		} else {
			if content != "" {
				sb.WriteString(fmt.Sprintf("Assistant: %s\n\n", content))
			} else if len(m.ToolCalls) > 0 {
				sb.WriteString("Assistant: [Executed tool calls to assist user]\n\n")
			}
		}
		if i == len(messages)-1 && role == "user" {
			// Prompt assistant to proceed
			sb.WriteString("Assistant: ")
		}
	}
	return strings.TrimSpace(sb.String())
}

func parseToolCalls(output string) []AIToolCall {
	var calls []AIToolCall

	// 1. Match all ```tool_call, ```json:tool_call, ```toolCall code fences
	matches := toolCallRegex.FindAllStringSubmatch(output, -1)
	for _, match := range matches {
		if len(match) > 1 {
			fenceContent := strings.TrimSpace(match[1])
			if fenceContent == "" {
				continue
			}
			calls = append(calls, parseToolCallsFromJSON(fenceContent)...)
		}
	}

	if len(calls) > 0 {
		return calls
	}

	// 2. Fallback: check if the entire output (or code block) is raw JSON tool call
	trimmed := strings.TrimSpace(output)
	return parseToolCallsFromJSON(trimmed)
}

func parseToolCallsFromJSON(raw string) []AIToolCall {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		var single struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Args      map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(raw), &single); err == nil && single.Name != "" {
			args := single.Arguments
			if args == nil {
				args = single.Args
			}
			if args == nil {
				args = make(map[string]any)
			}
			return []AIToolCall{{Name: single.Name, Args: args}}
		}
	} else if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		var list []struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Args      map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(raw), &list); err == nil && len(list) > 0 {
			var res []AIToolCall
			for _, item := range list {
				if item.Name != "" {
					args := item.Arguments
					if args == nil {
						args = item.Args
					}
					if args == nil {
						args = make(map[string]any)
					}
					res = append(res, AIToolCall{Name: item.Name, Args: args})
				}
			}
			return res
		}
	} else {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start != -1 && end > start {
			sub := raw[start : end+1]
			var single struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
				Args      map[string]any `json:"args"`
			}
			if err := json.Unmarshal([]byte(sub), &single); err == nil && single.Name != "" {
				args := single.Arguments
				if args == nil {
					args = single.Args
				}
				if args == nil {
					args = make(map[string]any)
				}
				return []AIToolCall{{Name: single.Name, Args: args}}
			}
		}
	}

	return nil
}

func parseToolCall(output string) (AIToolCall, bool) {
	calls := parseToolCalls(output)
	if len(calls) > 0 {
		return calls[0], true
	}
	return AIToolCall{}, false
}

func stripResidualToolCalls(content string) string {
	cleaned := toolCallRegex.ReplaceAllString(content, "")
	trimmed := strings.TrimSpace(cleaned)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var call struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(trimmed), &call); err == nil && call.Name != "" {
			return ""
		}
	}
	return strings.TrimSpace(cleaned)
}

func generateFallbackResponse(executedToolCalls []AIToolCall, doc *AIDocumentCard) string {
	if doc != nil {
		docName := "报价单"
		if doc.Type == "contract" {
			docName = "合同"
		}
		return fmt.Sprintf("已为您完成相关信息查询与核对，并成功生成了%s《%s》（买方：%s）。请在下方卡片中查阅或在文档编辑器中打开。",
			docName, doc.Title, doc.Counterparty)
	}
	if len(executedToolCalls) > 0 {
		var sb strings.Builder
		sb.WriteString("已为您完成邮件检索与数据核对。工具执行记录如下：\n\n")
		for i, tc := range executedToolCalls {
			if tc.Summary != "" {
				sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, tc.Summary))
			}
		}
		sb.WriteString("\n如需进一步查看邮件正文详情或起草正式文档，请随时告诉我。")
		return sb.String()
	}
	return "已为您处理完成。"
}

func extractDocumentFromResponse(output string) (string, *AIDocumentCard) {
	match := documentRegex.FindStringSubmatch(output)
	if len(match) > 1 {
		rawDoc := strings.TrimSpace(match[1])
		var card AIDocumentCard
		if err := json.Unmarshal([]byte(rawDoc), &card); err == nil && (card.Title != "" || card.Type != "") {
			cleaned := documentRegex.ReplaceAllString(output, "")
			return strings.TrimSpace(cleaned), &card
		}
	}
	return output, nil
}

func parseStringArg(args map[string]any, keys ...string) string {
	if args == nil {
		return ""
	}
	for _, k := range keys {
		if val, exists := args[k]; exists && val != nil {
			switch v := val.(type) {
			case string:
				if s := strings.TrimSpace(v); s != "" {
					return s
				}
			case float64:
				return strconv.FormatInt(int64(v), 10)
			case int:
				return strconv.Itoa(v)
			case int64:
				return strconv.FormatInt(v, 10)
			default:
				if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" && s != "<nil>" {
					return s
				}
			}
		}
	}
	return ""
}

func (h *AISettingsHandler) executeTool(ctx context.Context, owner, accountID string, call AIToolCall, model mailstore.AIModelRecord, apiKey string) (any, string, error) {
	switch call.Name {
	case "search_emails":
		query := parseStringArg(call.Args, "query", "q", "keyword", "keywords")
		return h.executeSearchEmails(ctx, accountID, query)

	case "read_email":
		uid := parseStringArg(call.Args, "uid", "email_id", "id", "message_id")
		folder := parseStringArg(call.Args, "folder", "mailbox")
		return h.executeReadEmail(ctx, accountID, folder, uid)

	case "lookup_price":
		return h.executeLookupPrice(ctx, owner, call.Args)

	case "list_documents":
		return h.executeListDocuments(ctx, owner, call.Args)

	case "read_document":
		return h.executeReadDocument(ctx, owner, call.Args)

	case "update_document":
		return h.executeUpdateDocument(ctx, owner, call.Args, model, apiKey)

	case "delete_document":
		return h.executeDeleteDocument(ctx, owner, call.Args)

	case "calculate_pricing":
		return executeCalculatePricing(call.Args)

	case "generate_document":
		return executeGenerateDocument(call.Args)

	default:
		return nil, "", fmt.Errorf("unknown tool: %q", call.Name)
	}
}

// maxPrefetchedPrices bounds how many price rows are resolved before the agent
// runs, so a long thread cannot turn into a large prompt.
const maxPrefetchedPrices = 8

var (
	// Model numbers look like DN50-BV, SN-100, A-1: letters, digits, and the
	// separators part numbers actually use.
	priceCandidatePattern = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._/-]*`)
	priceQuotedPattern    = regexp.MustCompile(`["“”'\x60]([^"“”'\x60]{2,40})["“”'\x60]`)
)

func priceCandidateHasLetter(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func priceCandidateHasDigit(value string) bool {
	for _, r := range value {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// priceLookupCandidates pulls model-number-shaped tokens and quoted product
// names out of what the user just sent. Resolving these before the model runs
// means the agent starts with the user's own prices even when it would not have
// thought to call lookup_price itself.
func priceLookupCandidates(texts []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, limit)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || len(out) >= limit {
			return
		}
		key := strings.ToLower(value)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, value)
	}
	for _, text := range texts {
		for _, match := range priceQuotedPattern.FindAllStringSubmatch(text, -1) {
			add(match[1])
		}
		for _, token := range priceCandidatePattern.FindAllString(text, -1) {
			if len(token) < 3 || len(token) > 40 {
				continue
			}
			if !priceCandidateHasLetter(token) || !priceCandidateHasDigit(token) {
				continue
			}
			add(token)
		}
	}
	return out
}

// prefetchPriceContext resolves candidates against the user's price lists and
// renders the matches as an authoritative block for the prompt.
func (h *AISettingsHandler) prefetchPriceContext(ctx context.Context, owner string, texts []string, limit int) (string, []mailstore.PriceLookupEntry) {
	if h == nil || h.mailDB == nil || strings.TrimSpace(owner) == "" || limit <= 0 {
		return "", nil
	}
	candidates := priceLookupCandidates(texts, limit)
	if len(candidates) == 0 {
		return "", nil
	}
	seen := make(map[string]bool)
	entries := make([]mailstore.PriceLookupEntry, 0, limit)
	for _, candidate := range candidates {
		found, err := h.mailDB.LookupPrices(ctx, owner, candidate, 3)
		if err != nil {
			continue
		}
		for _, entry := range found {
			if seen[entry.RowID] {
				continue
			}
			seen[entry.RowID] = true
			entries = append(entries, entry)
			if len(entries) >= limit {
				break
			}
		}
		if len(entries) >= limit {
			break
		}
	}
	if len(entries) == 0 {
		return "", nil
	}
	return formatPriceContext(entries), entries
}

// formatPriceContext renders matches compactly. It is deliberately explicit
// about provenance so the model treats the numbers as facts, not suggestions.
func formatPriceContext(entries []mailstore.PriceLookupEntry) string {
	var sb strings.Builder
	sb.WriteString("【价格表匹配】以下条目来自用户自己的价格表，是权威价格；回答价格、报价、交期、起订量时必须直接使用，不得改写或另编：\n")
	for _, entry := range entries {
		parts := make([]string, 0, 8)
		if entry.Model != "" {
			parts = append(parts, "型号 "+entry.Model)
		}
		if entry.Name != "" {
			parts = append(parts, "名称 "+entry.Name)
		}
		if entry.PriceText != "" {
			parts = append(parts, "单价 "+entry.PriceText)
		}
		if entry.Currency != "" {
			parts = append(parts, "币种 "+entry.Currency)
		}
		if entry.Unit != "" {
			parts = append(parts, "单位 "+entry.Unit)
		}
		if entry.TaxRate > 0 {
			parts = append(parts, fmt.Sprintf("税率 %.4g", entry.TaxRate))
		}
		if entry.MOQ != "" {
			parts = append(parts, "起订量 "+entry.MOQ)
		}
		if entry.LeadTime != "" {
			parts = append(parts, "交期 "+entry.LeadTime)
		}
		if entry.ListName != "" {
			parts = append(parts, "来源 "+entry.ListName)
		}
		sb.WriteString("- " + strings.Join(parts, "；") + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// aiDocumentSummary is one entry in the agent's document list.
type aiDocumentSummary struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Company   string `json:"company,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// maxDocumentReadChars bounds how much of a document body is handed to the
// model. Canvas payloads are mostly layout; the structured fields are what
// matters, so a truncated read is still useful.
const maxDocumentReadChars = 20000

func parseDocumentIDArg(args map[string]any) string {
	return parseStringArg(args, "id", "documentId", "document_id", "doc_id", "uid")
}

// parseStringMapArg reads a flat object argument such as {"delivery_time": "35 天"}.
func parseStringMapArg(args map[string]any, keys ...string) map[string]string {
	for _, key := range keys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		object, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out := make(map[string]string, len(object))
		for field, value := range object {
			field = strings.TrimSpace(field)
			if field == "" || value == nil {
				continue
			}
			text := strings.TrimSpace(fmt.Sprintf("%v", value))
			if text != "" {
				out[field] = text
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// parseItemsArg reads a line-item array argument.
func parseItemsArg(args map[string]any, keys ...string) []AIDocumentItem {
	for _, key := range keys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		list, ok := raw.([]any)
		if !ok || len(list) == 0 {
			continue
		}
		items := make([]AIDocumentItem, 0, len(list))
		for _, entry := range list {
			object, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			text := func(fields ...string) string {
				for _, field := range fields {
					if value, ok := object[field]; ok && value != nil {
						if trimmed := strings.TrimSpace(fmt.Sprintf("%v", value)); trimmed != "" {
							return trimmed
						}
					}
				}
				return ""
			}
			item := AIDocumentItem{
				Model:       text("model", "sku", "name"),
				Description: text("description", "desc", "item"),
				Qty:         text("qty", "quantity"),
				Price:       text("price", "unit_price", "unitPrice"),
				Amount:      text("amount", "total"),
			}
			if !itemIsEmpty(item) {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

// executeListDocuments finds stored quotations/contracts by name or customer.
func (h *AISettingsHandler) executeListDocuments(ctx context.Context, owner string, args map[string]any) (any, string, error) {
	docType := strings.ToLower(parseStringArg(args, "type", "documentType", "document_type"))
	if docType != "quotation" && docType != "contract" {
		docType = ""
	}
	query := strings.ToLower(strings.TrimSpace(parseStringArg(args, "query", "q", "keyword", "name", "company")))
	records, err := h.mailDB.ListDocuments(ctx, owner, docType)
	if err != nil {
		return fmt.Sprintf("读取文档列表出错: %v", err), "读取文档列表出错", nil
	}
	list := make([]aiDocumentSummary, 0, len(records))
	for _, record := range records {
		if query != "" && !strings.Contains(strings.ToLower(record.Name), query) && !strings.Contains(strings.ToLower(record.Company), query) {
			continue
		}
		list = append(list, aiDocumentSummary{
			ID: record.ID, Type: record.Type, Name: record.Name, Company: record.Company,
			UpdatedAt: record.UpdatedAt.Format("2006-01-02 15:04"),
		})
		if len(list) >= 20 {
			break
		}
	}
	if len(list) == 0 {
		return "没有找到匹配的报价单或合同。", "未找到匹配文档", nil
	}
	return list, fmt.Sprintf("找到 %d 份文档", len(list)), nil
}

// executeReadDocument returns a document's structured fields and line items so
// the agent can reason about its content before changing it.
func (h *AISettingsHandler) executeReadDocument(ctx context.Context, owner string, args map[string]any) (any, string, error) {
	id := parseDocumentIDArg(args)
	if id == "" {
		return "缺少文档 id。请先调用 list_documents 获取。", "缺少文档 id", nil
	}
	record, err := h.mailDB.GetDocument(ctx, owner, id)
	if err != nil || record == nil {
		return "未找到该文档，可能已被删除。", "未找到文档", nil
	}
	values, items, ok := canvasDocumentContent(record.HTML)
	payload := map[string]any{
		"id": record.ID, "type": record.Type, "name": record.Name, "company": record.Company,
		"updated_at": record.UpdatedAt.Format("2006-01-02 15:04"),
	}
	if ok {
		payload["fields"] = values
		payload["items"] = items
	} else {
		payload["fields"] = map[string]string{}
		payload["items"] = []AIDocumentItem{}
		payload["note"] = "该文档不是结构化模板文档，无法按字段读取或修改。"
	}
	return payload, fmt.Sprintf("已读取文档《%s》", record.Name), nil
}

// executeUpdateDocument turns an instruction into concrete field/item changes on
// a stored document. It proposes the change; the user applies it in the UI.
func (h *AISettingsHandler) executeUpdateDocument(ctx context.Context, owner string, args map[string]any, model mailstore.AIModelRecord, apiKey string) (any, string, error) {
	id := parseDocumentIDArg(args)
	if id == "" {
		return "缺少文档 id。请先调用 list_documents 获取。", "缺少文档 id", nil
	}
	instruction := strings.TrimSpace(parseStringArg(args, "instruction", "change", "request", "content"))
	explicitValues := parseStringMapArg(args, "values", "fields", "updates", "changes")
	explicitItems := parseItemsArg(args, "items", "line_items", "lineItems")
	if instruction == "" && len(explicitValues) == 0 && len(explicitItems) == 0 {
		return "缺少修改要求：请给出 instruction，或直接给出要改的 values / items。", "缺少修改要求", nil
	}
	record, err := h.mailDB.GetDocument(ctx, owner, id)
	if err != nil || record == nil {
		return "未找到该文档，可能已被删除。", "未找到文档", nil
	}
	values, items, ok := canvasDocumentContent(record.HTML)
	if !ok {
		return "该文档不是结构化模板文档，无法安全地按字段修改。请在「报价单与合同」页面用编辑器直接修改。", "文档不支持结构化修改", nil
	}
	// The agent usually knows the exact fields (it read the document first), so an
	// explicit update is applied directly. Only when it sends a bare instruction
	// does the server ask a model to resolve it into fields.
	updates, updatedItems := explicitValues, explicitItems
	if len(updates) == 0 && len(updatedItems) == 0 {
		// Resolving a bare instruction needs a model; explicit fields do not.
		if model.ID == "" || apiKey == "" {
			return "请直接给出要修改的字段（values），当前没有可用的 AI 模型来解析自然语言要求。", "缺少 AI 模型", nil
		}
		variables := make([]aiVariableItem, 0, len(values))
		for _, key := range documentValueKeys(values) {
			variables = append(variables, aiVariableItem{ConceptID: key, Label: key, CurrentValue: values[key]})
		}
		instructions, prompt := buildDocumentVariablesPrompt(aiDocumentInput{
			DocumentType: record.Type,
			Title:        record.Name,
			Instruction:  instruction,
			CurrentHTML:  record.HTML,
			Variables:    variables,
		})
		body, err := h.createAIResponseWithInstructions(ctx, model, apiKey, instructions, prompt, 4096)
		if err != nil {
			return fmt.Sprintf("解析修改要求出错: %v", err), "解析修改要求出错", nil
		}
		resolved, resolvedItems, err := parseAIDocumentUpdateResponse(body)
		if err != nil {
			return fmt.Sprintf("模型返回的修改内容无法解析: %v", err), "修改内容无法解析", nil
		}
		updates, updatedItems = resolved, resolvedItems
	}
	changed := make([]string, 0, len(updates))
	merged := make(map[string]string, len(values)+len(updates))
	for key, value := range values {
		merged[key] = value
	}
	for key, value := range updates {
		if strings.TrimSpace(merged[key]) != strings.TrimSpace(value) {
			changed = append(changed, key)
		}
		merged[key] = value
	}
	sort.Strings(changed)
	if len(updatedItems) > 0 {
		items = updatedItems
		changed = append(changed, fmt.Sprintf("items(%d)", len(updatedItems)))
	}
	if len(changed) == 0 {
		return fmt.Sprintf("根据要求没有解析出任何变更，请把修改内容说得更具体一些。"), "没有解析出变更", nil
	}
	action := &AIDocumentAction{
		Kind:       "update",
		DocumentID: record.ID,
		Type:       record.Type,
		Name:       record.Name,
		Company:    record.Company,
		Values:     merged,
		Items:      items,
		Summary:    "将修改：" + strings.Join(changed, ", "),
	}
	return action, fmt.Sprintf("已生成《%s》的修改方案（%d 处）", record.Name, len(changed)), nil
}

// executeDeleteDocument proposes removing a document; the user confirms it.
func (h *AISettingsHandler) executeDeleteDocument(ctx context.Context, owner string, args map[string]any) (any, string, error) {
	id := parseDocumentIDArg(args)
	if id == "" {
		return "缺少文档 id。请先调用 list_documents 获取。", "缺少文档 id", nil
	}
	record, err := h.mailDB.GetDocument(ctx, owner, id)
	if err != nil || record == nil {
		return "未找到该文档，可能已被删除。", "未找到文档", nil
	}
	action := &AIDocumentAction{
		Kind:       "delete",
		DocumentID: record.ID,
		Type:       record.Type,
		Name:       record.Name,
		Company:    record.Company,
		Summary:    fmt.Sprintf("将删除《%s》", record.Name),
	}
	return action, fmt.Sprintf("等待确认删除《%s》", record.Name), nil
}

// aiPriceHit is one authoritative price handed to the model by lookup_price.
type aiPriceHit struct {
	SKU       string  `json:"sku"`
	Name      string  `json:"name"`
	Unit      string  `json:"unit,omitempty"`
	Currency  string  `json:"currency,omitempty"`
	UnitPrice float64 `json:"unit_price"`
	PriceText string  `json:"price_text"`
	TaxRate   float64 `json:"tax_rate,omitempty"`
	MOQ       string  `json:"moq,omitempty"`
	LeadTime  string  `json:"lead_time,omitempty"`
	ListName  string  `json:"list_name"`
}

// executeLookupPrice resolves a model or product name against the user's price
// lists. It is the only way the agent can obtain a price, and an empty result
// is reported as such so the model has no room to invent one.
func (h *AISettingsHandler) executeLookupPrice(ctx context.Context, owner string, args map[string]any) (any, string, error) {
	query := parseStringArg(args, "query", "model", "sku", "keyword", "product", "name")
	if strings.TrimSpace(query) == "" {
		return "查询关键词不能为空", "查询关键词为空", nil
	}
	limit := 5
	if value, ok := args["limit"].(float64); ok && value > 0 {
		limit = int(value)
	}
	entries, err := h.mailDB.LookupPrices(ctx, owner, query, limit)
	if err != nil {
		return fmt.Sprintf("查询价格表出错: %v", err), "查询价格表出错", nil
	}
	if len(entries) == 0 {
		message := fmt.Sprintf("价格表中没有匹配【%s】的条目。请告知用户价格表未收录该型号，请其确认型号或先录入价格表；不要推测价格。", query)
		return message, fmt.Sprintf("价格表中未找到【%s】", query), nil
	}

	hits := make([]aiPriceHit, 0, len(entries))
	for _, entry := range entries {
		hits = append(hits, aiPriceHit{
			SKU:       entry.Model,
			Name:      entry.Name,
			Unit:      entry.Unit,
			Currency:  entry.Currency,
			UnitPrice: entry.Price,
			PriceText: entry.PriceText,
			TaxRate:   entry.TaxRate,
			MOQ:       entry.MOQ,
			LeadTime:  entry.LeadTime,
			ListName:  entry.ListName,
		})
	}
	payload := map[string]any{
		"matches": hits,
		"note":    "These prices are authoritative and come from the user's own price list. Quote them unchanged.",
	}
	if qty := parseStringArg(args, "qty", "quantity"); qty != "" {
		payload["requested_qty"] = qty
	}
	return payload, fmt.Sprintf("价格表匹配到 %d 条（关键词: %s）", len(hits), query), nil
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
	counterparty := parseStringArg(args, "counterparty", "buyer_company", "customer_company", "buyer", "customer", "company")
	contact := parseStringArg(args, "contact", "buyer_contact", "customer_contact", "contact_person")
	currency := parseStringArg(args, "currency")
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
	// Everything the agent found in the thread that has no dedicated argument:
	// the buyer's coordinates, the customer's PO, commercial terms, and any other
	// field the documents read by concept id.
	mergeDocumentValueArgs(card, args)

	docName := "报价单"
	if docType == "contract" {
		docName = "合同"
	}
	summary := fmt.Sprintf("已生成%s: %s (买方: %s)", docName, title, counterparty)
	return card, summary, nil
}

// mergeDocumentValueArgs fills the rest of a generated document from the agent's
// own field map.
//
// The three headline arguments cover a title, a company and a contact; a real
// quotation also carries the buyer's address, the customer's PO, payment and
// delivery terms, and the seller's own details. Those arrive either as the
// buyer's headline email/phone/address or in a generic "values" map keyed by the
// concept ids the canvas documents read. Explicit values land last so the
// agent's reading of the thread wins over anything derived from the headline
// arguments.
func mergeDocumentValueArgs(card *AIDocumentCard, args map[string]any) {
	if card == nil || args == nil {
		return
	}
	if card.Values == nil {
		card.Values = make(map[string]string)
	}
	set := func(key, value string) {
		if value = strings.TrimSpace(value); value != "" {
			card.Values[key] = value
		}
	}
	// Both spellings exist in the templates: quotations read customer_*, contracts
	// read buyer_* (and fall back to customer_*). Write both so the same document
	// renders correctly whichever type the user keeps it as.
	if email := parseStringArg(args, "email", "buyer_email", "customer_email", "contact_email"); email != "" {
		set("buyer_email", email)
		set("customer_email", email)
	}
	if phone := parseStringArg(args, "phone", "buyer_phone", "customer_phone", "contact_phone"); phone != "" {
		set("buyer_phone", phone)
		set("customer_phone", phone)
	}
	if address := parseStringArg(args, "address", "buyer_address", "customer_address"); address != "" {
		set("buyer_address", address)
		set("customer_address", address)
	}
	for key, value := range parseDocumentValueMap(args["values"]) {
		set(key, value)
	}
}

// parseDocumentValueMap normalizes a JSON field map to strings. Numbers are
// decoded as float64, so an unformatted id or amount must not arrive as "1e+06".
func parseDocumentValueMap(value any) map[string]string {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		switch typed := item.(type) {
		case string:
			out[key] = strings.TrimSpace(typed)
		case float64:
			out[key] = strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			if typed {
				out[key] = "true"
			}
		}
	}
	return out
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
	// The contract's amount block reads subtotal and total separately; without it
	// a multi-row document leaves that line blank.
	if card.Subtotal != "" {
		card.Values["subtotal"] = card.Subtotal
	}
	// The quotation prints the currency it was written in; the card's currency is
	// otherwise dropped when the document is rebuilt on the client.
	if card.Currency != "" {
		card.Values["currency"] = card.Currency
	}
	if card.Notes != "" {
		card.Values["notes"] = card.Notes
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
				// The quotation's line is "Lead Time", not "delivery date".
				card.Values["lead_time"] = term
			} else if strings.Contains(term, "质保") || strings.Contains(term, "保修") {
				card.Values["warranty_period"] = term
			} else {
				card.Values[fmt.Sprintf("special_terms_%d", i+1)] = term
			}
		}
	}
}
