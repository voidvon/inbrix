import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function getCookie(name: string) {
  const prefix = `${name}=`;
  const item = document.cookie.split(";").map((part) => part.trim()).find((part) => part.startsWith(prefix));
  return item ? decodeURIComponent(item.slice(prefix.length)) : "";
}

export function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB"];
  let value = bytes;
  let unit = -1;
  do {
    value /= 1024;
    unit += 1;
  } while (value >= 1024 && unit < units.length - 1);
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}`;
}

export function initials(name?: string, fallback?: string) {
  const value = (name || fallback || "?").trim();
  return Array.from(value)[0]?.toUpperCase() || "?";
}

export function formatMailDate(value: string, locale: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(locale === "zh-CN" ? "zh-CN" : "en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function formatFullDate(value: string, locale: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(locale === "zh-CN" ? "zh-CN" : "en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function isSentMailbox(mailbox: MailboxLike) {
  const name = mailbox.name.trim().toLowerCase();
  return ["sent", "sent items", "sent mail", "sent messages"].includes(name) ||
    name.endsWith("/sent") || name.endsWith("/sent messages") ||
    mailbox.attributes.some((attribute) => attribute.trim().toLowerCase() === "\\sent");
}

type MailboxLike = { name: string; attributes: string[] };

export function linkifyText(text: string) {
  const pattern = /(https?:\/\/[^\s<>]+|www\.[^\s<>]+|[\w.%+-]+@[\w.-]+\.[A-Za-z]{2,})/g;
  const parts: (string | { value: string; href: string })[] = [];
  let lastIndex = 0;
  for (const match of text.matchAll(pattern)) {
    const value = match[0];
    const index = match.index ?? 0;
    if (index > lastIndex) parts.push(text.slice(lastIndex, index));
    const trailing = value.match(/[.,;:!?)]+$/)?.[0] || "";
    const clean = trailing ? value.slice(0, -trailing.length) : value;
    const href = clean.includes("@") && !clean.includes("/")
      ? `mailto:${clean}`
      : clean.startsWith("www.") ? `https://${clean}` : clean;
    parts.push({ value: clean, href });
    if (trailing) parts.push(trailing);
    lastIndex = index + value.length;
  }
  if (lastIndex < text.length) parts.push(text.slice(lastIndex));
  return parts;
}
