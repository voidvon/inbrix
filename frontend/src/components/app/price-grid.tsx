import { memo, useCallback, useRef, useState, type ClipboardEvent, type KeyboardEvent, type PointerEvent } from "react";
import { ArrowDownAZ, ArrowUpAZ, Check, ChevronDown, Columns3, Pencil, Plus, Settings2, Trash2 } from "lucide-react";
import { cn } from "../../lib/utils";
import type { Copy } from "../../lib/locale";
import type { PriceColumn, PriceRow } from "../../types";
import { Button } from "../ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "../ui/dropdown-menu";
import { Input } from "../ui/input";

const ROW_NUMBER_WIDTH = 56;
const MIN_COLUMN_WIDTH = 60;
const MAX_COLUMN_WIDTH = 900;

export type PriceGridProps = {
  copy: Copy;
  columns: PriceColumn[];
  rows: PriceRow[];
  selectedIds: string[];
  /** Per-row save state, keyed by row id. */
  rowStatus: Record<string, "saving" | "error">;
  sortKey?: string;
  sortDir?: "asc" | "desc";
  loading?: boolean;
  onToggleRow: (id: string) => void;
  onToggleAll: () => void;
  onChangeCell: (rowId: string, key: string, value: string) => void;
  /** Called when a cell loses focus (or Enter moves off it): the autosave point. */
  onCommitCell: (rowId: string, key: string) => void;
  onPasteBlock: (anchorRowId: string, anchorKey: string, matrix: string[][]) => void;
  onResizeColumn: (key: string, width: number) => void;
  onRenameColumn: (key: string, label: string) => void;
  onSortColumn: (key: string) => void;
  onEditColumn: (key: string) => void;
  onInsertColumn: (key: string, side: "left" | "right") => void;
  onDeleteColumn: (key: string) => void;
  onAddColumn: () => void;
  onAddRow: () => void;
};

/** Full role name, used for the header tooltip. */
function roleLabel(copy: Copy, role: PriceColumn["role"]) {
  switch (role) {
    case "model":
      return copy.priceRoleModel;
    case "name":
      return copy.priceRoleName;
    case "unit":
      return copy.priceRoleUnit;
    case "price":
      return copy.priceRolePrice;
    case "currency":
      return copy.priceRoleCurrency;
    case "tax":
      return copy.priceRoleTax;
    case "moq":
      return copy.priceRoleMOQ;
    case "lead_time":
      return copy.priceRoleLeadTime;
    default:
      return "";
  }
}

function roleBadge(copy: Copy, role: PriceColumn["role"]) {
  switch (role) {
    case "model":
      return copy.priceRoleModel.split("（")[0];
    case "price":
      return copy.priceRolePrice.split("（")[0];
    case "name":
      return copy.priceRoleName;
    case "unit":
      return copy.priceRoleUnit;
    case "currency":
      return copy.priceRoleCurrency;
    case "tax":
      return copy.priceRoleTax;
    case "moq":
      return copy.priceRoleMOQ;
    case "lead_time":
      return copy.priceRoleLeadTime;
    default:
      return "";
  }
}

/**
 * The editable price grid. Cell edits are reported upward as they are typed and
 * committed on blur; the grid itself owns only transient interaction state
 * (which cell is focused, a column being resized, a header being renamed).
 */
