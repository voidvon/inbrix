import { Suspense, lazy, useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  Bold,
  Check,
  Download,
  FileText,
  Italic,
  Link,
  List,
  ListOrdered,
  Paperclip,
  Printer,
  Redo2,
  Sparkles,
  Table2,
  Underline,
  Undo2,
} from "lucide-react";
import { toast } from "sonner";
import type { CanvasDocumentEditorHandle } from "./canvas-document-editor";
import { DocumentStampManager } from "./document-stamps";
import { createDocumentPDF, exportDocumentPages } from "./document-export";
import { generateDocument } from "../../lib/api";
import { escapeHTML } from "../../lib/email-format";
import { type Copy, zh } from "../../lib/locale";
import {
  type StoredDocument,
  type StoredTemplate,
  type DocumentEditorTarget,
  type DocumentTemplate,
  documentTemplateHTML,
  readStoredDocuments,
  writeStoredDocuments,
  readStoredTemplates,
  writeStoredTemplates,
  setStoredTemplateDeleted,
} from "../../lib/document-storage";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "../ui/dropdown-menu";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from "../ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Separator } from "../ui/separator";
import { Textarea } from "../ui/textarea";

const CanvasDocumentEditor = lazy(() => import("./canvas-document-editor").then((module) => ({ default: module.CanvasDocumentEditor })));

export function DocumentEditorButtons({ copy, editor, disabled }: { copy: Copy; editor: CanvasDocumentEditorHandle | null; disabled: boolean }) {
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const insertAttachments = async (files: File[]) => {
    try {
      for (const file of files) await editor?.insertAttachment(file);
      if (files.length) toast.success(copy.documentAttachmentInserted);
    } catch (error) {
      toast.error(error instanceof Error && error.message.includes("3 MB") ? copy.documentAttachmentTooLarge : error instanceof Error ? error.message : copy.loadFailed);
    }
  };
  return (
    <>
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => editor?.undo()} aria-label={copy.undo} title={copy.undo}><Undo2 /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => editor?.redo()} aria-label={copy.redo} title={copy.redo}><Redo2 /></Button>
      <Separator orientation="vertical" className="mx-1 h-5" />
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => editor?.bold()} aria-label="Bold" title="Bold"><Bold /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => editor?.italic()} aria-label="Italic" title="Italic"><Italic /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => editor?.underline()} aria-label="Underline" title="Underline"><Underline /></Button>
      <Separator orientation="vertical" className="mx-1 h-5" />
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => editor?.bulletList()} aria-label="Bullet list" title="Bullet list"><List /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => editor?.orderedList()} aria-label="Numbered list" title="Numbered list"><ListOrdered /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => {
        const url = window.prompt("URL");
        if (!url) return;
        const label = window.prompt("Link text", url) || url;
        editor?.insertLink(label, url);
      }} aria-label="Link" title="Link"><Link /></Button>
      <input ref={attachmentInputRef} className="sr-only" type="file" multiple onChange={(event) => { const files = Array.from(event.target.files || []); event.target.value = ""; void insertAttachments(files); }} />
      <Button type="button" variant="ghost" size="icon" disabled={disabled} onClick={() => { editor?.focus(); attachmentInputRef.current?.click(); }} aria-label={copy.insertDocumentAttachment} title={copy.insertDocumentAttachment}><Paperclip /></Button>
      <DropdownMenu>
        <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon" disabled={disabled} aria-label={copy.insertTable} title={copy.insertTable} />}><Table2 /></DropdownMenuTrigger>
        <DropdownMenuContent align="start">{[2, 3, 4, 5].map((size) => <DropdownMenuItem key={size} onClick={() => editor?.insertTable(size, size)}>{size} x {size}</DropdownMenuItem>)}</DropdownMenuContent>
      </DropdownMenu>
    </>
  );
}

