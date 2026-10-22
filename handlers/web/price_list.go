package web

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// PriceListHandler serves the spreadsheet-shaped price tables that quotations
// and the AI tools price against.
type PriceListHandler struct {
	store  *session.Store
	mailDB *mailstore.Store
}

func NewPriceListHandler(store *session.Store, mailDB *mailstore.Store) *PriceListHandler {
	return &PriceListHandler{store: store, mailDB: mailDB}
}

func (h *PriceListHandler) owner(c *fiber.Ctx) string {
	if h == nil {
		return ""
	}
	if userID, ok := c.Locals("user_id").(string); ok && strings.TrimSpace(userID) != "" {
		return strings.TrimSpace(userID)
	}
	if h.store != nil {
		sess, err := requestSession(c, h.store)
		if err == nil {
			owner, _ := sess.Get("user_id").(string)
			return strings.TrimSpace(owner)
		}
	}
	return ""
}

func (h *PriceListHandler) ready(c *fiber.Ctx) (string, error) {
	if h == nil || h.mailDB == nil {
		return "", fiber.NewError(fiber.StatusNotImplemented, "Local database unavailable")
	}
	owner := h.owner(c)
	if owner == "" {
		return "", fiber.ErrUnauthorized
	}
	return owner, nil
}

// priceListError maps store errors onto HTTP status codes so every endpoint
// answers consistently.
func priceListError(err error) error {
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "Price list not found")
	}
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.HasPrefix(message, "mailstore: ") {
		message = strings.TrimPrefix(message, "mailstore: ")
	}
	return fiber.NewError(fiber.StatusBadRequest, message)
}

type priceListSaveInput struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Columns     []mailstore.PriceColumn `json:"columns"`
}

// HandleListPriceLists returns the owner's price tables.
// Route: GET /api/price-lists
func (h *PriceListHandler) HandleListPriceLists(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	lists, err := h.mailDB.ListPriceLists(c.UserContext(), owner)
	if err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"priceLists": lists})
}

// HandleCreatePriceList creates a price table. The client sends localized
// default columns; without them the server-side default shape is used.
// Route: POST /api/price-lists
func (h *PriceListHandler) HandleCreatePriceList(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input priceListSaveInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	list, columns, err := h.mailDB.CreatePriceList(c.UserContext(), owner, input.Name, input.Description, input.Columns)
	if err != nil {
		return priceListError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"priceList": list, "columns": columns})
}

// HandleGetPriceList returns one table with its column definitions.
// Route: GET /api/price-lists/:id
func (h *PriceListHandler) HandleGetPriceList(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	list, columns, err := h.mailDB.GetPriceList(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"priceList": list, "columns": columns})
}

// HandleUpdatePriceList renames a table or edits its description.
// Route: PUT /api/price-lists/:id
func (h *PriceListHandler) HandleUpdatePriceList(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input priceListSaveInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	list, err := h.mailDB.UpdatePriceList(c.UserContext(), owner, c.Params("id"), input.Name, input.Description)
	if err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"priceList": list})
}

// HandleDeletePriceList removes a table with its rows and columns.
// Route: DELETE /api/price-lists/:id
func (h *PriceListHandler) HandleDeletePriceList(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	if err := h.mailDB.DeletePriceList(c.UserContext(), owner, c.Params("id")); err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

// HandleSavePriceColumns replaces the column set of a table: this single call
// covers adding, renaming, retyping, reordering, and deleting columns.
// Route: PUT /api/price-lists/:id/columns
func (h *PriceListHandler) HandleSavePriceColumns(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		Columns []mailstore.PriceColumn `json:"columns"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	columns, err := h.mailDB.SavePriceColumns(c.UserContext(), owner, c.Params("id"), input.Columns)
	if err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"columns": columns})
}

// HandleListPriceRows returns a page of rows, filtered and sorted server-side.
// Route: GET /api/price-lists/:id/rows?q=&sort=&dir=&limit=&offset=
func (h *PriceListHandler) HandleListPriceRows(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	limit := 200
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		if parsed, parseErr := strconv.Atoi(value); parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	offset := 0
	if value := strings.TrimSpace(c.Query("offset")); value != "" {
		if parsed, parseErr := strconv.Atoi(value); parseErr == nil && parsed >= 0 {
			offset = parsed
		}
	}
	rows, total, err := h.mailDB.ListPriceRows(c.UserContext(), owner, c.Params("id"), c.Query("q"), c.Query("sort"), c.Query("dir"), limit, offset)
	if err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"rows": rows, "total": total, "limit": limit, "offset": offset})
}

// HandleAppendPriceRow creates a single row and returns it with its server id,
// so the client can address later cell edits to that row.
// Route: POST /api/price-lists/:id/rows/append
func (h *PriceListHandler) HandleAppendPriceRow(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		Data map[string]string `json:"data"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	row, err := h.mailDB.AppendPriceRow(c.UserContext(), owner, c.Params("id"), input.Data)
	if err != nil {
		return priceListError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"row": row})
}

