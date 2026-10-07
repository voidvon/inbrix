import type { AIToolCall, QuotationItem } from "./api";
import type { DocumentTemplate, StoredDocument } from "./document-storage";
import { documentTemplateHTML } from "./document-storage";
import { type Copy, zh } from "./locale";
import { createSpiraxQuotationCanvasDocument } from "../components/app/spirax-quotation-canvas";
import { createSpiraxContractCanvasDocument } from "../components/app/spirax-contract-canvas";

export type AIDocumentGeneratedData = {
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
  taxAmount?: string;
  total?: string;
  updatedAt: string;
};

export type AIDocumentMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  createdAt: string;
  toolCalls?: AIToolCall[];
  document?: AIDocumentGeneratedData;
  error?: string;
};

export type AIDocumentSession = {
  id: string;
  title: string;
  documentType: DocumentTemplate;
  agentId?: string;
  createdAt: string;
  updatedAt: string;
  messages: AIDocumentMessage[];
};

const AI_DOCUMENT_STORAGE_KEY = "inbrix-ai-document-sessions";
const ACTIVE_SESSION_STORAGE_KEY = "inbrix-ai-document-active-session";

export function readAIDocumentSessions(copy: Copy): AIDocumentSession[] {
  try {
    const raw = window.localStorage.getItem(AI_DOCUMENT_STORAGE_KEY);
    if (!raw) return [createInitialSession(copy)];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed) || parsed.length === 0) {
      return [createInitialSession(copy)];
    }
    return parsed as AIDocumentSession[];
  } catch {
    return [createInitialSession(copy)];
  }
}

export function writeAIDocumentSessions(sessions: AIDocumentSession[]) {
  try {
    window.localStorage.setItem(AI_DOCUMENT_STORAGE_KEY, JSON.stringify(sessions));
  } catch (err) {
    console.error("Failed to persist AI document sessions", err);
  }
}

export function getActiveSessionId(): string | null {
  try {
    return window.localStorage.getItem(ACTIVE_SESSION_STORAGE_KEY);
  } catch {
    return null;
  }
}

export function setActiveSessionId(id: string) {
  try {
    window.localStorage.setItem(ACTIVE_SESSION_STORAGE_KEY, id);
  } catch {
    // ignore
  }
}

export function createInitialSession(copy: Copy): AIDocumentSession {
  const isZh = copy === zh;
  const initialMessage: AIDocumentMessage = {
    id: "welcome-msg",
    role: "assistant",
    content: isZh
      ? "您好！我是您的 AI 文档助手。\n\n我可以帮您快速起草、修改和润色各类商业报价单与购销合同，精准填充变量、计算价格，并直接在编辑器中排版、导出 PDF 或添加至邮件附件。\n\n请在下方告诉我您的需求，例如：“帮我起草一份斯派莎克减压阀报价单，买方为上海宝钢…”"
      : "Hello! I am your AI Document Assistant.\n\nI can help you draft, modify, and polish business quotations and contracts with precise variable filling, price calculation, and direct editor canvas integration.\n\nTell me what you need below, for example: 'Draft a quotation for Spirax Sarco valves for Baosteel...'",
    createdAt: new Date().toISOString(),
  };

  return {
    id: "default-ai-session",
    title: isZh ? "文档助手对话" : "Document Assistant",
    documentType: "quotation",
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    messages: [initialMessage],
  };
}

