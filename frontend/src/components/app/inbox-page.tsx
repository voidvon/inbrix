import { useEffect, useMemo, useRef, useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  ApiError,
  deleteConversation,
  getConversation,
  getConversations,
  markConversationRead,
  markConversationUnread,
} from "../../lib/api";
import { en } from "../../lib/locale";
import {
  escapeHTML,
  generatedEmailHTML,
  splitRecipientValues,
  structuredQuotedTextToHTML,
  uniqueRecipients,
} from "../../lib/email-format";
import {
  reconcileOptimisticMessages,
  removeOptimisticMessage,
  retrySendMessage,
  useOptimisticMessages,
} from "../../lib/optimistic-messages";
import { flagsMarkedSeen, flagsMarkedUnread } from "../../lib/mailbox";
import { useDebouncedValue } from "../../lib/utils";
import type {
  ConversationDetail,
  ConversationDetailResponse,
  ConversationListResponse,
  ConversationMessage,
  ConversationSummary,
} from "../../types";
import { useShell } from "./shell-context";
import { ChatPanel, ConversationList } from "./chat-view";
import { LoginScreen } from "./auth-screens";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { ResizeHandle, usePersistedPaneWidth } from "./resize-handle";

export function conversationIdFromURL() {
  if (typeof window === "undefined") return null;
  return new URL(window.location.href).searchParams.get("conversation");
}

export function setConversationURL(id: string | null, mode: "push" | "replace" = "push") {
  const url = new URL(window.location.href);
  if (id) url.searchParams.set("conversation", id);
  else url.searchParams.delete("conversation");
  window.history[mode === "push" ? "pushState" : "replaceState"](window.history.state, "", url);
}

