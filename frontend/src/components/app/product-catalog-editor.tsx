import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { listProductDimensions, setProductVariantPrice, type ProductCatalog, type ProductDimension } from "../../lib/products";
import type { Copy } from "../../lib/locale";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "../ui/dropdown-menu";
import { Input } from "../ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import { ProductActionConfirmation } from "./product-confirmation";
import { ProductSelect } from "./product-select";

export function ProductCatalogEditor({ catalog, copy, onChange, disabled }: { catalog: ProductCatalog; copy: Copy; onChange: (catalog: ProductCatalog) => void; disabled: boolean }) {
  const [removing, setRemoving] = useState<{ kind: "dimension"; key: string } | { kind: "variant"; row: number } | null>(null);
  const definitions = useQuery({ queryKey: ["product-dimensions"], queryFn: listProductDimensions });
  const update = (fn: (next: ProductCatalog) => void) => { const next = structuredClone(catalog); fn(next); onChange(next); };
  const available = (definitions.data?.dimensions || []).filter(d => !catalog.dimensions.some(selected => selected.key === d.key));
  function selectDimension(dimension: ProductDimension) {
    if (catalog.dimensions.some(d => d.key === dimension.key)) return;
    const { key, label, type, unit, options } = dimension;
    update(c => { c.dimensions.push({ key, label, type, unit, options }); c.variants.forEach(v => { v.attributes[key] = ""; }); });
  }
  function removeSelection() {
    if (!removing) return;
    update(c => { if (removing.kind === "variant") c.variants.splice(removing.row, 1); else { c.dimensions = c.dimensions.filter(d => d.key !== removing.key); c.variants.forEach(v => { delete v.attributes[removing.key]; }); } }); setRemoving(null);
  }
  return <fieldset disabled={disabled} className="min-w-0 space-y-4" data-testid="product-catalog-editor">
    <div className="flex flex-wrap items-center gap-2" data-testid="product-selected-dimensions">
      {catalog.dimensions.map(d => <div key={d.key} className="flex items-center gap-2 rounded-md border px-2 py-1"><span className="text-sm">{d.label}</span>{d.unit && <Badge variant="secondary">{d.unit}</Badge>}<span className="text-xs text-muted-foreground">{d.type === "select" ? copy.priceTypeSelect : d.type === "number" ? copy.priceTypeNumber : copy.priceTypeText}</span><Button type="button" size="icon-sm" variant="ghost" aria-label={`${copy.productDimensionUnselect} ${d.label}`} onClick={() => { if (catalog.variants.length) setRemoving({ kind: "dimension", key: d.key }); else update(c => { c.dimensions = c.dimensions.filter(item => item.key !== d.key); }); }}><Trash2 /></Button></div>)}
      <DropdownMenu>
        <DropdownMenuTrigger disabled={disabled || catalog.dimensions.length >= 40} render={<Button type="button" variant="outline" size="sm" />}><Plus />{copy.productDimensionAdd}</DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="max-h-72 w-64 max-w-[calc(100vw-2rem)]">
          {definitions.isPending ? <DropdownMenuItem disabled>{copy.loading}</DropdownMenuItem> : definitions.isError ? <DropdownMenuItem onClick={() => void definitions.refetch()}>{copy.retry}</DropdownMenuItem> : available.length ? available.map(d => <DropdownMenuItem key={d.key} onClick={() => selectDimension(d)}>{d.label}{d.unit && ` (${d.unit})`}<span className="ml-auto text-xs text-muted-foreground">{d.type === "select" ? copy.priceTypeSelect : d.type === "number" ? copy.priceTypeNumber : copy.priceTypeText}</span></DropdownMenuItem>) : <DropdownMenuItem disabled>{copy.productDimensionNoneAvailable}</DropdownMenuItem>}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
    <div className="flex flex-wrap gap-2">
      <Button type="button" variant="outline" size="sm" disabled={catalog.variants.length >= 500} onClick={() => update(c => { c.variants.push({ id: "", sku: "", attributes: Object.fromEntries(c.dimensions.map(d => [d.key, ""])), prices: [] }); })}><Plus />{copy.productVariantAdd}</Button>
    </div>
    {!catalog.variants.length ? <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">{copy.productNoVariants}</p> : <div className="min-w-0 rounded-lg border">
      <Table><TableHeader><TableRow><TableHead>#</TableHead><TableHead>{copy.productVariantSKU}</TableHead>{catalog.dimensions.map(d => <TableHead key={d.key}>{d.label}{d.unit && ` (${d.unit})`}</TableHead>)}<TableHead>{copy.productPriceAmount}</TableHead><TableHead /></TableRow></TableHeader>
        <TableBody>{catalog.variants.map((variant, row) => <CatalogRows key={variant.id || `draft-${row}`} row={row} catalog={catalog} copy={copy} update={update} disabled={disabled} onRemove={() => setRemoving({ kind: "variant", row })} />)}</TableBody>
      </Table>
    </div>}
    <ProductActionConfirmation copy={copy} open={Boolean(removing)} title={copy.productDelete} description={removing?.kind === "dimension" ? copy.productDimensionDelete : copy.productVariantDelete} onOpenChange={value => { if (!value) setRemoving(null); }} onConfirm={removeSelection} />
  </fieldset>;
}
function CatalogRows({ row, catalog, copy, update, disabled, onRemove }: { row: number; catalog: ProductCatalog; copy: Copy; update: (fn: (next: ProductCatalog) => void) => void; disabled: boolean; onRemove: () => void }) {
  const variant = catalog.variants[row]; const price = variant.prices.length === 1 ? variant.prices[0] : undefined;
  const hasLegacyPrices = variant.prices.length > 1 || variant.prices.some(p => p.taxRate || p.taxMode || p.minQty || p.maxQty || p.validFrom || p.validUntil);
  return <>
    <TableRow>
      <TableCell className="text-xs text-muted-foreground">{row + 1}</TableCell>
      <TableCell><Input className="min-w-32" aria-label={`${copy.productVariantSKU} ${row + 1}`} value={variant.sku} maxLength={160} onChange={e => update(c => { c.variants[row].sku = e.target.value; })} /></TableCell>
      {catalog.dimensions.map(d => <TableCell key={d.key}>{d.type === "select" ? <ProductSelect className="min-w-28" disabled={disabled} label={`${d.label} ${row + 1}`} value={variant.attributes[d.key] || ""} options={[{ value: "", label: "—" }, ...(d.options || []).map(option => ({ value: option, label: option }))]} onValueChange={value => update(c => { c.variants[row].attributes[d.key] = value; })} /> : <Input className="min-w-28" type={d.type === "number" ? "number" : "text"} step="any" aria-label={`${d.label} ${row + 1}`} value={variant.attributes[d.key] || ""} maxLength={200} onChange={e => update(c => { c.variants[row].attributes[d.key] = e.target.value; })} />}</TableCell>)}
      <TableCell><Input className="min-w-24" aria-label={`${copy.productPriceAmount} ${row + 1}`} aria-describedby={hasLegacyPrices ? `legacy-price-${row}` : undefined} value={price?.amount || ""} placeholder={copy.productPriceUnknown} maxLength={40} inputMode="decimal" onChange={e => update(c => setProductVariantPrice(c.variants[row], e.target.value))} />{hasLegacyPrices && <p id={`legacy-price-${row}`} className="mt-1 max-w-64 text-xs text-muted-foreground">{copy.productLegacyPriceHint}</p>}</TableCell>
      <TableCell><Button type="button" size="icon" variant="ghost" aria-label={`${copy.productDelete} ${copy.productPricing} ${row + 1}`} onClick={onRemove}><Trash2 /></Button></TableCell>
    </TableRow>
  </>;
}
