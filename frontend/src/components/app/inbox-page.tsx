import { useEffect, useRef, useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { toast, toastError } from "../../lib/toast";
import {
  ApiError,
  deleteConversation,
  getConversation,
  markConversationRead,
  markConversationUnread,
  retryOutgoing,
  searchMailMessages,
} from "../../lib/api";
import { en } from "../../lib/locale";
import { useConversationsQuery } from "../../lib/use-conversations";
import {
  escapeHTML,
  splitRecipientValues,
  structuredQuotedTextToHTML,
  uniqueRecipients,
} from "../../lib/email-format";
import { migrateLegacyOutgoing } from "../../lib/legacy-outbox-migration";
import { flagsMarkedSeen, flagsMarkedUnread } from "../../lib/mailbox";
import { useDebouncedValue } from "../../lib/utils";
import type {
  ConversationDetail,
  ConversationDetailResponse,
  ConversationListResponse,
  ConversationMessage,
  ConversationSummary,
  MailSearchResult,
} from "../../types";
import { useShell } from "./shell-context";
import { ChatPanel, ConversationList, type MailFocusTarget } from "./chat-view";
import { LoginScreen } from "./auth-screens";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { ResizeHandle, usePersistedPaneWidth } from "./resize-handle";

export function conversationIdFromURL() {
  if (typeof window === "undefined") return null;
  return new URL(window.location.href).searchParams.get("conversation");
}

/**
 * The message a conversation should open at, taken from the URL so a search
 * hit survives reloads and browser back/forward.
 */
export function messageTargetFromURL(): MailFocusTarget | null {
  if (typeof window === "undefined") return null;
  const params = new URL(window.location.href).searchParams;
  const id = params.get("message");
  if (!id) return null;
  return { folder: params.get("messageFolder") || "INBOX", id };
}

export function setConversationURL(id: string | null, mode: "push" | "replace" = "push", target?: MailFocusTarget | null) {
  const url = new URL(window.location.href);
  if (id) url.searchParams.set("conversation", id);
  else url.searchParams.delete("conversation");
  if (id && target) {
    url.searchParams.set("message", target.id);
    url.searchParams.set("messageFolder", target.folder || "INBOX");
  } else {
    url.searchParams.delete("message");
    url.searchParams.delete("messageFolder");
  }
  window.history[mode === "push" ? "pushState" : "replaceState"](window.history.state, "", url);
}

export function InboxPage() {
  const queryClient = useQueryClient();
  const { copy: locale, openMobileMenu, openCompose, showAllConversations, setShowAllConversations } = useShell();
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(conversationIdFromURL);
  const [messageTarget, setMessageTarget] = useState<MailFocusTarget | null>(messageTargetFromURL);
  const [chatOpen, setChatOpen] = useState(() => Boolean(conversationIdFromURL()));
  const [deleteTarget, setDeleteTarget] = useState<ConversationSummary | null>(null);
  const [retryTarget, setRetryTarget] = useState<ConversationMessage | null>(null);
  const [retryPending, setRetryPending] = useState(false);
  const [listWidth, setListWidth] = usePersistedPaneWidth("inbrix-message-list-width", 370, 260, 560);
  const autoReadRef = useRef(new Set<string>());
  const manuallyUnreadRef = useRef(new Set<string>());
  const debouncedSearch = useDebouncedValue(search, 250);
  const searchQuery = debouncedSearch.trim();

  useEffect(() => {
    const restoreConversationFromURL = () => {
      const id = conversationIdFromURL();
      setSelectedId(id);
      setMessageTarget(messageTargetFromURL());
      setChatOpen(Boolean(id) || window.innerWidth > 1023);
    };
    window.addEventListener("popstate", restoreConversationFromURL);
    return () => window.removeEventListener("popstate", restoreConversationFromURL);
  }, []);

  const conversations = useConversationsQuery(showAllConversations ? "all" : "open");

  // Full-text search runs against the local mirror and returns matching
  // messages, so a result can be opened at its exact position in the thread.
  const mailSearch = useQuery({
    queryKey: ["mail-search", searchQuery],
    queryFn: () => searchMailMessages(searchQuery),
    enabled: searchQuery.length > 0,
    placeholderData: keepPreviousData,
    retry: 1,
  });

  const detail = useQuery({
    queryKey: ["conversation", selectedId],
    queryFn: () => getConversation(selectedId!),
    enabled: Boolean(selectedId),
    retry: 1,
  });

  useEffect(() => {
    if (!conversations.data) return;
    void migrateLegacyOutgoing(conversations.data.accounts.map((account) => account.email)).then((changed) => {
      if (changed) {
        void queryClient.invalidateQueries({ queryKey: ["conversations"] });
        void queryClient.invalidateQueries({ queryKey: ["conversation"] });
      }
    }).catch((error: unknown) => toast.error(error instanceof Error ? error.message : locale.sendFailed));
  }, [conversations.data, queryClient, locale.sendFailed]);

  const serverConversation = detail.data?.conversation;
  const retrying = useRef(new Set<string>());
  const sendState = detail.data?.conversation.messages.map((message) => `${message.id}:${message.sendStatus || ""}`).join("|");
  useEffect(() => {
    if (sendState?.includes(":sent") || sendState?.includes(":failed") || sendState?.includes(":unknown")) {
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
    }
  }, [sendState, queryClient]);

  useEffect(() => {
    const isAuthError = conversations.error instanceof ApiError && conversations.error.status === 401;
    if (conversations.error && conversations.data && !isAuthError) {
      toast.error(conversations.error instanceof Error ? conversations.error.message : locale.loadFailed, {
        id: "conversations-refresh-error",
        action: {
          label: locale.retry,
          onClick: () => void conversations.refetch(),
        },
      });
    } else if (!conversations.error || isAuthError) {
      toast.dismiss("conversations-refresh-error");
    }
  }, [conversations.error, conversations.data, locale.loadFailed, locale.retry]);

  useEffect(() => {
    if (detail.error && detail.data) {
      toast.error(detail.error instanceof Error ? detail.error.message : locale.loadFailed, {
        id: "conversation-detail-error",
        action: {
          label: locale.retry,
          onClick: () => void detail.refetch(),
        },
      });
    } else if (!detail.error) {
      toast.dismiss("conversation-detail-error");
    }
  }, [detail.error, detail.data, locale.loadFailed, locale.retry]);

  useEffect(() => {
    autoReadRef.current.clear();
  }, [selectedId]);

  const deleteMutation = useMutation({
    mutationFn: (conversation: ConversationSummary) => deleteConversation(conversation.id),
    onSuccess: async (_, conversation) => {
      setDeleteTarget(null);
      if (selectedId === conversation.id) {
        setSelectedId(null);
        setChatOpen(false);
        setConversationURL(null, "replace");
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["conversations"] }),
      ]);
      queryClient.removeQueries({ queryKey: ["conversation", conversation.id] });
    },
    onError: (value) => toastError(value, locale.deleteConversationFailed),
  });

  useEffect(() => {
    const conversation = detail.data?.conversation;
    if (!conversation || manuallyUnreadRef.current.has(conversation.id)) return;
    const unread = conversation.messages.filter((message) => !message.outgoing && !message.flags?.some((flag) => flag.toLowerCase() === "\\seen"));
    if (unread.length === 0) return;
    const fingerprint = `${conversation.id}:${unread.map((message) => `${message.folder || "INBOX"}/${message.id}`).join(",")}`;
    if (autoReadRef.current.has(fingerprint)) return;
    autoReadRef.current.add(fingerprint);
    const previousDetail = queryClient.getQueryData<ConversationDetailResponse>(["conversation", conversation.id]);
    const previousLists = queryClient.getQueriesData<ConversationListResponse>({ queryKey: ["conversations"] });
    queryClient.setQueryData<ConversationDetailResponse>(["conversation", conversation.id], (current) => current ? {
      ...current,
      conversation: { ...current.conversation, messages: current.conversation.messages.map((message) => message.outgoing ? message : { ...message, flags: flagsMarkedSeen(message.flags) }) },
    } : current);
    queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) => current ? {
      ...current,
      conversations: current.conversations.map((item) => item.id === conversation.id ? { ...item, unreadCount: 0 } : item),
    } : current);
    void markConversationRead(conversation.id).then(async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["conversations"] }),
        queryClient.invalidateQueries({ queryKey: ["conversation", conversation.id] }),
      ]);
    }).catch(() => {
      queryClient.setQueryData(["conversation", conversation.id], previousDetail);
      for (const [key, value] of previousLists) queryClient.setQueryData(key, value);
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
    });
  }, [detail.data?.conversation, queryClient]);

  const markUnread = async (conversation: ConversationSummary) => {
    manuallyUnreadRef.current.add(conversation.id);
    const previousDetail = queryClient.getQueryData<ConversationDetailResponse>(["conversation", conversation.id]);
    const previousLists = queryClient.getQueriesData<ConversationListResponse>({ queryKey: ["conversations"] });
    const incomingCount = previousDetail?.conversation.messages.filter((message) => !message.outgoing).length;
    queryClient.setQueryData<ConversationDetailResponse>(["conversation", conversation.id], (current) => current ? {
      ...current,
      conversation: { ...current.conversation, messages: current.conversation.messages.map((message) => message.outgoing ? message : { ...message, flags: flagsMarkedUnread(message.flags) }) },
    } : current);
    queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) => current ? {
      ...current,
      conversations: current.conversations.map((item) => item.id === conversation.id ? { ...item, unreadCount: incomingCount ?? Math.max(1, item.unreadCount) } : item),
    } : current);
    try {
      await markConversationUnread(conversation.id);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["conversations"] }),
        queryClient.invalidateQueries({ queryKey: ["conversation", conversation.id] }),
      ]);
    } catch (value) {
      manuallyUnreadRef.current.delete(conversation.id);
      queryClient.setQueryData(["conversation", conversation.id], previousDetail);
      for (const [key, data] of previousLists) queryClient.setQueryData(key, data);
      toast.error(value instanceof Error ? value.message : locale.markUnreadFailed);
    }
  };

  useEffect(() => {
    if (!conversations.data) return;
    const items = conversations.data.conversations || [];
    if (!selectedId && items.length > 0 && window.innerWidth > 1023) {
      setSelectedId(items[0].id);
      setChatOpen(true);
    }
  }, [conversations.data, selectedId]);

  // The list is server-filtered and paginated, so a conversation can be absent
  // from the loaded rows while still existing (deep link or a hidden status).
  // Only the detail endpoint's 404 is authoritative for "deleted".
  useEffect(() => {
    if (selectedId && detail.error instanceof ApiError && detail.error.status === 404) {
      setSelectedId(null);
      setChatOpen(false);
      if (conversationIdFromURL() === selectedId) setConversationURL(null, "replace");
    }
  }, [detail.error, selectedId]);

  const openReply = (conversation: ConversationDetail, message?: ConversationMessage, suggestedBody?: string) => {
    const source = message || conversation.messages.at(-1);
    const subject = source?.subject || conversation.subject;
    const replySubject = subject.toLowerCase().startsWith("re:") ? subject : `Re: ${subject}`;
    const recipient = source?.outgoing ? source.to : source?.from || conversation.peerEmail || "";
    const sender = source?.fromName && source.from ? `${source.fromName} <${source.from}>` : source?.from || conversation.peerEmail || "";
    const quoteLead = `On ${new Date(source?.date || Date.now()).toLocaleString(locale === en ? "en" : "zh-CN")}, ${sender} wrote:`;
    const originalBody = source?.body || source?.preview || "";
    const quotedHTML = structuredQuotedTextToHTML(originalBody);
    const references = [...(source?.references || [])];
    if (source?.messageId && !references.includes(source.messageId)) references.push(source.messageId);
    openCompose({
      accountEmail: conversation.accountEmail,
      to: recipient,
      subject: replySubject,
      // Deliberately the empty template: a body baked into `html` would be
      // indistinguishable from "already saved", and opening a reply with a
      // suggested answer would then leave no draft behind. The text travels in
      // `replaceBody`, which counts as an edit.
      html: `<p><br></p><p>${escapeHTML(quoteLead)}</p><blockquote>${quotedHTML}</blockquote>`,
      replaceBody: suggestedBody?.trim() || undefined,
      inReplyTo: source?.messageId,
      references,
      conversation: conversation.messages,
      conversationId: conversation.id,
    });
  };

  const openReplyAll = (conversation: ConversationDetail, message?: ConversationMessage, suggestedBody?: string) => {
    const source = message || conversation.messages.at(-1);
    if (!source) return;
    const subject = source.subject || conversation.subject;
    const replySubject = subject.toLowerCase().startsWith("re:") ? subject : `Re: ${subject}`;
    const self = conversation.accountEmail || conversations.data?.accountEmail || "";
    const originalTo = splitRecipientValues(source.to);
    const originalCc = splitRecipientValues(source.cc || "");
    const to = source.outgoing
      ? uniqueRecipients(originalTo, [self])
      : uniqueRecipients([source.from, ...originalTo], [self]);
    const finalTo = to.length > 0 ? to : uniqueRecipients([source.outgoing ? source.to : source.from || conversation.peerEmail || ""]);
    const cc = uniqueRecipients(originalCc, [self, ...finalTo]);
    const sender = source.fromName && source.from ? `${source.fromName} <${source.from}>` : source.from || conversation.peerEmail || "";
    const quoteLead = `On ${new Date(source.date || Date.now()).toLocaleString(locale === en ? "en" : "zh-CN")}, ${sender} wrote:`;
    const originalBody = source.body || source.preview || "";
    const references = [...(source.references || [])];
    if (source.messageId && !references.includes(source.messageId)) references.push(source.messageId);
    openCompose({
      accountEmail: conversation.accountEmail,
      to: finalTo.join(", "),
      cc: cc.join(", "),
      subject: replySubject,
      html: `<p><br></p><p>${escapeHTML(quoteLead)}</p><blockquote>${structuredQuotedTextToHTML(originalBody)}</blockquote>`,
      replaceBody: suggestedBody?.trim() || undefined,
      inReplyTo: source.messageId,
      references,
      conversation: conversation.messages,
      conversationId: conversation.id,
    });
  };

  const openNewMailForMessage = (conversation: ConversationDetail, message: ConversationMessage) => {
    const recipient = message.outgoing ? message.to : message.from || conversation.peerEmail || "";
    openCompose({ accountEmail: conversation.accountEmail, to: recipient, subject: "", conversation: conversation.messages, conversationId: conversation.id });
  };

  const handleRetrySend = async (message: ConversationMessage) => {
    if (retrying.current.has(message.id)) return;
    retrying.current.add(message.id);
    setRetryPending(true);
    try {
      await retryOutgoing(message.id);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["conversations"] }),
        queryClient.invalidateQueries({ queryKey: ["conversation"] }),
      ]);
      setRetryTarget(null);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : locale.sendFailedRetry);
    } finally { retrying.current.delete(message.id); setRetryPending(false); }
  };

  const handleReEditMessage = (message: ConversationMessage) => {
    if (!detail.data?.conversation || message.sendRetryable !== false) return;
    const document = new DOMParser().parseFromString(message.html || `<p>${escapeHTML(message.body)}</p>`, "text/html");
    toast.info(locale.sendAttachmentsLost);
    document.querySelectorAll('img[src^="cid:"], img[src^="blob:"], img[src*="/api/outbox/"]').forEach((image) => image.remove());
    openCompose({
      accountEmail: detail.data.conversation.accountEmail,
      to: message.to,
      cc: message.cc,
      subject: message.subject,
      html: document.body.innerHTML,
      inReplyTo: message.inReplyTo,
      references: message.references,
      conversation: detail.data.conversation.messages,
      conversationId: detail.data.conversation.id,
      replacesOutboxId: message.id,
    });
  };

  const closeChat = () => {
    setChatOpen(false);
    setSelectedId(null);
    setMessageTarget(null);
    setConversationURL(null, "replace");
  };

  const openSearchResult = (result: MailSearchResult) => {
    manuallyUnreadRef.current.delete(result.conversationId);
    const target = { folder: result.folder || "INBOX", id: result.messageId };
    setSelectedId(result.conversationId);
    setMessageTarget(target);
    setChatOpen(true);
    setConversationURL(result.conversationId, "push", target);
  };

  const authenticated = conversations.error instanceof ApiError && conversations.error.status === 401;
  if (authenticated) return <LoginScreen copy={locale} />;

  return (
    <>
      <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
        <ConversationList
          copy={locale}
          data={conversations.data}
          search={search}
          onSearch={setSearch}
          onMenu={openMobileMenu}
          loading={conversations.isPending}
          error={conversations.error}
          selectedId={selectedId}
          searchState={searchQuery ? {
            query: searchQuery,
            terms: mailSearch.data?.terms || [],
            results: mailSearch.data?.results || [],
            hasMore: mailSearch.data?.hasMore || false,
            loading: mailSearch.isFetching,
            error: mailSearch.error,
            selectedKey: selectedId && messageTarget ? `${selectedId}:${messageTarget.folder}:${messageTarget.id}` : undefined,
            onSelect: openSearchResult,
            onRetry: () => void mailSearch.refetch(),
          } : undefined}
          onSelect={(id) => {
            manuallyUnreadRef.current.delete(id);
            setSelectedId(id);
            setMessageTarget(null);
            setChatOpen(true);
            if (conversationIdFromURL() !== id) setConversationURL(id);
          }}
          onMarkUnread={(conversation) => void markUnread(conversation)}
          onDelete={(conversation) => setDeleteTarget(conversation)}
          onRefresh={() => void conversations.refetch()}
          onLoadMore={conversations.loadMore}
          hasMore={conversations.data?.hasMore}
          loadingMore={conversations.loadingMore}
          loadMoreError={conversations.loadMoreError}
          showAllConversations={showAllConversations}
          onShowAllChange={setShowAllConversations}
          desktopWidth={listWidth}
          className={chatOpen ? "hidden lg:flex" : "flex"}
        />
        {chatOpen && <ResizeHandle label="Resize conversation list" width={listWidth} minWidth={260} maxWidth={560} onResize={setListWidth} />}
        <ChatPanel
          copy={locale}
          detail={serverConversation}
          loading={detail.isPending && Boolean(selectedId)}
          error={detail.error}
          focusTarget={messageTarget}
          onBack={closeChat}
          onReply={openReply}
          onReplyAll={openReplyAll}
          onNewMail={openNewMailForMessage}
          onRetrySend={setRetryTarget}
          onReEdit={handleReEditMessage}
          onConversationEmpty={closeChat}
          className={chatOpen ? "flex" : "hidden lg:flex"}
        />
      </main>
      <Dialog open={Boolean(retryTarget)} onOpenChange={(open) => { if (!open && !retryPending) setRetryTarget(null); }}>
        <DialogContent className="sm:max-w-md" showCloseButton={!retryPending}>
          <DialogHeader>
            <DialogTitle>{locale.resendConfirmTitle}</DialogTitle>
            <DialogDescription>{locale.resendConfirmDescription}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-1 break-words text-sm">
            <p><span className="text-muted-foreground">{locale.to}：</span>{retryTarget?.to}</p>
            {retryTarget?.cc && <p><span className="text-muted-foreground">{locale.cc}：</span>{retryTarget.cc}</p>}
            <p><span className="text-muted-foreground">{locale.subject}：</span>{retryTarget?.subject || locale.noSubject}</p>
          </div>
          {retryTarget?.sendStatus === "unknown" && <p className="text-sm text-destructive">{locale.resendUnconfirmedWarning}</p>}
          <DialogFooter>
            <Button variant="ghost" disabled={retryPending} onClick={() => setRetryTarget(null)}>{locale.cancel}</Button>
            <Button disabled={retryPending || !retryTarget} onClick={() => retryTarget && void handleRetrySend(retryTarget)}>
              {retryPending ? locale.sending : locale.resendConfirmAction}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open && !deleteMutation.isPending) setDeleteTarget(null); }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{locale.deleteConversationTitle}</DialogTitle>
            <DialogDescription>{locale.deleteConversationDescription}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" disabled={deleteMutation.isPending} onClick={() => setDeleteTarget(null)}>{locale.cancel}</Button>
            <Button variant="destructive" disabled={deleteMutation.isPending || !deleteTarget} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>
              <Trash2 />{deleteMutation.isPending ? locale.deleting : locale.deleteConversation}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
