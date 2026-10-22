import { useEffect, useRef, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { BookOpen, FolderTree, ImagePlus, LoaderCircle, Menu, Pencil, Plus, Search, Trash2 } from "lucide-react";
import { deleteProduct, deleteProductCategory, getProduct, listProductCategories, prepareProductImage, saveProduct, saveProductCategory, searchProducts, type Product, type ProductCategory, type ProductInput } from "../../lib/products";
import { type Copy } from "../../lib/locale";
import { toast, toastError } from "../../lib/toast";
import { Button } from "../ui/button";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "../ui/alert-dialog";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "../ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "../ui/input-group";
import { Label } from "../ui/label";
import { Pagination, PaginationContent, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious } from "../ui/pagination";
import { ScrollArea } from "../ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Table, TableBody, TableCell, TableRow } from "../ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../ui/tabs";
import { Textarea } from "../ui/textarea";
import { EmptyState, ErrorState, ListSkeleton } from "./common-states";
import { MarkdownContent } from "./markdown-content";
import { useShell } from "./shell-context";

export function ProductsPage() {
  const { copy, openMobileMenu } = useShell();
  const client = useQueryClient();
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("");
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<string | null>(() => new URLSearchParams(window.location.search).get("product"));
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
  useEffect(() => {
    if (category && categories.data && !categories.data.categories.some(item => item.id === category)) {
      setCategory("");
      setOffset(0);
    }
  }, [category, categories.data]);
  const products = useQuery({ queryKey: ["products", search, category, offset], queryFn: () => searchProducts(search, category, offset) });
  const detail = useQuery({ queryKey: ["product", selected], queryFn: () => getProduct(selected!), enabled: Boolean(selected && selected !== "new"), staleTime: 0, gcTime: 0, refetchOnWindowFocus: false, refetchOnReconnect: false });
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ["products"] });
    void client.invalidateQueries({ queryKey: ["product-categories"] });
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
  return (
    <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col" data-testid="products-page">
      <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3 sm:px-6">
        <Button variant="ghost" size="icon" className="lg:hidden" onClick={openMobileMenu} aria-label={copy.folders}><Menu /></Button>
        <div className="min-w-0 flex-1"><h1 className="text-lg font-semibold">{copy.products}</h1><p className="text-xs text-muted-foreground">{copy.productsHint}</p></div>
        <Button variant="outline" aria-label={copy.productCategories} onClick={() => setCategoriesOpen(true)}><FolderTree /><span className="hidden sm:inline">{copy.productCategories}</span></Button>
        <Button onClick={() => setSelected("new")}><Plus />{copy.productNew}</Button>
      </header>
      <div className="flex flex-wrap gap-3 border-b px-4 py-3 sm:px-6">
        <InputGroup className="min-w-40 flex-1">
          <InputGroupInput aria-label={copy.productSearch} placeholder={copy.productSearch} value={query} onChange={event => setQuery(event.target.value)} maxLength={200} />
          <InputGroupAddon><Search /></InputGroupAddon>
        </InputGroup>
        <ProductCategorySelect copy={copy} categories={categoryItems} value={category} onValueChange={value => { setCategory(value); setOffset(0); }} filter />
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div className="p-4 sm:p-6">
        {categories.isError && <ErrorState copy={copy} onRetry={() => void categories.refetch()} />}
        {products.isPending ? <ListSkeleton /> : products.isError ? <ErrorState copy={copy} onRetry={() => void products.refetch()} /> : products.data.products.length === 0 ? (
          <EmptyState icon={<BookOpen className="size-8" />} text={`${copy.productEmpty} · ${copy.productEmptyHint}`} />
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {products.data.products.map(product => (
              <Card key={product.id} className="min-w-0">
                <CardHeader className="flex items-start gap-3">
                  {product.imageUrl ? <img src={product.imageUrl} alt={product.name} className="size-20 shrink-0 rounded-md border bg-white object-contain p-1" loading="lazy" /> : <div className="flex size-20 shrink-0 items-center justify-center rounded-md bg-muted"><BookOpen className="size-7 text-muted-foreground" /></div>}
                  <div className="min-w-0 flex-1"><CardTitle><h2><Button variant="link" className="h-auto max-w-full justify-start p-0 text-left whitespace-normal text-foreground" onClick={() => setSelected(product.id)}><span className="line-clamp-2 break-words">{product.name}</span></Button></h2></CardTitle><p className="mt-1 break-all text-sm">{product.model || "—"}</p><p className="mt-1 truncate text-xs text-muted-foreground">{[product.brand, product.categoryName].filter(Boolean).join(" · ")}</p></div>
                </CardHeader>
                <CardContent><p className="line-clamp-3 min-h-12 whitespace-pre-wrap break-words text-xs leading-relaxed text-muted-foreground">{product.snippet || copy.productNoSpecification}</p></CardContent>
                <CardFooter className="mt-auto gap-2 py-2"><span className="flex-1 text-xs text-muted-foreground">v{product.version} · {new Date(product.updatedAt).toLocaleDateString()}</span><Button size="icon" variant="ghost" aria-label={`${copy.productEdit} ${product.name}`} onClick={() => setSelected(product.id)}><Pencil /></Button><Button size="icon" variant="ghost" aria-label={`${copy.productDelete} ${product.name}`} onClick={() => setRemoving(product)}><Trash2 /></Button></CardFooter>
              </Card>
            ))}
          </div>
        )}
        </div>
      </ScrollArea>
      {products.data && <footer className="flex items-center justify-between gap-3 border-t px-4 py-3 text-xs text-muted-foreground sm:px-6">
        <span>{copy.productTotal}: {products.data.total}</span>
        <Pagination className="mx-0 w-auto" aria-label={copy.products}>
          <PaginationContent>
            <PaginationItem><PaginationPrevious href="#" text={copy.productPrevious} aria-label={copy.productPrevious} aria-disabled={offset === 0 || products.isFetching} tabIndex={offset === 0 || products.isFetching ? -1 : undefined} className="aria-disabled:pointer-events-none aria-disabled:opacity-50" onClick={event => { event.preventDefault(); if (offset > 0 && !products.isFetching) setOffset(Math.max(0, offset - 12)); }} /></PaginationItem>
            <PaginationItem><PaginationLink isActive href="#" onClick={event => event.preventDefault()}>{Math.floor(offset / 12) + 1}</PaginationLink></PaginationItem>
            <PaginationItem><PaginationNext href="#" text={copy.productNext} aria-label={copy.productNext} aria-disabled={!products.data.hasMore || products.isFetching} tabIndex={!products.data.hasMore || products.isFetching ? -1 : undefined} className="aria-disabled:pointer-events-none aria-disabled:opacity-50" onClick={event => { event.preventDefault(); if (products.data.hasMore && !products.isFetching) setOffset(products.data.nextOffset); }} /></PaginationItem>
          </PaginationContent>
        </Pagination>
      </footer>}
      <Dialog open={Boolean(selected)} onOpenChange={(open, details) => {
        if (open) return;
        if (editorDirty.current) { details.cancel(); setDiscardOpen(true); }
        else closeEditor();
      }}>
        <DialogContent className="flex max-h-[92dvh] flex-col sm:max-w-3xl">
          <DialogHeader><DialogTitle>{selected === "new" ? copy.productNew : copy.productEdit}</DialogTitle><DialogDescription>{copy.productEditorHint}</DialogDescription></DialogHeader>
          {selected === "new" || detail.data ? <ProductEditor key={`${selected}-${detail.data?.product.version || "new"}`} copy={copy} product={selected === "new" ? undefined : detail.data?.product} categories={categoryItems} onDirtyChange={value => { editorDirty.current = value; }} onSaved={() => { closeEditor(); refresh(); }} /> : detail.isError ? <ErrorState copy={copy} onRetry={() => void detail.refetch()} /> : <ListSkeleton />}
          <ProductConfirmation open={discardOpen} onOpenChange={setDiscardOpen} title={copy.productDiscardChanges} description={copy.productDiscardConfirm} cancelLabel={copy.cancel} actionLabel={copy.productDiscardChanges} onConfirm={closeEditor} />
        </DialogContent>
      </Dialog>
      <CategoryManager copy={copy} open={categoriesOpen} onClose={() => setCategoriesOpen(false)} categories={categoryItems} onChanged={refresh} />
      <ProductConfirmation open={Boolean(removing)} onOpenChange={open => { if (!open) setRemoving(null); }} title={copy.productDelete} description={`${copy.productDeleteConfirm} ${removing?.name || ""}`} cancelLabel={copy.cancel} actionLabel={copy.productDelete} pending={pending} onConfirm={() => void remove()} />
    </div>
  );
}

