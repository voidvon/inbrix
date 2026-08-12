import { getCookie } from "./utils";
import type { ConversationDetailResponse, ConversationListResponse } from "../types";

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function apiFetch<T>(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  const csrf = getCookie("_csrf");
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

export function getConversation(id: string) {
  return apiFetch<ConversationDetailResponse>(`/api/conversations/${encodeURIComponent(id)}`);
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
  return apiFetch<string>("/user-login", { method: "POST", body: form });
}

export function signOut() {
  return apiFetch<{ ok: boolean }>("/logout", { method: "POST" });
}

export function switchLanguage(locale: string) {
  window.location.assign(`/language?locale=${locale}&next=${encodeURIComponent("/inbox")}`);
}
