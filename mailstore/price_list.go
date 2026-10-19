package mailstore

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A price list is a spreadsheet-shaped table: the user decides which columns
// exist, so nothing about the shape is fixed in the schema. What IS fixed is
// the small set of optional column *roles* below. Roles are what keep a
// free-form table machine-readable — the quotation editor and the AI tools
// read "the price column", not "column three", so a user can rename, reorder,
// or add columns without breaking pricing.

// Price column roles. Empty means an ordinary, uninterpreted column.
const (
	PriceRoleModel    = "model"     // 型号 / SKU / part number: the lookup key
	PriceRoleName     = "name"      // human-readable product name
	PriceRoleUnit     = "unit"      // unit of measure
	PriceRolePrice    = "price"     // authoritative unit price
	PriceRoleCurrency = "currency"  // ISO code or symbol
	PriceRoleTax      = "tax"       // tax rate, 0.13 or "13%"
	PriceRoleMOQ      = "moq"       // minimum order quantity
	PriceRoleLeadTime = "lead_time" // lead time, in days by convention
)

// Price column value types drive the editor's input and sorting.
const (
	PriceColumnText   = "text"
	PriceColumnNumber = "number"
	PriceColumnDate   = "date"
	PriceColumnSelect = "select"
)

var (
	priceColumnRoles = map[string]bool{
		"": true, PriceRoleModel: true, PriceRoleName: true, PriceRoleUnit: true,
		PriceRolePrice: true, PriceRoleCurrency: true, PriceRoleTax: true,
		PriceRoleMOQ: true, PriceRoleLeadTime: true,
	}
	priceColumnTypes = map[string]bool{
		PriceColumnText: true, PriceColumnNumber: true, PriceColumnDate: true, PriceColumnSelect: true,
	}
	// Column keys end up in JSON paths (json_extract) and in the row payload,
	// so they are restricted to a conservative identifier shape.
	priceColumnKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	priceRoleColumns      = map[string]bool{
		PriceRoleModel: true, PriceRoleName: true, PriceRoleUnit: true, PriceRolePrice: true,
		PriceRoleCurrency: true, PriceRoleTax: true, PriceRoleMOQ: true, PriceRoleLeadTime: true,
	}
)

// PriceList is one user-owned price table.
type PriceList struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"ownerId,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     int64     `json:"version"`
	RowCount    int       `json:"rowCount"`
	ColumnCount int       `json:"columnCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PriceColumn is one user-defined column. Key is the stable identity used by
// row data; Label is what the user sees and may rename freely.
type PriceColumn struct {
	ID       string   `json:"id"`
	ListID   string   `json:"listId,omitempty"`
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Role     string   `json:"role"`
	Width    int      `json:"width"`
	Position int      `json:"position"`
	Options  []string `json:"options,omitempty"`
}

// PriceRow is one row: a sparse map of column key to cell text. Cells are
// stored as text because a spreadsheet's cells are text until a role gives
// them meaning; numeric roles are parsed when pricing.
type PriceRow struct {
	ID        string            `json:"id"`
	ListID    string            `json:"listId,omitempty"`
	Position  int               `json:"position"`
	Data      map[string]string `json:"data"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

// PriceLookupEntry is one authoritative price resolved from a price list. It is
// the shape the quotation editor and the AI tools consume.
type PriceLookupEntry struct {
	ListID    string            `json:"listId"`
	ListName  string            `json:"listName"`
	RowID     string            `json:"rowId"`
	Model     string            `json:"model"`
	Name      string            `json:"name"`
	Unit      string            `json:"unit"`
	Currency  string            `json:"currency"`
	Price     float64           `json:"price"`
	PriceText string            `json:"priceText"`
	TaxRate   float64           `json:"taxRate"`
	MOQ       string            `json:"moq,omitempty"`
	LeadTime  string            `json:"leadTime,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// PriceImportResult reports what a CSV import did (or would do, when dryRun).
type PriceImportResult struct {
	Headers        []string      `json:"headers"`
	Added          int           `json:"added"`
	Updated        int           `json:"updated"`
	Skipped        int           `json:"skipped"`
	CreatedColumns []PriceColumn `json:"createdColumns,omitempty"`
	Preview        [][]string    `json:"preview,omitempty"`
	Errors         []string      `json:"errors,omitempty"`
}

func normalizePriceColumnType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if priceColumnTypes[value] {
		return value
	}
	return PriceColumnText
}

func normalizePriceColumnRole(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if priceColumnRoles[value] {
		return value
	}
	return ""
}

func normalizePriceColumnWidth(width int) int {
	if width < 60 {
		return 160
	}
	if width > 900 {
		return 900
	}
	return width
}

// DefaultPriceColumns is the starting shape for a new list. The frontend sends
// its own localized labels; this is the server-side fallback (and what tests
// and the import path use when a list must exist without columns).
func DefaultPriceColumns() []PriceColumn {
	return []PriceColumn{
		{Key: "model", Label: "Model", Type: PriceColumnText, Role: PriceRoleModel, Width: 180},
		{Key: "name", Label: "Name", Type: PriceColumnText, Role: PriceRoleName, Width: 220},
		{Key: "unit", Label: "Unit", Type: PriceColumnText, Role: PriceRoleUnit, Width: 90},
		{Key: "price", Label: "Unit price", Type: PriceColumnNumber, Role: PriceRolePrice, Width: 120},
		{Key: "currency", Label: "Currency", Type: PriceColumnText, Role: PriceRoleCurrency, Width: 90},
		{Key: "tax", Label: "Tax rate", Type: PriceColumnNumber, Role: PriceRoleTax, Width: 90},
		{Key: "moq", Label: "Min qty", Type: PriceColumnNumber, Role: PriceRoleMOQ, Width: 90},
		{Key: "lead_time", Label: "Lead time (days)", Type: PriceColumnNumber, Role: PriceRoleLeadTime, Width: 120},
		{Key: "notes", Label: "Notes", Type: PriceColumnText, Width: 200},
	}
}

func (s *Store) priceListOwnedBy(ctx context.Context, ownerID, listID string) error {
	ownerID = strings.TrimSpace(ownerID)
	listID = strings.TrimSpace(listID)
	if ownerID == "" || listID == "" {
		return ErrNotFound
	}
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM price_lists WHERE id = ? AND owner_id = ?`, listID, ownerID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("mailstore: get price list: %w", err)
	}
	return nil
}

