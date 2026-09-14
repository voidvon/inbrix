import type { Copy } from "./locale";
import type { MailMessage, Mailbox } from "../types";

export function folderKind(folder: Mailbox) {
  const name = folder.name.toLowerCase();
  const attributes = folder.attributes.map((attribute) => attribute.trim().toLowerCase());
  if (attributes.includes("\\inbox") || name === "inbox") return "inbox";
  if (attributes.includes("\\sent") || ["sent", "sent items", "sent mail", "sent messages"].includes(name)) return "sent";
  if (attributes.includes("\\drafts") || name === "draft" || name === "drafts") return "drafts";
  if (attributes.includes("\\trash") || ["trash", "deleted", "deleted items", "deleted messages", "bin"].includes(name)) return "trash";
  if (attributes.includes("\\junk") || ["junk", "junk mail", "junk email", "junk e-mail", "spam", "bulk mail"].includes(name)) return "junk";
  if (attributes.includes("\\archive") || name === "archive" || name === "archives") return "archive";
  return "custom";
}

export function folderLabel(copy: Copy, folder: Mailbox) {
  const labels = {
    inbox: copy.inboxFolder,
    sent: copy.sentFolder,
    drafts: copy.draftsFolder,
    trash: copy.trashFolder,
    junk: copy.junkFolder,
    archive: copy.archiveFolder,
    custom: folder.name,
  };
  return labels[folderKind(folder)];
}

export function messageIsUnread(message: Pick<MailMessage, "flags">) {
  return !message.flags?.some((flag) => flag.toLowerCase() === "\\seen");
}

export function flagsMarkedSeen(flags: string[] = []) {
  return flags.some((flag) => flag.toLowerCase() === "\\seen") ? flags : [...flags, "\\Seen"];
}

export function flagsMarkedUnread(flags: string[] = []) {
  return flags.filter((flag) => flag.toLowerCase() !== "\\seen");
}
