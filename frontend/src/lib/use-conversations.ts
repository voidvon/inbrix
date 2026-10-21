import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { getConversations } from "./api";
import type { ConversationListResponse } from "../types";

/**
 * The single conversations query used by both the sidebar shell and the inbox
 * list. Sharing one query key removes the duplicate /api/conversations request
 * that the old mailbox-shell + conversations split made on every refresh.
 *
 * The server returns a per-user revision. When a reconnect resync finds the
 * revision unchanged, it replies with a tiny { unchanged: true } envelope and
 * we reuse the cached response instead of rebuilding every conversation.
 */
export function useConversationsQuery() {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: ["conversations"],
    queryFn: async (): Promise<ConversationListResponse> => {
      const previous = queryClient.getQueryData<ConversationListResponse>(["conversations"]);
      const next = await getConversations("", previous?.revision);
      if (next.unchanged) {
        // The server only answers "unchanged" when it saw the same revision we
        // sent, so a cached body is the authoritative current state.
        return previous ?? (await getConversations(""));
      }
      return next;
    },
    staleTime: 60_000,
    placeholderData: keepPreviousData,
    retry: 1,
  });
}
