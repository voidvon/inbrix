import type { CanvasDocumentEditorHandle } from "../components/app/canvas-document-editor";
import { generateDocument, type QuotationItem } from "./api";

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
  const result = await generateDocument({
    accountEmail, documentType, title, canvas,
    mode: "instruction" in change ? "variables" : "apply",
    instruction: "instruction" in change ? change.instruction : "",
    ...change,
    requestId: options?.requestId,
  }, options?.signal);
  options?.signal.throwIfAborted();
  if (editor.getDocument() !== canvas) {
    throw new Error("文档在处理期间已修改，请基于最新内容重试 / Document changed; retry using its latest content");
  }
  if (!result.html?.startsWith("__INBRIX_CANVAS_DOCUMENT__:")) {
    throw new Error("缺少经过校验的文档，请刷新后重试 / Missing validated document");
  }
  editor.setHTML(result.html);
  return result;
}
