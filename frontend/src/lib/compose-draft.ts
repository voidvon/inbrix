import { useQuery } from "@tanstack/react-query";
import { listDrafts } from "./api";
import type { ComposeDraft, ComposeDraftListResponse } from "../types";

/**
 * The single draft a compose window owns. A reply owns the draft parked on its
 * conversation, including a new email started by selecting a recipient. The
 * conversation-less lookup is retained for older drafts.
 *
 * The transcript's draft entry and the editor itself read the same query key, so
 * one fetch serves both and saving in the editor immediately updates the entry
 * without either component knowing about the other.
 */
export function composeDraftKey(accountEmail: string, conversationId: string) {
  return ["compose-draft", accountEmail, conversationId] as const;
}

export function useComposeDraft(
  accountEmail: string,
  conversationId: string,
  options: { enabled?: boolean; alwaysFresh?: boolean } = {},
) {
  return useQuery({
    queryKey: composeDraftKey(accountEmail, conversationId),
    queryFn: () => listDrafts(conversationId || undefined),
    enabled: options.enabled ?? true,
    retry: false,
    // A draft written on another computer must win over a warm local cache, so
    // the editor re-reads the server every time it opens.
    staleTime: options.alwaysFresh ? 0 : 15_000,
    refetchOnMount: options.alwaysFresh ? "always" : true,
    select: (data: ComposeDraftListResponse): ComposeDraft | null =>
      conversationId
        ? data.drafts.find((draft) => draft.conversationId === conversationId) ?? null
        : data.drafts.find((draft) => !draft.conversationId) ?? null,
  });
}