export function DocumentAIAssistant({ copy, editor, accountEmail, type, title, disabled }: { copy: Copy; editor: CanvasDocumentEditorHandle | null; accountEmail: string; type: DocumentTemplate; title: string; disabled: boolean }) {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<"generate" | "rewrite">("generate");
  const [instruction, setInstruction] = useState("");
  const [result, setResult] = useState("");
  const sourceHTMLRef = useRef("");
  const mutation = useMutation({
    mutationFn: () => {
      sourceHTMLRef.current = editor?.getHTML() || "";
      return generateDocument({ accountEmail, mode, documentType: type, title, instruction, currentHTML: mode === "rewrite" ? sourceHTMLRef.current : undefined });
    },
    onSuccess: (value) => setResult(value.html),
  });
  const sanitizeResult = (html: string) => {
    const parsed = new DOMParser().parseFromString(html, "text/html");
    parsed.body.querySelectorAll("script, style, iframe, object, embed").forEach((element) => element.remove());
    parsed.body.querySelectorAll<HTMLElement>("*").forEach((element) => Array.from(element.attributes).forEach((attribute) => { if (attribute.name.toLowerCase().startsWith("on")) element.removeAttribute(attribute.name); }));
    return parsed.body.innerHTML;
  };
  const applyResult = () => {
    if (!result) return;
    const nextHTML = sanitizeResult(result);
    if (mode === "rewrite" && sourceHTMLRef.current) {
      const before = new DOMParser().parseFromString(sourceHTMLRef.current, "text/html");
      const after = new DOMParser().parseFromString(nextHTML, "text/html");
      if (before.querySelectorAll("table").length !== after.querySelectorAll("table").length || before.querySelectorAll("h1, h2, h3").length !== after.querySelectorAll("h1, h2, h3").length) {
        toast.error(copy.aiStructureMismatch);
        return;
      }
    }
    editor?.setHTML(nextHTML);
    editor?.focus();
    setResult("");
    setOpen(false);
  };
  return (
    <Popover open={open} onOpenChange={(value) => { setOpen(value); if (!value) { setResult(""); mutation.reset(); } }}>
      <PopoverTrigger render={<Button type="button" variant={open ? "secondary" : "ghost"} size="sm" disabled={disabled || !accountEmail} />}><Sparkles />{copy.aiDocument}</PopoverTrigger>
      <PopoverContent side="bottom" align="end" sideOffset={8} className="w-[min(32rem,calc(100vw-2rem))] gap-0 p-4">
        <PopoverTitle className="text-sm font-semibold">{copy.aiDocument}</PopoverTitle>
        <PopoverDescription className="mt-1 text-xs">{mode === "generate" ? copy.aiGenerateDocument : copy.aiRewriteDocument}</PopoverDescription>
        <div className="mt-3 flex gap-1"><Button type="button" size="sm" variant={mode === "generate" ? "secondary" : "ghost"} onClick={() => setMode("generate")} disabled={mutation.isPending}>{copy.aiGenerateDocument}</Button><Button type="button" size="sm" variant={mode === "rewrite" ? "secondary" : "ghost"} onClick={() => setMode("rewrite")} disabled={mutation.isPending}>{copy.aiRewriteDocument}</Button></div>
        <Label className="mt-3 grid gap-1.5 text-xs"><span>{copy.aiDocumentInstruction}</span><Textarea value={instruction} onChange={(event) => setInstruction(event.target.value)} rows={4} disabled={mutation.isPending} placeholder={type === "quotation" ? copy.quotation : copy.contract} /></Label>
        {mutation.error && <p className="mt-2 text-xs text-destructive">{mutation.error instanceof Error ? mutation.error.message : copy.aiDocumentFailed}</p>}
        {result && <><p className="mt-3 text-xs text-amber-700 dark:text-amber-300">{copy.aiApplyHint}</p><div className="mt-2 max-h-48 overflow-auto rounded-md border bg-muted/30 p-3 text-sm" dangerouslySetInnerHTML={{ __html: sanitizeResult(result) }} /></>}
        <div className="mt-3 flex justify-end gap-2">{result && <Button type="button" variant="outline" size="sm" onClick={applyResult}><Check />{copy.aiApplyDocument}</Button>}<Button type="button" size="sm" disabled={mutation.isPending || !instruction.trim()} onClick={() => mutation.mutate()}><Sparkles />{mutation.isPending ? copy.aiGeneratingDocument : mode === "generate" ? copy.aiGenerateDocument : copy.aiRewriteDocument}</Button></div>
      </PopoverContent>
    </Popover>
  );
}