func (s *Store) touchPriceList(ctx context.Context, tx *sql.Tx, listID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE price_lists SET version = version + 1, updated_at = ? WHERE id = ?`, time.Now().Unix(), listID)
	if err != nil {
		return fmt.Errorf("mailstore: touch price list: %w", err)
	}
	return nil
}

func priceColumnsForList(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, listID string) ([]PriceColumn, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, list_id, column_key, label, type, role, width, position, options_json
		FROM price_list_columns WHERE list_id = ? ORDER BY position ASC, id ASC`, listID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list price columns: %w", err)
	}
	defer rows.Close()
	return scanPriceColumns(rows)
}

func scanPriceColumns(rows *sql.Rows) ([]PriceColumn, error) {
	columns := make([]PriceColumn, 0)
	for rows.Next() {
		var column PriceColumn
		var optionsJSON string
		if err := rows.Scan(&column.ID, &column.ListID, &column.Key, &column.Label, &column.Type, &column.Role, &column.Width, &column.Position, &optionsJSON); err != nil {
			return nil, fmt.Errorf("mailstore: scan price column: %w", err)
		}
		if strings.TrimSpace(optionsJSON) != "" {
			_ = json.Unmarshal([]byte(optionsJSON), &column.Options)
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

// ListPriceLists returns the owner's price tables, newest first, with the row
// and column counts the picker shows.
func (s *Store) ListPriceLists(ctx context.Context, ownerID string) ([]PriceList, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.owner_id, l.name, l.description, l.version, l.created_at, l.updated_at,
		       (SELECT COUNT(*) FROM price_list_rows r WHERE r.list_id = l.id),
		       (SELECT COUNT(*) FROM price_list_columns c WHERE c.list_id = l.id)
		FROM price_lists l WHERE l.owner_id = ?
		ORDER BY l.updated_at DESC, l.id DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list price lists: %w", err)
	}
	defer rows.Close()
	lists := make([]PriceList, 0)
	for rows.Next() {
		var list PriceList
		var createdAt, updatedAt int64
		if err := rows.Scan(&list.ID, &list.OwnerID, &list.Name, &list.Description, &list.Version, &createdAt, &updatedAt, &list.RowCount, &list.ColumnCount); err != nil {
			return nil, fmt.Errorf("mailstore: scan price list: %w", err)
		}
		list.CreatedAt = time.Unix(createdAt, 0).UTC()
		list.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

// GetPriceList returns one price table and its column definitions.
func (s *Store) GetPriceList(ctx context.Context, ownerID, listID string) (PriceList, []PriceColumn, error) {
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return PriceList{}, nil, err
	}
	var list PriceList
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, owner_id, name, description, version, created_at, updated_at
		FROM price_lists WHERE id = ? AND owner_id = ?`, listID, ownerID).
		Scan(&list.ID, &list.OwnerID, &list.Name, &list.Description, &list.Version, &createdAt, &updatedAt)
	if err != nil {
		return PriceList{}, nil, fmt.Errorf("mailstore: get price list: %w", err)
	}
	list.CreatedAt = time.Unix(createdAt, 0).UTC()
	list.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	columns, err := priceColumnsForList(ctx, s.db, list.ID)
	if err != nil {
		return PriceList{}, nil, err
	}
	list.ColumnCount = len(columns)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_list_rows WHERE list_id = ?`, list.ID).Scan(&list.RowCount); err != nil {
		return PriceList{}, nil, fmt.Errorf("mailstore: count price rows: %w", err)
	}
	return list, columns, nil
}

// CreatePriceList creates a table, optionally with caller-supplied columns
// (the frontend sends localized default labels). Without columns the
// server-side default shape is used so the table is immediately usable.
func (s *Store) CreatePriceList(ctx context.Context, ownerID, name, description string, columns []PriceColumn) (PriceList, []PriceColumn, error) {
	s.priceMu.Lock()
	defer s.priceMu.Unlock()
	ownerID = strings.TrimSpace(ownerID)
	name = strings.TrimSpace(name)
	if ownerID == "" {
		return PriceList{}, nil, errors.New("mailstore: owner_id is required")
	}
	if name == "" {
		return PriceList{}, nil, errors.New("mailstore: price list name is required")
	}
	if len([]rune(name)) > 120 {
		return PriceList{}, nil, errors.New("mailstore: price list name is too long")
	}
	if len(columns) == 0 {
		columns = DefaultPriceColumns()
	}
	id, err := newID("plist")
	if err != nil {
		return PriceList{}, nil, err
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceList{}, nil, fmt.Errorf("mailstore: create price list: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO price_lists(id, owner_id, name, description, version, created_at, updated_at)
		VALUES(?, ?, ?, ?, 1, ?, ?)`, id, ownerID, name, strings.TrimSpace(description), now, now); err != nil {
		return PriceList{}, nil, fmt.Errorf("mailstore: create price list: %w", err)
	}
	saved, err := savePriceColumnsTx(ctx, tx, id, columns)
	if err != nil {
		return PriceList{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return PriceList{}, nil, fmt.Errorf("mailstore: create price list: %w", err)
	}
	list, _, err := s.GetPriceList(ctx, ownerID, id)
	if err != nil {
		return PriceList{}, nil, err
	}
	return list, saved, nil
}

// UpdatePriceList renames a table or changes its description.
func (s *Store) UpdatePriceList(ctx context.Context, ownerID, listID, name, description string) (PriceList, error) {
	s.priceMu.Lock()
	defer s.priceMu.Unlock()
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return PriceList{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return PriceList{}, errors.New("mailstore: price list name is required")
	}
	if len([]rune(name)) > 120 {
		return PriceList{}, errors.New("mailstore: price list name is too long")
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE price_lists SET name = ?, description = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND owner_id = ?`, name, strings.TrimSpace(description), time.Now().Unix(), listID, ownerID); err != nil {
		return PriceList{}, fmt.Errorf("mailstore: update price list: %w", err)
	}
	list, _, err := s.GetPriceList(ctx, ownerID, listID)
	return list, err
}

// DeletePriceList removes a table, its columns, and its rows.
func (s *Store) DeletePriceList(ctx context.Context, ownerID, listID string) error {
	s.priceMu.Lock()
	defer s.priceMu.Unlock()
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mailstore: delete price list: %w", err)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DELETE FROM price_list_rows WHERE list_id = ?`,
		`DELETE FROM price_list_columns WHERE list_id = ?`,
		`DELETE FROM price_lists WHERE id = ? AND owner_id = ?`,
	} {
		args := []any{listID}
		if strings.Contains(statement, "owner_id") {
			args = append(args, ownerID)
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("mailstore: delete price list: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mailstore: delete price list: %w", err)
	}
	return nil
}

// SavePriceColumns replaces the column set of a table. Columns are matched by
// key, so renaming a label or moving a column keeps its data; removing a column
// drops its values from every row.
func (s *Store) SavePriceColumns(ctx context.Context, ownerID, listID string, columns []PriceColumn) ([]PriceColumn, error) {
	s.priceMu.Lock()
	defer s.priceMu.Unlock()
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, errors.New("mailstore: a price list needs at least one column")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mailstore: save price columns: %w", err)
	}
	defer tx.Rollback()

	existing, err := priceColumnsForList(ctx, tx, listID)
	if err != nil {
		return nil, err
	}
	kept := make(map[string]bool, len(columns))
	for _, column := range columns {
		if key := strings.TrimSpace(column.Key); priceColumnKeyPattern.MatchString(key) {
			kept[key] = true
		}
	}
	if _, err := savePriceColumnsTx(ctx, tx, listID, columns); err != nil {
		return nil, err
	}
	// Prune values for columns the user deleted so rows do not accumulate dead
	// keys that would still match a search.
	for _, column := range existing {
		if kept[column.Key] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_list_rows SET data_json = json_remove(data_json, ?) WHERE list_id = ?`, "$."+column.Key, listID); err != nil {
			return nil, fmt.Errorf("mailstore: prune removed price column: %w", err)
		}
	}
	if err := rebuildPriceSearchText(ctx, tx, listID); err != nil {
		return nil, err
	}
	if err := s.touchPriceList(ctx, tx, listID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("mailstore: save price columns: %w", err)
	}
	saved, err := priceColumnsForList(ctx, s.db, listID)
	return saved, err
}

func savePriceColumnsTx(ctx context.Context, tx *sql.Tx, listID string, columns []PriceColumn) ([]PriceColumn, error) {
	seen := make(map[string]bool, len(columns))
	normalized := make([]PriceColumn, 0, len(columns))
	for index, column := range columns {
		key := strings.TrimSpace(column.Key)
		if !priceColumnKeyPattern.MatchString(key) {
			generated, err := newID("pcol")
			if err != nil {
				return nil, err
			}
			key = strings.ReplaceAll(generated, "-", "")
			if !priceColumnKeyPattern.MatchString(key) {
				key = fmt.Sprintf("column_%d", index+1)
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		label := strings.TrimSpace(column.Label)
		if label == "" {
			label = key
		}
		if len([]rune(label)) > 80 {
			label = string([]rune(label)[:80])
		}
		normalized = append(normalized, PriceColumn{
			Key:      key,
			Label:    label,
			Type:     normalizePriceColumnType(column.Type),
			Role:     normalizePriceColumnRole(column.Role),
			Width:    normalizePriceColumnWidth(column.Width),
			Position: index,
			Options:  column.Options,
		})
	}
	if len(normalized) == 0 {
		return nil, errors.New("mailstore: a price list needs at least one column")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM price_list_columns WHERE list_id = ?`, listID); err != nil {
		return nil, fmt.Errorf("mailstore: replace price columns: %w", err)
	}
	for _, column := range normalized {
		id, err := newID("pcol")
		if err != nil {
			return nil, err
		}
		optionsJSON, err := json.Marshal(column.Options)
		if err != nil {
			optionsJSON = []byte("[]")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO price_list_columns(id, list_id, column_key, label, type, role, width, position, options_json)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, listID, column.Key, column.Label, column.Type, column.Role, column.Width, column.Position, string(optionsJSON)); err != nil {
			return nil, fmt.Errorf("mailstore: insert price column: %w", err)
		}
	}
	return normalized, nil
}

// rebuildPriceSearchText recomputes the searchable projection of every row in a
// list. Rows are small, so a full rebuild after a structural change is cheap
// and keeps search honest.
func rebuildPriceSearchText(ctx context.Context, tx *sql.Tx, listID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, data_json FROM price_list_rows WHERE list_id = ?`, listID)
	if err != nil {
		return fmt.Errorf("mailstore: rebuild price search: %w", err)
	}
	type rowValue struct {
		id   string
		data string
	}
	pending := make([]rowValue, 0)
	for rows.Next() {
		var value rowValue
		if err := rows.Scan(&value.id, &value.data); err != nil {
			rows.Close()
			return fmt.Errorf("mailstore: rebuild price search: %w", err)
		}
		pending = append(pending, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, value := range pending {
		var data map[string]string
		if err := json.Unmarshal([]byte(value.data), &data); err != nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_list_rows SET search_text = ? WHERE id = ?`, priceSearchText(data), value.id); err != nil {
			return fmt.Errorf("mailstore: rebuild price search: %w", err)
		}
	}
	return nil
}

