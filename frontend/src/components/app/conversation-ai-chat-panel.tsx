import React, { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bot,
  Check,
  ChevronDown,
  Copy as CopyIcon,
  Loader2,
  PenLine,
  Sparkles,
  Tags,
  Trash2,
  Wrench,
  X,
} from "lucide-react";
import { toast } from "../../lib/toast";
import {
  chatWithAIAgent,
  deleteConversationAIChat,
  getAIAgents,
  getConversationAIChat,
  saveConversationAIChat,
  type AIAgentChatRequest,
} from "../../lib/api";
import {
  copyToClipboard,
  markdownToPlainText,
} from "../../lib/email-format";
import { type Copy, zh } from "../../lib/locale";
import { cn, formatTime } from "../../lib/utils";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { AIChatInput } from "./ai-chat-input";
import { AIStatusIndicator } from "./ai-status-indicator";
import { ConversationDocumentCard } from "./conversation-document-card";
import { MarkdownContent } from "./markdown-content";
import { useAIStateMachine } from "../../lib/ai-state-machine";
import type { ConversationAIChatMessage, ConversationDetail } from "../../types";

/** Localized label for a tool the agent ran, so the chip reads as an action. */
function toolDisplayName(name: string, isZh: boolean) {
  switch (name) {
    case "search_emails":
      return isZh ? "检索邮箱邮件" : "Searched mail";
    case "read_email":
      return isZh ? "读取邮件正文" : "Read email";
    case "lookup_price":
      return isZh ? "查询价格表" : "Looked up price list";
    case "calculate_pricing":
      return isZh ? "核算价格与税费" : "Calculated pricing";
    case "generate_document":
      return isZh ? "生成结构化文档" : "Generated document";
    default:
      return name;
  }
}

