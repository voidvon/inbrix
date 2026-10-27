import type { CanvasDocumentEditorHandle } from "../components/app/canvas-document-editor";
import { generateDocument, type DocumentCommand, type QuotationItem } from "./api";

// AI and the variable form apply the exact server-validated snapshot. Never
// rebuild a template locally or apply a reply over intervening editor changes.
export async function updateEditorDocument(
  editor: CanvasDocumentEditorHandle,
  accountEmail: string,
  documentType: "quotation" | "contract",
  title: string,
  change: { instruction: string } | { values: Record<string, string>; items?: QuotationItem[] },
  options?: { requestId: string; signal: AbortSignal },
) {
  const canvas = editor.getDocument();
  const commands: DocumentCommand[] = [];
  if (!("instruction" in change)) {
    if (Object.keys(change.values).length) commands.push({ type: "set_fields", fields: change.values });
    if (change.items) {
      const current = editor.getDocumentData().items;
      const ids = new Set(change.items.flatMap(line => line.lineId ? [line.lineId] : []));
      for (const line of current) if (line.lineId && !ids.has(line.lineId)) commands.push({ type: "remove_line", lineId: line.lineId });
      for (const line of change.items) {
        const previous = current.find(old => old.lineId === line.lineId);
        const fields = Object.fromEntries(Object.entries(line).filter(([key, value]) => !["lineId", "amount", "taxRate", "taxMode"].includes(key) && value !== undefined && (!previous || previous[key as keyof QuotationItem] !== value)));
        if (Object.keys(fields).length) commands.push({ type: previous ? "update_line" : "add_line", lineId: previous?.lineId, fields });
      }
    }
    if (!commands.length) throw new Error("没有需要应用的变更 / No changes to apply");
  }
  const result = await generateDocument({
    accountEmail, documentType, title, canvas,
    mode: "instruction" in change ? "variables" : "apply",
    instruction: "instruction" in change ? change.instruction : "",
    commands: "instruction" in change ? undefined : commands,
    requestId: options?.requestId,
  }, options?.signal);
  options?.signal.throwIfAborted();
  if (editor.isReadOnly()) throw new Error("文档正在保存或导出，请稍后重试 / Document is being saved or exported; retry later");
  if (editor.getDocument() !== canvas) {
    throw new Error("文档在处理期间已修改，请基于最新内容重试 / Document changed; retry using its latest content");
  }
  if (!result.html?.startsWith("__INBRIX_CANVAS_DOCUMENT__:")) {
    throw new Error("缺少经过校验的文档，请刷新后重试 / Missing validated document");
  }
  editor.setHTML(result.html);
  return result;
}