// priceSearchText projects a row onto the text a query is matched against.
func priceSearchText(data map[string]string) string {
	if len(data) == 0 {
		return ""
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(data[key])
		if value != "" {
			values = append(values, value)
		}
	}
	text := strings.ToLower(strings.Join(values, " \u0001 "))
	if utf8.RuneCountInString(text) > 4000 {
		text = string([]rune(text)[:4000])
	}
	return text
}

func sanitizePriceRowData(data map[string]string, allowed map[string]bool) (map[string]string, error) {
	clean := make(map[string]string, len(data))
	for key, value := range data {
		key = strings.TrimSpace(key)
		if !priceColumnKeyPattern.MatchString(key) {
			continue
		}
		// Rows only carry values for columns that currently exist, so a deleted
		// column cannot live on as invisible data in every row.
		if allowed != nil && !allowed[key] {
			continue
		}
		value = strings.TrimSpace(value)
		if len([]rune(value)) > 2000 {
			value = string([]rune(value)[:2000])
		}
		clean[key] = value
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return nil, fmt.Errorf("mailstore: encode price row: %w", err)
	}
	if len(encoded) > 64*1024 {
		return nil, errors.New("mailstore: price row is too large")
	}
	return clean, nil
}

// normalizePriceNumberCell rewrites what a user typed into the canonical number
// a number column should hold, so sorting and pricing do not depend on whether
// someone typed "1,280.00", "¥1980", or "1280". A rate keeps its own unit
// ("13%" stays "13%"), and text that is not a number at all is left untouched
// rather than silently discarded.
func normalizePriceNumberCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	percent := strings.HasSuffix(value, "%")
	body := strings.TrimSuffix(value, "%")
	var builder strings.Builder
	for _, r := range body {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' || r == 'e' || r == 'E' {
			builder.WriteRune(r)
		}
	}
	cleaned := builder.String()
	if cleaned == "" {
		return value
	}
	if _, err := strconv.ParseFloat(cleaned, 64); err != nil {
		return value
	}
	if percent {
		return cleaned + "%"
	}
	return cleaned
}