export function PriceGrid(props: PriceGridProps) {
  const { copy, columns, rows, selectedIds, rowStatus, sortKey, sortDir, onPasteBlock, onResizeColumn, onAddColumn, onAddRow } = props;
  const containerRef = useRef<HTMLDivElement>(null);
  const [resizing, setResizing] = useState<{
    key: string;
    width: number;
  } | null>(null);
  const [renaming, setRenaming] = useState<{
    key: string;
    value: string;
  } | null>(null);
  const selected = new Set(selectedIds);

  const columnWidth = useCallback((column: PriceColumn) => (resizing?.key === column.key ? resizing.width : column.width || 160), [resizing]);

  const focusCell = useCallback((rowIndex: number, columnIndex: number) => {
    const selector = `[data-cell][data-row-index="${rowIndex}"][data-col-index="${columnIndex}"]`;
    const next = containerRef.current?.querySelector<HTMLInputElement>(selector);
    if (!next) return;
    next.focus();
    next.select();
  }, []);

  // A pasted block (Excel, Google Sheets, or a CSV column) is applied as a
  // rectangle from the focused cell, which is what a spreadsheet user expects.
  const handlePaste = (event: ClipboardEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement;
    const rowId = target?.dataset?.rowId;
    const columnKey = target?.dataset?.columnKey;
    if (!rowId || !columnKey) return;
    const text = event.clipboardData?.getData("text/plain") ?? "";
    if (!text.trim()) return;
    const matrix = text
      .replace(/\r\n?/g, "\n")
      .replace(/\n+$/, "")
      .split("\n")
      .map((line) => line.split("\t"));
    event.preventDefault();
    onPasteBlock(rowId, columnKey, matrix);
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>, rowIndex: number, columnIndex: number) => {
    if (event.key === "Enter") {
      event.preventDefault();
      focusCell(rowIndex + 1, columnIndex);
    } else if (event.key === "Escape") {
      event.currentTarget.blur();
    } else if (event.key === "ArrowDown" && event.altKey) {
      event.preventDefault();
      focusCell(rowIndex + 1, columnIndex);
    } else if (event.key === "ArrowUp" && event.altKey) {
      event.preventDefault();
      focusCell(rowIndex - 1, columnIndex);
    }
  };

  const startResize = (event: PointerEvent<HTMLSpanElement>, column: PriceColumn) => {
    event.preventDefault();
    event.stopPropagation();
    const startX = event.clientX;
    const startWidth = column.width || 160;
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);
    const move = (moveEvent: globalThis.PointerEvent) => {
      const width = Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, Math.round(startWidth + moveEvent.clientX - startX)));
      setResizing({ key: column.key, width });
    };
    const up = (upEvent: globalThis.PointerEvent) => {
      handle.releasePointerCapture(upEvent.pointerId);
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", up);
      const width = Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, Math.round(startWidth + upEvent.clientX - startX)));
      setResizing(null);
      if (width !== startWidth) onResizeColumn(column.key, width);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", up);
  };

  const commitRename = () => {
    if (!renaming) return;
    const label = renaming.value.trim();
    if (label) props.onRenameColumn(renaming.key, label);
    setRenaming(null);
  };

  return (
    <div ref={containerRef} className="min-h-0 flex-1 overflow-auto" onPaste={handlePaste}>
      <table className="w-max border-separate border-spacing-0 text-sm">
        <thead>
          <tr>
            <th className="sticky top-0 left-0 z-20 border-r border-b bg-muted px-1 py-0 text-center align-middle" style={{ width: ROW_NUMBER_WIDTH, minWidth: ROW_NUMBER_WIDTH }}>
              <button
                type="button"
                className="grid h-8 w-full place-items-center text-muted-foreground hover:text-foreground"
                onClick={props.onToggleAll}
                aria-label={copy.priceListDeleteRows}
                title={copy.priceListDeleteRows}
              >
                {rows.length > 0 && selected.size === rows.length ? <Check className="size-3.5" /> : <span className="text-[11px]">#</span>}
              </button>
            </th>
            {columns.map((column) => {
              const sorted = sortKey === column.key;
              const badge = roleBadge(copy, column.role);
              // A badge that just repeats the label is noise; the tooltip keeps the
              // full role discoverable either way.
              const showBadge = Boolean(badge) && badge !== column.label.trim();
              return (
                <th
                  key={column.key}
                  className={cn("sticky top-0 z-10 border-r border-b bg-muted px-0 py-0 text-left align-middle font-medium", sorted && "bg-primary/10")}
                  style={{
                    width: columnWidth(column),
                    minWidth: columnWidth(column),
                  }}
                >
                  <div className="relative flex h-8 items-center gap-1 pl-2">
                    {renaming?.key === column.key ? (
                      <Input
                        autoFocus
                        className="h-6 min-w-0 flex-1 px-1 text-xs"
                        value={renaming.value}
                        onChange={(event) =>
                          setRenaming({
                            key: column.key,
                            value: event.target.value,
                          })
                        }
                        onBlur={commitRename}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") commitRename();
                          if (event.key === "Escape") setRenaming(null);
                        }}
                      />
                    ) : (
                      <button type="button" className="flex min-w-0 flex-1 items-center gap-1 text-left" onClick={() => props.onSortColumn(column.key)} title={column.role ? `${column.label} · ${roleLabel(copy, column.role)}` : column.label}>
                        <span className="truncate text-xs font-medium">{column.label}</span>
                        {showBadge && <span className="shrink-0 rounded-sm bg-primary/15 px-1 text-[10px] leading-4 text-primary">{badge}</span>}
                        {sorted && (sortDir === "desc" ? <ChevronDown className="size-3 shrink-0" /> : <ArrowUpAZ className="size-3 shrink-0" />)}
                      </button>
                    )}
                    <DropdownMenu>
                      <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="size-6 shrink-0 text-muted-foreground" aria-label={`${copy.priceColumn} ${column.label}`} />}>
                        <ChevronDown className="size-3" />
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="start" className="w-52">
                        <DropdownMenuItem
                          className="gap-2 px-2 py-2"
                          onClick={() =>
                            setRenaming({
                              key: column.key,
                              value: column.label,
                            })
                          }
                        >
                          <Pencil className="size-4" />
                          {copy.priceColumnRename}
                        </DropdownMenuItem>
                        <DropdownMenuItem className="gap-2 px-2 py-2" onClick={() => props.onSortColumn(column.key)}>
                          <ArrowDownAZ className="size-4" />
                          {sortKey === column.key && sortDir === "asc" ? copy.priceColumnSortDesc : copy.priceColumnSortAsc}
                        </DropdownMenuItem>
                        <DropdownMenuItem className="gap-2 px-2 py-2" onClick={() => props.onEditColumn(column.key)}>
                          <Settings2 className="size-4" />
                          {copy.priceColumnSettings}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem className="gap-2 px-2 py-2" onClick={() => props.onInsertColumn(column.key, "left")}>
                          <Columns3 className="size-4" />
                          {copy.priceColumnInsertLeft}
                        </DropdownMenuItem>
                        <DropdownMenuItem className="gap-2 px-2 py-2" onClick={() => props.onInsertColumn(column.key, "right")}>
                          <Columns3 className="size-4" />
                          {copy.priceColumnInsertRight}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem variant="destructive" className="gap-2 px-2 py-2" disabled={columns.length <= 1} onClick={() => props.onDeleteColumn(column.key)}>
                          <Trash2 className="size-4" />
                          {copy.priceColumnDelete}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                  <span
                    role="separator"
                    aria-orientation="vertical"
                    className="absolute top-0 right-0 h-8 w-1.5 cursor-col-resize hover:bg-primary/40"
                    onPointerDown={(event) => startResize(event, column)}
                  />
                </th>
              );
            })}
            <th className="sticky top-0 z-10 border-b bg-muted px-1">
              <button
                type="button"
                className="grid size-7 place-items-center rounded-sm text-muted-foreground hover:bg-muted-foreground/10 hover:text-foreground"
                onClick={onAddColumn}
                aria-label={copy.priceColumnSettings}
                title={copy.priceColumnSettings}
              >
                <Plus className="size-4" />
              </button>
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, rowIndex) => (
            <PriceGridRow
              key={row.id}
              copy={copy}
              row={row}
              rowIndex={rowIndex}
              columns={columns}
              widthOf={columnWidth}
              selected={selected.has(row.id)}
              status={rowStatus[row.id]}
              onToggle={() => props.onToggleRow(row.id)}
              onChangeCell={props.onChangeCell}
              onCommitCell={props.onCommitCell}
              onKeyDown={handleKeyDown}
            />
          ))}
          <tr>
            <td className="sticky left-0 z-10 border-r border-b bg-card" style={{ width: ROW_NUMBER_WIDTH }} />
            <td colSpan={columns.length + 1} className="border-b bg-card px-2 py-1">
              <button type="button" className="flex items-center gap-1.5 px-1 py-1 text-xs text-muted-foreground hover:text-foreground" onClick={onAddRow}>
                <Plus className="size-3.5" />
                {copy.priceListAddRow}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}

