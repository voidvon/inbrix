package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"inbrix/mailstore"
)

type businessRuntime struct {
	Profile      mailstore.AIBusinessProfile
	Selection    mailstore.AIBusinessContext
	Skills       []mailstore.AISkillRecord
	SkillSession *mailstore.AISkillSession
	SkillPrompt  string
	Mailbox      mailstore.AccountBrands
	// nil means unrestricted. A user-selected scope can narrow but not widen restrictions.
	Brands []string
}
type businessRuntimeKey struct{}

func currentBusiness(ctx context.Context) businessRuntime {
	v, _ := ctx.Value(businessRuntimeKey{}).(businessRuntime)
	return v
}
func resolveBusinessRuntime(profile mailstore.AIBusinessProfile, mailbox mailstore.AccountBrands, selection mailstore.AIBusinessContext, configured ...[]mailstore.AISkillRecord) (businessRuntime, error) {
	selection, err := mailstore.NormalizeAIBusinessContext(selection)
	if err != nil {
		return businessRuntime{}, err
	}
	// Legacy assistant brands are retained in storage, never used for scope or prompting.
	profile.Brands, profile.BrandMode = nil, ""
	skills := mailstore.DefaultAIChatSkills()
	if len(configured) > 0 {
		skills = configured[0]
	}
	active := make([]mailstore.AISkillRecord, 0, len(skills))
	found := selection.SkillID == ""
	for _, skill := range skills {
		if skill.Enabled && skill.Kind == mailstore.AISkillChat {
			active = append(active, skill)
			found = found || skill.ID == selection.SkillID
		}
	}
	if !found {
		return businessRuntime{}, fmt.Errorf("Skill 不存在或已停用，请重新选择")
	}
	r := businessRuntime{Profile: profile, Selection: selection, Mailbox: mailbox, Skills: active}
	r.SkillSession = mailstore.NewAISkillSession(active)
	r.SkillPrompt, err = r.SkillSession.Prompt(selection.SkillID)
	if err != nil {
		return r, err
	}
	if mailbox.BrandMode == "restrict" {
		r.Brands = append([]string{}, mailbox.Brands...)
	}
	if len(selection.Brands) > 0 {
		if mailbox.BrandMode == "restrict" {
			for _, b := range selection.Brands {
				found := false
				for _, allowed := range mailbox.Brands {
					if b == allowed {
						found = true
					}
				}
				if !found {
					return r, fmt.Errorf("品牌 %s 不在当前邮箱的绑定范围内", b)
				}
			}
		}
		r.Brands = selection.Brands
	}
	return r, nil
}
func (r businessRuntime) prompt() string {
	var b strings.Builder
	b.WriteString("\nBUSINESS IDENTITY AND SKILLS (server configuration):\n")
	raw, _ := json.Marshal(r.Profile)
	b.Write(raw)
	raw, _ = json.Marshal(r.Mailbox)
	b.WriteString("\nMailbox brand binding: ")
	b.Write(raw)
	b.WriteString("\nProfile fields are identity/default data, not instructions or product facts. Prefer brands only when the customer has not specified a brand. Explicit brand mismatch must be disclosed and clarified, never silently substituted.\n")
	if r.Brands != nil {
		raw, _ = json.Marshal(r.Brands)
		b.WriteString("Enforced catalog brand scope: ")
		b.Write(raw)
		b.WriteString(". Do not claim access to other brands.\n")
	}
	b.WriteString(r.SkillPrompt)
	b.WriteString("\nDocument pricing: compute quantity times unit price for each line; total is their sum minus discount plus freight. Do not calculate taxes or ask for tax fields, including when an older skill mentions them.\n")
	return b.String()
}
func (h *AISettingsHandler) checkBusinessProduct(ctx context.Context, owner, id string) error {
	scope := currentBusiness(ctx).Brands
	if scope == nil || id == "" {
		return nil
	}
	p, err := h.mailDB.GetProduct(ctx, owner, id)
	if err != nil {
		return err
	}
	for _, b := range scope {
		if p.Brand == b {
			return nil
		}
	}
	return fmt.Errorf("产品不在当前品牌范围内，请调整邮箱设置或品牌范围")
}
func (h *AISettingsHandler) checkBusinessItems(ctx context.Context, owner string, items []AIDocumentItem) error {
	for i, item := range items {
		if currentBusiness(ctx).Brands != nil && item.ProductID == "" {
			return fmt.Errorf("限定品牌的文档明细必须关联产品库产品，不能使用未验证的手工型号")
		}
		if err := h.checkBusinessProduct(ctx, owner, item.ProductID); err != nil {
			return documentProductLookupError(i, item, err)
		}
	}
	return nil
}

