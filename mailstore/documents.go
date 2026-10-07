package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DocumentRecord represents a user-created quotation or contract document.
type DocumentRecord struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"ownerId,omitempty"`
	Type      string    `json:"type"` // "quotation" | "contract"
	Name      string    `json:"name"`
	Company   string    `json:"company"`
	HTML      string    `json:"html"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DocumentTemplateRecord represents a user-customized document template.
type DocumentTemplateRecord struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"ownerId,omitempty"`
	Type      string    `json:"type"` // "quotation" | "contract"
	Name      string    `json:"name"`
	HTML      string    `json:"html"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DocumentStampRecord represents an enterprise stamp/seal image.
type DocumentStampRecord struct {
	ID          string  `json:"id"`
	OwnerID     string  `json:"ownerId,omitempty"`
	Name        string  `json:"name"`
	DataURL     string  `json:"dataUrl,omitempty"`
	Value       string  `json:"value,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	InsertWidth int     `json:"insertWidth,omitempty"`
	AspectRatio float64 `json:"aspectRatio,omitempty"`
	CreatedAt   int64   `json:"createdAt"`
	UpdatedAt   int64   `json:"updatedAt"`
}

// ListDocuments returns all documents for an owner, optionally filtered by document type.
func (s *Store) ListDocuments(ctx context.Context, ownerID string, docType string) ([]DocumentRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}

	docType = strings.TrimSpace(docType)
	var rows *sql.Rows
	var err error
	if docType == "" || docType == "all" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, owner_id, type, name, company, html, created_at, updated_at
			FROM documents
			WHERE owner_id = ?
			ORDER BY updated_at DESC, id DESC
		`, ownerID)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, owner_id, type, name, company, html, created_at, updated_at
			FROM documents
			WHERE owner_id = ? AND type = ?
			ORDER BY updated_at DESC, id DESC
		`, ownerID, docType)
	}
	if err != nil {
		return nil, fmt.Errorf("mailstore: list documents: %w", err)
	}
	defer rows.Close()

	var records []DocumentRecord
	for rows.Next() {
		var r DocumentRecord
		var createdAtMs, updatedAtMs int64
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.Type, &r.Name, &r.Company, &r.HTML, &createdAtMs, &updatedAtMs); err != nil {
			return nil, fmt.Errorf("mailstore: scan document: %w", err)
		}
		r.CreatedAt = time.UnixMilli(createdAtMs)
		r.UpdatedAt = time.UnixMilli(updatedAtMs)
		records = append(records, r)
	}
	if records == nil {
		records = []DocumentRecord{}
	}
	return records, rows.Err()
}

// GetDocument returns a single document by ID belonging to the owner.
func (s *Store) GetDocument(ctx context.Context, ownerID, id string) (*DocumentRecord, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	ownerID = strings.TrimSpace(ownerID)
	id = strings.TrimSpace(id)
	if ownerID == "" || id == "" {
		return nil, sql.ErrNoRows
	}

	var r DocumentRecord
	var createdAtMs, updatedAtMs int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, owner_id, type, name, company, html, created_at, updated_at
		FROM documents
		WHERE owner_id = ? AND id = ?
	`, ownerID, id).Scan(&r.ID, &r.OwnerID, &r.Type, &r.Name, &r.Company, &r.HTML, &createdAtMs, &updatedAtMs)
	if err != nil {
		return nil, err
	}
	r.CreatedAt = time.UnixMilli(createdAtMs)
	r.UpdatedAt = time.UnixMilli(updatedAtMs)
	return &r, nil
}

