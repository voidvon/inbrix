import { useEffect, useRef, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { BookOpen, FolderTree, ImagePlus, LoaderCircle, Menu, Pencil, Plus, Search, Trash2 } from "lucide-react";
import { allProductsSelection, listProductNavigation, productCategoryRows, emptyCatalog, pendingProductPrices, deleteProduct, deleteProductCategory, getProduct, listProductCategories, prepareProductImage, saveProduct, saveProductCategory, searchProducts, type Product, type ProductCategory, type ProductInput, type ProductSelection } from "../../lib/products";
import { type Copy } from "../../lib/locale";
import { toast, toastError } from "../../lib/toast";
import { Button } from "../ui/button";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "../ui/alert-dialog";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "../ui/input-group";
import { Label } from "../ui/label";
import { Pagination, PaginationContent, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious } from "../ui/pagination";
import { ScrollArea } from "../ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../ui/tabs";
import { Textarea } from "../ui/textarea";
import { EmptyState, ErrorState, ListSkeleton } from "./common-states";
import { MarkdownContent } from "./markdown-content";
import { useShell } from "./shell-context";

import { ProductCatalogEditor } from "./product-catalog-editor";
import { ProductDimensionsManager } from "./product-dimensions-manager";
import { ProductPriceImports } from "./product-price-imports";
import { ProductNavigation } from "./product-navigation";
import { ProductSelect } from "./product-select";

export function ProductsPage() {
  const { copy, openMobileMenu } = useShell();
  const client = useQueryClient();
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [selection, setSelection] = useState<ProductSelection>(allProductsSelection);
  const [browseOpen, setBrowseOpen] = useState(false);
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<string | null>(() => new URLSearchParams(window.location.search).get("product"));
  const [importsOpen, setImportsOpen] = useState(() => window.location.pathname === "/price-lists");
  const imports = useQuery({ queryKey: ["product-price-imports", 0], queryFn: () => pendingProductPrices(0) });
  const [dimensionsOpen, setDimensionsOpen] = useState(false);
  const [categoriesOpen, setCategoriesOpen] = useState(false);
  const [removing, setRemoving] = useState<Product | null>(null);
  const [pending, setPending] = useState(false);
  const [discardOpen, setDiscardOpen] = useState(false);
  const editorDirty = useRef(false);
  useEffect(() => {
    const timer = window.setTimeout(() => { setSearch(query.trim()); setOffset(0); }, 250);
    return () => window.clearTimeout(timer);
  }, [query]);
  useEffect(() => {
    const sync = () => setSelected(new URLSearchParams(window.location.search).get("product"));
    window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, []);
  const categories = useQuery({ queryKey: ["product-categories"], queryFn: listProductCategories });
  const navigation = useQuery({ queryKey: ["product-navigation"], queryFn: listProductNavigation });
  useEffect(() => {
    if (selection.brand !== null && navigation.data && !navigation.data.brands.some(brand => brand.name === selection.brand)) {
      setSelection(allProductsSelection); setOffset(0);
    } else if (selection.categoryId && categories.data && !categories.data.categories.some(item => item.id === selection.categoryId)) {
      setSelection(current => ({ ...current, categoryId: "" })); setOffset(0);
    }
  }, [selection, categories.data, navigation.data]);
  const products = useQuery({ queryKey: ["products", search, selection, offset], queryFn: () => searchProducts(search, selection.categoryId, offset, selection.brand, selection.uncategorized) });
  const detail = useQuery({ queryKey: ["product", selected], queryFn: () => getProduct(selected!), enabled: Boolean(selected && selected !== "new"), staleTime: 0, gcTime: 0, refetchOnWindowFocus: false, refetchOnReconnect: false });
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ["products"] });
    void client.invalidateQueries({ queryKey: ["product-categories"] });
    void client.invalidateQueries({ queryKey: ["product-navigation"] });
    void client.invalidateQueries({ queryKey: ["product-dimensions"] });
    void client.invalidateQueries({ queryKey: ["product"] });
  };
  function closeEditor() {
    setDiscardOpen(false);
    setSelected(null);
    if (new URLSearchParams(window.location.search).has("product")) window.history.replaceState(window.history.state, "", "/products");
  }
  async function remove() {
    if (!removing) return;
    setPending(true);
    try {
      await deleteProduct(removing);
      setRemoving(null);
      setOffset(0);
      refresh();
      toast.success(copy.productDeleted);
    } catch (error) { toastError(error, copy.productFailed); }
    finally { setPending(false); }
  }
  const categoryItems = categories.data?.categories || [];
  const selectionLabel = selection.brand === null ? copy.productAllProducts : [selection.brand || copy.productUnbranded, selection.uncategorized ? copy.productUncategorized : productCategoryRows(categoryItems).find(item => item.id === selection.categoryId)?.path].filter(Boolean).join(" / ");
  const navigationContent = <ProductNavigation copy={copy} categories={categoryItems} data={navigation.data} selection={selection} onSelect={value => { setSelection(value); setOffset(0); setBrowseOpen(false); }} loading={categories.isPending || navigation.isPending} error={categories.isError || navigation.isError} onRetry={() => { void categories.refetch(); void navigation.refetch(); }} />;
  return (
    <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden" data-testid="products-page">
      <header className="flex shrink-0 flex-wrap items-center gap-3 border-b px-4 py-3 sm:px-6">
        <Button variant="ghost" size="icon" className="lg:hidden" onClick={openMobileMenu} aria-label={copy.folders}><Menu /></Button>
        <div className="min-w-0 flex-1 basis-[calc(100%-3.5rem)] lg:basis-0"><h1 className="text-lg font-semibold">{copy.products}</h1><p className="text-xs text-muted-foreground">{copy.productsHint}</p></div>
        {(imports.isError || (imports.data?.total || 0) > 0) && <Button variant="outline" onClick={() => setImportsOpen(true)}>{copy.productPendingPrices} {imports.data?.total ? `(${imports.data.total})` : ""}</Button>}
        <Button variant="outline" onClick={() => setDimensionsOpen(true)}>{copy.productGlobalDimensions}</Button>
        <Button variant="outline" aria-label={copy.productCategories} onClick={() => setCategoriesOpen(true)}><FolderTree /><span className="hidden sm:inline">{copy.productCategories}</span></Button>
        <Button onClick={() => setSelected("new")}><Plus />{copy.productNew}</Button>
      </header>
      <div className="flex min-h-0 flex-1">
      <aside className="hidden w-60 shrink-0 flex-col border-r md:flex" aria-label={copy.productBrowse}>
        <div className="border-b px-4 py-3 text-sm font-medium">{copy.productBrowse}</div>
        <ScrollArea className="min-h-0 flex-1">{navigationContent}</ScrollArea>
      </aside>
      <section className="flex min-h-0 min-w-0 flex-1 flex-col" aria-label={selectionLabel}>
      <div className="flex shrink-0 flex-wrap items-center gap-3 border-b px-4 py-3 sm:px-6">
        <Button variant="outline" className="max-w-full justify-start md:hidden" aria-label={copy.productBrowse} onClick={() => setBrowseOpen(true)}><FolderTree /><span className="truncate">{selectionLabel}</span></Button>
        <InputGroup className="min-w-40 flex-1">
          <InputGroupInput aria-label={copy.productSearch} placeholder={copy.productSearch} value={query} onChange={event => setQuery(event.target.value)} maxLength={200} />
          <InputGroupAddon><Search /></InputGroupAddon>
        </InputGroup>
        <span className="hidden max-w-64 truncate text-xs text-muted-foreground md:block" title={selectionLabel}>{selectionLabel}</span>
      </div>
      <ScrollArea className="min-h-0 flex-1 [&_[data-slot=table-container]]:overflow-visible" data-testid="products-table-scroll">
        <Table className="table-fixed" aria-label={copy.products} aria-busy={products.isFetching}>
          <TableHeader className="sticky top-0 z-10 bg-background">
            <TableRow>
              <TableHead scope="col" className="w-20 pl-4 sm:w-24 sm:pl-6">{copy.productThumbnail}</TableHead>
              <TableHead scope="col">{copy.productName}</TableHead>
              <TableHead scope="col" className="w-24 pr-4 text-right sm:w-28 sm:pr-6">{copy.actions}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {categories.isError && <TableRow><TableCell colSpan={3}><ErrorState copy={copy} onRetry={() => void categories.refetch()} /></TableCell></TableRow>}
            {products.isPending ? <TableRow><TableCell colSpan={3}><ListSkeleton /></TableCell></TableRow> : products.isError ? <TableRow><TableCell colSpan={3}><ErrorState copy={copy} onRetry={() => void products.refetch()} /></TableCell></TableRow> : products.data.products.length === 0 ? (
              <TableRow><TableCell colSpan={3} className="whitespace-normal"><EmptyState icon={<BookOpen className="size-8" />} text={`${copy.productEmpty} · ${copy.productEmptyHint}`} /></TableCell></TableRow>
            ) : products.data.products.map(product => (
              <TableRow key={product.id}>
                <TableCell className="py-3 pl-4 sm:pl-6">
                  {product.imageUrl ? <img src={product.imageUrl} alt={product.name} className="size-12 rounded-md border bg-white object-contain p-1" loading="lazy" width={48} height={48} /> : <div className="flex size-12 items-center justify-center rounded-md bg-muted" role="img" aria-label={copy.productNoImage}><BookOpen className="size-5 text-muted-foreground" /></div>}
                </TableCell>
                <TableCell className="whitespace-normal">
                  <Button variant="link" className="h-auto max-w-full justify-start p-0 text-left whitespace-normal text-foreground" onClick={() => setSelected(product.id)}><span className="line-clamp-2 break-words [overflow-wrap:anywhere]">{product.name}</span></Button>
                  <p className="mt-1 truncate text-xs text-muted-foreground">{[product.brand, product.model, product.categoryName].filter(Boolean).join(" · ")}</p>
                </TableCell>
                <TableCell className="pr-4 text-right sm:pr-6">
                  <div className="flex justify-end gap-1"><Button size="icon-sm" variant="ghost" aria-label={`${copy.productEdit} ${product.name}`} onClick={() => setSelected(product.id)}><Pencil /></Button><Button size="icon-sm" variant="ghost" aria-label={`${copy.productDelete} ${product.name}`} onClick={() => setRemoving(product)}><Trash2 /></Button></div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </ScrollArea>
      {products.data && <footer className="flex shrink-0 items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground sm:px-6">
        <span>{copy.productTotal}: {products.data.total}</span>
        <Pagination className="mx-0 w-auto" aria-label={copy.products}>
          <PaginationContent>
            <PaginationItem><PaginationPrevious href="#" text={copy.productPrevious} aria-label={copy.productPrevious} aria-disabled={offset === 0 || products.isFetching} tabIndex={offset === 0 || products.isFetching ? -1 : undefined} className="aria-disabled:pointer-events-none aria-disabled:opacity-50" onClick={event => { event.preventDefault(); if (offset > 0 && !products.isFetching) setOffset(Math.max(0, offset - 12)); }} /></PaginationItem>
            <PaginationItem><PaginationLink isActive href="#" onClick={event => event.preventDefault()}>{Math.floor(offset / 12) + 1}</PaginationLink></PaginationItem>
            <PaginationItem><PaginationNext href="#" text={copy.productNext} aria-label={copy.productNext} aria-disabled={!products.data.hasMore || products.isFetching} tabIndex={!products.data.hasMore || products.isFetching ? -1 : undefined} className="aria-disabled:pointer-events-none aria-disabled:opacity-50" onClick={event => { event.preventDefault(); if (products.data.hasMore && !products.isFetching) setOffset(products.data.nextOffset); }} /></PaginationItem>
          </PaginationContent>
        </Pagination>
      </footer>}
      </section>
      </div>
      <Dialog open={browseOpen} onOpenChange={setBrowseOpen}><DialogContent className="flex max-h-[85dvh] flex-col"><DialogHeader><DialogTitle>{copy.productBrowse}</DialogTitle><DialogDescription>{copy.productBrowseHint}</DialogDescription></DialogHeader><ScrollArea className="min-h-0 flex-1">{navigationContent}</ScrollArea></DialogContent></Dialog>
      <Dialog open={Boolean(selected)} onOpenChange={(open, details) => {
        if (open) return;
        if (editorDirty.current) { details.cancel(); setDiscardOpen(true); }
        else closeEditor();
      }}>
        <DialogContent className="flex h-[92dvh] flex-col overflow-hidden sm:max-w-6xl">
          <DialogHeader className="shrink-0 pr-8"><DialogTitle>{selected === "new" ? copy.productNew : copy.productEdit}</DialogTitle><DialogDescription>{copy.productEditorHint}</DialogDescription></DialogHeader>
          {selected === "new" || detail.data ? <ProductEditor key={`${selected}-${detail.data?.product.version || "new"}`} copy={copy} product={selected === "new" ? undefined : detail.data?.product} categories={categoryItems} defaultBrand={selection.brand || ""} defaultCategory={selection.categoryId} onDirtyChange={value => { editorDirty.current = value; }} onSaved={() => { closeEditor(); refresh(); }} /> : detail.isError ? <ErrorState copy={copy} onRetry={() => void detail.refetch()} /> : <ListSkeleton />}
          <ProductConfirmation open={discardOpen} onOpenChange={setDiscardOpen} title={copy.productDiscardChanges} description={copy.productDiscardConfirm} cancelLabel={copy.cancel} actionLabel={copy.productDiscardChanges} onConfirm={closeEditor} />
        </DialogContent>
      </Dialog>
      <ProductPriceImports copy={copy} open={importsOpen} onOpenChange={setImportsOpen} />
      <ProductDimensionsManager copy={copy} open={dimensionsOpen} onOpenChange={setDimensionsOpen} onChanged={refresh} />
      <CategoryManager copy={copy} open={categoriesOpen} onClose={() => setCategoriesOpen(false)} categories={categoryItems} onChanged={refresh} />
      <ProductConfirmation open={Boolean(removing)} onOpenChange={open => { if (!open) setRemoving(null); }} title={copy.productDelete} description={`${copy.productDeleteConfirm} ${removing?.name || ""}`} cancelLabel={copy.cancel} actionLabel={copy.productDelete} pending={pending} onConfirm={() => void remove()} />
    </div>
  );
}

function ProductCategorySelect({ copy, categories, value, onValueChange, filter = false }: { copy: Copy; categories: ProductCategory[]; value: string; onValueChange: (value: string) => void; filter?: boolean }) {
  const emptyValue = "__uncategorized__";
  const items = [{ value: emptyValue, label: filter ? copy.productAllCategories : copy.productUncategorized }, ...productCategoryRows(categories).map(item => ({ value: item.id, label: filter ? `${item.path} (${item.productCount})` : item.path }))];
  return <Select items={items} value={value || emptyValue} onValueChange={next => { if (next !== null) onValueChange(next === emptyValue ? "" : next); }}>
    <SelectTrigger id={filter ? undefined : "product-category"} aria-label={filter ? copy.productCategory : undefined} className={filter ? "w-full min-w-0 sm:w-52" : "w-full min-w-0"}><SelectValue /></SelectTrigger>
    <SelectContent>{items.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
  </Select>;
}

function ProductConfirmation({ open, onOpenChange, title, description, cancelLabel, actionLabel, pending = false, onConfirm }: { open: boolean; onOpenChange: (open: boolean) => void; title: string; description: string; cancelLabel: string; actionLabel: string; pending?: boolean; onConfirm: () => void }) {
  return <AlertDialog open={open} onOpenChange={(value, details) => { if (pending) details.cancel(); else onOpenChange(value); }}>
    <AlertDialogContent>
      <AlertDialogHeader><AlertDialogTitle>{title}</AlertDialogTitle><AlertDialogDescription>{description}</AlertDialogDescription></AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel disabled={pending}>{cancelLabel}</AlertDialogCancel>
        <AlertDialogAction variant="destructive" disabled={pending} onClick={onConfirm}>{pending && <LoaderCircle className="animate-spin" />}{actionLabel}</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}

function ProductEditor({ copy, product, categories, defaultBrand, defaultCategory, onSaved, onDirtyChange }: { copy: Copy; product?: Product; categories: ProductCategory[]; defaultBrand: string; defaultCategory: string; onSaved: () => void; onDirtyChange: (dirty: boolean) => void }) {
  const [input, setInput] = useState<ProductInput>(() => ({ name: product?.name || "", brand: product?.brand ?? defaultBrand, model: product?.model || "", categoryId: product?.categoryId ?? defaultCategory, specification: product?.specification || "", version: product?.version || 0, catalog: structuredClone(product?.catalog || emptyCatalog()) }));
  const [pending, setPending] = useState(false);
  const [imagePending, setImagePending] = useState(false);
  useEffect(() => {
    onDirtyChange(input.name !== (product?.name || "") || input.brand !== (product?.brand ?? defaultBrand) || input.model !== (product?.model || "") || input.categoryId !== (product?.categoryId ?? defaultCategory) || input.specification !== (product?.specification || "") || input.imageDataUrl !== undefined || JSON.stringify(input.catalog) !== JSON.stringify(product?.catalog || emptyCatalog()));
    return () => onDirtyChange(false);
  }, [input, product, defaultBrand, defaultCategory, onDirtyChange]);
  const fileInput = useRef<HTMLInputElement>(null);
  const imageSource = input.imageDataUrl === undefined ? product?.imageUrl : input.imageDataUrl;
  async function submit(event: FormEvent) {
    event.preventDefault(); setPending(true);
    try { await saveProduct(input, product?.id); toast.success(copy.productSaved); onSaved(); }
    catch (error) { toastError(error, copy.productFailed); }
    finally { setPending(false); }
  }
  async function upload(file?: File) {
    if (!file) return;
    setImagePending(true);
    try { const imageDataUrl = await prepareProductImage(file); setInput(current => ({ ...current, imageDataUrl })); }
    catch (error) { toastError(error, copy.productImageFailed); }
    finally { setImagePending(false); if (fileInput.current) fileInput.current.value = ""; }
  }
  return (
    <form onSubmit={event => void submit(event)} className="flex min-h-0 flex-1 flex-col gap-4">
      <Tabs defaultValue="details" className="flex min-h-0 flex-1 flex-col">
      <TabsList className="shrink-0" aria-label={copy.products}><TabsTrigger value="details">{copy.productDetails}</TabsTrigger><TabsTrigger value="pricing">{copy.productPricing}</TabsTrigger></TabsList>
      <ScrollArea className="min-h-0 flex-1"><div className="space-y-4 px-1 pb-1">
      <TabsContent value="details" className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5"><Label htmlFor="product-name">{copy.productName}</Label><Input id="product-name" value={input.name} required maxLength={200} onChange={event => setInput({ ...input, name: event.target.value })} /></div>
          <div className="space-y-1.5"><Label htmlFor="product-model">{copy.productModel}</Label><Input id="product-model" value={input.model} maxLength={160} onChange={event => setInput({ ...input, model: event.target.value })} /></div>
          <div className="space-y-1.5"><Label htmlFor="product-brand">{copy.productBrand}</Label><Input id="product-brand" value={input.brand} maxLength={100} onChange={event => setInput({ ...input, brand: event.target.value })} /></div>
          <div className="space-y-1.5"><Label htmlFor="product-category">{copy.productCategory}</Label><ProductCategorySelect copy={copy} categories={categories} value={input.categoryId} onValueChange={value => setInput({ ...input, categoryId: value })} /></div>
        </div>
        <div className="flex flex-wrap items-center gap-3 rounded-lg border p-3">
          {imageSource && <img src={imageSource} alt={input.name || copy.productImage} className="size-24 rounded-md border bg-white object-contain p-1" />}
          <div className="space-y-2"><Label>{copy.productImage}</Label><p className="text-xs text-muted-foreground">{copy.productImageHint}</p><div className="flex gap-2"><Button type="button" variant="outline" size="sm" disabled={imagePending || pending} onClick={() => fileInput.current?.click()}>{imagePending ? <LoaderCircle className="animate-spin" /> : <ImagePlus />}{copy.productUploadImage}</Button>{imageSource && <Button type="button" variant="ghost" size="sm" disabled={pending} onClick={() => setInput({ ...input, imageDataUrl: "" })}>{copy.productRemoveImage}</Button>}</div></div>
          <Input ref={fileInput} type="file" accept="image/png,image/jpeg,image/webp" className="hidden" aria-label={copy.productUploadImage} onChange={event => void upload(event.target.files?.[0])} />
        </div>
        <div className="space-y-2"><Label htmlFor="product-specification">{copy.productSpecification}</Label>
          <Tabs defaultValue="edit">
            <TabsList aria-label={copy.productSpecification}><TabsTrigger value="edit">{copy.productEdit}</TabsTrigger><TabsTrigger value="preview">{copy.productPreview}</TabsTrigger></TabsList>
            <TabsContent value="edit"><Textarea id="product-specification" className="min-h-64 font-mono text-sm" value={input.specification} maxLength={100000} placeholder={copy.productSpecificationPlaceholder} onChange={event => setInput({ ...input, specification: event.target.value })} /></TabsContent>
            <TabsContent value="preview" className="min-h-64 rounded-md border p-3"><MarkdownContent content={input.specification || copy.productNoSpecification} /></TabsContent>
          </Tabs>
          <div className="flex justify-between gap-3 text-xs text-muted-foreground"><span>{copy.productSpecificationHint}</span><span>{Array.from(input.specification).length.toLocaleString()} / 100,000</span></div>
        </div>
      </TabsContent>
      <TabsContent value="pricing" className="min-w-0"><ProductCatalogEditor copy={copy} catalog={input.catalog || emptyCatalog()} disabled={pending} onChange={catalog => setInput(current => ({ ...current, catalog }))} /></TabsContent>
        {product && <p className="text-xs text-muted-foreground">v{product.version} · {new Date(product.updatedAt).toLocaleString()}</p>}
      </div></ScrollArea>
      </Tabs>
      <DialogFooter className="shrink-0"><Button type="submit" disabled={pending || imagePending || !input.name.trim()}>{pending && <LoaderCircle className="animate-spin" />}{copy.save}</Button></DialogFooter>
    </form>
  );
}

function CategoryManager({ copy, open, onClose, categories, onChanged }: { copy: Copy; open: boolean; onClose: () => void; categories: ProductCategory[]; onChanged: () => void }) {
  const [name, setName] = useState("");
  const [editing, setEditing] = useState<string | undefined>();
  const [parentId, setParentId] = useState("");
  const [pending, setPending] = useState(false);
  const [removing, setRemoving] = useState<ProductCategory | null>(null);
  async function save(event: FormEvent) {
    event.preventDefault(); setPending(true);
    try { await saveProductCategory(name, editing, parentId); setName(""); setParentId(""); setEditing(undefined); onChanged(); }
    catch (error) { toastError(error, copy.productFailed); }
    finally { setPending(false); }
  }
  async function remove() {
    if (!removing) return;
    setPending(true);
    try { await deleteProductCategory(removing.id); if (editing === removing.id) { setEditing(undefined); setName(""); setParentId(""); } setRemoving(null); onChanged(); }
    catch (error) { toastError(error, copy.productFailed); }
    finally { setPending(false); }
  }
  const descendants = editing ? new Set([editing, ...productCategoryRows(categories, editing).map(item => item.id)]) : new Set<string>();
  return <Dialog open={open} onOpenChange={value => { if (!value) onClose(); }}><DialogContent className="sm:max-w-lg"><DialogHeader><DialogTitle>{copy.productCategories}</DialogTitle><DialogDescription>{copy.productCategoryHint}</DialogDescription></DialogHeader>
    <form className="space-y-3" onSubmit={event => void save(event)}>
      <Input aria-label={copy.productCategoryName} value={name} maxLength={100} required placeholder={copy.productCategoryName} onChange={event => setName(event.target.value)} />
      <ProductSelect label={copy.productParentCategory} value={parentId} onValueChange={setParentId} disabled={pending} options={[{ value: "", label: copy.productTopCategory }, ...productCategoryRows(categories).filter(item => !descendants.has(item.id)).map(item => ({ value: item.id, label: item.path }))]} />
      <div className="flex gap-2"><Button type="submit" disabled={pending || !name.trim()}>{editing ? copy.save : copy.productAddCategory}</Button>{editing && <Button type="button" variant="ghost" onClick={() => { setEditing(undefined); setName(""); setParentId(""); }}>{copy.cancel}</Button>}</div>
    </form>
    <ScrollArea className="min-h-0" style={{ height: Math.min(categories.length * 53, 288) }}>
      <Table aria-label={copy.productCategories}><TableBody>{productCategoryRows(categories).map(item => <TableRow key={item.id}><TableCell className="max-w-0 truncate" title={item.path}><span style={{ paddingLeft: item.depth * 16 }}>{item.depth > 0 && "↳ "}{item.name}</span> <span className="text-muted-foreground">({item.productCount})</span></TableCell><TableCell className="w-20 text-right"><Button size="icon" variant="ghost" disabled={pending} aria-label={`${copy.productEdit} ${item.name}`} onClick={() => { setEditing(item.id); setName(item.name); setParentId(item.parentId); }}><Pencil /></Button><Button size="icon" variant="ghost" disabled={pending || categories.some(child => child.parentId === item.id)} title={categories.some(child => child.parentId === item.id) ? copy.productCategoryHasChildren : undefined} aria-label={`${copy.productDelete} ${item.name}`} onClick={() => setRemoving(item)}><Trash2 /></Button></TableCell></TableRow>)}</TableBody></Table>
    </ScrollArea>
    <ProductConfirmation open={Boolean(removing)} onOpenChange={value => { if (!value) setRemoving(null); }} title={copy.productDelete} description={`${copy.productCategoryDeleteConfirm} ${removing?.name || ""}`} cancelLabel={copy.cancel} actionLabel={copy.productDelete} pending={pending} onConfirm={() => void remove()} />
  </DialogContent></Dialog>;
}
