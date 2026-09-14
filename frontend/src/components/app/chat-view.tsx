import { Fragment, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  ChevronDown,
  Copy as CopyIcon,
  Loader2,
  Mail,
  MessageCircle,
  Paperclip,
  Pencil,
  ReplyAll,
  RotateCcw,
  Search,
  Send,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { ConversationStatusTag, type ConversationStatus } from "./conversation-status-tag";
import { ListSkeleton, ErrorState, EmptyState } from "./common-states";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "../ui/context-menu";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { ScrollArea } from "../ui/scroll-area";
import { Separator } from "../ui/separator";
import {
  deleteConversationMessage,
  generateEmail,
  getAIModels,
  saveConversationNote,
  saveConversationStatus,
  sendMessage,
  summarizeMailMessage,
} from "../../lib/api";
import { cn, formatSize, formatTime, splitQuotedText } from "../../lib/utils";
import {
  copyToClipboard,
  escapeHTML,
  extractEmailAddress,
  renderLinkifiedText,
  splitRecipientValues,
  structuredQuotedTextToHTML,
  uniqueRecipients,
} from "../../lib/email-format";
import {
  addOptimisticMessage,
  removeOptimisticMessage,
  updateOptimisticStatus,
  type OptimisticMessage,
} from "../../lib/optimistic-messages";
import type {
  ConversationDetail,
  ConversationDetailResponse,
  ConversationListResponse,
  ConversationMessage,
  ConversationSummary,
  MailMessage,
  MailSummary,
} from "../../types";
import type { Copy } from "../../lib/locale";

export function ConversationList({
  copy,
  data,
  search,
  onSearch,
  onMenu,
  loading,
  error,
  selectedId,
  onSelect,
  onMarkUnread,
  onDelete,
  onRefresh,
  className,
}: {
  copy: Copy;
  data?: ConversationListResponse;
  search: string;
  onSearch: (value: string) => void;
  onMenu: () => void;
  loading: boolean;
  error: Error | null;
  selectedId: string | null;
  onSelect: (id: string) => void;
  onMarkUnread: (conversation: ConversationSummary) => void;
  onDelete: (conversation: ConversationSummary) => void;
  onRefresh: () => void;
  className?: string;
}) {
  const rows = data?.conversations || [];
  const hasData = Boolean(data);
  return (
    <section data-testid="conversation-list" className={cn("min-w-0 flex-1 flex-col border-r bg-card lg:w-[23.125rem] lg:flex-none", className)}>
      <div className="border-b bg-card px-3 py-3">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={onMenu} aria-label={copy.folders} title={copy.folders}>
            <MenuIcon />
          </Button>
          <div className="relative flex min-w-0 flex-1 items-center">
            <Search className="pointer-events-none absolute left-3 size-4 text-muted-foreground" aria-hidden="true" />
            <Input
              data-testid="mail-search"
              type="search"
              className="h-9 bg-muted/60 pl-9 pr-9"
              value={search}
              onChange={(event) => onSearch(event.target.value)}
              placeholder={copy.search}
              aria-label={copy.search}
            />
            {search && (
              <Button variant="ghost" size="icon" className="absolute right-1 size-7" onClick={() => onSearch("")} aria-label={copy.cancel} title={copy.cancel}>
                <X />
              </Button>
            )}
          </div>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        {!hasData && (loading || !error) && <ListSkeleton />}
        {!hasData && error && <ErrorState copy={copy} onRetry={onRefresh} />}
        {hasData && rows.length === 0 && <EmptyState icon={<MessageCircle />} text={search ? copy.noConversations : copy.noConversations} />}
        {hasData &&
          rows.map((conversation) => (
            <ConversationRow
              key={conversation.id}
              copy={copy}
              conversation={conversation}
              selected={conversation.id === selectedId}
              onClick={() => onSelect(conversation.id)}
              onMarkUnread={() => onMarkUnread(conversation)}
              onDelete={() => onDelete(conversation)}
            />
          ))}
      </ScrollArea>
    </section>
  );
}

// Internal icon for Menu to keep it decoupled
function MenuIcon() {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="lucide lucide-menu">
      <line x1="4" x2="20" y1="12" y2="12" />
      <line x1="4" x2="20" y1="6" y2="6" />
      <line x1="4" x2="20" y1="18" y2="18" />
    </svg>
  );
}