function ProductCategorySelect({ copy, categories, value, onValueChange, filter = false }: { copy: Copy; categories: ProductCategory[]; value: string; onValueChange: (value: string) => void; filter?: boolean }) {
  const emptyValue = "__uncategorized__";
  const items = [{ value: emptyValue, label: filter ? copy.productAllCategories : copy.productUncategorized }, ...categories.map(item => ({ value: item.id, label: filter ? `${item.name} (${item.productCount})` : item.name }))];
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

function ProductEditor({ copy, product, categories, onSaved, onDirtyChange }: { copy: Copy; product?: Product; categories: ProductCategory[]; onSaved: () => void; onDirtyChange: (dirty: boolean) => void }) {
  const [input, setInput] = useState<ProductInput>(() => ({ name: product?.name || "", brand: product?.brand || "", model: product?.model || "", categoryId: product?.categoryId || "", specification: product?.specification || "", version: product?.version || 0 }));
  const [pending, setPending] = useState(false);
  const [imagePending, setImagePending] = useState(false);
  useEffect(() => {
    onDirtyChange(input.name !== (product?.name || "") || input.brand !== (product?.brand || "") || input.model !== (product?.model || "") || input.categoryId !== (product?.categoryId || "") || input.specification !== (product?.specification || "") || input.imageDataUrl !== undefined);
    return () => onDirtyChange(false);
  }, [input, product, onDirtyChange]);
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
      <ScrollArea className="min-h-0 flex-1"><div className="space-y-4 px-1 pb-1">
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
        {product && <p className="text-xs text-muted-foreground">v{product.version} · {new Date(product.updatedAt).toLocaleString()}</p>}
      </div></ScrollArea>
      <DialogFooter><Button type="submit" disabled={pending || imagePending || !input.name.trim()}>{pending && <LoaderCircle className="animate-spin" />}{copy.save}</Button></DialogFooter>
    </form>
  );
}