// normalizePriceRowData applies the number-column normalization of one list.
func normalizePriceRowData(data map[string]string, numberColumns map[string]bool) map[string]string {
	if len(numberColumns) == 0 {
		return data
	}
	for key := range numberColumns {
		if value, exists := data[key]; exists {
			data[key] = normalizePriceNumberCell(value)
		}
	}
	return data
}

// ListPriceRows returns a page of rows, optionally filtered by free text and
// sorted by any user-defined column.
func (s *Store) ListPriceRows(ctx context.Context, ownerID, listID, query, sortKey, sortDir string, limit, offset int) ([]PriceRow, int, error) {
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	if offset < 0 {
		offset = 0
	}

	clauses := []string{"list_id = ?"}
	args := []any{listID}
	for _, term := range strings.Fields(strings.ToLower(strings.TrimSpace(query))) {
		clauses = append(clauses, "search_text LIKE ?")
		args = append(args, "%"+term+"%")
	}
	where := strings.Join(clauses, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_list_rows WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("mailstore: count price rows: %w", err)
	}

	order := "position ASC, id ASC"
	if sortKey = strings.TrimSpace(sortKey); priceColumnKeyPattern.MatchString(sortKey) {
		columns, err := priceColumnsForList(ctx, s.db, listID)
		if err != nil {
			return nil, 0, err
		}
		columnType := ""
		for _, column := range columns {
			if column.Key == sortKey {
				columnType = column.Type
				break
			}
		}
		expression := fmt.Sprintf("json_extract(data_json, '$.%s')", sortKey)
		if columnType == PriceColumnNumber {
			expression = "CAST(" + expression + " AS REAL)"
		}
		direction := "ASC"
		if strings.EqualFold(strings.TrimSpace(sortDir), "desc") {
			direction = "DESC"
		}
		order = fmt.Sprintf("%s %s, position ASC, id ASC", expression, direction)
	}

	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, list_id, position, data_json, updated_at
		FROM price_list_rows WHERE `+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("mailstore: list price rows: %w", err)
	}
	defer rows.Close()
	result := make([]PriceRow, 0)
	for rows.Next() {
		var row PriceRow
		var dataJSON string
		var updatedAt int64
		if err := rows.Scan(&row.ID, &row.ListID, &row.Position, &dataJSON, &updatedAt); err != nil {
			return nil, 0, fmt.Errorf("mailstore: scan price row: %w", err)
		}
		row.Data = map[string]string{}
		if strings.TrimSpace(dataJSON) != "" {
			_ = json.Unmarshal([]byte(dataJSON), &row.Data)
		}
		row.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		result = append(result, row)
	}
	return result, total, rows.Err()
}

// priceRowWritePlan is the per-list write context shared by the incremental row
// endpoints: which column keys may be written and which of them hold numbers.
type priceRowWritePlan struct {
	allowed       map[string]bool
	numberColumns map[string]bool
}

func (s *Store) priceRowWritePlan(ctx context.Context, listID string) (priceRowWritePlan, error) {
	columns, err := priceColumnsForList(ctx, s.db, listID)
	if err != nil {
		return priceRowWritePlan{}, err
	}
	plan := priceRowWritePlan{
		allowed:       make(map[string]bool, len(columns)),
		numberColumns: make(map[string]bool, len(columns)),
	}
	for _, column := range columns {
		plan.allowed[column.Key] = true
		if column.Type == PriceColumnNumber {
			plan.numberColumns[column.Key] = true
		}
	}
	return plan, nil
}

// cleanPriceCells keeps only writable keys and applies the list's numeric
// normalization, so an incremental write stores exactly what a full save would.
func (plan priceRowWritePlan) cleanPriceCells(cells map[string]string) map[string]string {
	clean := make(map[string]string, len(cells))
	for key, value := range cells {
		key = strings.TrimSpace(key)
		if !priceColumnKeyPattern.MatchString(key) || !plan.allowed[key] {
			continue
		}
		value = strings.TrimSpace(value)
		if len([]rune(value)) > 2000 {
			value = string([]rune(value)[:2000])
		}
		if plan.numberColumns[key] {
			value = normalizePriceNumberCell(value)
		}
		clean[key] = value
	}
	return clean
}

// AppendPriceRow inserts a single row and returns it with its server id, so the
// client can address later edits to that row instead of resending the sheet.
func (s *Store) AppendPriceRow(ctx context.Context, ownerID, listID string, data map[string]string) (PriceRow, error) {
	s.priceMu.Lock()
	defer s.priceMu.Unlock()
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return PriceRow{}, err
	}
	plan, err := s.priceRowWritePlan(ctx, listID)
	if err != nil {
		return PriceRow{}, err
	}
	clean := plan.cleanPriceCells(data)
	encoded, err := json.Marshal(clean)
	if err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: encode price row: %w", err)
	}
	if len(encoded) > 64*1024 {
		return PriceRow{}, errors.New("mailstore: price row is too large")
	}
	id, err := newID("prow")
	if err != nil {
		return PriceRow{}, err
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: append price row: %w", err)
	}
	defer tx.Rollback()
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM price_list_rows WHERE list_id = ?`, listID).Scan(&position); err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: append price row: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO price_list_rows(id, list_id, position, data_json, search_text, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, id, listID, position, string(encoded), priceSearchText(clean), now, now); err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: append price row: %w", err)
	}
	if err := s.touchPriceList(ctx, tx, listID); err != nil {
		return PriceRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: append price row: %w", err)
	}
	return PriceRow{ID: id, ListID: listID, Position: position, Data: clean, UpdatedAt: time.Unix(now, 0).UTC()}, nil
}

