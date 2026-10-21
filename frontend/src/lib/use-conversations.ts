import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { getConversations } from "./api";
import type { ConversationListResponse } from "../types";

export const CONVERSATION_PAGE_SIZE = 30;

export type ConversationFilter = "open" | "all";

export function conversationsQueryKey(filter: ConversationFilter) {
  return ["conversations", filter] as const;
}

/**
 * Refresh only the head page and keep the tail the user already loaded. A head
 * row that also exists in the tail wins and is removed from the tail, which is
 * how a conversation that just received a reply moves to the top without
 * appearing twice.
 */
function mergeHead(
  previous: ConversationListResponse | undefined,
  head: ConversationListResponse,
  filter: ConversationFilter,
): ConversationListResponse {
  if (!previous) return head;
  const visible = (conversation: ConversationListResponse["conversations"][number]) =>
    filter === "all" || conversation.status === "unanswered" || conversation.status === "draft";
  const headConversations = head.conversations.filter(visible);
  const headIDs = new Set(headConversations.map((conversation) => conversation.id));
  const conversations = [
    ...headConversations,
    ...previous.conversations.filter((conversation) => visible(conversation) && !headIDs.has(conversation.id)),
  ];
  // If the user had already reached the end, the head response plus the cached
  // tail still covers the whole mailbox. Keep the window closed instead of
  // making the next scroll walk every page again just because a new message
  // arrived.
  const allLoaded = previous.hasMore === false
    && conversations.length >= (head.total ?? previous.total ?? 0);
  return {
    ...head,
    conversations,
    // The head page's cursor points at page two. Once the user has scrolled
    // further, the tail cursor is the only valid continuation.
    nextCursor: allLoaded ? undefined : previous.hasMore ? previous.nextCursor : head.nextCursor,
    hasMore: allLoaded ? false : previous.hasMore ? true : Boolean(head.hasMore),
    total: head.total ?? previous.total,
    unreadCount: head.unreadCount ?? previous.unreadCount,
  };
}

/**
 * The single conversations query used by both the sidebar shell and the inbox
 * list. It keeps a flat cache (rather than an infinite-query pages array) so
 * the existing optimistic note/read/status updates keep working unchanged.
 *
 * The server returns a per-user revision. When a reconnect resync finds the
 * revision unchanged, it replies with a tiny { unchanged: true } envelope and
 * we reuse the cached response, including every already-loaded tail page.
 */
export function useConversationsQuery(filter: ConversationFilter = "open") {
  const queryClient = useQueryClient();
  const key = useMemo(() => conversationsQueryKey(filter), [filter]);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState<Error | null>(null);
  const loadingRef = useRef(false);

  useEffect(() => {
    setLoadMoreError(null);
  }, [filter]);

  const query = useQuery({
    queryKey: key,
    queryFn: async (): Promise<ConversationListResponse> => {
      const previous = queryClient.getQueryData<ConversationListResponse>(key);
      const next = await getConversations({
        filter,
        revision: previous?.revision,
        limit: CONVERSATION_PAGE_SIZE,
      });
      if (next.unchanged) {
        // The server only answers "unchanged" when it saw the same revision we
        // sent, so a cached body is the authoritative current state.
        return previous ?? (await getConversations({ filter, limit: CONVERSATION_PAGE_SIZE }));
      }
      return mergeHead(previous, next, filter);
    },
    staleTime: 60_000,
    placeholderData: keepPreviousData,
    retry: 1,
  });

  const loadMore = useCallback(async () => {
    if (loadingRef.current) return;
    const current = queryClient.getQueryData<ConversationListResponse>(key);
    if (!current?.hasMore || !current.nextCursor) return;

    loadingRef.current = true;
    setLoadingMore(true);
    setLoadMoreError(null);
    try {
      const next = await getConversations({
        filter,
        cursor: current.nextCursor,
        limit: CONVERSATION_PAGE_SIZE,
      });
      queryClient.setQueryData<ConversationListResponse>(key, (latest) => {
        if (!latest) return next;
        // A head refresh can replace the continuation cursor while this page is
        // in flight; appending the stale page would duplicate or skip rows.
        if (latest.nextCursor !== current.nextCursor) return latest;
        const seen = new Set(latest.conversations.map((conversation) => conversation.id));
        return {
          ...latest,
          conversations: [
            ...latest.conversations,
            ...next.conversations.filter((conversation) => !seen.has(conversation.id)),
          ],
          nextCursor: next.nextCursor,
          hasMore: next.hasMore,
          total: next.total ?? latest.total,
          unreadCount: next.unreadCount ?? latest.unreadCount,
        };
      });
    } catch (error) {
      setLoadMoreError(error instanceof Error ? error : new Error(String(error)));
    } finally {
      loadingRef.current = false;
      setLoadingMore(false);
    }
  }, [filter, key, queryClient]);

  return { ...query, loadMore, loadingMore, loadMoreError };
}