export function DocumentEditorDialog({ copy, accountEmail, target, templates, onOpenChange, onSave, onAttach, onBusyChange }: { copy: Copy; accountEmail: string; target: DocumentEditorTarget | null; templates: StoredTemplate[]; onOpenChange: (open: boolean) => void; onSave: (kind: DocumentEditorTarget["kind"], record: StoredDocument) => void; onAttach?: (file: File) => void; onBusyChange?: (busy: boolean) => void }) {
  const initialTemplate = target?.initialTemplate || templates.find((template) => template.type === target?.record?.type) || templates[0];
  const initialType = target?.record?.type || initialTemplate?.type || "quotation";
  const editorRef = useRef<CanvasDocumentEditorHandle>(null);
  const editorScrollRef = useRef<HTMLDivElement>(null);
  const [name, setName] = useState(target?.record?.name || (target?.kind === "template" ? copy.newTemplate : initialType === "quotation" ? copy.quotation : copy.contract));
  const [type, setType] = useState<DocumentTemplate>(initialType);
  const [selectedTemplateId, setSelectedTemplateId] = useState(initialTemplate?.id || "");
  const [editorReady, setEditorReady] = useState(false);
  const [exporting, setExporting] = useState(false);
  const initialHTML = target?.record?.html || initialTemplate?.html || documentTemplateHTML(initialType, copy);

  if (!target) return null;

  const selectTemplate = (templateId: string) => {
    const template = templates.find((item) => item.id === templateId);
    if (!template) return;
    setSelectedTemplateId(template.id);
    setType(template.type);
    if (!target.record) setName(template.type === "quotation" ? copy.quotation : copy.contract);
    editorRef.current?.setHTML(template.html);
    window.requestAnimationFrame(() => editorScrollRef.current?.scrollTo({ top: 0, left: 0 }));
  };
  const selectType = (next: DocumentTemplate) => {
    setType(next);
    if (!target.record) setName(next === "quotation" ? copy.quotationTemplate : copy.contractTemplate);
    editorRef.current?.setHTML(documentTemplateHTML(next, copy));
    window.requestAnimationFrame(() => editorScrollRef.current?.scrollTo({ top: 0, left: 0 }));
  };
  const downloadDocument = () => {
    const body = editorRef.current?.getHTML() || "";
    const title = escapeHTML(name || copy.newDocument);
    const html = `<!doctype html><html><head><meta charset="utf-8"><title>${title}</title></head><body>${body}</body></html>`;
    const url = URL.createObjectURL(new Blob([html], { type: "text/html;charset=utf-8" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${(name || copy.newDocument).replace(/[\\/:*?"<>|]/g, "-")}.html`;
    anchor.click();
    URL.revokeObjectURL(url);
  };
  const exportDocument = async (format: "pdf" | "png") => {
    if (!editorRef.current || exporting) return;
    setExporting(true);
    try {
      await exportDocumentPages(await editorRef.current.getPageImages(), name || copy.newDocument, format);
      toast.success(copy === zh ? "文档已导出" : "Document exported");
    } catch (error) {
      console.error("Document export failed", error);
      toast.error(copy === zh ? "导出失败，请重试或使用打印功能另存为 PDF。" : "Export failed. Try again or use Print to save as PDF.");
    } finally { setExporting(false); }
  };
  const save = () => {
    const html = editorRef.current?.getDocument();
    if (!html) return;
    try {
      onSave(target.kind, {
        id: target.record?.id || crypto.randomUUID(),
        type,
        name: name.trim() || (target.kind === "template" ? copy.newTemplate : copy.newDocument),
        html,
        updatedAt: new Date().toISOString(),
      });
    } catch {
      toast.error(copy === zh ? "保存失败，请检查浏览器存储空间。可先导出 PDF 或图片保留副本。" : "Save failed. Check browser storage space. Export a PDF or image to keep a copy.");
    }
  };
  const attachPDF = async () => {
    if (!editorRef.current || exporting || !onAttach) return;
    setExporting(true);
    onBusyChange?.(true);
    try {
      const file = await createDocumentPDF(await editorRef.current.getPageImages(), name || copy.newDocument);
      onAttach(file);
      toast.success(copy === zh ? "PDF 已添加到邮件附件" : "PDF added to email attachments");
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : copy.loadFailed);
    } finally { setExporting(false); onBusyChange?.(false); }
  };

  return (
    <Dialog open onOpenChange={(value) => { if (!exporting) onOpenChange(value); }}>
      <DialogContent data-testid="document-editor-dialog" data-editor-kind={target.kind} className="flex h-[92vh] w-[94vw] max-w-[1400px] flex-col gap-0 overflow-hidden p-0 sm:max-w-[1400px]">
        <DialogHeader className="shrink-0 border-b px-4 py-3 pr-12 text-left"><DialogTitle className="text-base">{target.record ? (target.kind === "template" ? copy.editTemplate : copy.editDocument) : (target.kind === "template" ? copy.newTemplate : copy.newDocument)}</DialogTitle><DialogDescription className="sr-only">{copy.documentEditor}</DialogDescription></DialogHeader>
        <div className="flex flex-wrap items-center gap-2 border-b bg-muted/40 px-3 py-2 sm:px-4">
          {target.kind === "document" ? <Select value={selectedTemplateId} onValueChange={(value) => value && selectTemplate(value)}><SelectTrigger className="h-8 w-52" aria-label={copy.useTemplate}><SelectValue>{templates.find((template) => template.id === selectedTemplateId)?.name || copy.useTemplate}</SelectValue></SelectTrigger><SelectContent>{templates.map((template) => <SelectItem key={template.id} value={template.id}>{template.name}</SelectItem>)}</SelectContent></Select> : <div className="flex rounded-md bg-muted p-0.5" role="group" aria-label={copy.documentType}><Button type="button" variant={type === "quotation" ? "secondary" : "ghost"} size="sm" onClick={() => selectType("quotation")}>{copy.quotation}</Button><Button type="button" variant={type === "contract" ? "secondary" : "ghost"} size="sm" onClick={() => selectType("contract")}>{copy.contract}</Button></div>}
          <Input className="h-8 min-w-40 flex-1 sm:max-w-72" value={name} onChange={(event) => setName(event.target.value)} placeholder={target.kind === "template" ? copy.templateName : copy.documentName} aria-label={target.kind === "template" ? copy.templateName : copy.documentName} />
          <div className="flex items-center gap-1 overflow-x-auto"><DocumentEditorButtons copy={copy} editor={editorRef.current} disabled={!editorReady} /><Separator orientation="vertical" className="mx-1 h-5" /><DocumentAIAssistant copy={copy} editor={editorRef.current} accountEmail={accountEmail} type={type} title={name} disabled={!editorReady} /></div>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <DocumentStampManager chinese={copy === zh} disabled={!editorReady || exporting} onInsert={(stamp, width) => editorRef.current?.insertStamp(stamp, width)} />
            <Button type="button" variant="outline" size="sm" disabled={!editorReady || exporting} onClick={() => void editorRef.current?.print().catch(() => toast.error(copy.loadFailed))}><Printer />{copy.printDocument}</Button>
            <DropdownMenu>
              <DropdownMenuTrigger render={<Button type="button" variant="outline" size="sm" disabled={!editorReady || exporting} />}><Download />{exporting ? (copy === zh ? "导出中…" : "Exporting…") : (copy === zh ? "导出" : "Export")}</DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => void exportDocument("pdf")}>{copy === zh ? "下载 PDF" : "Download PDF"}</DropdownMenuItem>
                <DropdownMenuItem onClick={() => void exportDocument("png")}>{copy === zh ? "下载 PNG 图片（多页 ZIP）" : "Download PNG images (multi-page ZIP)"}</DropdownMenuItem>
                <DropdownMenuItem onClick={downloadDocument}>{copy.downloadHTML}</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button type="button" size="sm" disabled={!editorReady || exporting} onClick={save}><Check />{copy.saveDocument}</Button>
            {onAttach && <Button type="button" size="sm" disabled={!editorReady || exporting} onClick={() => void attachPDF()}><Paperclip />{exporting ? (copy === zh ? "正在生成 PDF…" : "Generating PDF…") : (copy === zh ? "作为 PDF 添加到邮件" : "Attach PDF to email")}</Button>}
          </div>
        </div>
        <div ref={editorScrollRef} className="canvas-document-scroll min-h-0 flex-1 overflow-auto bg-muted/30 p-3 sm:p-6">
          <Suspense fallback={<div className="grid h-64 place-items-center text-sm text-muted-foreground">{copy.loadingEditor}</div>}><CanvasDocumentEditor ref={editorRef} initialHTML={initialHTML} locale={copy === zh ? "zh-CN" : "en"} onReady={() => { setEditorReady(true); editorScrollRef.current?.scrollTo({ top: 0, left: 0 }); }} /></Suspense>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function ComposeDocumentAttachment({ copy, accountEmail, action, onClose, onAttach, onBusyChange }: {
  copy: Copy; accountEmail: string; action: "create" | "import"; onClose: () => void;
  onAttach: (file: File) => void; onBusyChange: (busy: boolean) => void;
}) {
  const [templates, setTemplates] = useState(() => readStoredTemplates(copy));
  const [documents] = useState(() => readStoredDocuments().sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)));
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<StoredDocument | null>(null);
  const [target, setTarget] = useState<DocumentEditorTarget>({ kind: "document", initialTemplate: templates.find((template) => template.type === "quotation") });
  const [busy, setBusy] = useState(false);
  const converterRef = useRef<CanvasDocumentEditorHandle>(null);
  const [converterReady, setConverterReady] = useState(false);
  const conversionRef = useRef(false);
  const callbacksRef = useRef({ onAttach, onClose, onBusyChange });
  callbacksRef.current = { onAttach, onClose, onBusyChange };

  useEffect(() => {
    if (!selected || !converterReady || !converterRef.current || conversionRef.current) return;
    conversionRef.current = true;
    let cancelled = false;
    const convert = async () => {
      try {
        const pages = await converterRef.current!.getPageImages();
        const file = await createDocumentPDF(pages, selected.name);
        if (cancelled) return;
        callbacksRef.current.onAttach(file);
        toast.success(copy === zh ? "PDF 已添加到邮件附件" : "PDF added to email attachments");
        callbacksRef.current.onClose();
      } catch (error) {
        if (!cancelled) toast.error(error instanceof Error ? error.message : copy.loadFailed);
      } finally {
        if (!cancelled) {
          setBusy(false); setSelected(null); setConverterReady(false); conversionRef.current = false;
          callbacksRef.current.onBusyChange(false);
        }
      }
    };
    void convert();
    return () => { cancelled = true; conversionRef.current = false; };
  }, [selected, converterReady, copy]);

  if (action === "create") return <DocumentEditorDialog copy={copy} accountEmail={accountEmail} target={target} templates={templates}
    onOpenChange={(open) => { if (!open) onClose(); }} onAttach={onAttach} onBusyChange={onBusyChange}
    onSave={(kind, record) => {
      if (kind === "template") {
        const next = [record, ...readStoredTemplates(copy).filter((item) => item.id !== record.id)];
        setStoredTemplateDeleted(record.id, false);
        writeStoredTemplates(next); setTemplates(next);
      } else writeStoredDocuments([record, ...readStoredDocuments().filter((item) => item.id !== record.id)]);
      setTarget({ kind, record });
      toast.success(copy.documentSaved);
    }} />;

  const filtered = documents.filter((document) => document.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose(); }}>
      <DialogContent data-testid="document-attachment-picker" className="sm:max-w-xl">
        <DialogHeader><DialogTitle>{copy === zh ? "引入附件" : "Import attachment"}</DialogTitle><DialogDescription>{copy === zh ? "选择已保存的报价单或合同，自动转换为 PDF 并加入当前邮件，保留排版与印章。" : "Select a saved quotation or contract to attach it as a PDF, preserving its layout and stamps."}</DialogDescription></DialogHeader>
        <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={copy === zh ? "搜索文档" : "Search documents"} aria-label={copy === zh ? "搜索文档" : "Search documents"} disabled={busy} />
        <div className="grid max-h-[50vh] gap-2 overflow-y-auto">
          {filtered.map((document) => <Button type="button" key={document.id} variant="outline" disabled={busy} className="h-auto justify-start py-3 text-left" onClick={() => { setSelected(document); setBusy(true); onBusyChange(true); }}>
            <FileText /><span className="min-w-0 flex-1"><span className="block truncate">{document.name}</span><span className="block text-xs font-normal text-muted-foreground">{document.type === "quotation" ? copy.quotation : copy.contract} · {new Date(document.updatedAt).toLocaleString(copy === zh ? "zh-CN" : "en")}</span></span><span className="shrink-0 text-xs">{copy === zh ? "引用为 PDF" : "Attach as PDF"}</span>
          </Button>)}
          {!filtered.length && <p className="py-6 text-center text-muted-foreground">{documents.length ? (copy === zh ? "没有匹配的文档" : "No matching documents") : copy.noDocuments}</p>}
        </div>
        {busy && <p role="status" className="text-sm text-muted-foreground">{copy === zh ? "正在转换 PDF，请稍候…" : "Converting to PDF…"}</p>}
        {selected && <div aria-hidden="true" className="pointer-events-none fixed top-0 left-[-10000px] w-[794px]">
          <Suspense fallback={null}><CanvasDocumentEditor key={selected.id} ref={converterRef} initialHTML={selected.html} locale={copy === zh ? "zh-CN" : "en"} onReady={() => setConverterReady(true)} /></Suspense>
        </div>}
      </DialogContent>
    </Dialog>
  );
}
