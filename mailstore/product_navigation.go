package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type ProductSearchFilter struct {
	// nil means all brands; an empty string selects products without a brand.
	Brands        []string // nil = all, empty non-nil = no brands
	Brand         *string
	Uncategorized bool
}

type ProductCategoryCount struct {
	CategoryID   string `json:"categoryId"`
	ProductCount int    `json:"productCount"`
}

type ProductBrandGroup struct {
	Name         string                 `json:"name"`
	ProductCount int                    `json:"productCount"`
	Categories   []ProductCategoryCount `json:"categories"`
}

type ProductNavigation struct {
	Brands []ProductBrandGroup `json:"brands"`
	Total  int                 `json:"total"`
}

func (s *Store) migrateProductCategoryTree(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(product_categories)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "parent_id" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		if _, err = s.db.ExecContext(ctx, `ALTER TABLE product_categories ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_product_categories_parent ON product_categories(owner_id,parent_id)`)
	return err
}

func (s *Store) ListProductCategories(ctx context.Context, owner string) ([]ProductCategory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name,c.parent_id,count(p.id) FROM product_categories c LEFT JOIN products p ON p.category_id=c.id AND p.owner_id=c.owner_id WHERE c.owner_id=? GROUP BY c.id ORDER BY c.name,c.id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ProductCategory{}
	for rows.Next() {
		var c ProductCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.ParentID, &c.ProductCount); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// Omitted parent preserves the parent when older clients rename a category.
func (s *Store) SaveProductCategory(ctx context.Context, owner, id, name string, parents ...string) (ProductCategory, error) {
	name = strings.TrimSpace(name)
	if owner == "" || name == "" || utf8.RuneCountInString(name) > 100 {
		return ProductCategory{}, invalidProduct("category name is required (max 100 characters)")
	}
	s.productMu.Lock()
	defer s.productMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProductCategory{}, err
	}
	defer tx.Rollback()
	parent := ""
	creating := id == ""
	if !creating {
		err = tx.QueryRowContext(ctx, `SELECT parent_id FROM product_categories WHERE owner_id=? AND id=?`, owner, id).Scan(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			return ProductCategory{}, ErrNotFound
		}
		if err != nil {
			return ProductCategory{}, err
		}
	}
	if len(parents) > 0 {
		parent = parents[0]
	}
	if parent != "" {
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM product_categories WHERE owner_id=? AND id=?`, owner, parent).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ProductCategory{}, ErrNotFound
		}
		if err != nil {
			return ProductCategory{}, err
		}
		if !creating {
			var cycle int
			err = tx.QueryRowContext(ctx, `WITH RECURSIVE subtree(id) AS (SELECT id FROM product_categories WHERE owner_id=? AND id=? UNION SELECT c.id FROM product_categories c JOIN subtree t ON c.parent_id=t.id WHERE c.owner_id=?) SELECT count(*) FROM subtree WHERE id=?`, owner, id, owner, parent).Scan(&cycle)
			if err != nil {
				return ProductCategory{}, err
			}
			if cycle > 0 {
				return ProductCategory{}, invalidProduct("a category cannot be placed under itself or its descendants")
			}
		}
	}
	if creating {
		id, err = newID("pcat")
		if err != nil {
			return ProductCategory{}, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO product_categories(id,owner_id,name,parent_id) VALUES(?,?,?,?)`, id, owner, name, parent)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE product_categories SET name=?,parent_id=? WHERE owner_id=? AND id=?`, name, parent, owner, id)
	}
	if err != nil {
		return ProductCategory{}, invalidProduct("category name already exists or could not be saved")
	}
	category := ProductCategory{ID: id, Name: name, ParentID: parent}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM products WHERE owner_id=? AND category_id=?`, owner, id).Scan(&category.ProductCount); err != nil {
		return ProductCategory{}, err
	}
	return category, tx.Commit()
}

func (s *Store) DeleteProductCategory(ctx context.Context, owner, id string) error {
	s.productMu.Lock()
	defer s.productMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var children int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM product_categories WHERE owner_id=? AND parent_id=?`, owner, id).Scan(&children); err != nil {
		return err
	}
	if children > 0 {
		return invalidProduct("move or delete child categories first")
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM product_categories WHERE id=? AND owner_id=?`, id, owner)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = tx.ExecContext(ctx, `UPDATE products SET category_id='',version=version+1,updated_at=? WHERE category_id=? AND owner_id=?`, time.Now().UnixMilli(), id, owner)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Aggregate the complete library, independently of search text and pagination.
func (s *Store) ProductNavigation(ctx context.Context, owner string) (ProductNavigation, error) {
	result := ProductNavigation{Brands: []ProductBrandGroup{}}
	rows, err := s.db.QueryContext(ctx, `SELECT brand,category_id,count(*) FROM products WHERE owner_id=? GROUP BY brand,category_id ORDER BY brand,category_id`, owner)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var brand string
		var count ProductCategoryCount
		if err := rows.Scan(&brand, &count.CategoryID, &count.ProductCount); err != nil {
			return result, err
		}
		if len(result.Brands) == 0 || result.Brands[len(result.Brands)-1].Name != brand {
			result.Brands = append(result.Brands, ProductBrandGroup{Name: brand, Categories: []ProductCategoryCount{}})
		}
		group := &result.Brands[len(result.Brands)-1]
		group.ProductCount += count.ProductCount
		group.Categories = append(group.Categories, count)
		result.Total += count.ProductCount
	}
	return result, rows.Err()
}