export function ConversationRow({
  copy,
  conversation,
  selected,
  onClick,
  onMarkUnread,
  onDelete,
}: {
  copy: Copy;
  conversation: ConversationSummary;
  selected: boolean;
  onClick: () => void;
  onMarkUnread: () => void;
  onDelete: () => void;
}) {
  const queryClient = useQueryClient();
  const inputRef = useRef<HTMLInputElement>(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(conversation.note || "");
  const [error, setError] = useState("");
  const [statusSaving, setStatusSaving] = useState(false);
  const cancelRef = useRef(false);

  useEffect(() => {
    if (!editing) setDraft(conversation.note || "");
  }, [conversation.note, editing]);

  const beginEditing = () => {
    setError("");
    setEditing(true);
    window.requestAnimationFrame(() => inputRef.current?.focus());
  };

  const saveNote = async () => {
    if (cancelRef.current) {
      cancelRef.current = false;
      return;
    }
    const note = draft.trim();
    setEditing(false);
    if (note === (conversation.note || "")) return;
    try {
      await saveConversationNote(conversation.id, note);
      setError("");
      queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) =>
        current
          ? {
              ...current,
              conversations: current.conversations.map((item) => (item.id === conversation.id ? { ...item, note } : item)),
            }
          : current
      );
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
    } catch {
      setError(copy.noteSaveFailed);
      setEditing(true);
      window.requestAnimationFrame(() => inputRef.current?.focus());
    }
  };

  const cancelEditing = () => {
    cancelRef.current = true;
    setDraft(conversation.note || "");
    setError("");
    setEditing(false);
    inputRef.current?.blur();
  };

  const changeStatus = async (status: ConversationStatus) => {
    if (status === conversation.status) return;
    setStatusSaving(true);
    try {
      await saveConversationStatus(conversation.id, status);
      setError("");
      queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) =>
        current
          ? {
              ...current,
              conversations: current.conversations.map((item) => (item.id === conversation.id ? { ...item, status } : item)),
            }
          : current
      );
    } catch {
      setError(copy.statusSaveFailed);
    } finally {
      setStatusSaving(false);
    }
  };

  const emailToCopy = extractEmailAddress(conversation.peerEmail) || extractEmailAddress(conversation.title) || extractEmailAddress(conversation.accountEmail);
  const handleCopyEmail = async () => {
    if (!emailToCopy) return;
    const ok = await copyToClipboard(emailToCopy);
    if (ok) {
      toast.success(copy.copyEmailSuccess);
    } else {
      toast.error(copy.copyEmailFailed);
    }
  };

  return (
    <ContextMenu>
      <ContextMenuTrigger className="block">
        <article
          data-testid="conversation-row"
          className={cn(
            "relative w-full max-w-full overflow-hidden border-b bg-card px-4 py-3 transition-colors hover:bg-muted",
            selected && "border-l-2 border-l-foreground bg-muted pl-[0.875rem]"
          )}
        >
          <button className="flex w-full min-w-0 max-w-full items-start gap-3 overflow-hidden text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50" onClick={onClick} type="button">
            <span className="min-w-0 max-w-full flex-1 overflow-hidden">
              <span className="flex min-w-0 max-w-full items-baseline justify-between gap-2 overflow-hidden">
                <strong className="min-w-0 flex-1 truncate text-sm font-semibold">{conversation.peerEmail || conversation.title || copy.conversations}</strong>
                <time className="shrink-0 text-[10px] text-muted-foreground">{formatTime(conversation.date)}</time>
              </span>
              <span className="mt-1 block max-w-full truncate text-xs text-muted-foreground/70">{conversation.preview || copy.noBody}</span>
            </span>
            {conversation.unreadCount > 0 && (
              <Badge title={`${conversation.unreadCount} ${copy.unread}`} className="mt-0.5 min-w-5 justify-center px-1.5 text-[10px] leading-4">
                {conversation.unreadCount}
              </Badge>
            )}
          </button>
          <div className="mt-1.5 flex h-5 w-full items-center gap-1.5 overflow-visible">
            <ConversationStatusTag
              value={conversation.status}
              labels={{ answered: copy.answered, unanswered: copy.unanswered, no_action: copy.noAction }}
              disabled={statusSaving}
              onChange={(status) => void changeStatus(status)}
            />
            {editing ? (
              <Input
                ref={inputRef}
                className="block h-full min-w-0 flex-1 rounded-none border-0 bg-transparent px-0 py-0 text-xs leading-4 shadow-none focus-visible:border-transparent focus-visible:ring-0"
                value={draft}
                maxLength={200}
                onChange={(event) => setDraft(event.target.value)}
                onBlur={() => void saveNote()}
                onKeyDown={(event) => {
                  if (event.key === "Enter") event.currentTarget.blur();
                  if (event.key === "Escape") cancelEditing();
                }}
                aria-label={copy.addNote}
              />
            ) : (
              <div className="group/note relative h-full min-w-0 flex-1">
                <button type="button" className={cn("block h-full w-full truncate pr-6 text-left text-xs leading-4", conversation.note ? "text-primary" : "text-muted-foreground/60")} onClick={onClick}>
                  {conversation.note || copy.addNote}
                </button>
                <button
                  type="button"
                  className="absolute top-0 right-0 grid size-4 place-items-center text-muted-foreground opacity-0 transition-opacity group-hover/note:opacity-100 hover:text-foreground focus-visible:opacity-100"
                  onClick={beginEditing}
                  aria-label={copy.addNote}
                  title={copy.addNote}
                >
                  <Pencil className="size-3" />
                </button>
              </div>
            )}
          </div>
          {error && <p className="mt-1 text-xs text-destructive">{error}</p>}
        </article>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-44">
        <ContextMenuItem className="gap-2 px-2 py-2" disabled={!emailToCopy} onClick={() => void handleCopyEmail()}>
          <CopyIcon className="size-4" />
          {copy.copyEmail}
        </ContextMenuItem>
        <ContextMenuItem className="gap-2 px-2 py-2" onClick={onMarkUnread}>
          <Mail className="size-4" />
          {copy.markUnread}
        </ContextMenuItem>
        <ContextMenuItem variant="destructive" className="gap-2 px-2 py-2" onClick={onDelete}>
          <Trash2 className="size-4" />
          {copy.deleteConversation}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

export function ChatPanel({
  copy,
  detail,
  loading,
  error,
  onBack,
  onReply,
  onReplyAll,
  onNewMail,
  onRetrySend,
  onReEdit,
  onConversationEmpty,
  className,
}: {
  copy: Copy;
  detail?: ConversationDetail;
  loading: boolean;
  error: Error | null;
  onBack: () => void;
  onReply: (conversation: ConversationDetail, message: ConversationMessage, suggestedBody?: string) => void;
  onReplyAll: (conversation: ConversationDetail, message: ConversationMessage, suggestedBody?: string) => void;
  onNewMail: (conversation: ConversationDetail, message: ConversationMessage) => void;
  onRetrySend?: (message: ConversationMessage) => void;
  onReEdit?: (message: ConversationMessage) => void;
  onConversationEmpty: () => void;
  className?: string;
}) {
  const panelClass = cn("min-w-0 flex-1 flex-col bg-surface", className);
  if (loading && !detail)
    return (
      <section className={cn(panelClass, "items-center justify-center")}>
        <div className="text-sm text-muted-foreground">{copy.loading}</div>
      </section>
    );
  if (error && !detail)
    return (
      <section className={panelClass}>
        <ErrorState copy={copy} />
      </section>
    );
  if (!detail)
    return (
      <section className={panelClass}>
        <EmptyState icon={<MessageCircle />} text={copy.selectConversation} />
      </section>
    );
  return (
    <ChatView
      copy={copy}
      detail={detail}
      onBack={onBack}
      onReply={(message, suggestedBody) => onReply(detail, message, suggestedBody)}
      onReplyAll={(message, suggestedBody) => onReplyAll(detail, message, suggestedBody)}
      onNewMail={(message) => onNewMail(detail, message)}
      onRetrySend={onRetrySend}
      onReEdit={onReEdit}
      onConversationEmpty={onConversationEmpty}
    />
  );
}

export function ChatView({
  copy,
  detail,
  onBack,
  onReply,
  onReplyAll,
  onNewMail,
  onRetrySend,
  onReEdit,
  onConversationEmpty,
}: {
  copy: Copy;
  detail: ConversationDetail;
  onBack: () => void;
  onReply: (message: ConversationMessage, suggestedBody?: string) => void;
  onReplyAll: (message: ConversationMessage, suggestedBody?: string) => void;
  onNewMail: (message: ConversationMessage) => void;
  onRetrySend?: (message: ConversationMessage) => void;
  onReEdit?: (message: ConversationMessage) => void;
  onConversationEmpty: () => void;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const queryClient = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<ConversationMessage | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const latestConversationMessage = detail.messages.at(-1);
  const persistedSuggestionMessage =
    latestConversationMessage &&
    !latestConversationMessage.outgoing &&
    latestConversationMessage.suggestedReply?.status === "ready" &&
    latestConversationMessage.suggestedReply.text.trim()
      ? latestConversationMessage
      : undefined;
  const persistedSuggestionKey = persistedSuggestionMessage ? `${persistedSuggestionMessage.folder || "INBOX"}\u0000${persistedSuggestionMessage.id}` : null;
  const [suggestionTargetKey, setSuggestionTargetKey] = useState<string | null>(persistedSuggestionKey);
  const [suggestionGeneration, setSuggestionGeneration] = useState(0);
  const aiSettings = useQuery({ queryKey: ["ai-models"], queryFn: getAIModels, retry: false });

  useEffect(() => {
    setSuggestionTargetKey(persistedSuggestionKey);
    setSuggestionGeneration(0);
  }, [detail.id, persistedSuggestionKey]);

  useEffect(() => {
    if (!suggestionTargetKey && persistedSuggestionKey) setSuggestionTargetKey(persistedSuggestionKey);
  }, [persistedSuggestionKey, suggestionTargetKey]);

  const deleteMutation = useMutation({
    mutationFn: (message: ConversationMessage) => {
      if (message.id.startsWith("optimistic-")) {
        removeOptimisticMessage(message.id);
        return Promise.resolve({ ok: true });
      }
      return deleteConversationMessage(detail.id, message.id, message.folder || "INBOX");
    },
    onSuccess: async (_, message) => {
      setDeleteTarget(null);
      setDeleteError("");
      if (message.id.startsWith("optimistic-")) {
        removeOptimisticMessage(message.id);
        return;
      }
      if (detail.messages.length === 1) onConversationEmpty();
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["conversations"] }), queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] })]);
      if (detail.messages.length > 1) await queryClient.invalidateQueries({ queryKey: ["conversation", detail.id] });
    },
    onError: (value) => setDeleteError(value instanceof Error ? value.message : copy.deleteEmailFailed),
  });

  const generateSuggestedReply = (message: ConversationMessage) => {
    setSuggestionTargetKey(`${message.folder || "INBOX"}\u0000${message.id}`);
    setSuggestionGeneration((value) => value + 1);
  };

  useLayoutEffect(() => {
    const scroll = scrollRef.current;
    const content = contentRef.current;
    if (!scroll || !content) return;
    const stickToBottom = { current: true };
    const jumpToLatest = () => {
      const previousBehavior = scroll.style.scrollBehavior;
      scroll.style.scrollBehavior = "auto";
      scroll.scrollTop = scroll.scrollHeight;
      scroll.style.scrollBehavior = previousBehavior;
    };
    const updateStickiness = () => {
      stickToBottom.current = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 80;
    };
    const keepLatestVisible = () => {
      if (stickToBottom.current) jumpToLatest();
    };

    scroll.addEventListener("scroll", updateStickiness, { passive: true });
    const resizeObserver = new ResizeObserver(keepLatestVisible);
    jumpToLatest();
    resizeObserver.observe(content);
    const frame = window.requestAnimationFrame(jumpToLatest);
    return () => {
      window.cancelAnimationFrame(frame);
      scroll.removeEventListener("scroll", updateStickiness);
      resizeObserver.disconnect();
    };
  }, [detail.id, detail.messages.length]);

  return (
    <section data-testid="conversation-detail" className="flex min-w-0 flex-1 flex-col">
      <header className="grid min-h-[4.5rem] grid-cols-[minmax(0,1fr)_minmax(0,2fr)_minmax(0,1fr)] items-center border-b bg-card px-3 py-3 sm:px-5">
        <div className="flex min-w-0 items-center justify-start">
          <Button variant="ghost" size="icon" className="lg:hidden" onClick={onBack} aria-label={copy.cancel} title={copy.cancel}>
            <ArrowLeft />
          </Button>
        </div>
        <div className="min-w-0 text-center">
          <h2 className="truncate text-sm font-semibold">{detail.title || copy.conversations}</h2>
          <p className="mt-1 truncate text-xs text-muted-foreground">
            {detail.subject || copy.noSubject}
            <span className="px-1.5">·</span>
            {detail.count} {copy.messages}
          </p>
        </div>
        <div className="flex min-w-0 items-center justify-end gap-1.5">
          {detail.messages.length > 0 && (
            <>
              <Button
                variant="ghost"
                size="sm"
                className="h-8 gap-1.5 text-xs text-muted-foreground hover:text-foreground"
                onClick={() => onReply(detail.messages.at(-1)!)}
                title={copy.reply}
              >
                <Send className="size-3.5" />
                <span className="hidden sm:inline">{copy.reply}</span>
              </Button>
              {Boolean(detail.messages.at(-1)?.cc?.trim()) && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 gap-1.5 text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => onReplyAll(detail.messages.at(-1)!)}
                  title={copy.replyAll}
                >
                  <ReplyAll className="size-3.5" />
                  <span className="hidden sm:inline">{copy.replyAll}</span>
                </Button>
              )}
            </>
          )}
        </div>
      </header>
      <ScrollArea className="min-h-0 flex-1" viewportClassName="scroll-smooth" contentClassName="px-3 py-6 sm:px-[5vw] sm:py-8" viewportRef={scrollRef}>
        <div ref={contentRef}>
          {detail.messages.map((message, index) => {
            const messageKey = `${message.folder || "INBOX"}\u0000${message.id}`;
            const canGenerateReply = !message.outgoing && Boolean(detail.accountEmail) && Boolean(aiSettings.data?.models.length);
            return (
              <Fragment key={messageKey}>
                <MessageBubble
                  copy={copy}
                  message={message}
                  senderFallback={detail.peerEmail || detail.title}
                  accountEmail={detail.accountEmail}
                  rootRef={scrollRef}
                  eager={index >= detail.messages.length - 3}
                  onReply={() => onReply(message)}
                  onReplyAll={() => onReplyAll(message)}
                  onNewMail={() => onNewMail(message)}
                  onDelete={() => {
                    setDeleteError("");
                    setDeleteTarget(message);
                  }}
                  onGenerateReply={canGenerateReply ? () => generateSuggestedReply(message) : undefined}
                  onRetrySend={onRetrySend ? () => onRetrySend(message) : undefined}
                  onReEdit={onReEdit ? () => onReEdit(message) : undefined}
                />
                {!message.outgoing && suggestionTargetKey === messageKey ? (
                  <SuggestedReplyBubble
                    key={messageKey}
                    copy={copy}
                    detail={detail}
                    message={message}
                    generation={suggestionGeneration}
                    onReply={(body) => onReply(message, body)}
                    onReplyAll={(body) => onReplyAll(message, body)}
                  />
                ) : null}
              </Fragment>
            );
          })}
        </div>
      </ScrollArea>
      <QuickReplyBar
        copy={copy}
        detail={detail}
        onOpenFullReply={(body) => onReply(detail.messages.at(-1)!, body)}
        onOpenFullReplyAll={(body) => onReplyAll(detail.messages.at(-1)!, body)}
      />
      <Dialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) {
            setDeleteTarget(null);
            setDeleteError("");
          }
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{copy.deleteEmailTitle}</DialogTitle>
            <DialogDescription>{copy.deleteEmailDescription}</DialogDescription>
          </DialogHeader>
          {deleteError && <p className="text-sm text-destructive">{deleteError}</p>}
          <DialogFooter>
            <Button variant="ghost" disabled={deleteMutation.isPending} onClick={() => setDeleteTarget(null)}>
              {copy.cancel}
            </Button>
            <Button variant="destructive" disabled={deleteMutation.isPending || !deleteTarget} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>
              <Trash2 />
              {deleteMutation.isPending ? copy.deleting : copy.deleteEmail}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export function SuggestedReplyBubble({
  copy,
  detail,
  message,
  generation,
  onReply,
  onReplyAll,
}: {
  copy: Copy;
  detail: ConversationDetail;
  message: ConversationMessage;
  generation: number;
  onReply: (body: string) => void;
  onReplyAll: (body: string) => void;
}) {
  const queryClient = useQueryClient();
  const [body, setBody] = useState(message.suggestedReply?.text.trim() || "");
  const previousGenerationRef = useRef(generation > 0 ? generation - 1 : generation);
  const suggestion = useMutation({
    mutationFn: () =>
      generateEmail({
        accountEmail: detail.accountEmail || "",
        taskType: "reply_suggestion",
        folder: message.folder || "INBOX",
        messageId: message.id,
        instruction: "Generate a persisted reply suggestion for this received email.",
        subject: message.subject || detail.subject,
        recipients: message.from || detail.peerEmail || "",
      }),
    onSuccess: (result) => {
      const text = result.body.trim();
      setBody(text);
      queryClient.setQueriesData<ConversationDetailResponse>({ queryKey: ["conversation"] }, (current) => {
        if (!current || current.conversation.id !== detail.id) return current;
        const messages = current.conversation.messages.map((item) =>
          item.id === message.id && (item.folder || "INBOX") === (message.folder || "INBOX") ? { ...item, suggestedReply: { text, status: "ready" as const, updatedAt: result.updatedAt } } : item
        );
        return { ...current, conversation: { ...current.conversation, messages } };
      });
    },
  });

  useEffect(() => {
    if (previousGenerationRef.current === generation) return;
    previousGenerationRef.current = generation;
    suggestion.mutate();
  }, [generation]);

  return (
    <article className="mb-5">
      <div className="mb-1 flex justify-end text-right text-xs leading-tight text-muted-foreground">
        <span className="flex items-center gap-1.5 font-medium">
          <Sparkles className="size-3.5" />
          {copy.suggestedReply}
        </span>
      </div>
      <ContextMenu>
        <ContextMenuTrigger className="block select-text">
          <div className="flex justify-end">
            <div className="min-w-0 max-w-[80%] rounded-xl border border-dashed bg-secondary px-3 py-2 text-sm leading-relaxed text-secondary-foreground">
              {suggestion.isPending && !body && (
                <p className="flex items-center gap-2 text-muted-foreground">
                  <Sparkles className="size-4 animate-pulse" />
                  {copy.suggestedReplyGenerating}
                </p>
              )}
              {body && <p className="whitespace-pre-wrap">{body}</p>}
              {suggestion.error && <p className="text-destructive">{suggestion.error instanceof Error ? suggestion.error.message : copy.suggestedReplyFailed}</p>}
            </div>
          </div>
        </ContextMenuTrigger>
        {body && (
          <ContextMenuContent className="w-40">
            {message.cc?.trim() ? (
              <ContextMenuItem className="gap-2 px-2 py-2" onClick={() => onReplyAll(body)}>
                <ReplyAll className="size-4" />
                {copy.replyAll}
              </ContextMenuItem>
            ) : null}
            <ContextMenuItem className="gap-2 px-2 py-2" onClick={() => onReply(body)}>
              <Send className="size-4" />
              {copy.reply}
            </ContextMenuItem>
          </ContextMenuContent>
        )}
      </ContextMenu>
      <div className="mt-1.5 flex min-h-7 justify-end gap-1">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7"
          disabled={suggestion.isPending}
          onClick={() => suggestion.mutate()}
          aria-label={copy.regenerateSuggestedReply}
          title={copy.regenerateSuggestedReply}
        >
          <RotateCcw className={cn("size-3.5", suggestion.isPending && "animate-spin")} />
        </Button>
        {body && (
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => onReply(body)}>
            <Send className="size-3.5" />
            {copy.useSuggestedReply}
          </Button>
        )}
      </div>
    </article>
  );
}