// UpdatePriceRowCells merges changed cells into one row. Only the keys the
// client sends are touched, so a cell-level autosave never rewrites (or
// clobbers) the rest of the row.
func (s *Store) UpdatePriceRowCells(ctx context.Context, ownerID, listID, rowID string, cells map[string]string) (PriceRow, error) {
	s.priceMu.Lock()
	defer s.priceMu.Unlock()
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return PriceRow{}, err
	}
	plan, err := s.priceRowWritePlan(ctx, listID)
	if err != nil {
		return PriceRow{}, err
	}
	clean := plan.cleanPriceCells(cells)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: update price row: %w", err)
	}
	defer tx.Rollback()

	var dataJSON string
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT data_json, position FROM price_list_rows WHERE list_id = ? AND id = ?`, listID, strings.TrimSpace(rowID)).Scan(&dataJSON, &position); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PriceRow{}, ErrNotFound
		}
		return PriceRow{}, fmt.Errorf("mailstore: update price row: %w", err)
	}
	data := map[string]string{}
	if strings.TrimSpace(dataJSON) != "" {
		_ = json.Unmarshal([]byte(dataJSON), &data)
	}
	for key, value := range clean {
		data[key] = value
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: encode price row: %w", err)
	}
	if len(encoded) > 64*1024 {
		return PriceRow{}, errors.New("mailstore: price row is too large")
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		UPDATE price_list_rows SET data_json = ?, search_text = ?, updated_at = ?
		WHERE list_id = ? AND id = ?`, string(encoded), priceSearchText(data), now, listID, rowID); err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: update price row: %w", err)
	}
	if err := s.touchPriceList(ctx, tx, listID); err != nil {
		return PriceRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return PriceRow{}, fmt.Errorf("mailstore: update price row: %w", err)
	}
	return PriceRow{ID: rowID, ListID: listID, Position: position, Data: data, UpdatedAt: time.Unix(now, 0).UTC()}, nil
}

