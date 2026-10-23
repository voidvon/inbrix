package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// AIBusinessProfile describes identity and defaults, never catalog facts.
type AIBusinessProfile struct {
	Company             string   `json:"company"`
	Brands              []string `json:"brands"`    // legacy; retained for migration only
	BrandMode           string   `json:"brandMode"` // prefer or restrict
	QuotationTemplateID string   `json:"quotationTemplateId"`
	ContractTemplateID  string   `json:"contractTemplateId"`
}

type AIBusinessContext struct {
	AgentID string   `json:"agentId"`
	SkillID string   `json:"skillId"` // empty = automatically choose/combine skills
	Brands  []string `json:"brands"`  // empty = inherit mailbox configuration
}

func NormalizeAIBrands(brands []string) ([]string, error) {
	if len(brands) > 30 {
		return nil, errors.New("最多配置 30 个品牌")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, b := range brands {
		b = strings.TrimSpace(b)
		if b == "" || utf8.RuneCountInString(b) > 100 || strings.ContainsAny(b, "\r\n") {
			return nil, errors.New("品牌名称不能为空且不能超过 100 字")
		}
		if !seen[b] {
			out = append(out, b)
			seen[b] = true
		}
	}
	return out, nil
}
func NormalizeAIBusinessContext(in AIBusinessContext) (AIBusinessContext, error) {
	in.AgentID = strings.TrimSpace(in.AgentID)
	switch in.SkillID {
	case "", "email_analysis", "quotation", "contract":
	default:
		return in, errors.New("未知的业务 Skill")
	}
	var err error
	in.Brands, err = NormalizeAIBrands(in.Brands)
	return in, err
}
func (s *Store) ValidateAIBusinessProfile(ctx context.Context, owner string, p AIBusinessProfile) (AIBusinessProfile, error) {
	var err error
	p.Brands, err = NormalizeAIBrands(p.Brands)
	if err != nil {
		return p, err
	}
	p.Company = strings.TrimSpace(p.Company)
	if utf8.RuneCountInString(p.Company) > 200 {
		return p, errors.New("公司名称过长")
	}
	if p.BrandMode == "" {
		p.BrandMode = "prefer"
	}
	if p.BrandMode != "prefer" && p.BrandMode != "restrict" {
		return p, errors.New("无效的品牌范围模式")
	}
	if p.BrandMode == "restrict" && len(p.Brands) == 0 {
		return p, errors.New("限制品牌时至少选择一个品牌")
	}
	for kind, id := range map[string]string{"quotation": p.QuotationTemplateID, "contract": p.ContractTemplateID} {
		if id == "" {
			continue
		}
		if id == "default-"+kind {
			// Default ids can also be customized and persisted in the template library.
			templates, e := s.ListDocumentTemplates(ctx, owner)
			if e != nil {
				return p, e
			}
			found := false
			for _, t := range templates {
				if t.ID == id {
					found = true
					if t.Type != kind {
						return p, errors.New("模板类型不匹配")
					}
				}
			}
			if !found {
				continue
			}
		}
		var typ string
		if e := s.db.QueryRowContext(ctx, `SELECT type FROM document_templates WHERE owner_id=? AND id=?`, owner, id).Scan(&typ); e != nil || typ != kind {
			return p, errors.New("默认模板不存在或类型不匹配")
		}
	}
	return p, nil
}
func (s *Store) migrateAIBusiness(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(ai_agents)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def sql.NullString
		if err = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "business_profile_json" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		if _, err = s.db.ExecContext(ctx, `ALTER TABLE ai_agents ADD COLUMN business_profile_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS conversation_ai_business (account_id TEXT NOT NULL, conversation_id TEXT NOT NULL, context_json TEXT NOT NULL DEFAULT '{}', PRIMARY KEY(account_id,conversation_id), FOREIGN KEY(account_id) REFERENCES mail_accounts(id) ON DELETE CASCADE)`)
	if err != nil {
		return err
	}
	return s.migrateAccountBrands(ctx)
}
func (s *Store) GetAIBusinessContext(ctx context.Context, account, conversation string) (AIBusinessContext, error) {
	var raw string
	out := AIBusinessContext{Brands: []string{}}
	err := s.db.QueryRowContext(ctx, `SELECT context_json FROM conversation_ai_business WHERE account_id=? AND conversation_id=?`, account, conversation).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}
func (s *Store) SaveAIBusinessContext(ctx context.Context, owner, account, conversation string, in AIBusinessContext) error {
	in, err := NormalizeAIBusinessContext(in)
	if err != nil {
		return err
	}
	if strings.TrimSpace(conversation) == "" {
		return errors.New("conversation id is required")
	}
	if err = s.ValidateAIChatBusiness(ctx, owner, account, in); err != nil {
		return err
	}
	raw, _ := json.Marshal(in)
	_, err = s.db.ExecContext(ctx, `INSERT INTO conversation_ai_business(account_id,conversation_id,context_json) VALUES(?,?,?) ON CONFLICT(account_id,conversation_id) DO UPDATE SET context_json=excluded.context_json`, account, conversation, string(raw))
	return err
}

func (s *Store) ValidateAIChatBusiness(ctx context.Context, owner, account string, in AIBusinessContext) error {
	in, err := NormalizeAIBusinessContext(in)
	if err != nil {
		return err
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mail_accounts WHERE id=? AND owner_id=?`, account, owner).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	if in.AgentID != "" {
		if _, e := s.GetAIAgent(ctx, owner, in.AgentID); e != nil {
			return fmt.Errorf("业务助手不存在: %w", e)
		}
	}
	binding, err := s.GetAccountBrands(ctx, owner, account)
	if err != nil {
		return err
	}
	if binding.BrandMode == "restrict" {
		for _, b := range in.Brands {
			allowed := false
			for _, a := range binding.Brands {
				if a == b {
					allowed = true
				}
			}
			if !allowed {
				return errors.New("品牌不在当前邮箱绑定范围内，请在邮箱设置中修改")
			}
		}
	}
	return nil
}
