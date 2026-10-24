package maileval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	mailapi "inbrix/handlers/api"
	"inbrix/mailstore"
)

// LoadConfiguredExecution reads only settings in a read-only SQLite transaction.
// It deliberately never opens the live Store (which would run migrations),
// reads mail bodies, initializes skills, changes bindings, or starts workers.
func LoadConfiguredExecution(ctx context.Context, configPath, owner, modelID, agentID string) (mailstore.MailAnalysisExecutionConfig, string, error) {
	var settings struct {
		MailSync struct {
			Database string `toml:"database"`
		} `toml:"mail_sync"`
		Cache struct {
			Folder string `toml:"folder"`
		} `toml:"cache"`
		Encryption struct {
			Key string `toml:"key"`
		} `toml:"encryption"`
	}
	if _, err := toml.DecodeFile(configPath, &settings); err != nil {
		return mailstore.MailAnalysisExecutionConfig{}, "", err
	}
	path := settings.MailSync.Database
	if path == "" {
		folder := settings.Cache.Folder
		if folder == "" {
			folder = "data"
		}
		path = filepath.Join(folder, "mail.db")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(configPath), path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return mailstore.MailAnalysisExecutionConfig{}, "", err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return mailstore.MailAnalysisExecutionConfig{}, "", err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return mailstore.MailAnalysisExecutionConfig{}, "", err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT owner_id FROM ai_models WHERE (?='' OR owner_id=?) AND (?='' OR id=?)`, owner, owner, modelID, modelID)
	if err != nil {
		return mailstore.MailAnalysisExecutionConfig{}, "", err
	}
	owners := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return mailstore.MailAnalysisExecutionConfig{}, "", err
		}
		owners = append(owners, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return mailstore.MailAnalysisExecutionConfig{}, "", err
	}
	if len(owners) != 1 {
		return mailstore.MailAnalysisExecutionConfig{}, "", errors.New("model selection is absent or ambiguous; supply -owner and/or -model-id")
	}
	owner = owners[0]
	cfg := DefaultExecutionConfig()
	var encrypted string
	err = tx.QueryRowContext(ctx, `SELECT id,owner_id,provider,base_url,model,reasoning_effort,extra_params,encrypted_api_key FROM ai_models WHERE owner_id=? AND (?='' OR id=?) ORDER BY is_default DESC,created_at,id LIMIT 1`, owner, modelID, modelID).Scan(&cfg.Model.ID, &cfg.Model.OwnerID, &cfg.Model.Provider, &cfg.Model.BaseURL, &cfg.Model.Model, &cfg.Model.ReasoningEffort, &cfg.Model.ExtraParams, &encrypted)
	if err != nil {
		return cfg, "", err
	}
	if mailapi.DecryptJSON(encrypted, &cfg.APIKey, settings.Encryption.Key) != nil || strings.TrimSpace(cfg.APIKey) == "" {
		return cfg, "", errors.New("cannot decrypt selected model key")
	}
	var labels string
	err = tx.QueryRowContext(ctx, `SELECT id,name,prompt,output_labels_json FROM ai_agents WHERE owner_id=? AND (?='' OR id=?) ORDER BY CASE purpose WHEN 'mail_summary' THEN 0 WHEN 'feishu_inquiry_analysis' THEN 1 ELSE 2 END,created_at,name LIMIT 1`, owner, agentID, agentID).Scan(&cfg.Agent.ID, &cfg.Agent.Name, &cfg.Agent.Prompt, &labels)
	if err != nil && (!errors.Is(err, sql.ErrNoRows) || agentID != "") {
		return cfg, "", err
	}
	if err == nil {
		if err := json.Unmarshal([]byte(labels), &cfg.Agent.OutputLabels); err != nil {
			return cfg, "", err
		}
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='ai_skill_settings'`).Scan(&exists); err != nil {
		return cfg, "", err
	}
	origin := "builtin-default (no initialized skill registry)"
	if exists > 0 {
		var active string
		err := tx.QueryRowContext(ctx, `SELECT mail_summary_skill_id FROM ai_skill_settings WHERE owner_id=?`, owner).Scan(&active)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return cfg, "", err
		}
		if err == nil {
			rows, err := tx.QueryContext(ctx, `SELECT id,name,description,instructions,kind,depends_on_json,enabled,built_in,revision FROM ai_skills WHERE owner_id=?`, owner)
			if err != nil {
				return cfg, "", err
			}
			var records []mailstore.AISkillRecord
			for rows.Next() {
				var r mailstore.AISkillRecord
				var deps string
				var enabled, builtin int
				if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Instructions, &r.Kind, &deps, &enabled, &builtin, &r.Revision); err != nil {
					rows.Close()
					return cfg, "", err
				}
				if err := json.Unmarshal([]byte(deps), &r.DependsOn); err != nil {
					rows.Close()
					return cfg, "", err
				}
				r.Enabled = enabled != 0
				r.BuiltIn = builtin != 0
				records = append(records, r)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return cfg, "", err
			}
			cfg.Skill, err = mailstore.ResolveAISkillSnapshot(records, active, mailstore.AISkillMailSummary)
			if err != nil {
				return cfg, "", err
			}
			origin = "configured-snapshot"
		}
	}
	return cfg, origin, nil
}

func OverrideSkillFile(cfg mailstore.MailAnalysisExecutionConfig, path string) (mailstore.MailAnalysisExecutionConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if len(raw) == 0 || len(raw) > 16000 || !utf8.Valid(raw) || strings.TrimSpace(string(raw)) == "" {
		return cfg, fmt.Errorf("candidate skill must contain 1..16000 bytes")
	}
	records := append([]mailstore.AISkillRecord{}, cfg.Skill.Versions...)
	for i := range records {
		if records[i].ID == cfg.Skill.ID {
			records[i].Instructions = string(raw)
			records[i].Revision = 0
		}
	}
	cfg.Skill, err = mailstore.ResolveAISkillSnapshot(records, cfg.Skill.ID, mailstore.AISkillMailSummary)
	return cfg, err
}
