package mailstore

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"
)

//go:embed templates/chinese-sales-contract.json
var chineseSalesContractTemplate string

var ErrDocumentConflict = errors.New("文档已修改或删除，请重新打开最新文档，或让 AI 重新生成修改方案 / Document changed or was deleted; reopen it or regenerate the proposal")

// DocumentRecord represents a user-created quotation or contract document.
type DocumentRecord struct {
	AIOrigin  *AIChatDocumentOrigin `json:"aiOrigin,omitempty"`
	Revision  int64                 `json:"revision"`
	ID        string                `json:"id"`
	OwnerID   string                `json:"ownerId,omitempty"`
	Type      string                `json:"type"` // "quotation" | "contract"
	Name      string                `json:"name"`
	Company   string                `json:"company"`
	HTML      string                `json:"html"`
	CreatedAt time.Time             `json:"createdAt"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

// DocumentSummary contains only the fields needed to display a library row.
// Canvas data, embedded images and other document content are fetched by ID.
type DocumentSummary struct {
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Company   string    `json:"company"`
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

// ListDocuments returns metadata for an owner's documents without reading HTML.
func (s *Store) ListDocuments(ctx context.Context, ownerID string, docType string) ([]DocumentSummary, error) {
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
			SELECT id, type, name, company, created_at, updated_at, revision
			FROM documents
			WHERE owner_id = ?
			ORDER BY updated_at DESC, id DESC
		`, ownerID)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, type, name, company, created_at, updated_at, revision
			FROM documents
			WHERE owner_id = ? AND type = ?
			ORDER BY updated_at DESC, id DESC
		`, ownerID, docType)
	}
	if err != nil {
		return nil, fmt.Errorf("mailstore: list documents: %w", err)
	}
	defer rows.Close()

	records := []DocumentSummary{}
	for rows.Next() {
		var r DocumentSummary
		var createdAtMs, updatedAtMs int64
		if err := rows.Scan(&r.ID, &r.Type, &r.Name, &r.Company, &createdAtMs, &updatedAtMs, &r.Revision); err != nil {
			return nil, fmt.Errorf("mailstore: scan document: %w", err)
		}
		r.CreatedAt = time.UnixMilli(createdAtMs)
		r.UpdatedAt = time.UnixMilli(updatedAtMs)
		records = append(records, r)
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
		SELECT id, owner_id, type, name, company, html, created_at, updated_at, revision
		FROM documents
		WHERE owner_id = ? AND id = ?
	`, ownerID, id).Scan(&r.ID, &r.OwnerID, &r.Type, &r.Name, &r.Company, &r.HTML, &createdAtMs, &updatedAtMs, &r.Revision)
	if err != nil {
		return nil, err
	}
	r.CreatedAt = time.UnixMilli(createdAtMs)
	r.UpdatedAt = time.UnixMilli(updatedAtMs)
	return &r, nil
}

