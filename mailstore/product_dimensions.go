package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Dimensions are shared within an owner's library. Products store selections,
// while the canonical definition and its version live here.
type ProductDimensionRecord struct {
	ProductDimension
	Version      int64     `json:"version"`
	ProductCount int       `json:"productCount"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func dimensionIdentity(d ProductDimension) string {
	return strings.ToLower(strings.TrimSpace(d.Label)) + "\x00" + d.Type + "\x00" + strings.TrimSpace(d.Unit)
}
func readDimension(ctx context.Context, reader catalogReader, owner, key string) (ProductDimensionRecord, error) {
	var r ProductDimensionRecord
	var data string
	var updated int64
	err := reader.QueryRowContext(ctx, `SELECT definition_json,version,updated_at FROM product_dimensions WHERE owner_id=? AND dimension_key=?`, owner, key).Scan(&data, &r.Version, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal([]byte(data), &r.ProductDimension); err != nil {
		return r, err
	}
	r.UpdatedAt = time.UnixMilli(updated).UTC()
	err = reader.QueryRowContext(ctx, `SELECT count(*) FROM product_catalogs c JOIN products p ON p.id=c.product_id WHERE p.owner_id=? AND EXISTS(SELECT 1 FROM json_each(c.dimensions_json) j WHERE json_extract(j.value,'$.key')=?)`, owner, key).Scan(&r.ProductCount)
	return r, err
}
func (s *Store) ListProductDimensions(ctx context.Context, owner string) ([]ProductDimensionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT dimension_key FROM product_dimensions WHERE owner_id=? ORDER BY label_key,dimension_key`, owner)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []ProductDimensionRecord{}
	for _, key := range keys {
		r, err := readDimension(ctx, s.db, owner, key)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

// Inline definitions from old clients and CSV imports are promoted into the
// shared dictionary. Existing keys always resolve to the canonical definition.
func ensureProductDimension(ctx context.Context, tx *sql.Tx, owner string, d ProductDimension, mergeOptions bool) (ProductDimension, error) {
	if r, err := readDimension(ctx, tx, owner, d.Key); err == nil {
		return r.ProductDimension, nil
	} else if !errors.Is(err, ErrNotFound) {
		return d, err
	}
	var removed string
	if err := tx.QueryRowContext(ctx, `SELECT dimension_key FROM product_dimension_tombstones WHERE owner_id=? AND dimension_key=?`, owner, d.Key).Scan(&removed); err == nil {
		return d, invalidProduct("dimension was removed; select it again from the shared dictionary")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return d, err
	}
	checked := &ProductCatalog{Dimensions: []ProductDimension{d}}
	if err := validateCatalog(checked); err != nil {
		return d, err
	}
	d = checked.Dimensions[0]
	var data string
	err := tx.QueryRowContext(ctx, `SELECT definition_json FROM product_dimensions WHERE owner_id=? AND identity_key=?`, owner, dimensionIdentity(d)).Scan(&data)
	if err == nil {
		var existing ProductDimension
		if err = json.Unmarshal([]byte(data), &existing); err != nil {
			return d, err
		}
		// Legacy select lists can differ. A union preserves every existing value.
		if d.Type == "select" && mergeOptions {
			seen := map[string]bool{}
			for _, o := range existing.Options {
				seen[o] = true
			}
			for _, o := range d.Options {
				if !seen[o] {
					existing.Options = append(existing.Options, o)
					seen[o] = true
				}
			}
			b, _ := json.Marshal(existing)
			if _, err = tx.ExecContext(ctx, `UPDATE product_dimensions SET definition_json=? WHERE owner_id=? AND dimension_key=?`, string(b), owner, existing.Key); err != nil {
				return d, err
			}
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return d, err
	}
	b, _ := json.Marshal(d)
	_, err = tx.ExecContext(ctx, `INSERT INTO product_dimensions(owner_id,dimension_key,label_key,identity_key,definition_json,version,updated_at) VALUES(?,?,?,?,?,1,?)`, owner, d.Key, strings.ToLower(d.Label), dimensionIdentity(d), string(b), time.Now().UnixMilli())
	return d, err
}

func resolveCatalogDimensions(ctx context.Context, tx *sql.Tx, productID string, c *ProductCatalog) error {
	var owner string
	if err := tx.QueryRowContext(ctx, `SELECT owner_id FROM products WHERE id=?`, productID).Scan(&owner); err != nil {
		return err
	}
	for i, d := range c.Dimensions {
		resolved, err := ensureProductDimension(ctx, tx, owner, d, false)
		if err != nil {
			return err
		}
		if resolved.Key != d.Key {
			for j := range c.Variants {
				v := &c.Variants[j]
				if value, ok := v.Attributes[d.Key]; ok {
					v.Attributes[resolved.Key] = value
					delete(v.Attributes, d.Key)
				}
			}
		}
		c.Dimensions[i] = resolved
	}
	return nil
}
func catalogSearchText(c *ProductCatalog) string {
	parts := []string{}
	for _, d := range c.Dimensions {
		parts = append(parts, d.Label, d.Unit)
	}
	for _, v := range c.Variants {
		parts = append(parts, v.SKU, productModelKey(v.SKU))
		for _, d := range c.Dimensions {
			parts = append(parts, v.Attributes[d.Key])
		}
	}
	return strings.Join(parts, " ")
}
func reindexProductCatalog(ctx context.Context, tx *sql.Tx, id string, c *ProductCatalog, bump bool) error {
	defs, err := json.Marshal(c.Dimensions)
	if err != nil {
		return err
	}
	search := catalogSearchText(c)
	if _, err = tx.ExecContext(ctx, `UPDATE product_catalogs SET dimensions_json=?,search_text=? WHERE product_id=?`, string(defs), search, id); err != nil {
		return err
	}
	var name, brand, model string
	if err = tx.QueryRowContext(ctx, `SELECT name,brand,model FROM products WHERE id=?`, id).Scan(&name, &brand, &model); err != nil {
		return err
	}
	metadata := strings.Join(productTokens(name+" "+brand+" "+model), " ") + " " + productModelKey(model) + " " + strings.Join(productTokens(search), " ")
	if _, err = tx.ExecContext(ctx, `UPDATE product_chunks_fts SET metadata=? WHERE rowid IN (SELECT id FROM product_chunks WHERE product_id=?)`, metadata, id); err != nil {
		return err
	}
	if bump {
		_, err = tx.ExecContext(ctx, `UPDATE products SET version=version+1,updated_at=? WHERE id=?`, time.Now().UnixMilli(), id)
	}
	return err
}
func dimensionProductIDs(ctx context.Context, reader catalogReader, owner, key string) ([]string, error) {
	rows, err := reader.QueryContext(ctx, `SELECT c.product_id FROM product_catalogs c JOIN products p ON p.id=c.product_id WHERE p.owner_id=? AND EXISTS(SELECT 1 FROM json_each(c.dimensions_json) j WHERE json_extract(j.value,'$.key')=?)`, owner, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) SaveProductDimension(ctx context.Context, owner, key string, in ProductDimensionRecord) (ProductDimensionRecord, error) {
	if owner == "" {
		return in, ErrNotFound
	}
	s.productMu.Lock()
	defer s.productMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return in, err
	}
	defer tx.Rollback()
	var previous ProductDimensionRecord
	if key == "" {
		key, err = newID("pdim")
		if err != nil {
			return in, err
		}
	} else {
		previous, err = readDimension(ctx, tx, owner, key)
		if err != nil {
			return in, err
		}
		if previous.Version != in.Version {
			return in, ErrProductConflict
		}
	}
	in.Key = key
	c := &ProductCatalog{Dimensions: []ProductDimension{in.ProductDimension}}
	if err = validateCatalog(c); err != nil {
		return in, err
	}
	in.ProductDimension = c.Dimensions[0]
	if previous.Version > 0 {
		before, _ := json.Marshal(previous.ProductDimension)
		after, _ := json.Marshal(in.ProductDimension)
		if string(before) == string(after) {
			return previous, tx.Commit()
		}
	}
	ids, err := dimensionProductIDs(ctx, tx, owner, key)
	if err != nil {
		return in, err
	}
	if len(ids) > 0 && (in.Type != previous.Type || in.Unit != previous.Unit) {
		return in, invalidProduct("dimension type and unit cannot change while products use it")
	}
	if len(ids) > 0 && in.Type == "select" {
		allowed := map[string]bool{}
		for _, option := range in.Options {
			allowed[option] = true
		}
		for _, id := range ids {
			catalog, err := s.readProductCatalog(ctx, tx, id)
			if err != nil {
				return in, err
			}
			for _, v := range catalog.Variants {
				if !allowed[v.Attributes[key]] {
					return in, invalidProduct("cannot remove an option used by a product configuration")
				}
			}
		}
	}
	var duplicate string
	err = tx.QueryRowContext(ctx, `SELECT dimension_key FROM product_dimensions WHERE owner_id=? AND identity_key=? AND dimension_key<>?`, owner, dimensionIdentity(in.ProductDimension), key).Scan(&duplicate)
	if err == nil {
		return in, invalidProduct("a dimension with this name, type and unit already exists")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return in, err
	}
	b, _ := json.Marshal(in.ProductDimension)
	now := time.Now().UnixMilli()
	version := previous.Version + 1
	_, err = tx.ExecContext(ctx, `INSERT INTO product_dimensions(owner_id,dimension_key,label_key,identity_key,definition_json,version,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(owner_id,dimension_key) DO UPDATE SET label_key=excluded.label_key,identity_key=excluded.identity_key,definition_json=excluded.definition_json,version=excluded.version,updated_at=excluded.updated_at`, owner, key, strings.ToLower(in.Label), dimensionIdentity(in.ProductDimension), string(b), version, now)
	if err != nil {
		return in, err
	}
	for _, id := range ids {
		catalog, err := s.readProductCatalog(ctx, tx, id)
		if err != nil {
			return in, err
		}
		if err = validateCatalog(catalog); err != nil {
			return in, err
		}
		if err = reindexProductCatalog(ctx, tx, id, catalog, true); err != nil {
			return in, err
		}
	}
	if err = tx.Commit(); err != nil {
		return in, err
	}
	in.Version = version
	in.ProductCount = len(ids)
	in.UpdatedAt = time.UnixMilli(now).UTC()
	return in, nil
}
func (s *Store) DeleteProductDimension(ctx context.Context, owner, key string, version int64) error {
	s.productMu.Lock()
	defer s.productMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := readDimension(ctx, tx, owner, key)
	if err != nil {
		return err
	}
	if r.Version != version {
		return ErrProductConflict
	}
	if r.ProductCount > 0 {
		return invalidProduct("dimension is used by products; remove those references before deleting it")
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO product_dimension_tombstones(owner_id,dimension_key) VALUES(?,?)`, owner, key); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM product_dimensions WHERE owner_id=? AND dimension_key=?`, owner, key); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrateGlobalProductDimensions(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS product_dimensions (owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,dimension_key TEXT NOT NULL,label_key TEXT NOT NULL,identity_key TEXT NOT NULL,definition_json TEXT NOT NULL,version INTEGER NOT NULL,updated_at INTEGER NOT NULL,PRIMARY KEY(owner_id,dimension_key),UNIQUE(owner_id,identity_key))`,
		`CREATE TABLE IF NOT EXISTS product_dimension_tombstones (owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,dimension_key TEXT NOT NULL,PRIMARY KEY(owner_id,dimension_key))`,
		`CREATE TABLE IF NOT EXISTS product_catalog_migrations (name TEXT PRIMARY KEY)`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done string
	err = tx.QueryRowContext(ctx, `SELECT name FROM product_catalog_migrations WHERE name='global_dimensions_v1'`).Scan(&done)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.product_id,p.owner_id,c.dimensions_json FROM product_catalogs c JOIN products p ON p.id=c.product_id ORDER BY c.product_id`)
	if err != nil {
		return err
	}
	type legacy struct{ id, owner, defs string }
	var originals []legacy
	for rows.Next() {
		var r legacy
		if err = rows.Scan(&r.id, &r.owner, &r.defs); err != nil {
			rows.Close()
			return err
		}
		originals = append(originals, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range originals {
		var dimensions []ProductDimension
		if err = json.Unmarshal([]byte(r.defs), &dimensions); err != nil {
			return err
		}
		for i, d := range dimensions {
			// Conflicting old keys from copied/imported products must not merge meanings.
			if current, e := readDimension(ctx, tx, r.owner, d.Key); e == nil && dimensionIdentity(current.ProductDimension) != dimensionIdentity(d) {
				d.Key, err = newID("pdim")
				if err != nil {
					return err
				}
			}
			if current, e := readDimension(ctx, tx, r.owner, d.Key); e == nil && d.Type == "select" {
				seen := map[string]bool{}
				for _, o := range current.Options {
					seen[o] = true
				}
				for _, o := range d.Options {
					if !seen[o] {
						current.Options = append(current.Options, o)
						seen[o] = true
					}
				}
				b, _ := json.Marshal(current.ProductDimension)
				if _, e = tx.ExecContext(ctx, `UPDATE product_dimensions SET definition_json=? WHERE owner_id=? AND dimension_key=?`, string(b), r.owner, d.Key); e != nil {
					return e
				}
			}
			canonical, e := ensureProductDimension(ctx, tx, r.owner, d, true)
			if e != nil {
				return e
			}
			oldKey := dimensions[i].Key
			dimensions[i] = canonical
			if oldKey != canonical.Key {
				rows, e := tx.QueryContext(ctx, `SELECT id,attributes_json FROM product_variants WHERE product_id=?`, r.id)
				if e != nil {
					return e
				}
				type attrsRow struct{ id, data string }
				var variants []attrsRow
				for rows.Next() {
					var v attrsRow
					if e = rows.Scan(&v.id, &v.data); e != nil {
						rows.Close()
						return e
					}
					variants = append(variants, v)
				}
				e = rows.Err()
				rows.Close()
				if e != nil {
					return e
				}
				for _, v := range variants {
					attrs := map[string]string{}
					if e = json.Unmarshal([]byte(v.data), &attrs); e != nil {
						return e
					}
					if value, ok := attrs[oldKey]; ok {
						attrs[canonical.Key] = value
						delete(attrs, oldKey)
					}
					b, _ := json.Marshal(attrs)
					if _, e = tx.ExecContext(ctx, `UPDATE product_variants SET attributes_json=? WHERE id=?`, string(b), v.id); e != nil {
						return e
					}
				}
			}
		}
		b, _ := json.Marshal(dimensions)
		if _, err = tx.ExecContext(ctx, `UPDATE product_catalogs SET dimensions_json=? WHERE product_id=?`, string(b), r.id); err != nil {
			return err
		}
	}
	// A union from a later product must also be visible in earlier products.
	for _, r := range originals {
		catalog, err := s.readProductCatalog(ctx, tx, r.id)
		if err != nil {
			return err
		}
		if err = reindexProductCatalog(ctx, tx, r.id, catalog, true); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO product_catalog_migrations(name) VALUES('global_dimensions_v1')`); err != nil {
		return err
	}
	return tx.Commit()
}
