import { getCookie } from "./utils";
import type { CalendarEvent, ConnectedAccount, ConversationDetailResponse, ConversationListResponse, MailAttachmentListResponse, MailMessage, Mailbox } from "../types";

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function fetchCSRFToken() {
  const response = await fetch("/csrf", {
    headers: { Accept: "application/json" },
    credentials: "include",
  });
  if (!response.ok) throw new ApiError("Failed to initialize request security", response.status);
  const payload = await response.json() as { token?: string };
  return payload.token || getCookie("_csrf");
}

export async function apiFetch<T>(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const method = (init.method || "GET").toUpperCase();
  const mutates = method !== "GET" && method !== "HEAD" && method !== "OPTIONS";
  let csrf = getCookie("_csrf");
  if (!csrf && mutates) {
    csrf = await fetchCSRFToken();
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
    csrf = await fetchCSRFToken();
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

export function getConversations(query = "") {
  const suffix = query.trim() ? `?q=${encodeURIComponent(query.trim())}` : "";
  return apiFetch<ConversationListResponse>(`/api/conversations${suffix}`);
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

export function getConversation(id: string) {
  return apiFetch<ConversationDetailResponse>(`/api/conversations/${encodeURIComponent(id)}`);
}

export function saveConversationNote(id: string, note: string) {
  return apiFetch<{ ok: boolean; note: string }>(`/api/conversations/${encodeURIComponent(id)}/note`, {
    method: "PUT",
    body: JSON.stringify({ note }),
  });
}

export function markConversationRead(id: string) {
  return apiFetch<{ ok: boolean; updated: number }>(`/api/conversations/${encodeURIComponent(id)}/read`, { method: "PATCH" });
}

export function markConversationUnread(id: string) {
  return apiFetch<{ ok: boolean; updated: number }>(`/api/conversations/${encodeURIComponent(id)}/unread`, { method: "PATCH" });
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

export type AIModel = {
  id: string;
  provider: "openai";
  baseUrl: string;
  model: string;
  reasoningEffort: "low" | "medium";
  isDefault: boolean;
};

export type AddAIModelInput = {
  baseUrl: string;
  model: string;
  apiKey: string;
  reasoningEffort: "low" | "medium";
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
};

export type AITaskBindingInput = Pick<AITaskBinding, "accountEmail" | "taskType" | "agentId" | "modelId">;

export function getAITaskBindings() {
  return apiFetch<{ bindings: AITaskBinding[] }>("/api/settings/ai/task-bindings");
}

export function saveAITaskBinding(binding: AITaskBindingInput) {
  return apiFetch<AITaskBinding>("/api/settings/ai/task-bindings", {
    method: "PUT",
    body: JSON.stringify(binding),
  });
}

export function sendMessage(form: FormData) {
  return apiFetch<{ success: boolean; message: string }>("/api/compose", {
    method: "POST",
    body: form,
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

export function permanentlyDeleteJunkMessage(folder: string, id: string, accountEmail?: string) {
  return apiFetch<{ ok: boolean }>(mailMessageMutationPath(folder, id, accountEmail), { method: "DELETE" });
}

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

export function updateAccount(email: string, account: AddAccountInput) {
  return apiFetch<{ ok?: boolean; id?: string; email: string; label: string }>(`/api/accounts/${encodeURIComponent(email)}`, { method: "PUT", body: JSON.stringify(account) });
}

export function deleteAccount(email: string) {
  return apiFetch<{ ok?: boolean }>(`/api/accounts/${encodeURIComponent(email)}`, { method: "DELETE" });
}

export function switchAccount(email: string) {
  return apiFetch<{ ok?: boolean; next?: string }>(`/api/accounts/${encodeURIComponent(email)}/switch`, { method: "POST" });
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
