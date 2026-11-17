package mailstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const MaxDocumentAttachmentBytes = 10 * 1024 * 1024
const DocumentAttachmentURLPrefix = "/api/document-attachments/"
const storedCanvasPrefix = "__INBRIX_CANVAS_DOCUMENT__:"

var ErrDocumentAttachmentInvalid = errors.New("invalid document image attachment")

// Images are immutable and owned by a user, not by the editable product, stamp
// or template that supplied them. Removing those sources must not break a saved
// quotation or contract. Identical bytes are stored once per owner.
type DocumentAttachment struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
}

type documentAttachmentDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func documentImageType(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxDocumentAttachmentBytes {
		return "", fmt.Errorf("%w: image must be between 1 byte and 10 MiB", ErrDocumentAttachmentInvalid)
	}
	if config, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
			return "", fmt.Errorf("%w: image exceeds 32 megapixels", ErrDocumentAttachmentInvalid)
		}
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return "", fmt.Errorf("%w: image cannot be decoded", ErrDocumentAttachmentInvalid)
		}
		return "image/" + format, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	root := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if root {
				return "image/svg+xml", nil
			}
			break
		}
		if err != nil {
			break
		}
		if start, ok := token.(xml.StartElement); ok && !root {
			if start.Name.Local != "svg" {
				break
			}
			root = true
		}
	}
	return "", fmt.Errorf("%w: unsupported or malformed image", ErrDocumentAttachmentInvalid)
}

func saveDocumentAttachment(ctx context.Context, db documentAttachmentDB, owner string, data []byte) (*DocumentAttachment, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}
	mime, err := documentImageType(data)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	hash.Write([]byte(owner + "\x00"))
	hash.Write(data)
	id := "dimg_" + hex.EncodeToString(hash.Sum(nil))
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO document_attachments(id,owner_id,content_type,data) VALUES(?,?,?,?)`, id, owner, mime, data)
	if err != nil {
		return nil, err
	}
	return &DocumentAttachment{ID: id, URL: DocumentAttachmentURLPrefix + id, ContentType: mime, Size: len(data)}, nil
}

func (s *Store) SaveDocumentAttachment(ctx context.Context, owner string, data []byte) (*DocumentAttachment, error) {
	return saveDocumentAttachment(ctx, s.db, owner, data)
}

func (s *Store) GetDocumentAttachment(ctx context.Context, owner, id string) ([]byte, string, error) {
	var data []byte
	var mime string
	err := s.db.QueryRowContext(ctx, `SELECT data,content_type FROM document_attachments WHERE owner_id=? AND id=?`, owner, id).Scan(&data, &mime)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return data, mime, err
}

func documentImageReference(ctx context.Context, db documentAttachmentDB, owner, source string) (string, error) {
	if strings.HasPrefix(source, DocumentAttachmentURLPrefix) {
		var id string
		err := db.QueryRowContext(ctx, `SELECT id FROM document_attachments WHERE owner_id=? AND id=?`, owner, strings.TrimPrefix(source, DocumentAttachmentURLPrefix)).Scan(&id)
		if err != nil {
			return "", fmt.Errorf("%w: attachment not found", ErrDocumentAttachmentInvalid)
		}
		return source, nil
	}
	if !strings.HasPrefix(source, "data:image/") {
		return source, nil
	}
	meta, encoded, ok := strings.Cut(source, ",")
	if !ok || len(source) > MaxDocumentAttachmentBytes*4/3+1024 {
		return "", fmt.Errorf("%w: invalid or oversized data URL", ErrDocumentAttachmentInvalid)
	}
	var data []byte
	var err error
	if strings.HasSuffix(meta, ";base64") {
		data, err = base64.StdEncoding.DecodeString(encoded)
	} else {
		var decoded string
		decoded, err = url.PathUnescape(encoded)
		data = []byte(decoded)
	}
	if err != nil {
		return "", fmt.Errorf("%w: invalid data URL", ErrDocumentAttachmentInvalid)
	}
	attachment, err := saveDocumentAttachment(ctx, db, owner, data)
	if err != nil {
		return "", err
	}
	return attachment.URL, nil
}

func externalizeDocumentImages(ctx context.Context, db documentAttachmentDB, owner, body string) (string, error) {
	if !strings.HasPrefix(body, storedCanvasPrefix) || (!strings.Contains(body, "data:image/") && !strings.Contains(body, DocumentAttachmentURLPrefix)) {
		return body, nil
	}
	var payload any
	decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(body, storedCanvasPrefix)))
	decoder.UseNumber() // Preserve unknown editor properties and integer identities.
	if err := decoder.Decode(&payload); err != nil {
		return "", err
	}
	cache := map[string]string{}
	changed := false
	var walk func(any) (any, error)
	walk = func(node any) (any, error) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				next, err := walk(child)
				if err != nil {
					return nil, err
				}
				value[key] = next
			}
		case []any:
			for i, child := range value {
				next, err := walk(child)
				if err != nil {
					return nil, err
				}
				value[i] = next
			}
		case string:
			if strings.HasPrefix(value, "data:image/") || strings.HasPrefix(value, DocumentAttachmentURLPrefix) {
				if ref, ok := cache[value]; ok {
					return ref, nil
				}
				ref, err := documentImageReference(ctx, db, owner, value)
				if err != nil {
					return nil, err
				}
				cache[value] = ref
				changed = changed || ref != value
				return ref, nil
			}
		}
		return node, nil
	}
	if _, err := walk(payload); err != nil {
		return "", err
	}
	if !changed {
		return body, nil
	}
	raw, err := json.Marshal(payload)
	return storedCanvasPrefix + string(raw), err
}

// ExternalizeDocumentImages also handles imported DOCX images, backgrounds,
// watermarks and row prototypes; it must not discard unknown snapshot fields.
func (s *Store) ExternalizeDocumentImages(ctx context.Context, owner, body string) (string, error) {
	if !strings.Contains(body, "data:image/") && !strings.Contains(body, DocumentAttachmentURLPrefix) {
		return body, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	next, err := externalizeDocumentImages(ctx, tx, owner, body)
	if err != nil {
		return "", err
	}
	return next, tx.Commit()
}

// Storage-only migration: compare the original body so a concurrent edit cannot
// be overwritten, and leave business revisions/timestamps untouched.
func (s *Store) compactStoredDocumentImages(ctx context.Context, owner, id, body, table, column string) (string, error) {
	if !strings.Contains(body, "data:image/") {
		return body, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var next string
	if table == "document_stamps" {
		next, err = documentImageReference(ctx, tx, owner, body)
	} else {
		next, err = externalizeDocumentImages(ctx, tx, owner, body)
	}
	if err != nil {
		return "", err
	}
	if next != body {
		// table/column are internal constants, never request parameters.
		_, err = tx.ExecContext(ctx, `UPDATE `+table+` SET `+column+`=? WHERE owner_id=? AND id=? AND `+column+`=?`, next, owner, id, body)
		if err != nil {
			return "", err
		}
	}
	return next, tx.Commit()
}