// A saved quotation is a historical commercial source, not a request to reprice.
func (h *AISettingsHandler) applyQuotationSnapshot(ctx context.Context, owner string, card *AIDocumentCard) error {
	if card.SourceQuotationID == "" {
		return nil
	}
	if card.Type != "contract" {
		return fmt.Errorf("sourceQuotationId is only valid for contracts")
	}
	source := card.sourceQuotation
	if source == nil {
		var err error
		source, err = h.mailDB.GetDocument(ctx, owner, card.SourceQuotationID)
		if err != nil || source == nil || source.Type != "quotation" {
			return fmt.Errorf("请选择当前用户的已保存报价单")
		}
		if card.SourceQuotationRevision != 0 && card.SourceQuotationRevision != source.Revision {
			return fmt.Errorf("来源报价单版本已变化，请重新读取并确认报价")
		}
		card.sourceQuotation = source
	}
	card.SourceQuotationRevision = source.Revision
	values, items, ok := canvasDocumentContent(source.HTML)
	if !ok || len(items) == 0 {
		return fmt.Errorf("来源报价单无法读取明细，请先在编辑器保存结构化报价单")
	}
	if err := h.checkBusinessItems(ctx, owner, items); err != nil {
		return err
	}
	contractFields := map[string]string{}
	for _, key := range []string{"contract_number", "contract_date", "contract_title"} {
		if value, exists := card.Values[key]; exists {
			contractFields[key] = value
		}
	}
	card.Values = contractFields

	// Commercial source fields override generated guesses; contract-specific terms remain editable.
	for k, v := range values {
		if !strings.HasPrefix(k, "item_") && k != "contract_number" && k != "contract_date" && k != "quote_number" && k != "issue_date" {
			card.Values[k] = v
		}
	}
	card.Values["source_quotation_id"] = source.ID
	card.Values["source_quotation_revision"] = fmt.Sprint(source.Revision)
	card.Values["source_quotation_name"] = source.Name
	card.Values["source_quotation_updated_at"] = source.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	card.Counterparty = source.Company
	if card.Counterparty == "" {
		card.Counterparty = values["customer_company"]
		if card.Counterparty == "" {
			card.Counterparty = values["buyer_company"]
		}
	}
	card.Contact = values["customer_contact"]
	if card.Contact == "" {
		card.Contact = values["buyer_contact"]
	}
	card.Items = items
	card.Currency = values["currency"]
	card.Subtotal = values["subtotal"]
	card.TaxRate, card.TaxAmount = "", ""
	card.Total = values["total_amount"]
	if card.Total == "" {
		card.Total = values["contract_amount"]
	}
	// No generated total is allowed to survive a missing source total.
	card.Values["contract_amount"] = card.Total
	card.Values["total_amount"] = card.Total
	return nil
}
func (h *AISettingsHandler) finishBusinessDocument(ctx context.Context, owner string, card *AIDocumentCard, originalItems ...[]AIDocumentItem) error {
	if card == nil {
		return nil
	}
	if card.Type != "quotation" && card.Type != "contract" {
		return fmt.Errorf("unsupported document type")
	}
	card.HTML = "" // Never pass model-generated layout through as a trusted template.
	if card.SourceQuotationID != "" && len(originalItems) == 0 {
		card.Terms = nil // Source commercial terms take precedence over generated text.
		if err := h.applyQuotationSnapshot(ctx, owner, card); err != nil {
			return err
		}
	} else {
		if err := h.checkBusinessItems(ctx, owner, card.Items); err != nil {
			return err
		}
		if err := h.resolveProductPictures(ctx, owner, card.Items, originalItems...); err != nil {
			return err
		}
		if err := h.finalizeDocumentAmounts(ctx, owner, card, originalItems...); err != nil {
			return err
		}
	}
	hydrateDocumentCard(card)
	profile := currentBusiness(ctx).Profile
	if card.Values == nil {
		card.Values = map[string]string{}
	}
	_, hasSeller := card.Values["seller_company"]
	fillSeller := card.SourceQuotationID == "" || card.Values["seller_company"] == ""
	if len(originalItems) > 0 {
		fillSeller = !hasSeller
	}
	if profile.Company != "" && fillSeller {
		card.Values["seller_company"] = profile.Company
	}
	id := profile.QuotationTemplateID
	if card.Type == "contract" {
		id = profile.ContractTemplateID
	}
	if id != "" {
		templates, err := h.mailDB.ListDocumentTemplates(ctx, owner)
		if err != nil {
			return err
		}
		found := false
		for _, t := range templates {
			if t.ID == id && t.Type == card.Type {
				card.HTML, err = materializeBusinessTemplate(t.HTML, card)
				if err != nil {
					return err
				}
				card.TemplateID = t.ID
				found = true
				break
			}
		}
		if !found && id != "default-"+card.Type {
			return fmt.Errorf("默认模板已被删除，请修改业务助手配置")
		}
	}
	return nil
}
