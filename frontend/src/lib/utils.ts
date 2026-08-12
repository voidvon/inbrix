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

export type SplitQuotedTextResult = {
  visible: string;
  quoted: string;
};

// Email clients use several plain-text conventions for the previous message.
// Keep the heuristic intentionally line-oriented: it preserves normal prose
// and moves only the trailing quoted section behind a native <details> block.
export function splitQuotedText(text: string): SplitQuotedTextResult {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  const quoteLine = /^\s*>+\s?/;
  const attributionLine = /^\s*(?:on\s+.+\bwrote\s*:|(?:在|於)\s*.+(?:写道|寫道)\s*[:：])\s*$/i;
  const originalMarker = /^\s*(?:[-_]{2,}\s*)?(?:original(?:\s+message)?|forwarded\s+message|原始邮件|原始郵件|转发邮件|轉發郵件)\s*[-_:：]?\s*[-_]*\s*$/i;

  let boundary = -1;
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (originalMarker.test(line) || attributionLine.test(line) || quoteLine.test(line)) {
      boundary = index;
      break;
    }
  }

  if (boundary < 0) return { visible: text, quoted: "" };
  return {
    visible: lines.slice(0, boundary).join("\n").trimEnd(),
    quoted: lines.slice(boundary).join("\n").trim(),
  };
}
