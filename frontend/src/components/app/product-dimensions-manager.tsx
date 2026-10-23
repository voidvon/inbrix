import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, Pencil, Plus, Search, Trash2 } from "lucide-react";
import { deleteProductDimension, listProductDimensions, saveProductDimension, type ProductDimensionRecord } from "../../lib/products";
import type { Copy } from "../../lib/locale";
import { toast, toastError } from "../../lib/toast";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "../ui/input-group";
import { Label } from "../ui/label";
import { ScrollArea } from "../ui/scroll-area";
import { Textarea } from "../ui/textarea";
import { Badge } from "../ui/badge";
import { ErrorState, ListSkeleton } from "./common-states";
import { ProductSelect } from "./product-select";
import { ProductActionConfirmation } from "./product-confirmation";

export function ProductDimensionEditorDialog({ copy, open, dimension, onOpenChange, onSaved }: {
  copy: Copy; open: boolean; dimension?: ProductDimensionRecord; onOpenChange: (open: boolean) => void; onSaved: (dimension: ProductDimensionRecord) => void;
}) {
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="flex max-h-[90dvh] flex-col overflow-hidden sm:max-w-lg">
    <DialogHeader><DialogTitle>{dimension ? copy.productDimensionEdit : copy.productDimensionNew}</DialogTitle><DialogDescription>{copy.productDimensionCreatedHint}</DialogDescription></DialogHeader>
    {open && <DimensionForm key={`${dimension?.key || "new"}-${dimension?.version || 0}`} copy={copy} dimension={dimension} onSaved={onSaved} />}
  </DialogContent></Dialog>;
}
function DimensionForm({ copy, dimension, onSaved }: { copy: Copy; dimension?: ProductDimensionRecord; onSaved: (dimension: ProductDimensionRecord) => void }) {
  const client = useQueryClient();
  const [label, setLabel] = useState(dimension?.label || "");
  const [type, setType] = useState(dimension?.type || "text");
  const [unit, setUnit] = useState(dimension?.unit || "");
  const [options, setOptions] = useState(dimension?.options?.join("\n") || "");
  const [busy, setBusy] = useState(false);
  const used = Boolean(dimension?.productCount);
  async function submit(event: FormEvent) {
    event.preventDefault(); event.stopPropagation(); setBusy(true);
    try {
      const { dimension: saved } = await saveProductDimension({ key: dimension?.key || "", label: label.trim(), type, unit: unit.trim(), options: type === "select" ? options.split(/\r?\n/).map(s => s.trim()).filter(Boolean) : [] }, dimension?.key, dimension?.version);
      await client.invalidateQueries({ queryKey: ["product-dimensions"] });
      toast.success(copy.productDimensionSaved); onSaved(saved);
    } catch (error) { toastError(error, copy.productFailed); }
    finally { setBusy(false); }
  }
  return <form className="flex min-h-0 flex-col gap-4" onSubmit={event => void submit(event)}>
    <ScrollArea className="min-h-0 flex-1"><fieldset disabled={busy} className="space-y-4 px-1 pb-1">
      <div className="space-y-1.5"><Label htmlFor="dimension-name">{copy.productDimensionName}</Label><Input id="dimension-name" required autoFocus value={label} maxLength={80} placeholder={copy.productDimensionPlaceholder} onChange={event => setLabel(event.target.value)} /></div>
      <div className="grid gap-3 sm:grid-cols-2"><div className="space-y-1.5"><Label htmlFor="dimension-type">{copy.productDimensionType}</Label><ProductSelect id="dimension-type" label={copy.productDimensionType} value={type} disabled={busy || used} onValueChange={value => setType(value as typeof type)} options={[{ value: "text", label: copy.priceTypeText }, { value: "number", label: copy.priceTypeNumber }, { value: "select", label: copy.priceTypeSelect }]} /></div><div className="space-y-1.5"><Label htmlFor="dimension-unit">{copy.productDimensionUnit}</Label><Input id="dimension-unit" value={unit} disabled={used} maxLength={20} onChange={event => setUnit(event.target.value)} /></div></div>
      {type === "select" && <div className="space-y-1.5"><Label htmlFor="dimension-options">{copy.productDimensionOptionsLines}</Label><Textarea id="dimension-options" required value={options} rows={6} maxLength={30000} onChange={event => setOptions(event.target.value)} /></div>}
      {used && <p className="text-xs text-muted-foreground">{copy.productDimensionLocked}</p>}
      {dimension && <p className="text-xs text-muted-foreground">{copy.productDimensionChangesHint}</p>}
    </fieldset></ScrollArea>
    <DialogFooter className="shrink-0"><Button type="submit" disabled={busy || !label.trim() || type === "select" && !options.trim()}>{busy && <LoaderCircle className="animate-spin" />}{copy.save}</Button></DialogFooter>
  </form>;
}

