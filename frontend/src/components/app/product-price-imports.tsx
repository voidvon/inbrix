import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import { emptyProductPrice, getProduct, importProductPrice, pendingProductPrices, searchProducts, type PendingProductPrice, type ProductPrice } from "../../lib/products";
import type { Copy } from "../../lib/locale";
import { toast, toastError } from "../../lib/toast";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { ScrollArea } from "../ui/scroll-area";
import { ProductSelect } from "./product-select";
import { ErrorState, ListSkeleton } from "./common-states";

function rowPrice(row: PendingProductPrice): ProductPrice {
  const get = (role: string) => row.fields[row.columns.find(c => c.role === role)?.key || ""] || "";
  const currency = get("currency").toUpperCase();
  const isCNY = ["", "CNY", "RMB", "¥", "￥", "人民币"].includes(currency);
  return { ...emptyProductPrice(), amount: isCNY ? get("price") : "" };
}
export function ProductPriceImports({ copy, open, onOpenChange }: { copy: Copy; open: boolean; onOpenChange: (open: boolean) => void }) {
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<PendingProductPrice | null>(null);
  const [price, setPrice] = useState<ProductPrice>(emptyProductPrice);
  const [target, setTarget] = useState("");
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const client = useQueryClient();
  const pending = useQuery({ queryKey: ["product-price-imports", offset], queryFn: () => pendingProductPrices(offset), enabled: open });
  const candidates = useQuery({ queryKey: ["product-import-targets", query], queryFn: () => searchProducts(query), enabled: Boolean(selected) && open });
  async function save() {
    if (!selected) return; setBusy(true);
    try {
      const version = target ? (await getProduct(target)).product.version : 0;
      await importProductPrice(selected.rowId, target, version, price);
      setSelected(null); setOffset(0);
      await Promise.all([client.invalidateQueries({ queryKey: ["product-price-imports"] }), client.invalidateQueries({ queryKey: ["products"] }), client.invalidateQueries({ queryKey: ["product"] }), client.invalidateQueries({ queryKey: ["product-categories"] }), client.invalidateQueries({ queryKey: ["product-navigation"] })]);
      toast.success(copy.productImportDone);
    } catch (error) { toastError(error, copy.productFailed); }
    finally { setBusy(false); }
  }
  return <Dialog open={open} onOpenChange={(value, details) => { if (busy) details.cancel(); else onOpenChange(value); }}><DialogContent className="flex max-h-[90dvh] min-w-0 flex-col overflow-hidden sm:max-w-4xl"><DialogHeader><DialogTitle>{copy.productPendingPrices} {pending.data ? `(${pending.data.total})` : ""}</DialogTitle><DialogDescription>{copy.productPendingHint}</DialogDescription></DialogHeader>
    <ScrollArea className="min-h-0 flex-1"><div className="min-w-0 space-y-4 pr-3">
      {selected ? <div className="space-y-4">
        <Button type="button" variant="ghost" disabled={busy} onClick={() => setSelected(null)}>← {copy.productPrevious}</Button>
        <div className="rounded-lg border p-3"><p className="mb-2 font-medium">{selected.name || selected.model} · {selected.listName}</p><dl className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-3">{selected.columns.map(c => <div key={c.key} className="min-w-0"><dt className="text-muted-foreground">{c.label}</dt><dd className="whitespace-pre-wrap break-words">{selected.fields[c.key] || "—"}</dd></div>)}</dl></div>
        <div className="space-y-2"><Label htmlFor="import-target-search">{copy.productImportTarget}</Label><Input id="import-target-search" placeholder={copy.productSearch} disabled={busy} value={query} onChange={e => { setQuery(e.target.value); setTarget(""); }} />
          <ProductSelect label={copy.productImportTarget} value={target} disabled={busy} onValueChange={setTarget} options={[{ value: "", label: copy.productImportNew }, ...(candidates.data?.products || []).map(p => ({ value: p.id, label: [p.name, p.brand, p.model].filter(Boolean).join(" · ") }))]} />
          {candidates.isError && <ErrorState copy={copy} onRetry={() => void candidates.refetch()} />}
        </div>
        <div className="space-y-2"><Label htmlFor="import-price">{copy.productPriceAmount}</Label><Input id="import-price" inputMode="decimal" maxLength={40} value={price.amount} disabled={busy} onChange={event => setPrice({ ...emptyProductPrice(), amount: event.target.value })} /></div>
        <Button type="button" disabled={busy || !price.amount} onClick={() => void save()}>{busy && <LoaderCircle className="animate-spin" />}{copy.productImportConfirm}</Button>
      </div> : pending.isPending ? <ListSkeleton /> : pending.isError ? <ErrorState copy={copy} onRetry={() => void pending.refetch()} /> : <>
        {!pending.data.total && <p className="p-8 text-center text-sm text-muted-foreground">{copy.productPendingEmpty}</p>}
        {pending.data.rows.map(row => <Button key={row.rowId} type="button" variant="outline" className="h-auto w-full justify-between gap-3 px-3 py-3 text-left whitespace-normal" onClick={() => { setSelected(row); setPrice(rowPrice(row)); setTarget(""); setQuery(row.model || row.name); }}><span className="min-w-0 break-words">{row.name || row.model || row.rowId}<span className="ml-2 text-xs text-muted-foreground">{row.model}</span></span><span className="shrink-0 text-xs text-muted-foreground">{row.listName}</span></Button>)}
        <div className="flex items-center justify-between gap-2"><Button variant="outline" disabled={!offset} onClick={() => setOffset(Math.max(0, offset - 30))}>{copy.productPrevious}</Button><span className="text-xs text-muted-foreground">{pending.data.total}</span><Button variant="outline" disabled={!pending.data.hasMore} onClick={() => setOffset(pending.data.nextOffset)}>{copy.productNext}</Button></div>
      </>}
    </div></ScrollArea>
  </DialogContent></Dialog>;
}
