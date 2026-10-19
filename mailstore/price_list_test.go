package mailstore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newPriceListFixture(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	user, err := store.CreateUser(ctx, "pricer", "Pricer", "hash")
	if err != nil {
		t.Fatal(err)
	}
	return store, user.ID
}

func TestPriceListColumnsAndRows(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)

	list, columns, err := store.CreatePriceList(ctx, owner, "2026 阀门价目表", "标准价", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != len(DefaultPriceColumns()) {
		t.Fatalf("default columns = %d", len(columns))
	}
	if columns[0].Role != PriceRoleModel || columns[3].Role != PriceRolePrice {
		t.Fatalf("default roles = %+v", columns)
	}

	saved, err := store.SavePriceRows(ctx, owner, list.ID, []PriceRow{
		{Data: map[string]string{"model": "DN50-BV", "name": "DN50 球阀", "price": "1,280.00", "currency": "CNY", "unit": "台", "lead_time": "15"}},
		{Data: map[string]string{"model": "DN80-BV", "name": "DN80 球阀", "price": "¥1980", "currency": "CNY", "unit": "台"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if saved != 2 {
		t.Fatalf("saved = %d", saved)
	}

	rows, total, err := store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("rows = %d total = %d", len(rows), total)
	}

	// Free-text search matches any cell, and search survives a label rename.
	found, total, err := store.ListPriceRows(ctx, owner, list.ID, "球阀", "", "", 50, 0)
	if err != nil || total != 2 {
		t.Fatalf("search 球阀: total=%d err=%v", total, err)
	}
	_ = found
	if _, total, err = store.ListPriceRows(ctx, owner, list.ID, "DN80", "", "", 50, 0); err != nil || total != 1 {
		t.Fatalf("search DN80: total=%d err=%v", total, err)
	}

	// Sorting uses the column type: a number column sorts numerically.
	if _, err := store.SavePriceRows(ctx, owner, list.ID, []PriceRow{
		{Data: map[string]string{"model": "DN25-BV", "price": "9"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	sorted, _, err := store.ListPriceRows(ctx, owner, list.ID, "", "price", "asc", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sorted[0].Data["model"] != "DN25-BV" {
		t.Fatalf("numeric sort put %q first", sorted[0].Data["model"])
	}

	// Editing an existing row updates in place rather than appending.
	target := rows[0]
	target.Data["price"] = "1300"
	if _, err := store.SavePriceRows(ctx, owner, list.ID, []PriceRow{target}, nil); err != nil {
		t.Fatal(err)
	}
	if _, total, err = store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0); err != nil || total != 3 {
		t.Fatalf("after update total=%d err=%v", total, err)
	}

	// Deleting removes the row.
	if _, err := store.SavePriceRows(ctx, owner, list.ID, nil, []string{target.ID}); err != nil {
		t.Fatal(err)
	}
	if _, total, err = store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0); err != nil || total != 2 {
		t.Fatalf("after delete total=%d err=%v", total, err)
	}
}

func TestPriceColumnEditsKeepData(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)
	list, columns, err := store.CreatePriceList(ctx, owner, "价目表", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Start with an extra, unroled column so its removal can be observed.
	withExtra := append(append([]PriceColumn{}, columns...), PriceColumn{Key: "extra", Label: "补充", Type: PriceColumnText, Width: 120})
	columns, err = store.SavePriceColumns(ctx, owner, list.ID, withExtra)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavePriceRows(ctx, owner, list.ID, []PriceRow{
		{Data: map[string]string{"model": "A-1", "name": "产品A", "price": "100", "extra": "keep"}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	// Rename a label, drop the extra column, and add a new one.
	next := make([]PriceColumn, 0, len(columns)+1)
	for _, column := range columns {
		if column.Key == "extra" {
			continue
		}
		if column.Key == "name" {
			column.Label = "产品名称"
		}
		next = append(next, column)
	}
	next = append(next, PriceColumn{Key: "brand", Label: "品牌", Type: PriceColumnText, Role: "", Width: 120})
	saved, err := store.SavePriceColumns(ctx, owner, list.ID, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != len(next) {
		t.Fatalf("saved columns = %d, want %d", len(saved), len(next))
	}

	rows, _, err := store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows after column edit: %d err=%v", len(rows), err)
	}
	if rows[0].Data["model"] != "A-1" || rows[0].Data["name"] != "产品A" {
		t.Fatalf("row data lost on column edit: %+v", rows[0].Data)
	}
	if _, exists := rows[0].Data["extra"]; exists {
		t.Fatalf("removed column kept its value: %+v", rows[0].Data)
	}
	// A renamed label no longer matches the old one, but the key is stable.
	if _, total, err := store.ListPriceRows(ctx, owner, list.ID, "产品A", "", "", 50, 0); err != nil || total != 1 {
		t.Fatalf("search after rename: total=%d err=%v", total, err)
	}
}

func TestLookupPricesPrefersExactModel(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)
	list, _, err := store.CreatePriceList(ctx, owner, "价目表", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavePriceRows(ctx, owner, list.ID, []PriceRow{
		{Data: map[string]string{"model": "DN50-BV-EXTRA", "name": "DN50 球阀加长", "price": "1500", "currency": "CNY", "unit": "台", "tax": "13%", "lead_time": "20", "moq": "2"}},
		{Data: map[string]string{"model": "DN50-BV", "name": "DN50 球阀", "price": "1,280.00", "currency": "CNY", "unit": "台", "tax": "0.13", "lead_time": "15"}},
		{Data: map[string]string{"model": "DN80-BV", "name": "DN80 球阀", "price": "1980", "currency": "CNY"}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	entries, err := store.LookupPrices(ctx, owner, "DN50-BV", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Model != "DN50-BV" {
		t.Fatalf("exact match not ranked first: %+v", entries[0])
	}
	if entries[0].Price != 1280 {
		t.Fatalf("price = %v (thousand separators must parse)", entries[0].Price)
	}
	if entries[0].TaxRate != 0.13 || entries[0].Unit != "台" || entries[0].LeadTime != "15" {
		t.Fatalf("roles not resolved: %+v", entries[0])
	}
	if entries[1].TaxRate != 0.13 {
		t.Fatalf("percent tax not normalized: %+v", entries[1])
	}

	// A list with no price role is not a price source.
	other, _, err := store.CreatePriceList(ctx, owner, "无价格表", "", []PriceColumn{{Key: "note", Label: "备注", Type: PriceColumnText, Width: 120}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavePriceRows(ctx, owner, other.ID, []PriceRow{{Data: map[string]string{"note": "DN50-BV 停产"}}}, nil); err != nil {
		t.Fatal(err)
	}
	entries, err = store.LookupPrices(ctx, owner, "停产", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("list without a price column leaked into lookup: %+v", entries)
	}
}

func TestImportPriceRowsCreatesColumnsAndUpserts(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)
	list, _, err := store.CreatePriceList(ctx, owner, "导入测试", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	content := "型号,名称,单价,交期\nDN50-BV,DN50 球阀,1280,15\nDN80-BV,DN80 球阀,1980,20\n"
	preview, err := store.ImportPriceRows(ctx, owner, list.ID, content, 0, true, "append", true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Added != 2 || preview.Updated != 0 {
		t.Fatalf("dry run counts = %+v", preview)
	}
	if _, total, _ := store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0); total != 0 {
		t.Fatalf("dry run wrote %d rows", total)
	}

	result, err := store.ImportPriceRows(ctx, owner, list.ID, content, 0, true, "append", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 2 || len(result.CreatedColumns) != 0 {
		t.Fatalf("import result = %+v", result)
	}
	if _, total, _ := store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0); total != 2 {
		t.Fatalf("imported rows = %d", total)
	}

	// Headers with no matching label are matched by role first ("货号" fills the
	// existing model column, "价格" the existing price column) and only a
	// genuinely new header becomes a new column.
	result, err = store.ImportPriceRows(ctx, owner, list.ID, "货号,品牌,价格\nDN25-BV,Acme,9\n", 0, true, "append", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CreatedColumns) != 1 || result.CreatedColumns[0].Label != "品牌" {
		t.Fatalf("created columns = %+v", result.CreatedColumns)
	}
	if _, total, _ := store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0); total != 3 {
		t.Fatalf("role-matched import appended a duplicate row: %d", total)
	}
	entries, err := store.LookupPrices(ctx, owner, "Acme", 5)
	if err != nil || len(entries) != 1 {
		t.Fatalf("lookup after import: %+v err=%v", entries, err)
	}
	if entries[0].Model != "DN25-BV" || entries[0].Price != 9 {
		t.Fatalf("imported entry = %+v", entries[0])
	}

	// Upsert by model updates instead of duplicating.
	result, err = store.ImportPriceRows(ctx, owner, list.ID, "型号,单价\nDN50-BV,1300\n", 0, true, "upsert", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 || result.Added != 0 {
		t.Fatalf("upsert result = %+v", result)
	}
	if _, total, _ := store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0); total != 3 {
		t.Fatalf("upsert duplicated rows: %d", total)
	}
	entries, _ = store.LookupPrices(ctx, owner, "DN50-BV", 5)
	if entries[0].Price != 1300 {
		t.Fatalf("upsert did not update price: %+v", entries[0])
	}
}

func TestExportPriceRowsRoundTrips(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)
	list, _, err := store.CreatePriceList(ctx, owner, "导出测试", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavePriceRows(ctx, owner, list.ID, []PriceRow{
		{Data: map[string]string{"model": "A-1", "name": "产品,带逗号", "price": "100"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	content, name, err := store.ExportPriceRows(ctx, owner, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if name != "导出测试" || !strings.HasPrefix(content, "\ufeff") {
		t.Fatalf("export header/name = %q %q", name, content[:3])
	}
	if !strings.Contains(content, "产品,带逗号") || !strings.Contains(content, `"产品,带逗号"`) {
		t.Fatalf("comma value not quoted: %q", content)
	}

	target, _, err := store.CreatePriceList(ctx, owner, "导入目标", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportPriceRows(ctx, owner, target.ID, content, 0, true, "append", false); err != nil {
		t.Fatal(err)
	}
	rows, _, err := store.ListPriceRows(ctx, owner, target.ID, "", "", "", 50, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("round-trip rows = %d err=%v", len(rows), err)
	}
	if rows[0].Data["name"] != "产品,带逗号" || rows[0].Data["price"] != "100" {
		t.Fatalf("round-trip data = %+v", rows[0].Data)
	}
}

func TestPriceListsAreOwnerScoped(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)
	other, err := store.CreateUser(ctx, "other", "Other", "hash")
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := store.CreatePriceList(ctx, owner, "私有价目", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.GetPriceList(ctx, other.ID, list.ID); err != ErrNotFound {
		t.Fatalf("cross-owner read err = %v", err)
	}
	if _, err := store.SavePriceRows(ctx, other.ID, list.ID, []PriceRow{{Data: map[string]string{"model": "X"}}}, nil); err != ErrNotFound {
		t.Fatalf("cross-owner write err = %v", err)
	}
	if err := store.DeletePriceList(ctx, other.ID, list.ID); err != ErrNotFound {
		t.Fatalf("cross-owner delete err = %v", err)
	}
	lists, err := store.ListPriceLists(ctx, other.ID)
	if err != nil || len(lists) != 0 {
		t.Fatalf("other owner lists = %+v err=%v", lists, err)
	}
	entries, err := store.LookupPrices(ctx, other.ID, "DN50", 5)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cross-owner lookup = %+v err=%v", entries, err)
	}
}

func TestPriceListDeletionCascades(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)
	list, _, err := store.CreatePriceList(ctx, owner, "待删除", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SavePriceRows(ctx, owner, list.ID, []PriceRow{{Data: map[string]string{"model": "A"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePriceList(ctx, owner, list.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetPriceList(ctx, owner, list.ID); err != ErrNotFound {
		t.Fatalf("list survived delete: %v", err)
	}
	var rows, columns int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_list_rows`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_list_columns`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if rows != 0 || columns != 0 {
		t.Fatalf("orphan rows=%d columns=%d", rows, columns)
	}
}

func TestIncrementalPriceRowWrites(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)
	list, _, err := store.CreatePriceList(ctx, owner, "增量保存", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Appending one row returns the server id the grid needs.
	row, err := store.AppendPriceRow(ctx, owner, list.ID, map[string]string{"model": "DN50-BV", "price": "¥1,280.00"})
	if err != nil {
		t.Fatal(err)
	}
	if row.ID == "" || row.Position != 1 {
		t.Fatalf("appended row = %+v", row)
	}
	if row.Data["price"] != "1280.00" {
		t.Fatalf("numeric cell not normalized on append: %+v", row.Data)
	}

	// A cell-level update touches only the keys it was given.
	updated, err := store.UpdatePriceRowCells(ctx, owner, list.ID, row.ID, map[string]string{"lead_time": "15"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Data["lead_time"] != "15" || updated.Data["model"] != "DN50-BV" || updated.Data["price"] != "1280.00" {
		t.Fatalf("partial update lost or clobbered cells: %+v", updated.Data)
	}
	if updated.Position != row.Position {
		t.Fatalf("position changed on update: %d", updated.Position)
	}

	// Unknown columns and oversized values are ignored rather than stored.
	updated, err = store.UpdatePriceRowCells(ctx, owner, list.ID, row.ID, map[string]string{"not_a_column": "x", "model": "DN50-BV-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := updated.Data["not_a_column"]; exists {
		t.Fatalf("unknown column was written: %+v", updated.Data)
	}
	if updated.Data["model"] != "DN50-BV-2" {
		t.Fatalf("model not updated: %+v", updated.Data)
	}

	// The search index follows an incremental edit.
	if _, total, err := store.ListPriceRows(ctx, owner, list.ID, "DN50-BV-2", "", "", 50, 0); err != nil || total != 1 {
		t.Fatalf("search after cell update: total=%d err=%v", total, err)
	}

	// A missing row is reported as not-found so the client can re-create it.
	if _, err := store.UpdatePriceRowCells(ctx, owner, list.ID, "prow_missing", map[string]string{"model": "x"}); err != ErrNotFound {
		t.Fatalf("missing row update err = %v", err)
	}
	if err := store.DeletePriceRow(ctx, owner, list.ID, "prow_missing"); err != ErrNotFound {
		t.Fatalf("missing row delete err = %v", err)
	}

	// The list version advances per write, which is what invalidates AI caches.
	before, _, err := store.GetPriceList(ctx, owner, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePriceRowCells(ctx, owner, list.ID, row.ID, map[string]string{"model": "DN50-BV-3"}); err != nil {
		t.Fatal(err)
	}
	after, _, err := store.GetPriceList(ctx, owner, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version <= before.Version {
		t.Fatalf("version did not advance: %d -> %d", before.Version, after.Version)
	}

	if err := store.DeletePriceRow(ctx, owner, list.ID, row.ID); err != nil {
		t.Fatal(err)
	}
	if _, total, err := store.ListPriceRows(ctx, owner, list.ID, "", "", "", 50, 0); err != nil || total != 0 {
		t.Fatalf("row survived delete: total=%d err=%v", total, err)
	}
}

func TestIncrementalPriceRowWritesAreOwnerScoped(t *testing.T) {
	ctx := context.Background()
	store, owner := newPriceListFixture(t)
	other, err := store.CreateUser(ctx, "other2", "Other", "hash")
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := store.CreatePriceList(ctx, owner, "私有", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	row, err := store.AppendPriceRow(ctx, owner, list.ID, map[string]string{"model": "A"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendPriceRow(ctx, other.ID, list.ID, map[string]string{"model": "B"}); err != ErrNotFound {
		t.Fatalf("cross-owner append err = %v", err)
	}
	if _, err := store.UpdatePriceRowCells(ctx, other.ID, list.ID, row.ID, map[string]string{"model": "C"}); err != ErrNotFound {
		t.Fatalf("cross-owner update err = %v", err)
	}
	if err := store.DeletePriceRow(ctx, other.ID, list.ID, row.ID); err != ErrNotFound {
		t.Fatalf("cross-owner delete err = %v", err)
	}
}

// Autosave fires many small writes at once. SQLite answers overlapping
// read-then-write transactions with SQLITE_BUSY instead of waiting, so the
// store queues price-list writes; this proves the queue works.
func TestConcurrentPriceRowWrites(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "price.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.CreateUser(ctx, "concurrent", "Concurrent", "hash")
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := store.CreatePriceList(ctx, user.ID, "并发写入", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	const writers = 12
	var wait sync.WaitGroup
	failures := make(chan error, writers)
	for index := 0; index < writers; index += 1 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			row, appendErr := store.AppendPriceRow(ctx, user.ID, list.ID, map[string]string{"model": fmt.Sprintf("M-%02d", index)})
			if appendErr != nil {
				failures <- appendErr
				return
			}
			if _, updateErr := store.UpdatePriceRowCells(ctx, user.ID, list.ID, row.ID, map[string]string{"price": fmt.Sprintf("%d", 100+index)}); updateErr != nil {
				failures <- updateErr
			}
		}(index)
	}
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Fatalf("concurrent price write failed: %v", failure)
	}

	_, total, err := store.ListPriceRows(ctx, user.ID, list.ID, "", "", "", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != writers {
		t.Fatalf("rows = %d, want %d", total, writers)
	}
	// Positions stay unique even though every append raced.
	rows, _, err := store.ListPriceRows(ctx, user.ID, list.ID, "", "price", "asc", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, row := range rows {
		if seen[row.Position] {
			t.Fatalf("duplicate position %d", row.Position)
		}
		seen[row.Position] = true
	}
	if len(seen) != writers {
		t.Fatalf("positions = %d, want %d", len(seen), writers)
	}
}
