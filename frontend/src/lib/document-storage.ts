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
  id: string;
  type: DocumentTemplate;
  name: string;
  company?: string;
  html: string;
  updatedAt: string;
};

export type StoredTemplate = StoredDocument;

export type DocumentEditorTarget = {
  kind: "document" | "template";
  record?: StoredDocument;
  initialTemplate?: StoredTemplate;
};

export const DOCUMENT_STORAGE_KEY = "inbrix-documents";
export const TEMPLATE_STORAGE_KEY = "inbrix-document-templates";
export const DELETED_TEMPLATE_STORAGE_KEY = "inbrix-deleted-document-templates";
export const DOCUMENT_PAGE_SIZE = 20;

export function readStoredDocuments(): StoredDocument[] {
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(DOCUMENT_STORAGE_KEY) || "[]");
    if (!Array.isArray(value)) return [];
    return value.filter((item): item is StoredDocument => {
      if (!item || typeof item !== "object") return false;
      const document = item as Partial<StoredDocument>;
      return typeof document.id === "string" &&
        (document.type === "quotation" || document.type === "contract") &&
        typeof document.name === "string" &&
        typeof document.html === "string" &&
        typeof document.updatedAt === "string";
    });
  } catch {
    return [];
  }
}

export function writeStoredDocuments(documents: StoredDocument[]) {
  try {
    window.localStorage.setItem(DOCUMENT_STORAGE_KEY, JSON.stringify(documents));
  } catch {
    // Ignore quota errors as server is primary store
  }
}

export function defaultDocumentTemplates(copy: Copy): StoredTemplate[] {
  return [
    { id: "default-quotation", type: "quotation", name: copy.quotationTemplate, html: documentTemplateHTML("quotation", copy), updatedAt: "" },
    { id: "default-contract", type: "contract", name: copy.contractTemplate, html: documentTemplateHTML("contract", copy), updatedAt: "" },
  ];
}

export function readStoredTemplates(copy: Copy): StoredTemplate[] {
  let stored: StoredTemplate[] = [];
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(TEMPLATE_STORAGE_KEY) || "[]");
    if (Array.isArray(value)) stored = value.filter((item): item is StoredTemplate => {
      if (!item || typeof item !== "object") return false;
      const template = item as Partial<StoredTemplate>;
      return typeof template.id === "string" &&
        (template.type === "quotation" || template.type === "contract") &&
        typeof template.name === "string" &&
        typeof template.html === "string" &&
        typeof template.updatedAt === "string";
    });
  } catch {
    stored = [];
  }
  let deletedIds = new Set<string>();
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(DELETED_TEMPLATE_STORAGE_KEY) || "[]");
    if (Array.isArray(value)) deletedIds = new Set(value.filter((id): id is string => typeof id === "string"));
  } catch {
    deletedIds = new Set();
  }
  const storedById = new Map(stored.filter((template) => !deletedIds.has(template.id)).map((template) => [template.id, template]));
  const defaults = defaultDocumentTemplates(copy).map((template) => {
    const storedTemplate = storedById.get(template.id);
    if (template.id === "default-quotation" && !storedTemplate?.html.includes('data-spirax-quotation="reference-v1"')) return template;
    if (template.id === "default-contract" && !storedTemplate?.html.includes('data-spirax-contract="reference-v1"')) return template;
    return storedTemplate || template;
  });
  return [...defaults.filter((template) => !deletedIds.has(template.id)), ...stored.filter((template) => !template.id.startsWith("default-") && !deletedIds.has(template.id))];
}

export function writeStoredTemplates(templates: StoredTemplate[]) {
  try {
    window.localStorage.setItem(TEMPLATE_STORAGE_KEY, JSON.stringify(templates));
  } catch {
    // Ignore quota errors as server is primary store
  }
}

export function setStoredTemplateDeleted(id: string, deleted: boolean) {
  let ids = new Set<string>();
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(DELETED_TEMPLATE_STORAGE_KEY) || "[]");
    if (Array.isArray(value)) ids = new Set(value.filter((item): item is string => typeof item === "string"));
  } catch {
    ids = new Set();
  }
  if (deleted) ids.add(id);
  else ids.delete(id);
  try {
    window.localStorage.setItem(DELETED_TEMPLATE_STORAGE_KEY, JSON.stringify([...ids]));
  } catch {
    // Ignore quota errors
  }
}

// ---------------------------------------------------------------------------
// Server Persistence Helpers
// ---------------------------------------------------------------------------

export async function fetchServerDocuments(filter: DocumentListFilter = "all"): Promise<StoredDocument[]> {
  const { listDocuments } = await import("./api");
  try {
    const res = await listDocuments(filter);
    if (Array.isArray(res.documents)) {
      writeStoredDocuments(res.documents);
      return res.documents;
    }
  } catch (err) {
    console.warn("Failed to fetch documents from server, falling back to local cache", err);
  }
  return readStoredDocuments().filter((doc) => filter === "all" || doc.type === filter);
}

export async function fetchServerTemplates(copy: Copy): Promise<StoredTemplate[]> {
  const defaults = defaultDocumentTemplates(copy);
  const { listDocumentTemplates } = await import("./api");
  try {
    const res = await listDocumentTemplates();
    if (Array.isArray(res.templates)) {
      const custom = res.templates.filter((t) => !t.id.startsWith("default-"));
      return [...defaults, ...custom];
    }
  } catch (err) {
    console.warn("Failed to fetch templates from server, falling back to local cache", err);
  }
  return readStoredTemplates(copy);
}

export async function persistServerDocument(record: StoredDocument): Promise<StoredDocument> {
  const { saveDocument } = await import("./api");
  const res = await saveDocument(record);
  const saved = (res.document as StoredDocument) || record;
  const current = readStoredDocuments();
  writeStoredDocuments([saved, ...current.filter((d) => d.id !== saved.id)]);
  return saved;
}

export async function persistServerTemplate(record: StoredTemplate, copy?: Copy): Promise<StoredTemplate> {
  const { saveDocumentTemplate } = await import("./api");
  const res = await saveDocumentTemplate(record);
  const saved = (res.template as StoredTemplate) || record;
  setStoredTemplateDeleted(record.id, false);
  const current = readStoredTemplates(copy || en);
  writeStoredTemplates([saved, ...current.filter((t) => t.id !== saved.id)]);
  return saved;
}

export async function deleteServerDocuments(ids: string[]): Promise<void> {
  const { batchDeleteDocuments } = await import("./api");
  await batchDeleteDocuments(ids);
  const idSet = new Set(ids);
  const current = readStoredDocuments();
  writeStoredDocuments(current.filter((d) => !idSet.has(d.id)));
}

export async function deleteServerTemplate(id: string, copy?: Copy): Promise<void> {
  const { deleteDocumentTemplate } = await import("./api");
  await deleteDocumentTemplate(id);
  setStoredTemplateDeleted(id, true);
  const current = readStoredTemplates(copy || en);
  writeStoredTemplates(current.filter((t) => t.id !== id));
}