// SaveDocument inserts or updates a document for the owner.
func (s *Store) SaveDocument(ctx context.Context, doc DocumentRecord) (*DocumentRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("mailstore: database unavailable")
	}
	ownerID := strings.TrimSpace(doc.OwnerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}
	docID := strings.TrimSpace(doc.ID)
	if docID == "" {
		var err error
		docID, err = newID("doc")
		if err != nil {
			return nil, err
		}
	}
	docType := strings.TrimSpace(doc.Type)
	if docType != "quotation" && docType != "contract" {
		docType = "quotation"
	}
	name := strings.TrimSpace(doc.Name)
	if name == "" {
		name = "未命名文档"
	}

	now := time.Now()
	nowMs := now.UnixMilli()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO documents (id, owner_id, type, name, company, html, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			name = excluded.name,
			company = excluded.company,
			html = excluded.html,
			updated_at = excluded.updated_at
		WHERE documents.owner_id = excluded.owner_id
	`, docID, ownerID, docType, name, strings.TrimSpace(doc.Company), doc.HTML, nowMs, nowMs)
	if err != nil {
		return nil, fmt.Errorf("mailstore: save document: %w", err)
	}

	saved, err := s.GetDocument(ctx, ownerID, docID)
	if err != nil {
		return &DocumentRecord{
			ID:        docID,
			OwnerID:   ownerID,
			Type:      docType,
			Name:      name,
			Company:   strings.TrimSpace(doc.Company),
			HTML:      doc.HTML,
			CreatedAt: now,
			UpdatedAt: now,
		}, nil
	}
	return saved, nil
}

// DeleteDocuments deletes documents matching the given IDs belonging to the owner.
func (s *Store) DeleteDocuments(ctx context.Context, ownerID string, ids []string) error {
	if s == nil || s.db == nil || len(ids) == 0 {
		return nil
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return errors.New("mailstore: owner_id is required")
	}

	validIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			validIDs = append(validIDs, trimmed)
		}
	}
	if len(validIDs) == 0 {
		return nil
	}

	placeholders := make([]string, len(validIDs))
	args := make([]any, 0, len(validIDs)+1)
	args = append(args, ownerID)
	for i, id := range validIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf("DELETE FROM documents WHERE owner_id = ? AND id IN (%s)", strings.Join(placeholders, ","))
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mailstore: delete documents: %w", err)
	}
	return nil
}

// ListDocumentTemplates returns all custom templates for an owner.
func (s *Store) ListDocumentTemplates(ctx context.Context, ownerID string) ([]DocumentTemplateRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_id, type, name, html, created_at, updated_at
		FROM document_templates
		WHERE owner_id = ?
		ORDER BY updated_at DESC, id DESC
	`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list templates: %w", err)
	}
	defer rows.Close()

	var records []DocumentTemplateRecord
	for rows.Next() {
		var r DocumentTemplateRecord
		var createdAtMs, updatedAtMs int64
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.Type, &r.Name, &r.HTML, &createdAtMs, &updatedAtMs); err != nil {
			return nil, fmt.Errorf("mailstore: scan template: %w", err)
		}
		r.CreatedAt = time.UnixMilli(createdAtMs)
		r.UpdatedAt = time.UnixMilli(updatedAtMs)
		records = append(records, r)
	}
	if records == nil {
		records = []DocumentTemplateRecord{}
	}
	return records, rows.Err()
}

