import { useState } from "react";
import { BookmarkCheck, Eye, FileSignature, FileText, Loader2, Trash2, Wand2 } from "lucide-react";
import { toast } from "../../lib/toast";
import { Button } from "../ui/button";
import { resolveGeneratedDocument } from "../../lib/ai-document-storage";
import { getDocument } from "../../lib/api";
import {
  type DocumentEditorTarget,
  type StoredDocument,
  type StoredTemplate,
  deleteServerDocumentRevision,
  persistServerDocument,
  readStoredTemplates,
} from "../../lib/document-storage";
import type { ConversationAIDocumentAction, ConversationAIDocumentDraft } from "../../types";
import { type Copy, zh } from "../../lib/locale";
import { cn } from "../../lib/utils";
import { DocumentEditorDialog } from "./document-editor-dialog";

type Props = {
  origin?: StoredDocument["aiOrigin"];
  copy: Copy;
  accountEmail: string;
  /** The conversation's other side — the buyer, so their address is never blank. */
  counterpartyEmail?: string;
  /** A freshly drafted quotation/contract the user can save. */
  draft?: ConversationAIDocumentDraft;
  /** A proposed change to an already saved document. */
  action?: ConversationAIDocumentAction;
};

/** The persisted message keeps loose item fields; the builder wants them all. */
function toQuotationItems(items: ConversationAIDocumentDraft["items"]) {
  return (items ?? []).map((item) => ({
    productId: item.productId,
    productVersion: item.productVersion,
    variantId: item.variantId,
    priceId: item.priceId,
    picture: item.picture,
    model: item.model ?? "",
    description: item.description ?? "",
    qty: item.qty ?? "",
    price: item.price ?? "",
    amount: item.amount ?? "",
  }));
}

function documentTypeLabel(type: string | undefined, isZh: boolean) {
  if (type === "contract") return isZh ? "购销合同" : "Sales contract";
  return isZh ? "报价单" : "Quotation";
}

/**
 * Renders a document the conversation agent produced: a draft to save, or a
 * proposed update/delete of a saved document. Nothing is written until the user
 * presses a button, because the agent cannot see the document library.
 *
 * "View document" opens the same full-screen editor the documents page uses,
 * right here, so a quotation the agent just filled in can be read and corrected
 * without leaving the conversation. What it shows is the document as it would be
 * saved — the proposed fields for an update, the generated content for a new
 * one — and saving from the editor writes it to the library.
 */