export function MessageBubble({
  copy,
  message,
  senderFallback,
  accountEmail,
  rootRef,
  eager,
  onReply,
  onReplyAll,
  onNewMail,
  onDelete,
  onGenerateReply,
  onRetrySend,
  onReEdit,
}: {
  copy: Copy;
  message: ConversationMessage;
  senderFallback?: string;
  accountEmail?: string;
  rootRef: { current: HTMLDivElement | null };
  eager: boolean;
  onReply: () => void;
  onReplyAll: () => void;
  onNewMail: () => void;
  onDelete: () => void;
  onGenerateReply?: () => void;
  onRetrySend?: () => void;
  onReEdit?: () => void;
}) {
  const sender = message.outgoing ? copy.me : message.fromName || message.from || senderFallback || copy.conversations;
  const split = splitQuotedText(message.body || message.preview || copy.noBody);
  const visibleText = split.visible || (split.quoted ? copy.quotedOnly : copy.noBody);
  const outgoing = message.outgoing;
  const formattedDate = formatTime(message.date);

  return (
    <ContextMenu>
      <ContextMenuTrigger className="mb-5 block select-text">
        <article>
          <div className={cn("mb-1 flex min-w-0 items-start gap-2 text-xs leading-tight text-muted-foreground", outgoing && "justify-end text-right")}>
            <div className="min-w-0">
              <div>
                <span className="font-medium">{sender}</span>
                <time className="ml-2" title={formattedDate}>
                  {formattedDate}
                </time>
              </div>
              <div className="mt-1 max-w-full break-words text-[11px]">
                <span className="font-medium">{copy.to}:</span> {message.to || "-"}
                {message.cc && (
                  <>
                    <span className="mx-1.5">·</span>
                    <span className="font-medium">{copy.cc}:</span> {message.cc}
                  </>
                )}
              </div>
            </div>
          </div>
          <div className={cn("flex items-end gap-2", outgoing && "justify-end")}>
            {outgoing && message.sendStatus === "sending" && (
              <div className="mb-2 flex shrink-0 items-center justify-center self-center text-muted-foreground" title={copy.sending}>
                <Loader2 className="size-4 animate-spin text-muted-foreground/80" />
              </div>
            )}
            {outgoing && message.sendStatus === "failed" && (
              <div className="mb-2 flex shrink-0 items-center justify-center self-center">
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    onRetrySend?.();
                  }}
                  className="group flex size-5 shrink-0 items-center justify-center rounded-full bg-destructive text-destructive-foreground font-bold text-xs leading-none shadow-xs transition-transform hover:scale-110 active:scale-95 focus:outline-hidden cursor-pointer"
                  title={message.sendError ? `${message.sendError} · ${copy.retrySend}` : copy.sendFailedRetry}
                  aria-label={copy.sendFailedRetry}
                >
                  !
                </button>
              </div>
            )}
            <div className={cn("min-w-0 max-w-[80%] overflow-x-auto rounded-xl border border-transparent bg-secondary px-3 py-2 text-sm leading-relaxed text-secondary-foreground", message.html && "w-full")}>
              {message.html ? (
                <EmailHTMLFrame html={message.html} title={message.subject || copy.noSubject} rootRef={rootRef} eager={eager} />
              ) : (
                <div className="whitespace-pre-wrap">{renderLinkifiedText(visibleText)}</div>
              )}
              {!message.html && split.quoted && (
                <details className="mt-2 border-t border-border/60 pt-2 text-muted-foreground">
                  <summary className="flex cursor-pointer list-none items-center gap-1 text-xs [&::-webkit-details-marker]:hidden">
                    <ChevronDown className="size-3.5" />
                    {copy.showQuoted}
                  </summary>
                  <div className="mt-2 border-l-2 border-border pl-2 whitespace-pre-wrap">{renderLinkifiedText(split.quoted)}</div>
                </details>
              )}
              {message.attachments?.length ? (
                <>
                  <Separator className="my-2 opacity-50" />
                  <div className="grid gap-1.5">
                    {message.attachments.map((attachment) => (
                      <a
                        className="flex min-w-0 items-center gap-1.5 text-xs text-primary"
                        key={attachment.id}
                        href={attachment.id.startsWith("opt-att-") ? "#" : `/api/attachment/${encodeURIComponent(attachment.id)}?account_email=${encodeURIComponent(accountEmail || "")}`}
                        onClick={attachment.id.startsWith("opt-att-") ? (e) => e.preventDefault() : undefined}
                      >
                        <Paperclip className="size-3.5 shrink-0" />
                        <span className="min-w-0 truncate">{attachment.filename}</span>
                        <small className="shrink-0 text-muted-foreground">{formatSize(attachment.size)}</small>
                      </a>
                    ))}
                  </div>
                </>
              ) : null}
            </div>
          </div>
          {outgoing && message.sendStatus === "failed" && (
            <div className="mt-1 flex items-center justify-end gap-1.5 text-right text-xs text-destructive">
              <span>{message.sendError || copy.sendFailed}</span>
              {onRetrySend && (
                <button
                  type="button"
                  className="font-medium underline hover:opacity-80 cursor-pointer"
                  onClick={onRetrySend}
                >
                  {copy.retry}
                </button>
              )}
            </div>
          )}
          {!message.id.startsWith("optimistic-") && (
            <MailMessageSummary copy={copy} accountEmail={accountEmail} folder={message.folder || "INBOX"} messageId={message.id} initialSummary={message.mailSummary} outgoing={outgoing} onGenerateReply={onGenerateReply} />
          )}
        </article>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-40">
        {message.sendStatus === "failed" ? (
          <>
            {onRetrySend && (
              <ContextMenuItem className="gap-2 px-2 py-2" onClick={onRetrySend}>
                <RotateCcw className="size-4" />
                {copy.resend}
              </ContextMenuItem>
            )}
            {onReEdit && (
              <ContextMenuItem className="gap-2 px-2 py-2" onClick={onReEdit}>
                <Pencil className="size-4" />
                {copy.reEdit}
              </ContextMenuItem>
            )}
            <ContextMenuItem variant="destructive" className="gap-2 px-2 py-2" onClick={onDelete}>
              <Trash2 className="size-4" />
              {copy.deleteEmail}
            </ContextMenuItem>
          </>
        ) : message.sendStatus === "sending" ? (
          <ContextMenuItem variant="destructive" className="gap-2 px-2 py-2" onClick={onDelete}>
            <Trash2 className="size-4" />
            {copy.cancel}
          </ContextMenuItem>
        ) : (
          <>
            {message.cc?.trim() ? (
              <ContextMenuItem className="gap-2 px-2 py-2" onClick={onReplyAll}>
                <ReplyAll className="size-4" />
                {copy.replyAll}
              </ContextMenuItem>
            ) : null}
            <ContextMenuItem className="gap-2 px-2 py-2" onClick={onReply}>
              <Send className="size-4" />
              {copy.reply}
            </ContextMenuItem>
            <ContextMenuItem className="gap-2 px-2 py-2" onClick={onNewMail}>
              <Mail className="size-4" />
              {copy.sendEmail}
            </ContextMenuItem>
            <ContextMenuItem variant="destructive" className="gap-2 px-2 py-2" onClick={onDelete}>
              <Trash2 className="size-4" />
              {copy.deleteEmail}
            </ContextMenuItem>
          </>
        )}
      </ContextMenuContent>
    </ContextMenu>
  );
}

