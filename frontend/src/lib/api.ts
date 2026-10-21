import { getCookie } from "./utils";
import { CLIENT_ID } from "./client-id";
import type { CalendarEvent, ComposeDraftInput, ComposeDraftListResponse, ComposeDraftResponse, ConnectedAccount, ConversationAIChatMessage, ConversationDetailResponse, ConversationListResponse, MailAttachmentListResponse, MailMessage, MailSearchResponse, Mailbox, PriceColumn, PriceImportResult, PriceList, PriceListDetailResponse, PriceLookupEntry, PriceRow, PriceRowsResponse } from "../types";

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function fetchCSRFToken(signal?: AbortSignal | null) {
  const response = await fetch("/csrf", {
    headers: { Accept: "application/json" },
    credentials: "include",
    signal,
  });
  if (!response.ok) throw new ApiError("Failed to initialize request security", response.status);
  const payload = await response.json() as { token?: string };
  return payload.token || getCookie("_csrf");
}

export async function apiFetch<T>(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  headers.set("X-Client-Id", CLIENT_ID);
  const method = (init.method || "GET").toUpperCase();
  const mutates = method !== "GET" && method !== "HEAD" && method !== "OPTIONS";
  let csrf = getCookie("_csrf");
  if (!csrf && mutates) {
    csrf = await fetchCSRFToken(init.signal);
  }
  if (csrf) headers.set("X-CSRF-Token", csrf);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const send = () => fetch(path, {
    ...init,
    headers,
    credentials: "include",
  });
  let response = await send();
  // Fiber keeps CSRF tokens in process memory. After a backend restart, the
  // browser can retain a cookie whose server-side token no longer exists. The
  // first rejected mutation expires that stale cookie; refresh and retry once.
  if (response.status === 403 && mutates) {
    csrf = await fetchCSRFToken(init.signal);
    if (csrf) headers.set("X-CSRF-Token", csrf);
    response = await send();
  }
  const contentType = response.headers.get("content-type") || "";
  const payload: unknown = contentType.includes("application/json") ? await response.json() as unknown : await response.text();
  if (!response.ok) {
    const message = typeof payload === "object" && payload && "error" in payload
      ? String((payload as { error?: unknown }).error)
      : typeof payload === "string" ? payload : "Request failed";
    throw new ApiError(message, response.status);
  }
  return payload as T;
}

export function getConversations(options: {
  query?: string;
  filter?: "open" | "all";
  revision?: number;
  cursor?: string;
  limit?: number;
} = {}) {
  const params = new URLSearchParams();
  if (options.query?.trim()) params.set("q", options.query.trim());
  if (options.filter) params.set("filter", options.filter);
  if (options.revision !== undefined && options.revision > 0) params.set("rev", String(options.revision));
  if (options.cursor) params.set("cursor", options.cursor);
  if (options.limit !== undefined && options.limit > 0) params.set("limit", String(options.limit));
  const suffix = params.toString() ? `?${params.toString()}` : "";
  return apiFetch<ConversationListResponse>(`/api/conversations${suffix}`);
}

/**
 * Full-text search over the local mail mirror. Results are message-level so the
 * client can open a conversation at the matching message.
 */
export function searchMailMessages(query: string, limit = 50) {
  const params = new URLSearchParams({ q: query.trim(), limit: String(limit) });
  return apiFetch<MailSearchResponse>(`/api/mail/search?${params.toString()}`);
}

// ---------------------------------------------------------------------------
// Price lists: user-defined spreadsheet tables that quotations and the AI
// tools price against.
// ---------------------------------------------------------------------------

export function listPriceLists() {
  return apiFetch<{ priceLists: PriceList[] }>("/api/price-lists");
}