function CategoryManager({ copy, open, onClose, categories, onChanged }: { copy: Copy; open: boolean; onClose: () => void; categories: ProductCategory[]; onChanged: () => void }) {
  const [name, setName] = useState("");
  const [editing, setEditing] = useState<string | undefined>();
  const [pending, setPending] = useState(false);
  const [removing, setRemoving] = useState<ProductCategory | null>(null);
  async function save(event: FormEvent) {
    event.preventDefault(); setPending(true);
    try { await saveProductCategory(name, editing); setName(""); setEditing(undefined); onChanged(); }
    catch (error) { toastError(error, copy.productFailed); }
    finally { setPending(false); }
  }
  async function remove() {
    if (!removing) return;
    setPending(true);
    try { await deleteProductCategory(removing.id); if (editing === removing.id) { setEditing(undefined); setName(""); } setRemoving(null); onChanged(); }
    catch (error) { toastError(error, copy.productFailed); }
    finally { setPending(false); }
  }
  return <Dialog open={open} onOpenChange={value => { if (!value) onClose(); }}><DialogContent><DialogHeader><DialogTitle>{copy.productCategories}</DialogTitle><DialogDescription>{copy.productCategoryHint}</DialogDescription></DialogHeader>
    <form className="flex gap-2" onSubmit={event => void save(event)}><Input aria-label={copy.productCategoryName} value={name} maxLength={100} required placeholder={copy.productCategoryName} onChange={event => setName(event.target.value)} /><Button type="submit" disabled={pending || !name.trim()}>{editing ? copy.save : copy.productAddCategory}</Button>{editing && <Button type="button" variant="ghost" onClick={() => { setEditing(undefined); setName(""); }}>{copy.cancel}</Button>}</form>
    <ScrollArea className="min-h-0" style={{ height: Math.min(categories.length * 53, 288) }}>
      <Table aria-label={copy.productCategories}><TableBody>{categories.map(item => <TableRow key={item.id}><TableCell className="max-w-0 truncate">{item.name} <span className="text-muted-foreground">({item.productCount})</span></TableCell><TableCell className="w-20 text-right"><Button size="icon" variant="ghost" disabled={pending} aria-label={`${copy.productEdit} ${item.name}`} onClick={() => { setEditing(item.id); setName(item.name); }}><Pencil /></Button><Button size="icon" variant="ghost" disabled={pending} aria-label={`${copy.productDelete} ${item.name}`} onClick={() => setRemoving(item)}><Trash2 /></Button></TableCell></TableRow>)}</TableBody></Table>
    </ScrollArea>
    <ProductConfirmation open={Boolean(removing)} onOpenChange={value => { if (!value) setRemoving(null); }} title={copy.productDelete} description={`${copy.productCategoryDeleteConfirm} ${removing?.name || ""}`} cancelLabel={copy.cancel} actionLabel={copy.productDelete} pending={pending} onConfirm={() => void remove()} />
  </DialogContent></Dialog>;
}
