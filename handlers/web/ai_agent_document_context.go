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
			doc.TaxRate = values["tax_rate"]
			doc.TaxAmount = values["tax_amount"]
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
