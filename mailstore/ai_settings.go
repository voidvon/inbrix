package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const aiModelColumns = `id, owner_id, provider, base_url, model, reasoning_effort, extra_params, supports_images, encrypted_api_key, is_default, created_at`

// SupportsImageInput honors an explicit capability setting. Image input is
// enabled by default for each supported protocol.
func (model AIModelRecord) SupportsImageInput() bool {
	if model.SupportsImages != nil {
		return *model.SupportsImages
	}
	return true
}

func (s *Store) ensureAIModelSupportsImagesColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(ai_models)`)
	if err != nil {
		return fmt.Errorf("mailstore: inspect AI models schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("mailstore: scan AI models schema: %w", err)
		}
		if name == "supports_images" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("mailstore: inspect AI models schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE ai_models ADD COLUMN supports_images INTEGER`); err != nil {
		return fmt.Errorf("mailstore: add AI model image support column: %w", err)
	}
	return nil
}

func scanAIModel(scanner interface{ Scan(...any) error }) (AIModelRecord, error) {
	var model AIModelRecord
	var isDefault int
	var createdAt int64
	err := scanner.Scan(&model.ID, &model.OwnerID, &model.Provider, &model.BaseURL, &model.Model, &model.ReasoningEffort, &model.ExtraParams, &model.SupportsImages, &model.EncryptedAPIKey, &isDefault, &createdAt)
	model.IsDefault = intBool(isDefault)
	model.CreatedAt = time.Unix(createdAt, 0)
	return model, err
}

func (s *Store) ListAIModels(ctx context.Context, ownerID string) ([]AIModelRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+aiModelColumns+` FROM ai_models WHERE owner_id = ? ORDER BY is_default DESC, created_at, model`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list AI models: %w", err)
	}
	defer rows.Close()
	models := []AIModelRecord{}
	for rows.Next() {
		model, err := scanAIModel(rows)
		if err != nil {
			return nil, fmt.Errorf("mailstore: scan AI model: %w", err)
		}
		models = append(models, model)
	}
	return models, rows.Err()
}