export function MailMessageSummary({
  copy,
  accountEmail,
  folder,
  messageId,
  initialSummary,
  outgoing = false,
  onGenerateReply,
}: {
  copy: Copy;
  accountEmail?: string;
  folder: string;
  messageId: string;
  initialSummary?: ConversationMessage["mailSummary"];
  outgoing?: boolean;
  onGenerateReply?: () => void;
}) {
  const queryClient = useQueryClient();
  const [savedSummary, setSavedSummary] = useState(initialSummary);
  const mutation = useMutation({
    mutationFn: (regenerate: boolean) => summarizeMailMessage(accountEmail || "", folder, messageId, regenerate),
    onSuccess: (result) => {
      const summary: MailSummary = { text: result.summary, status: result.status, stale: result.stale, updatedAt: result.updatedAt };
      setSavedSummary(summary);
      queryClient.setQueryData<MailMessage>(["message", folder, messageId], (current) => (current ? { ...current, mailSummary: summary } : current));
      queryClient.setQueriesData<ConversationDetailResponse>({ queryKey: ["conversation"] }, (current) => {
        if (!current || (accountEmail && current.conversation.accountEmail !== accountEmail)) return current;
        let changed = false;
        const messages = current.conversation.messages.map((message) => {
          if (message.id !== messageId || (message.folder || "INBOX") !== folder) return message;
          changed = true;
          return { ...message, mailSummary: summary };
        });
        return changed ? { ...current, conversation: { ...current.conversation, messages } } : current;
      });
    },
  });

  useEffect(() => {
    setSavedSummary(initialSummary);
    mutation.reset();
  }, [accountEmail, folder, messageId, initialSummary]);

  return (
    <div className={cn("mt-1.5 max-w-[80%]", outgoing && "ml-auto")}>
      {!savedSummary && (
        <div className="flex min-h-7 items-center gap-1">
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground" disabled={mutation.isPending || !accountEmail} onClick={() => mutation.mutate(false)}>
            <Sparkles className="size-3.5" />
            {mutation.isPending ? copy.summarizing : copy.summarize}
          </Button>
          {onGenerateReply && (
            <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground" onClick={onGenerateReply}>
              <MessageCircle className="size-3.5" />
              {copy.generateSuggestedReply}
            </Button>
          )}
        </div>
      )}
      {savedSummary && (
        <div className="mt-1 border-l-2 border-primary/40 bg-muted/40 px-3 py-2 text-sm leading-relaxed">
          <div className="mb-1 flex items-center justify-between gap-2">
            <div className="flex min-w-0 items-center gap-2">
              <strong className="text-xs font-medium text-muted-foreground">{copy.mailSummaryTitle}</strong>
              {savedSummary.stale && <span className="truncate text-[10px] text-amber-700 dark:text-amber-400">{copy.summaryStale}</span>}
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-6 shrink-0"
              disabled={mutation.isPending || !accountEmail}
              onClick={() => mutation.mutate(true)}
              aria-label={copy.regenerateSummary}
              title={copy.regenerateSummary}
            >
              <RotateCcw className={cn("size-3.5", mutation.isPending && "animate-spin")} />
            </Button>
          </div>
          <p className="whitespace-pre-wrap">{savedSummary.text}</p>
        </div>
      )}
      {savedSummary && onGenerateReply && (
        <Button type="button" variant="ghost" size="sm" className="mt-1 h-7 px-2 text-xs text-muted-foreground" onClick={onGenerateReply}>
          <MessageCircle className="size-3.5" />
          {copy.generateSuggestedReply}
        </Button>
      )}
      {mutation.error && <p className="mt-1 px-2 text-xs text-destructive">{mutation.error instanceof Error ? mutation.error.message : copy.loadFailed}</p>}
    </div>
  );
}

