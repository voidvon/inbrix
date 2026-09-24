import type { ReactNode } from "react";
import React from "react";
import type { Editor } from "@tiptap/react";
import { linkifyText, splitQuotedText } from "./utils";
import type { ConversationMessage } from "../types";
import type { EmailSignature } from "./api";

export const MAX_COMPOSE_ATTACHMENT_BYTES = 18 * 1024 * 1024;
export const DEFAULT_INLINE_IMAGE_MAX_SIZE = 480;
export const SUPPORTED_INLINE_IMAGE_TYPES = new Set(["image/jpeg", "image/png", "image/gif"]);

export type InlineComposeImage = {
  contentId: string;
  file: File;
  previewURL: string;
};

export type ComposeDefaults = {
  accountEmail?: string;
  to: string;
  cc?: string;
  subject: string;
  html?: string;
  inReplyTo?: string;
  references?: string[];
  conversation?: ConversationMessage[];
  conversationId?: string;
};

export function inlineImageDimensions(previewURL: string) {
  return new Promise<{ width: number; height: number }>((resolve, reject) => {
    const image = new Image();
    image.onload = () => {
      if (!image.naturalWidth || !image.naturalHeight) {
        reject(new Error("Image has no dimensions"));
        return;
      }
      const scale = Math.min(1, DEFAULT_INLINE_IMAGE_MAX_SIZE / Math.max(image.naturalWidth, image.naturalHeight));
      resolve({
        width: Math.max(1, Math.round(image.naturalWidth * scale)),
        height: Math.max(1, Math.round(image.naturalHeight * scale)),
      });
    };
    image.onerror = () => reject(new Error("Image could not be decoded"));
    image.src = previewURL;
  });
}

export function extractEmailAddress(value?: string) {
  if (!value) return "";
  const trimmed = value.trim();
  if (!trimmed) return "";
  const parseSingle = (item: string) => {
    const s = item.trim();
    const angleMatch = s.match(/<([^<>]+)>\s*$/);
    if (angleMatch?.[1]) return angleMatch[1].trim();
    const emailMatch = s.match(/[^\s<]+@[^\s>]+/);
    return emailMatch ? emailMatch[0].replace(/[;,]+$/, "").trim() : "";
  };
  if (trimmed.includes(",")) {
    return trimmed
      .split(",")
      .map(parseSingle)
      .filter(Boolean)
      .join(", ");
  }
  return parseSingle(trimmed);
}

export async function copyToClipboard(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fallback to execCommand below
    }
  }
  try {
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.select();
    const success = document.execCommand("copy");
    document.body.removeChild(textarea);
    return success;
  } catch {
    return false;
  }
}

export function renderLinkifiedText(text: string): ReactNode[] {
  return linkifyText(text).map((part, index) =>
    typeof part === "string" ? (
      <span key={index}>{part}</span>
    ) : (
      <a
        className="text-foreground underline underline-offset-3"
        key={index}
        href={part.href}
        target={part.href.startsWith("mailto:") ? undefined : "_blank"}
        rel="noreferrer"
      >
        {part.value}
      </a>
    ),
  );
}