// DeletePriceRow removes a single row.
func (s *Store) DeletePriceRow(ctx context.Context, ownerID, listID, rowID string) error {
	s.priceMu.Lock()
	defer s.priceMu.Unlock()
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mailstore: delete price row: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM price_list_rows WHERE list_id = ? AND id = ?`, listID, strings.TrimSpace(rowID))
	if err != nil {
		return fmt.Errorf("mailstore: delete price row: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	if err := s.touchPriceList(ctx, tx, listID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mailstore: delete price row: %w", err)
	}
	return nil
}

// SavePriceRows applies a grid edit: rows with an existing id are updated, rows
// without one are appended, and deletedIDs are removed. One version bump covers
// the whole edit so downstream caches (AI reply context) invalidate once.
func (s *Store) SavePriceRows(ctx context.Context, ownerID, listID string, rows []PriceRow, deletedIDs []string) (int, error) {
	s.priceMu.Lock()
	defer s.priceMu.Unlock()
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return 0, err
	}
	columns, err := priceColumnsForList(ctx, s.db, listID)
	if err != nil {
		return 0, err
	}
	allowed := make(map[string]bool, len(columns))
	numberColumns := make(map[string]bool, len(columns))
	for _, column := range columns {
		allowed[column.Key] = true
		if column.Type == PriceColumnNumber {
			numberColumns[column.Key] = true
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("mailstore: save price rows: %w", err)
	}
	defer tx.Rollback()

	var maxPosition int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) FROM price_list_rows WHERE list_id = ?`, listID).Scan(&maxPosition); err != nil {
		return 0, fmt.Errorf("mailstore: save price rows: %w", err)
	}
	now := time.Now().Unix()

	if len(deletedIDs) > 0 {
		for _, id := range deletedIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM price_list_rows WHERE list_id = ? AND id = ?`, listID, id); err != nil {
				return 0, fmt.Errorf("mailstore: delete price row: %w", err)
			}
		}
	}

	saved := 0
	for _, row := range rows {
		data, err := sanitizePriceRowData(normalizePriceRowData(row.Data, numberColumns), allowed)
		if err != nil {
			return 0, err
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			return 0, fmt.Errorf("mailstore: encode price row: %w", err)
		}
		search := priceSearchText(data)
		id := strings.TrimSpace(row.ID)
		if id != "" {
			result, err := tx.ExecContext(ctx, `
				UPDATE price_list_rows SET data_json = ?, search_text = ?, updated_at = ?
				WHERE list_id = ? AND id = ?`, string(encoded), search, now, listID, id)
			if err != nil {
				return 0, fmt.Errorf("mailstore: update price row: %w", err)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				saved++
				continue
			}
			// An unknown id means the row was deleted elsewhere; fall through and
			// re-insert it so the user's edit is not silently dropped.
		}
		newRowID, err := newID("prow")
		if err != nil {
			return 0, err
		}
		maxPosition++
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO price_list_rows(id, list_id, position, data_json, search_text, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?)`, newRowID, listID, maxPosition, string(encoded), search, now, now); err != nil {
			return 0, fmt.Errorf("mailstore: insert price row: %w", err)
		}
		saved++
	}
	if saved > 0 || len(deletedIDs) > 0 {
		if err := s.touchPriceList(ctx, tx, listID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("mailstore: save price rows: %w", err)
	}
	return saved, nil
}

// priceLookupColumns indexes the roled columns of each list so a lookup can
// pull the model, price, and currency out of arbitrary user-defined shapes.
func priceLookupColumns(columns []PriceColumn) map[string]PriceColumn {
	byRole := make(map[string]PriceColumn, len(columns))
	for _, column := range columns {
		if priceRoleColumns[column.Role] {
			if _, exists := byRole[column.Role]; !exists {
				byRole[column.Role] = column
			}
		}
	}
	return byRole
}

// ParsePriceNumber accepts the shapes a spreadsheet actually contains: "1,280",
// "¥1280.00", "13%", "1.28e3", and plain numbers.
func ParsePriceNumber(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	percent := strings.HasSuffix(value, "%")
	value = strings.TrimSuffix(value, "%")
	cleaned := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' || r == 'e' || r == 'E' {
			return r
		}
		return -1
	}, value)
	parsed, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0
	}
	if percent {
		return parsed / 100
	}
	return parsed
}

