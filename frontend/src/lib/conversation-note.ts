import { useMutation, useQueryClient } from "@tanstack/react-query";
import { saveConversationNote } from "./api";
import type { ConversationDetailResponse, ConversationListResponse } from "../types";

// Both note editors write the same server record and update both views.
export function useSaveConversationNote(conversationId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (note: string) => saveConversationNote(conversationId, note),
    onSuccess: ({ note }) => {
      queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) => current ? {
        ...current,
        conversations: current.conversations.map((item) => item.id === conversationId ? { ...item, note } : item),
      } : current);
      queryClient.setQueryData<ConversationDetailResponse>(["conversation", conversationId], (current) => current ? {
        ...current,
        conversation: { ...current.conversation, note },
      } : current);
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
      void queryClient.invalidateQueries({ queryKey: ["conversation", conversationId] });
      void queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] });
    },
  });
}
