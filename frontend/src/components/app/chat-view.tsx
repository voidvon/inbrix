import { Fragment, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bot,
  Check,
  ChevronDown,
  Copy as CopyIcon,
  Languages,
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
import { toast, toastError } from "../../lib/toast";
import { ConversationAIChatPanel } from "./conversation-ai-chat-panel";
import { AttachmentPreviewDialog, mirroredAttachmentDownloadURL, mirroredAttachmentPreviewURL, type AttachmentPreviewTarget } from "./attachment-preview";
import { MailSearchResults, type MailSearchState } from "./mail-search-results";
import { ResizeHandle, usePersistedPaneWidth } from "./resize-handle";
import { ConversationStatusTag, type EditableConversationStatus } from "./conversation-status-tag";
import { ComposeDialog } from "./compose-dialog";
import { ThreadDraftEntry } from "./thread-draft-entry";
import { ConversationNoteEntry } from "./conversation-note-entry";
import { useSaveConversationNote } from "../../lib/conversation-note";
import { useComposeDraft } from "../../lib/compose-draft";
import { useShell } from "./shell-context";
import { ListSkeleton, ErrorState, EmptyState } from "./common-states";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "../ui/context-menu";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { ScrollArea } from "../ui/scroll-area";
import { Separator } from "../ui/separator";
import { Switch } from "../ui/switch";
import {
  deleteConversationMessage,
  deleteOutgoing,
  outgoingAttachmentURL,
  generateEmail,
  getAIModels,
  saveConversationStatus,
  summarizeMailMessage,
  translateMailMessage,
} from "../../lib/api";
import { cn, formatSize, formatTime, splitQuotedText } from "../../lib/utils";
import {
  canReplyAll,
  copyToClipboard,
  htmlToPlainText,
  extractEmailAddress,
  renderLinkifiedText,
} from "../../lib/email-format";
import type {
  ConversationDetail,
  ConversationDetailResponse,
  ConversationListResponse,
  ConversationMessage,
  ConversationSummary,
  MailMessage,
  MailSummary,
  MailTranslation,
} from "../../types";
import { type Copy, zh } from "../../lib/locale";
import { AIStatusIndicator } from "./ai-status-indicator";
import { useAIStateMachine } from "../../lib/ai-state-machine";
import { captureTranscriptAnchor, preserveTranscriptScroll, registerTranscriptScroll, transcriptAnchorTop, type TranscriptScrollAnchor } from "../../lib/transcript-scroll";

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
  onLoadMore,
  hasMore = false,
  loadingMore = false,
  loadMoreError,
  showAllConversations,
  onShowAllChange,
  desktopWidth,
  className,
  searchState,
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
  onLoadMore: () => void | Promise<void>;
  hasMore?: boolean;
  loadingMore?: boolean;
  loadMoreError?: Error | null;
  showAllConversations: boolean;
  onShowAllChange: (value: boolean) => void;
  desktopWidth?: number;
  className?: string;
  /** When set, a non-empty search box renders message-level results instead of conversations. */
  searchState?: MailSearchState;
}) {
  const filterLabelId = useId();
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const allRows = data?.conversations || [];
  const rows = showAllConversations
    ? allRows
    : allRows.filter((conversation) => conversation.status === "unanswered" || conversation.status === "draft");
  const hasData = Boolean(data);
  const searching = Boolean(search.trim()) && Boolean(searchState);

  // The sentinel loads the next page before it reaches the bottom. The explicit
  // button below is the accessible fallback when IntersectionObserver is not
  // available.
  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node || !hasMore || searching || loadMoreError) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) void onLoadMore();
    }, { rootMargin: "400px" });
    observer.observe(node);
    return () => observer.disconnect();
  }, [hasMore, onLoadMore, rows.length, searching, loadMoreError]);

  // The default filter hides answered threads. A page can therefore contain no
  // visible row even though later pages do; keep pulling until one matches or
  // the server says there is nothing left.
  useEffect(() => {
    if (!hasData || searching || !hasMore || rows.length > 0 || loadingMore || loadMoreError) return;
    void onLoadMore();
  }, [hasData, searching, hasMore, rows.length, loadingMore, loadMoreError, onLoadMore]);

  return (
    <section data-testid="conversation-list" style={{ "--conversation-list-width": `${desktopWidth || 370}px` } as React.CSSProperties} className={cn("min-w-0 flex-1 flex-col border-r bg-card lg:w-(--conversation-list-width) lg:flex-none", className)}>
      <div className="flex h-14 shrink-0 items-center border-b bg-card px-3">
        <div className="flex w-full items-center gap-2">
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
              placeholder={copy.searchHint}
              aria-label={copy.search}
            />
            {search && (
              <Button variant="ghost" size="icon" className="absolute right-1 size-7" onClick={() => onSearch("")} aria-label={copy.cancel} title={copy.cancel}>
                <X />
              </Button>
            )}
          </div>
          <label className="flex shrink-0 cursor-pointer items-center gap-1.5 text-xs text-muted-foreground" title={copy.showAllConversationsHint}>
            <span id={filterLabelId}>{copy.showAllConversations}</span>
            <Switch checked={showAllConversations} onCheckedChange={onShowAllChange} aria-labelledby={filterLabelId} />
          </label>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1" contentClassName="pb-[max(2rem,env(safe-area-inset-bottom))]">
        {searching && searchState ? (
          <MailSearchResults copy={copy} state={searchState} />
        ) : (
          <>
            {!hasData && (loading || !error) && <ListSkeleton />}
            {!hasData && error && <ErrorState copy={copy} onRetry={onRefresh} />}
            {hasData && rows.length === 0 && !hasMore && <EmptyState icon={<MessageCircle />} text={copy.noConversations} />}
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
            {hasData && hasMore && (
              <div ref={loadMoreRef} className="flex items-center justify-center px-3 py-4">
                <Button
                  variant="ghost"
                  size="sm"
                  className="w-full text-muted-foreground"
                  disabled={loadingMore}
                  onClick={() => void onLoadMore()}
                >
                  {loadingMore ? <Loader2 className="size-4 animate-spin" /> : loadMoreError ? copy.retry : copy.loadMore}
                </Button>
              </div>
            )}
          </>
        )}
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
  const [statusSaving, setStatusSaving] = useState(false);
  const noteMutation = useSaveConversationNote(conversation.id);
  const cancelRef = useRef(false);

  useEffect(() => {
    if (!editing) setDraft(conversation.note || "");
  }, [conversation.note, editing]);

  const beginEditing = () => {
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
      await noteMutation.mutateAsync(note);
    } catch {
      setDraft(note);
      toast.error(copy.noteSaveFailed);
      setEditing(true);
      window.requestAnimationFrame(() => inputRef.current?.focus());
    }
  };

  const cancelEditing = () => {
    cancelRef.current = true;
    setDraft(conversation.note || "");
    setEditing(false);
    inputRef.current?.blur();
  };

  const changeStatus = async (status: EditableConversationStatus) => {
    if (status === conversation.status) return;
    setStatusSaving(true);
    try {
      await saveConversationStatus(conversation.id, status);
      queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) =>
        current
          ? {
              ...current,
              conversations: current.conversations.map((item) => (item.id === conversation.id ? { ...item, status } : item)),
            }
          : current
      );
    } catch {
      toast.error(copy.statusSaveFailed);
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
          onClick={(event) => {
            const target = event.target;
            // Fill the gaps between controls and the row's outer padding.
            // Buttons/inputs handle themselves; portaled menus belong to the
            // row in React's event tree but must never select a conversation.
            if (event.defaultPrevented || !(target instanceof Element) || !event.currentTarget.contains(target)) return;
            if (target.closest("button, a, input, textarea, select, [contenteditable=true]")) return;
            onClick();
          }}
          className={cn(
            "relative w-full max-w-full cursor-pointer overflow-hidden border-b bg-card px-4 py-3 transition-colors hover:bg-muted",
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
              labels={{ answered: copy.answered, unanswered: copy.unanswered, no_action: copy.noAction, draft: copy.draftBadge }}
              draftHint={copy.draftStatusHint}
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

type TranscriptPaginationProps = {
  onLoadOlder?: () => void | Promise<void>;
  loadingOlder?: boolean;
  loadOlderError?: Error | null;
  onLoadNewer?: () => void | Promise<void>;
  loadingNewer?: boolean;
  loadNewerError?: Error | null;
  onGoLatest?: () => Promise<boolean>;
  loadingLatest?: boolean;
};

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
  focusTarget,
  onLoadOlder,
  loadingOlder,
  loadOlderError,
  onLoadNewer,
  loadingNewer,
  loadNewerError,
  onGoLatest,
  loadingLatest,
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
  /** Message to scroll to and highlight once the conversation is on screen. */
  focusTarget?: MailFocusTarget | null;
} & TranscriptPaginationProps) {
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
      className={className}
      focusTarget={focusTarget}
      onLoadOlder={onLoadOlder}
      loadingOlder={loadingOlder}
      loadOlderError={loadOlderError}
      onLoadNewer={onLoadNewer}
      loadingNewer={loadingNewer}
      loadNewerError={loadNewerError}
      onGoLatest={onGoLatest}
      loadingLatest={loadingLatest}
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

/** A message a conversation should open at, e.g. a full-text search hit. */
export type MailFocusTarget = { folder: string; id: string };

/** How long a jumped-to message stays highlighted. */
const MESSAGE_HIGHLIGHT_MS = 3000;

export function messageKeyOf(message: { folder?: string; id: string }) {
  return `${message.folder || "INBOX"}\u0000${message.id}`;
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
  className,
  focusTarget,
  onLoadOlder,
  loadingOlder = false,
  loadOlderError,
  onLoadNewer,
  loadingNewer,
  loadNewerError,
  onGoLatest,
  loadingLatest,
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
  className?: string;
  focusTarget?: MailFocusTarget | null;
} & TranscriptPaginationProps) {
  const [awayFromBottom, setAwayFromBottom] = useState(false);
  const [latestJump, setLatestJump] = useState(0);
  const jumpLatestPending = useRef(false);
  const focusedNavigation = useRef("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);
  const scrollAnchor = useRef<TranscriptScrollAnchor | null>(null);
  const programmaticTop = useRef(-1);
  const userScrollDelta = useRef(0);
  const syncScrollRef = useRef<() => void>(() => {});
  const queryClient = useQueryClient();
  const { compose, openCompose, closeCompose, accountEmail: shellAccountEmail } = useShell();
  // The compose window resolves its own account the same way, so both sides of
  // the draft query agree on the key and share one fetch.
  const draftAccountEmail = detail.accountEmail || shellAccountEmail || "";
  const composeOpenHere = compose.open && compose.defaults.conversationId === detail.id;
  const conversationDraft = useComposeDraft(draftAccountEmail, detail.id, { enabled: Boolean(detail.id) });
  const openReplyToLatest = () => {
    const draft = conversationDraft.data;
    if (draft) {
      openCompose({
        accountEmail: draftAccountEmail, conversationId: detail.id,
        conversation: detail.messages, to: draft.to, cc: draft.cc, subject: draft.subject,
        inReplyTo: draft.inReplyTo || undefined,
        references: draft.references.trim().split(/\s+/).filter(Boolean),
      });
      return;
    }
    const latest = detail.messages.at(-1);
    if (latest) onReply(latest);
  };
  // An answer from the AI panel lands in the reply draft at the bottom of the
  // transcript — the same editor Reply opens, and the same row the server keeps
  // — instead of in a second compose surface of its own.
  const writeDraftToLatest = (body: string) => {
    const latest = detail.messages.at(-1);
    if (!latest) return;
    onReply(latest, body);
  };
  const [deleteTarget, setDeleteTarget] = useState<ConversationMessage | null>(null);
  const [isAIChatOpen, setIsAIChatOpen] = useState(false);
  const [focusedMessageIds, setFocusedMessageIds] = useState<string[]>([]);
  const [highlightedMessageKey, setHighlightedMessageKey] = useState<string | null>(null);
  const [aiPanelWidth, setAiPanelWidth] = usePersistedPaneWidth("inbrix-ai-chat-panel-width", 384, 300, 800);
  const focusKey = focusTarget ? messageKeyOf(focusTarget) : null;
  // While a search hit is focused, the transcript must not keep snapping to the
  // newest message: the reader asked to land on a specific one.
  const focusActiveRef = useRef(Boolean(focusKey));
  focusActiveRef.current = Boolean(focusKey && focusedNavigation.current !== `${detail.id}\u0000${focusKey}`);
  const hasNewerRef = useRef(Boolean(detail.hasNewer));
  hasNewerRef.current = Boolean(detail.hasNewer);

  useEffect(() => {
    setFocusedMessageIds([]);
  }, [detail.id]);

  const handleAddToAIChat = (message: ConversationMessage) => {
    setIsAIChatOpen(true);
    setFocusedMessageIds((prev) => {
      if (prev.includes(message.id)) return prev;
      return [...prev, message.id];
    });
  };

  const handleRemoveFocusedMessage = (id: string) => {
    setFocusedMessageIds((prev) => prev.filter((mId) => mId !== id));
  };

  const handleClearFocusedMessages = () => {
    setFocusedMessageIds([]);
  };

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
      if (message.folder === "Outbox") return deleteOutgoing(message.id);
      return deleteConversationMessage(detail.id, message.id, message.folder || "INBOX");
    },
    onSuccess: async () => {
      setDeleteTarget(null);
      if (detail.count === 1) onConversationEmpty();
      await queryClient.invalidateQueries({ queryKey: ["conversations"] });
      if (detail.count > 1) await queryClient.invalidateQueries({ queryKey: ["conversation", detail.id] });
    },
    onError: (value) => toastError(value, copy.deleteEmailFailed),
  });

  const generateSuggestedReply = (message: ConversationMessage) => {
    setSuggestionTargetKey(`${message.folder || "INBOX"}\u0000${message.id}`);
    setSuggestionGeneration((value) => value + 1);
  };

  useLayoutEffect(() => {
    const scroll = scrollRef.current;
    const content = contentRef.current;
    if (!scroll || !content) return;
    stickToBottom.current = !focusActiveRef.current && !detail.hasNewer;
    scrollAnchor.current = null;
    programmaticTop.current = -1;
    const jumpToLatest = () => {
      const previousBehavior = scroll.style.scrollBehavior;
      scroll.style.scrollBehavior = "auto";
      scroll.scrollTop = scroll.scrollHeight;
      programmaticTop.current = scroll.scrollTop;
      scroll.style.scrollBehavior = previousBehavior;
    };
    const captureAnchor = () => {
      const top = scroll.getBoundingClientRect().top;
      const anchor = Array.from(content.querySelectorAll<HTMLElement>("[data-message-key]")).find(
        (element) => element.getBoundingClientRect().bottom > top
      );
      scrollAnchor.current = anchor ? captureTranscriptAnchor(scroll, anchor) : null;
      programmaticTop.current = scroll.scrollTop;
    };
    const updateStickiness = () => {
      // Restoring the viewport can expose part of a newly prepended bubble.
      // Keep the original anchor until the reader actually scrolls, so a late
      // iframe resize cannot replace it with that bubble and shift the page.
      userScrollDelta.current = scroll.scrollTop - programmaticTop.current;
      if (userScrollDelta.current === 0) return;
      syncScroll();
      captureAnchor();
    };
    const syncScroll = () => {
      // Wheel/touch scrolling can move the viewport before its scroll event is
      // delivered. Carry that movement into the stored anchor before a resize
      // correction, otherwise a late image load cancels the reader's input.
      const delta = programmaticTop.current >= 0 ? scroll.scrollTop - programmaticTop.current : 0;
      // Upward movement always leaves follow mode. Re-enter only at the actual
      // bottom (allowing pixel rounding), so small wheel steps are never undone.
      if (delta < 0) stickToBottom.current = false;
      else if (delta > 0) stickToBottom.current = !hasNewerRef.current && scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight <= 2;
      if (scrollAnchor.current) {
        scrollAnchor.current.top -= delta;
      }
      if (stickToBottom.current && !focusActiveRef.current && !hasNewerRef.current) {
        jumpToLatest();
        captureAnchor();
      } else if (scrollAnchor.current) {
        const anchor = Array.from(content.querySelectorAll<HTMLElement>("[data-message-key]")).find(
          (element) => element.dataset.messageKey === scrollAnchor.current?.key
        );
        if (anchor) {
          scroll.scrollTop += transcriptAnchorTop(scroll, anchor, scrollAnchor.current) - scrollAnchor.current.top;
          programmaticTop.current = scroll.scrollTop;
        } else captureAnchor();
      } else captureAnchor();
      programmaticTop.current = scroll.scrollTop;
      setAwayFromBottom(scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight > 80);
    };
    syncScrollRef.current = syncScroll;
    const unregisterScroll = registerTranscriptScroll(scroll, syncScroll);

    // Release following on upward input before the browser scrolls, including
    // when a frame resizes before the resulting scroll event is delivered.
    const onWheel = (event: WheelEvent) => {
      if (event.deltaY < 0 && !event.ctrlKey) stickToBottom.current = false;
    };
    let touchY = 0;
    const onTouchStart = (event: TouchEvent) => { touchY = event.touches[0]?.clientY || 0; };
    const onTouchMove = (event: TouchEvent) => {
      const nextY = event.touches[0]?.clientY || 0;
      if (nextY > touchY) stickToBottom.current = false;
      touchY = nextY;
    };

    scroll.addEventListener("scroll", updateStickiness, { passive: true });
    scroll.addEventListener("wheel", onWheel, { passive: true });
    scroll.addEventListener("touchstart", onTouchStart, { passive: true });
    scroll.addEventListener("touchmove", onTouchMove, { passive: true });
    const resizeObserver = new ResizeObserver(syncScroll);
    if (!focusActiveRef.current) jumpToLatest();
    captureAnchor();
    resizeObserver.observe(content);
    const frame = window.requestAnimationFrame(() => {
      syncScroll();
    });
    return () => {
      window.cancelAnimationFrame(frame);
      scroll.removeEventListener("scroll", updateStickiness);
      scroll.removeEventListener("wheel", onWheel);
      scroll.removeEventListener("touchstart", onTouchStart);
      scroll.removeEventListener("touchmove", onTouchMove);
      resizeObserver.disconnect();
      unregisterScroll();
      syncScrollRef.current = () => {};
    };
  }, [detail.id]);

  useLayoutEffect(() => {
    syncScrollRef.current();
  }, [detail.messages]);

  // Leave a viewport of runway for fetching/rendering before trackpad inertia
  // reaches the top. Only upward input loads history, so a short initial page
  // must not automatically drain the whole conversation.
  useEffect(() => {
    const scroll = scrollRef.current;
    if (!scroll || !detail.hasMore || !onLoadOlder || loadingOlder || loadOlderError) return;
    const load = () => {
      const preloadDistance = Math.max(600, scroll.clientHeight);
      if (scroll.scrollTop > preloadDistance) return;
      stickToBottom.current = false;
      void onLoadOlder();
    };
    const onScroll = () => {
      if (userScrollDelta.current < 0) load();
    };
    const onWheel = (event: WheelEvent) => { if (event.deltaY < 0 && !event.ctrlKey) load(); };
    let touchY = 0;
    const onTouchStart = (event: TouchEvent) => { touchY = event.touches[0]?.clientY || 0; };
    const onTouchMove = (event: TouchEvent) => {
      const nextY = event.touches[0]?.clientY || 0;
      if (nextY > touchY + 5) load();
      touchY = nextY;
    };
    scroll.addEventListener("scroll", onScroll, { passive: true });
    scroll.addEventListener("wheel", onWheel, { passive: true });
    scroll.addEventListener("touchstart", onTouchStart, { passive: true });
    scroll.addEventListener("touchmove", onTouchMove, { passive: true });
    return () => {
      scroll.removeEventListener("scroll", onScroll);
      scroll.removeEventListener("wheel", onWheel);
      scroll.removeEventListener("touchstart", onTouchStart);
      scroll.removeEventListener("touchmove", onTouchMove);
    };
  }, [detail.id, detail.hasMore, onLoadOlder, loadingOlder, loadOlderError]);

  // A middle window also extends forwards. As with older pages, request with
  // a screen of runway and only after downward input; appending must not enable
  // follow mode while there are still newer pages outside the window.
  useEffect(() => {
    const scroll = scrollRef.current;
    if (!scroll || !detail.hasNewer || !onLoadNewer || loadingNewer || loadNewerError) return;
    const load = () => {
      if (scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight > Math.max(600, scroll.clientHeight)) return;
      stickToBottom.current = false;
      void onLoadNewer();
    };
    const onScroll = () => { if (userScrollDelta.current > 0) load(); };
    const onWheel = (event: WheelEvent) => { if (event.deltaY > 0 && !event.ctrlKey) load(); };
    let touchY = 0;
    const onTouchStart = (event: TouchEvent) => { touchY = event.touches[0]?.clientY || 0; };
    const onTouchMove = (event: TouchEvent) => {
      const nextY = event.touches[0]?.clientY || 0;
      if (nextY < touchY - 5) load();
      touchY = nextY;
    };
    scroll.addEventListener("scroll", onScroll, { passive: true });
    scroll.addEventListener("wheel", onWheel, { passive: true });
    scroll.addEventListener("touchstart", onTouchStart, { passive: true });
    scroll.addEventListener("touchmove", onTouchMove, { passive: true });
    return () => {
      scroll.removeEventListener("scroll", onScroll);
      scroll.removeEventListener("wheel", onWheel);
      scroll.removeEventListener("touchstart", onTouchStart);
      scroll.removeEventListener("touchmove", onTouchMove);
    };
  }, [detail.id, detail.hasNewer, onLoadNewer, loadingNewer, loadNewerError]);

  useLayoutEffect(() => {
    if (!jumpLatestPending.current) return;
    jumpLatestPending.current = false;
    scrollAnchor.current = null;
    programmaticTop.current = -1;
    stickToBottom.current = true;
    syncScrollRef.current();
  }, [latestJump]);

  // The reply editor is appended to the end of the transcript, so opening one
  // has to bring it into view even when the reader had scrolled back.
  useEffect(() => {
    if (!composeOpenHere) return;
    const frame = window.requestAnimationFrame(() => {
      const editor = contentRef.current?.querySelector<HTMLElement>("[data-testid=compose-inline]");
      editor?.scrollIntoView({ block: "end", behavior: "smooth" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [composeOpenHere]);

  // A search hit opens the conversation at the matching message. The bubble is
  // looked up by key rather than a CSS selector so folder names containing
  // quotes or NUL-adjacent characters cannot break the query.
  useEffect(() => {
    if (!focusKey) {
      focusedNavigation.current = "";
      setHighlightedMessageKey(null);
      return;
    }
    const navigation = `${detail.id}\u0000${focusKey}`;
    if (focusedNavigation.current === navigation) return;
    const content = contentRef.current;
    if (!content) return;
    const target = Array.from(content.querySelectorAll<HTMLElement>("[data-message-key]")).find(
      (element) => element.dataset.messageKey === focusKey
    );
    if (!target) return;
    setHighlightedMessageKey(focusKey);
    const frame = window.requestAnimationFrame(() => {
      focusedNavigation.current = navigation;
      focusActiveRef.current = false;
      target.scrollIntoView({ block: "center", behavior: "auto" });
      const scroll = scrollRef.current;
      if (scroll) {
        stickToBottom.current = false;
        programmaticTop.current = scroll.scrollTop;
        scrollAnchor.current = { key: focusKey, top: target.getBoundingClientRect().top - scroll.getBoundingClientRect().top };
      }
    });
    return () => window.cancelAnimationFrame(frame);
  }, [focusKey, detail.id, detail.messages]);

  useEffect(() => {
    if (!highlightedMessageKey) return;
    const timer = window.setTimeout(() => setHighlightedMessageKey(null), MESSAGE_HIGHLIGHT_MS);
    return () => window.clearTimeout(timer);
  }, [highlightedMessageKey]);

  return (
    <div className={cn("relative flex min-w-0 flex-1 overflow-hidden", className)}>
      <section data-testid="conversation-detail" className="relative flex min-w-0 flex-1 flex-col bg-surface">
        <header className="grid h-14 shrink-0 grid-cols-[minmax(0,1fr)_minmax(0,2fr)_minmax(0,1fr)] items-center border-b bg-card px-3 sm:px-5">
          <div className="flex min-w-0 items-center justify-start">
            <Button
              variant="ghost"
              size="icon"
              className="size-8 shrink-0 lg:hidden"
              onClick={onBack}
              aria-label={copy.back}
              title={copy.back}
            >
              <ArrowLeft className="size-4" />
            </Button>
          </div>
          <div className="min-w-0 text-center">
            <h2 className="truncate text-sm font-semibold leading-tight">{detail.title || copy.conversations}</h2>
            <p className="mt-0.5 truncate text-xs text-muted-foreground">
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
                  className={cn(
                    "h-8 gap-1.5 text-xs transition-colors",
                    isAIChatOpen
                      ? "bg-primary/10 text-primary font-medium hover:bg-primary/15 dark:bg-primary/20"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                  onClick={() => setIsAIChatOpen((prev) => !prev)}
                  title={copy.openAIChat}
                >
                  <Bot className="size-3.5" />
                  <span className="hidden sm:inline">{copy.openAIChat}</span>
                </Button>
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
                {canReplyAll(detail.messages.at(-1)) && (
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
        <ScrollArea className="min-h-0 flex-1" viewportClassName="[overflow-anchor:none]" contentClassName="px-3 sm:px-[5vw] pt-4 sm:pt-6 pb-[max(5rem,calc(env(safe-area-inset-bottom)+3.5rem))] sm:pb-8" viewportRef={scrollRef}>
          <div ref={contentRef}>
            {detail.hasMore && onLoadOlder && (
              <div className="flex justify-center pb-4" data-testid="load-older-messages">
                <Button variant="ghost" size="sm" className="text-muted-foreground" disabled={loadingOlder} onClick={() => { stickToBottom.current = false; void onLoadOlder(); }}>
                  {loadingOlder ? <><Loader2 className="size-4 animate-spin" />{copy.loading}</> : loadOlderError ? copy.retry : copy.loadOlderMessages}
                </Button>
              </div>
            )}
            {detail.messages.map((message, index) => {
              const messageKey = messageKeyOf(message);
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
                      setDeleteTarget(message);
                    }}
                    onGenerateReply={canGenerateReply ? () => generateSuggestedReply(message) : undefined}
                    onRetrySend={onRetrySend && message.sendRetryable ? () => onRetrySend(message) : undefined}
                    onReEdit={onReEdit && message.sendRetryable === false && (message.sendStatus === "failed" || message.sendStatus === "unknown") ? () => onReEdit(message) : undefined}
                    onAddToAIChat={() => handleAddToAIChat(message)}
                    isFocused={focusedMessageIds.includes(message.id)}
                    highlighted={highlightedMessageKey === messageKey}
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
            {detail.hasNewer && onLoadNewer && (
              <div className="flex justify-center py-4" data-testid="load-newer-messages">
                <Button variant="ghost" size="sm" className="text-muted-foreground" disabled={loadingNewer} onClick={() => { stickToBottom.current = false; void onLoadNewer(); }}>
                  {loadingNewer ? <><Loader2 className="size-4 animate-spin" />{copy.loading}</> : loadNewerError ? copy.retry : copy.loadNewerMessages}
                </Button>
              </div>
            )}
            {composeOpenHere ? (
              <ComposeDialog
                copy={copy}
                open
                variant="inline"
                defaults={compose.defaults}
                accountEmail={draftAccountEmail}
                onOpenChange={(value) => {
                  if (!value) closeCompose();
                }}
              />
            ) : conversationDraft.data ? (
              <ThreadDraftEntry copy={copy} draft={conversationDraft.data} onOpen={openReplyToLatest} />
            ) : null}
            <ConversationNoteEntry key={detail.id} copy={copy} conversationId={detail.id} note={detail.note || ""} />
          </div>
        </ScrollArea>
        {onGoLatest && (awayFromBottom || detail.hasNewer || focusKey) && (
          <Button
            variant="outline"
            size="sm"
            className="absolute bottom-16 right-4 z-10 rounded-full shadow-md sm:bottom-4"
            data-testid="return-to-latest"
            disabled={loadingLatest}
            onClick={() => { void (async () => {
              try {
                if (!await onGoLatest()) return;
                jumpLatestPending.current = true;
                setLatestJump(value => value + 1);
              } catch (error) {
                toastError(error, copy.loadFailed);
              }
            })(); }}
          >
            {loadingLatest ? <Loader2 className="size-4 animate-spin" /> : <ChevronDown className="size-4" />}
            {copy.returnToLatest}
          </Button>
        )}
        <Dialog
          open={Boolean(deleteTarget)}
          onOpenChange={(open) => {
            if (!open && !deleteMutation.isPending) {
              setDeleteTarget(null);
            }
          }}
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>{copy.deleteEmailTitle}</DialogTitle>
              <DialogDescription>{copy.deleteEmailDescription}</DialogDescription>
            </DialogHeader>
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

      {/* 桌面端右侧侧边 AI 聊天面板 */}
      {isAIChatOpen && (
        <>
          <ResizeHandle
            label="Resize AI chat panel"
            width={aiPanelWidth}
            minWidth={300}
            maxWidth={800}
            side="right"
            onResize={setAiPanelWidth}
          />
          <aside
            style={{ width: `${aiPanelWidth}px` }}
            className="hidden lg:flex shrink-0 flex-col border-l bg-card shadow-xs"
          >
            <ConversationAIChatPanel
              copy={copy}
              detail={detail}
              focusedMessageIds={focusedMessageIds}
              onRemoveFocusedMessage={handleRemoveFocusedMessage}
              onClearFocusedMessages={handleClearFocusedMessages}
              onClose={() => setIsAIChatOpen(false)}
              onWriteDraft={writeDraftToLatest}
            />
          </aside>
        </>
      )}

      {/* 移动端全屏 AI 聊天弹出页面 */}
      {isAIChatOpen && (
        <div className="fixed inset-0 z-50 flex flex-col bg-background lg:hidden">
          <ConversationAIChatPanel
            copy={copy}
            detail={detail}
            isMobile
            focusedMessageIds={focusedMessageIds}
            onRemoveFocusedMessage={handleRemoveFocusedMessage}
            onClearFocusedMessages={handleClearFocusedMessages}
            onClose={() => setIsAIChatOpen(false)}
            onWriteDraft={writeDraftToLatest}
          />
        </div>
      )}
    </div>
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
  const isZh = copy === zh;
  const previousGenerationRef = useRef(generation > 0 ? generation - 1 : generation);
  const replyAI = useAIStateMachine({
    taskType: "reply",
    locale: isZh ? "zh" : "en",
  });

  const suggestion = useMutation({
    mutationFn: () =>
      replyAI.run(() =>
        generateEmail({
          accountEmail: detail.accountEmail || "",
          taskType: "reply_suggestion",
          folder: message.folder || "INBOX",
          messageId: message.id,
          instruction: "Generate a persisted reply suggestion for this received email.",
          subject: message.subject || detail.subject,
          recipients: message.from || detail.peerEmail || "",
        })
      ),
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
    onError: (value) => toastError(value, copy.suggestedReplyFailed),
  });

  useEffect(() => {
    if (previousGenerationRef.current === generation) return;
    previousGenerationRef.current = generation;
    suggestion.mutate();
  }, [generation]);

  // A failed generation is reported by toast; there is no reply body to show.
  if (suggestion.isError && !body) return null;

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
                <div className="py-1">
                  <AIStatusIndicator
                    snapshot={replyAI.snapshot}
                    variant="inline"
                    showTimer
                  />
                </div>
              )}
              {body && <p className="whitespace-pre-wrap">{body}</p>}
            </div>
          </div>
        </ContextMenuTrigger>
        {body && (
          <ContextMenuContent className="w-40">
            {canReplyAll(message) ? (
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

/** Resolve the inline preview and download URLs for a conversation attachment. */
function conversationAttachmentPreviewTarget(
  attachment: { id: string; filename: string; contentType?: string; size?: number },
  folder: string | undefined,
  accountEmail: string | undefined,
): AttachmentPreviewTarget {
  if (folder === "Outbox") {
    const downloadURL = outgoingAttachmentURL(attachment.id);
    return { id: attachment.id, filename: attachment.filename, contentType: attachment.contentType, size: attachment.size, previewURL: `${downloadURL}?inline=true`, downloadURL };
  }
  return {
    id: attachment.id,
    filename: attachment.filename,
    contentType: attachment.contentType,
    size: attachment.size,
    previewURL: mirroredAttachmentPreviewURL(attachment.id, accountEmail),
    downloadURL: mirroredAttachmentDownloadURL(attachment.id, accountEmail),
  };
}

function CopyMessageMenuItem({ copy, message }: { copy: Copy; message: { html?: string; body?: string; preview?: string } }) {
  const copyAll = async () => {
    const text = message.html ? htmlToPlainText(message.html) : message.body || message.preview || "";
    const success = await copyToClipboard(text);
    if (success) toast.success(copy.copyMessageSuccess);
    else toast.error(copy.copyMessageFailed);
  };

  return (
    <ContextMenuItem className="gap-2 px-2 py-2" onClick={() => void copyAll()}>
      <CopyIcon className="size-4" />
      {copy.copyMessage}
    </ContextMenuItem>
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
  onAddToAIChat,
  isFocused,
  highlighted,
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
  onAddToAIChat?: () => void;
  isFocused?: boolean;
  /** Set when a search jump landed on this message. */
  highlighted?: boolean;
}) {
  const sender = message.outgoing ? copy.me : message.fromName || message.from || senderFallback || copy.conversations;
  const [preview, setPreview] = useState<AttachmentPreviewTarget | null>(null);
  const split = splitQuotedText(message.body || message.preview || copy.noBody);
  const visibleText = split.visible || (split.quoted ? copy.quotedOnly : copy.noBody);
  const outgoing = message.outgoing;
  const sendNeedsAttention = message.sendStatus === "failed" || message.sendStatus === "unknown";
  const sendNotice = message.sendStatus === "unknown"
    ? [message.sendError, copy.sendUnconfirmed].filter(Boolean).join(" · ")
    : message.sendError || copy.sendFailed;
  const formattedDate = formatTime(message.date);

  return (
    <>
    <ContextMenu>
      <ContextMenuTrigger
        data-message-key={messageKeyOf(message)}
        className={cn(
          "mb-5 block select-text rounded-lg transition-shadow",
          highlighted && "ring-2 ring-amber-400/80 ring-offset-2 ring-offset-background"
        )}
      >
        <article>
          {isFocused && (
            <div className={cn("mb-1.5 flex items-center gap-1 text-[11px] font-medium text-primary", outgoing && "justify-end")}>
              <Sparkles className="size-3" />
              <span>{copy.aiChatFocusedBadge}</span>
            </div>
          )}
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
            {outgoing && sendNeedsAttention && (
              <div className="mb-2 flex shrink-0 items-center justify-center self-center">
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    (onRetrySend || onReEdit)?.();
                  }}
                  className="group flex size-5 shrink-0 items-center justify-center rounded-full bg-destructive text-destructive-foreground font-bold text-xs leading-none shadow-xs transition-transform hover:scale-110 active:scale-95 focus:outline-hidden cursor-pointer"
                  title={onRetrySend ? `${sendNotice} · ${copy.retrySend}` : sendNotice}
                  aria-label={onRetrySend ? copy.sendFailedRetry : copy.reEdit}
                >
                  !
                </button>
              </div>
            )}
            <div className={cn("min-w-0 max-w-[80%] overflow-x-auto rounded-xl border px-3 py-2 text-sm leading-relaxed transition-all", message.sendStatus === "sending" ? "border-dashed border-primary/40 bg-primary/10 text-foreground" : sendNeedsAttention ? "border-destructive/40 bg-destructive/10 text-foreground" : "border-transparent bg-secondary text-secondary-foreground", message.html && "w-full", isFocused && "ring-2 ring-primary/60 border-primary/50 shadow-xs")}>
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
                    {message.attachments.map((attachment) => {
                      const target = conversationAttachmentPreviewTarget(attachment, message.folder, accountEmail);
                      const unavailable = message.folder === "Outbox" && message.sendRetryable === false && message.sendStatus !== "sent";
                      return (
                        <a
                          className="flex min-w-0 cursor-pointer items-center gap-1.5 text-xs text-primary hover:underline"
                          key={attachment.id}
                          href={unavailable ? "#" : target.previewURL}
                          data-testid="message-attachment"
                          onClick={unavailable ? (event) => event.preventDefault() : (event) => { event.preventDefault(); setPreview(target); }}
                        >
                          <Paperclip className="size-3.5 shrink-0" />
                          <span className="min-w-0 truncate">{attachment.filename}</span>
                          <small className="shrink-0 text-muted-foreground">{formatSize(attachment.size)}</small>
                        </a>
                      );
                    })}
                  </div>
                </>
              ) : null}
            </div>
          </div>
          {outgoing && message.sendStatus === "sending" && (
            <div className="mt-1 flex items-center justify-end gap-1.5 text-xs text-muted-foreground" role="status" aria-label={copy.sending}>
              <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
              <span>{copy.sending}</span>
            </div>
          )}
          {outgoing && sendNeedsAttention && (
            <div className="mt-1 flex items-center justify-end gap-1.5 text-right text-xs text-destructive">
              <span>{sendNotice}</span>
              {onRetrySend && (
                <button
                  type="button"
                  className="font-medium underline hover:opacity-80 cursor-pointer"
                  onClick={onRetrySend}
                >
                  {copy.retry}
                </button>
              )}
              {!onRetrySend && onReEdit && (
                <button type="button" className="font-medium underline hover:opacity-80 cursor-pointer" onClick={onReEdit}>
                  {copy.reEdit}
                </button>
              )}
            </div>
          )}
          {message.folder !== "Outbox" && (
            <MailMessageSummary copy={copy} accountEmail={accountEmail} folder={message.folder || "INBOX"} messageId={message.id} initialSummary={message.mailSummary} initialTranslation={message.mailTranslation} initialTranslationAll={message.mailTranslationAll} outgoing={outgoing} onGenerateReply={onGenerateReply} />
          )}
        </article>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-44">
        {sendNeedsAttention ? (
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
            <CopyMessageMenuItem copy={copy} message={message} />
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
            {canReplyAll(message) ? (
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
            <CopyMessageMenuItem copy={copy} message={message} />
            {message.folder !== "Outbox" && onAddToAIChat && (
              <ContextMenuItem className="gap-2 px-2 py-2" onClick={onAddToAIChat}>
                <Sparkles className="size-4 text-primary" />
                {copy.addToAIChat}
              </ContextMenuItem>
            )}
            <ContextMenuItem variant="destructive" className="gap-2 px-2 py-2" onClick={onDelete}>
              <Trash2 className="size-4" />
              {copy.deleteEmail}
            </ContextMenuItem>
          </>
        )}
      </ContextMenuContent>
    </ContextMenu>
    <AttachmentPreviewDialog copy={copy} target={preview} onOpenChange={(open) => { if (!open) setPreview(null); }} />
    </>
  );
}

function formatTranslationParagraphs(text: string): string[] {
  if (!text) return [];
  const normalized = text.replace(/\r\n/g, "\n").replace(/\r/g, "\n").trim();
  if (!normalized) return [];
  return normalized
    .split(/\n{2,}/)
    .map((paragraph) =>
      paragraph
        .split("\n")
        .map((line) => line.replace(/[ \t]+/g, " ").trim())
        .join("\n")
        .trim()
    )
    .filter(Boolean);
}

export function MailMessageSummary({
  copy,
  accountEmail,
  folder,
  messageId,
  initialSummary,
  initialTranslation,
  initialTranslationAll,
  outgoing = false,
  onGenerateReply,
}: {
  copy: Copy;
  accountEmail?: string;
  folder: string;
  messageId: string;
  initialSummary?: ConversationMessage["mailSummary"];
  initialTranslation?: ConversationMessage["mailTranslation"];
  initialTranslationAll?: ConversationMessage["mailTranslationAll"];
  outgoing?: boolean;
  onGenerateReply?: () => void;
}) {
  const queryClient = useQueryClient();
  const [savedSummary, setSavedSummary] = useState(initialSummary);
  const [savedTranslation, setSavedTranslation] = useState(initialTranslation);
  const [savedTranslationAll, setSavedTranslationAll] = useState(initialTranslationAll);
  const [showTranslation, setShowTranslation] = useState(false);
  const [showTranslationAll, setShowTranslationAll] = useState(false);
  const [copiedTranslation, setCopiedTranslation] = useState(false);
  const [copiedTranslationAll, setCopiedTranslationAll] = useState(false);

  const messageKey = `${accountEmail || ""}:${folder}:${messageId}`;
  const previousMessageKeyRef = useRef(messageKey);

  const translationParagraphs = useMemo(
    () => formatTranslationParagraphs(savedTranslation?.text || ""),
    [savedTranslation?.text]
  );
  const translationAllParagraphs = useMemo(
    () => formatTranslationParagraphs(savedTranslationAll?.text || ""),
    [savedTranslationAll?.text]
  );

  const isZh = copy === zh;
  const summaryAI = useAIStateMachine({
    taskType: "summary",
    locale: isZh ? "zh" : "en",
  });
  const translationAI = useAIStateMachine({
    taskType: "translation",
    locale: isZh ? "zh" : "en",
  });
  const translationAllAI = useAIStateMachine({
    taskType: "translation",
    locale: isZh ? "zh" : "en",
  });

  const summaryMutation = useMutation({
    mutationFn: (regenerate: boolean) =>
      summaryAI.run(() =>
        summarizeMailMessage(accountEmail || "", folder, messageId, regenerate)
      ),
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
    onError: (value) => toastError(value, copy.loadFailed),
  });

  const translationMutation = useMutation({
    mutationFn: (regenerate: boolean) =>
      translationAI.run(() =>
        translateMailMessage(accountEmail || "", folder, messageId, regenerate, false)
      ),
    onSuccess: (result) => {
      const translation: MailTranslation = { text: result.translation, status: result.status, updatedAt: result.updatedAt };
      setSavedTranslation(translation);
      setShowTranslation(true);
      setShowTranslationAll(false);
      queryClient.setQueryData<MailMessage>(["message", folder, messageId], (current) => (current ? { ...current, mailTranslation: translation } : current));
      queryClient.setQueriesData<ConversationDetailResponse>({ queryKey: ["conversation"] }, (current) => {
        if (!current || (accountEmail && current.conversation.accountEmail !== accountEmail)) return current;
        let changed = false;
        const messages = current.conversation.messages.map((message) => {
          if (message.id !== messageId || (message.folder || "INBOX") !== folder) return message;
          changed = true;
          return { ...message, mailTranslation: translation };
        });
        return changed ? { ...current, conversation: { ...current.conversation, messages } } : current;
      });
    },
    onError: (value) => toastError(value, copy.translateFailed),
  });

  const translationAllMutation = useMutation({
    mutationFn: (regenerate: boolean) =>
      translationAllAI.run(() =>
        translateMailMessage(accountEmail || "", folder, messageId, regenerate, true)
      ),
    onSuccess: (result) => {
      const translation: MailTranslation = { text: result.translation, status: result.status, updatedAt: result.updatedAt };
      setSavedTranslationAll(translation);
      setShowTranslationAll(true);
      setShowTranslation(false);
      queryClient.setQueryData<MailMessage>(["message", folder, messageId], (current) => (current ? { ...current, mailTranslationAll: translation } : current));
      queryClient.setQueriesData<ConversationDetailResponse>({ queryKey: ["conversation"] }, (current) => {
        if (!current || (accountEmail && current.conversation.accountEmail !== accountEmail)) return current;
        let changed = false;
        const messages = current.conversation.messages.map((message) => {
          if (message.id !== messageId || (message.folder || "INBOX") !== folder) return message;
          changed = true;
          return { ...message, mailTranslationAll: translation };
        });
        return changed ? { ...current, conversation: { ...current.conversation, messages } } : current;
      });
    },
    onError: (value) => toastError(value, copy.translateAllFailed),
  });

  useEffect(() => {
    if (previousMessageKeyRef.current !== messageKey) {
      previousMessageKeyRef.current = messageKey;
      setSavedSummary(initialSummary);
      setSavedTranslation(initialTranslation);
      setSavedTranslationAll(initialTranslationAll);
      setShowTranslation(false);
      setShowTranslationAll(false);
      summaryMutation.reset();
      translationMutation.reset();
      translationAllMutation.reset();
      return;
    }

    setSavedSummary(initialSummary);
    setSavedTranslation(initialTranslation);
    setSavedTranslationAll(initialTranslationAll);
  }, [messageKey, initialSummary, initialTranslation, initialTranslationAll]);

  const copyTranslationText = async (text: string) => {
    const formatted = formatTranslationParagraphs(text).join("\n\n");
    const success = await copyToClipboard(formatted || text);
    if (success) {
      setCopiedTranslation(true);
      toast.success(copy.copiedTranslation);
      window.setTimeout(() => setCopiedTranslation(false), 1600);
    }
  };

  const copyTranslationAllText = async (text: string) => {
    const formatted = formatTranslationParagraphs(text).join("\n\n");
    const success = await copyToClipboard(formatted || text);
    if (success) {
      setCopiedTranslationAll(true);
      toast.success(copy.copiedTranslationAll);
      window.setTimeout(() => setCopiedTranslationAll(false), 1600);
    }
  };

  const handleTranslateClick = () => {
    if (savedTranslation) {
      setShowTranslation((prev) => {
        const next = !prev;
        if (next) setShowTranslationAll(false);
        return next;
      });
    } else {
      setShowTranslationAll(false);
      translationMutation.mutate(false);
    }
  };

  const handleTranslateAllClick = () => {
    if (savedTranslationAll) {
      setShowTranslationAll((prev) => {
        const next = !prev;
        if (next) setShowTranslation(false);
        return next;
      });
    } else {
      setShowTranslation(false);
      translationAllMutation.mutate(false);
    }
  };

  return (
    <div className={cn("mt-1.5 max-w-[80%]", outgoing && "ml-auto")}>
      <div className="flex min-h-7 flex-wrap items-center gap-1">
        {!savedSummary && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs text-muted-foreground"
            disabled={summaryMutation.isPending || !accountEmail}
            onClick={() => summaryMutation.mutate(false)}
          >
            {summaryMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Sparkles className="size-3.5" />
            )}
            {summaryMutation.isPending ? copy.summarizing : copy.summarize}
          </Button>
        )}
        {savedTranslation ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className={cn(
              "h-7 px-2 text-xs font-medium transition-colors",
              showTranslation
                ? "bg-emerald-500/15 text-emerald-700 hover:bg-emerald-500/20 dark:bg-emerald-500/20 dark:text-emerald-300 dark:hover:bg-emerald-500/30"
                : "text-emerald-600 hover:bg-emerald-500/10 hover:text-emerald-700 dark:text-emerald-400 dark:hover:bg-emerald-500/20 dark:hover:text-emerald-300"
            )}
            disabled={translationMutation.isPending || !accountEmail}
            onClick={handleTranslateClick}
            title={showTranslation ? copy.hideTranslation : copy.showTranslation}
          >
            {translationMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin text-emerald-600 dark:text-emerald-400" />
            ) : (
              <Languages className="size-3.5 text-emerald-600 dark:text-emerald-400" />
            )}
            {translationMutation.isPending ? copy.translating : copy.translate}
          </Button>
        ) : (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs text-muted-foreground"
            disabled={translationMutation.isPending || !accountEmail}
            onClick={handleTranslateClick}
          >
            {translationMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Languages className="size-3.5" />
            )}
            {translationMutation.isPending ? copy.translating : copy.translate}
          </Button>
        )}
        {savedTranslationAll ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className={cn(
              "h-7 px-2 text-xs font-medium transition-colors",
              showTranslationAll
                ? "bg-emerald-500/15 text-emerald-700 hover:bg-emerald-500/20 dark:bg-emerald-500/20 dark:text-emerald-300 dark:hover:bg-emerald-500/30"
                : "text-emerald-600 hover:bg-emerald-500/10 hover:text-emerald-700 dark:text-emerald-400 dark:hover:bg-emerald-500/20 dark:hover:text-emerald-300"
            )}
            disabled={translationAllMutation.isPending || !accountEmail}
            onClick={handleTranslateAllClick}
            title={showTranslationAll ? copy.hideTranslationAll : copy.showTranslationAll}
          >
            {translationAllMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin text-emerald-600 dark:text-emerald-400" />
            ) : (
              <Languages className="size-3.5 text-emerald-600 dark:text-emerald-400" />
            )}
            {translationAllMutation.isPending ? copy.translatingAll : copy.translateAll}
          </Button>
        ) : (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs text-muted-foreground"
            disabled={translationAllMutation.isPending || !accountEmail}
            onClick={handleTranslateAllClick}
          >
            {translationAllMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Languages className="size-3.5" />
            )}
            {translationAllMutation.isPending ? copy.translatingAll : copy.translateAll}
          </Button>
        )}
        {onGenerateReply && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs text-muted-foreground"
            onClick={onGenerateReply}
          >
            <MessageCircle className="size-3.5" />
            {copy.generateSuggestedReply}
          </Button>
        )}
      </div>
      {summaryMutation.isPending && (
        <div className="mt-1.5">
          <AIStatusIndicator snapshot={summaryAI.snapshot} variant="card" showTimer showSteps />
        </div>
      )}
      {savedSummary && !summaryMutation.isPending && (
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
              disabled={summaryMutation.isPending || !accountEmail}
              onClick={() => summaryMutation.mutate(true)}
              aria-label={copy.regenerateSummary}
              title={copy.regenerateSummary}
            >
              <RotateCcw className={cn("size-3.5", summaryMutation.isPending && "animate-spin")} />
            </Button>
          </div>
          <p className="whitespace-pre-wrap">{savedSummary.text}</p>
        </div>
      )}
      {translationMutation.isPending && (
        <div className="mt-1.5">
          <AIStatusIndicator snapshot={translationAI.snapshot} variant="card" showTimer showSteps />
        </div>
      )}
      {savedTranslation && showTranslation && !translationMutation.isPending && (
        <div className="mt-1 border-l-2 border-emerald-500/60 bg-muted/40 px-3 py-2 text-sm leading-relaxed">
          <div className="mb-1 flex items-center justify-between gap-2">
            <div className="flex min-w-0 items-center gap-2">
              <strong className="text-xs font-medium text-muted-foreground">{copy.mailTranslationTitle}</strong>
            </div>
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-6 shrink-0"
                onClick={() => void copyTranslationText(savedTranslation.text)}
                aria-label={copy.copyTranslation}
                title={copiedTranslation ? copy.copiedTranslation : copy.copyTranslation}
              >
                {copiedTranslation ? <Check className="size-3.5" /> : <CopyIcon className="size-3.5" />}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-6 shrink-0"
                disabled={translationMutation.isPending || !accountEmail}
                onClick={() => translationMutation.mutate(true)}
                aria-label={copy.regenerateTranslation}
                title={copy.regenerateTranslation}
              >
                <RotateCcw className={cn("size-3.5", translationMutation.isPending && "animate-spin")} />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-6 shrink-0 text-muted-foreground hover:text-foreground"
                onClick={() => setShowTranslation(false)}
                aria-label={copy.hideTranslation}
                title={copy.hideTranslation}
              >
                <X className="size-3.5" />
              </Button>
            </div>
          </div>
          <div className="space-y-2">
            {translationParagraphs.map((paragraph, index) => (
              <p key={index} className="whitespace-pre-wrap leading-relaxed">
                {paragraph}
              </p>
            ))}
          </div>
        </div>
      )}
      {translationAllMutation.isPending && (
        <div className="mt-1.5">
          <AIStatusIndicator snapshot={translationAllAI.snapshot} variant="card" showTimer showSteps />
        </div>
      )}
      {savedTranslationAll && showTranslationAll && !translationAllMutation.isPending && (
        <div className="mt-1 border-l-2 border-emerald-500/60 bg-muted/40 px-3 py-2 text-sm leading-relaxed">
          <div className="mb-1 flex items-center justify-between gap-2">
            <div className="flex min-w-0 items-center gap-2">
              <strong className="text-xs font-medium text-muted-foreground">{copy.mailTranslationAllTitle}</strong>
            </div>
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-6 shrink-0"
                onClick={() => void copyTranslationAllText(savedTranslationAll.text)}
                aria-label={copy.copyTranslationAll}
                title={copiedTranslationAll ? copy.copiedTranslationAll : copy.copyTranslationAll}
              >
                {copiedTranslationAll ? <Check className="size-3.5" /> : <CopyIcon className="size-3.5" />}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-6 shrink-0"
                disabled={translationAllMutation.isPending || !accountEmail}
                onClick={() => translationAllMutation.mutate(true)}
                aria-label={copy.regenerateTranslationAll}
                title={copy.regenerateTranslationAll}
              >
                <RotateCcw className={cn("size-3.5", translationAllMutation.isPending && "animate-spin")} />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="size-6 shrink-0 text-muted-foreground hover:text-foreground"
                onClick={() => setShowTranslationAll(false)}
                aria-label={copy.hideTranslationAll}
                title={copy.hideTranslationAll}
              >
                <X className="size-3.5" />
              </Button>
            </div>
          </div>
          <div className="space-y-2">
            {translationAllParagraphs.map((paragraph, index) => (
              <p key={index} className="whitespace-pre-wrap leading-relaxed">
                {paragraph}
              </p>
            ))}
          </div>
        </div>
      )}
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

    preserveTranscriptScroll(rootRef.current, () => { frame.style.height = "1px"; });

    const measureNow = () => {
      if (!mounted) return;
      const doc = frame.contentDocument;
      const root = doc?.documentElement;
      const body = doc?.body;
      if (!root || !body) return;

      const height = Math.max(body.scrollHeight, body.offsetHeight, body.getBoundingClientRect().height, 48);
      if (height === lastHeight) {
        preserveTranscriptScroll(rootRef.current, () => {});
        return;
      }
      lastHeight = height;
      preserveTranscriptScroll(rootRef.current, () => { frame.style.height = `${height}px`; });
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
        style.textContent = "html, body { background-color: transparent !important; overflow-anchor: none; }";
        doc.head.append(style);
      };

      installTransparentBackground();
      resizeObserver = new ResizeObserver(measureNow);
      resizeObserver.observe(doc.documentElement);
      if (doc.body) resizeObserver.observe(doc.body);
      mutationObserver = new MutationObserver(() => {
        installTransparentBackground();
        measure();
      });
      mutationObserver.observe(doc, { childList: true, subtree: true, characterData: true });

      const onResource = (event: Event) => {
        // Image nodes belong to the iframe's realm, so instanceof against the
        // parent window's HTMLImageElement would miss their load events.
        if ((event.target as Element | null)?.nodeName === "IMG") measureNow();
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
      // Input inside an iframe does not bubble to the transcript. Forward only
      // the intent (synthetic wheel events do not perform native scrolling),
      // so even a short window at its boundary can request the next page.
      const forwardScrollIntent = (deltaY: number, ctrlKey = false) => {
        rootRef.current?.dispatchEvent(new WheelEvent("wheel", { deltaY, ctrlKey, bubbles: true }));
      };
      const onWheel = (event: WheelEvent) => forwardScrollIntent(event.deltaY, event.ctrlKey);
      let touchY = 0;
      const onTouchStart = (event: TouchEvent) => { touchY = event.touches[0]?.clientY || 0; };
      const onTouchMove = (event: TouchEvent) => {
        const nextY = event.touches[0]?.clientY || 0;
        if (Math.abs(touchY - nextY) > 5) forwardScrollIntent(touchY - nextY);
        touchY = nextY;
      };
      doc.addEventListener("load", onResource, true);
      doc.addEventListener("error", onResource, true);
      doc.addEventListener("contextmenu", onContextMenu);
      doc.addEventListener("wheel", onWheel, { passive: true });
      doc.addEventListener("touchstart", onTouchStart, { passive: true });
      doc.addEventListener("touchmove", onTouchMove, { passive: true });
      resourceCleanup = () => {
        doc.removeEventListener("load", onResource, true);
        doc.removeEventListener("error", onResource, true);
        doc.removeEventListener("contextmenu", onContextMenu);
        doc.removeEventListener("wheel", onWheel);
        doc.removeEventListener("touchstart", onTouchStart);
        doc.removeEventListener("touchmove", onTouchMove);
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
  }, [enabled, html, rootRef]);

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
  const [preview, setPreview] = useState<AttachmentPreviewTarget | null>(null);
  return (
    <>
    <ContextMenu>
      <ContextMenuTrigger className="block select-text">
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
              {message.attachments.map((item) => {
                const target = conversationAttachmentPreviewTarget(item, message.folder, message.accountEmail);
                return (
                  <a
                    className="flex min-w-0 cursor-pointer items-center gap-1.5 text-xs text-primary hover:underline"
                    key={item.id || item.partId}
                    href={target.previewURL}
                    data-testid="message-attachment"
                    onClick={(event) => { event.preventDefault(); setPreview(target); }}
                  >
                    <Paperclip className="size-3.5 shrink-0" />
                    <span className="truncate">{item.filename}</span>
                    <small className="shrink-0 text-muted-foreground">{formatSize(item.size)}</small>
                  </a>
                );
              })}
            </div>
          </>
        ) : null}
      </div>
      <MailMessageSummary copy={copy} accountEmail={message.accountEmail} folder={message.folder || "INBOX"} messageId={message.id} initialSummary={message.mailSummary} initialTranslation={message.mailTranslation} initialTranslationAll={message.mailTranslationAll} />
    </article>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-40">
        <CopyMessageMenuItem copy={copy} message={message} />
      </ContextMenuContent>
    </ContextMenu>
    <AttachmentPreviewDialog copy={copy} target={preview} onOpenChange={(open) => { if (!open) setPreview(null); }} />
    </>
  );
}