export function createPriceList(input: { name: string; description?: string; columns?: PriceColumn[] }) {
  return apiFetch<PriceListDetailResponse>("/api/price-lists", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function getPriceList(id: string) {
  return apiFetch<PriceListDetailResponse>(`/api/price-lists/${encodeURIComponent(id)}`);
}

export function updatePriceList(id: string, input: { name: string; description?: string }) {
  return apiFetch<{ priceList: PriceList }>(`/api/price-lists/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function deletePriceList(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/price-lists/${encodeURIComponent(id)}`, { method: "DELETE" });
}

/** Replaces the column set: add, rename, retype, reorder, and delete in one call. */
export function savePriceColumns(id: string, columns: PriceColumn[]) {
  return apiFetch<{ columns: PriceColumn[] }>(`/api/price-lists/${encodeURIComponent(id)}/columns`, {
    method: "PUT",
    body: JSON.stringify({ columns }),
  });
}

export function listPriceRows(id: string, options: { q?: string; sort?: string; dir?: "asc" | "desc"; limit?: number; offset?: number } = {}) {
  const params = new URLSearchParams();
  if (options.q?.trim()) params.set("q", options.q.trim());
  if (options.sort) params.set("sort", options.sort);
  if (options.dir) params.set("dir", options.dir);
  params.set("limit", String(options.limit ?? 200));
  params.set("offset", String(options.offset ?? 0));
  return apiFetch<PriceRowsResponse>(`/api/price-lists/${encodeURIComponent(id)}/rows?${params.toString()}`);
}

/**
 * Creates one row and returns it with its server id. The grid appends a row
 * once, then addresses every later edit to that id.
 */
export function appendPriceRow(id: string, data: Record<string, string>) {
  return apiFetch<{ row: PriceRow }>(`/api/price-lists/${encodeURIComponent(id)}/rows/append`, {
    method: "POST",
    body: JSON.stringify({ data }),
  });
}

/**
 * Writes only the cells that changed. This is the autosave-on-blur call: one
 * row, one or two cells, so a large sheet never resends itself.
 */
export function updatePriceRowCells(id: string, rowId: string, cells: Record<string, string>) {
  return apiFetch<{ row?: PriceRow }>(`/api/price-lists/${encodeURIComponent(id)}/rows/${encodeURIComponent(rowId)}`, {
    method: "PATCH",
    body: JSON.stringify({ cells }),
  });
}

export function deletePriceRow(id: string, rowId: string) {
  return apiFetch<{ ok: boolean }>(`/api/price-lists/${encodeURIComponent(id)}/rows/${encodeURIComponent(rowId)}`, {
    method: "DELETE",
  });
}

export function savePriceRows(id: string, input: { rows: PriceRow[]; deletedIds?: string[] }) {
  return apiFetch<{ ok: boolean; saved: number }>(`/api/price-lists/${encodeURIComponent(id)}/rows`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function importPriceRows(id: string, input: {
  content: string;
  delimiter?: string;
  createColumns?: boolean;
  mode?: "append" | "upsert";
  dryRun?: boolean;
}) {
  return apiFetch<{ result: PriceImportResult }>(`/api/price-lists/${encodeURIComponent(id)}/import`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function priceListExportURL(id: string) {
  return `/api/price-lists/${encodeURIComponent(id)}/export`;
}

/**
 * Drafts live on the server so an unsent compose window follows the account to
 * another computer. `conversationId` narrows the list to the single draft a
 * conversation can hold.
 */
export function listDrafts(conversationId?: string) {
  const suffix = conversationId?.trim() ? `?conversation_id=${encodeURIComponent(conversationId.trim())}` : "";
  return apiFetch<ComposeDraftListResponse>(`/api/drafts${suffix}`);
}

export function saveDraft(input: ComposeDraftInput) {
  return apiFetch<ComposeDraftResponse>("/api/drafts", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteDraft(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/drafts/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function lookupPrices(input: { query?: string; queries?: string[]; limit?: number }) {
  return apiFetch<{ entries: PriceLookupEntry[] }>("/api/price-lists/lookup", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export type UserRole = "user" | "super_admin";

export function getCapabilities() {
  return apiFetch<{
    notifications: boolean;
    webPush: boolean;
    calendar: boolean;
    role: UserRole;
    currentUser?: { login: string; displayName: string; role: UserRole };
  }>("/api/capabilities");
}

export function updateAccountProfile(displayName: string) {
  return apiFetch<SystemUser>("/api/account/profile", {
    method: "PATCH",
    body: JSON.stringify({ displayName }),
  });
}

export function updateAccountPassword(currentPassword: string, newPassword: string, confirmation: string) {
  return apiFetch<{ ok: boolean }>("/api/account/password", {
    method: "PATCH",
    body: JSON.stringify({ currentPassword, newPassword, confirmation }),
  });
}

export type SystemUser = {
  id: string;
  login: string;
  displayName: string;
  role: UserRole;
  createdAt: string;
};

export type SystemSettings = {
  version: string;
  currentUserId: string;
  registrationOpen: boolean;
  users: SystemUser[];
};

export type PublicSettings = {
  registrationOpen: boolean;
};

export function getPublicSettings() {
  return apiFetch<PublicSettings>("/api/public/settings");
}

export function getSystemSettings() {
  return apiFetch<SystemSettings>("/api/system/settings");
}

export type UpdateStatus = {
  currentVersion: string;
  latestVersion?: string;
  updateAvailable: boolean;
  repositoryUrl: string;
  releaseUrl?: string;
  canAutoUpdate: boolean;
};

export function getUpdateInfo() {
  return apiFetch<UpdateStatus>("/api/update");
}

export function checkForUpdates() {
  return apiFetch<UpdateStatus>("/api/update/check", { method: "POST" });
}

export function installUpdate() {
  return apiFetch<{ ok: boolean; version: string; restarting: boolean }>("/api/system/update/install", { method: "POST" });
}

export function updateSystemUserRole(id: string, role: UserRole) {
  return apiFetch<SystemUser>(`/api/system/users/${encodeURIComponent(id)}/role`, {
    method: "PATCH",
    body: JSON.stringify({ role }),
  });
}

export function updateRegistrationOpen(registrationOpen: boolean) {
  return apiFetch<PublicSettings>("/api/system/settings/registration", {
    method: "PATCH",
    body: JSON.stringify({ registrationOpen }),
  });
}

export function getConversation(id: string, options: { limit?: number; cursor?: string; after?: string; through?: string; until?: string; around?: string; folder?: string; signal?: AbortSignal } = {}) {
  const params = new URLSearchParams();
  if (options.limit) params.set("limit", String(options.limit));
  if (options.cursor) params.set("cursor", options.cursor);
  if (options.through) params.set("through", options.through);
  if (options.until) params.set("until", options.until);
  if (options.after) params.set("after", options.after);
  if (options.around) params.set("around", options.around);
  if (options.folder) params.set("folder", options.folder);
  const suffix = params.size ? `?${params.toString()}` : "";
  return apiFetch<ConversationDetailResponse>(`/api/conversations/${encodeURIComponent(id)}${suffix}`, { signal: options.signal });
}

export function startConversation(accountEmail: string, to: string) {
  return apiFetch<ConversationDetailResponse>("/api/conversations", {
    method: "POST",
    body: JSON.stringify({ accountEmail, to }),
  });
}

export function saveConversationNote(id: string, note: string) {
  return apiFetch<{ ok: boolean; note: string }>(`/api/conversations/${encodeURIComponent(id)}/note`, {
    method: "PUT",
    body: JSON.stringify({ note }),
  });
}

export function saveConversationStatus(id: string, status: "answered" | "unanswered" | "no_action") {
	return apiFetch<{ ok: boolean; status: string }>(`/api/conversations/${encodeURIComponent(id)}/status`, {
		method: "PUT",
		body: JSON.stringify({ status }),
	});
}

export function getConversationAIChat(id: string) {
  return apiFetch<{ ok: boolean; messages: ConversationAIChatMessage[] }>(
    `/api/conversations/${encodeURIComponent(id)}/ai-chat`
  );
}

export function saveConversationAIChat(id: string, messages: ConversationAIChatMessage[]) {
  return apiFetch<{ ok: boolean }>(
    `/api/conversations/${encodeURIComponent(id)}/ai-chat`,
    {
      method: "PUT",
      body: JSON.stringify({ messages }),
    }
  );
}

export function deleteConversationAIChat(id: string) {
  return apiFetch<{ ok: boolean }>(
    `/api/conversations/${encodeURIComponent(id)}/ai-chat`,
    { method: "DELETE" }
  );
}

export function markConversationRead(id: string) {
  return apiFetch<{ ok: boolean; updated: number; sync: "pending" | "idle" }>(`/api/conversations/${encodeURIComponent(id)}/read`, { method: "PATCH" });
}

export function markConversationUnread(id: string) {
  return apiFetch<{ ok: boolean; updated: number; sync: "pending" | "idle" }>(`/api/conversations/${encodeURIComponent(id)}/unread`, { method: "PATCH" });
}

export function deleteConversation(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/conversations/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function deleteConversationMessage(conversationId: string, uid: string, folder: string) {
  return apiFetch<{ ok: boolean }>(`/api/conversations/${encodeURIComponent(conversationId)}/messages/${encodeURIComponent(uid)}`, {
    method: "DELETE",
    body: JSON.stringify({ folder }),
  });
}

export function resyncAttachments() {
  return apiFetch<{ ok: boolean; queued: boolean; account: string }>("/api/accounts/resync-attachments", { method: "POST" });
}

export type FeishuWebhookSettings = { enabled: boolean; url: string };

export function getFeishuWebhookSettings() {
  return apiFetch<FeishuWebhookSettings>("/api/settings/feishu-webhook");
}

export function saveFeishuWebhookSettings(settings: FeishuWebhookSettings) {
  return apiFetch<FeishuWebhookSettings>("/api/settings/feishu-webhook", {
    method: "PUT",
    body: JSON.stringify(settings),
  });
}

export function getAccountFeishuWebhookSettings(accountIdentifier: string) {
	return apiFetch<FeishuWebhookSettings>(`/api/accounts/${encodeURIComponent(accountIdentifier)}/feishu-webhook`);
}

export function saveAccountFeishuWebhookSettings(accountIdentifier: string, settings: FeishuWebhookSettings) {
	return apiFetch<FeishuWebhookSettings>(`/api/accounts/${encodeURIComponent(accountIdentifier)}/feishu-webhook`, { method: "PUT", body: JSON.stringify(settings) });
}

export function testFeishuWebhook(url: string) {
  return apiFetch<{ ok: boolean }>("/api/settings/feishu-webhook/test", {
    method: "POST",
    body: JSON.stringify({ url }),
  });
}

export type EmailSignature = {
  id: string;
  name: string;
  html: string;
  default?: boolean;
};

export function getSignatures() {
  return apiFetch<{ signatures: EmailSignature[] }>("/v1/settings/signatures");
}

export function saveSignatures(signatures: EmailSignature[]) {
  return apiFetch<{ signatures: EmailSignature[] }>("/v1/settings/signatures", {
    method: "PUT",
    body: JSON.stringify({ signatures }),
  });
}

export type ReplyTemplate = {
  id: string;
  name: string;
  body: string;
};

export type ReplyTemplatesResponse = {
  templates: ReplyTemplate[];
  configured?: boolean;
};

export function getReplyTemplates() {
  return apiFetch<ReplyTemplatesResponse>("/v1/settings/reply-templates");
}

export function saveReplyTemplates(templates: ReplyTemplate[]) {
  return apiFetch<ReplyTemplatesResponse>("/v1/settings/reply-templates", {
    method: "PUT",
    body: JSON.stringify({ templates }),
  });
}

export type AIModel = {
  id: string;
  provider: "openai" | "openai-compatible" | "gemini" | "deepseek";
  baseUrl: string;
  model: string;
  reasoningEffort: "low" | "medium" | "high";
  extraParams: string;
  isDefault: boolean;
};

export type AddAIModelInput = {
  provider?: "openai" | "openai-compatible" | "gemini" | "deepseek";
  baseUrl: string;
  model: string;
  apiKey: string;
  reasoningEffort: "low" | "medium" | "high";
  extraParams?: string;
};

export function getAIModels() {
  return apiFetch<{ models: AIModel[] }>("/api/settings/ai/models");
}

export function addAIModel(model: AddAIModelInput) {
  return apiFetch<AIModel>("/api/settings/ai/models", {
    method: "POST",
    body: JSON.stringify(model),
  });
}

export function updateAIModel(id: string, model: AddAIModelInput) {
  return apiFetch<AIModel>(`/api/settings/ai/models/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(model),
  });
}

export function testAIModel(model: AddAIModelInput) {
  return apiFetch<{ ok: boolean; output: string; latencyMs: number }>("/api/settings/ai/models/test", {
    method: "POST",
    body: JSON.stringify(model),
  });
}

export function testSavedAIModel(id: string, model: AddAIModelInput) {
  return apiFetch<{ ok: boolean; output: string; latencyMs: number }>(`/api/settings/ai/models/${encodeURIComponent(id)}/test`, {
    method: "POST",
    body: JSON.stringify(model),
  });
}

export function deleteAIModel(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/settings/ai/models/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function setDefaultAIModel(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/settings/ai/models/${encodeURIComponent(id)}/default`, { method: "POST" });
}

export type GenerateEmailInput = {
  accountEmail: string;
  taskType?: "email_draft" | "reply_suggestion";
  folder?: string;
  messageId?: string;
  instruction: string;
  subject: string;
  recipients: string;
  context?: string;
  draft?: string;
};

export function generateEmail(input: GenerateEmailInput) {
  return apiFetch<{ body: string; persisted?: boolean; updatedAt?: string }>("/api/ai/write-email", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export type DocumentVariable = {
  conceptId: string;
  label?: string;
  currentValue?: string;
};

export type GenerateDocumentInput = {
  accountEmail: string;
  mode?: "generate" | "rewrite" | "variables";
  documentType: "quotation" | "contract";
  title: string;
  instruction: string;
  currentHTML?: string;
  variables?: DocumentVariable[];
};

export type QuotationItem = {
  model: string;
  picture?: string;
  description: string;
  qty: string;
  price: string;
  amount: string;
};

export type GenerateDocumentResult = {
  mode?: "variables" | "rewrite" | "generate";
  html?: string;
  values?: Record<string, string>;
  items?: QuotationItem[];
};

export function generateDocument(input: GenerateDocumentInput) {
  return apiFetch<GenerateDocumentResult>("/api/ai/write-document", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export type AIToolCall = {
  id?: string;
  name: string;
  args?: Record<string, unknown>;
  result?: unknown;
  summary?: string;
  status?: "success" | "error";
};

export type AIAgentDocumentCard = {
  id?: string;
  type: "quotation" | "contract";
  title: string;
  counterparty: string;
  contact?: string;
  currency?: string;
  items?: QuotationItem[];
  terms?: string[];
  notes?: string;
  subtotal?: string;
  taxRate?: string;
  taxAmount?: string;
  total?: string;
  values?: Record<string, string>;
  html?: string;
};

export type AIAgentChatRequest = {
  accountEmail: string;
  agentId?: string;
  modelId?: string;
  messages: Array<{
    role: "user" | "assistant";
    content: string;
    toolCalls?: AIToolCall[];
    document?: AIAgentDocumentCard;
  }>;
  activeDocument?: {
    type: string;
    title: string;
    counterparty: string;
    items?: QuotationItem[];
    values?: Record<string, string>;
  };
};

/** A saved-document change the agent proposes and the user confirms. */
export type AIAgentDocumentAction = {
  kind: "update" | "delete";
  documentId: string;
  type?: "quotation" | "contract";
  name?: string;
  company?: string;
  values?: Record<string, string>;
  items?: QuotationItem[];
  summary?: string;
};

export type AIAgentChatResponse = {
  content: string;
  toolCalls: AIToolCall[];
  document?: AIAgentDocumentCard;
  documentAction?: AIAgentDocumentAction;
};

export function chatWithAIAgent(payload: AIAgentChatRequest) {
  return apiFetch<AIAgentChatResponse>("/api/ai/agent-chat", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export type MailSummaryResult = {
  summary: string;
  status: "ready";
  cached: boolean;
  stale: boolean;
  updatedAt: string;
};

export function summarizeMailMessage(accountEmail: string, folder: string, messageId: string, regenerate = false) {
  return apiFetch<MailSummaryResult>("/api/ai/mail-summary", {
    method: "POST",
    body: JSON.stringify({ accountEmail, folder, messageId, regenerate }),
  });
}

export type MailTranslationResult = {
  translation: string;
  status: "ready" | "generating" | "failed";
  cached: boolean;
  updatedAt: string;
};

export function translateMailMessage(accountEmail: string, folder: string, messageId: string, regenerate = false, all = false) {
  return apiFetch<MailTranslationResult>("/api/ai/mail-translation", {
    method: "POST",
    body: JSON.stringify({ accountEmail, folder, messageId, regenerate, all }),
  });
}

export type AIAgent = {
  id: string;
  name: string;
  prompt: string;
  outputLabels: string[];
};

export type AIAgentInput = {
  name: string;
  prompt: string;
  outputLabels: string[];
};

export function getAIAgents() {
  return apiFetch<{ agents: Array<Omit<AIAgent, "outputLabels"> & { outputLabels?: string[] }> }>("/api/settings/ai/agents").then((payload) => ({
    agents: payload.agents.map((agent) => ({
      ...agent,
      outputLabels: Array.isArray(agent.outputLabels) ? agent.outputLabels : ["客户", "需求", "要求", "问题"],
    })),
  }));
}

export function addAIAgent(agent: AIAgentInput) {
  return apiFetch<AIAgent>("/api/settings/ai/agents", {
    method: "POST",
    body: JSON.stringify(agent),
  });
}

export function updateAIAgent(id: string, agent: AIAgentInput) {
  return apiFetch<AIAgent>(`/api/settings/ai/agents/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(agent),
  });
}

export type AITaskBinding = {
  accountEmail: string;
  taskType: "mail_summary" | "email_draft" | "reply_suggestion";
  agentId: string;
  modelId: string;
  explicit: boolean;
  enabled: boolean;
};

export type AITaskBindingInput = Pick<AITaskBinding, "accountEmail" | "taskType" | "agentId" | "modelId"> & { enabled?: boolean };

export function getAITaskBindings() {
  return apiFetch<{ bindings: AITaskBinding[] }>("/api/settings/ai/task-bindings");
}

export function saveAITaskBinding(binding: AITaskBindingInput) {
  return apiFetch<{ ok: boolean }>("/api/settings/ai/task-bindings", {
    method: "PUT",
    body: JSON.stringify(binding),
  });
}

export type AIErrorLog = {
  id: string;
  taskType: string;
  accountEmail: string;
  modelName: string;
  agentName: string;
  errorMessage: string;
  createdAt: string;
};

export function getAIErrorLogs(limit = 100) {
  return apiFetch<{ logs: AIErrorLog[] }>(`/api/settings/ai/error-logs?limit=${encodeURIComponent(limit)}`);
}

export function clearAIErrorLogs() {
  return apiFetch<{ ok: boolean }>("/api/settings/ai/error-logs", {
    method: "DELETE",
  });
}

export function sendMessage(form: FormData) {
  return apiFetch<{ success: boolean; outboxId?: string; conversationId?: string }>("/api/compose", {
    method: "POST",
    body: form,
    signal: AbortSignal.timeout(3 * 60_000),
    // Sending requires the authenticated session and the CSRF cookie. Keep
    // this explicit at the compose boundary so a future request-wrapper
    // change cannot silently turn this into an unauthenticated request.
    credentials: "include",
  });
}

export function signIn(login: string, password: string) {
  const form = new FormData();
  form.set("login", login);
  form.set("password", password);
  return apiFetch<{ ok: boolean; next: string }>("/user-login", { method: "POST", body: form });
}

export function register(login: string, displayName: string, password: string, confirmation: string) {
  const form = new FormData();
  form.set("login", login);
  form.set("display_name", displayName);
  form.set("password", password);
  form.set("password_confirm", confirmation);
  return apiFetch<{ ok: boolean; next: string }>("/register", { method: "POST", body: form });
}

export function signOut() {
  return apiFetch<{ ok: boolean }>("/logout", { method: "POST" });
}

export function switchLanguage(locale: string) {
  window.location.assign(`/language?locale=${locale}&next=${encodeURIComponent("/inbox")}`);
}

export function getFolders() {
  return apiFetch<{ folders: Mailbox[] }>("/api/mail/folders");
}

export function getFolderMessages(folder: string) {
  return apiFetch<{ messages: MailMessage[]; syncComplete: boolean; syncError?: string }>(`/api/mail/messages?folder=${encodeURIComponent(folder)}&limit=100`);
}

export function getMessage(folder: string, id: string) {
  return apiFetch<MailMessage>(`/api/mail/messages/${encodeURIComponent(id)}?folder=${encodeURIComponent(folder)}`);
}

export function markMailMessageRead(folder: string, id: string, accountEmail?: string) {
  return apiFetch<{ ok: boolean; updated: number }>(mailMessageMutationPath(folder, id, accountEmail, "/read"), { method: "PATCH" });
}

export function getMailAttachments(query: string, type: string, offset = 0, limit = 100) {
  const params = new URLSearchParams({ offset: String(offset), limit: String(limit) });
  if (query.trim()) params.set("q", query.trim());
  if (type && type !== "all") params.set("type", type);
  return apiFetch<MailAttachmentListResponse>(`/api/attachments?${params.toString()}`);
}

function mailMessageMutationPath(folder: string, id: string, accountEmail?: string, suffix = "") {
  const query = new URLSearchParams({ folder });
  if (accountEmail) query.set("account_email", accountEmail);
  return `/api/mail/messages/${encodeURIComponent(id)}${suffix}?${query.toString()}`;
}

export function restoreJunkMessage(folder: string, id: string, accountEmail?: string) {
  return apiFetch<{ ok: boolean; folder: string }>(mailMessageMutationPath(folder, id, accountEmail, "/not-spam"), { method: "POST" });
}

export function permanentlyDeleteMessage(folder: string, id: string, accountEmail?: string) {
  return apiFetch<{ ok: boolean }>(mailMessageMutationPath(folder, id, accountEmail), { method: "DELETE" });
}

export const permanentlyDeleteJunkMessage = permanentlyDeleteMessage;

export function getAccounts() {
  return apiFetch<{ accounts: ConnectedAccount[] } | ConnectedAccount[]>("/api/accounts").then((payload) => ({
    accounts: Array.isArray(payload) ? payload.map(normalizeAccount) : payload.accounts.map(normalizeAccount),
  }));
}

function normalizeAccount(account: ConnectedAccount & { imap_server?: string; imap_port?: number; smtp_server?: string; smtp_port?: number }): ConnectedAccount {
  return {
    ...account,
    imapServer: account.imapServer || account.imap_server || "",
    imapPort: account.imapPort || account.imap_port,
    smtpServer: account.smtpServer || account.smtp_server,
    smtpPort: account.smtpPort || account.smtp_port,
  };
}

export type AddAccountInput = {
  email: string;
  password: string;
  label: string;
  color: string;
  imap_server: string;
  imap_port: number;
  smtp_server: string;
  smtp_port: number;
};

export function addAccount(account: AddAccountInput) {
  return apiFetch<{ ok?: boolean; id?: string; email: string; label: string }>("/api/accounts", { method: "POST", body: JSON.stringify(account) });
}

export function updateAccount(id: string, account: AddAccountInput) {
	return apiFetch<{ ok?: boolean; id: string; email: string; label: string }>(`/api/accounts/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(account) });
}

export function deleteAccount(id: string) {
	return apiFetch<{ ok?: boolean }>(`/api/accounts/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function switchAccount(id: string) {
	return apiFetch<{ ok?: boolean; next?: string }>(`/api/accounts/${encodeURIComponent(id)}/switch`, { method: "POST" });
}

export function getCalendarEvents(start: string, end: string) {
  return apiFetch<{ events: CalendarEvent[] }>(`/v1/calendar/events?start=${encodeURIComponent(start)}&end=${encodeURIComponent(end)}`);
}

export function createCalendarEvent(event: Omit<CalendarEvent, "uid">) {
  return apiFetch<{ created: boolean; uid: string }>("/v1/calendar/events", { method: "POST", body: JSON.stringify(event) });
}

export function getVapidPublicKey() {
  return apiFetch<{ publicKey: string }>("/api/push/vapid-public");
}

export function savePushSubscription(subscription: PushSubscriptionJSON) {
  return apiFetch<{ ok: boolean }>("/api/push/subscribe", {
    method: "POST",
    body: JSON.stringify(subscription),
  });
}

export function removePushSubscription(endpoint: string) {
  return apiFetch<{ ok: boolean }>("/api/push/subscribe", {
    method: "DELETE",
    body: JSON.stringify({ endpoint }),
  });
}

// ---------------------------------------------------------------------------
// Documents, Templates & Stamps Server APIs
// ---------------------------------------------------------------------------

export type ServerDocument = {
  id: string;
  type: "quotation" | "contract";
  name: string;
  company?: string;
  html: string;
  updatedAt: string;
  createdAt?: string;
};

export type ServerDocumentTemplate = ServerDocument;

export type ServerDocumentStamp = {
  id: string;
  name: string;
  dataUrl?: string;
  value?: string;
  width?: number;
  height?: number;
  insertWidth?: number;
  aspectRatio?: number;
  createdAt?: number;
};

export function listDocuments(type?: "all" | "quotation" | "contract") {
  const query = type && type !== "all" ? `?type=${encodeURIComponent(type)}` : "";
  return apiFetch<{ documents: ServerDocument[] }>(`/api/documents${query}`);
}

export function getDocument(id: string) {
  return apiFetch<{ document: ServerDocument }>(`/api/documents/${encodeURIComponent(id)}`);
}

export function saveDocument(document: {
  id?: string;
  type: "quotation" | "contract";
  name: string;
  company?: string;
  html: string;
}) {
  return apiFetch<{ ok: boolean; document: ServerDocument }>("/api/documents", {
    method: "POST",
    body: JSON.stringify(document),
  });
}

export function deleteDocument(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/documents/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function batchDeleteDocuments(ids: string[]) {
  return apiFetch<{ ok: boolean; count: number }>("/api/documents/batch-delete", {
    method: "POST",
    body: JSON.stringify({ ids }),
  });
}

export function listDocumentTemplates() {
  return apiFetch<{ templates: ServerDocumentTemplate[] }>("/api/document-templates");
}

export function saveDocumentTemplate(template: {
  id?: string;
  type: "quotation" | "contract";
  name: string;
  html: string;
}) {
  return apiFetch<{ ok: boolean; template: ServerDocumentTemplate }>("/api/document-templates", {
    method: "POST",
    body: JSON.stringify(template),
  });
}

export function deleteDocumentTemplate(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/document-templates/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function listDocumentStamps() {
  return apiFetch<{ stamps: ServerDocumentStamp[] }>("/api/document-stamps");
}

export function saveDocumentStamp(stamp: ServerDocumentStamp) {
  return apiFetch<{ ok: boolean; stamp: ServerDocumentStamp }>("/api/document-stamps", {
    method: "POST",
    body: JSON.stringify(stamp),
  });
}

export function deleteDocumentStamp(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/document-stamps/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}


export function retryOutgoing(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/outbox/${encodeURIComponent(id)}/retry`, { method: "POST" });
}
export function deleteOutgoing(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/outbox/${encodeURIComponent(id)}`, { method: "DELETE" });
}
export function importLegacyOutgoing(form: FormData) {
  return apiFetch<{ outboxId: string }>("/api/outbox/import", { method: "POST", body: form });
}
export function outgoingAttachmentURL(id: string) {
  const dot = id.lastIndexOf(".");
  return `/api/outbox/${encodeURIComponent(id.slice(0, dot))}/attachments/${encodeURIComponent(id.slice(dot + 1))}`;
}