export function EmailHTMLFrame({
  html,
  title,
  rootRef,
  eager,
}: {
  html: string;
  title: string;
  rootRef: { current: HTMLDivElement | null };
  eager: boolean;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [enabled, setEnabled] = useState(eager);

  useEffect(() => {
    if (eager) setEnabled(true);
  }, [eager]);

  useEffect(() => {
    if (enabled) return;
    const host = hostRef.current;
    if (!host || !("IntersectionObserver" in window)) {
      setEnabled(true);
      return;
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;
        setEnabled(true);
        observer.disconnect();
      },
      { root: rootRef.current, rootMargin: "1200px 0px" }
    );
    observer.observe(host);
    return () => observer.disconnect();
  }, [enabled, rootRef]);

  useLayoutEffect(() => {
    if (!enabled) return;
    const frame = frameRef.current;
    if (!frame) return;

    let mounted = true;
    let measureFrame = 0;
    let resizeObserver: ResizeObserver | null = null;
    let mutationObserver: MutationObserver | null = null;
    let resourceCleanup: (() => void) | null = null;
    let documentProbeTimer = 0;
    let lastHeight = 0;

    frame.style.height = "1px";

    const measureNow = () => {
      if (!mounted) return;
      const doc = frame.contentDocument;
      const root = doc?.documentElement;
      const body = doc?.body;
      if (!root || !body) return;

      const height = Math.max(body.scrollHeight, body.offsetHeight, body.getBoundingClientRect().height, 48);
      if (height === lastHeight) return;
      lastHeight = height;
      frame.style.height = `${height}px`;
    };

    const measure = () => {
      if (!mounted) return;
      window.cancelAnimationFrame(measureFrame);
      measureFrame = window.requestAnimationFrame(measureNow);
    };

    const clearDocumentObservers = () => {
      resizeObserver?.disconnect();
      mutationObserver?.disconnect();
      resizeObserver = null;
      mutationObserver = null;
      resourceCleanup?.();
      resourceCleanup = null;
    };

    const observeDocument = (doc: Document) => {
      clearDocumentObservers();

      const installTransparentBackground = () => {
        if (!doc.head || doc.head.querySelector("[data-inbrix-frame-style]")) return;
        const style = doc.createElement("style");
        style.dataset.inbrixFrameStyle = "";
        style.textContent = "html, body { background-color: transparent !important; }";
        doc.head.append(style);
      };

      installTransparentBackground();
      resizeObserver = new ResizeObserver(measure);
      resizeObserver.observe(doc.documentElement);
      if (doc.body) resizeObserver.observe(doc.body);
      mutationObserver = new MutationObserver(() => {
        installTransparentBackground();
        measure();
      });
      mutationObserver.observe(doc, { childList: true, subtree: true, characterData: true });

      const onResource = (event: Event) => {
        if (event.target instanceof HTMLImageElement) measure();
      };
      const onContextMenu = (event: MouseEvent) => {
        const target = event.target as Element | null;
        if (target?.closest?.("img")) return;

        event.preventDefault();
        const frameRect = frame.getBoundingClientRect();
        frame.dispatchEvent(
          new MouseEvent("contextmenu", {
            bubbles: true,
            cancelable: true,
            clientX: frameRect.left + event.clientX,
            clientY: frameRect.top + event.clientY,
            button: 2,
            buttons: 2,
          })
        );
      };
      doc.addEventListener("load", onResource, true);
      doc.addEventListener("error", onResource, true);
      doc.addEventListener("contextmenu", onContextMenu);
      resourceCleanup = () => {
        doc.removeEventListener("load", onResource, true);
        doc.removeEventListener("error", onResource, true);
        doc.removeEventListener("contextmenu", onContextMenu);
      };
      void doc.fonts?.ready.then(measure, measure);
      measure();
    };

    const doc = frame.contentDocument;
    if (!doc) return clearDocumentObservers;
    doc.open();
    doc.write(html);
    doc.close();
    observeDocument(doc);
    measureNow();

    let probePasses = 0;
    const probeDocument = () => {
      if (!mounted) return;
      const current = frame.contentDocument;
      if (current && current !== doc) {
        observeDocument(current);
      }
      measureNow();
      probePasses += 1;
      if (mounted && probePasses < 20) {
        documentProbeTimer = window.setTimeout(probeDocument, 50);
      }
    };
    probeDocument();

    return () => {
      mounted = false;
      window.cancelAnimationFrame(measureFrame);
      window.clearTimeout(documentProbeTimer);
      clearDocumentObservers();
    };
  }, [enabled, html]);

  return (
    <div ref={hostRef} className="min-h-12 w-full min-w-0">
      {enabled && (
        <iframe
          ref={frameRef}
          className="block w-full min-w-0 border-0 bg-transparent"
          scrolling="auto"
          sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
          title={title}
        />
      )}
    </div>
  );
}

