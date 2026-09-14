import { useEffect, useRef, useState } from "react";
import {
  FilePenLine,
  FileSpreadsheet,
  FileText,
  Menu,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Separator } from "../ui/separator";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import { DocumentStampManager } from "./document-stamps";
import { DocumentEditorDialog } from "./document-editor-dialog";
import { useShell } from "./shell-context";
import {
  type StoredDocument,
  type StoredTemplate,
  type DocumentEditorTarget,
  type DocumentListFilter,
  DOCUMENT_PAGE_SIZE,
  readStoredDocuments,
  writeStoredDocuments,
  readStoredTemplates,
  writeStoredTemplates,
  setStoredTemplateDeleted,
} from "../../lib/document-storage";
import { cn, paginationPageItems } from "../../lib/utils";
import { zh, en } from "../../lib/locale";

export function DocumentListPage({ createDocument = false, documentId }: { createDocument?: boolean; documentId?: string }) {
  const { copy, accountEmail, openMobileMenu } = useShell();
  const [documents, setDocuments] = useState(() => readStoredDocuments().sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)));
  const [templates, setTemplates] = useState(() => readStoredTemplates(copy));
  const [selectedDocumentIds, setSelectedDocumentIds] = useState<Set<string>>(() => new Set());
  const [documentPage, setDocumentPage] = useState(1);
  const [documentFilter, setDocumentFilter] = useState<DocumentListFilter>("all");
  const [templatePopoverOpen, setTemplatePopoverOpen] = useState(false);
  const [editorTarget, setEditorTarget] = useState<DocumentEditorTarget | null>(() => {
    const existing = documentId ? readStoredDocuments().find((document) => document.id === documentId) : undefined;
    if (existing) return { kind: "document", record: existing };
    if (createDocument) {
      const type = new URLSearchParams(window.location.search).get("type") === "contract" ? "contract" : "quotation";
      return { kind: "document", initialTemplate: readStoredTemplates(copy).find((template) => template.type === type) };
    }
    return null;
  });
  const selectAllDocumentsRef = useRef<HTMLInputElement>(null);
  const documentTableScrollRef = useRef<HTMLDivElement>(null);
  const filteredDocuments = documentFilter === "all" ? documents : documents.filter((document) => document.type === documentFilter);
  const documentPageCount = Math.max(1, Math.ceil(filteredDocuments.length / DOCUMENT_PAGE_SIZE));
  const currentDocumentPage = Math.min(documentPage, documentPageCount);
  const pageDocuments = filteredDocuments.slice((currentDocumentPage - 1) * DOCUMENT_PAGE_SIZE, currentDocumentPage * DOCUMENT_PAGE_SIZE);
  const allDocumentsSelected = pageDocuments.length > 0 && pageDocuments.every((document) => selectedDocumentIds.has(document.id));
  const someDocumentsSelected = pageDocuments.some((document) => selectedDocumentIds.has(document.id)) && !allDocumentsSelected;

  useEffect(() => {
    if (selectAllDocumentsRef.current) selectAllDocumentsRef.current.indeterminate = someDocumentsSelected;
  }, [someDocumentsSelected]);

  useEffect(() => {
    if (documentPage > documentPageCount) setDocumentPage(documentPageCount);
  }, [documentPage, documentPageCount]);

  const closeEditor = () => {
    setEditorTarget(null);
    if (window.location.pathname !== "/documents") window.history.replaceState(window.history.state, "", "/documents");
  };
  const deleteTemplate = (template: StoredTemplate) => {
    if (!window.confirm(`${copy.deleteTemplateConfirm}\n\n${template.name}`)) return;
    const next = templates.filter((item) => item.id !== template.id);
    try {
      writeStoredTemplates(next);
      setStoredTemplateDeleted(template.id, true);
    } catch {
      toast.error(copy.loadFailed);
      return;
    }
    setTemplates(next);
    if (editorTarget?.kind === "template" && editorTarget.record?.id === template.id) closeEditor();
    toast.success(copy.templateDeleted);
  };
  const deleteDocuments = (ids: Set<string>) => {
    const targets = documents.filter((document) => ids.has(document.id));
    if (!targets.length) return;
    const confirmed = window.confirm(targets.length === 1 ? `${copy.deleteDocumentConfirm}\n\n${targets[0].name}` : `${copy.deleteDocumentsConfirm}\n\n${targets.length} ${copy.selectedDocuments}`);
    if (!confirmed) return;
    const next = documents.filter((document) => !ids.has(document.id));
    try {
      writeStoredDocuments(next);
    } catch {
      toast.error(copy.loadFailed);
      return;
    }
    setDocuments(next);
    setSelectedDocumentIds((current) => new Set([...current].filter((id) => !ids.has(id))));
    if (editorTarget?.kind === "document" && editorTarget.record && ids.has(editorTarget.record.id)) closeEditor();
    toast.success(targets.length === 1 ? copy.documentDeleted : copy.documentsDeleted);
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
  const saveEditorRecord = (kind: DocumentEditorTarget["kind"], record: StoredDocument) => {
    if (kind === "template") {
      const next = [record, ...templates.filter((template) => template.id !== record.id)];
      setStoredTemplateDeleted(record.id, false);
      writeStoredTemplates(next);
      setTemplates(next);
      setEditorTarget({ kind, record });
      toast.success(copy.templateSaved);
      return;
    }
    const next = [record, ...documents.filter((document) => document.id !== record.id)].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
    writeStoredDocuments(next);
    setDocuments(next);
    setEditorTarget({ kind, record });
    window.history.replaceState(window.history.state, "", `/documents/${encodeURIComponent(record.id)}`);
    toast.success(copy.documentSaved);
  };

  return (
    <>
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex min-h-14 items-center gap-3 border-b bg-card px-3 py-2 sm:px-5">
          <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={openMobileMenu} aria-label={copy.folders} title={copy.folders}><Menu /></Button>
          <FilePenLine className="size-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-semibold">{copy.documents}</h1>
          <DocumentStampManager chinese={copy === zh} />
          <Button size="sm" onClick={() => setEditorTarget({ kind: "document", initialTemplate: templates[0] })}><Plus />{copy.newDocument}</Button>
          <Popover open={templatePopoverOpen} onOpenChange={setTemplatePopoverOpen}>
            <PopoverTrigger render={<Button variant="outline" size="sm" />}><FileSpreadsheet />{copy.templateManagement}</PopoverTrigger>
            <PopoverContent side="bottom" align="start" sideOffset={8} className="w-80 gap-2 p-2">
              <PopoverTitle className="px-2 py-1 text-sm font-semibold">{copy.templateManagement}</PopoverTitle>
              <div className="grid gap-1">{templates.map((template) => <div key={template.id} className="flex min-w-0 items-center gap-1"><Button type="button" variant="ghost" className="h-auto min-w-0 flex-1 justify-start px-2 py-2 text-left" onClick={() => { setTemplatePopoverOpen(false); setEditorTarget({ kind: "template", record: template }); }}><FileText className="size-4" /><span className="min-w-0 flex-1"><strong className="block truncate text-sm font-medium">{template.name}</strong><small className="block text-muted-foreground">{template.type === "quotation" ? copy.quotation : copy.contract}</small></span><Pencil className="size-3.5" /></Button><Button type="button" variant="ghost" size="icon" className="size-8 shrink-0 text-destructive hover:text-destructive" onClick={() => deleteTemplate(template)} aria-label={`${copy.deleteTemplate}: ${template.name}`} title={copy.deleteTemplate}><Trash2 /></Button></div>)}</div>
              <Separator />
              <Button type="button" variant="ghost" className="w-full justify-start" onClick={() => { setTemplatePopoverOpen(false); setEditorTarget({ kind: "template", initialTemplate: templates[0] }); }}><Plus />{copy.newTemplate}</Button>
            </PopoverContent>
          </Popover>
          {Boolean(selectedDocumentIds.size) && <Button variant="destructive" size="sm" onClick={() => deleteDocuments(selectedDocumentIds)}><Trash2 />{copy.deleteSelectedDocuments} ({selectedDocumentIds.size})</Button>}
          <div className="ml-auto flex items-center gap-2">
            <Select value={documentFilter} onValueChange={(value) => changeDocumentFilter(value as DocumentListFilter)}>
              <SelectTrigger className="h-8 w-28 text-xs sm:w-32"><SelectValue>{documentFilter === "all" ? copy.allDocumentTypes : documentFilter === "quotation" ? copy.quotation : copy.contract}</SelectValue></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{copy.allDocumentTypes}</SelectItem>
                <SelectItem value="quotation">{copy.quotation}</SelectItem>
                <SelectItem value="contract">{copy.contract}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </header>
        <div className="flex min-h-11 shrink-0 flex-wrap items-center justify-between gap-2 border-b px-4 py-1.5 sm:px-5">
          <div className="inline-flex h-8 items-center rounded-md border bg-muted/30 p-0.5" role="group" aria-label={copy.documentType}>
            {(["all", "quotation", "contract"] as const).map((filter) => <Button key={filter} type="button" variant={documentFilter === filter ? "secondary" : "ghost"} size="sm" className="h-7 px-3 text-xs shadow-none" aria-pressed={documentFilter === filter} onClick={() => changeDocumentFilter(filter)}>{filter === "all" ? copy.allDocumentTypes : filter === "quotation" ? copy.quotation : copy.contract}</Button>)}
          </div>
          <div className="flex items-center gap-3"><span className="text-xs text-muted-foreground">{selectedDocumentIds.size} {copy.selectedDocuments}</span><Button type="button" variant="destructive" size="sm" disabled={!selectedDocumentIds.size} onClick={() => deleteDocuments(selectedDocumentIds)}><Trash2 />{copy.deleteSelectedDocuments}</Button></div>
        </div>
        <div ref={documentTableScrollRef} className="min-h-0 flex-1 overflow-auto [&_[data-slot=table-container]]:overflow-visible">
          <Table className="min-w-[640px] table-fixed">
            <TableHeader className="sticky top-0 z-10 bg-background"><TableRow className="hover:bg-transparent">
              <TableHead className="w-12 px-4"><input ref={selectAllDocumentsRef} type="checkbox" className="size-4 accent-primary" checked={allDocumentsSelected} disabled={!pageDocuments.length} onChange={(event) => togglePageDocuments(event.target.checked)} aria-label={copy.deleteSelectedDocuments} /></TableHead>
              <TableHead className="px-2">{copy.documentName}</TableHead><TableHead className="w-[18%]">{copy.documentType}</TableHead><TableHead className="w-[26%] text-right">{copy.documentUpdatedAt}</TableHead><TableHead className="w-20 px-4 text-right">{copy.actions}</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {pageDocuments.map((document) => <TableRow key={document.id} className="cursor-pointer" onClick={() => setEditorTarget({ kind: "document", record: document })}>
                <TableCell className="px-4 py-3" onClick={(event) => event.stopPropagation()}><input type="checkbox" className="size-4 accent-primary" checked={selectedDocumentIds.has(document.id)} onChange={(event) => toggleDocument(document.id, event.target.checked)} aria-label={`${copy.selectedDocuments}: ${document.name}`} /></TableCell>
                <TableCell className="px-2 py-3"><span className="block truncate font-medium">{document.name}</span></TableCell>
                <TableCell><Badge variant="secondary">{document.type === "quotation" ? copy.quotation : copy.contract}</Badge></TableCell>
                <TableCell className="text-right text-sm text-muted-foreground">{new Date(document.updatedAt).toLocaleString(copy === en ? "en" : "zh-CN")}</TableCell>
                <TableCell className="px-4 text-right"><Button type="button" variant="ghost" size="icon" className="size-8 text-destructive hover:text-destructive" onClick={(event) => { event.stopPropagation(); deleteDocuments(new Set([document.id])); }} aria-label={`${copy.deleteDocument}: ${document.name}`} title={copy.deleteDocument}><Trash2 /></Button></TableCell>
              </TableRow>)}
              {!filteredDocuments.length && <TableRow><TableCell colSpan={5} className="h-40 text-center text-muted-foreground"><div className="grid justify-items-center gap-2"><FilePenLine className="size-6" /><span>{copy.noDocuments}</span></div></TableCell></TableRow>}
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
      </main>
      {editorTarget && <DocumentEditorDialog key={`${editorTarget.kind}-${editorTarget.record?.id || "new"}`} copy={copy} accountEmail={accountEmail} target={editorTarget} templates={templates} onOpenChange={(open) => { if (!open) closeEditor(); }} onSave={saveEditorRecord} />}
    </>
  );
}
