import { useState, type ReactNode } from "react";
import { ChevronRight, Folder, Layers, Tag } from "lucide-react";
import type { Copy } from "../../lib/locale";
import { allProductsSelection, type ProductBrandGroup, type ProductCategory, type ProductNavigation as Navigation, type ProductSelection } from "../../lib/products";
import { Button } from "../ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "../ui/collapsible";
import { Skeleton } from "../ui/skeleton";

export function ProductNavigation({ copy, categories, data, selection, onSelect, loading, error, onRetry }: {
  copy: Copy; categories: ProductCategory[]; data?: Navigation; selection: ProductSelection;
  onSelect: (selection: ProductSelection) => void; loading: boolean; error: boolean; onRetry: () => void;
}) {
  return <nav aria-label={copy.productBrowse} className="space-y-1 p-2">
    <Button variant={selection.brand === null ? "secondary" : "ghost"} className="w-full justify-start" aria-current={selection.brand === null ? "page" : undefined} onClick={() => onSelect(allProductsSelection)}><Layers /><span className="flex-1 text-left">{copy.productAllProducts}</span>{data && <span className="text-xs text-muted-foreground">{data.total}</span>}</Button>
    {loading ? <div className="space-y-3 p-2">{[0, 1, 2].map(i => <Skeleton key={i} className="h-5 w-full" />)}</div> : error ? <div className="p-2 text-sm text-muted-foreground">{copy.loadFailed}<Button variant="link" onClick={onRetry}>{copy.retry}</Button></div> : !data?.brands.length ? <p className="px-2 py-4 text-xs text-muted-foreground">{copy.productEmpty}</p> : <ul className="space-y-1">{data.brands.map(brand => <li key={brand.name}><BrandBranch copy={copy} brand={brand} categories={categories} selection={selection} onSelect={onSelect} onlyBrand={data.brands.length === 1} /></li>)}</ul>}
  </nav>;
}

function Branch({ label, count, selected, icon, children, onSelect, defaultOpen = false }: {
  label: string; count: number; selected: boolean; icon: ReactNode; children?: ReactNode; onSelect: () => void; defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return <Collapsible open={open} onOpenChange={setOpen}>
    <div className="flex min-w-0 items-center gap-0.5">
      {children ? <CollapsibleTrigger aria-label={label} render={<Button variant="ghost" size="icon-xs" className="shrink-0" />}><ChevronRight className={open ? "size-3.5 rotate-90" : "size-3.5"} /></CollapsibleTrigger> : <span className="w-6 shrink-0" />}
      <Button variant={selected ? "secondary" : "ghost"} className="min-w-0 flex-1 justify-start px-1.5" aria-current={selected ? "page" : undefined} title={label} onClick={() => { if (children) setOpen(true); onSelect(); }}>{icon}<span className="min-w-0 flex-1 truncate text-left">{label}</span><span className="text-xs text-muted-foreground">{count}</span></Button>
    </div>
    {children && <CollapsibleContent><ul className="ml-3 border-l pl-1">{children}</ul></CollapsibleContent>}
  </Collapsible>;
}

function BrandBranch({ copy, brand, categories, selection, onSelect, onlyBrand }: {
  copy: Copy; brand: ProductBrandGroup; categories: ProductCategory[]; selection: ProductSelection; onSelect: (selection: ProductSelection) => void; onlyBrand: boolean;
}) {
  const counts = new Map(brand.categories.map(item => [item.categoryId, item.productCount]));
  const parents = new Map(categories.map(category => [category.id, category.parentId]));
  const totals = new Map<string, number>();
  for (const [categoryId, count] of counts) {
    for (let id = categoryId; id; id = parents.get(id) || "") totals.set(id, (totals.get(id) || 0) + count);
  }
  const selectedBrand = selection.brand === brand.name;
  const select = (categoryId = "", uncategorized = false) => onSelect({ brand: brand.name, categoryId, uncategorized });
  function categoryNodes(parentId: string): ReactNode[] {
    return categories.filter(category => category.parentId === parentId && (totals.get(category.id) || 0) > 0).map(category => {
      const children = categoryNodes(category.id);
      return <li key={category.id}><Branch label={category.name} count={totals.get(category.id) || 0} icon={<Folder className="size-3.5" />} selected={selectedBrand && selection.categoryId === category.id} defaultOpen onSelect={() => select(category.id)}>{children.length ? children : undefined}</Branch></li>;
    });
  }
  return <Branch label={brand.name || copy.productUnbranded} count={brand.productCount} icon={<Tag className="size-3.5" />} selected={selectedBrand && !selection.categoryId && !selection.uncategorized} defaultOpen={onlyBrand || selectedBrand} onSelect={() => select()}>
    {categoryNodes("")}
    {(counts.get("") || 0) > 0 && <li><Branch label={copy.productUncategorized} count={counts.get("") || 0} icon={<Folder className="size-3.5" />} selected={selectedBrand && selection.uncategorized} onSelect={() => select("", true)} /></li>}
  </Branch>;
}
