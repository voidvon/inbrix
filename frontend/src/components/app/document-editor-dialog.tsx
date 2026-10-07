import { Suspense, lazy, useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bold,
  Check,
  Download,
  Eye,
  FileText,
  Italic,
  Link,
  List,
  ListOrdered,
  Paperclip,
  Printer,
  Redo2,
  SlidersHorizontal,
  Sparkles,
  Table2,
  Underline,
  Undo2,
  Upload,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { toast } from "sonner";
import type { CanvasDocumentEditorHandle } from "./canvas-document-editor";
import { DocumentVariableForm } from "./document-variable-form";
import { DocumentStampManager } from "./document-stamps";
import { createDocumentPDF, exportDocumentPages } from "./document-export";
import { generateDocument } from "../../lib/api";
import { createDocumentNumber, extractDocumentCompany, extractDocumentNumber, numberDocumentTemplate } from "../../lib/document-number";
import { type Copy, zh } from "../../lib/locale";
import { cn, formatTime } from "../../lib/utils";
import {
  type StoredDocument,
  type StoredTemplate,
  type DocumentEditorTarget,
  type DocumentTemplate,
  documentTemplateHTML,
  isSellerConcept,
  readStoredDocuments,
  readStoredTemplates,
  persistServerDocument,
  persistServerTemplate,
} from "../../lib/document-storage";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "../ui/dropdown-menu";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from "../ui/popover";
import { Separator } from "../ui/separator";
import { Textarea } from "../ui/textarea";

const CanvasDocumentEditor = lazy(() => import("./canvas-document-editor").then((module) => ({ default: module.CanvasDocumentEditor })));

// 与 canvas-document-editor.tsx 中 applyDocumentUpdates 的别名归一化保持一致：
// AI 有时返回旧别名 key（如 customer_company），实际控件是 buyer_company，归一化后仍可应用。
const DOCUMENT_VARIABLE_ALIASES: Array<[string, string]> = [
  ["customer_company", "buyer_company"],
  ["customer_contact", "buyer_contact"],
  ["customer_email", "buyer_email"],
  ["customer_address", "buyer_address"],
  ["quote_number", "contract_number"],
  ["issue_date", "contract_date"],
];

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

function sanitizeResult(html: string) {
  let clean = html.trim();
  const match = clean.match(/```(?:html)?\s*([\s\S]*?)```/i);
  if (match) {
    clean = match[1].trim();
  }
  const parsed = new DOMParser().parseFromString(clean, "text/html");
  parsed.body.querySelectorAll("script, style, iframe, object, embed").forEach((element) => element.remove());
  parsed.body.querySelectorAll<HTMLElement>("*").forEach((element) => {
    Array.from(element.attributes).forEach((attribute) => {
      if (attribute.name.toLowerCase().startsWith("on")) element.removeAttribute(attribute.name);
    });
  });
  return parsed.body.innerHTML;
}

export function DocumentAISidebar({
  copy,
  editor,
  accountEmail,
  type,
  title,
  onClose,
  isMobile = false,
  onPreviewDocument,
}: {
  copy: Copy;
  editor: CanvasDocumentEditorHandle | null;
  accountEmail: string;
  type: DocumentTemplate;
  title: string;
  onClose?: () => void;
  isMobile?: boolean;
  onPreviewDocument?: () => void;
}) {
  const isZh = copy === zh;
  const [messages, setMessages] = useState<
    Array<{ id: string; role: "user" | "assistant"; content: string; time: string; error?: boolean; hasUpdate?: boolean }>
  >(() => [
    {
      id: "initial-msg",
      role: "assistant",
      content: isZh
        ? "我是您的 AI 文档助手。请输入您对当前文档的修改要求（如更改客户名称、调整产品单价、交货期、补充特别条款等），我将直接为您精准更新文档变量并保留排版。"
        : "I am your AI Document Assistant. Tell me what to update (customer name, prices, delivery dates, special clauses), and I will update the document variables while keeping formatting.",
      time: new Date().toISOString(),
    },
  ]);
  const [instruction, setInstruction] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    window.requestAnimationFrame(() => {
      if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    });
  }, [messages.length]);

  const mutation = useMutation({
    mutationFn: async (text: string) => {
      const rawHTML = editor?.getHTML() || "";
      const currentHTML = rawHTML
        .replace(/data:[^;]+;base64,[a-zA-Z0-9/+=]+/g, "[image]")
        .replace(/<svg[^>]*>[\s\S]*?<\/svg>/gi, "[svg-graphic]");
      const activeControls = editor?.getControls() || [];
      if (activeControls.length > 0) {
        return generateDocument({
          accountEmail,
          mode: "variables",
          documentType: type,
          title,
          instruction: text,
          currentHTML,
          variables: activeControls.map((c) => {
            const isStart = messages.length <= 1;
            const isSeller = isSellerConcept(c.conceptId);
            const val = isStart && !isSeller ? "" : (c.value || "");
            return {
              conceptId: c.conceptId,
              label: c.placeholder || c.conceptId,
              currentValue: val,
            };
          }),
        });
      }
      return generateDocument({ accountEmail, mode: "rewrite", documentType: type, title, instruction: text, currentHTML });
    },
    onSuccess: (value) => {
      if (value.mode === "variables" && (value.values || value.items)) {
        const activeControls = editor?.getControls() || [];
        const knownIds = new Set(activeControls.map((c) => c.conceptId));
        for (const [a, b] of DOCUMENT_VARIABLE_ALIASES) {
          if (knownIds.has(a) || knownIds.has(b)) {
            knownIds.add(a);
            knownIds.add(b);
          }
        }
        const values: Record<string, string> = {};
        for (const [key, val] of Object.entries(value.values || {})) {
          if (knownIds.has(key)) values[key] = val;
        }
        const hasValues = Object.keys(values).length > 0;
        const hasItems = value.items && value.items.length > 0;
        if (!hasValues && !hasItems) {
          setMessages((prev) => [
            ...prev,
            {
              id: `asst-${Date.now()}`,
              role: "assistant",
              content: isZh ? "AI 未发现需要变更的变量。请提供更明确的修改指示。" : "No variable updates detected.",
              time: new Date().toISOString(),
            },
          ]);
          return;
        }
        const applied = editor?.applyDocumentUpdates({
          values,
          items: value.items,
        });
        editor?.focus();
        const itemCount = value.items?.length || 0;
        const varCount = Object.keys(values).length;
        const replyText = isZh
          ? `已成功为您应用修改：\n${itemCount > 0 ? `• 更新了 ${itemCount} 项产品明细\n` : ""}• 更新了 ${varCount} 处文档变量`
          : `Applied updates: ${itemCount} items, ${varCount} variables`;
        setMessages((prev) => [
          ...prev,
          {
            id: `asst-${Date.now()}`,
            role: "assistant",
            content: replyText,
            time: new Date().toISOString(),
            hasUpdate: true,
          },
        ]);
        toast.success(isZh ? "文档已智能更新" : "Document updated");
        return;
      }
      if (value.html) {
        const nextHTML = sanitizeResult(value.html);
        if (!nextHTML) {
          toast.error(copy.aiDocumentFailed);
          return;
        }
        editor?.setHTML(nextHTML);
        editor?.focus();
        setMessages((prev) => [
          ...prev,
          {
            id: `asst-${Date.now()}`,
            role: "assistant",
            content: isZh ? "已根据要求完成文档改写并应用至编辑器。" : "Document rewritten and applied.",
            time: new Date().toISOString(),
            hasUpdate: true,
          },
        ]);
        toast.success(copy.aiDocumentRewritten);
        return;
      }
      toast.error(copy.aiDocumentFailed);
    },
    onError: (error) => {
      const errMsg = error instanceof Error ? error.message : copy.aiDocumentFailed;
      setMessages((prev) => [
        ...prev,
        {
          id: `asst-err-${Date.now()}`,
          role: "assistant",
          content: isZh ? `生成失败：${errMsg}` : `Failed: ${errMsg}`,
          time: new Date().toISOString(),
          error: true,
        },
      ]);
      toast.error(errMsg);
    },
  });

  const handleSend = () => {
    const trimmed = instruction.trim();
    if (!trimmed || mutation.isPending) return;
    setMessages((prev) => [
      ...prev,
      {
        id: `user-${Date.now()}`,
        role: "user",
        content: trimmed,
        time: new Date().toISOString(),
      },
    ]);
    setInstruction("");
    mutation.mutate(trimmed);
  };

  return (
    <aside
      className={cn(
        "flex flex-col bg-card",
        isMobile
          ? "flex-1 w-full h-full border-0"
          : "w-80 lg:w-96 border-l shrink-0 shadow-lg"
      )}
    >
      <div className="flex h-11 items-center justify-between border-b px-3.5 bg-muted/20 shrink-0">
        <div className="flex items-center gap-1.5">
          <Sparkles className="size-4 text-primary" />
          <span className="text-sm font-semibold">{copy.aiDocument}</span>
          {!isMobile && (
            <span className="text-[10px] text-muted-foreground bg-muted px-1.5 py-0.5 rounded font-normal">
              {isZh ? "常驻助手" : "Resident"}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          {isMobile && onPreviewDocument && (
            <Button
              type="button"
              size="sm"
              className="h-7 text-xs gap-1 shadow-2xs"
              onClick={onPreviewDocument}
            >
              <Eye className="size-3.5" />
              <span>{isZh ? "预览文档" : "Preview"}</span>
            </Button>
          )}
          {!isMobile && (
            <Button
              variant="ghost"
              size="icon"
              className="size-7"
              onClick={onClose}
              aria-label={copy.cancel}
              title={isZh ? "隐藏助手" : "Hide assistant"}
            >
              <X className="size-4" />
            </Button>
          )}
        </div>
      </div>
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-4 space-y-3">
        {messages.map((m) => (
          <div key={m.id} className={cn("flex flex-col text-xs", m.role === "user" ? "items-end" : "items-start")}>
            <div
              className={cn(
                "rounded-lg px-3 py-2 leading-relaxed max-w-[90%]",
                m.role === "user"
                  ? "bg-primary text-primary-foreground"
                  : m.error
                  ? "border border-destructive/30 bg-destructive/10 text-destructive"
                  : "border bg-muted/50 text-foreground"
              )}
            >
              <p className="whitespace-pre-wrap">{m.content}</p>
              {m.hasUpdate && onPreviewDocument && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  className="mt-2.5 h-7 gap-1.5 text-xs bg-background text-primary border-primary/30 hover:bg-primary/5 shadow-2xs"
                  onClick={onPreviewDocument}
                >
                  <Eye className="size-3.5" />
                  <span>{isZh ? "点击预览最新文档" : "Preview Document"}</span>
                </Button>
              )}
            </div>
            <span className="mt-1 text-[9px] text-muted-foreground">{formatTime(m.time)}</span>
          </div>
        ))}
        {mutation.isPending && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Sparkles className="size-3 animate-spin text-primary" />
            <span>{copy.aiDocumentDrafting}</span>
          </div>
        )}
      </div>
      <div className="border-t p-2.5 bg-background shrink-0">
        <div className="relative flex flex-col rounded-lg border bg-background focus-within:ring-1 focus-within:ring-primary shadow-2xs">
          <Textarea
            value={instruction}
            onChange={(e) => setInstruction(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            rows={2}
            placeholder={isZh ? "输入对当前文档的修改要求…" : "Type changes to document…"}
            className="min-h-12 max-h-28 overflow-y-auto resize-none border-0 bg-transparent px-2.5 py-1.5 text-xs leading-5 shadow-none focus-visible:ring-0"
            disabled={mutation.isPending}
          />
          <div className="flex items-center justify-between border-t px-2 py-1 bg-muted/10 shrink-0">
            <span className="text-[10px] text-muted-foreground">{isZh ? "Enter 发送" : "Enter to send"}</span>
            <Button
              type="button"
              size="sm"
              className="size-7 rounded p-0"
              disabled={mutation.isPending || !instruction.trim()}
              onClick={handleSend}
            >
              <Sparkles className={mutation.isPending ? "size-3.5 animate-spin" : "size-3.5"} />
            </Button>
          </div>
        </div>
      </div>
    </aside>
  );
}

export function DocumentEditorDialog({ copy, accountEmail, target, templates, onOpenChange, onSave, onAttach, onBusyChange }: { copy: Copy; accountEmail: string; target: DocumentEditorTarget | null; templates: StoredTemplate[]; onOpenChange: (open: boolean) => void; onSave: (kind: DocumentEditorTarget["kind"], record: StoredDocument) => void | Promise<void>; onAttach?: (file: File) => void; onBusyChange?: (busy: boolean) => void }) {
  const initialTemplate = target?.initialTemplate || templates.find((template) => template.type === target?.record?.type) || templates[0];
  const initialType = target?.record?.type || initialTemplate?.type || "quotation";
  const editorRef = useRef<CanvasDocumentEditorHandle>(null);
  const editorScrollRef = useRef<HTMLDivElement>(null);
  const docxInputRef = useRef<HTMLInputElement>(null);
  const numbersRef = useRef<Partial<Record<DocumentTemplate, string>>>({});
  const defaultDocNumber = target?.kind === "document" && !target?.record
    ? (numbersRef.current[initialType] ??= createDocumentNumber(initialType))
    : undefined;
  const [name, setName] = useState(
    target?.record?.name ||
      defaultDocNumber ||
      (target?.kind === "template" ? copy.newTemplate : initialType === "quotation" ? copy.quotation : copy.contract)
  );
  const [type, setType] = useState<DocumentTemplate>(initialType);
  const [editorReady, setEditorReady] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [open, setOpen] = useState(true);
  const [isMobile, setIsMobile] = useState(() => (typeof window !== "undefined" ? window.innerWidth < 768 : false));
  useEffect(() => {
    const handleResize = () => setIsMobile(window.innerWidth < 768);
    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, []);
  const [aiSidebarOpen, setAiSidebarOpen] = useState(true);
  const [activeTab, setActiveTab] = useState<"chat" | "document" | "variables">(() => {
    if (typeof window !== "undefined" && window.innerWidth < 768) return "chat";
    return "document";
  });
  useEffect(() => {
    if (!isMobile && activeTab === "chat") {
      setActiveTab("document");
    }
  }, [isMobile, activeTab]);
  const [fitToScreen, setFitToScreen] = useState(true);
  const [containerWidth, setContainerWidth] = useState(() => (typeof window !== "undefined" ? window.innerWidth - 16 : 374));
  const [canvasHeight, setCanvasHeight] = useState(1123);
  const canvasInnerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = editorScrollRef.current;
    if (!el) return;
    const updateWidth = () => {
      if (el.clientWidth > 0) {
        const available = Math.max(200, el.clientWidth - 16);
        setContainerWidth(available);
      } else if (typeof window !== "undefined") {
        setContainerWidth(Math.max(200, window.innerWidth - 16));
      }
    };
    updateWidth();
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        if (entry.contentRect.width > 0) {
          setContainerWidth(entry.contentRect.width);
        }
      }
    });
    observer.observe(el);
    window.addEventListener("resize", updateWidth);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", updateWidth);
    };
  }, [activeTab]);

  useEffect(() => {
    const el = canvasInnerRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        if (entry.contentRect.height > 0) {
          setCanvasHeight(entry.contentRect.height);
        }
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [editorReady]);

  const previewScale = Math.min(1, Math.max(0.2, containerWidth / 794));
  const prepareTemplate = (html: string, documentType: DocumentTemplate) => {
    if (target?.kind !== "document") return html;
    const number = numbersRef.current[documentType] ??= createDocumentNumber(documentType);
    return numberDocumentTemplate(html, number);
  };
  const [initialHTML] = useState(() => target?.record?.html || prepareTemplate(initialTemplate?.html || documentTemplateHTML(initialType, copy), initialType));

  if (!target) return null;

  const handleOpenChange = (nextOpen: boolean) => {
    if (exporting) return;
    setOpen(nextOpen);
  };

  const selectType = (next: DocumentTemplate) => {
    setType(next);
    if (!target.record) setName(next === "quotation" ? copy.quotationTemplate : copy.contractTemplate);
    editorRef.current?.setHTML(documentTemplateHTML(next, copy));
    window.requestAnimationFrame(() => editorScrollRef.current?.scrollTo({ top: 0, left: 0 }));
  };
  const downloadDocument = () => {
    const body = editorRef.current?.getHTML() || "";
    if (!body) return;
    const blob = new Blob([body], { type: "text/html;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${resolveExportName()}.html`;
    anchor.click();
    URL.revokeObjectURL(url);
  };
  const importDocx = async (file: File) => {
    if (!editorRef.current || exporting) return;
    if (!/\.docx$/i.test(file.name) || file.size > 10 * 1024 * 1024) {
      toast.error(copy === zh ? "请选择不超过 10 MB 的 .docx 文件。" : "Choose a .docx file no larger than 10 MB.");
      return;
    }
    if (!window.confirm(copy === zh ? "导入将替换当前编辑内容，是否继续？" : "Import will replace the current editor contents. Continue?")) return;
    setExporting(true);
    onBusyChange?.(true);
    try {
      await editorRef.current.importDocx(file);
      setName(file.name.replace(/\.docx$/i, ""));
      editorScrollRef.current?.scrollTo({ top: 0, left: 0 });
      toast.success(copy === zh ? "Word 文档已导入，请检查排版后保存。" : "Word document imported. Review the layout before saving.");
    } catch (error) {
      console.error("DOCX import failed", error);
      toast.error(copy === zh ? "导入失败，请检查文件是否为有效的 DOCX 文档。当前内容已保留。" : "Import failed. Check that this is a valid DOCX document. Your current content was preserved.");
    } finally { setExporting(false); onBusyChange?.(false); }
  };
  const resolveExportName = () => {
    if (target.kind === "document" && editorRef.current) {
      const docNum = extractDocumentNumber(editorRef.current);
      if (docNum) return docNum;
    }
    return name.trim() || defaultDocNumber || (target.kind === "template" ? copy.newTemplate : copy.newDocument);
  };
  const exportDocument = async (format: "docx" | "pdf" | "image-pdf" | "jpg") => {
    if (!editorRef.current || exporting) return;
    setExporting(true);
    const exportName = resolveExportName();
    try {
      if (format === "docx") await editorRef.current.exportDocx(exportName);
      else await exportDocumentPages(await editorRef.current.getPageImages(), exportName, format);
      toast.success(copy === zh ? "文档已导出" : "Document exported");
    } catch (error) {
      console.error("Document export failed", error);
      toast.error(copy === zh ? "导出失败，请重试或使用打印功能另存为 PDF。" : "Export failed. Try again or use Print to save as PDF.");
    } finally { setExporting(false); }
  };
  const save = async () => {
    const html = editorRef.current?.getDocument();
    if (!html || saving) return;
    const company = extractDocumentCompany(editorRef.current) || extractDocumentCompany(html);
    setSaving(true);
    try {
      await onSave(target.kind, {
        id: target.record?.id || crypto.randomUUID(),
        type,
        name: name.trim() || defaultDocNumber || (target.kind === "template" ? copy.newTemplate : copy.newDocument),
        company,
        html,
        updatedAt: new Date().toISOString(),
      });
    } catch (error) {
      console.error("Document save failed", error);
      const msg = error instanceof Error ? error.message : (copy === zh ? "保存失败，请检查网络或服务端连接。" : "Save failed. Check network or server connection.");
      toast.error(msg);
    } finally {
      setSaving(false);
    }
  };
  const attachPDF = async () => {
    if (!editorRef.current || exporting || !onAttach) return;
    setExporting(true);
    onBusyChange?.(true);
    const exportName = resolveExportName();
    try {
      const file = await createDocumentPDF(await editorRef.current.getPageImages(), exportName);
      onAttach(file);
      toast.success(copy === zh ? "PDF 已添加到邮件附件" : "PDF added to email attachments");
      setOpen(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : copy.loadFailed);
    } finally { setExporting(false); onBusyChange?.(false); }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      onOpenChangeComplete={(nextOpen) => {
        if (!nextOpen) onOpenChange(false);
      }}
    >
      <DialogContent
        data-testid="document-editor-dialog"
        data-editor-kind={target.kind}
        showCloseButton={false}
        className="canvas-document-dialog fixed inset-0 flex h-screen w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none border-0 p-0 ring-0 sm:max-w-none duration-200 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95"
      >
        <DialogTitle className="sr-only">{target.record ? (target.kind === "template" ? copy.editTemplate : copy.editDocument) : (target.kind === "template" ? copy.newTemplate : copy.newDocument)}</DialogTitle>
        <DialogDescription className="sr-only">{copy.documentEditor}</DialogDescription>
        {/* Top Header Bar: Document Title, View Mode Tabs, and Action Buttons */}
        <div className="flex items-center justify-between gap-2 border-b bg-muted/40 px-2 sm:px-4 py-1.5 sm:py-2 overflow-x-auto whitespace-nowrap scrollbar-none touch-pan-x shrink-0">
          <div className="flex items-center gap-1.5 sm:gap-2 shrink-0">
            {isMobile && activeTab === "document" && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-8 px-2 text-xs shrink-0 gap-1 text-primary font-medium"
                onClick={() => setActiveTab("chat")}
              >
                <ArrowLeft className="size-4" />
                <span>{copy === zh ? "AI 对话" : "Chat"}</span>
              </Button>
            )}
            {target.kind === "template" && (
              <div className="flex rounded-md bg-muted p-0.5 shrink-0" role="group" aria-label={copy.documentType}>
                <Button type="button" variant={type === "quotation" ? "secondary" : "ghost"} size="sm" className="h-7 text-xs px-2" onClick={() => selectType("quotation")}>
                  {copy.quotation}
                </Button>
                <Button type="button" variant={type === "contract" ? "secondary" : "ghost"} size="sm" className="h-7 text-xs px-2" onClick={() => selectType("contract")}>
                  {copy.contract}
                </Button>
              </div>
            )}
            <Input
              className="h-8 min-w-28 max-w-40 sm:min-w-36 sm:max-w-64 text-xs sm:text-sm font-medium shrink-0"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={target.kind === "template" ? copy.templateName : copy.documentName}
              aria-label={target.kind === "template" ? copy.templateName : copy.documentName}
            />
          </div>

          {/* View Switcher Tabs - Located in the header */}
          <div className="flex items-center rounded-lg bg-muted p-0.5 border shadow-2xs shrink-0" role="tablist">
            {isMobile ? (
              <>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === "chat"}
                  onClick={() => setActiveTab("chat")}
                  className={cn(
                    "inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-all",
                    activeTab === "chat"
                      ? "bg-background text-foreground shadow-2xs font-semibold"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <Sparkles className="size-3.5 text-primary" />
                  <span>{copy === zh ? "AI 对话" : "Chat"}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === "document"}
                  onClick={() => setActiveTab("document")}
                  className={cn(
                    "inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-all",
                    activeTab === "document"
                      ? "bg-background text-foreground shadow-2xs font-semibold"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <Eye className="size-3.5" />
                  <span>{copy === zh ? "预览文档" : "Preview"}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === "variables"}
                  onClick={() => setActiveTab("variables")}
                  className={cn(
                    "inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-all",
                    activeTab === "variables"
                      ? "bg-background text-foreground shadow-2xs font-semibold"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <SlidersHorizontal className="size-3.5" />
                  <span>{copy === zh ? "变量表单" : "Variables"}</span>
                </button>
              </>
            ) : (
              <>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === "document"}
                  onClick={() => setActiveTab("document")}
                  className={cn(
                    "inline-flex items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium transition-all",
                    activeTab === "document"
                      ? "bg-background text-foreground shadow-2xs font-semibold"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <FileText className="size-3.5" />
                  <span>{copy === zh ? "文档排版" : "Document"}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={activeTab === "variables"}
                  onClick={() => setActiveTab("variables")}
                  className={cn(
                    "inline-flex items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium transition-all",
                    activeTab === "variables"
                      ? "bg-background text-foreground shadow-2xs font-semibold"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  <SlidersHorizontal className="size-3.5" />
                  <span>{copy === zh ? "变量列表" : "Variables"}</span>
                </button>
              </>
            )}
          </div>

          <div className="flex items-center gap-1.5 sm:gap-2 shrink-0">
            <input
              ref={docxInputRef}
              type="file"
              accept=".docx,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
              className="hidden"
              data-testid="document-docx-input"
              onChange={(event) => {
                const file = event.currentTarget.files?.[0];
                event.currentTarget.value = "";
                if (file) void importDocx(file);
              }}
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 text-xs shrink-0"
              disabled={!editorReady || exporting}
              onClick={() => docxInputRef.current?.click()}
            >
              <Upload />
              <span className="hidden sm:inline">{copy === zh ? "导入 Word" : "Import Word"}</span>
              <span className="sm:hidden">{copy === zh ? "导入" : "Word"}</span>
            </Button>
            <DocumentStampManager
              chinese={copy === zh}
              disabled={!editorReady || exporting}
              onInsert={(stamp, width) => editorRef.current?.insertStamp(stamp, width)}
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 text-xs shrink-0"
              disabled={!editorReady || exporting}
              onClick={() => void editorRef.current?.print().catch(() => toast.error(copy.loadFailed))}
            >
              <Printer />
              <span className="hidden sm:inline">{copy.printDocument}</span>
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger render={<Button type="button" variant="outline" size="sm" className="h-8 text-xs shrink-0" disabled={!editorReady || exporting} />}>
                <Download />
                {exporting ? (copy === zh ? "导出中…" : "Exporting…") : (copy === zh ? "导出" : "Export")}
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-auto min-w-44 whitespace-nowrap">
                <DropdownMenuItem className="whitespace-nowrap" onClick={() => void exportDocument("docx")}>
                  {copy === zh ? "下载 docx" : "Download docx"}
                </DropdownMenuItem>
                <DropdownMenuItem className="whitespace-nowrap" onClick={() => void exportDocument("pdf")}>
                  {copy === zh ? "下载 PDF" : "Download PDF"}
                </DropdownMenuItem>
                <DropdownMenuItem className="whitespace-nowrap" onClick={() => void exportDocument("image-pdf")}>
                  {copy === zh ? "下载 PDF（图片）" : "Download PDF (Image)"}
                </DropdownMenuItem>
                <DropdownMenuItem className="whitespace-nowrap" onClick={() => void exportDocument("jpg")}>
                  {copy === zh ? "下载图片" : "Download Image"}
                </DropdownMenuItem>
                <DropdownMenuItem className="whitespace-nowrap" onClick={downloadDocument}>
                  {copy.downloadHTML}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button type="button" size="sm" className="h-8 text-xs shrink-0 font-medium" disabled={!editorReady || exporting || saving} onClick={() => void save()}>
              <Check />
              {saving ? (copy === zh ? "正在保存…" : "Saving…") : copy.saveDocument}
            </Button>
            {onAttach && (
              <Button type="button" size="sm" className="h-8 text-xs shrink-0" disabled={!editorReady || exporting} onClick={() => void attachPDF()}>
                <Paperclip />
                <span className="hidden sm:inline">{exporting ? (copy === zh ? "正在生成 PDF…" : "Generating PDF…") : (copy === zh ? "作为 PDF 添加到邮件" : "Attach PDF to email")}</span>
                <span className="sm:hidden">{copy === zh ? "添加附件" : "Attach"}</span>
              </Button>
            )}
            <Button type="button" variant="outline" size="sm" className="h-8 text-xs shrink-0" disabled={exporting} onClick={() => setOpen(false)}>
              <X />
              <span className="hidden sm:inline">{copy === zh ? "关闭" : "Close"}</span>
            </Button>
          </div>
        </div>

        <div className="flex min-h-0 flex-1 overflow-hidden relative">
          {/* Mobile AI Chat View */}
          {isMobile && activeTab === "chat" ? (
            <DocumentAISidebar
              copy={copy}
              editor={editorRef.current}
              accountEmail={accountEmail}
              type={type}
              title={name}
              isMobile={true}
              onPreviewDocument={() => setActiveTab("document")}
            />
          ) : (
            <>
              {/* Left Content Column: Document Editor (+ Toolbar) or Variable Form */}
              <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
                {/* Formatting Toolbar: Positioned only in the left content column, shown in document view */}
                {activeTab === "document" && (
                  <div className="flex h-11 items-center gap-1 overflow-x-auto whitespace-nowrap border-b bg-muted/20 px-2 sm:px-4 py-1.5 scrollbar-none touch-pan-x shrink-0">
                    {isMobile && (
                      <>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs px-2 shrink-0 gap-1 text-primary font-medium"
                          onClick={() => setActiveTab("chat")}
                        >
                          <ArrowLeft className="size-3.5" />
                          <span>{copy === zh ? "返回 AI 对话" : "Back to Chat"}</span>
                        </Button>
                        <Button
                          type="button"
                          variant={fitToScreen ? "secondary" : "ghost"}
                          size="sm"
                          className="h-7 text-xs px-2 shrink-0 gap-1"
                          onClick={() => setFitToScreen((prev) => !prev)}
                        >
                          {fitToScreen ? <ZoomIn className="size-3.5" /> : <ZoomOut className="size-3.5" />}
                          <span>{fitToScreen ? (copy === zh ? "适应手机" : "Fit") : (copy === zh ? "100% 原始" : "100%")}</span>
                        </Button>
                        <Separator orientation="vertical" className="mx-1 h-4 shrink-0" />
                      </>
                    )}
                    <DocumentEditorButtons copy={copy} editor={editorRef.current} disabled={!editorReady} />
                    <Separator orientation="vertical" className="mx-1 h-5 shrink-0" />
                    <Button
                      type="button"
                      variant={aiSidebarOpen ? "secondary" : "ghost"}
                      size="sm"
                      className="h-7 text-xs shrink-0 gap-1"
                      disabled={!editorReady || !accountEmail}
                      onClick={() => {
                        if (isMobile) {
                          setActiveTab("chat");
                        } else {
                          setAiSidebarOpen((prev) => !prev);
                        }
                      }}
                    >
                      <Sparkles className="size-3.5 text-primary" />
                      <span>{copy.aiDocument}</span>
                    </Button>
                  </div>
                )}

                {/* Document Editor Area (kept mounted with hidden class to preserve state) */}
                <div
                  ref={editorScrollRef}
                  className={cn(
                    "canvas-document-scroll min-h-0 flex-1 bg-muted/30 p-2 sm:p-6",
                    isMobile && fitToScreen ? "overflow-y-auto overflow-x-hidden" : "overflow-auto",
                    activeTab !== "document" && "hidden"
                  )}
                >
                  <div
                    className={cn(
                      "relative transition-transform",
                      isMobile && fitToScreen ? "mx-auto overflow-hidden" : "flex justify-center"
                    )}
                    style={
                      isMobile && fitToScreen
                        ? {
                            width: `${Math.round(794 * previewScale)}px`,
                            height: canvasHeight > 0 ? `${Math.round(canvasHeight * previewScale)}px` : undefined,
                          }
                        : undefined
                    }
                  >
                    <div
                      ref={canvasInnerRef}
                      style={
                        isMobile && fitToScreen
                          ? {
                              width: "794px",
                              transform: `scale(${previewScale})`,
                              transformOrigin: "top left",
                            }
                          : undefined
                      }
                    >
                      <Suspense fallback={<div className="grid h-64 place-items-center text-sm text-muted-foreground">{copy.loadingEditor}</div>}>
                        <CanvasDocumentEditor
                          ref={editorRef}
                          initialHTML={initialHTML}
                          locale={copy === zh ? "zh-CN" : "en"}
                          onReady={() => {
                            setEditorReady(true);
                            editorScrollRef.current?.scrollTo({ top: 0, left: 0 });
                          }}
                        />
                      </Suspense>
                    </div>
                  </div>
                </div>

                {/* Variable Form View */}
                {activeTab === "variables" && (
                  <div className="min-h-0 flex-1 overflow-hidden flex flex-col bg-background">
                    <DocumentVariableForm
                      copy={copy}
                      editor={editorRef.current}
                      aiSidebarOpen={aiSidebarOpen}
                      onToggleAi={() => setAiSidebarOpen((prev) => !prev)}
                      onApplyComplete={(switchToDoc) => {
                        if (switchToDoc) {
                          setActiveTab("document");
                        }
                      }}
                    />
                  </div>
                )}
              </div>

              {/* Right Content Column: AI Document Assistant on Desktop */}
              {!isMobile && aiSidebarOpen && (
                <DocumentAISidebar
                  copy={copy}
                  editor={editorRef.current}
                  accountEmail={accountEmail}
                  type={type}
                  title={name}
                  onClose={() => setAiSidebarOpen(false)}
                />
              )}
            </>
          )}
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
    onSave={async (kind, record) => {
      if (kind === "template") {
        await persistServerTemplate(record, copy);
        const next = [record, ...readStoredTemplates(copy).filter((item) => item.id !== record.id)];
        setTemplates(next);
      } else {
        await persistServerDocument(record);
      }
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
