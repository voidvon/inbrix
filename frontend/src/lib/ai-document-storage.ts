import type { AIUserQuestion } from "../types";
import { createBusinessFromTemplate, createDocumentBusiness } from "./document-business";
import type { AIChatAttachment } from "../types";
import type { AIChatSession, AIAgentDocumentAction, AIBusinessContext, AIToolCall, QuotationItem } from "./api";
import type { DocumentTemplate, StoredDocument } from "./document-storage";
import { documentTemplateHTML } from "./document-storage";
import { createDocumentNumber } from "./document-number";
import { type Copy, zh } from "./locale";
import { createSpiraxQuotationCanvasDocument } from "../components/app/spirax-quotation-canvas";
import { createSpiraxContractCanvasDocument } from "../components/app/spirax-contract-canvas";
import { ApiError, getDocument } from "./api";

export type AIDocumentGeneratedData = {
  savedDocumentId?: string;
  aiOrigin?: StoredDocument["aiOrigin"];
  templateId?: string;
  sourceQuotationId?: string;
  sourceQuotationRevision?: number;
  id: string;
  type: DocumentTemplate;
  name: string;
  html: string;
  company?: string;
  summary?: string;
  values?: Record<string, string>;
  items?: QuotationItem[];
  terms?: string[];
  notes?: string;
  subtotal?: string;
  total?: string;
  updatedAt: string;
};

export type AIDocumentMessage = {
  question?: AIUserQuestion;
  attachments?: AIChatAttachment[];
  documentAction?: AIAgentDocumentAction;
  id: string;
  role: "user" | "assistant";
  content: string;
  createdAt: string;
  toolCalls?: AIToolCall[];
  document?: AIDocumentGeneratedData;
  error?: string;
};

export type AIDocumentSession = {
  version: number;
  businessContext?: AIBusinessContext;
  id: string;
  title: string;
  documentType: DocumentTemplate;
  agentId?: string;
  createdAt: string;
  updatedAt: string;
  messages: AIDocumentMessage[];
};

export function createInitialSession(copy: Copy): AIDocumentSession {
  const isZh = copy === zh;
  const initialMessage: AIDocumentMessage = {
    id: "welcome-msg",
    role: "assistant",
    content: isZh
      ? "您好！我是您的 AI 文档助手。\n\n我可以帮您快速起草、修改和润色各类商业报价单与购销合同，精准填充变量、计算价格，并直接在编辑器中排版、导出 PDF 或添加至邮件附件。\n\n请在下方告诉我您的需求，例如：“分析客户需求，核对产品规格与价格，然后制作报价草稿。”"
      : "Hello! I am your AI Document Assistant.\n\nI can help you draft, modify, and polish business quotations and contracts with precise variable filling, price calculation, and direct editor canvas integration.\n\nTell me what you need below, for example: 'Analyze the requirements, verify catalog specifications and prices, then draft a quotation.'",
    createdAt: new Date().toISOString(),
  };

  return {
    id: "",
    version: 0,
    title: isZh ? "文档助手对话" : "Document Assistant",
    documentType: "quotation",
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    messages: [initialMessage],
  };
}

// Server cards use title/counterparty; the document canvas uses name/company.
export function toAIDocumentSession(session: AIChatSession, copy: Copy): AIDocumentSession {
  return {
    ...session,
    agentId: session.businessContext.agentId || undefined,
    messages: session.messages.map(message => ({
      ...message,
      toolCalls: message.toolCalls?.map(call => ({ ...call, args: {} })),
      document: message.document ? {
        ...message.document,
        id: message.id,
        aiOrigin: { sessionId: session.id, messageId: message.id },
        name: message.document.title,
        company: message.document.counterparty,
        html: message.document.html || documentTemplateHTML(message.document.type, copy),
        updatedAt: message.createdAt,
      } : undefined,
    })),
  };
}

export function convertGeneratedToStoredDocument(generated: AIDocumentGeneratedData): StoredDocument {
  let html = generated.html;
  if (!html || html.includes("data-spirax-")) {
    if (generated.type === "quotation" || generated.type === "contract") {
      const isQuotation = generated.type === "quotation";
      const values = isQuotation ? generated.values : { trade_term: "EXW (Ex Works)", contract_title: "PROFORMA INVOICE", ...generated.values };
      const date = new Date().toLocaleDateString("zh-CN");
      const docNum = isQuotation
        ? (generated.values?.quote_number || createDocumentNumber("quotation"))
        : (generated.values?.contract_number || "SC-" + Date.now().toString().slice(-6));
      try {
        const main = isQuotation
          ? createSpiraxQuotationCanvasDocument(date, docNum, values, generated.items)
          : createSpiraxContractCanvasDocument(date, docNum, values, generated.items);
        const canvasDoc = {
          template: isQuotation ? "spirax-quotation" : "spirax-contract",
          data: { main },
          business: createDocumentBusiness({ ...createBusinessFromTemplate({ main }).values, ...values }, generated.items),
        };
        html = `__INBRIX_CANVAS_DOCUMENT__:${JSON.stringify(canvasDoc)}`;
      } catch (e) {
        console.error("Failed to build canvas document", e);
      }
    }
  }

  return {
    aiOrigin: generated.aiOrigin,
    id: generated.id || crypto.randomUUID(),
    type: generated.type,
    name: generated.name,
    company: generated.company,
    html: html || documentTemplateHTML(generated.type, zh),
    updatedAt: new Date().toISOString(),
  };
}

// Opening or saving an already saved card resolves the latest library content.
// Never attach a freshly fetched revision to the old generated fields.
export async function resolveGeneratedDocument(generated: AIDocumentGeneratedData): Promise<StoredDocument> {
  const draft = convertGeneratedToStoredDocument(generated);
  if (!draft.aiOrigin) return draft;
  try {
    const { document } = await getDocument(draft.id);
    return document;
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      if (generated.savedDocumentId) throw new Error("此卡片关联的文档已删除，请重新起草 / The linked document was deleted; start a new draft");
      return draft;
    }
    throw error;
  }
}
