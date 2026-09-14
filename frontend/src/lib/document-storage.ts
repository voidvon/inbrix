import type { Copy } from "./locale";
import { en } from "./locale";
import { escapeHTML } from "./email-format";

export type DocumentTemplate = "quotation" | "contract";
export type DocumentListFilter = "all" | DocumentTemplate;

export function spiraxQuotationTemplate(date: string) {
  return `<div data-spirax-quotation="reference-v1" data-issue-date="${escapeHTML(date)}"></div>`;
}

export function documentTemplateHTML(type: DocumentTemplate, copy: Copy) {
  const date = new Date().toLocaleDateString(copy === en ? "en-US" : "zh-CN");
  if (type === "contract") {
    return copy === en
      ? `<h1 style="text-align:center">CONTRACT</h1><p><strong>Contract No.:</strong> [Contract number]</p><p><strong>Effective date:</strong> ${date}</p><p><strong>Party A:</strong> [Company / individual]</p><p><strong>Party B:</strong> [Company / individual]</p><h2>1. Scope</h2><p>[Describe the products, services, or cooperation covered by this contract.]</p><h2>2. Price and payment</h2><p>[Specify the contract value, payment method, and payment schedule.]</p><h2>3. Delivery and acceptance</h2><p>[Specify delivery milestones and acceptance criteria.]</p><h2>4. Rights and obligations</h2><p>[Specify the rights and obligations of each party.]</p><h2>5. Confidentiality and breach</h2><p>[Specify confidentiality obligations and liability for breach.]</p><h2>6. Term and termination</h2><p>[Specify the contract term and termination conditions.]</p><p><br></p><table><tbody><tr><td><strong>Party A (signature)</strong><p><br></p><p>Date:</p></td><td><strong>Party B (signature)</strong><p><br></p><p>Date:</p></td></tr></tbody></table>`
      : `<h1 style="text-align:center">合同</h1><p><strong>合同编号：</strong>[合同编号]</p><p><strong>生效日期：</strong>${date}</p><p><strong>甲方：</strong>[公司或个人名称]</p><p><strong>乙方：</strong>[公司或个人名称]</p><h2>一、合同范围</h2><p>[填写本合同涉及的产品、服务或合作内容。]</p><h2>二、价款与支付</h2><p>[填写合同金额、支付方式和付款节点。]</p><h2>三、交付与验收</h2><p>[填写交付时间、阶段目标和验收标准。]</p><h2>四、双方权利与义务</h2><p>[填写甲乙双方的权利与义务。]</p><h2>五、保密与违约责任</h2><p>[填写保密义务与违约责任。]</p><h2>六、期限与终止</h2><p>[填写合同期限和终止条件。]</p><p><br></p><table><tbody><tr><td><strong>甲方（签章）</strong><p><br></p><p>日期：</p></td><td><strong>乙方（签章）</strong><p><br></p><p>日期：</p></td></tr></tbody></table>`;
  }
  return spiraxQuotationTemplate(date);
}

export type StoredDocument = {
  id: string;
  type: DocumentTemplate;
  name: string;
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
