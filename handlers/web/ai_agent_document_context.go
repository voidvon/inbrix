package web

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"inbrix/mailstore"
)

type agentSessionExecution struct {
	turn   mailstore.AIChatTurn
	memory mailstore.AIChatContext
}
type agentDocumentContextKey struct{}

func hasCurrentDocument(doc *AIAgentChatDocContext, kind string) bool {
	return doc != nil && !doc.NewDraft && doc.Type == kind &&
		(doc.ID != "" || doc.Title != "" || doc.Counterparty != "" || len(doc.Values) > 0 || len(doc.Items) > 0 || len(doc.Terms) > 0 || doc.Notes != "" || doc.SavedDocumentMissing || doc.StructuredContentUnavailable)
}

// Continuing a draft uses the same atomic commands as the editor. A model's
// partial re-generation must never become a replacement business snapshot.
func (h *AISettingsHandler) continueAgentDocument(ctx context.Context, owner string, doc *AIAgentChatDocContext, args map[string]any) (any, string, error) {
	if doc.SavedDocumentMissing || doc.StructuredContentUnavailable {
		return nil, "", fmt.Errorf("当前文档已删除或无法读取，不能用生成结果替换；请先检查文档")
	}
	if doc.Revision > 0 {
		return nil, "", fmt.Errorf("当前文档已保存，请使用 update_document，id=%s，baseRevision=%d；不得重新生成覆盖原内容", doc.ID, doc.Revision)
	}
	for key := range args {
		if key != "type" && key != "commands" {
			return nil, "", fmt.Errorf("继续当前草稿只能提供 type 和 commands；请用增量命令修改，未提及的字段和明细必须保留")
		}
	}
	encoded, err := json.Marshal(map[string]any{"commands": args["commands"]})
	if err != nil {
		return nil, "", err
	}
	commands, err := decodeDocumentCommands(string(encoded))
	if err != nil {
		return nil, "", fmt.Errorf("继续当前草稿需要非空 commands，不能替换整份报价单: %w", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, "", err
	}
	var card AIDocumentCard
	if err := json.Unmarshal(raw, &card); err != nil {
		return nil, "", err
	}
	hydrateDocumentCard(&card)
	// Terms and notes have now been projected into Values. Do not reapply old
	// headline text over the user's subsequent set_fields command.
	card.Terms = nil
	originalItems := append([]AIDocumentItem{}, card.Items...)
	payload := map[string]any{"data": map[string]any{"main": []any{map[string]any{"value": ""}}}}
	if err := setCanvasBusiness(payload, card.Values, card.Items); err != nil {
		return nil, "", err
	}
	body, _ := json.Marshal(payload)
	updates, items, err := applyDocumentCommands(canvasDocumentPrefix+string(body), commands)
	if err != nil {
		return nil, "", err
	}
	for key, value := range updates {
		card.Values[key] = value
	}
	if items != nil {
		card.Items = items
	}
	card.Counterparty = card.Values["customer_company"]
	card.Contact = card.Values["customer_contact"]
	card.Notes = card.Values["notes"]
	card.Currency = card.Values["currency"]
	if _, changed := updates["currency"]; changed {
		if err := validateDocumentCurrencyChange(doc.Values, card.Values, card.Items); err != nil {
			return nil, "", err
		}
	}
	if err := h.finishBusinessDocument(ctx, owner, &card, originalItems); err != nil {
		return nil, "", err
	}
	return &card, generatedDocumentSummary(&card), nil
}

// Only explicit fresh-document requests detach the current draft. Merely asking
// to draft/generate a contract can mean converting the current quotation.
var newDocumentIntent = regexp.MustCompile(`^\s*(?:(?:请|帮我|我想|我要|现在|再)\s*)*(?:新建|全新|空白|另做).*(?:报价|合同|协议)|(?i:^\s*(?:please\s+)?(?:(?:create|start|make|draft)\s+)?(?:a\s+)?(?:new|another|fresh|blank)\s+(?:quotation|quote|contract|agreement)\b)`)
var existingDocumentIntent = regexp.MustCompile(`修改|调整|根据|基于|沿用|保留|刚才|转为|转成|转换|在.*基础|(?i:\b(?:modify|adjust|revise|based on|from|convert|retain|keep)\b)`)
var documentIntentQuestion = regexp.MustCompile(`如何|怎么|需要什么|需要哪些|有什么|[？?]|(?:吗|么)\s*$|(?i:\b(?:how to|what does|what is)\b)`)
var contractDocumentIntent = regexp.MustCompile(`合同|协议|(?i:\b(?:contract|agreement)\b)`)
var quotationDocumentIntent = regexp.MustCompile(`报价|(?i:\b(?:quotation|quote)\b)`)

func sessionDocumentContext(session mailstore.AIChatSession, question string) (*AIAgentChatDocContext, error) {
	doc := &AIAgentChatDocContext{Type: session.DocumentType}
	if len(session.CurrentDocument) == 0 {
		if session.Kind == "mail" {
			return nil, nil
		}
		return doc, nil
	}
	if err := json.Unmarshal(session.CurrentDocument, doc); err != nil {
		return nil, err
	}
	if saved := session.CurrentSavedDocument; saved != nil {
		// Rebuild from the saved canvas, never merge with generated fields: users
		// can remove products or clear terms in the editor.
		values, items, ok := canvasDocumentContent(saved.HTML)
		doc = &AIAgentChatDocContext{ID: saved.ID, Revision: saved.Revision, Type: saved.Type, Title: saved.Name, Counterparty: saved.Company, Values: values, Items: items, StructuredContentUnavailable: !ok}
		if ok {
			doc.Contact = values["customer_contact"]
			doc.Currency = values["currency"]
			doc.Subtotal = values["subtotal"]
			doc.TaxRate, doc.TaxAmount = "", ""
			doc.Total = values["total_amount"]
		}
	} else if session.CurrentSavedDocumentMissing {
		doc = &AIAgentChatDocContext{Type: doc.Type, Title: doc.Title, SavedDocumentMissing: true}
	}
	if newDocumentIntent.MatchString(question) && !existingDocumentIntent.MatchString(question) && !documentIntentQuestion.MatchString(question) {
		values := map[string]string{}
		for key, value := range doc.Values {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "seller") || strings.HasPrefix(lower, "vendor_") || strings.HasPrefix(lower, "supplier_") || strings.HasPrefix(lower, "bank_") || strings.HasPrefix(lower, "beneficiary_") || lower == "swift_code" || lower == "iban" {
				values[key] = value
			}
		}
		kind := doc.Type
		if contractDocumentIntent.MatchString(question) {
			kind = "contract"
		} else if quotationDocumentIntent.MatchString(question) {
			kind = "quotation"
		}
		doc = &AIAgentChatDocContext{Type: kind, Values: values, NewDraft: true}
	}
	// Embedded image bytes are not document facts. Model images have their own budget.
	for i := range doc.Items {
		if strings.HasPrefix(doc.Items[i].Picture, "data:") {
			doc.Items[i].Picture = ""
		}
	}
	return doc, nil
}