export function createNewAIDocumentSession(
  copy: Copy,
  type: DocumentTemplate = "quotation",
  title?: string
): AIDocumentSession {
  const isZh = copy === zh;
  const newSession: AIDocumentSession = {
    id: `ai-session-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    title: title || (isZh ? `新${type === "quotation" ? "报价单" : "合同"}对话` : `New ${type} Chat`),
    documentType: type,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    messages: [
      {
        id: `welcome-${Date.now()}`,
        role: "assistant",
        content: isZh
          ? `已开启关于【${type === "quotation" ? "报价单" : "销售合同"}】的新对话。请告诉我有关于客户、产品清单、价格、交货期等具体需求。`
          : `Started a new chat for 【${type}】. Please provide customer details, product items, pricing, delivery terms, etc.`,
        createdAt: new Date().toISOString(),
      },
    ],
  };

  const sessions = readAIDocumentSessions(copy);
  const next = [newSession, ...sessions];
  writeAIDocumentSessions(next);
  setActiveSessionId(newSession.id);
  return newSession;
}

export function saveMessageToSession(
  copy: Copy,
  sessionId: string,
  message: AIDocumentMessage
): AIDocumentSession[] {
  const sessions = readAIDocumentSessions(copy);
  let found = false;
  const next = sessions.map((session) => {
    if (session.id === sessionId) {
      found = true;
      const updatedMessages = [...session.messages, message];
      let newTitle = session.title;
      // If title is default, rename using first user message summary
      if (session.messages.length <= 2 && message.role === "user") {
        const clean = message.content.trim().slice(0, 18);
        if (clean) newTitle = clean;
      }
      return {
        ...session,
        title: newTitle,
        updatedAt: new Date().toISOString(),
        messages: updatedMessages,
      };
    }
    return session;
  });

  if (!found) {
    // create and prepend
    const newSession: AIDocumentSession = {
      id: sessionId,
      title: message.role === "user" ? message.content.slice(0, 18) : "文档对话",
      documentType: message.document?.type || "quotation",
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      messages: [message],
    };
    next.unshift(newSession);
  }

  writeAIDocumentSessions(next);
  return next;
}

export function deleteAIDocumentSession(copy: Copy, sessionId: string): AIDocumentSession[] {
  const sessions = readAIDocumentSessions(copy);
  const next = sessions.filter((s) => s.id !== sessionId);
  if (next.length === 0) {
    const initial = createInitialSession(copy);
    writeAIDocumentSessions([initial]);
    setActiveSessionId(initial.id);
    return [initial];
  }
  writeAIDocumentSessions(next);
  if (getActiveSessionId() === sessionId) {
    setActiveSessionId(next[0].id);
  }
  return next;
}

export function clearSessionHistory(copy: Copy, sessionId: string): AIDocumentSession[] {
  const sessions = readAIDocumentSessions(copy);
  const initial = createInitialSession(copy);
  const next = sessions.map((s) => {
    if (s.id === sessionId) {
      return {
        ...s,
        updatedAt: new Date().toISOString(),
        messages: [initial.messages[0]],
      };
    }
    return s;
  });
  writeAIDocumentSessions(next);
  return next;
}

export function updateSessionAgent(copy: Copy, sessionId: string, agentId?: string): AIDocumentSession[] {
  const sessions = readAIDocumentSessions(copy);
  const next = sessions.map((s) => {
    if (s.id === sessionId) {
      return {
        ...s,
        agentId,
        updatedAt: new Date().toISOString(),
      };
    }
    return s;
  });
  writeAIDocumentSessions(next);
  return next;
}

export function convertGeneratedToStoredDocument(generated: AIDocumentGeneratedData): StoredDocument {
  let html = generated.html;
  if (!html || html.includes("data-spirax-") || html.trim().startsWith("<div")) {
    if (generated.type === "quotation" || generated.type === "contract") {
      const isQuotation = generated.type === "quotation";
      const date = new Date().toLocaleDateString("zh-CN");
      const docNum = isQuotation
        ? (generated.values?.quote_number || "SQ-" + Date.now().toString().slice(-6))
        : (generated.values?.contract_number || "SC-" + Date.now().toString().slice(-6));
      try {
        const main = isQuotation
          ? createSpiraxQuotationCanvasDocument(date, docNum, generated.values, generated.items)
          : createSpiraxContractCanvasDocument(date, docNum, generated.values, generated.items);
        const canvasDoc = {
          template: isQuotation ? "spirax-quotation" : "spirax-contract",
          data: { main },
        };
        html = `__INBRIX_CANVAS_DOCUMENT__:${JSON.stringify(canvasDoc)}`;
      } catch (e) {
        console.error("Failed to build canvas document", e);
      }
    }
  }

  return {
    id: generated.id || crypto.randomUUID(),
    type: generated.type,
    name: generated.name,
    company: generated.company,
    html: html || documentTemplateHTML(generated.type, zh),
    updatedAt: new Date().toISOString(),
  };
}