// SaveDocument inserts or updates a document for the owner.
func (s *Store) SaveDocument(ctx context.Context, doc DocumentRecord, expectedRevision ...int64) (*DocumentRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("mailstore: database unavailable")
	}
	ownerID := strings.TrimSpace(doc.OwnerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Acquire the write lock before validating the origin or revision.
	if _, err = tx.ExecContext(ctx, `UPDATE documents SET revision=revision WHERE id=? AND owner_id=?`, doc.ID, ownerID); err != nil {
		return nil, err
	}
	if doc.AIOrigin != nil {
		if err = validateAIChatDocumentOrigin(ctx, tx, ownerID, doc.ID, *doc.AIOrigin, len(expectedRevision) > 0 && expectedRevision[0] > 0); err != nil {
			return nil, err
		}
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

	var query string
	args := []any{docID, ownerID, docType, name, strings.TrimSpace(doc.Company), doc.HTML, nowMs, nowMs}
	if len(expectedRevision) > 0 && expectedRevision[0] < 0 {
		return nil, ErrDocumentConflict
	}
	if len(expectedRevision) > 0 && expectedRevision[0] > 0 {
		query = `UPDATE documents SET type=?,name=?,company=?,html=?,updated_at=?,revision=revision+1 WHERE id=? AND owner_id=? AND revision=?`
		args = []any{docType, name, strings.TrimSpace(doc.Company), doc.HTML, nowMs, docID, ownerID, expectedRevision[0]}
	} else {
		query = `INSERT INTO documents (id,owner_id,type,name,company,html,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`
		// Internal legacy callers can still upsert. HTTP callers always supply a
		// revision: zero creates only, positive revisions update existing rows only.
		if len(expectedRevision) == 0 {
			query = `INSERT INTO documents (id,owner_id,type,name,company,html,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET type=excluded.type,name=excluded.name,company=excluded.company,html=excluded.html,updated_at=excluded.updated_at,revision=documents.revision+1 WHERE documents.owner_id=excluded.owner_id`
		}
	}
	// RETURNING keeps the returned content/version tied to this exact write.
	saved := &DocumentRecord{}
	var created, updated int64
	err = tx.QueryRowContext(ctx, query+` RETURNING id,owner_id,type,name,company,html,created_at,updated_at,revision`, args...).Scan(&saved.ID, &saved.OwnerID, &saved.Type, &saved.Name, &saved.Company, &saved.HTML, &created, &updated, &saved.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDocumentConflict
	}
	if err != nil {
		return nil, fmt.Errorf("mailstore: save document: %w", err)
	}
	saved.CreatedAt = time.UnixMilli(created)
	saved.UpdatedAt = time.UnixMilli(updated)
	if doc.AIOrigin != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_chat_document_links(message_id,document_id) VALUES(?,?) ON CONFLICT(message_id) DO NOTHING`, doc.AIOrigin.MessageID, saved.ID); err != nil {
			return nil, err
		}
	}
	if err = invalidateAIChatDocumentReaders(ctx, tx, ownerID, []string{saved.ID}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mailstore: delete documents: %w", err)
	}
	if err = invalidateAIChatDocumentReaders(ctx, tx, ownerID, validIDs); err != nil {
		return err
	}
	return tx.Commit()
}

// EnsureDefaultDocumentTemplates seeds the built-in templates once per owner.
// Later saves and deletions are authoritative and never reset on subsequent reads.
func (s *Store) EnsureDefaultDocumentTemplates(ctx context.Context, owner string, english bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO document_template_initializations(owner_id) VALUES(?)`, owner)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count > 0 {
		now := time.Now().UnixMilli()
		for _, kind := range []string{"quotation", "contract"} {
			name := "标准报价单模板"
			if kind == "contract" {
				name = "标准合同模板"
			}
			if english {
				name = "Standard quotation"
				if kind == "contract" {
					name = "Standard contract"
				}
			}
			legacyID := "default-" + kind
			body := fmt.Sprintf(`<div data-spirax-%s="reference-v1"></div>`, kind)
			_, err = tx.ExecContext(ctx, `INSERT INTO document_templates(id,owner_id,type,name,html,created_at,updated_at)
				SELECT ?,?,?,?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM document_templates WHERE owner_id=? AND id=?)`, legacyID+"-"+owner, owner, kind, name, body, now, now, owner, legacyID)
			if err != nil {
				return err
			}
		}
	}

	// Each built-in addition is seeded independently, so existing owners receive
	// it once while their subsequent edits and deletions remain authoritative.
	result, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO document_builtin_template_seeds(owner_id,template_key) VALUES(?,?)`, owner, "chinese-sales-contract")
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count > 0 {
		now := time.Now().UnixMilli()
		_, err = tx.ExecContext(ctx, `INSERT INTO document_templates(id,owner_id,type,name,html,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "chinese-sales-contract-"+owner, owner, "contract", "中文购销合同模板", "__INBRIX_CANVAS_DOCUMENT__:"+strings.TrimSpace(chineseSalesContractTemplate), now, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListDocumentTemplates returns the owner's saved template catalog.
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
		ORDER BY CASE WHEN id LIKE 'default-quotation%' THEN 0 WHEN id LIKE 'default-contract%' THEN 1 ELSE 2 END, updated_at DESC, id DESC
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
		SELECT id, owner_id, name, image_data, width, height, insert_width, aspect_ratio, created_at, updated_at
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
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.Name, &r.DataURL, &r.Width, &r.Height, &r.InsertWidth, &r.AspectRatio, &r.CreatedAt, &r.UpdatedAt); err != nil {
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
	if stamp.InsertWidth < 10 || stamp.InsertWidth > 150 {
		stamp.InsertWidth = 40
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO document_stamps (id, owner_id, name, image_data, width, height, insert_width, aspect_ratio, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			image_data = excluded.image_data,
			width = excluded.width,
			height = excluded.height,
			insert_width = excluded.insert_width,
			aspect_ratio = excluded.aspect_ratio,
			updated_at = excluded.updated_at
		WHERE document_stamps.owner_id = excluded.owner_id
	`, stampID, ownerID, name, dataURL, stamp.Width, stamp.Height, stamp.InsertWidth, stamp.AspectRatio, createdAt, now)
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
		InsertWidth: stamp.InsertWidth,
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

// DeleteDocumentRevision atomically rejects stale proposals, including missing documents.
func (s *Store) DeleteDocumentRevision(ctx context.Context, ownerID, id string, revision int64) error {
	if revision < 1 {
		return ErrDocumentConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM documents WHERE owner_id=? AND id=? AND revision=?`, ownerID, id, revision)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrDocumentConflict
	}
	if err = invalidateAIChatDocumentReaders(ctx, tx, ownerID, []string{id}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrateDocumentRevision(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(documents)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, required, pk int
		var name, typ string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &required, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "revision" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		_, err = s.db.ExecContext(ctx, `ALTER TABLE documents ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`)
	}
	return err
}

func (s *Store) migrateDocumentStampWidth(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(document_stamps)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, required, pk int
		var name, typ string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &required, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		found = found || name == "insert_width"
	}
	err = rows.Err()
	rows.Close()
	if err != nil || found {
		return err
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE document_stamps ADD COLUMN insert_width INTEGER NOT NULL DEFAULT 40`)
	return err
}