export function ConversationAIChatPanel({
  copy,
  detail,
  onClose,
  isMobile = false,
  focusedMessageIds = [],
  onRemoveFocusedMessage,
  onClearFocusedMessages,
  onWriteDraft,
  className,
}: {
  copy: Copy;
  detail: ConversationDetail;
  onClose: () => void;
  isMobile?: boolean;
  focusedMessageIds?: string[];
  onRemoveFocusedMessage?: (id: string) => void;
  onClearFocusedMessages?: () => void;
  /** Puts an answer into the reply draft in the transcript, creating it if needed. */
  onWriteDraft?: (body: string) => void;
  className?: string;
}) {
  const queryClient = useQueryClient();
  const [input, setInput] = useState("");
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [clearDialogOpen, setClearDialogOpen] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Query conversation AI chat messages from backend SQLite database
  const chatQuery = useQuery({
    queryKey: ["conversation-ai-chat", detail.id],
    queryFn: async () => {
      const res = await getConversationAIChat(detail.id);
      return res.messages || [];
    },
  });

  const messages: ConversationAIChatMessage[] = chatQuery.data || [];

  const [pendingUserText, setPendingUserText] = useState<string | null>(null);

  useEffect(() => {
    setPendingUserText(null);
  }, [detail.id]);

  const displayMessages = useMemo(() => {
    if (!pendingUserText) return messages;
    const pendingMsg: ConversationAIChatMessage = {
      id: "pending-user-msg",
      role: "user",
      content: pendingUserText,
      createdAt: new Date().toISOString(),
    };
    return [...messages, pendingMsg];
  }, [messages, pendingUserText]);

  // Query available AI agents from backend
  const { data: agentsData } = useQuery({
    queryKey: ["ai-agents"],
    queryFn: getAIAgents,
    staleTime: 60_000,
  });
  const agents = agentsData?.agents || [];
  const [selectedAgentId, setSelectedAgentId] = useState<string | undefined>(undefined);
  const currentAgent = agents.find((a) => a.id === selectedAgentId);

  // Auto-scroll to bottom on messages update
  const scrollToBottom = () => {
    window.requestAnimationFrame(() => {
      if (scrollRef.current) {
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
      }
    });
  };

  useEffect(() => {
    scrollToBottom();
  }, [displayMessages.length]);

  const focusedMessages = useMemo(() => {
    if (!focusedMessageIds.length) return [];
    const idSet = new Set(focusedMessageIds);
    return detail.messages.filter((m) => idSet.has(m.id));
  }, [detail.messages, focusedMessageIds]);

  const isZh = copy === zh;
  const aiState = useAIStateMachine({
    taskType: "chat",
    locale: isZh ? "zh" : "en",
  });

  const mutation = useMutation({
    mutationFn: async (userQuestion: string) => {
      const text = userQuestion.trim();
      if (!text) return null;

      const accountEmail = detail.accountEmail;
      if (!accountEmail) {
        throw new Error(isZh ? "当前邮件会话缺少关联账户邮箱" : "Account email required");
      }

      // The server resolves current notes/mail from the conversation identity.
      // Send discussion and source references, independent of the UI mail window.
      const apiMessages: AIAgentChatRequest["messages"] = messages.map((message) => ({
        role: message.role,
        content: message.content,
        sources: message.sources,
      }));
      apiMessages.push({ role: "user", content: text });

      return await aiState.run(async () => {
        const res = await chatWithAIAgent({
          accountEmail,
          conversationId: detail.id,
          focusedMessages: focusedMessages.map((message) => ({ uid: message.id, folder: message.folder || "INBOX" })),
          agentId: selectedAgentId,
          messages: apiMessages,
        });

        if (res.toolCalls && res.toolCalls.length > 0) {
          const first = res.toolCalls[0];
          aiState.setToolCall(first.name, first.summary);
        }

        return { res, text };
      });
    },
    onSuccess: async (data) => {
      setPendingUserText(null);
      if (!data?.res) return;
      const now = new Date().toISOString();
      const userMsg: ConversationAIChatMessage = {
        id: `user-${Date.now()}`,
        role: "user",
        content: data.text,
        createdAt: now,
      };
      const assistantMsg: ConversationAIChatMessage = {
        id: `assistant-${Date.now() + 1}`,
        role: "assistant",
        content: data.res.content || "已处理完成。",
        sources: data.res.sources,
        createdAt: now,
        // Keep the tools the agent used, so "did it read the price list?" is
        // answerable after the fact instead of only during the status spinner.
        toolCalls: (data.res.toolCalls ?? []).map((call) => ({
          id: call.id,
          name: call.name,
          summary: call.summary,
          status: call.status,
        })),
        document: data.res.document
          ? {
              type: data.res.document.type === "contract" ? "contract" : "quotation",
              title: data.res.document.title,
              counterparty: data.res.document.counterparty,
              contact: data.res.document.contact,
              currency: data.res.document.currency,
              items: data.res.document.items,
              terms: data.res.document.terms,
              notes: data.res.document.notes,
              total: data.res.document.total,
              values: data.res.document.values,
              html: data.res.document.html,
            }
          : undefined,
        documentAction: data.res.documentAction
          ? {
              kind: data.res.documentAction.kind,
              documentId: data.res.documentAction.documentId,
              type: data.res.documentAction.type,
              name: data.res.documentAction.name,
              company: data.res.documentAction.company,
              values: data.res.documentAction.values,
              items: data.res.documentAction.items,
              summary: data.res.documentAction.summary,
            }
          : undefined,
      };
      const nextMessages = [...messages, userMsg, assistantMsg];

      // Optimistically update React Query cache
      queryClient.setQueryData<ConversationAIChatMessage[]>(
        ["conversation-ai-chat", detail.id],
        nextMessages
      );

      // Persist to SQLite database
      try {
        await saveConversationAIChat(detail.id, nextMessages);
      } catch {
        toast.error("保存对话记录到数据库失败");
      }

      setInput("");
      window.setTimeout(scrollToBottom, 50);
    },
    onError: (err) => {
      setPendingUserText(null);
      toast.error(err instanceof Error ? err.message : "AI 回复失败");
    },
  });

  const handleSend = (textToSend?: string) => {
    const text = (textToSend ?? input).trim();
    if (!text || mutation.isPending) return;
    setInput("");
    setPendingUserText(text);
    window.setTimeout(scrollToBottom, 20);
    mutation.mutate(text);
  };

  const handleCopy = async (id: string, text: string) => {
    const ok = await copyToClipboard(text);
    if (ok) {
      setCopiedId(id);
      toast.success(copy.aiChatCopySuccess);
      window.setTimeout(() => setCopiedId(null), 1500);
    }
  };

  const handleConfirmClear = async () => {
    setClearDialogOpen(false);
    setPendingUserText(null);
    queryClient.setQueryData<ConversationAIChatMessage[]>(
      ["conversation-ai-chat", detail.id],
      []
    );

    try {
      await deleteConversationAIChat(detail.id);
      toast.success(copy.aiChatClearHistory);
    } catch {
      toast.error("清除数据库记录失败");
    }
  };

  return (
    <div className={cn("flex h-full min-h-0 flex-1 flex-col overflow-hidden bg-card text-foreground", className)}>
      {/* Header */}
      <header className="flex h-14 shrink-0 items-center justify-between border-b px-3 sm:px-4">
        <div className="flex min-w-0 items-center gap-2">
          {isMobile ? (
            <Button
              variant="ghost"
              size="icon"
              className="size-8 shrink-0 lg:hidden"
              onClick={onClose}
              aria-label={copy.back}
              title={copy.back}
            >
              <ArrowLeft className="size-4" />
            </Button>
          ) : (
            <div className="flex size-7 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Bot className="size-4" />
            </div>
          )}
          <div className="min-w-0">
            <h3 className="truncate text-xs sm:text-sm font-semibold">{copy.aiChatTitle}</h3>
            <p className="truncate text-[11px] text-muted-foreground">{detail.subject || copy.noSubject}</p>
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {/* Agent Picker */}
          {agents.length > 0 && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 max-w-[120px] sm:max-w-[140px] gap-1 px-2 text-xs font-normal text-muted-foreground hover:text-foreground"
                  >
                    <span className="truncate">{currentAgent?.name || copy.aiChatDefaultAgent}</span>
                    <ChevronDown className="size-3 shrink-0 opacity-60" />
                  </Button>
                }
              />
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuItem
                  className={cn(!selectedAgentId && "font-semibold text-primary")}
                  onClick={() => setSelectedAgentId(undefined)}
                >
                  {copy.aiChatDefaultAgent}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                {agents.map((agent) => (
                  <DropdownMenuItem
                    key={agent.id}
                    className={cn(selectedAgentId === agent.id && "font-semibold text-primary")}
                    onClick={() => setSelectedAgentId(agent.id)}
                  >
                    <span className="truncate">{agent.name}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          )}

          {/* Clear Chat */}
          {(messages.length > 0 || Boolean(pendingUserText)) && (
            <Button
              variant="ghost"
              size="icon"
              className="size-7 text-muted-foreground hover:text-destructive"
              onClick={() => setClearDialogOpen(true)}
              title={copy.aiChatClearHistory}
              aria-label={copy.aiChatClearHistory}
            >
              <Trash2 className="size-3.5" />
            </Button>
          )}

          {/* Close Panel */}
          <Button
            variant="ghost"
            size="icon"
            className="size-7 text-muted-foreground hover:text-foreground"
            onClick={onClose}
            title={copy.closeAIChat}
            aria-label={copy.closeAIChat}
          >
            <X className="size-4" />
          </Button>
        </div>
      </header>

      {/* Messages Scroll Area */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto px-3 sm:px-4 py-4 space-y-4">
        {chatQuery.isPending ? (
          <div className="flex h-full min-h-[200px] items-center justify-center text-xs text-muted-foreground gap-2">
            <Loader2 className="size-4 animate-spin text-primary" />
            <span>{copy.loading}</span>
          </div>
        ) : displayMessages.length === 0 ? (
          <div className="flex h-full min-h-[240px] flex-col items-center justify-center text-center px-4">
            <div className="mb-3 flex size-12 items-center justify-center rounded-2xl bg-primary/10 text-primary">
              <Sparkles className="size-6" />
            </div>
            <h4 className="text-sm font-semibold">{copy.aiChatEmptyTitle}</h4>
            <p className="mt-1.5 max-w-xs text-xs text-muted-foreground leading-relaxed">
              {copy.aiChatEmptyDescription}
            </p>

            {/* Quick Prompts */}
            <div className="mt-6 flex flex-col w-full max-w-sm gap-2">
              <button
                type="button"
                className="group flex items-center justify-between rounded-xl border border-border/70 bg-secondary/40 hover:bg-secondary/80 px-3 py-2 text-left text-xs transition-colors cursor-pointer"
                onClick={() => handleSend(copy.aiChatPromptSummarize)}
              >
                <span className="truncate text-foreground group-hover:text-primary transition-colors">
                  💡 {copy.aiChatPromptSummarize}
                </span>
                <span className="text-[11px] text-muted-foreground ml-2 shrink-0">↵</span>
              </button>
              <button
                type="button"
                className="group flex items-center justify-between rounded-xl border border-border/70 bg-secondary/40 hover:bg-secondary/80 px-3 py-2 text-left text-xs transition-colors cursor-pointer"
                onClick={() => handleSend(copy.aiChatPromptAnalyze)}
              >
                <span className="truncate text-foreground group-hover:text-primary transition-colors">
                  🔍 {copy.aiChatPromptAnalyze}
                </span>
                <span className="text-[11px] text-muted-foreground ml-2 shrink-0">↵</span>
              </button>
              <button
                type="button"
                className="group flex items-center justify-between rounded-xl border border-border/70 bg-secondary/40 hover:bg-secondary/80 px-3 py-2 text-left text-xs transition-colors cursor-pointer"
                onClick={() => handleSend(copy.aiChatPromptPrice)}
              >
                <span className="truncate text-foreground group-hover:text-primary transition-colors">
                  💰 {copy.aiChatPromptPrice}
                </span>
                <span className="text-[11px] text-muted-foreground ml-2 shrink-0">↵</span>
              </button>
              <button
                type="button"
                className="group flex items-center justify-between rounded-xl border border-border/70 bg-secondary/40 hover:bg-secondary/80 px-3 py-2 text-left text-xs transition-colors cursor-pointer"
                onClick={() => handleSend(copy.aiChatPromptReplySuggestion)}
              >
                <span className="truncate text-foreground group-hover:text-primary transition-colors">
                  ✍️ {copy.aiChatPromptReplySuggestion}
                </span>
                <span className="text-[11px] text-muted-foreground ml-2 shrink-0">↵</span>
              </button>
            </div>
          </div>
        ) : (
          displayMessages.map((message) => {
            const isUser = message.role === "user";
            return (
              <div
                key={message.id}
                className={cn("group flex flex-col gap-1", isUser ? "items-end" : "items-start")}
              >
                <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground px-1 select-none">
                  {!isUser && (
                    <div className="flex size-4 items-center justify-center rounded-full bg-primary/15 text-primary">
                      <Bot className="size-2.5" />
                    </div>
                  )}
                  <span>{isUser ? copy.me : (currentAgent?.name || copy.aiChatDefaultAgent)}</span>
                  <span>·</span>
                  <span>{formatTime(message.createdAt)}</span>
                </div>
                <div
                  className={cn(
                    "flex items-end gap-1.5 max-w-[92%] sm:max-w-[88%]",
                    isUser ? "flex-row-reverse" : "flex-row"
                  )}
                >
                  <div
                    className={cn(
                      "min-w-0 rounded-2xl px-3.5 py-2.5 text-xs sm:text-sm leading-relaxed break-words shadow-xs select-text cursor-text",
                      isUser
                        ? "bg-primary text-primary-foreground rounded-tr-xs whitespace-pre-wrap selection:bg-primary-foreground/30 selection:text-primary-foreground"
                        : "bg-secondary text-secondary-foreground rounded-tl-xs"
                    )}
                  >
                    {isUser ? (
                      <span className="select-text">{message.content}</span>
                    ) : (
                      <MarkdownContent
                        content={message.content}
                        copyLabels={{
                          copy: copy.copyAIContent,
                          copied: copy.aiChatCopySuccess,
                        }}
                      />
                    )}
                  </div>
                  {!isUser && onWriteDraft ? (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className={cn(
                        "size-6 shrink-0 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-opacity",
                        "opacity-70 sm:opacity-0 sm:group-hover:opacity-100 focus-visible:opacity-100"
                      )}
                      onClick={() => {
                        const body = markdownToPlainText(message.content);
                        if (!body) return;
                        onWriteDraft(body);
                        // On mobile the panel covers the transcript, and the
                        // point of the click is to see the draft it filled.
                        if (isMobile) onClose();
                      }}
                      title={copy.draftWriteFromAI}
                      aria-label={copy.draftWriteFromAI}
                      data-testid="ai-message-write-draft"
                    >
                      <PenLine className="size-3" />
                    </Button>
                  ) : null}
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className={cn(
                      "size-6 shrink-0 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-opacity",
                      "opacity-70 sm:opacity-0 sm:group-hover:opacity-100 focus-visible:opacity-100"
                    )}
                    onClick={() => void handleCopy(message.id, message.content)}
                    title={copy.copyAIContent}
                    aria-label={copy.copyAIContent}
                  >
                    {copiedId === message.id ? (
                      <Check className="size-3 text-emerald-600 dark:text-emerald-400" />
                    ) : (
                      <CopyIcon className="size-3" />
                    )}
                  </Button>
                </div>
                {!isUser && (message.document || message.documentAction) ? (
                  <ConversationDocumentCard
                    copy={copy}
                    accountEmail={detail.accountEmail || ""}
                    counterpartyEmail={detail.peerEmail}
                    draft={message.document}
                    action={message.documentAction}
                  />
                ) : null}
                {!isUser && message.toolCalls?.length ? (
                  <div className="flex max-w-full flex-wrap items-center gap-1 px-1">
                    {message.toolCalls.map((call, index) => (
                      <span
                        key={call.id ?? `${call.name}-${index}`}
                        className={cn(
                          "inline-flex max-w-full items-center gap-1 rounded-full border px-1.5 py-0.5 text-[10px] leading-4",
                          call.status === "error"
                            ? "border-destructive/30 bg-destructive/10 text-destructive"
                            : "border-border bg-muted/60 text-muted-foreground"
                        )}
                        title={call.summary}
                      >
                        {call.name === "lookup_price" ? <Tags className="size-2.5 shrink-0 text-cyan-600" /> : <Wrench className="size-2.5 shrink-0" />}
                        <span className="shrink-0">{toolDisplayName(call.name, isZh)}</span>
                        {call.summary ? <span className="min-w-0 truncate opacity-80">· {call.summary}</span> : null}
                      </span>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          })
        )}

        {/* State Machine Status Feedback Indicator */}
        {mutation.isPending && (
          <AIStatusIndicator
            snapshot={aiState.snapshot}
            variant="bubble"
            agentName={currentAgent?.name || copy.aiChatDefaultAgent}
            showTimer
          />
        )}
      </div>

      {/* Input Box Footer */}
      <footer className="shrink-0 border-t bg-card/80 p-3 sm:p-4 backdrop-blur-xs">
        {focusedMessages.length > 0 && (
          <div className="mb-2 flex flex-wrap items-center gap-1.5">
            <span className="flex items-center gap-1 text-[11px] font-medium text-primary">
              <Sparkles className="size-3" />
              {copy.aiChatFocusedContext}:
            </span>
            {focusedMessages.map((msg) => {
              const sender = msg.fromName || msg.from || (msg.outgoing ? copy.me : "-");
              const preview = msg.subject || msg.body?.slice(0, 18) || copy.noSubject;
              return (
                <div
                  key={msg.id}
                  className="flex max-w-[200px] items-center gap-1 rounded-md border border-primary/25 bg-primary/10 px-2 py-0.5 text-[11px] text-foreground transition-colors hover:bg-primary/15"
                  title={`${sender}: ${preview}`}
                >
                  <span className="truncate font-medium">{sender}</span>
                  {onRemoveFocusedMessage && (
                    <button
                      type="button"
                      onClick={() => onRemoveFocusedMessage(msg.id)}
                      className="ml-0.5 rounded p-0.5 text-muted-foreground hover:bg-primary/20 hover:text-foreground cursor-pointer"
                      aria-label={copy.cancel}
                    >
                      <X className="size-3" />
                    </button>
                  )}
                </div>
              );
            })}
            {onClearFocusedMessages && focusedMessages.length > 1 && (
              <button
                type="button"
                onClick={onClearFocusedMessages}
                className="text-[11px] text-muted-foreground underline hover:text-foreground cursor-pointer"
              >
                {copy.aiChatClearFocus}
              </button>
            )}
          </div>
        )}
        <AIChatInput
          value={input}
          onChange={setInput}
          onSend={() => handleSend()}
          placeholder={
            focusedMessages.length > 0
              ? (isZh
                  ? `针对选中的 ${focusedMessages.length} 封邮件提问… (Enter 发送)`
                  : `Ask about ${focusedMessages.length} focused email(s)...`)
              : copy.aiChatPlaceholder
          }
          disabled={mutation.isPending}
          isPending={mutation.isPending}
          sendLabel={copy.aiChatSend}
          textareaRef={textareaRef}
          autoFocus={!isMobile}
          hint={
            <span className="text-[10px] text-muted-foreground">
              {focusedMessages.length > 0
                ? (isZh
                    ? `已锁定 ${focusedMessages.length} 封邮件作为重点上下文`
                    : `Focusing on ${focusedMessages.length} selected email(s)`)
                : copy.aiChatSubtitle}
            </span>
          }
        />
      </footer>

      {/* Clear Chat Confirmation Dialog */}
      <Dialog open={clearDialogOpen} onOpenChange={setClearDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{copy.aiChatClearHistory}</DialogTitle>
            <DialogDescription>
              {copy.aiChatClearConfirm}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="gap-2 sm:gap-0">
            <Button
              type="button"
              variant="ghost"
              onClick={() => setClearDialogOpen(false)}
            >
              {copy.cancel}
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={() => void handleConfirmClear()}
            >
              <Trash2 className="size-4" />
              {copy.aiChatClearHistory}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