// readCurrentDraft exposes the current draft, including its latest saved
// revision when linked, independently of loaded history pages.
func readCurrentDraft(ctx context.Context, args map[string]any) (any, string, error) {
	doc, _ := ctx.Value(agentDocumentContextKey{}).(*AIAgentChatDocContext)
	if doc == nil {
		return map[string]any{"available": false}, "当前没有草稿", nil
	}
	return readDraftData(doc, args)
}

func readDraftData(draft any, args map[string]any) (any, string, error) {
	raw, err := json.Marshal(draft)
	if err != nil {
		return nil, "", err
	}
	if string(raw) == "null" {
		return map[string]any{"available": false}, "当前没有草稿", nil
	}
	text := []rune(string(raw))
	offset, err := agentIntArg(args, "offset", 0, len(text))
	if err != nil {
		return nil, "", err
	}
	length, err := agentIntArg(args, "length", 2400, 4000)
	if err != nil || length == 0 {
		return nil, "", fmt.Errorf("length must be between 1 and 4000 characters")
	}
	end := offset + length
	if end > len(text) {
		end = len(text)
	}
	return map[string]any{"content": string(text[offset:end]), "offset": offset, "nextOffset": end, "hasMore": end < len(text), "totalLength": len(text)}, "读取当前草稿", nil
}

func freshDraftSnapshot(doc *AIAgentChatDocContext, kind string) json.RawMessage {
	if (kind != "document" && kind != "mail") || doc == nil || !doc.NewDraft {
		return nil
	}
	raw, _ := json.Marshal(doc)
	return raw
}