export function MailDetail({ copy, message }: { copy: Copy; message: MailMessage }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  return (
    <article ref={scrollRef} className="mx-auto min-w-0 max-w-4xl">
      <div className="mb-1 flex items-baseline gap-2 text-xs text-muted-foreground">
        <span className="font-medium">{message.fromName || message.from}</span>
        <time>{formatTime(message.date)}</time>
      </div>
      <div className="min-w-0 max-w-[80%] overflow-x-auto rounded-xl border bg-background px-3 py-2 text-sm leading-relaxed">
        {message.html ? (
          <EmailHTMLFrame html={message.html} title={message.subject || copy.noSubject} rootRef={scrollRef} eager />
        ) : (
          <div className="whitespace-pre-wrap">{renderLinkifiedText(message.body || message.preview || copy.noBody)}</div>
        )}
        {message.attachments?.length ? (
          <>
            <Separator className="my-2 opacity-50" />
            <div className="grid gap-1.5">
              {message.attachments.map((item) => (
                <a
                  className="flex min-w-0 items-center gap-1.5 text-xs text-primary"
                  key={item.id || item.partId}
                  href={`/api/attachment/${encodeURIComponent(item.id)}?account_email=${encodeURIComponent(message.accountEmail || "")}`}
                >
                  <Paperclip className="size-3.5 shrink-0" />
                  <span className="truncate">{item.filename}</span>
                  <small className="shrink-0 text-muted-foreground">{formatSize(item.size)}</small>
                </a>
              ))}
            </div>
          </>
        ) : null}
      </div>
      <MailMessageSummary copy={copy} accountEmail={message.accountEmail} folder={message.folder || "INBOX"} messageId={message.id} initialSummary={message.mailSummary} />
    </article>
  );
}

