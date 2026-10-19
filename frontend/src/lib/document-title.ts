/** App name that always closes the browser tab title. */
export const APP_TITLE_NAME = "Inbrix AI";

/**
 * Builds the browser tab title the way mail clients do it:
 *
 *   Inbox (11) - me@example.com - Inbrix AI
 *
 * The unread count is only shown when there is unread mail, so a quiet mailbox
 * keeps a clean title. Missing pieces are skipped rather than leaving a
 * dangling separator.
 */
export function mailDocumentTitle(label: string, unread: number, accountEmail?: string) {
  const parts: string[] = [];
  const trimmedLabel = label.trim();
  if (trimmedLabel) {
    parts.push(unread > 0 ? `${trimmedLabel} (${unread})` : trimmedLabel);
  }
  const trimmedAccount = (accountEmail || "").trim();
  if (trimmedAccount) parts.push(trimmedAccount);
  parts.push(APP_TITLE_NAME);
  return parts.join(" - ");
}
