import { useCallback, useEffect, useRef, useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Download, FileSpreadsheet, Menu, Pencil, Plus, Save, Search, Table2, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import { ApiError, appendPriceRow, createPriceList, deletePriceRow, deletePriceList, getPriceList, importPriceRows, listPriceLists, listPriceRows, updatePriceRowCells, priceListExportURL, savePriceColumns, updatePriceList } from "../../lib/api";
import type { Copy } from "../../lib/locale";
import { cn } from "../../lib/utils";
import type { PriceColumn, PriceColumnRole, PriceColumnType, PriceImportResult, PriceList, PriceRow } from "../../types";
import { useShell } from "./shell-context";
import { EmptyState, ErrorState, ListSkeleton } from "./common-states";
import { PriceGrid } from "./price-grid";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { ScrollArea } from "../ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Textarea } from "../ui/textarea";

const ROW_PAGE_SIZE = 300;

const DRAFT_PREFIX = "draft-";

/** A row that exists only in the browser until its first cell is saved. */
function newDraftRow(seed = 0) {
  return { id: `${DRAFT_PREFIX}${Date.now()}-${seed}`, data: {} as Record<string, string> };
}

function isDraftRow(rowId: string) {
  return rowId.startsWith(DRAFT_PREFIX);
}

/** The column shape a new price list starts from, so it is AI-readable immediately. */
export function defaultPriceColumns(copy: Copy): PriceColumn[] {
  return [
    {
      key: "model",
      label: copy.priceFieldModel,
      type: "text",
      role: "model",
      width: 180,
    },
    {
      key: "name",
      label: copy.priceFieldName,
      type: "text",
      role: "name",
      width: 220,
    },
    {
      key: "unit",
      label: copy.priceFieldUnit,
      type: "text",
      role: "unit",
      width: 90,
    },
    {
      key: "price",
      label: copy.priceFieldPrice,
      type: "number",
      role: "price",
      width: 120,
    },
    {
      key: "currency",
      label: copy.priceFieldCurrency,
      type: "text",
      role: "currency",
      width: 90,
    },
    {
      key: "tax",
      label: copy.priceFieldTax,
      type: "number",
      role: "tax",
      width: 90,
    },
    {
      key: "moq",
      label: copy.priceFieldMOQ,
      type: "number",
      role: "moq",
      width: 90,
    },
    {
      key: "lead_time",
      label: copy.priceFieldLeadTime,
      type: "number",
      role: "lead_time",
      width: 110,
    },
    {
      key: "notes",
      label: copy.priceFieldNotes,
      type: "text",
      role: "",
      width: 200,
    },
  ];
}

function newColumnKey() {
  return `c${Math.random().toString(36).slice(2, 9)}`;
}

function columnTypeLabel(copy: Copy, type: PriceColumnType) {
  switch (type) {
    case "number":
      return copy.priceTypeNumber;
    case "date":
      return copy.priceTypeDate;
    case "select":
      return copy.priceTypeSelect;
    default:
      return copy.priceTypeText;
  }
}

function columnRoleLabel(copy: Copy, role: PriceColumnRole) {
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
      return copy.priceRoleNone;
  }
}

const ROLE_OPTIONS: PriceColumnRole[] = ["", "model", "name", "unit", "price", "currency", "tax", "moq", "lead_time"];
const TYPE_OPTIONS: PriceColumnType[] = ["text", "number", "date", "select"];

