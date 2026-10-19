import { useState } from "react";
import { BookmarkCheck, FileSignature, FileText, Loader2, Trash2, Wand2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "../ui/button";
import { convertGeneratedToStoredDocument } from "../../lib/ai-document-storage";
import { deleteServerDocuments, persistServerDocument } from "../../lib/document-storage";
import type { ConversationAIDocumentAction, ConversationAIDocumentDraft } from "../../types";
import { cn } from "../../lib/utils";

type Props = {
  isZh: boolean;
  /** A freshly drafted quotation/contract the user can save. */
  draft?: ConversationAIDocumentDraft;
  /** A proposed change to an already saved document. */
  action?: ConversationAIDocumentAction;
};

/** The persisted message keeps loose item fields; the builder wants them all. */
function toQuotationItems(items: ConversationAIDocumentDraft["items"]) {
  return (items ?? []).map((item) => ({
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
 * presses the button, because the agent cannot see the document library.
 */
export function ConversationDocumentCard({ isZh, draft, action }: Props) {
  const [busy, setBusy] = useState<"save" | "apply" | "delete" | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const saveDraft = async () => {
    if (!draft || busy) return;
    setBusy("save");
    try {
      const stored = convertGeneratedToStoredDocument({
        id: crypto.randomUUID(),
        type: draft.type,
        name: draft.title || documentTypeLabel(draft.type, isZh),
        company: draft.counterparty,
        html: draft.html || "",
        values: draft.values,
        items: toQuotationItems(draft.items),
        terms: draft.terms,
        notes: draft.notes,
        total: draft.total,
        updatedAt: new Date().toISOString(),
      });
      const saved = await persistServerDocument(stored);
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
      // Rebuild the canvas document from the merged fields the server returned,
      // keeping the original document id so the library entry is replaced.
      const rebuilt = convertGeneratedToStoredDocument({
        id: action.documentId,
        type: action.type === "contract" ? "contract" : "quotation",
        name: action.name || documentTypeLabel(action.type, isZh),
        company: action.company,
        html: "",
        values: action.values,
        items: toQuotationItems(action.items),
        updatedAt: new Date().toISOString(),
      });
      const saved = await persistServerDocument({ ...rebuilt, id: action.documentId });
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
      await deleteServerDocuments([action.documentId]);
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
        </div>
      ) : null}

      {action?.kind === "update" && action.items?.length ? (
        <div className="mt-2 text-[11px] text-muted-foreground">
          {isZh ? "明细将更新为" : "Line items will become"}：{action.items.length} {isZh ? "项" : "items"}
        </div>
      ) : null}

      {done ? (
        <div className="mt-2 flex items-center gap-1.5 text-[11px] text-emerald-600 dark:text-emerald-400">
          <BookmarkCheck className="size-3" />
          {done}
        </div>
      ) : (
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
          {draft ? (
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
          {action?.kind === "update" ? (
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
          {isDelete ? (
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
        </div>
      )}
    </div>
  );
}