// SaveDocumentTemplate creates or updates a custom template for an owner.
func (s *Store) SaveDocumentTemplate(ctx context.Context, tpl DocumentTemplateRecord) (*DocumentTemplateRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("mailstore: database unavailable")
	}
	ownerID := strings.TrimSpace(tpl.OwnerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}
	tplID := strings.TrimSpace(tpl.ID)
	if tplID == "" {
		var err error
		tplID, err = newID("tpl")
		if err != nil {
			return nil, err
		}
	}
	docType := strings.TrimSpace(tpl.Type)
	if docType != "quotation" && docType != "contract" {
		docType = "quotation"
	}
	name := strings.TrimSpace(tpl.Name)
	if name == "" {
		name = "自定义模板"
	}

	now := time.Now()
	nowMs := now.UnixMilli()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO document_templates (id, owner_id, type, name, html, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			name = excluded.name,
			html = excluded.html,
			updated_at = excluded.updated_at
		WHERE document_templates.owner_id = excluded.owner_id
	`, tplID, ownerID, docType, name, tpl.HTML, nowMs, nowMs)
	if err != nil {
		return nil, fmt.Errorf("mailstore: save template: %w", err)
	}

	return &DocumentTemplateRecord{
		ID:        tplID,
		OwnerID:   ownerID,
		Type:      docType,
		Name:      name,
		HTML:      tpl.HTML,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// DeleteDocumentTemplate removes a custom template by ID for an owner.
func (s *Store) DeleteDocumentTemplate(ctx context.Context, ownerID, id string) error {
	if s == nil || s.db == nil {
		return nil
	}
	ownerID = strings.TrimSpace(ownerID)
	id = strings.TrimSpace(id)
	if ownerID == "" || id == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM document_templates
		WHERE owner_id = ? AND id = ?
	`, ownerID, id)
	return err
}

// ListDocumentStamps returns all stamps for an owner.
func (s *Store) ListDocumentStamps(ctx context.Context, ownerID string) ([]DocumentStampRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_id, name, image_data, width, height, aspect_ratio, created_at, updated_at
		FROM document_stamps
		WHERE owner_id = ?
		ORDER BY created_at DESC, id DESC
	`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list stamps: %w", err)
	}
	defer rows.Close()

	var records []DocumentStampRecord
	for rows.Next() {
		var r DocumentStampRecord
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.Name, &r.DataURL, &r.Width, &r.Height, &r.AspectRatio, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("mailstore: scan stamp: %w", err)
		}
		r.Value = r.DataURL
		records = append(records, r)
	}
	if records == nil {
		records = []DocumentStampRecord{}
	}
	return records, rows.Err()
}

// SaveDocumentStamp saves or updates an enterprise stamp for an owner.
func (s *Store) SaveDocumentStamp(ctx context.Context, stamp DocumentStampRecord) (*DocumentStampRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("mailstore: database unavailable")
	}
	ownerID := strings.TrimSpace(stamp.OwnerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}
	stampID := strings.TrimSpace(stamp.ID)
	if stampID == "" {
		var err error
		stampID, err = newID("stmp")
		if err != nil {
			return nil, err
		}
	}
	name := strings.TrimSpace(stamp.Name)
	if name == "" {
		name = "印章"
	}
	dataURL := stamp.DataURL
	if dataURL == "" {
		dataURL = stamp.Value
	}
	now := time.Now().UnixMilli()
	createdAt := stamp.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO document_stamps (id, owner_id, name, image_data, width, height, aspect_ratio, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			image_data = excluded.image_data,
			width = excluded.width,
			height = excluded.height,
			aspect_ratio = excluded.aspect_ratio,
			updated_at = excluded.updated_at
		WHERE document_stamps.owner_id = excluded.owner_id
	`, stampID, ownerID, name, dataURL, stamp.Width, stamp.Height, stamp.AspectRatio, createdAt, now)
	if err != nil {
		return nil, fmt.Errorf("mailstore: save stamp: %w", err)
	}

	return &DocumentStampRecord{
		ID:          stampID,
		OwnerID:     ownerID,
		Name:        name,
		DataURL:     dataURL,
		Value:       dataURL,
		Width:       stamp.Width,
		Height:      stamp.Height,
		AspectRatio: stamp.AspectRatio,
		CreatedAt:   createdAt,
		UpdatedAt:   now,
	}, nil
}

// DeleteDocumentStamp deletes a stamp by ID for an owner.
func (s *Store) DeleteDocumentStamp(ctx context.Context, ownerID, id string) error {
	if s == nil || s.db == nil {
		return nil
	}
	ownerID = strings.TrimSpace(ownerID)
	id = strings.TrimSpace(id)
	if ownerID == "" || id == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM document_stamps
		WHERE owner_id = ? AND id = ?
	`, ownerID, id)
	return err
}