// HandleUpdatePriceRowCells merges only the cells the client changed. This is
// what an autosave on blur sends: one row, one or two cells.
// Route: PATCH /api/price-lists/:id/rows/:rowId
func (h *PriceListHandler) HandleUpdatePriceRowCells(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		Cells map[string]string `json:"cells"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if len(input.Cells) == 0 {
		return c.JSON(fiber.Map{"ok": true})
	}
	row, err := h.mailDB.UpdatePriceRowCells(c.UserContext(), owner, c.Params("id"), c.Params("rowId"), input.Cells)
	if err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"row": row})
}

// HandleDeletePriceRow removes a single row.
// Route: DELETE /api/price-lists/:id/rows/:rowId
func (h *PriceListHandler) HandleDeletePriceRow(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	if err := h.mailDB.DeletePriceRow(c.UserContext(), owner, c.Params("id"), c.Params("rowId")); err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

// HandleSavePriceRows applies a grid edit: updated rows, new rows, and deleted
// row ids in one transaction.
// Route: POST /api/price-lists/:id/rows
func (h *PriceListHandler) HandleSavePriceRows(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		Rows       []mailstore.PriceRow `json:"rows"`
		DeletedIDs []string             `json:"deletedIds"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if len(input.Rows) == 0 && len(input.DeletedIDs) == 0 {
		return c.JSON(fiber.Map{"ok": true, "saved": 0})
	}
	saved, err := h.mailDB.SavePriceRows(c.UserContext(), owner, c.Params("id"), input.Rows, input.DeletedIDs)
	if err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"ok": true, "saved": saved})
}

// HandleImportPriceRows merges a pasted or uploaded CSV/TSV table into a price
// list. dryRun reports what would happen without writing anything.
// Route: POST /api/price-lists/:id/import
func (h *PriceListHandler) HandleImportPriceRows(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		Content       string `json:"content"`
		Delimiter     string `json:"delimiter"`
		CreateColumns *bool  `json:"createColumns"`
		Mode          string `json:"mode"`
		DryRun        bool   `json:"dryRun"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	delimiter := rune(0)
	switch strings.TrimSpace(input.Delimiter) {
	case "", "auto":
		delimiter = 0
	case "\\t", "tab":
		delimiter = '\t'
	default:
		runes := []rune(input.Delimiter)
		if len(runes) != 1 {
			return fiber.NewError(fiber.StatusBadRequest, "delimiter must be a single character")
		}
		delimiter = runes[0]
	}
	createColumns := true
	if input.CreateColumns != nil {
		createColumns = *input.CreateColumns
	}
	result, err := h.mailDB.ImportPriceRows(c.UserContext(), owner, c.Params("id"), input.Content, delimiter, createColumns, input.Mode, input.DryRun)
	if err != nil {
		return priceListError(err)
	}
	return c.JSON(fiber.Map{"result": result})
}

// HandleExportPriceRows streams the whole table as CSV so the browser can save
// it directly, with a BOM that makes Excel read CJK headers correctly.
// Route: GET /api/price-lists/:id/export
func (h *PriceListHandler) HandleExportPriceRows(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	content, name, err := h.mailDB.ExportPriceRows(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return priceListError(err)
	}
	filename := strings.TrimSpace(name)
	if filename == "" {
		filename = "price-list"
	}
	c.Set(fiber.HeaderContentType, "text/csv; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename*=UTF-8''%s.csv", url.PathEscape(filename)))
	return c.SendString(content)
}

// HandleLookupPrices resolves models or free text against every price list the
// owner has. The quotation editor and the AI tools both price through here.
// Route: POST /api/price-lists/lookup
func (h *PriceListHandler) HandleLookupPrices(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
		Limit   int      `json:"limit"`
	}
	if err := c.BodyParser(&input); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	queries := make([]string, 0, len(input.Queries)+1)
	if strings.TrimSpace(input.Query) != "" {
		queries = append(queries, input.Query)
	}
	for _, query := range input.Queries {
		if strings.TrimSpace(query) != "" {
			queries = append(queries, query)
		}
	}
	if len(queries) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "query is required")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	seen := make(map[string]bool)
	entries := make([]mailstore.PriceLookupEntry, 0, limit)
	for _, query := range queries {
		found, err := h.mailDB.LookupPrices(c.UserContext(), owner, query, limit)
		if err != nil {
			return priceListError(err)
		}
		for _, entry := range found {
			if seen[entry.RowID] {
				continue
			}
			seen[entry.RowID] = true
			entries = append(entries, entry)
		}
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return c.JSON(fiber.Map{"entries": entries})
}
