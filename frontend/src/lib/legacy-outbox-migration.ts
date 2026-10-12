import type { ConversationMessage } from "../types";
import { importLegacyOutgoing } from "./api";

// Compatibility only: upload old browser-only records, then remove each one
// after the server acknowledges it. New messages never enter browser storage.
const key = "inbrix-optimistic-messages";
type LegacyMessage = ConversationMessage & { conversationId: string };
let active: Promise<boolean> | undefined;

function readLegacy(): LegacyMessage[] {
  const value: unknown = JSON.parse(localStorage.getItem(key) || "[]");
  return Array.isArray(value) ? value as LegacyMessage[] : [];
}

function removeLegacy(id: string) {
  const remaining = readLegacy().filter((message) => message.id !== id);
  if (remaining.length) localStorage.setItem(key, JSON.stringify(remaining));
  else localStorage.removeItem(key);
}

export function migrateLegacyOutgoing(accountEmails: string[]): Promise<boolean> {
  if (active) return active;
  active = migrate(accountEmails).finally(() => { active = undefined; });
  return active;
}

async function migrate(accountEmails: string[]) {
  let messages: LegacyMessage[];
  try { messages = readLegacy(); } catch { return false; }
  const owned = new Set(accountEmails.map((email) => email.trim().toLowerCase()));
  let changed = false;
  for (const message of messages) {
    if (!owned.has(message.from?.trim().toLowerCase()) || !message.id || !message.conversationId) continue;
    if (message.sendStatus === "sent") { removeLegacy(message.id); continue; }
    const form = new FormData();
    form.set("request_id", `legacy:${message.id}`);
    form.set("conversation_id", message.conversationId);
    form.set("account_email", message.from);
    form.set("to", message.to);
    form.set("subject", message.subject);
    form.set("body", message.body || message.preview || "");
    if (message.html) form.set("html_body", message.html);
    if (message.cc) form.set("cc", message.cc);
    if (message.inReplyTo) form.set("in_reply_to", message.inReplyTo);
    if (message.references?.length) form.set("references", message.references.join(" "));
    form.set("original_date", message.date);
    form.set("original_status", message.sendStatus || "unknown");
    if (message.hasAttachments || message.attachments?.length || /\b(?:cid|blob):/i.test(message.html || "")) {
      form.set("missing_attachments", "true");
      form.set("attachment_metadata", JSON.stringify(message.attachments || []));
    }
    await importLegacyOutgoing(form);
    removeLegacy(message.id);
    changed = true;
  }
  return changed;
}