export function QuickReplyBar({
  copy,
  detail,
  onOpenFullReply,
  onOpenFullReplyAll,
}: {
  copy: Copy;
  detail: ConversationDetail;
  onOpenFullReply: (suggestedBody?: string) => void;
  onOpenFullReplyAll: (suggestedBody?: string) => void;
}) {
  const [text, setText] = useState("");
  const queryClient = useQueryClient();
  const latestMessage = detail.messages.filter((m) => !m.id.startsWith("optimistic-")).at(-1) || detail.messages.at(-1);
  const hasCc = Boolean(latestMessage?.cc?.trim());

  if (!latestMessage) return null;

  const handleSend = async (replyAll = false) => {
    const trimmed = text.trim();
    if (!trimmed || !latestMessage) return;

    const source = latestMessage;
    const subject = source.subject || detail.subject;
    const replySubject = subject.toLowerCase().startsWith("re:") ? subject : `Re: ${subject}`;
    const accountEmail = detail.accountEmail || "";

    let toRecipients: string[] = [];
    let ccRecipients: string[] = [];
    if (replyAll) {
      const originalTo = splitRecipientValues(source.to);
      const originalCc = splitRecipientValues(source.cc || "");
      toRecipients = source.outgoing
        ? uniqueRecipients(originalTo, [accountEmail])
        : uniqueRecipients([source.from], [accountEmail]);
      ccRecipients = uniqueRecipients([...originalTo, ...originalCc], [accountEmail, ...toRecipients]);
    } else {
      const recipient = source.outgoing ? source.to : source.from || detail.peerEmail || "";
      toRecipients = [recipient];
    }

    const sender = source.fromName && source.from ? `${source.fromName} <${source.from}>` : source.from || detail.peerEmail || "";
    const quoteLead = `On ${new Date(source.date || Date.now()).toLocaleString()}, ${sender} wrote:`;
    const originalBody = source.body || source.preview || "";
    const htmlBody = `<p>${escapeHTML(trimmed).replace(/\n/g, "<br>")}</p><p>${escapeHTML(quoteLead)}</p><blockquote>${structuredQuotedTextToHTML(originalBody)}</blockquote>`;

    const form = new FormData();
    form.set("to", toRecipients.join(", "));
    if (ccRecipients.length) form.set("cc", ccRecipients.join(", "));
    form.set("subject", replySubject);
    form.set("body", trimmed);
    form.set("html_body", htmlBody);
    if (source.messageId) form.set("in_reply_to", source.messageId);
    const references = [...(source.references || [])];
    if (source.messageId && !references.includes(source.messageId)) references.push(source.messageId);
    if (references.length) form.set("references", references.join(" "));
    form.set("conversation_id", detail.id);
    if (accountEmail) form.set("account_email", accountEmail);

    const optimisticId = `optimistic-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
    const optimisticMessage: OptimisticMessage = {
      id: optimisticId,
      conversationId: detail.id,
      from: accountEmail,
      to: toRecipients.join(", "),
      cc: ccRecipients.length ? ccRecipients.join(", ") : undefined,
      subject: replySubject,
      preview: trimmed.slice(0, 200),
      body: trimmed,
      html: htmlBody,
      date: new Date().toISOString(),
      hasAttachments: false,
      outgoing: true,
      sendStatus: "sending",
      form,
      createdAt: Date.now(),
    };

    addOptimisticMessage(optimisticMessage);
    setText("");

    queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) => {
      if (!current) return current;
      return {
        ...current,
        conversations: current.conversations.map((item) =>
          item.id === detail.id
            ? {
                ...item,
                preview: trimmed.slice(0, 100),
                date: optimisticMessage.date,
                status: "answered",
              }
            : item
        ),
      };
    });

    try {
      await sendMessage(form);
      updateOptimisticStatus(optimisticId, "sent");
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
      void queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] });
      void queryClient.invalidateQueries({ queryKey: ["conversation", detail.id] });
    } catch (err) {
      const msg = err instanceof Error ? err.message : copy.sendFailed;
      updateOptimisticStatus(optimisticId, "failed", msg);
      toast.error(msg);
    }
  };

  return (
    <footer className="border-t bg-card px-3 py-2.5 sm:px-5">
      <div className="flex items-end gap-2">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
              e.preventDefault();
              void handleSend(false);
            }
          }}
          placeholder={copy.quickReply}
          rows={1}
          className="min-h-9 max-h-32 flex-1 resize-none rounded-lg border border-input bg-transparent px-3 py-2 text-sm leading-5 placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        />
        <div className="flex shrink-0 items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-9 text-muted-foreground hover:text-foreground"
            onClick={() => onOpenFullReply(text.trim() || undefined)}
            title={copy.reply}
            aria-label={copy.reply}
          >
            <Pencil className="size-4" />
          </Button>
          {hasCc && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-9 text-muted-foreground hover:text-foreground"
              onClick={() => onOpenFullReplyAll(text.trim() || undefined)}
              title={copy.replyAll}
              aria-label={copy.replyAll}
            >
              <ReplyAll className="size-4" />
            </Button>
          )}
          {hasCc && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-9 px-2.5 text-xs gap-1"
              disabled={!text.trim()}
              onClick={() => void handleSend(true)}
              title={copy.replyAll}
            >
              <ReplyAll className="size-3.5" />
              <span className="hidden sm:inline">{copy.replyAll}</span>
            </Button>
          )}
          <Button
            type="button"
            size="sm"
            className="h-9 px-3 gap-1.5"
            disabled={!text.trim()}
            onClick={() => void handleSend(false)}
          >
            <Send className="size-3.5" />
            <span>{copy.send}</span>
          </Button>
        </div>
      </div>
    </footer>
  );
}
