import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getConversation } from "./api";
import type { ConversationDetailResponse } from "../types";

export const MESSAGE_PAGE_SIZE = 3;

type Target = { folder: string; id: string };
type Direction = "older" | "newer";
type PageState = { id: string; target: string; direction: Direction; loading: boolean; error: Error | null };
const messageKey = (message: { folder?: string; id: string }) => `${message.folder || "INBOX"}\u0000${message.id}`;
const windowKey = (data?: ConversationDetailResponse) => JSON.stringify([
  data?.windowTarget || "", data?.conversation.oldestCursor, data?.conversation.newestCursor, Boolean(data?.conversation.hasNewer),
]);

export function useConversationMessages(id: string | null, target?: Target | null) {
  const queryClient = useQueryClient();
  const targetKey = target ? messageKey(target) : "";
  const pending = useRef(new Map<string, AbortController>());
  const epoch = useRef(0);
  const previousNavigation = useRef({ id, targetKey });
  const [pageStates, setPageStates] = useState<Partial<Record<Direction, PageState>>>({});
  const [latestRequest, setLatestRequest] = useState<number | null>(null);
  const loadingLatest = latestRequest !== null;
  const query = useQuery({
    queryKey: ["conversation", id],
    queryFn: async ({ signal }): Promise<ConversationDetailResponse> => {
      const key = ["conversation", id];
      for (;;) {
        const current = queryClient.getQueryData<ConversationDetailResponse>(key);
        const signature = windowKey(current);
        const navigate = (current?.windowTarget || "") !== targetKey;
        const response = await getConversation(id!, {
          limit: MESSAGE_PAGE_SIZE,
          ...(navigate ? (target ? { around: target.id, folder: target.folder } : {}) : {
            through: current?.conversation.oldestCursor,
            until: current?.conversation.hasNewer ? current.conversation.newestCursor : undefined,
          }),
          signal,
        });
        // A page may finish during refresh. Re-read both boundaries before
        // replacing the cache, retaining extensions in either direction.
        if (windowKey(queryClient.getQueryData<ConversationDetailResponse>(key)) === signature) {
          return { ...response, windowTarget: targetKey };
        }
      }
    },
    enabled: Boolean(id),
    retry: 1,
  });

  // A different search hit in the same conversation is a new window. Keep the
  // shared query key so existing optimistic mail/AI updates still reach it.
  useEffect(() => {
    const previous = previousNavigation.current;
    previousNavigation.current = { id, targetKey };
    if (previous.id === id && previous.targetKey === targetKey) return;
    epoch.current++;
    for (const request of pending.current.values()) request.abort();
    pending.current.clear();
    setPageStates({});
    setLatestRequest(null);
    if (previous.id === id && id) {
      const key = ["conversation", id];
      void queryClient.cancelQueries({ queryKey: key, exact: true }, { revert: false }).then(() => {
        if ((queryClient.getQueryData<ConversationDetailResponse>(key)?.windowTarget || "") !== targetKey) {
          return queryClient.invalidateQueries({ queryKey: key, exact: true });
        }
      });
    }
  }, [id, targetKey, queryClient]);

  useEffect(() => {
    const requests = pending.current;
    return () => { for (const request of requests.values()) request.abort(); };
  }, []);

  const loadPage = useCallback(async (direction: Direction) => {
    const requestKey = `${id}:${direction}`;
    if (!id || pending.current.has(requestKey) || loadingLatest) return;
    const key = ["conversation", id];
    const current = queryClient.getQueryData<ConversationDetailResponse>(key);
    if (!current || (current.windowTarget || "") !== targetKey) return;
    const older = direction === "older";
    const cursor = older ? current.conversation.oldestCursor : current.conversation.newestCursor;
    if (!cursor || !(older ? current.conversation.hasMore : current.conversation.hasNewer)) return;
    const controller = new AbortController();
    const version = epoch.current;
    pending.current.set(requestKey, controller);
    setPageStates(states => ({ ...states, [direction]: { id, target: targetKey, direction, loading: true, error: null } }));
    try {
      const page = (await getConversation(id, {
        limit: MESSAGE_PAGE_SIZE, ...(older ? { cursor } : { after: cursor }), signal: controller.signal,
      })).conversation;
      if (version !== epoch.current || controller.signal.aborted) return;
      queryClient.setQueryData<ConversationDetailResponse>(key, latest => {
        if (!latest || (latest.windowTarget || "") !== targetKey || (older ? latest.conversation.oldestCursor : latest.conversation.newestCursor) !== cursor) return latest;
        const seen = new Set(latest.conversation.messages.map(messageKey));
        const additions = page.messages.filter(message => !seen.has(messageKey(message)));
        return {
          ...latest,
          conversation: {
            ...latest.conversation,
            messages: older ? [...additions, ...latest.conversation.messages] : [...latest.conversation.messages, ...additions],
            ...(older ? { hasMore: page.hasMore, oldestCursor: page.oldestCursor || cursor } : { hasNewer: page.hasNewer, newestCursor: page.newestCursor || cursor }),
          },
        };
      });
    } catch (error) {
      if (!controller.signal.aborted) setPageStates(states => ({ ...states, [direction]: {
        id, target: targetKey, direction, loading: false, error: error instanceof Error ? error : new Error(String(error)),
      } }));
    } finally {
      if (pending.current.get(requestKey) === controller) {
        pending.current.delete(requestKey);
        setPageStates(states => ({ ...states, [direction]: states[direction] ? { ...states[direction], loading: false } : undefined }));
      }
    }
  }, [id, targetKey, queryClient, loadingLatest]);
  const loadOlder = useCallback(() => loadPage("older"), [loadPage]);
  const loadNewer = useCallback(() => loadPage("newer"), [loadPage]);

  const goLatest = useCallback(async () => {
    if (!id) return false;
    const version = ++epoch.current;
    for (const request of pending.current.values()) request.abort();
    pending.current.clear();
    setPageStates({});
    setLatestRequest(version);
    const controller = new AbortController();
    pending.current.set("latest", controller);
    try {
      const key = ["conversation", id];
      await queryClient.cancelQueries({ queryKey: key, exact: true });
      const response = await getConversation(id, { limit: MESSAGE_PAGE_SIZE, signal: controller.signal });
      if (version !== epoch.current) return false;
      // A realtime refresh can have started while the latest page was fetched.
      // Cancel it without reverting the replacement window to its old snapshot.
      await queryClient.cancelQueries({ queryKey: key, exact: true }, { revert: false });
      if (version !== epoch.current) return false;
      queryClient.setQueryData(key, { ...response, windowTarget: "" });
      return true;
    } catch (error) {
      if (controller.signal.aborted) return false;
      throw error;
    } finally {
      if (pending.current.get("latest") === controller) pending.current.delete("latest");
      setLatestRequest(current => current === version ? null : current);
    }
  }, [id, queryClient]);

  const stateFor = (direction: Direction) => {
    const state = pageStates[direction];
    return state?.id === id && state.target === targetKey ? state : undefined;
  };
  return {
    ...query,
    loadOlder, loadNewer, goLatest, loadingLatest,
    loadingOlder: Boolean(stateFor("older")?.loading),
    loadOlderError: stateFor("older")?.error,
    loadingNewer: Boolean(stateFor("newer")?.loading),
    loadNewerError: stateFor("newer")?.error,
  };
}