export function PriceListPage() {
  const queryClient = useQueryClient();
  const { copy, openMobileMenu } = useShell();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [searchInput, setSearchInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [sort, setSort] = useState<{ key?: string; dir: "asc" | "desc" }>({
    dir: "asc",
  });
  const [rows, setRows] = useState<PriceRow[]>([]);
  const [rowStatus, setRowStatus] = useState<Record<string, "saving" | "error">>({});
  const [pending, setPending] = useState({ cells: 0, rows: 0 });
  const [selectedRowIds, setSelectedRowIds] = useState<string[]>([]);
  const [columns, setColumns] = useState<PriceColumn[]>([]);
  const [createOpen, setCreateOpen] = useState(false);
  const [createName, setCreateName] = useState("");
  const [createDescription, setCreateDescription] = useState("");
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameValue, setRenameValue] = useState("");
  const [editColumnKey, setEditColumnKey] = useState<string | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [importContent, setImportContent] = useState("");
  const [importMode, setImportMode] = useState<"append" | "upsert">("append");
  const [importPreview, setImportPreview] = useState<PriceImportResult | null>(null);
  const loadedRef = useRef("");
  // Autosave bookkeeping. `savedRef` is the last value the server confirmed,
  // `pendingRef` holds changed-but-unsent cells per row, and `savingRef` marks
  // rows with a request in flight so edits never interleave on one row.
  const rowsRef = useRef<PriceRow[]>([]);
  const savedRef = useRef(new Map<string, Record<string, string>>());
  const pendingRef = useRef(new Map<string, Record<string, string>>());
  const timersRef = useRef(new Map<string, number>());
  const savingRef = useRef(new Set<string>());
  const deletedRef = useRef(new Set<string>());
  const selectedIdRef = useRef<string | null>(null);
  rowsRef.current = rows;
  selectedIdRef.current = selectedId;

  const lists = useQuery({
    queryKey: ["price-lists"],
    queryFn: listPriceLists,
    retry: 1,
  });

  // Only pick a list when nothing is selected: an explicit selection (including a
  // list just created, which the refreshed index may not carry yet) must win.
  useEffect(() => {
    if (selectedId) return;
    const available = lists.data?.priceLists ?? [];
    if (available.length > 0) setSelectedId(available[0].id);
  }, [lists.data, selectedId]);


  const detail = useQuery({
    queryKey: ["price-list", selectedId],
    queryFn: () => getPriceList(selectedId!),
    enabled: Boolean(selectedId),
    // Column definitions follow the same rule as the rows below: no stale
    // snapshot survives leaving the list.
    staleTime: 0,
    gcTime: 0,
    retry: 1,
  });

  // A selection that no longer resolves (deleted in another tab) falls back to
  // the index instead of leaving an error pane on screen.
  useEffect(() => {
    if (selectedId && detail.error instanceof ApiError && detail.error.status === 404) setSelectedId(null);
  }, [detail.error, selectedId]);

  const rowsQuery = useQuery({
    queryKey: ["price-rows", selectedId, searchQuery, sort.key, sort.dir],
    queryFn: () =>
      listPriceRows(selectedId!, {
        q: searchQuery,
        sort: sort.key,
        dir: sort.dir,
        limit: ROW_PAGE_SIZE,
      }),
    enabled: Boolean(selectedId),
    placeholderData: keepPreviousData,
    // A page of rows is editor state, not a cache. Keeping no entry after the
    // observer leaves means returning to an earlier search or sort always
    // refetches instead of rendering a stale snapshot.
    staleTime: 0,
    gcTime: 0,
    retry: 1,
  });

  useEffect(() => {
    if (detail.data) setColumns(detail.data.columns);
  }, [detail.data]);

  // Local rows are rebuilt whenever a new page arrives (a different list,
  // search, or sort). Pending edits are settled before a switch, so a rebuild
  // never discards typing.
  useEffect(() => {
    const data = rowsQuery.data;
    if (!data) return;
    const signature = `${selectedId}#${searchQuery}#${sort.key ?? ""}#${sort.dir}#${rowsQuery.dataUpdatedAt}`;
    if (loadedRef.current === signature) return;
    loadedRef.current = signature;
    for (const timer of timersRef.current.values()) window.clearTimeout(timer);
    timersRef.current.clear();
    pendingRef.current.clear();
    savingRef.current.clear();
    deletedRef.current.clear();
    setRowStatus({});
    setPending({ cells: 0, rows: 0 });
    // An empty sheet still shows one blank row so the user can start typing (or
    // paste) straight away; it stays untouched and is only persisted once edited.
    const next = data.rows.length > 0 ? data.rows.map((row) => ({ ...row, data: { ...row.data } })) : [newDraftRow()];
    savedRef.current = new Map(next.filter((row) => !isDraftRow(row.id)).map((row) => [row.id, { ...row.data }]));
    setRows(next);
    setSelectedRowIds([]);
  }, [rowsQuery.data, rowsQuery.dataUpdatedAt, selectedId, searchQuery, sort]);

  const reload = async () => {
    // Anything still queued is written before the refetch, so the fresh page
    // cannot silently discard a pending edit.
    await settle();
    loadedRef.current = "";
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["price-rows"] }),
      queryClient.invalidateQueries({ queryKey: ["price-list", selectedId] }),
      queryClient.invalidateQueries({ queryKey: ["price-lists"] }),
    ]);
  };

  const createMutation = useMutation({
    mutationFn: () =>
      createPriceList({
        name: createName.trim(),
        description: createDescription.trim(),
        columns: defaultPriceColumns(copy),
      }),
    onSuccess: async (result) => {
      setCreateOpen(false);
      setCreateName("");
      setCreateDescription("");
      setSelectedId(result.priceList.id);
      await queryClient.invalidateQueries({ queryKey: ["price-lists"] });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : copy.priceListSaveFailed),
  });

  const renameMutation = useMutation({
    mutationFn: () =>
      updatePriceList(selectedId!, {
        name: renameValue.trim(),
        description: detail.data?.priceList.description ?? "",
      }),
    onSuccess: async () => {
      setRenameOpen(false);
      await queryClient.invalidateQueries({ queryKey: ["price-lists"] });
      await queryClient.invalidateQueries({
        queryKey: ["price-list", selectedId],
      });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : copy.priceListSaveFailed),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deletePriceList(id),
    onSuccess: async (_result, id) => {
      // Deselect first, and drop the list from the cached index before the
      // refetch lands: otherwise the auto-select below would pick the list that
      // was just deleted and refetch it.
      setSelectedId(null);
      queryClient.setQueryData<{ priceLists: PriceList[] }>(["price-lists"], (current) =>
        current ? { ...current, priceLists: current.priceLists.filter((item) => item.id !== id) } : current,
      );
      await queryClient.invalidateQueries({ queryKey: ["price-lists"] });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : copy.priceListSaveFailed),
  });

  const columnsMutation = useMutation({
    mutationFn: (next: PriceColumn[]) => savePriceColumns(selectedId!, next),
    onSuccess: async (result) => {
      setColumns(result.columns);
      await reload();
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : copy.priceListSaveFailed);
      void queryClient.invalidateQueries({
        queryKey: ["price-list", selectedId],
      });
    },
  });

  const syncPending = useCallback(() => {
    let cells = 0;
    for (const rowCells of pendingRef.current.values()) cells += Object.keys(rowCells).length;
    setPending({ cells, rows: pendingRef.current.size });
  }, []);

  const clearTimer = useCallback((rowId: string) => {
    const timer = timersRef.current.get(rowId);
    if (timer !== undefined) {
      window.clearTimeout(timer);
      timersRef.current.delete(rowId);
    }
  }, []);

  // A ref breaks the mutual reference between scheduling and flushing, so both
  // callbacks keep stable identities.
  const flushRowRef = useRef<(rowId: string) => void>(() => {});

  const scheduleFlush = useCallback(
    (rowId: string) => {
      clearTimer(rowId);
      timersRef.current.set(
        rowId,
        window.setTimeout(() => void flushRowRef.current(rowId), 250),
      );
    },
    [clearTimer],
  );

  // flushRow sends one row's changed cells and nothing else, so a large sheet
  // never resends itself. A request already in flight for that row is left to
  // finish; anything that arrives meanwhile is picked up by the follow-up.
  const flushRow = useCallback(
    async (rowId: string) => {
      clearTimer(rowId);
      if (deletedRef.current.has(rowId)) {
        pendingRef.current.delete(rowId);
        syncPending();
        return;
      }
      if (savingRef.current.has(rowId)) return;
      const cells = pendingRef.current.get(rowId);
      const listId = selectedIdRef.current;
      if (!cells || Object.keys(cells).length === 0 || !listId) return;
      pendingRef.current.delete(rowId);
      savingRef.current.add(rowId);
      syncPending();
      setRowStatus((prev) => ({ ...prev, [rowId]: "saving" }));
      try {
        if (isDraftRow(rowId)) {
          const local = rowsRef.current.find((row) => row.id === rowId);
          const { row } = await appendPriceRow(listId, local?.data ?? cells);
          const carried = pendingRef.current.get(rowId);
          pendingRef.current.delete(rowId);
          savedRef.current.delete(rowId);
          savedRef.current.set(row.id, { ...row.data });
          if (carried) pendingRef.current.set(row.id, carried);
          setRows((prev) =>
            prev.map((current) => {
              if (current.id !== rowId) return current;
              const merged = { ...row.data };
              if (carried) for (const key of Object.keys(carried)) merged[key] = current.data[key] ?? carried[key];
              return { ...row, data: merged };
            }),
          );
          setRowStatus((prev) => {
            const next = { ...prev };
            delete next[rowId];
            return next;
          });
          if (deletedRef.current.has(rowId)) {
            deletedRef.current.add(row.id);
            void deletePriceRow(listId, row.id).catch(() => undefined);
          } else if (carried) {
            scheduleFlush(row.id);
          }
          void queryClient.invalidateQueries({ queryKey: ["price-lists"] });
        } else {
          const { row } = await updatePriceRowCells(listId, rowId, cells);
          const confirmed = { ...(savedRef.current.get(rowId) ?? {}) };
          for (const key of Object.keys(cells)) confirmed[key] = row?.data?.[key] ?? cells[key];
          savedRef.current.set(rowId, confirmed);
          setRows((prev) =>
            prev.map((current) => {
              if (current.id !== rowId || !row?.data) return current;
              const next = { ...current.data };
              // Only the cells this request owned are overwritten, so a value typed
              // while it was in flight survives.
              for (const key of Object.keys(cells)) if (key in row.data) next[key] = row.data[key];
              return { ...current, data: next };
            }),
          );
          setRowStatus((prev) => {
            const next = { ...prev };
            delete next[rowId];
            return next;
          });
        }
      } catch (error) {
        const restore = pendingRef.current.get(rowId) ?? {};
        pendingRef.current.set(rowId, { ...cells, ...restore });
        setRowStatus((prev) => ({ ...prev, [rowId]: "error" }));
        toast.error(error instanceof Error ? error.message : copy.priceListSaveRetry, { id: "price-list-save" });
      } finally {
        savingRef.current.delete(rowId);
        syncPending();
        const remaining = pendingRef.current.get(rowId);
        if (remaining && Object.keys(remaining).length > 0) scheduleFlush(rowId);
      }
    },
    [clearTimer, copy.priceListSaveRetry, queryClient, scheduleFlush, syncPending],
  );
  flushRowRef.current = (rowId: string) => void flushRow(rowId);

  // A manual save also commits the cell that still holds focus, which has not
  // reached its blur handler yet.
  const flushNow = async () => {
    const active = document.activeElement;
    if (active instanceof HTMLElement && active.dataset.cell !== undefined) {
      active.blur();
      await new Promise((resolve) => window.setTimeout(resolve, 0));
    }
    await settle();
  };

  // settle drains the queue, including edits made while a request was in flight,
  // so a search/sort/list switch never races a pending write.
  const settle = useCallback(async () => {
    for (let round = 0; round < 5; round += 1) {
      if (pendingRef.current.size === 0 && savingRef.current.size === 0) return;
      await Promise.all(Array.from(pendingRef.current.keys()).map((rowId) => flushRow(rowId)));
      if (savingRef.current.size > 0) await new Promise((resolve) => window.setTimeout(resolve, 60));
    }
  }, [flushRow]);

  // The search box debounces, then settles pending edits before the query key
  // changes so a pending write can never race the reload.
  useEffect(() => {
    if (searchInput === searchQuery) return;
    const timer = window.setTimeout(() => {
      void settle().then(() => setSearchQuery(searchInput));
    }, 250);
    return () => window.clearTimeout(timer);
  }, [searchInput, searchQuery, settle]);

  const importMutation = useMutation({
    mutationFn: (dryRun: boolean) =>
      importPriceRows(selectedId!, {
        content: importContent,
        mode: importMode,
        dryRun,
        createColumns: true,
      }),
    onSuccess: async (result, dryRun) => {
      setImportPreview(result.result);
      if (dryRun) return;
      setImportOpen(false);
      setImportContent("");
      setImportPreview(null);
      toast.success(`${copy.priceImportAdded} ${result.result.added} · ${copy.priceImportUpdated} ${result.result.updated}`);
      await reload();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : copy.priceImportFailed),
  });

  const list = detail.data?.priceList;
  const savingRows = Object.values(rowStatus).filter((value) => value === "saving").length;
  const hasPending = pending.cells > 0 || pending.rows > 0 || savingRows > 0;
  const draftHasContent = rows.some((row) => Object.values(row.data).some((value) => value.trim() !== ""));

  // Leaving is guarded both for queued writes and for a cell that is still being
  // edited (typed but not yet blurred, so not queued yet).
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => {
      const active = document.activeElement;
      const editingCell = active instanceof HTMLElement && active.dataset.cell !== undefined;
      if (!hasPending && !editingCell) return;
      event.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [hasPending]);

  const changeCell = (rowId: string, key: string, value: string) => {
    setRows((prev) => prev.map((row) => (row.id === rowId ? { ...row, data: { ...row.data, [key]: value } } : row)));
  };

  // The autosave point: on blur (or Enter) only the cells whose value actually
  // differs from the last confirmed value are queued for the server.
  const commitCell = useCallback(
    (rowId: string, key: string) => {
      const row = rowsRef.current.find((current) => current.id === rowId);
      if (!row) return;
      const value = row.data[key] ?? "";
      const savedValue = savedRef.current.get(rowId)?.[key] ?? "";
      const queued = pendingRef.current.get(rowId) ?? {};
      if (value === savedValue) {
        if (!(key in queued)) return;
        delete queued[key];
        if (Object.keys(queued).length === 0) {
          pendingRef.current.delete(rowId);
          clearTimer(rowId);
        } else {
          pendingRef.current.set(rowId, queued);
        }
        syncPending();
        return;
      }
      queued[key] = value;
      pendingRef.current.set(rowId, queued);
      setRowStatus((prev) => {
        if (!prev[rowId]) return prev;
        const next = { ...prev };
        delete next[rowId];
        return next;
      });
      syncPending();
      scheduleFlush(rowId);
    },
    [clearTimer, scheduleFlush, syncPending],
  );

  // A pasted rectangle grows the sheet when it runs past the last row, which is
  // how pasting a block into a spreadsheet behaves. Everything it touched is
  // queued for autosave.
  const pasteBlock = (anchorRowId: string, anchorKey: string, matrix: string[][]) => {
    const startRow = rowsRef.current.findIndex((row) => row.id === anchorRowId);
    const startColumn = columns.findIndex((column) => column.key === anchorKey);
    if (startRow < 0 || startColumn < 0) return;
    const next = rowsRef.current.map((row) => ({ ...row, data: { ...row.data } }));
    const touched: string[] = [];
    matrix.forEach((cells, rowOffset) => {
      const rowIndex = startRow + rowOffset;
      while (next.length <= rowIndex) next.push(newDraftRow(next.length));
      const target = next[rowIndex];
      cells.forEach((value, columnOffset) => {
        const column = columns[startColumn + columnOffset];
        if (!column) return;
        target.data[column.key] = value;
      });
      touched.push(target.id);
    });
    setRows(next);
    rowsRef.current = next;
    for (const rowId of touched) {
      const row = next.find((current) => current.id === rowId);
      if (!row) continue;
      const queued = pendingRef.current.get(rowId) ?? {};
      for (const column of columns) {
        const value = row.data[column.key] ?? "";
        const savedValue = savedRef.current.get(rowId)?.[column.key] ?? "";
        if (value !== savedValue) queued[column.key] = value;
      }
      if (Object.keys(queued).length > 0) {
        pendingRef.current.set(rowId, queued);
        scheduleFlush(rowId);
      }
    }
    syncPending();
  };

  const addRow = () => {
    setRows((prev) => [...prev, newDraftRow(prev.length)]);
  };

  const deleteSelectedRows = async () => {
    const ids = selectedRowIds;
    const listId = selectedIdRef.current;
    if (ids.length === 0 || !listId) return;
    if (!window.confirm(copy.priceListDeleteRowsConfirm)) return;
    const removed = rowsRef.current.filter((row) => ids.includes(row.id));
    for (const id of ids) {
      deletedRef.current.add(id);
      clearTimer(id);
      pendingRef.current.delete(id);
      savedRef.current.delete(id);
    }
    setRows((prev) => prev.filter((row) => !ids.includes(row.id)));
    setSelectedRowIds([]);
    syncPending();
    const existing = removed.filter((row) => !isDraftRow(row.id));
    const results = await Promise.allSettled(existing.map((row) => deletePriceRow(listId, row.id)));
    const failed = results.filter((result) => result.status === "rejected").length;
    if (failed > 0) {
      toast.error(copy.priceListSaveRetry, { id: "price-list-delete" });
      await reload();
      return;
    }
    void queryClient.invalidateQueries({ queryKey: ["price-lists"] });
  };

  const applyColumns = (next: PriceColumn[]) => {
    setColumns(next);
    columnsMutation.mutate(next.map((column, index) => ({ ...column, position: index })));
  };

  const resizeColumn = (key: string, width: number) => {
    applyColumns(columns.map((column) => (column.key === key ? { ...column, width } : column)));
  };

  const renameColumn = (key: string, label: string) => {
    applyColumns(columns.map((column) => (column.key === key ? { ...column, label } : column)));
  };

  const sortColumn = (key: string) => {
    void settle().then(() =>
      setSort((prev) => (prev.key === key ? { key, dir: prev.dir === "asc" ? "desc" : "asc" } : { key, dir: "asc" })),
    );
  };

  const insertColumn = (key: string, side: "left" | "right") => {
    const index = columns.findIndex((column) => column.key === key);
    if (index < 0) return;
    const column: PriceColumn = {
      key: newColumnKey(),
      label: copy.priceColumn,
      type: "text",
      role: "",
      width: 160,
    };
    const next = [...columns];
    next.splice(side === "left" ? index : index + 1, 0, column);
    applyColumns(next);
    setEditColumnKey(column.key);
  };

  const addColumn = () => {
    const column: PriceColumn = {
      key: newColumnKey(),
      label: copy.priceColumn,
      type: "text",
      role: "",
      width: 160,
    };
    applyColumns([...columns, column]);
    setEditColumnKey(column.key);
  };

  const deleteColumn = (key: string) => {
    if (columns.length <= 1) return;
    if (!window.confirm(copy.priceColumnDeleteConfirm)) return;
    applyColumns(columns.filter((column) => column.key !== key));
  };

  const editingColumn = editColumnKey ? columns.find((column) => column.key === editColumnKey) : undefined;

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    const text = await file.text();
    setImportContent(text);
    setImportPreview(null);
  };

  return (
    <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
      <section className={cn("flex w-full min-w-0 flex-col border-r bg-card lg:w-72 lg:flex-none", selectedId && "hidden lg:flex")}>
        <div className="flex h-14 shrink-0 items-center gap-2 border-b px-3">
          <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={openMobileMenu} aria-label={copy.folders}>
            <Menu className="size-4" />
          </Button>
          <strong className="min-w-0 flex-1 truncate text-sm">{copy.priceLists}</strong>
          <Button variant="ghost" size="icon" className="size-8" onClick={() => setCreateOpen(true)} aria-label={copy.newPriceList} title={copy.newPriceList}>
            <Plus className="size-4" />
          </Button>
        </div>
        <ScrollArea className="min-h-0 flex-1">
          {lists.isPending && <ListSkeleton />}
          {lists.error && <ErrorState copy={copy} onRetry={() => void lists.refetch()} />}
          {lists.data && lists.data.priceLists.length === 0 && (
            <div className="grid gap-2 p-6 text-center">
              <Table2 className="mx-auto size-7 text-muted-foreground" />
              <strong className="text-sm font-medium">{copy.priceListEmpty}</strong>
              <p className="text-xs text-muted-foreground">{copy.priceListEmptyHint}</p>
            </div>
          )}
          {(lists.data?.priceLists ?? []).map((item) => (
            <button
              key={item.id}
              type="button"
              onClick={() => {
                void settle().then(() => {
                  setSelectedId(item.id);
                  setSearchInput("");
                  setSearchQuery("");
                });
              }}
              className={cn("block w-full border-b px-4 py-3 text-left transition-colors hover:bg-muted", item.id === selectedId && "border-l-2 border-l-foreground bg-muted pl-[0.875rem]")}
            >
              <span className="flex items-baseline justify-between gap-2">
                <strong className="min-w-0 truncate text-sm font-medium">{item.name}</strong>
                {hasPending && item.id === selectedId && <span className="size-1.5 shrink-0 rounded-full bg-amber-500" />}
              </span>
              <span className="mt-1 flex items-center gap-1.5 text-[11px] text-muted-foreground">
                <span>
                  {item.rowCount} {copy.priceListRows}
                </span>
                <span>·</span>
                <span>
                  {item.columnCount} {copy.priceListColumns}
                </span>
              </span>
            </button>
          ))}
        </ScrollArea>
      </section>

      {selectedId ? (
        <section className="flex min-w-0 flex-1 flex-col bg-surface">
          <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-card px-3">
            <Button variant="ghost" size="icon" className="size-8 shrink-0 lg:hidden" onClick={() => setSelectedId(null)} aria-label={copy.back}>
              <ArrowLeft className="size-4" />
            </Button>
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <strong className="truncate text-sm font-semibold">{list?.name ?? copy.priceLists}</strong>
                {savingRows > 0 ? (
                  <Badge variant="secondary" className="shrink-0 px-1.5 text-[10px]">
                    {copy.priceListSaving}
                  </Badge>
                ) : pending.cells > 0 ? (
                  <Badge variant="secondary" className="shrink-0 px-1.5 text-[10px]">
                    {pending.cells} {copy.priceListPending}
                  </Badge>
                ) : null}
              </div>
              <p className="truncate text-[11px] text-muted-foreground">
                {rowsQuery.data ? `${rowsQuery.data.total} ${copy.priceListRows}` : ""}
                {list?.description ? ` · ${list.description}` : ""}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="size-8"
              onClick={() => {
                setRenameValue(list?.name ?? "");
                setRenameOpen(true);
              }}
              aria-label={copy.priceListRename}
              title={copy.priceListRename}
            >
              <Pencil className="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-8 text-destructive"
              onClick={() => {
                if (list && window.confirm(copy.priceListDeleteConfirm)) deleteMutation.mutate(list.id);
              }}
              aria-label={copy.priceListDelete}
              title={copy.priceListDelete}
            >
              <Trash2 className="size-4" />
            </Button>
            <Button size="sm" className="h-8 gap-1.5" disabled={!hasPending} onClick={() => void flushNow()}>
              <Save className="size-3.5" />
              {savingRows > 0 ? copy.priceListSaving : copy.priceListSave}
            </Button>
          </header>

          <div className="flex shrink-0 flex-wrap items-center gap-2 border-b bg-card px-3 py-2">
            <div className="relative flex min-w-48 flex-1 items-center">
              <Search className="pointer-events-none absolute left-3 size-3.5 text-muted-foreground" aria-hidden="true" />
              <Input
                className="h-8 bg-muted/60 pl-8 text-xs"
                value={searchInput}
                onChange={(event) => setSearchInput(event.target.value)}
                placeholder={copy.priceListSearch}
                aria-label={copy.priceListSearch}
              />
            </div>
            <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs" onClick={addRow}>
              <Plus className="size-3.5" />
              {copy.priceListAddRow}
            </Button>
            <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs text-destructive" disabled={selectedRowIds.length === 0} onClick={() => void deleteSelectedRows()}>
              <Trash2 className="size-3.5" />
              {copy.priceListDeleteRows}
              {selectedRowIds.length > 0 ? ` (${selectedRowIds.length})` : ""}
            </Button>
            <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs" onClick={() => setImportOpen(true)}>
              <Upload className="size-3.5" />
              {copy.priceListImport}
            </Button>
            <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs" onClick={() => window.location.assign(priceListExportURL(selectedId))}>
              <Download className="size-3.5" />
              {copy.priceListExport}
            </Button>
            <span className="hidden text-[11px] text-muted-foreground xl:inline" title={copy.priceListPasteHint}>
              {copy.priceListAutoSave}
            </span>
          </div>

          {detail.isPending ? (
            <ListSkeleton />
          ) : detail.error ? (
            <ErrorState copy={copy} onRetry={() => void detail.refetch()} />
          ) : (
            <>
              {rowsQuery.data?.total === 0 && !draftHasContent && !rowsQuery.isFetching && <p className="shrink-0 border-b bg-muted/40 px-3 py-1.5 text-[11px] text-muted-foreground">{copy.priceListNoRows}</p>}
              <PriceGrid
                copy={copy}
                columns={columns}
                rows={rows}
                selectedIds={selectedRowIds}
                rowStatus={rowStatus}
                sortKey={sort.key}
                sortDir={sort.dir}
                loading={rowsQuery.isFetching}
                onToggleRow={(id) => setSelectedRowIds((prev) => (prev.includes(id) ? prev.filter((value) => value !== id) : [...prev, id]))}
                onToggleAll={() => setSelectedRowIds((prev) => (prev.length === rows.length ? [] : rows.map((row) => row.id)))}
                onChangeCell={changeCell}
                onCommitCell={commitCell}
                onPasteBlock={pasteBlock}
                onResizeColumn={resizeColumn}
                onRenameColumn={renameColumn}
                onSortColumn={sortColumn}
                onEditColumn={(key) => setEditColumnKey(key)}
                onInsertColumn={insertColumn}
                onDeleteColumn={deleteColumn}
                onAddColumn={addColumn}
                onAddRow={addRow}
              />
            </>
          )}
        </section>
      ) : (
        <section className="hidden min-w-0 flex-1 flex-col bg-surface lg:flex">
          <EmptyState icon={<FileSpreadsheet />} text={lists.data?.priceLists.length ? copy.priceListSelect : copy.priceListEmptyHint} />
        </section>
      )}

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{copy.newPriceList}</DialogTitle>
            <DialogDescription>{copy.priceListsHint}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="grid gap-1.5">
              <Label htmlFor="price-list-name">{copy.priceListName}</Label>
              <Input id="price-list-name" value={createName} onChange={(event) => setCreateName(event.target.value)} placeholder={copy.priceFieldModel} autoFocus />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="price-list-description">{copy.priceListDescription}</Label>
              <Input id="price-list-description" value={createDescription} onChange={(event) => setCreateDescription(event.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>
              {copy.cancel}
            </Button>
            <Button disabled={!createName.trim() || createMutation.isPending} onClick={() => createMutation.mutate()}>
              {copy.priceListCreate}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{copy.priceListRename}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="price-list-rename">{copy.priceListName}</Label>
            <Input id="price-list-rename" value={renameValue} onChange={(event) => setRenameValue(event.target.value)} autoFocus />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setRenameOpen(false)}>
              {copy.cancel}
            </Button>
            <Button disabled={!renameValue.trim() || renameMutation.isPending} onClick={() => renameMutation.mutate()}>
              {copy.priceListSave}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(editingColumn)}
        onOpenChange={(open) => {
          if (!open) setEditColumnKey(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{copy.priceColumnSettings}</DialogTitle>
            <DialogDescription>{copy.priceListsHint}</DialogDescription>
          </DialogHeader>
          {editingColumn && (
            <div className="grid gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor="price-column-label">{copy.priceColumnLabel}</Label>
                <Input
                  id="price-column-label"
                  value={editingColumn.label}
                  onChange={(event) => setColumns((prev) => prev.map((column) => (column.key === editingColumn.key ? { ...column, label: event.target.value } : column)))}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="grid gap-1.5">
                  <Label>{copy.priceColumnType}</Label>
                  <Select
                    value={editingColumn.type}
                    onValueChange={(value) => setColumns((prev) => prev.map((column) => (column.key === editingColumn.key ? { ...column, type: value as PriceColumnType } : column)))}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {TYPE_OPTIONS.map((type) => (
                        <SelectItem key={type} value={type}>
                          {columnTypeLabel(copy, type)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-1.5">
                  <Label>{copy.priceColumnRole}</Label>
                  <Select
                    value={editingColumn.role || "none"}
                    onValueChange={(value) =>
                      setColumns((prev) =>
                        prev.map((column) =>
                          column.key === editingColumn.key
                            ? {
                                ...column,
                                role: value === "none" ? "" : (value as PriceColumnRole),
                              }
                            : column,
                        ),
                      )
                    }
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {ROLE_OPTIONS.map((role) => (
                        <SelectItem key={role || "none"} value={role || "none"}>
                          {columnRoleLabel(copy, role)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                setEditColumnKey(null);
                void queryClient.invalidateQueries({
                  queryKey: ["price-list", selectedId],
                });
              }}
            >
              {copy.cancel}
            </Button>
            <Button
              disabled={columnsMutation.isPending}
              onClick={() => {
                setEditColumnKey(null);
                applyColumns(columns);
              }}
            >
              {copy.priceListSave}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={importOpen}
        onOpenChange={(open) => {
          setImportOpen(open);
          if (!open) setImportPreview(null);
        }}
      >
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{copy.priceImportTitle}</DialogTitle>
            <DialogDescription>{copy.priceImportHint}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="grid gap-1.5">
              <Label htmlFor="price-import-content">{copy.priceImportContent}</Label>
              <Textarea
                id="price-import-content"
                className="min-h-32 font-mono text-xs"
                value={importContent}
                onChange={(event) => {
                  setImportContent(event.target.value);
                  setImportPreview(null);
                }}
                placeholder={"型号,名称,单价\nDN50-BV,DN50 球阀,1280"}
              />
            </div>
            <div className="flex flex-wrap items-end gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor="price-import-file">{copy.priceListImport}</Label>
                <Input id="price-import-file" type="file" accept=".csv,text/csv,text/plain" className="h-9 text-xs" onChange={(event) => void onFile(event.target.files?.[0])} />
              </div>
              <div className="grid gap-1.5">
                <Label>{copy.priceImportMode}</Label>
                <Select value={importMode} onValueChange={(value) => setImportMode(value as "append" | "upsert")}>
                  <SelectTrigger className="w-44">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="append">{copy.priceImportModeAppend}</SelectItem>
                    <SelectItem value="upsert">{copy.priceImportModeUpsert}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            {importPreview && (
              <div className="rounded-lg border bg-muted/40 p-3 text-xs">
                <div className="flex flex-wrap items-center gap-3">
                  <span>
                    {copy.priceImportAdded} <strong>{importPreview.added}</strong>
                  </span>
                  <span>
                    {copy.priceImportUpdated} <strong>{importPreview.updated}</strong>
                  </span>
                  <span>
                    {copy.priceImportSkipped} <strong>{importPreview.skipped}</strong>
                  </span>
                  {importPreview.createdColumns?.length ? (
                    <span>
                      {copy.priceImportCreatedColumns} <strong>{importPreview.createdColumns.map((column) => column.label).join(", ")}</strong>
                    </span>
                  ) : null}
                </div>
                {importPreview.preview?.length ? (
                  <div className="mt-2 overflow-x-auto">
                    <table className="text-[11px]">
                      <tbody>
                        {importPreview.preview.map((row, index) => (
                          <tr key={index}>
                            {row.map((cell, cellIndex) => (
                              <td key={cellIndex} className="border px-1.5 py-0.5 whitespace-nowrap">
                                {cell}
                              </td>
                            ))}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : null}
                {importPreview.errors?.length ? <p className="mt-2 text-destructive">{importPreview.errors.join("; ")}</p> : null}
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setImportOpen(false)}>
              {copy.cancel}
            </Button>
            <Button variant="outline" disabled={!importContent.trim() || importMutation.isPending} onClick={() => importMutation.mutate(true)}>
              {copy.priceImportPreview}
            </Button>
            <Button disabled={!importContent.trim() || importMutation.isPending} onClick={() => importMutation.mutate(false)}>
              {copy.priceImportApply}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </main>
  );
}