export function ProductDimensionsManager({ copy, open, onOpenChange, onChanged }: { copy: Copy; open: boolean; onOpenChange: (open: boolean) => void; onChanged: () => void }) {
  const client = useQueryClient();
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<ProductDimensionRecord | "new" | null>(null);
  const [removing, setRemoving] = useState<ProductDimensionRecord | null>(null);
  const [busy, setBusy] = useState(false);
  const definitions = useQuery({ queryKey: ["product-dimensions"], queryFn: listProductDimensions, enabled: open });
  const matches = (definitions.data?.dimensions || []).filter(d => `${d.label} ${d.unit}`.toLowerCase().includes(query.trim().toLowerCase()));
  async function remove() {
    if (!removing) return; setBusy(true);
    try { await deleteProductDimension(removing); setRemoving(null); await client.invalidateQueries({ queryKey: ["product-dimensions"] }); toast.success(copy.productDimensionRemoved); onChanged(); }
    catch (error) { toastError(error, copy.productFailed); }
    finally { setBusy(false); }
  }
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="flex max-h-[90dvh] min-w-0 flex-col overflow-hidden sm:max-w-3xl">
      <DialogHeader><DialogTitle>{copy.productGlobalDimensions}</DialogTitle><DialogDescription>{copy.productGlobalDimensionsHint}</DialogDescription></DialogHeader>
      <div className="flex flex-wrap gap-2"><InputGroup className="min-w-40 flex-1"><InputGroupAddon><Search /></InputGroupAddon><InputGroupInput aria-label={copy.productDimensionSearch} placeholder={copy.productDimensionSearch} value={query} onChange={event => setQuery(event.target.value)} /></InputGroup><Button type="button" onClick={() => setEditing("new")}><Plus />{copy.productDimensionNew}</Button></div>
      <ScrollArea className="min-h-0 flex-1"><div className="space-y-2 pr-2">
        {definitions.isPending ? <ListSkeleton /> : definitions.isError ? <ErrorState copy={copy} onRetry={() => void definitions.refetch()} /> : !matches.length ? <p className="p-6 text-center text-sm text-muted-foreground">{copy.productDimensionEmpty}</p> : matches.map(d => <div key={d.key} className="flex min-w-0 items-start gap-3 rounded-lg border p-3">
          <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><span className="font-medium">{d.label}</span>{d.unit && <Badge variant="secondary">{d.unit}</Badge>}<Badge variant="outline">{d.type === "select" ? copy.priceTypeSelect : d.type === "number" ? copy.priceTypeNumber : copy.priceTypeText}</Badge></div><p className="mt-1 text-xs text-muted-foreground">{d.productCount} {copy.productDimensionUsage}</p>{d.type === "select" && <p className="mt-1 break-words text-xs text-muted-foreground">{d.options?.join(" · ")}</p>}</div>
          <Button type="button" size="icon" variant="ghost" aria-label={`${copy.productDimensionEdit} ${d.label}`} onClick={() => setEditing(d)}><Pencil /></Button><Button type="button" size="icon" variant="ghost" disabled={d.productCount > 0} aria-label={`${copy.productDelete} ${d.label}`} title={d.productCount > 0 ? copy.productDimensionLocked : copy.productDelete} onClick={() => setRemoving(d)}><Trash2 /></Button>
        </div>)}
      </div></ScrollArea>
    </DialogContent>
    <ProductDimensionEditorDialog copy={copy} open={Boolean(editing)} dimension={editing && editing !== "new" ? editing : undefined} onOpenChange={value => { if (!value) setEditing(null); }} onSaved={() => { setEditing(null); onChanged(); }} />
    <ProductActionConfirmation copy={copy} open={Boolean(removing)} title={`${copy.productDelete} ${removing?.label || ""}`} description={copy.productDimensionRemoveConfirm} pending={busy} onOpenChange={value => { if (!value) setRemoving(null); }} onConfirm={() => void remove()} />
  </Dialog>;
}
