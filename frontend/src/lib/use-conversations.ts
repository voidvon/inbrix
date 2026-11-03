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
    queryFn: async ({ signal }): Promise<ConversationListResponse> => {
      const previous = queryClient.getQueryData<ConversationListResponse>(key);
      // Rebuild the loaded window from server rows. Merging cached rows back
      // into a filtered head resurrects conversations that were answered or
      // deleted, because their old cached status still matches the filter.
      const windowSize = Math.max(CONVERSATION_PAGE_SIZE, previous?.conversations.length ?? 0);
      const limit = Math.min(100, windowSize);
      const next = await getConversations({
        filter,
        revision: previous?.revision,
        limit,
        signal,
      });
      if (next.unchanged) {
        // The server only answers "unchanged" when it saw the same revision we
        // sent, so a cached body is the authoritative current state.
        return previous ?? (await getConversations({ filter, limit, signal }));
      }
      let page = next;
      const rows = new Map(next.conversations.map((conversation) => [conversation.id, conversation]));
      const cursors = new Set<string>();
      while (rows.size < windowSize && page.hasMore && page.nextCursor && !cursors.has(page.nextCursor)) {
        cursors.add(page.nextCursor);
        page = await getConversations({ filter, cursor: page.nextCursor, limit: Math.min(limit, windowSize - rows.size), signal });
        for (const conversation of page.conversations) rows.set(conversation.id, conversation);
      }
      return {
        ...next,
        conversations: [...rows.values()],
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
      };
    },
    staleTime: 60_000,
    placeholderData: keepPreviousData,
    retry: 1,
  });

  const loadMore = useCallback(async () => {
    if (loadingRef.current || queryClient.isFetching({ queryKey: key, exact: true })) return;
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
        if (latest.nextCursor !== current.nextCursor || latest.revision !== current.revision) return latest;
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