export function InboxPage() {
  const queryClient = useQueryClient();
  const { copy: locale, openMobileMenu, openCompose } = useShell();
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(conversationIdFromURL);
  const [chatOpen, setChatOpen] = useState(() => Boolean(conversationIdFromURL()));
  const [deleteTarget, setDeleteTarget] = useState<ConversationSummary | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const [listWidth, setListWidth] = usePersistedPaneWidth("inbrix-message-list-width", 370, 260, 560);
  const autoReadRef = useRef(new Set<string>());
  const manuallyUnreadRef = useRef(new Set<string>());
  const debouncedSearch = useDebouncedValue(search, 250);

  useEffect(() => {
    const restoreConversationFromURL = () => {
      const id = conversationIdFromURL();
      setSelectedId(id);
      setChatOpen(Boolean(id) || window.innerWidth > 1023);
    };
    window.addEventListener("popstate", restoreConversationFromURL);
    return () => window.removeEventListener("popstate", restoreConversationFromURL);
  }, []);

  const conversations = useQuery({
    queryKey: ["conversations", debouncedSearch],
    queryFn: () => getConversations(debouncedSearch),
    refetchInterval: 30_000,
    placeholderData: keepPreviousData,
    retry: 1,
  });

  const detail = useQuery({
    queryKey: ["conversation", selectedId],
    queryFn: () => getConversation(selectedId!),
    enabled: Boolean(selectedId),
    retry: 1,
  });

  const optimisticMessages = useOptimisticMessages(selectedId);

  useEffect(() => {
    if (selectedId && detail.data?.conversation?.messages) {
      reconcileOptimisticMessages(selectedId, detail.data.conversation.messages);
    }
  }, [selectedId, detail.data?.conversation?.messages]);

  const mergedConversation = useMemo(() => {
    const conv = detail.data?.conversation;
    if (!conv) return undefined;
    if (!optimisticMessages.length) return conv;

    const existingIds = new Set(conv.messages.map((m) => m.id));
    const extraMessages = optimisticMessages.filter((m) => !existingIds.has(m.id));
    if (!extraMessages.length) return conv;

    return {
      ...conv,
      count: conv.messages.length + extraMessages.length,
      messages: [...conv.messages, ...extraMessages],
    };
  }, [detail.data?.conversation, optimisticMessages]);

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
      setDeleteError("");
      if (selectedId === conversation.id) {
        setSelectedId(null);
        setChatOpen(false);
        setConversationURL(null, "replace");
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["conversations"] }),
        queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] }),
      ]);
      queryClient.removeQueries({ queryKey: ["conversation", conversation.id] });
    },
    onError: (value) => setDeleteError(value instanceof Error ? value.message : locale.deleteConversationFailed),
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
        queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] }),
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
        queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] }),
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
    const items = conversations.data?.conversations || [];
    if (selectedId && !items.some((item) => item.id === selectedId)) {
      setSelectedId(null);
      setChatOpen(false);
      if (conversationIdFromURL() === selectedId) setConversationURL(null, "replace");
      return;
    }
    if (!selectedId && items.length > 0 && window.innerWidth > 1023) {
      setSelectedId(items[0].id);
      setChatOpen(true);
    }
  }, [conversations.data, selectedId]);

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
      html: `${suggestedBody?.trim() ? generatedEmailHTML(suggestedBody) : "<p><br></p>"}<p>${escapeHTML(quoteLead)}</p><blockquote>${quotedHTML}</blockquote>`,
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
      html: `${suggestedBody?.trim() ? generatedEmailHTML(suggestedBody) : "<p><br></p>"}<p>${escapeHTML(quoteLead)}</p><blockquote>${structuredQuotedTextToHTML(originalBody)}</blockquote>`,
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
    await retrySendMessage(
      message.id,
      () => {
        void queryClient.invalidateQueries({ queryKey: ["conversations"] });
        void queryClient.invalidateQueries({ queryKey: ["conversation", selectedId] });
        void queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] });
      },
      (err) => {
        toast.error(err.message || locale.sendFailedRetry);
      }
    );
  };

  const handleReEditMessage = (message: ConversationMessage) => {
    const opt = optimisticMessages.find((m) => m.id === message.id);
    removeOptimisticMessage(message.id);
    if (!opt || !detail.data?.conversation) return;
    openCompose({
      accountEmail: detail.data.conversation.accountEmail,
      to: opt.to,
      cc: opt.cc,
      subject: opt.subject,
      html: opt.html || `<p>${escapeHTML(opt.body)}</p>`,
      inReplyTo: opt.inReplyTo,
      references: opt.references,
      conversation: detail.data.conversation.messages,
      conversationId: opt.conversationId,
    });
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
          onSelect={(id) => {
            manuallyUnreadRef.current.delete(id);
            setSelectedId(id);
            setChatOpen(true);
            if (conversationIdFromURL() !== id) setConversationURL(id);
          }}
          onMarkUnread={(conversation) => void markUnread(conversation)}
          onDelete={(conversation) => { setDeleteError(""); setDeleteTarget(conversation); }}
          onRefresh={() => void conversations.refetch()}
          desktopWidth={listWidth}
          className={chatOpen ? "hidden lg:flex" : "flex"}
        />
        {chatOpen && <ResizeHandle label="Resize conversation list" width={listWidth} minWidth={260} maxWidth={560} onResize={setListWidth} />}
        <ChatPanel
          copy={locale}
          detail={mergedConversation}
          loading={detail.isPending && Boolean(selectedId)}
          error={detail.error}
          onBack={() => setChatOpen(false)}
          onReply={openReply}
          onReplyAll={openReplyAll}
          onNewMail={openNewMailForMessage}
          onRetrySend={(message) => { void handleRetrySend(message); }}
          onReEdit={handleReEditMessage}
          onConversationEmpty={() => {
            setSelectedId(null);
            setChatOpen(false);
            setConversationURL(null, "replace");
          }}
          className={chatOpen ? "flex" : "hidden lg:flex"}
        />
      </main>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open && !deleteMutation.isPending) { setDeleteTarget(null); setDeleteError(""); } }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{locale.deleteConversationTitle}</DialogTitle>
            <DialogDescription>{locale.deleteConversationDescription}</DialogDescription>
          </DialogHeader>
          {deleteError && <p className="text-sm text-destructive">{deleteError}</p>}
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
