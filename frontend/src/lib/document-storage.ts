import type { Copy } from "./locale";
import { en } from "./locale";
import { escapeHTML } from "./email-format";

export type DocumentTemplate = "quotation" | "contract";
export type DocumentListFilter = "all" | DocumentTemplate;

export function spiraxQuotationTemplate(date: string) {
  return `<div data-spirax-quotation="reference-v1" data-issue-date="${escapeHTML(date)}"></div>`;
}

export function spiraxContractTemplate(date: string) {
  return `<div data-spirax-contract="reference-v1" data-issue-date="${escapeHTML(date)}"></div>`;
}

export function documentTemplateHTML(type: DocumentTemplate, copy: Copy) {
  const date = new Date().toLocaleDateString(copy === en ? "en-US" : "zh-CN");
  if (type === "contract") {
    return spiraxContractTemplate(date);
  }
  return spiraxQuotationTemplate(date);
}

export function isSellerConcept(conceptId: string): boolean {
  const lower = conceptId.toLowerCase();
  return (
    lower.startsWith("seller_") ||
    lower.startsWith("vendor_") ||
    lower.startsWith("supplier_") ||
    lower.startsWith("bank_") ||
    lower.startsWith("beneficiary_") ||
    lower.includes("seller") ||
    lower === "swift_code" ||
    lower === "iban"
  );
}

export function cleanDocumentVariablesForConversation<T extends { values?: Record<string, string>; items?: unknown[]; company?: string; terms?: string[]; notes?: string; subtotal?: string; taxAmount?: string; total?: string }>(doc: T): T {
  const cleanedValues: Record<string, string> = {};
  if (doc.values) {
    for (const [key, val] of Object.entries(doc.values)) {
      if (isSellerConcept(key)) {
        cleanedValues[key] = val;
      }
    }
  }
  return {
    ...doc,
    company: undefined,
    items: [],
    values: cleanedValues,
    terms: undefined,
    notes: undefined,
    subtotal: undefined,
    taxAmount: undefined,
    total: undefined,
  };
}

export type StoredDocument = {
  aiOrigin?: { sessionId: string; messageId: string };
  revision?: number;
  id: string;
  type: DocumentTemplate;
  name: string;
  company?: string;
  html: string;
  updatedAt: string;
};

export type StoredTemplate = StoredDocument;
export type DocumentSummary = Omit<StoredDocument, "html" | "aiOrigin">;

export function summarizeDocument(document: DocumentSummary): DocumentSummary {
  return { id: document.id, revision: document.revision, type: document.type, name: document.name, company: document.company, updatedAt: document.updatedAt };
}

export type DocumentEditorTarget = {
  kind: "document" | "template";
  record?: StoredDocument;
  initialTemplate?: StoredTemplate;
};

export const DOCUMENT_CHANGED_EVENT = "inbrix-documents-changed";
export const DOCUMENT_PAGE_SIZE = 20;

// Old browser records are discarded, never imported as the current user's data.
export function clearLegacyDocumentCache() {
  for (const key of ["inbrix-documents", "inbrix-document-templates", "inbrix-deleted-document-templates", "inbrix-document-stamps", "inbrix-ai-document-sessions"]) {
    try { window.localStorage.removeItem(key); } catch { /* Storage may be blocked. */ }
  }
}

// ---------------------------------------------------------------------------
// Server Persistence Helpers
// ---------------------------------------------------------------------------

export async function fetchServerDocuments(filter: DocumentListFilter = "all"): Promise<DocumentSummary[]> {
  const { listDocuments } = await import("./api");
  const res = await listDocuments(filter);
  return res.documents.map(summarizeDocument);
}

export async function fetchServerTemplates(copy: Copy): Promise<StoredTemplate[]> {
  const { listDocumentTemplates } = await import("./api");
  const { templates } = await listDocumentTemplates(copy === en ? "en" : "zh-CN");
  if (!Array.isArray(templates)) throw new Error(copy.loadFailed);
  return templates;
}

export async function persistServerDocument(record: StoredDocument): Promise<StoredDocument> {
  const { saveDocument } = await import("./api");
  const res = await saveDocument(record);
  const saved = res.document;
  window.dispatchEvent(new Event(DOCUMENT_CHANGED_EVENT));
  return saved;
}

export async function persistServerTemplate(record: StoredTemplate): Promise<StoredTemplate> {
  const { saveDocumentTemplate } = await import("./api");
  const { template } = await saveDocumentTemplate(record);
  return template;
}

export async function deleteServerDocuments(ids: string[]): Promise<void> {
  const { batchDeleteDocuments } = await import("./api");
  await batchDeleteDocuments(ids);
  window.dispatchEvent(new Event(DOCUMENT_CHANGED_EVENT));
}

export async function deleteServerTemplate(id: string): Promise<void> {
  const { deleteDocumentTemplate } = await import("./api");
  await deleteDocumentTemplate(id);
}

export async function deleteServerDocumentRevision(id: string, revision: number): Promise<void> {
  const { deleteDocument } = await import("./api");
  await deleteDocument(id, revision);
  window.dispatchEvent(new Event(DOCUMENT_CHANGED_EVENT));
}