func (s *Store) GetAIModel(ctx context.Context, ownerID, id string) (AIModelRecord, error) {
	model, err := scanAIModel(s.db.QueryRowContext(ctx, `SELECT `+aiModelColumns+` FROM ai_models WHERE owner_id = ? AND id = ?`, ownerID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AIModelRecord{}, ErrNotFound
	}
	if err != nil {
		return AIModelRecord{}, fmt.Errorf("mailstore: get AI model: %w", err)
	}
	return model, nil
}

func (s *Store) CreateAIModel(ctx context.Context, model AIModelRecord) (AIModelRecord, error) {
	if strings.TrimSpace(model.OwnerID) == "" || strings.TrimSpace(model.BaseURL) == "" || strings.TrimSpace(model.Model) == "" || model.EncryptedAPIKey == "" {
		return AIModelRecord{}, errors.New("mailstore: AI model owner, Base URL, model, and API key are required")
	}
	model.Provider = strings.ToLower(strings.TrimSpace(model.Provider))
	if model.Provider == "" {
		model.Provider = "openai"
	}
	if !isSupportedAIProvider(model.Provider) {
		return AIModelRecord{}, errors.New("mailstore: provider must be openai, openai-compatible, or gemini")
	}
	if model.ReasoningEffort == "" {
		model.ReasoningEffort = "medium"
	}
	if strings.TrimSpace(model.ExtraParams) == "" {
		model.ExtraParams = "{}"
	}
	if model.ID == "" {
		var err error
		model.ID, err = newID("aimodel")
		if err != nil {
			return AIModelRecord{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIModelRecord{}, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_models WHERE owner_id = ?`, model.OwnerID).Scan(&count); err != nil {
		return AIModelRecord{}, err
	}
	model.IsDefault = count == 0 || model.IsDefault
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ai_models(id, owner_id, provider, base_url, model, reasoning_effort, extra_params, supports_images, encrypted_api_key, is_default, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		model.ID, model.OwnerID, model.Provider, model.BaseURL, model.Model, model.ReasoningEffort, model.ExtraParams, model.SupportsImages, model.EncryptedAPIKey, boolInt(model.IsDefault), now, now); err != nil {
		return AIModelRecord{}, fmt.Errorf("mailstore: create AI model: %w", err)
	}
	if model.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_models SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END WHERE owner_id = ?`, model.ID, model.OwnerID); err != nil {
			return AIModelRecord{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AIModelRecord{}, err
	}
	model.CreatedAt = time.Unix(now, 0)
	return model, nil
}

func (s *Store) UpdateAIModel(ctx context.Context, model AIModelRecord) (AIModelRecord, error) {
	if strings.TrimSpace(model.ID) == "" || strings.TrimSpace(model.OwnerID) == "" || strings.TrimSpace(model.BaseURL) == "" || strings.TrimSpace(model.Model) == "" || strings.TrimSpace(model.ReasoningEffort) == "" {
		return AIModelRecord{}, errors.New("mailstore: AI model id, owner, Base URL, model, and reasoning effort are required")
	}
	model.Provider = strings.ToLower(strings.TrimSpace(model.Provider))
	if model.Provider != "" && !isSupportedAIProvider(model.Provider) {
		return AIModelRecord{}, errors.New("mailstore: provider must be openai, openai-compatible, or gemini")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE ai_models SET provider = CASE WHEN ? = '' THEN provider ELSE ? END, base_url = ?, model = ?, reasoning_effort = ?, extra_params = CASE WHEN ? = '' THEN extra_params ELSE ? END, supports_images = COALESCE(?, supports_images), encrypted_api_key = CASE WHEN ? = '' THEN encrypted_api_key ELSE ? END, updated_at = ? WHERE owner_id = ? AND id = ?`,
		model.Provider, model.Provider, model.BaseURL, model.Model, model.ReasoningEffort, model.ExtraParams, model.ExtraParams, model.SupportsImages, model.EncryptedAPIKey, model.EncryptedAPIKey, time.Now().Unix(), model.OwnerID, model.ID)
	if err != nil {
		return AIModelRecord{}, fmt.Errorf("mailstore: update AI model: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return AIModelRecord{}, ErrNotFound
	}
	updated, err := scanAIModel(s.db.QueryRowContext(ctx, `SELECT `+aiModelColumns+` FROM ai_models WHERE owner_id = ? AND id = ?`, model.OwnerID, model.ID))
	if err != nil {
		return AIModelRecord{}, fmt.Errorf("mailstore: read updated AI model: %w", err)
	}
	return updated, nil
}

func (s *Store) SetDefaultAIModel(ctx context.Context, ownerID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_models WHERE owner_id = ? AND id = ?`, ownerID, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_models SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END, updated_at = ? WHERE owner_id = ?`, id, time.Now().Unix(), ownerID); err != nil {
		return fmt.Errorf("mailstore: set default AI model: %w", err)
	}
	return tx.Commit()
}

func (s *Store) DeleteAIModel(ctx context.Context, ownerID, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var wasDefault int
	if err := tx.QueryRowContext(ctx, `SELECT is_default FROM ai_models WHERE owner_id = ? AND id = ?`, ownerID, id).Scan(&wasDefault); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_models WHERE owner_id = ? AND id = ?`, ownerID, id); err != nil {
		return err
	}
	if intBool(wasDefault) {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_models SET is_default = 1 WHERE id = (SELECT id FROM ai_models WHERE owner_id = ? ORDER BY created_at LIMIT 1)`, ownerID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetDefaultAIModel(ctx context.Context, ownerID string) (AIModelRecord, error) {
	model, err := scanAIModel(s.db.QueryRowContext(ctx, `SELECT `+aiModelColumns+` FROM ai_models WHERE owner_id = ? ORDER BY is_default DESC, created_at LIMIT 1`, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return AIModelRecord{}, ErrNotFound
	}
	if err != nil {
		return AIModelRecord{}, fmt.Errorf("mailstore: get default AI model: %w", err)
	}
	return model, nil
}

func isSupportedAIProvider(provider string) bool {
	switch provider {
	case "openai", "openai-compatible", "gemini":
		return true
	}
	return false
}