export function escapeHTML(value: string) {
  return value.replace(
    /[&<>"']/g,
    (character) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[character] || character,
  );
}

export function quoteAttribution(value: string) {
  return value.replace(/\s+/g, " ").trim().replace(/:$/, "") || "Quoted message";
}

export function quotedTextToHTML(text: string) {
  const root = document.createElement("div");
  const containers: HTMLElement[] = [root];
  let currentDepth = 0;
  for (const rawLine of text.replace(/\r\n?/g, "\n").split("\n")) {
    const match = rawLine.match(/^\s*(>+)\s?(.*)$/);
    const depth = match ? match[1].length : 0;
    const value = match ? match[2] : rawLine;
    while (currentDepth < depth) {
      const quote = document.createElement("blockquote");
      containers[currentDepth].appendChild(quote);
      containers.push(quote);
      currentDepth += 1;
    }
    while (currentDepth > depth) {
      containers.pop();
      currentDepth -= 1;
    }
    const line = document.createElement("div");
    if (value) line.textContent = value;
    else line.appendChild(document.createElement("br"));
    containers[currentDepth].appendChild(line);
  }
  return root.innerHTML;
}

export function structuredQuotedTextToHTML(text: string): string {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  const attribution = /^\s*(?:on\s+.+\bwrote\s*:|(?:在|於)\s*.+(?:写道|寫道)\s*[:：])\s*$/i;
  const original = /^\s*[-_]{2,}\s*(?:original(?:\s+message)?|原始邮件|原始郵件)\s*[-_]{2,}\s*$/i;
  const boundary = lines.findIndex((line, index) => index > 0 && (attribution.test(line) || original.test(line.replace(/\u00a0/g, " "))));
  if (boundary < 0) return quotedTextToHTML(text);
  const current = quotedTextToHTML(lines.slice(0, boundary).join("\n"));
  const lead = lines[boundary].trim();
  const tailLines = lines.slice(boundary + 1);
  const nonEmptyTail = tailLines.filter((line) => line.trim());
  const tail = nonEmptyTail.length > 0 && nonEmptyTail.every((line) => /^\s*>/.test(line))
    ? tailLines.map((line) => line.replace(/^\s*>\s?/, "")).join("\n")
    : tailLines.join("\n");
  return `${current}<p>${escapeHTML(lead)}</p><blockquote>${structuredQuotedTextToHTML(tail)}</blockquote>`;
}

export function normalizeQuoteHTML(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  const quoteSelector = "blockquote, includetail, .gmail_quote, .yahoo_quoted, .protonmail_quote, .outlook_quote, .quoted-text, .quotedcontent, .original-message";
  Array.from(document.body.querySelectorAll(quoteSelector)).reverse().forEach((element) => {
    if (element.closest("[data-inbrix-signature]")) return;
    const tag = element.tagName.toLowerCase();
    if (tag !== "blockquote" && tag !== "includetail" && element.querySelector(quoteSelector)) return;
    let attribution = element.getAttribute("data-attribution") || "";
    const previous = element.previousElementSibling;
    if (!attribution && previous && /(?:\bwrote\s*:|写道\s*[:：]|寫道\s*[:：]|original)/i.test(previous.textContent || "")) {
      attribution = previous.textContent || "";
      previous.remove();
    }
    const wrapper = document.createElement("div");
    wrapper.dataset.inbrixReplyQuote = "";
    wrapper.dataset.attribution = quoteAttribution(attribution);
    while (element.firstChild) wrapper.appendChild(element.firstChild);
    element.replaceWith(wrapper);
  });
  const children = Array.from(document.body.children);
  const separatorIndex = children.findIndex((element) => /^[-_\s]*original(?:\s+message)?[-_\s]*$/i.test((element.textContent || "").replace(/\u00a0/g, " ").trim()));
  if (separatorIndex >= 0) {
    const wrapper = document.createElement("div");
    wrapper.dataset.inbrixReplyQuote = "";
    wrapper.dataset.attribution = quoteAttribution(children[separatorIndex].textContent || "Original message");
    children.slice(separatorIndex + 1).forEach((element) => wrapper.appendChild(element));
    children[separatorIndex].replaceWith(wrapper);
  }
  return document.body.innerHTML;
}

export function serializeEmailHTML(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  document.body.querySelectorAll("p:empty").forEach((paragraph) => paragraph.appendChild(document.createElement("br")));
  return document.body.innerHTML;
}

export function serializeQuoteHTML(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  Array.from(document.body.querySelectorAll("[data-inbrix-reply-quote]")).reverse().forEach((element) => {
    const attribution = element.getAttribute("data-attribution") || "Quoted message";
    const paragraph = document.createElement("p");
    paragraph.textContent = attribution.endsWith(":") ? attribution : `${attribution}:`;
    const blockquote = document.createElement("blockquote");
    while (element.firstChild) blockquote.appendChild(element.firstChild);
    element.before(paragraph);
    element.replaceWith(blockquote);
  });
  return serializeEmailHTML(document.body.innerHTML);
}

export function serializeInlineImageReferences(html: string, removeMetadata: boolean) {
  const document = new DOMParser().parseFromString(html, "text/html");
  document.body.querySelectorAll<HTMLImageElement>("img[data-inline-image-id]").forEach((image) => {
    const contentId = image.dataset.inlineImageId;
    if (contentId) image.setAttribute("src", `cid:${contentId}`);
    if (removeMetadata) image.removeAttribute("data-inline-image-id");
  });
  return document.body.innerHTML;
}

export function restoreInlineImagePreviews(html: string, inlineImages: InlineComposeImage[]) {
  const document = new DOMParser().parseFromString(html, "text/html");
  const previews = new Map(inlineImages.map((image) => [image.contentId, image.previewURL]));
  document.body.querySelectorAll<HTMLImageElement>("img[data-inline-image-id]").forEach((image) => {
    const previewURL = previews.get(image.dataset.inlineImageId || "");
    if (previewURL) image.setAttribute("src", previewURL);
  });
  return document.body.innerHTML;
}

export function serializeComposeHTML(html: string) {
  const document = new DOMParser().parseFromString(serializeInlineImageReferences(serializeQuoteHTML(html), true), "text/html");
  Array.from(document.body.querySelectorAll("[data-inbrix-signature]")).forEach((element) => {
    element.replaceWith(...Array.from(element.childNodes));
  });
  return document.body.innerHTML;
}

export function signatureNodeHTML(signature: EmailSignature) {
  return `<div data-inbrix-signature="${escapeHTML(signature.id)}" data-signature-name="${escapeHTML(signature.name)}">${signature.html.trim() || "<p></p>"}</div>`;
}

export function setEditorSignature(editor: Editor, signature: EmailSignature | null) {
  let existingRange: { from: number; to: number } | null = null;
  let quotePosition: number | null = null;
  editor.state.doc.descendants((node, position) => {
    if (!existingRange && node.type.name === "emailSignature") {
      existingRange = { from: position, to: position + node.nodeSize };
      return false;
    }
    if (quotePosition === null && node.type.name === "replyQuote") {
      quotePosition = position;
      return false;
    }
    return true;
  });

  if (existingRange) {
    if (signature) editor.commands.insertContentAt(existingRange, signatureNodeHTML(signature));
    else editor.commands.deleteRange(existingRange);
    return;
  }
  if (signature) editor.commands.insertContentAt(quotePosition ?? editor.state.doc.content.size, signatureNodeHTML(signature));
}

export function aiConversationContext(messages: ConversationMessage[]) {
  return messages.map((message, index) => {
    const summary = message.mailSummary?.status === "ready" ? message.mailSummary.text.trim() : "";
    const body = splitQuotedText(message.body || message.preview || "").visible.trim() || (message.body || message.preview || "").trim();
    const content = summary ? `AI summary:\n${summary}` : `Message body:\n${body}`;
    return [
      `Message ${index + 1}`,
      `Date: ${message.date}`,
      `From: ${message.fromName ? `${message.fromName} <${message.from}>` : message.from}`,
      `To: ${message.to}`,
      message.cc ? `Cc: ${message.cc}` : "",
      `Subject: ${message.subject}`,
      content,
    ].filter(Boolean).join("\n");
  }).join("\n\n---\n\n");
}

export function generatedEmailHTML(body: string) {
  return body.replace(/\r\n?/g, "\n").split(/\n{2,}/).map((paragraph) => {
    const content = paragraph.split("\n").map((line) => escapeHTML(line)).join("<br>");
    return `<p>${content || "<br>"}</p>`;
  }).join("");
}

export function replaceEditorDraft(editor: Editor, body: string) {
  let protectedPosition: number | null = null;
  editor.state.doc.descendants((node, position) => {
    if (protectedPosition === null && (node.type.name === "emailSignature" || node.type.name === "replyQuote")) {
      protectedPosition = position;
      return false;
    }
    return protectedPosition === null;
  });
  const html = generatedEmailHTML(body);
  if (protectedPosition === null) {
    editor.commands.setContent(html);
  } else {
    editor.chain().deleteRange({ from: 0, to: protectedPosition }).insertContentAt(0, html).run();
  }
  editor.commands.focus("start");
}

export function htmlToPlainText(html: string) {
  const body = new DOMParser().parseFromString(html, "text/html").body;
  const lines: { depth: number; text: string }[] = [{ depth: 0, text: "" }];
  const newLine = (depth: number, preserveEmpty = false) => {
    if (!lines.at(-1)?.text) {
      lines[lines.length - 1].depth = depth;
      if (preserveEmpty) lines.push({ depth, text: "" });
    }
    else lines.push({ depth, text: "" });
  };
  const appendText = (value: string, depth: number) => {
    value.split("\n").forEach((part, index) => {
      if (index > 0) lines.push({ depth, text: "" });
      const line = lines[lines.length - 1];
      if (!line.text) line.depth = depth;
      line.text += part;
    });
  };
  const walk = (node: Node, depth: number) => {
    if (node.nodeType === Node.TEXT_NODE) {
      appendText(node.textContent || "", depth);
      return;
    }
    if (!(node instanceof HTMLElement)) return;
    if (node.tagName === "BR") {
      lines.push({ depth, text: "" });
      return;
    }
    if (node.tagName === "IMG") {
      appendText(`[Image: ${node.getAttribute("alt")?.trim() || "Image"}]`, depth);
      return;
    }
    const childDepth = node.tagName === "BLOCKQUOTE" ? depth + 1 : depth;
    if (node.tagName === "BLOCKQUOTE") newLine(childDepth);
    Array.from(node.childNodes).forEach((child) => walk(child, childDepth));
    if (["P", "DIV", "LI", "BLOCKQUOTE"].includes(node.tagName)) {
      const hasVisibleContent = Boolean((node.textContent || "").trim() || node.querySelector("br, img, hr"));
      newLine(depth, !hasVisibleContent);
    }
  };
  Array.from(body.childNodes).forEach((node) => walk(node, 0));
  const result = lines
    .map(({ depth, text }) => `${depth ? `${">".repeat(depth)} ` : ""}${text.trimEnd()}`.trimEnd())
    .join("\n")
    .trimEnd();
  return result.trim() ? result : "";
}

export function splitRecipientValues(value: string) {
  return value.split(/[,;\n]+/).map((item) => item.trim()).filter(Boolean);
}

export function recipientAddress(value: string) {
  const match = value.trim().match(/<([^<>]+)>\s*$/);
  return (match?.[1] || value).trim().toLowerCase();
}

export function uniqueRecipients(values: string[], excluded: string[] = []) {
  const seen = new Set(excluded.map(recipientAddress).filter(Boolean));
  return values.filter((value) => {
    const address = recipientAddress(value);
    if (!address || seen.has(address)) return false;
    seen.add(address);
    return true;
  });
}

export function isValidRecipient(value: string) {
  const match = value.trim().match(/^(?:[^<>]*<)?([^\s<>@,;]+@[^\s<>@,;]+\.[^\s<>@,;]+)>?$/);
  return Boolean(match);
}

export function canReplyAll(message?: { to?: string; cc?: string } | null) {
  if (!message) return false;
  if (message.cc?.trim()) return true;
  return splitRecipientValues(message.to || "").length > 1;
}