const PriceGridRow = memo(function PriceGridRow({
  copy,
  row,
  rowIndex,
  columns,
  widthOf,
  selected,
  status,
  onToggle,
  onChangeCell,
  onCommitCell,
  onKeyDown,
}: {
  copy: Copy;
  row: PriceRow;
  rowIndex: number;
  columns: PriceColumn[];
  widthOf: (column: PriceColumn) => number;
  selected: boolean;
  status?: "saving" | "error";
  onToggle: () => void;
  onChangeCell: (rowId: string, key: string, value: string) => void;
  onCommitCell: (rowId: string, key: string) => void;
  onKeyDown: (event: KeyboardEvent<HTMLInputElement>, rowIndex: number, columnIndex: number) => void;
}) {
  return (
    <tr className={cn("group", selected && "bg-primary/5")}>
      <td className={cn("sticky left-0 z-10 border-r border-b bg-card text-center align-middle", selected && "bg-primary/10")} style={{ width: ROW_NUMBER_WIDTH }}>
        <button type="button" className="grid h-8 w-full place-items-center text-[11px] text-muted-foreground" onClick={onToggle} title={copy.priceListDeleteRows}>
          {selected ? <Check className="size-3.5 text-primary" /> : <span>{rowIndex + 1}</span>}
          {!selected && status === "saving" && <span className="absolute mt-3 size-1.5 animate-pulse rounded-full bg-primary" />}
          {!selected && status === "error" && <span className="absolute mt-3 size-1.5 rounded-full bg-destructive" title={copy.priceListSaveRetry} />}
        </button>
      </td>
      {columns.map((column, columnIndex) => (
        <td key={column.key} className="border-r border-b bg-card p-0 align-middle" style={{ width: widthOf(column), minWidth: widthOf(column) }}>
          <input
            data-cell
            data-row-index={rowIndex}
            data-col-index={columnIndex}
            data-row-id={row.id}
            data-column-key={column.key}
            className="h-8 w-full bg-transparent px-2 text-sm outline-none focus:bg-background focus:ring-2 focus:ring-inset focus:ring-ring/60"
            value={row.data[column.key] ?? ""}
            inputMode={column.type === "number" ? "decimal" : undefined}
            onChange={(event) => onChangeCell(row.id, column.key, event.target.value)}
            onBlur={() => onCommitCell(row.id, column.key)}
            onKeyDown={(event) => {
              if (event.key === "Enter") onCommitCell(row.id, column.key);
              onKeyDown(event, rowIndex, columnIndex);
            }}
          />
        </td>
      ))}
      <td className="border-b bg-card" />
    </tr>
  );
});
