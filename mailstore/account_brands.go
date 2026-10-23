package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// AccountBrands is shared by all assistants and business tasks for a mailbox.
type AccountBrands struct {
	Brands    []string `json:"brands"`
	BrandMode string   `json:"brandMode"`
}

func NormalizeAccountBrands(in AccountBrands) (AccountBrands, error) {
	var err error
	in.Brands, err = NormalizeAIBrands(in.Brands)
	if err != nil {
		return in, err
	}
	if in.BrandMode == "" {
		in.BrandMode = "prefer"
	}
	if in.BrandMode != "prefer" && in.BrandMode != "restrict" {
		return in, errors.New("无效的品牌范围模式")
	}
	if in.BrandMode == "restrict" && len(in.Brands) == 0 {
		return in, errors.New("限制品牌时至少选择一个品牌")
	}
	return in, nil
}
func (s *Store) migrateAccountBrands(ctx context.Context) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='account_brand_settings'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE account_brand_settings(account_id TEXT PRIMARY KEY, settings_json TEXT NOT NULL DEFAULT '{}', FOREIGN KEY(account_id) REFERENCES mail_accounts(id) ON DELETE CASCADE)`); err != nil {
		return err
	}
	// One-time upgrade: move the mailbox's email-draft assistant brands to its own binding.
	// Existing profiles remain stored for compatibility, but no longer control runtime scope.
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_brand_settings(account_id,settings_json)
 SELECT b.account_id, json_object('brands',json_extract(a.business_profile_json,'$.brands'),'brandMode',COALESCE(json_extract(a.business_profile_json,'$.brandMode'),'prefer'))
 FROM ai_task_bindings b JOIN ai_agents a ON a.id=b.agent_id
 WHERE b.task_type='email_draft' AND json_array_length(json_extract(a.business_profile_json,'$.brands'))>0`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) GetAccountBrands(ctx context.Context, owner, account string) (AccountBrands, error) {
	out := AccountBrands{Brands: []string{}, BrandMode: "prefer"}
	a, err := s.GetAccount(ctx, account)
	if err != nil {
		return out, err
	}
	if a.OwnerID != owner {
		return out, ErrNotFound
	}
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT settings_json FROM account_brand_settings WHERE account_id=?`, account).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(raw), &out); err != nil {
		return out, err
	}
	return NormalizeAccountBrands(out)
}
func (s *Store) SaveAccountBrands(ctx context.Context, owner, account string, in AccountBrands) error {
	a, err := s.GetAccount(ctx, account)
	if err != nil {
		return err
	}
	if a.OwnerID != owner {
		return ErrNotFound
	}
	in, err = NormalizeAccountBrands(in)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO account_brand_settings(account_id,settings_json) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET settings_json=excluded.settings_json`, account, string(raw))
	return err
}
