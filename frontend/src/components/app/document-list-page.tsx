import { AIDocumentChatPanel } from "./ai-document-chat-panel";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  FilePenLine,
  Sparkles,
  FileSpreadsheet,
  FileText,
  Loader2,
  Menu,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { toast } from "../../lib/toast";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "../ui/pagination";
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from "../ui/popover";
import { Separator } from "../ui/separator";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import { DocumentStampManager } from "./document-stamps";
import { DocumentEditorDialog } from "./document-editor-dialog";
import { useShell } from "./shell-context";
import {
  type StoredDocument,
  type DocumentSummary,
  summarizeDocument,
  type StoredTemplate,
  type DocumentEditorTarget,
  type DocumentListFilter,
  DOCUMENT_PAGE_SIZE,
  fetchServerDocuments,
  fetchServerTemplates,
  persistServerDocument,
  persistServerTemplate,
  deleteServerDocuments,
  deleteServerTemplate,
} from "../../lib/document-storage";
import { cn, paginationPageItems } from "../../lib/utils";
import { getDocument } from "../../lib/api";
import { zh, en } from "../../lib/locale";

export function DocumentListPage({ createDocument = false, documentId }: { createDocument?: boolean; documentId?: string }) {
  const { copy, accountEmail, openMobileMenu } = useShell();
  const [documents, setDocuments] = useState<DocumentSummary[]>([]);
  const [openingDocumentId, setOpeningDocumentId] = useState<string | null>(null);
  const detailRequest = useRef(0);
  const [loading, setLoading] = useState(true);
  const [showAIChat, setShowAIChat] = useState(() => window.location.pathname === "/ai-document");
  const [templates, setTemplates] = useState<StoredTemplate[]>([]);
  const [templatesLoading, setTemplatesLoading] = useState(false);
  const templateRequest = useRef(0);
  const [selectedDocumentIds, setSelectedDocumentIds] = useState<Set<string>>(() => new Set());
  const [documentPage, setDocumentPage] = useState(1);
  const [documentFilter, setDocumentFilter] = useState<DocumentListFilter>("all");
  const [templatePopoverOpen, setTemplatePopoverOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<
    | { kind: "documents"; records: DocumentSummary[] }
    | { kind: "template"; record: StoredTemplate }
    | null
  >(null);
  const [editorTarget, setEditorTarget] = useState<DocumentEditorTarget | null>(null);
  const selectAllDocumentsRef = useRef<HTMLInputElement>(null);
  const documentTableScrollRef = useRef<HTMLDivElement>(null);
  const filteredDocuments = documentFilter === "all" ? documents : documents.filter((document) => document.type === documentFilter);
  const documentPageCount = Math.max(1, Math.ceil(filteredDocuments.length / DOCUMENT_PAGE_SIZE));
  const currentDocumentPage = Math.min(documentPage, documentPageCount);
  const pageDocuments = filteredDocuments.slice((currentDocumentPage - 1) * DOCUMENT_PAGE_SIZE, currentDocumentPage * DOCUMENT_PAGE_SIZE);
  const allDocumentsSelected = pageDocuments.length > 0 && pageDocuments.every((document) => selectedDocumentIds.has(document.id));
  const someDocumentsSelected = pageDocuments.some((document) => selectedDocumentIds.has(document.id)) && !allDocumentsSelected;

  useEffect(() => {
    let mounted = true;
    setLoading(true);
    void fetchServerDocuments("all")
      .then((docs) => {
        if (mounted) setDocuments(docs.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)));
      })
      .catch((err) => {
        if (mounted) toast.error(err instanceof Error ? err.message : copy.loadFailed);
      })
      .finally(() => {
        if (mounted) setLoading(false);
      });
    return () => { mounted = false; };
  }, [copy]);

  const loadTemplates = useCallback(async () => {
    const request = ++templateRequest.current;
    setTemplates([]);
    setTemplatesLoading(true);
    try {
      const records = await fetchServerTemplates(copy);
      if (request === templateRequest.current) setTemplates(records);
      return records;
    } finally {
      if (request === templateRequest.current) setTemplatesLoading(false);
    }
  }, [copy]);
  const refreshTemplates = () => void loadTemplates().catch(error => toast.error(error instanceof Error ? error.message : copy.loadFailed));
  useEffect(() => {
    if (!createDocument) return;
    let active = true;
    void loadTemplates().then(records => {
      if (!active) return;
      const type = new URLSearchParams(window.location.search).get("type") === "contract" ? "contract" : "quotation";
      const template = records.find(record => record.type === type);
      if (!template) throw new Error(copy === zh ? "请先添加对应的文档模板" : "Add a template for this document type first");
      setEditorTarget({ kind: "document", initialTemplate: template });
    }).catch(error => { if (active) toast.error(error instanceof Error ? error.message : copy.loadFailed); });
    return () => { active = false; };
  }, [createDocument, loadTemplates, copy]);

  const showEditor = (target: DocumentEditorTarget) => {
    detailRequest.current++;
    setOpeningDocumentId(null);
    setEditorTarget(target);
  };
  const openDocument = useCallback(async (id: string) => {
    const request = ++detailRequest.current;
    setOpeningDocumentId(id);
    setEditorTarget(null);
    try {
      const [{ document }] = await Promise.all([getDocument(id), loadTemplates()]);
      if (request === detailRequest.current) setEditorTarget({ kind: "document", record: document });
    } catch (error) {
      if (request === detailRequest.current) toast.error(error instanceof Error ? error.message : copy.loadFailed);
    } finally {
      if (request === detailRequest.current) setOpeningDocumentId(null);
    }
  }, [copy.loadFailed, loadTemplates]);

  useEffect(() => {
    if (documentId) void openDocument(documentId);
    return () => { detailRequest.current++; };
  }, [documentId, openDocument]);

  useEffect(() => {
    if (selectAllDocumentsRef.current) selectAllDocumentsRef.current.indeterminate = someDocumentsSelected;
  }, [someDocumentsSelected]);

  useEffect(() => {
    if (documentPage > documentPageCount) setDocumentPage(documentPageCount);
  }, [documentPage, documentPageCount]);

  const closeEditor = () => {
    detailRequest.current++;
    setOpeningDocumentId(null);
    setEditorTarget(null);
    if (!showAIChat && window.location.pathname !== "/documents") window.history.replaceState(window.history.state, "", "/documents");
  };
  const confirmDelete = async () => {
    if (!deleteTarget) return;
    if (deleteTarget.kind === "template") {
      const { record } = deleteTarget;
      const next = templates.filter((item) => item.id !== record.id);
      try {
        await deleteServerTemplate(record.id);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : copy.loadFailed);
        setDeleteTarget(null);
        return;
      }
      setTemplates(next);
      if (editorTarget?.kind === "template" && editorTarget.record?.id === record.id) closeEditor();
      toast.success(copy.templateDeleted);
      setDeleteTarget(null);
      return;
    }

    const { records } = deleteTarget;
    const ids = records.map((document) => document.id);
    const idSet = new Set(ids);
    const next = documents.filter((document) => !idSet.has(document.id));
    try {
      await deleteServerDocuments(ids);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : copy.loadFailed);
      setDeleteTarget(null);
      return;
    }
    setDocuments(next);
    setSelectedDocumentIds((current) => new Set([...current].filter((id) => !idSet.has(id))));
    if (editorTarget?.kind === "document" && editorTarget.record && idSet.has(editorTarget.record.id)) closeEditor();
    toast.success(records.length === 1 ? copy.documentDeleted : copy.documentsDeleted);
    setDeleteTarget(null);
  };
  const toggleDocument = (id: string, selected: boolean) => {
    setSelectedDocumentIds((current) => {
      const next = new Set(current);
      if (selected) next.add(id);
      else next.delete(id);
      return next;
    });
  };
  const togglePageDocuments = (selected: boolean) => {
    setSelectedDocumentIds((current) => {
      const next = new Set(current);
      pageDocuments.forEach((document) => selected ? next.add(document.id) : next.delete(document.id));
      return next;
    });
  };
  const changeDocumentPage = (page: number) => {
    setDocumentPage(Math.min(documentPageCount, Math.max(1, page)));
    documentTableScrollRef.current?.scrollTo({ top: 0 });
  };
  const changeDocumentFilter = (filter: DocumentListFilter) => {
    setDocumentFilter(filter);
    setDocumentPage(1);
    setSelectedDocumentIds(new Set());
    documentTableScrollRef.current?.scrollTo({ top: 0 });
  };
  const saveEditorRecord = async (kind: DocumentEditorTarget["kind"], record: StoredDocument) => {
    if (kind === "template") {
      try {
        const saved = await persistServerTemplate(record);
        const next = [saved, ...templates.filter((template) => template.id !== saved.id)];
        setTemplates(next);
        setEditorTarget({ kind, record: saved });
        toast.success(copy.templateSaved);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : copy.loadFailed);
        throw err;
      }
      return;
    }
    try {
      const saved = await persistServerDocument(record);
      const next = [summarizeDocument(saved), ...documents.filter((document) => document.id !== saved.id)].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
      setDocuments(next);
      setEditorTarget({ kind, record: saved });
      window.history.replaceState(window.history.state, "", `/documents/${encodeURIComponent(saved.id)}`);
      toast.success(copy.documentSaved);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : copy.loadFailed);
      throw err;
    }
  };

  return (
    <>
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {showAIChat ? <AIDocumentChatPanel copy={copy} accountEmail={accountEmail} onBack={() => { setShowAIChat(false); void fetchServerDocuments("all").then(setDocuments).catch(error => toast.error(error instanceof Error ? error.message : copy.loadFailed)); }} onOpenDocument={record => { void loadTemplates().then(() => showEditor({ kind: "document", record })).catch(error => toast.error(error instanceof Error ? error.message : copy.loadFailed)); }} className="mobile-page-enter min-h-0 flex-1" /> : <>
        <header className="flex min-h-14 items-center gap-3 border-b bg-card px-3 py-2 sm:px-5">
          <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={openMobileMenu} aria-label={copy.folders} title={copy.folders}><Menu /></Button>
          <FilePenLine className="size-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-semibold">{copy.documents}</h1>
          {(loading || openingDocumentId) && <Loader2 className="size-3.5 animate-spin text-muted-foreground" />}
        </header>
        <div className="flex min-h-11 shrink-0 flex-wrap items-center gap-2 border-b px-4 py-1.5 sm:px-5">
          <div className="inline-flex h-8 items-center rounded-md border bg-muted/30 p-0.5" role="group" aria-label={copy.documentType}>
            {(["all", "quotation", "contract"] as const).map((filter) => <Button key={filter} type="button" variant={documentFilter === filter ? "secondary" : "ghost"} size="sm" className="h-7 px-3 text-xs shadow-none" aria-pressed={documentFilter === filter} onClick={() => changeDocumentFilter(filter)}>{filter === "all" ? copy.allDocumentTypes : filter === "quotation" ? copy.quotation : copy.contract}</Button>)}
          </div>
          <Button size="sm" variant="outline" onClick={() => setShowAIChat(true)}><Sparkles />{copy === zh ? "AI 起草" : "Draft with AI"}</Button>
          <DropdownMenu onOpenChange={open => { if (open) refreshTemplates(); }}>
            <DropdownMenuTrigger render={<Button size="sm" />}>
              <Plus />{copy.newDocument}
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" side="bottom" sideOffset={6} className="w-56">
              {templatesLoading && <DropdownMenuItem disabled>{copy.loading}</DropdownMenuItem>}
              {templates.map((template) => (
                <DropdownMenuItem
                  key={template.id}
                  className="flex cursor-pointer items-center gap-2 px-2.5 py-2"
                  onClick={() => showEditor({ kind: "document", initialTemplate: template })}
                >
                  <FileText className="size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{template.name}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {template.type === "quotation" ? copy.quotation : copy.contract}
                  </span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <DocumentStampManager chinese={copy === zh} />
          <Popover open={templatePopoverOpen} onOpenChange={open => { setTemplatePopoverOpen(open); if (open) refreshTemplates(); }}>
            <PopoverTrigger render={<Button variant="outline" size="sm" />}><FileSpreadsheet />{copy.templateManagement}</PopoverTrigger>
            <PopoverContent side="bottom" align="start" sideOffset={8} className="w-80 gap-2 p-2">
              <PopoverTitle className="px-2 py-1 text-sm font-semibold">{copy.templateManagement}</PopoverTitle>
              {templatesLoading && <p className="p-2 text-sm text-muted-foreground">{copy.loading}</p>}
              <div className="grid gap-1">{templates.map((template) => <div key={template.id} className="flex min-w-0 items-center gap-1"><Button type="button" variant="ghost" className="h-auto min-w-0 flex-1 justify-start px-2 py-2 text-left" onClick={() => { setTemplatePopoverOpen(false); showEditor({ kind: "template", record: template }); }}><FileText className="size-4" /><span className="min-w-0 flex-1"><strong className="block truncate text-sm font-medium">{template.name}</strong><small className="block text-muted-foreground">{template.type === "quotation" ? copy.quotation : copy.contract}</small></span><Pencil className="size-3.5" /></Button><Button type="button" variant="ghost" size="icon" className="size-8 shrink-0 text-destructive hover:text-destructive" onClick={() => setDeleteTarget({ kind: "template", record: template })} aria-label={`${copy.deleteTemplate}: ${template.name}`} title={copy.deleteTemplate}><Trash2 /></Button></div>)}</div>
              <Separator />
              <Button type="button" variant="ghost" className="w-full justify-start" onClick={() => { setTemplatePopoverOpen(false); showEditor({ kind: "template", initialTemplate: templates[0] }); }}><Plus />{copy.newTemplate}</Button>
            </PopoverContent>
          </Popover>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={!selectedDocumentIds.size}
              onClick={() => {
                const targets = documents.filter((document) => selectedDocumentIds.has(document.id));
                if (targets.length) setDeleteTarget({ kind: "documents", records: targets });
              }}
            >
              <Trash2 />{copy.deleteSelectedDocuments}
            </Button>
            {Boolean(selectedDocumentIds.size) && <span className="text-xs text-muted-foreground">{selectedDocumentIds.size} {copy.selectedDocuments}</span>}
          </div>
        </div>
        <div ref={documentTableScrollRef} className="min-h-0 flex-1 overflow-auto touch-pan-x touch-pan-y [&_[data-slot=table-container]]:overflow-visible">
          <Table className="min-w-[760px] table-fixed">
            <TableHeader className="sticky top-0 z-10 bg-background"><TableRow className="hover:bg-transparent">
              <TableHead className="w-12 px-4"><input ref={selectAllDocumentsRef} type="checkbox" className="size-4 accent-primary" checked={allDocumentsSelected} disabled={!pageDocuments.length || loading} onChange={(event) => togglePageDocuments(event.target.checked)} aria-label={copy.deleteSelectedDocuments} /></TableHead>
              <TableHead className="px-2">{copy.documentName}</TableHead>
              <TableHead className="w-[24%]">{copy.documentCounterparty}</TableHead>
              <TableHead className="w-[14%]">{copy.documentType}</TableHead>
              <TableHead className="w-[22%] text-right">{copy.documentUpdatedAt}</TableHead>
              <TableHead className="w-20 px-4 text-right">{copy.actions}</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {loading && !filteredDocuments.length ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-48 text-center text-muted-foreground">
                    <div className="flex flex-col items-center justify-center gap-2.5">
                      <Loader2 className="size-6 animate-spin text-primary" />
                      <span className="text-sm">{copy.loading}</span>
                    </div>
                  </TableCell>
                </TableRow>
              ) : !filteredDocuments.length ? (
                <TableRow><TableCell colSpan={6} className="h-40 text-center text-muted-foreground"><div className="grid justify-items-center gap-2"><FilePenLine className="size-6" /><span>{copy.noDocuments}</span></div></TableCell></TableRow>
              ) : (
                pageDocuments.map((document) => <TableRow key={document.id} className="cursor-pointer" aria-busy={openingDocumentId === document.id} onClick={() => void openDocument(document.id)}>
                  <TableCell className="px-4 py-3" onClick={(event) => event.stopPropagation()}><input type="checkbox" className="size-4 accent-primary" checked={selectedDocumentIds.has(document.id)} onChange={(event) => toggleDocument(document.id, event.target.checked)} aria-label={`${copy.selectedDocuments}: ${document.name}`} /></TableCell>
                  <TableCell className="px-2 py-3"><span className="block truncate font-medium">{document.name}</span></TableCell>
                  <TableCell className="px-2 py-3"><span className="block truncate text-muted-foreground">{document.company || "—"}</span></TableCell>
                  <TableCell><Badge variant="secondary">{document.type === "quotation" ? copy.quotation : copy.contract}</Badge></TableCell>
                  <TableCell className="text-right text-sm text-muted-foreground">{new Date(document.updatedAt).toLocaleString(copy === en ? "en" : "zh-CN")}</TableCell>
                  <TableCell className="px-4 text-right"><Button type="button" variant="ghost" size="icon" className="size-8 text-destructive hover:text-destructive" onClick={(event) => { event.stopPropagation(); setDeleteTarget({ kind: "documents", records: [document] }); }} aria-label={`${copy.deleteDocument}: ${document.name}`} title={copy.deleteDocument}><Trash2 /></Button></TableCell>
                </TableRow>)
              )}
            </TableBody>
          </Table>
        </div>
        {documentPageCount > 1 && <div className="shrink-0 border-t bg-background px-3 py-2 sm:px-5">
          <Pagination>
            <PaginationContent>
              <PaginationItem><PaginationPrevious href="#" text={copy.previousPage} aria-label={copy.previousPage} aria-disabled={currentDocumentPage === 1} tabIndex={currentDocumentPage === 1 ? -1 : undefined} className={cn(currentDocumentPage === 1 && "pointer-events-none opacity-50")} onClick={(event) => { event.preventDefault(); if (currentDocumentPage > 1) changeDocumentPage(currentDocumentPage - 1); }} /></PaginationItem>
              {paginationPageItems(currentDocumentPage, documentPageCount).map((item) => typeof item === "number"
                ? <PaginationItem key={item}><PaginationLink href="#" isActive={item === currentDocumentPage} onClick={(event) => { event.preventDefault(); changeDocumentPage(item); }}>{item}</PaginationLink></PaginationItem>
                : <PaginationItem key={item}><PaginationEllipsis /></PaginationItem>)}
              <PaginationItem><PaginationNext href="#" text={copy.nextPage} aria-label={copy.nextPage} aria-disabled={currentDocumentPage === documentPageCount} tabIndex={currentDocumentPage === documentPageCount ? -1 : undefined} className={cn(currentDocumentPage === documentPageCount && "pointer-events-none opacity-50")} onClick={(event) => { event.preventDefault(); if (currentDocumentPage < documentPageCount) changeDocumentPage(currentDocumentPage + 1); }} /></PaginationItem>
            </PaginationContent>
          </Pagination>
        </div>}
        </>}
      </main>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {deleteTarget?.kind === "template"
                ? copy.deleteTemplate
                : (deleteTarget?.records.length ?? 0) > 1
                  ? copy.deleteSelectedDocuments
                  : copy.deleteDocument}
            </DialogTitle>
            <DialogDescription>
              {deleteTarget?.kind === "template"
                ? copy.deleteTemplateConfirm
                : (deleteTarget?.records.length ?? 0) > 1
                  ? copy.deleteDocumentsConfirm
                  : copy.deleteDocumentConfirm}
            </DialogDescription>
          </DialogHeader>
          {deleteTarget && (
            <div className="rounded-md border bg-muted/40 px-3 py-2 text-sm">
              {deleteTarget.kind === "template" ? (
                <span className="block truncate font-medium">{deleteTarget.record.name}</span>
              ) : deleteTarget.records.length === 1 ? (
                <span className="block truncate font-medium">{deleteTarget.records[0].name}</span>
              ) : (
                <span className="text-muted-foreground">
                  {deleteTarget.records.length} {copy.selectedDocuments}
                </span>
              )}
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setDeleteTarget(null)}>
              {copy.cancel}
            </Button>
            <Button type="button" variant="destructive" onClick={() => void confirmDelete()}>
              <Trash2 />
              {deleteTarget?.kind === "template"
                ? copy.deleteTemplate
                : (deleteTarget?.records.length ?? 0) > 1
                  ? copy.deleteSelectedDocuments
                  : copy.deleteDocument}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {editorTarget && <DocumentEditorDialog key={`${editorTarget.kind}-${editorTarget.record?.id || editorTarget.initialTemplate?.id || "new"}`} copy={copy} accountEmail={accountEmail} target={editorTarget} templates={templates} onOpenChange={(open) => { if (!open) closeEditor(); }} onSave={saveEditorRecord} />}
    </>
  );
}