// LookupPrices resolves a free-text query against every price list the owner
// has, preferring an exact model match. This is the only pricing entry point
// the quotation editor and the AI tools use, so a price can never be invented
// on one path and authoritative on another.
func (s *Store) LookupPrices(ctx context.Context, ownerID, query string, limit int) ([]PriceLookupEntry, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, errors.New("mailstore: owner_id is required")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(terms) == 0 {
		return nil, nil
	}

	columnRows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.list_id, c.column_key, c.label, c.type, c.role, c.width, c.position, c.options_json
		FROM price_list_columns c JOIN price_lists l ON l.id = c.list_id
		WHERE l.owner_id = ? ORDER BY c.list_id, c.position`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: lookup price columns: %w", err)
	}
	columns, err := scanPriceColumns(columnRows)
	columnRows.Close()
	if err != nil {
		return nil, err
	}
	columnsByList := map[string][]PriceColumn{}
	for _, column := range columns {
		columnsByList[column.ListID] = append(columnsByList[column.ListID], column)
	}

	clauses := []string{"l.owner_id = ?"}
	args := []any{ownerID}
	for _, term := range terms {
		clauses = append(clauses, "r.search_text LIKE ?")
		args = append(args, "%"+term+"%")
	}
	args = append(args, limit*4)
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.list_id, l.name, r.data_json
		FROM price_list_rows r JOIN price_lists l ON l.id = r.list_id
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY l.updated_at DESC, r.position ASC, r.id ASC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("mailstore: lookup prices: %w", err)
	}
	defer rows.Close()

	type scored struct {
		entry PriceLookupEntry
		score int
		order int
	}
	found := make([]scored, 0)
	index := 0
	for rows.Next() {
		var rowID, listID, listName, dataJSON string
		if err := rows.Scan(&rowID, &listID, &listName, &dataJSON); err != nil {
			return nil, fmt.Errorf("mailstore: scan price lookup: %w", err)
		}
		index++
		byRole := priceLookupColumns(columnsByList[listID])
		if _, hasPrice := byRole[PriceRolePrice]; !hasPrice {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			continue
		}
		entry := PriceLookupEntry{ListID: listID, ListName: listName, RowID: rowID, Fields: data}
		if column, ok := byRole[PriceRoleModel]; ok {
			entry.Model = strings.TrimSpace(data[column.Key])
		}
		if column, ok := byRole[PriceRoleName]; ok {
			entry.Name = strings.TrimSpace(data[column.Key])
		}
		if column, ok := byRole[PriceRoleUnit]; ok {
			entry.Unit = strings.TrimSpace(data[column.Key])
		}
		if column, ok := byRole[PriceRoleCurrency]; ok {
			entry.Currency = strings.TrimSpace(data[column.Key])
		}
		if column, ok := byRole[PriceRolePrice]; ok {
			entry.PriceText = strings.TrimSpace(data[column.Key])
			entry.Price = ParsePriceNumber(entry.PriceText)
		}
		if column, ok := byRole[PriceRoleTax]; ok {
			entry.TaxRate = ParsePriceNumber(data[column.Key])
		}
		if column, ok := byRole[PriceRoleMOQ]; ok {
			entry.MOQ = strings.TrimSpace(data[column.Key])
		}
		if column, ok := byRole[PriceRoleLeadTime]; ok {
			entry.LeadTime = strings.TrimSpace(data[column.Key])
		}
		lowerQuery := strings.ToLower(strings.TrimSpace(query))
		lowerModel := strings.ToLower(entry.Model)
		score := 2
		if lowerModel != "" && lowerModel == lowerQuery {
			score = 0
		} else if lowerModel != "" && strings.Contains(lowerModel, lowerQuery) {
			score = 1
		}
		found = append(found, scored{entry: entry, score: score, order: index})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score < found[j].score
		}
		return found[i].order < found[j].order
	})
	if len(found) > limit {
		found = found[:limit]
	}
	result := make([]PriceLookupEntry, 0, len(found))
	for _, item := range found {
		result = append(result, item.entry)
	}
	return result, nil
}

// priceRoleHints maps the header names real price sheets use onto roles, so an
// imported spreadsheet becomes AI-readable without manual column setup.
var priceRoleHints = []struct {
	role    string
	needles []string
}{
	{PriceRoleModel, []string{"型号", "货号", "料号", "物料编码", "产品编码", "model", "sku", "part", "item no", "item_no", "code"}},
	{PriceRoleName, []string{"名称", "品名", "产品", "物料", "name", "product", "description"}},
	{PriceRoleUnit, []string{"单位", "unit", "uom"}},
	{PriceRolePrice, []string{"单价", "价格", "含税单价", "不含税单价", "price", "unit price", "unit_price", "rate"}},
	{PriceRoleCurrency, []string{"币种", "货币", "currency", "ccy"}},
	{PriceRoleTax, []string{"税率", "tax", "vat"}},
	{PriceRoleMOQ, []string{"起订量", "最小起订", "moq", "min qty", "min_qty", "minimum"}},
	{PriceRoleLeadTime, []string{"交期", "货期", "lead time", "lead_time", "delivery"}},
}

func inferPriceRole(label string) string {
	normalized := strings.ToLower(strings.TrimSpace(label))
	if normalized == "" {
		return ""
	}
	for _, hint := range priceRoleHints {
		for _, needle := range hint.needles {
			if strings.Contains(normalized, needle) {
				return hint.role
			}
		}
	}
	return ""
}

// detectPriceDelimiter picks the separator a pasted or uploaded table uses.
func detectPriceDelimiter(content string) rune {
	line := content
	if index := strings.IndexAny(content, "\r\n"); index >= 0 {
		line = content[:index]
	}
	counts := map[rune]int{',': strings.Count(line, ","), '\t': strings.Count(line, "\t"), ';': strings.Count(line, ";")}
	best := ','
	bestCount := -1
	for _, candidate := range []rune{',', '\t', ';'} {
		if counts[candidate] > bestCount {
			best, bestCount = candidate, counts[candidate]
		}
	}
	return best
}

// ImportPriceRows merges a CSV/TSV table into an existing price list. Headers
// are matched to columns by key or label; unknown headers create columns whose
// role is inferred from the header name. mode "upsert" updates the row whose
// model column matches instead of appending a duplicate.
func (s *Store) ImportPriceRows(ctx context.Context, ownerID, listID, content string, delimiter rune, createColumns bool, mode string, dryRun bool) (PriceImportResult, error) {
	if err := s.priceListOwnedBy(ctx, ownerID, listID); err != nil {
		return PriceImportResult{}, err
	}
	content = strings.TrimPrefix(content, "\ufeff")
	if strings.TrimSpace(content) == "" {
		return PriceImportResult{}, errors.New("mailstore: import content is empty")
	}
	if delimiter == 0 {
		delimiter = detectPriceDelimiter(content)
	}
	reader := csv.NewReader(strings.NewReader(content))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	records, err := reader.ReadAll()
	if err != nil {
		return PriceImportResult{}, fmt.Errorf("mailstore: parse import: %w", err)
	}
	if len(records) < 2 {
		return PriceImportResult{}, errors.New("mailstore: import needs a header row and at least one data row")
	}
	headers := make([]string, 0, len(records[0]))
	for _, header := range records[0] {
		headers = append(headers, strings.TrimSpace(header))
	}

	columns, err := priceColumnsForList(ctx, s.db, listID)
	if err != nil {
		return PriceImportResult{}, err
	}
	result := PriceImportResult{Headers: headers}

	// Resolve each header to a column, creating one when asked. Matching is
	// case-insensitive on key or label, and falls back to the column's role so
	// importing "单价" fills the existing price column even when it is labelled
	// "Unit price" — a duplicate column would break pricing silently.
	columnForHeader := make([]*PriceColumn, len(headers))
	usedKeys := make(map[string]bool, len(columns))
	mapped := make(map[string]bool, len(columns))
	for _, column := range columns {
		usedKeys[column.Key] = true
	}
	nextPosition := len(columns)
	unmapped := make([]string, 0)
	for index, header := range headers {
		if header == "" {
			continue
		}
		lower := strings.ToLower(header)
		for i := range columns {
			if mapped[columns[i].Key] {
				continue
			}
			if strings.ToLower(columns[i].Key) == lower || strings.ToLower(columns[i].Label) == lower {
				columnForHeader[index] = &columns[i]
				mapped[columns[i].Key] = true
				break
			}
		}
		if columnForHeader[index] == nil {
			if inferred := inferPriceRole(header); inferred != "" {
				for i := range columns {
					if mapped[columns[i].Key] || columns[i].Role != inferred {
						continue
					}
					columnForHeader[index] = &columns[i]
					mapped[columns[i].Key] = true
					break
				}
			}
		}
		if columnForHeader[index] != nil {
			continue
		}
		if !createColumns {
			unmapped = append(unmapped, header)
			continue
		}
		key := strings.ToLower(strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(header))
		cleaned := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
				return r
			}
			return -1
		}, key)
		if cleaned == "" || !priceColumnKeyPattern.MatchString(cleaned) {
			cleaned = fmt.Sprintf("column_%d", nextPosition+1)
		}
		for usedKeys[cleaned] {
			cleaned = cleaned + "_x"
			if !priceColumnKeyPattern.MatchString(cleaned) {
				cleaned = fmt.Sprintf("column_%d", nextPosition+1)
				break
			}
		}
		usedKeys[cleaned] = true
		role := inferPriceRole(header)
		columnType := PriceColumnText
		if role == PriceRolePrice || role == PriceRoleTax || role == PriceRoleMOQ || role == PriceRoleLeadTime {
			columnType = PriceColumnNumber
		}
		columns = append(columns, PriceColumn{Key: cleaned, Label: header, Type: columnType, Role: role, Width: 160, Position: nextPosition})
		nextPosition++
		mapped[cleaned] = true
		columnForHeader[index] = &columns[len(columns)-1]
		result.CreatedColumns = append(result.CreatedColumns, columns[len(columns)-1])
	}
	if len(unmapped) > 0 {
		result.Errors = append(result.Errors, "unmapped headers were ignored: "+strings.Join(unmapped, ", "))
	}

	// Existing model values, for upsert matching.
	existingByModel := map[string]string{}
	modelColumn := priceLookupColumns(columns)[PriceRoleModel]
	if strings.EqualFold(strings.TrimSpace(mode), "upsert") && modelColumn.Key != "" {
		rowValues, err := s.db.QueryContext(ctx, `SELECT id, data_json FROM price_list_rows WHERE list_id = ?`, listID)
		if err != nil {
			return PriceImportResult{}, fmt.Errorf("mailstore: load price rows for upsert: %w", err)
		}
		for rowValues.Next() {
			var id, dataJSON string
			if err := rowValues.Scan(&id, &dataJSON); err != nil {
				rowValues.Close()
				return PriceImportResult{}, err
			}
			var data map[string]string
			if json.Unmarshal([]byte(dataJSON), &data) == nil {
				if value := strings.ToLower(strings.TrimSpace(data[modelColumn.Key])); value != "" {
					existingByModel[value] = id
				}
			}
		}
		rowValues.Close()
	}

	pending := make([]PriceRow, 0, len(records)-1)
	for _, record := range records[1:] {
		data := map[string]string{}
		empty := true
		for index, value := range record {
			if index >= len(columnForHeader) || columnForHeader[index] == nil {
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			empty = false
			data[columnForHeader[index].Key] = value
		}
		if empty {
			result.Skipped++
			continue
		}
		if len(result.Preview) < 5 {
			result.Preview = append(result.Preview, record)
		}
		row := PriceRow{Data: data}
		if modelColumn.Key != "" {
			if existingID, ok := existingByModel[strings.ToLower(strings.TrimSpace(data[modelColumn.Key]))]; ok && data[modelColumn.Key] != "" {
				row.ID = existingID
				result.Updated++
			}
		}
		if row.ID == "" {
			result.Added++
		}
		pending = append(pending, row)
	}

	if dryRun {
		return result, nil
	}
	// Columns are structural, so persist them first; then the rows in one edit.
	if _, err := s.SavePriceColumns(ctx, ownerID, listID, columns); err != nil {
		return PriceImportResult{}, err
	}
	if _, err := s.SavePriceRows(ctx, ownerID, listID, pending, nil); err != nil {
		return PriceImportResult{}, err
	}
	return result, nil
}

// ExportPriceRows renders the whole table as CSV text (with a UTF-8 BOM so
// Excel opens CJK headers correctly).
func (s *Store) ExportPriceRows(ctx context.Context, ownerID, listID string) (string, string, error) {
	list, columns, err := s.GetPriceList(ctx, ownerID, listID)
	if err != nil {
		return "", "", err
	}
	var builder strings.Builder
	builder.WriteString("\ufeff")
	writer := csv.NewWriter(&builder)
	header := make([]string, 0, len(columns))
	for _, column := range columns {
		header = append(header, column.Label)
	}
	if err := writer.Write(header); err != nil {
		return "", "", fmt.Errorf("mailstore: export price rows: %w", err)
	}
	offset := 0
	for {
		rows, _, err := s.ListPriceRows(ctx, ownerID, listID, "", "", "", 1000, offset)
		if err != nil {
			return "", "", err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			record := make([]string, 0, len(columns))
			for _, column := range columns {
				record = append(record, row.Data[column.Key])
			}
			if err := writer.Write(record); err != nil {
				return "", "", fmt.Errorf("mailstore: export price rows: %w", err)
			}
		}
		offset += len(rows)
		if len(rows) < 1000 {
			break
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", "", fmt.Errorf("mailstore: export price rows: %w", err)
	}
	return builder.String(), list.Name, nil
}
