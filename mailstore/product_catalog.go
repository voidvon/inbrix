package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Definitions describe dynamic attributes; stable keys survive display-label edits.
type ProductDimension struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Unit    string   `json:"unit"`
	Options []string `json:"options,omitempty"`
}
type ProductPrice struct {
	ID         string `json:"id"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	TaxRate    string `json:"taxRate"`
	TaxMode    string `json:"taxMode"`
	MinQty     string `json:"minQty"`
	MaxQty     string `json:"maxQty"`
	LeadTime   string `json:"leadTime"`
	Source     string `json:"source"`
	ValidFrom  string `json:"validFrom"`
	ValidUntil string `json:"validUntil"`
}
type ProductVariant struct {
	ID         string            `json:"id"`
	SKU        string            `json:"sku"`
	Attributes map[string]string `json:"attributes"`
	Prices     []ProductPrice    `json:"prices"`
}
type ProductCatalog struct {
	Dimensions []ProductDimension `json:"dimensions"`
	Variants   []ProductVariant   `json:"variants"`
}

func EmptyProductCatalog() *ProductCatalog {
	return &ProductCatalog{Dimensions: []ProductDimension{}, Variants: []ProductVariant{}}
}

func (s *Store) migrateProductCatalog(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS product_catalogs (product_id TEXT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE, dimensions_json TEXT NOT NULL, search_text TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS product_variants (id TEXT PRIMARY KEY, product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE, position INTEGER NOT NULL, sku TEXT NOT NULL, attributes_json TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_product_variants_product ON product_variants(product_id,position)`,
		`CREATE TABLE IF NOT EXISTS product_variant_prices (id TEXT PRIMARY KEY, variant_id TEXT NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE, position INTEGER NOT NULL, price_json TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_product_variant_prices_variant ON product_variant_prices(variant_id,position)`,
		// Original tables remain an archive. A resolved row is never silently re-imported.
		`CREATE TABLE IF NOT EXISTS product_price_migrations (row_id TEXT PRIMARY KEY, owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, product_id TEXT NOT NULL, variant_id TEXT NOT NULL, imported_at INTEGER NOT NULL)`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return s.migrateGlobalProductDimensions(ctx)
}

// Product prices always use renminbi. Currency is fixed response metadata;
// the field also validates legacy clients and stored records.
const ProductPriceCurrency = "CNY"

func isProductPriceCNY(currency string) bool {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "", "CNY", "RMB", "¥", "￥", "人民币":
		return true
	default:
		return false
	}
}

var catalogDecimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,6})?$`)

func decimal(value string, positive bool) (float64, error) {
	if !catalogDecimal.MatchString(value) {
		return 0, invalidProduct("expected a nonnegative decimal number (up to 6 decimal places)")
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || n > 1e12 || (positive && n <= 0) {
		return 0, invalidProduct("number out of range")
	}
	return n, nil
}
func validateCatalog(c *ProductCatalog) error {
	if c.Dimensions == nil {
		c.Dimensions = []ProductDimension{}
	}
	if c.Variants == nil {
		c.Variants = []ProductVariant{}
	}
	if encoded, err := json.Marshal(c); err != nil || len(encoded) > 1024*1024 {
		return invalidProduct("catalog exceeds 1 MiB")
	}

	if len(c.Dimensions) > 40 || len(c.Variants) > 500 {
		return invalidProduct("at most 40 dimensions and 500 variants per product")
	}
	keys, labels := map[string]bool{}, map[string]bool{}
	for i := range c.Dimensions {
		d := &c.Dimensions[i]
		d.Label = strings.TrimSpace(d.Label)
		d.Unit = strings.TrimSpace(d.Unit)
		label := strings.ToLower(d.Label)
		if !priceColumnKeyPattern.MatchString(d.Key) || keys[d.Key] || d.Label == "" || len(d.Label) > 200 || labels[label] || len(d.Unit) > 40 {
			return invalidProduct("invalid or duplicate dimension key/label")
		}
		if d.Type != "text" && d.Type != "number" && d.Type != "select" {
			return invalidProduct("dimension type must be text, number or select")
		}
		if len(d.Options) > 200 || (d.Type == "select" && len(d.Options) == 0) {
			return invalidProduct("select dimensions require 1–200 options")
		}
		opts := map[string]bool{}
		for _, o := range d.Options {
			if strings.TrimSpace(o) == "" || len(o) > 500 || opts[o] {
				return invalidProduct("invalid or duplicate select option")
			}
			opts[o] = true
		}
		keys[d.Key] = true
		labels[label] = true
	}
	combinations, skus, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i := range c.Variants {
		v := &c.Variants[i]
		v.SKU = strings.TrimSpace(v.SKU)
		if v.Attributes == nil {
			v.Attributes = map[string]string{}
		}
		if len(v.SKU) > 200 || (v.SKU != "" && skus[strings.ToLower(v.SKU)]) {
			return invalidProduct("duplicate or oversized variant SKU")
		}
		if v.SKU != "" {
			skus[strings.ToLower(v.SKU)] = true
		}
		if v.ID != "" {
			if ids[v.ID] {
				return invalidProduct("duplicate variant id")
			}
			ids[v.ID] = true
		}
		for key := range v.Attributes {
			if !keys[key] {
				return invalidProduct("unknown specification attribute: " + key)
			}
		}
		for _, d := range c.Dimensions {
			value := strings.TrimSpace(v.Attributes[d.Key])
			v.Attributes[d.Key] = value
			if value == "" || len(value) > 500 {
				return invalidProduct("every variant must specify " + d.Label)
			}
			if d.Type == "number" {
				n, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					return invalidProduct("invalid numeric attribute: " + d.Label)
				}
				if n == 0 {
					n = 0
				}
				v.Attributes[d.Key] = strconv.FormatFloat(n, 'g', -1, 64)
			}
			if d.Type == "select" {
				found := false
				for _, o := range d.Options {
					found = found || o == value
				}
				if !found {
					return invalidProduct("invalid option for " + d.Label)
				}
			}
		}
		canonical := map[string]string{}
		for key, value := range v.Attributes {
			canonical[key] = strings.ToLower(value)
		}
		encoded, _ := json.Marshal(canonical)
		combination := string(encoded)
		if combinations[combination] {
			return invalidProduct("duplicate specification combination")
		}
		combinations[combination] = true
		if len(v.Prices) > 50 {
			return invalidProduct("at most 50 prices per variant")
		}
		for j := range v.Prices {
			p := &v.Prices[j]
			if p.ID != "" {
				if ids[p.ID] {
					return invalidProduct("duplicate price id")
				}
				ids[p.ID] = true
			}
			if _, err := decimal(p.Amount, false); err != nil {
				return err
			}
			if !isProductPriceCNY(p.Currency) {
				return invalidProduct("product prices must use CNY")
			}
			p.Currency = ProductPriceCurrency
			if len(p.Source) > 500 || len(p.LeadTime) > 100 {
				return invalidProduct("price conditions are too long")
			}
			if p.TaxMode != "" && p.TaxMode != "included" && p.TaxMode != "excluded" {
				return invalidProduct("invalid tax mode")
			}
			if p.TaxMode != "" && p.TaxRate == "" {
				return invalidProduct("tax mode requires an explicit tax rate")
			}
			if p.TaxRate != "" {
				n, err := decimal(p.TaxRate, false)
				if err != nil || n > 1 {
					return invalidProduct("tax rate must be a decimal between 0 and 1")
				}
			}
			low := 0.0
			if p.MinQty != "" {
				n, err := decimal(p.MinQty, true)
				if err != nil {
					return err
				}
				low = n
			}
			if p.MaxQty != "" {
				n, err := decimal(p.MaxQty, true)
				if err != nil || n < low {
					return invalidProduct("invalid quantity interval")
				}
			}
			for _, day := range []string{p.ValidFrom, p.ValidUntil} {
				if day != "" {
					if _, err := time.Parse("2006-01-02", day); err != nil {
						return invalidProduct("invalid price validity date")
					}
				}
			}
			if p.ValidFrom != "" && p.ValidUntil != "" && p.ValidUntil < p.ValidFrom {
				return invalidProduct("invalid validity interval")
			}
		}
	}
	return nil
}

func saveProductCatalog(ctx context.Context, tx *sql.Tx, productID string, c *ProductCatalog) error {
	if err := resolveCatalogDimensions(ctx, tx, productID, c); err != nil {
		return err
	}
	if err := validateCatalog(c); err != nil {
		return err
	}
	// Supplied identifiers must already belong to this product. New rows receive server IDs.
	for _, v := range c.Variants {
		if v.ID != "" {
			var found string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM product_variants WHERE id=? AND product_id=?`, v.ID, productID).Scan(&found); err != nil {
				return invalidProduct("unknown variant id")
			}
		}
		for _, p := range v.Prices {
			if p.ID != "" {
				var found string
				if err := tx.QueryRowContext(ctx, `SELECT id FROM product_variant_prices WHERE id=? AND variant_id=?`, p.ID, v.ID).Scan(&found); err != nil {
					return invalidProduct("unknown price id")
				}
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM product_variants WHERE product_id=?`, productID); err != nil {
		return err
	}
	defs, err := json.Marshal(c.Dimensions)
	if err != nil {
		return err
	}
	var search []string
	for _, d := range c.Dimensions {
		search = append(search, d.Label)
	}
	for i := range c.Variants {
		v := &c.Variants[i]
		if v.ID == "" {
			v.ID, err = newID("pvar")
			if err != nil {
				return err
			}
		}
		attrs, _ := json.Marshal(v.Attributes)
		if _, err = tx.ExecContext(ctx, `INSERT INTO product_variants(id,product_id,position,sku,attributes_json) VALUES(?,?,?,?,?)`, v.ID, productID, i, v.SKU, string(attrs)); err != nil {
			return err
		}
		search = append(search, v.SKU, productModelKey(v.SKU))
		for _, d := range c.Dimensions {
			search = append(search, v.Attributes[d.Key])
		}
		for j := range v.Prices {
			p := &v.Prices[j]
			if p.ID == "" {
				p.ID, err = newID("pprice")
				if err != nil {
					return err
				}
			}
			b, _ := json.Marshal(p)
			if _, err = tx.ExecContext(ctx, `INSERT INTO product_variant_prices(id,variant_id,position,price_json) VALUES(?,?,?,?)`, p.ID, v.ID, j, string(b)); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO product_catalogs(product_id,dimensions_json,search_text) VALUES(?,?,?) ON CONFLICT(product_id) DO UPDATE SET dimensions_json=excluded.dimensions_json,search_text=excluded.search_text`, productID, string(defs), strings.Join(search, " "))
	return err
}

type catalogReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Store) readProductCatalog(ctx context.Context, reader catalogReader, id string) (*ProductCatalog, error) {
	c := EmptyProductCatalog()
	var defs string
	err := reader.QueryRowContext(ctx, `SELECT dimensions_json FROM product_catalogs WHERE product_id=?`, id).Scan(&defs)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(defs), &c.Dimensions); err != nil {
		return nil, err
	}
	var owner string
	if err = reader.QueryRowContext(ctx, `SELECT owner_id FROM products WHERE id=?`, id).Scan(&owner); err != nil {
		return nil, err
	}
	for i, d := range c.Dimensions {
		canonical, e := readDimension(ctx, reader, owner, d.Key)
		if e != nil {
			return nil, e
		}
		c.Dimensions[i] = canonical.ProductDimension
	}
	rows, err := reader.QueryContext(ctx, `SELECT v.id,v.sku,v.attributes_json,coalesce(p.price_json,'') FROM product_variants v LEFT JOIN product_variant_prices p ON p.variant_id=v.id WHERE v.product_id=? ORDER BY v.position,p.position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var vid, sku, attrs, price string
		if err = rows.Scan(&vid, &sku, &attrs, &price); err != nil {
			return nil, err
		}
		if len(c.Variants) == 0 || c.Variants[len(c.Variants)-1].ID != vid {
			v := ProductVariant{ID: vid, SKU: sku, Prices: []ProductPrice{}}
			if err = json.Unmarshal([]byte(attrs), &v.Attributes); err != nil {
				return nil, err
			}
			c.Variants = append(c.Variants, v)
		}
		if price != "" {
			var p ProductPrice
			if err = json.Unmarshal([]byte(price), &p); err != nil {
				return nil, err
			}
			if isProductPriceCNY(p.Currency) {
				p.Currency = ProductPriceCurrency
			}
			v := &c.Variants[len(c.Variants)-1]
			v.Prices = append(v.Prices, p)
		}
	}
	return c, rows.Err()
}

type ProductPriceQuery struct {
	Brands     []string          `json:"-"`
	VariantID  string            `json:"variantId"`
	PriceID    string            `json:"priceId"`
	ProductID  string            `json:"productId"`
	Version    int64             `json:"version"`
	Query      string            `json:"query"`
	Attributes map[string]string `json:"attributes"`
	Qty        string            `json:"qty"`
	Currency   string            `json:"currency"`
	Source     string            `json:"source"`
	Date       string            `json:"date"`
	Limit      int               `json:"limit"`
	Offset     int               `json:"offset"`
}
type ProductPriceMatch struct {
	ProductID      string            `json:"productId"`
	ProductVersion int64             `json:"productVersion"`
	ProductName    string            `json:"productName"`
	Model          string            `json:"model"`
	VariantID      string            `json:"variantId"`
	SKU            string            `json:"sku"`
	Attributes     map[string]string `json:"attributes"`
	Prices         []ProductPrice    `json:"prices"`
}
type ProductPriceResult struct {
	Status     string              `json:"status"`
	Dimensions []ProductDimension  `json:"dimensions"`
	Matches    []ProductPriceMatch `json:"matches"`
	Missing    []string            `json:"missing"`
	Total      int                 `json:"total"`
	HasMore    bool                `json:"hasMore"`
	NextOffset int                 `json:"nextOffset"`
}

// Matching is exact on normalized text attributes. Retrieval never turns a single
// keyword hit into proof that unspecified configuration or price conditions fit.
func (s *Store) QueryProductPrices(ctx context.Context, owner string, q ProductPriceQuery) (ProductPriceResult, error) {
	result := ProductPriceResult{Status: "no_match", Dimensions: []ProductDimension{}, Matches: []ProductPriceMatch{}, Missing: []string{}}
	if q.PriceID != "" && (q.ProductID == "" || q.VariantID == "") {
		return result, invalidProduct("priceId requires productId and variantId")
	}
	if q.Limit == 0 {
		q.Limit = 10
	}
	if q.Limit < 1 || q.Limit > 50 || q.Offset < 0 || q.Offset > 100000 {
		return result, invalidProduct("invalid result pagination")
	}
	var quantity float64
	var err error
	if q.Qty != "" {
		quantity, err = decimal(q.Qty, true)
		if err != nil {
			return result, err
		}
	}
	if q.Date == "" {
		q.Date = time.Now().UTC().Format("2006-01-02")
	}
	if _, err = time.Parse("2006-01-02", q.Date); err != nil {
		return result, invalidProduct("invalid query date")
	}
	if q.ProductID == "" && len(q.Attributes) > 0 {
		return result, invalidProduct("use productId with specification filters")
	}
	var ids []string
	if q.ProductID != "" {
		ids = []string{q.ProductID}
	} else {
		if strings.TrimSpace(q.Query) == "" {
			return result, invalidProduct("productId or query is required")
		}
		// No arbitrary search cap: status and total must account for every candidate.
		for offset := 0; ; {
			page, err := s.SearchProducts(ctx, owner, q.Query, "", 20, offset, ProductSearchFilter{Brands: q.Brands})
			if err != nil {
				return result, err
			}
			for _, p := range page.Products {
				ids = append(ids, p.ID)
			}
			if !page.HasMore {
				break
			}
			offset = page.NextOffset
		}
	}
	missing := map[string]bool{}
	priceCount := 0
	matchedProducts := map[string]bool{}
	for _, id := range ids {
		product, err := s.GetProduct(ctx, owner, id)
		if err != nil {
			return result, err
		}
		if q.Brands != nil {
			allowed := false
			for _, b := range q.Brands {
				if b == product.Brand {
					allowed = true
				}
			}
			if !allowed {
				return result, errors.New("product is outside the selected brand scope")
			}
		}
		if q.ProductID != "" && q.Version != 0 && q.Version != product.Version {
			return result, ErrProductConflict
		}
		catalog := product.Catalog
		if q.ProductID != "" {
			result.Dimensions = catalog.Dimensions
		}
		defs := map[string]ProductDimension{}
		for _, d := range catalog.Dimensions {
			defs[d.Key] = d
		}
		for key := range q.Attributes {
			if _, ok := defs[key]; !ok {
				return result, invalidProduct("unknown specification filter: " + key)
			}
		}
		for _, v := range catalog.Variants {
			if q.VariantID != "" && q.VariantID != v.ID {
				continue
			}
			matches := true
			for key, value := range q.Attributes {
				if defs[key].Type == "number" {
					n, err := strconv.ParseFloat(value, 64)
					if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
						return result, invalidProduct("invalid numeric filter: " + key)
					}
					if n == 0 {
						n = 0
					}
					value = strconv.FormatFloat(n, 'g', -1, 64)
				}
				if !strings.EqualFold(strings.TrimSpace(v.Attributes[key]), strings.TrimSpace(value)) {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			for _, d := range catalog.Dimensions {
				if q.VariantID == "" && strings.TrimSpace(q.Attributes[d.Key]) == "" {
					missing[d.Key] = true
				}
			}
			hit := ProductPriceMatch{ProductID: product.ID, ProductVersion: product.Version, ProductName: product.Name, Model: product.Model, VariantID: v.ID, SKU: v.SKU, Attributes: v.Attributes, Prices: []ProductPrice{}}
			for _, p := range v.Prices {
				if q.PriceID != "" && q.PriceID != p.ID {
					continue
				}
				if p.ValidFrom != "" && p.ValidFrom > q.Date || p.ValidUntil != "" && p.ValidUntil < q.Date {
					continue
				}
				if !isProductPriceCNY(p.Currency) || !isProductPriceCNY(q.Currency) || q.Source != "" && p.Source != q.Source {
					continue
				}
				if q.Qty != "" {
					if p.MinQty != "" {
						n, _ := decimal(p.MinQty, true)
						if quantity < n {
							continue
						}
					}
					if p.MaxQty != "" {
						n, _ := decimal(p.MaxQty, true)
						if quantity > n {
							continue
						}
					}
				} else if p.MinQty != "" || p.MaxQty != "" {
					missing["qty"] = true
				}
				hit.Prices = append(hit.Prices, p)
				priceCount++
			}
			result.Matches = append(result.Matches, hit)
			matchedProducts[id] = true
		}
	}
	result.Total = len(result.Matches)
	if result.Total > 0 {
		result.Status = "matched"
		if len(matchedProducts) > 1 {
			missing["productId"] = true
		}
		if priceCount == 0 {
			result.Status = "no_price"
		} else if len(missing) > 0 || result.Total > 1 || priceCount > 1 {
			result.Status = "needs_clarification"
		}
	}
	if priceCount > 1 {
		missing["priceId"] = true
	}
	for key := range missing {
		result.Missing = append(result.Missing, key)
	}
	sort.Strings(result.Missing)
	start := min(q.Offset, result.Total)
	end := min(start+q.Limit, result.Total)
	result.Matches = result.Matches[start:end]
	result.HasMore = end < result.Total
	if result.HasMore {
		result.NextOffset = end
	}
	return result, nil
}

// Validate a document reference against the current product, configuration and
// exact stored price. A price from the catalog cannot be paired with another SKU.
func (s *Store) ResolveProductPrice(ctx context.Context, owner, productID, variantID, priceID string, version int64, qty string) (ProductVariant, ProductPrice, error) {
	if productID == "" || variantID == "" || priceID == "" || version < 1 || qty == "" {
		return ProductVariant{}, ProductPrice{}, invalidProduct("complete product price reference and quantity required")
	}
	p, err := s.GetProduct(ctx, owner, productID)
	if err != nil {
		return ProductVariant{}, ProductPrice{}, err
	}
	if version != p.Version {
		return ProductVariant{}, ProductPrice{}, ErrProductConflict
	}
	for _, v := range p.Catalog.Variants {
		if v.ID == variantID {
			exact, err := s.QueryProductPrices(ctx, owner, ProductPriceQuery{ProductID: productID, Version: version, Attributes: v.Attributes, Qty: qty, Limit: 50})
			if err != nil {
				return v, ProductPrice{}, err
			}
			for _, m := range exact.Matches {
				for _, price := range m.Prices {
					if price.ID == priceID {
						return v, price, nil
					}
				}
			}
		}
	}
	return ProductVariant{}, ProductPrice{}, invalidProduct("price does not apply to this configuration, quantity or date")
}

// Pending legacy rows stay readable with their complete original column labels.
type PendingProductPrice struct {
	RowID    string            `json:"rowId"`
	ListName string            `json:"listName"`
	Model    string            `json:"model"`
	Name     string            `json:"name"`
	Fields   map[string]string `json:"fields"`
	Columns  []PriceColumn     `json:"columns"`
}

func (s *Store) PendingProductPrices(ctx context.Context, owner string, offset, limit int) ([]PendingProductPrice, int, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, invalidProduct("invalid pagination")
	}
	where := ` FROM price_list_rows r JOIN price_lists l ON l.id=r.list_id LEFT JOIN product_price_migrations m ON m.row_id=r.id WHERE l.owner_id=? AND m.row_id IS NULL`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*)`+where, owner).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.list_id,l.name,r.data_json`+where+` ORDER BY l.id,r.position,r.id LIMIT ? OFFSET ?`, owner, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	type raw struct{ row, list, name, data string }
	var raws []raw
	for rows.Next() {
		var r raw
		if err = rows.Scan(&r.row, &r.list, &r.name, &r.data); err != nil {
			rows.Close()
			return nil, 0, err
		}
		raws = append(raws, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	result := []PendingProductPrice{}
	for _, r := range raws {
		_, cols, err := s.GetPriceList(ctx, owner, r.list)
		if err != nil {
			return nil, 0, err
		}
		p := PendingProductPrice{RowID: r.row, ListName: r.name, Fields: map[string]string{}, Columns: cols}
		if err = json.Unmarshal([]byte(r.data), &p.Fields); err != nil {
			return nil, 0, err
		}
		for _, c := range cols {
			if c.Role == PriceRoleModel {
				p.Model = p.Fields[c.Key]
			}
			if c.Role == PriceRoleName {
				p.Name = p.Fields[c.Key]
			}
		}
		result = append(result, p)
	}
	return result, total, nil
}

// Import is explicit and atomic with the product update. The original source row
// is retained, and migration markers make retries safe.
func (s *Store) ImportProductPrice(ctx context.Context, owner, rowID, productID string, version int64, override *ProductPrice) (Product, error) {
	// Resolve the original row without relying on a paginated UI snapshot.
	var listID, data, listName string
	err := s.db.QueryRowContext(ctx, `SELECT r.list_id,r.data_json,l.name FROM price_list_rows r JOIN price_lists l ON l.id=r.list_id WHERE r.id=? AND l.owner_id=?`, rowID, owner).Scan(&listID, &data, &listName)
	if errors.Is(err, sql.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, err
	}
	_, cols, err := s.GetPriceList(ctx, owner, listID)
	if err != nil {
		return Product{}, err
	}
	fields := map[string]string{}
	if err = json.Unmarshal([]byte(data), &fields); err != nil {
		return Product{}, err
	}
	var p Product
	if productID != "" {
		p, err = s.GetProduct(ctx, owner, productID)
		if err != nil {
			return p, err
		}
		if version != p.Version {
			return p, ErrProductConflict
		}
	} else {
		p.Catalog = EmptyProductCatalog()
	}
	roles := priceLookupColumns(cols)
	get := func(role string) string {
		c, ok := roles[role]
		if !ok {
			return ""
		}
		return strings.TrimSpace(fields[c.Key])
	}
	if productID == "" {
		p.Name = get(PriceRoleName)
		p.Model = get(PriceRoleModel)
		if p.Name == "" {
			p.Name = p.Model
		}
		if p.Name == "" {
			return p, invalidProduct("select a product for a row without name or model")
		}
	}
	shared, err := s.ListProductDimensions(ctx, owner)
	if err != nil {
		return p, err
	}
	attrs := map[string]string{}
	for _, c := range cols {
		if c.Role == "" && c.Key != "notes" && strings.TrimSpace(fields[c.Key]) != "" {
			key := "legacy_" + c.Key
			for _, d := range p.Catalog.Dimensions {
				if strings.EqualFold(d.Label, c.Label) {
					key = d.Key
					break
				}
			}
			if len(key) > 40 {
				key = key[:40]
			}
			found := false
			for _, d := range p.Catalog.Dimensions {
				if d.Key == key {
					found = true
					if d.Label != c.Label {
						return p, invalidProduct("legacy dimension key conflict")
					}
				}
			}
			if !found {
				candidate := ProductDimension{Key: key, Label: c.Label, Type: "text"}
				named := 0
				compatible := []ProductDimension{}
				for _, definition := range shared {
					if !strings.EqualFold(definition.Label, c.Label) {
						continue
					}
					named++
					if definition.Unit != "" {
						continue
					}
					fits := definition.Type == "text"
					if definition.Type == "select" {
						for _, o := range definition.Options {
							fits = fits || o == strings.TrimSpace(fields[c.Key])
						}
					}
					if definition.Type == "number" && c.Type == PriceColumnNumber {
						n, e := strconv.ParseFloat(strings.TrimSpace(fields[c.Key]), 64)
						fits = e == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
					}
					if fits {
						compatible = append(compatible, definition.ProductDimension)
					}
				}
				if len(compatible) == 1 {
					candidate = compatible[0]
					key = candidate.Key
				} else if named > 0 {
					return p, invalidProduct("ambiguous shared dimension or unknown source unit: " + c.Label + "; link to a product with explicit dimensions")
				}
				if existing, e := readDimension(ctx, s.db, owner, key); e == nil && dimensionIdentity(existing.ProductDimension) != dimensionIdentity(candidate) {
					key, err = newID("pdim")
					if err != nil {
						return p, err
					}
					candidate.Key = key
				}
				p.Catalog.Dimensions = append(p.Catalog.Dimensions, candidate)
			}
			attrs[key] = fields[c.Key]
		}
	}
	for _, d := range p.Catalog.Dimensions {
		if attrs[d.Key] == "" {
			return p, invalidProduct("legacy row is missing dimension: " + d.Label + "; import as a new product and complete it before merging")
		}
	}
	price := ProductPrice{Amount: get(PriceRolePrice), Currency: get(PriceRoleCurrency), TaxRate: get(PriceRoleTax), MinQty: get(PriceRoleMOQ), LeadTime: get(PriceRoleLeadTime), Source: listName}
	if override != nil {
		price = *override
		price.ID = ""
		if price.Source == "" {
			price.Source = listName
		}
	}
	// Preserve invalid amounts as pending; never coerce them to zero.
	if _, err = decimal(price.Amount, false); err != nil {
		return p, invalidProduct("legacy price requires a plain decimal amount; correct the source or import CSV with explicit values")
	}
	if strings.HasSuffix(price.TaxRate, "%") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(price.TaxRate, "%"), 64)
		if err != nil {
			return p, invalidProduct("invalid tax rate")
		}
		price.TaxRate = strconv.FormatFloat(n/100, 'f', -1, 64)
	}
	if !isProductPriceCNY(price.Currency) {
		return p, invalidProduct("legacy currency is not CNY; supply a reviewed amount in CNY")
	}
	price.Currency = ProductPriceCurrency
	foundVariant := -1
	encoded, _ := json.Marshal(attrs)
	for i, v := range p.Catalog.Variants {
		b, _ := json.Marshal(v.Attributes)
		if string(b) == string(encoded) {
			foundVariant = i
			break
		}
	}
	if foundVariant < 0 {
		p.Catalog.Variants = append(p.Catalog.Variants, ProductVariant{SKU: get(PriceRoleModel), Attributes: attrs, Prices: []ProductPrice{}})
		foundVariant = len(p.Catalog.Variants) - 1
	}
	p.Catalog.Variants[foundVariant].Prices = append(p.Catalog.Variants[foundVariant].Prices, price)
	// SaveProduct's transaction also writes the migration marker via this private input.
	return s.SaveProduct(ctx, owner, productID, ProductInput{Name: p.Name, Brand: p.Brand, Model: p.Model, CategoryID: p.CategoryID, Specification: p.Specification, Version: p.Version, Catalog: p.Catalog, legacyPriceRowID: rowID, legacyPriceVariantIndex: foundVariant})
}

func catalogSummary(v ProductVariant, dimensions []ProductDimension) string {
	var parts []string
	for _, d := range dimensions {
		parts = append(parts, d.Label+": "+v.Attributes[d.Key]+d.Unit)
	}
	return strings.Join(parts, " / ")
}
