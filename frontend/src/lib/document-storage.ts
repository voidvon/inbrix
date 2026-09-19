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
  window.localStorage.setItem(DOCUMENT_STORAGE_KEY, JSON.stringify(documents));
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
  window.localStorage.setItem(TEMPLATE_STORAGE_KEY, JSON.stringify(templates));
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
  window.localStorage.setItem(DELETED_TEMPLATE_STORAGE_KEY, JSON.stringify([...ids]));
}
