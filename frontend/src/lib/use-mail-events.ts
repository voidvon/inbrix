import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CLIENT_ID } from "./client-id";

type MailEventPayload = {
  type?: string;
  accountId?: string;
  conversationId?: string;
  folder?: string;
  messageId?: string;
  from?: string;
  subject?: string;
  origin?: string;
  revision?: number;
};

export type MailEventStatus = "connecting" | "open" | "closed";

// Query roots owned by the mail screens. Drafts and conversation AI chats are
// deliberately absent: they autosave frequently and are not part of the
// mailbox list, so they must not be invalidated on every event.
const MAIL_QUERY_ROOTS = new Set([
  "conversations",
  "conversation",
  "folder",
  "message",
  "attachments",
  "mail-search",
]);

function isMailQueryKey(queryKey: readonly unknown[]) {
  return typeof queryKey[0] === "string" && MAIL_QUERY_ROOTS.has(queryKey[0]);
}

function notifyNewMail(payload: MailEventPayload) {
  if (typeof window === "undefined" || typeof document === "undefined") return;
  if (!("Notification" in window) || Notification.permission !== "granted") return;
  if (document.visibilityState === "visible") return;
  try {
    new Notification(payload.from ? `New mail from ${payload.from}` : "New mail", { body: payload.subject || "" });
  } catch {
    // Native notifications are best-effort; the in-app list is authoritative.
  }
}

/**
 * Subscribes to /events and invalidates every mail query when the server says
 * something changed. This replaces periodic polling entirely: the first frame
 * is a resync, a reconnect also resyncs, and the server sends a heartbeat every
 * 15s to keep proxies from closing the stream.
 */
export function useMailEvents(enabled = true): MailEventStatus {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<MailEventStatus>("connecting");
  const pendingRef = useRef(false);
  const timerRef = useRef<number | null>(null);
  const failuresRef = useRef(0);

  useEffect(() => {
    if (!enabled) {
      setStatus("connecting");
      return;
    }
    let disposed = false;
    let source: EventSource | null = null;

    const invalidate = () => {
      if (pendingRef.current) return;
      pendingRef.current = true;
      timerRef.current = window.setTimeout(() => {
        pendingRef.current = false;
        timerRef.current = null;
        if (disposed) return;
        void queryClient.invalidateQueries({ predicate: (query) => isMailQueryKey(query.queryKey) });
      }, 150);
    };

    const cachedConversations = () => {
      // The list query key includes the open/all filter. Read the query that is
      // currently observed so a resync compares the right revision after a
      // filter switch.
      const query = queryClient
        .getQueryCache()
        .findAll({ queryKey: ["conversations"] })
        .find((candidate) => candidate.getObserversCount() > 0);
      return query?.state.data as { revision?: number } | undefined;
    };

    source = new EventSource("/events", { withCredentials: true });
    source.onopen = () => {
      failuresRef.current = 0;
      setStatus("open");
      // No refetch here: the resync frame carries the server revision, and we
      // compare it with the cached revision below. Equal revision => zero
      // requests on reconnect.
    };
    source.onmessage = (event) => {
      let payload: MailEventPayload = {};
      try {
        payload = JSON.parse(String(event.data)) as MailEventPayload;
      } catch {
        // Ignore malformed frames; invalidating is still safe.
      }
      if (payload.type === "resync") {
        const cached = cachedConversations();
        // A fresh page has no cached data yet; the initial query is already
        // fetching, so the resync must not trigger a duplicate request.
        if (!cached) return;
        // Older servers do not send a revision; fall back to refetching.
        if (payload.revision === undefined) {
          invalidate();
          return;
        }
        if (cached.revision !== undefined && payload.revision === cached.revision) return;
        invalidate();
        return;
      }
      if (payload.type === "mail.new") notifyNewMail(payload);
      // The tab that caused an HTTP mutation already invalidated locally; its
      // own echo would otherwise trigger a second full conversation rebuild.
      if (payload.origin && payload.origin === CLIENT_ID) return;
      invalidate();
    };
    source.onerror = () => {
      setStatus("closed");
      failuresRef.current += 1;
      // EventSource reconnects automatically. After a few failures, refetch
      // once so an expired session surfaces as a 401 instead of a silent
      // reconnect loop. This is recovery, not a polling interval.
      if (failuresRef.current === 3) invalidate();
    };

    return () => {
      disposed = true;
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      pendingRef.current = false;
      source?.close();
    };
  }, [enabled, queryClient]);

  return status;
}
