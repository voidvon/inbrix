import { getCookie } from "./utils";
import type { CalendarEvent, ConnectedAccount, ConversationDetailResponse, ConversationListResponse, MailMessage, Mailbox } from "../types";

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export async function apiFetch<T>(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const method = (init.method || "GET").toUpperCase();
  let csrf = getCookie("_csrf");
  if (!csrf && method !== "GET" && method !== "HEAD" && method !== "OPTIONS") {
    const response = await fetch("/csrf", {
      headers: { Accept: "application/json" },
      credentials: "include",
    });
    if (!response.ok) throw new ApiError("Failed to initialize request security", response.status);
    const payload = await response.json() as { token?: string };
    csrf = payload.token || getCookie("_csrf");
  }
  if (csrf) headers.set("X-CSRF-Token", csrf);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "include",
  });
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

export function getCapabilities() {
  return apiFetch<{ notifications: boolean; webPush: boolean; calendar: boolean }>("/api/capabilities");
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

export function deleteConversationMessage(conversationId: string, uid: string, folder: string) {
  return apiFetch<{ ok: boolean }>(`/api/conversations/${encodeURIComponent(conversationId)}/messages/${encodeURIComponent(uid)}`, {
    method: "DELETE",
    body: JSON.stringify({ folder }),
  });
}

export function resyncAttachments() {
  return apiFetch<{ ok: boolean; queued: boolean; account: string }>("/api/accounts/resync-attachments", { method: "POST" });
}

export function sendMessage(form: FormData) {
  return apiFetch<{ success: boolean; message: string }>("/api/compose", {
    method: "POST",
    body: form,
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
  return apiFetch<{ folders: Mailbox[] }>("/v1/folders");
}

export function getFolderMessages(folder: string) {
  return apiFetch<{ messages: MailMessage[] }>(`/v1/messages?folder=${encodeURIComponent(folder)}&limit=100`);
}

export function getMessage(folder: string, id: string) {
  return apiFetch<MailMessage>(`/v1/messages/${encodeURIComponent(id)}?folder=${encodeURIComponent(folder)}`);
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