export function ConversationDocumentCard({ copy, accountEmail, counterpartyEmail, draft, action, origin }: Props) {
  const isZh = copy === zh;
  const [busy, setBusy] = useState<"save" | "apply" | "delete" | null>(null);
  const [savedRecord, setSavedRecord] = useState<StoredDocument | null>(null);
  const [done, setDone] = useState<string | null>(null);
  // Legacy callers without a persisted message retain a local fallback id.
  const [draftId] = useState(() => crypto.randomUUID());
  const [editor, setEditor] = useState<{ record: StoredDocument; templates: StoredTemplate[] } | null>(null);

  /** The document this card stands for, as the editor and the library see it. */
  const buildRecord = async (): Promise<StoredDocument | null> => {
    if (savedRecord) return (await getDocument(savedRecord.id)).document;
    if (draft) {
      // The buyer's address is the one field the conversation itself knows for
      // certain, so it stands in when the agent did not extract one.
      const values = { ...(draft.values || {}) };
      if (counterpartyEmail && !values.customer_email && !values.buyer_email) {
        values.customer_email = counterpartyEmail;
        values.buyer_email = counterpartyEmail;
      }
      return resolveGeneratedDocument({
        savedDocumentId: draft.savedDocumentId,
        id: origin?.messageId || draftId,
        aiOrigin: origin,
        type: draft.type,
        name: draft.title || documentTypeLabel(draft.type, isZh),
        company: draft.counterparty,
        html: draft.html || "",
        values,
        items: toQuotationItems(draft.items),
        terms: draft.terms,
        notes: draft.notes,
        total: draft.total,
        updatedAt: new Date().toISOString(),
      });
    }
    if (action && action.kind !== "delete") {
      if (!action.baseRevision) throw new Error(isZh ? "此方案缺少文档版本，请让 AI 重新生成" : "Regenerate this proposal to include its document revision");
      if (!action.html?.startsWith("__INBRIX_CANVAS_DOCUMENT__:")) throw new Error(isZh ? "此方案缺少原文档内容，请让 AI 重新生成" : "Regenerate this proposal to preserve the original document");
      return {
        id: action.documentId,
        revision: action.baseRevision,
        type: action.type === "contract" ? "contract" : "quotation",
        name: action.name || documentTypeLabel(action.type, isZh),
        company: action.company,
        html: action.html,
        updatedAt: new Date().toISOString(),
      };
    }
    return null;
  };

  const openDocument = async () => {
    try {
      const record = await buildRecord();
      if (!record) return;
      setEditor({ record, templates: readStoredTemplates(copy) });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : copy.loadFailed);
    }
  };

  const saveFromEditor = async (kind: DocumentEditorTarget["kind"], record: StoredDocument) => {
    if (kind !== "document") return;
    try {
      if (action && !action.baseRevision) throw new Error(isZh ? "此方案缺少文档版本，请让 AI 重新生成" : "Regenerate this proposal to include its document revision");
      const saved = await persistServerDocument(record);
      setSavedRecord(saved);
      setEditor({ record: saved, templates: readStoredTemplates(copy) });
      setDone(isZh ? `已保存《${saved.name}》` : `Saved "${saved.name}"`);
      toast.success(isZh ? "已保存到报价单与合同" : "Saved to quotations & contracts");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : isZh ? "保存失败" : "Save failed");
    }
  };

  const saveDraft = async () => {
    if (!draft || busy) return;
    setBusy("save");
    try {
      const stored = await buildRecord();
      if (!stored) return;
      const saved = stored.revision ? stored : await persistServerDocument(stored);
      setSavedRecord(saved);
      setDone(isZh ? `已保存《${saved.name}》` : `Saved "${saved.name}"`);
      toast.success(isZh ? "已保存到报价单与合同" : "Saved to quotations & contracts");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : isZh ? "保存失败" : "Save failed");
    } finally {
      setBusy(null);
    }
  };

  const applyUpdate = async () => {
    if (!action || action.kind !== "update" || busy) return;
    setBusy("apply");
    try {
      if (!action.baseRevision) throw new Error(isZh ? "此方案缺少文档版本，请让 AI 重新生成" : "Regenerate this proposal to include its document revision");
      // The server patched the original canvas at baseRevision. Saving uses
      // that same revision so a concurrent edit cannot be overwritten.
      const rebuilt = await buildRecord();
      if (!rebuilt) return;
      const saved = await persistServerDocument({ ...rebuilt, id: action.documentId });
      setSavedRecord(saved);
      setDone(isZh ? `已更新《${saved.name}》` : `Updated "${saved.name}"`);
      toast.success(isZh ? "文档已更新" : "Document updated");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : isZh ? "更新失败" : "Update failed");
    } finally {
      setBusy(null);
    }
  };

  const removeDocument = async () => {
    if (!action || action.kind !== "delete" || busy) return;
    setBusy("delete");
    try {
      if (!action.baseRevision) throw new Error(isZh ? "此方案缺少文档版本，请让 AI 重新生成" : "Regenerate this proposal to include its document revision");
      await deleteServerDocumentRevision(action.documentId, action.baseRevision);
      setDone(isZh ? `已删除《${action.name}》` : `Deleted "${action.name}"`);
      toast.success(isZh ? "文档已删除" : "Document deleted");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : isZh ? "删除失败" : "Delete failed");
    } finally {
      setBusy(null);
    }
  };

  const isDelete = action?.kind === "delete";
  const Icon = isDelete ? Trash2 : action ? Wand2 : FileText;
  const heading = draft
    ? `${documentTypeLabel(draft.type, isZh)}：${draft.title || draft.counterparty || ""}`
    : isDelete
      ? `${isZh ? "删除文档" : "Delete document"}：${action?.name ?? ""}`
      : `${isZh ? "修改文档" : "Update document"}：${action?.name ?? ""}`;
  const viewable = Boolean(draft) || Boolean(action && action.kind !== "delete");

  return (
    <div
      data-testid="conversation-document-card"
      className={cn(
        "mt-1 w-full max-w-[92%] rounded-xl border bg-card p-3 text-xs shadow-xs",
        isDelete ? "border-destructive/40" : "border-border"
      )}
    >
      <div className="flex items-start gap-2">
        <div
          className={cn(
            "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-md",
            isDelete ? "bg-destructive/10 text-destructive" : "bg-primary/10 text-primary"
          )}
        >
          <Icon className="size-3.5" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium text-foreground" title={heading}>
            {heading}
          </div>
          {draft?.counterparty ? (
            <div className="mt-0.5 truncate text-[11px] text-muted-foreground">
              {isZh ? "客户" : "Customer"}：{draft.counterparty}
              {draft.contact ? ` · ${draft.contact}` : ""}
            </div>
          ) : null}
          {draft ? (
            (() => {
              // Buyers' coordinates, so the card shows how much of the document
              // the thread actually filled instead of only the company name.
              const details = [
                draft.values?.customer_email || draft.values?.buyer_email,
                draft.values?.customer_phone || draft.values?.buyer_phone,
                draft.values?.customer_address || draft.values?.buyer_address,
              ]
                .filter(Boolean)
                .join(" · ");
              return details ? (
                <div className="mt-0.5 truncate text-[11px] text-muted-foreground" title={details}>
                  {details}
                </div>
              ) : null;
            })()
          ) : null}
          {action?.company && !isDelete ? (
            <div className="mt-0.5 truncate text-[11px] text-muted-foreground">
              {isZh ? "客户" : "Customer"}：{action.company}
            </div>
          ) : null}
          {action?.summary ? (
            <div className="mt-1 line-clamp-2 text-[11px] text-muted-foreground" title={action.summary}>
              {action.summary}
            </div>
          ) : null}
        </div>
      </div>

      {draft?.items?.length ? (
        <div className="mt-2 overflow-hidden rounded-lg border border-border/70">
          <table className="w-full text-[11px]">
            <thead className="bg-muted/60 text-muted-foreground">
              <tr>
                <th className="px-2 py-1 text-left font-medium">{isZh ? "型号" : "Model"}</th>
                <th className="px-2 py-1 text-right font-medium">{isZh ? "数量" : "Qty"}</th>
                <th className="px-2 py-1 text-right font-medium">{isZh ? "单价" : "Price"}</th>
                <th className="px-2 py-1 text-right font-medium">{isZh ? "金额" : "Amount"}</th>
              </tr>
            </thead>
            <tbody>
              {draft.items.slice(0, 5).map((item, index) => (
                <tr key={`${item.model ?? item.description ?? "item"}-${index}`} className="border-t border-border/60">
                  <td className="max-w-[120px] truncate px-2 py-1" title={item.model || item.description}>
                    {item.model || item.description || "—"}
                  </td>
                  <td className="px-2 py-1 text-right">{item.qty || "—"}</td>
                  <td className="px-2 py-1 text-right">{item.price || "—"}</td>
                  <td className="px-2 py-1 text-right">{item.amount || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {draft.items.length > 5 ? (
            <div className="border-t border-border/60 bg-muted/40 px-2 py-1 text-[10px] text-muted-foreground">
              {isZh
                ? `另有 ${draft.items.length - 5} 行，打开文档查看完整明细`
                : `${draft.items.length - 5} more rows — open the document for the full list`}
            </div>
          ) : null}
        </div>
      ) : null}

      {action?.kind === "update" && action.items?.length ? (
        <div className="mt-2 text-[11px] text-muted-foreground">
          {isZh ? "明细将更新为" : "Line items will become"}：{action.items.length} {isZh ? "项" : "items"}
        </div>
      ) : null}

      <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
        {viewable ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-7 gap-1 px-2.5 text-xs shadow-none"
            onClick={() => void openDocument()}
            title={copy.viewDocument}
            data-testid="conversation-document-view"
          >
            <Eye className="size-3" />
            {copy.viewDocument}
          </Button>
        ) : null}
        {!done && draft ? (
          <Button
            type="button"
            size="sm"
            className="h-7 gap-1 px-2.5 text-xs shadow-none"
            disabled={busy !== null}
            onClick={() => void saveDraft()}
          >
            {busy === "save" ? <Loader2 className="size-3 animate-spin" /> : <BookmarkCheck className="size-3" />}
            {isZh ? "保存到报价单与合同" : "Save to documents"}
          </Button>
        ) : null}
        {!done && action?.kind === "update" ? (
          <Button
            type="button"
            size="sm"
            className="h-7 gap-1 px-2.5 text-xs shadow-none"
            disabled={busy !== null}
            onClick={() => void applyUpdate()}
          >
            {busy === "apply" ? <Loader2 className="size-3 animate-spin" /> : <FileSignature className="size-3" />}
            {isZh ? "应用修改" : "Apply changes"}
          </Button>
        ) : null}
        {!done && isDelete ? (
          <Button
            type="button"
            variant="destructive"
            size="sm"
            className="h-7 gap-1 px-2.5 text-xs shadow-none"
            disabled={busy !== null}
            onClick={() => void removeDocument()}
          >
            {busy === "delete" ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />}
            {isZh ? "确认删除" : "Delete"}
          </Button>
        ) : null}
        {done ? (
          <div className="flex items-center gap-1.5 text-[11px] text-emerald-600 dark:text-emerald-400">
            <BookmarkCheck className="size-3" />
            {done}
          </div>
        ) : null}
      </div>

      {editor ? (
        <DocumentEditorDialog
          key={`document-${editor.record.id}`}
          copy={copy}
          accountEmail={accountEmail}
          target={{ kind: "document", record: editor.record }}
          templates={editor.templates}
          initialTab="document"
          onOpenChange={(open) => {
            if (!open) setEditor(null);
          }}
          onSave={saveFromEditor}
        />
      ) : null}
    </div>
  );
}
