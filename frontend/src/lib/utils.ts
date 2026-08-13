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

type DateValue = string | number | Date;

const DAY_IN_MILLISECONDS = 24 * 60 * 60 * 1000;
const WEEKDAY_LABELS = ["星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"];

function padDatePart(value: number) {
  return String(value).padStart(2, "0");
}

function toDate(value: DateValue) {
  const date = value instanceof Date ? new Date(value.getTime()) : new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

function localDateKey(date: Date) {
  return Date.UTC(date.getFullYear(), date.getMonth(), date.getDate());
}

function formatClock(date: Date) {
  return `${padDatePart(date.getHours())}:${padDatePart(date.getMinutes())}`;
}

function formatMonthDay(date: Date) {
  return `${padDatePart(date.getMonth() + 1)}月${padDatePart(date.getDate())}日`;
}

export function formatTime(value: DateValue, now = new Date()) {
  const date = toDate(value);
  if (!date || Number.isNaN(now.getTime())) return "";

  const dateKey = localDateKey(date);
  const todayKey = localDateKey(now);
  const dayBeforeThisWeek = todayKey - ((now.getDay() + 6) % 7) * DAY_IN_MILLISECONDS;
  const dayDifference = Math.round((todayKey - dateKey) / DAY_IN_MILLISECONDS);
  const clock = formatClock(date);

  if (dayDifference === 0) return clock;
  if (dayDifference === 1) return `昨天 ${clock}`;
  if (dayDifference === 2) return `前天 ${clock}`;
  if (dateKey >= dayBeforeThisWeek && dateKey < todayKey - 2 * DAY_IN_MILLISECONDS) {
    return `${WEEKDAY_LABELS[date.getDay()]} ${clock}`;
  }

  const yearDifference = now.getFullYear() - date.getFullYear();
  if (yearDifference === 0) return `${formatMonthDay(date)} ${clock}`;
  if (yearDifference === 1) return `去年 ${formatMonthDay(date)}`;
  if (yearDifference === 2) return `前年 ${formatMonthDay(date)}`;
  return `${date.getFullYear()}年${formatMonthDay(date)}`;
}

// Keep the previous helpers as compatibility aliases for existing callers.
export function formatMailDate(value: string, locale?: string) {
  void locale;
  return formatTime(value);
}

export function formatFullDate(value: string, locale?: string) {
  void locale;
  return formatTime(value);
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
