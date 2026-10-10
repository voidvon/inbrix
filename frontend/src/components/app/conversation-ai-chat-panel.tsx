import React, { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bot,
  Check,
  ChevronDown,
  Copy as CopyIcon,
  Loader2,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import {
  chatWithAIAgent,
  deleteConversationAIChat,
  getAIAgents,
  getConversationAIChat,
  saveConversationAIChat,
  type AIAgentChatRequest,
} from "../../lib/api";
import {
  buildConversationAIContext,
  copyToClipboard,
} from "../../lib/email-format";
import { type Copy, zh } from "../../lib/locale";
import { cn, formatTime } from "../../lib/utils";
import { Button } from "../ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { AIChatInput } from "./ai-chat-input";
import { AIStatusIndicator } from "./ai-status-indicator";
import { MarkdownContent } from "./markdown-content";
import { useAIStateMachine } from "../../lib/ai-state-machine";
import type { ConversationAIChatMessage, ConversationDetail } from "../../types";

export function ConversationAIChatPanel({
  copy,
  detail,
  onClose,
  isMobile = false,
  className,
}: {
  copy: Copy;
  detail: ConversationDetail;
  onClose: () => void;
  isMobile?: boolean;
  className?: string;
}) {
  const queryClient = useQueryClient();
  const [input, setInput] = useState("");
  const [copiedId, setCopiedId] = useState<string | null>(null);
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
  }, [messages.length]);

  // Build context from email messages with quotes filtered out
  const emailContext = useMemo(() => buildConversationAIContext(detail), [detail]);

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

      // Build message payload for backend
      const apiMessages: AIAgentChatRequest["messages"] = [];

      if (messages.length === 0) {
        // First message: prepend filtered email context as background
        const firstTurnWithContext = `【背景信息：以下是当前讨论的邮件往来记录（已自动过滤掉冗余重复的历史引用）】\n${emailContext}\n\n---\n【用户问题】：\n${text}`;
        apiMessages.push({ role: "user", content: firstTurnWithContext });
      } else {
        // Multi-turn conversation: keep context in the first user message
        for (let i = 0; i < messages.length; i++) {
          const m = messages[i];
          if (i === 0 && m.role === "user") {
            const firstTurnWithContext = `【背景信息：以下是当前讨论的邮件往来记录（已自动过滤掉冗余重复的历史引用）】\n${emailContext}\n\n---\n【用户问题】：\n${m.content}`;
            apiMessages.push({ role: "user", content: firstTurnWithContext });
          } else {
            apiMessages.push({ role: m.role, content: m.content });
          }
        }
        apiMessages.push({ role: "user", content: text });
      }

      return await aiState.run(async () => {
        const res = await chatWithAIAgent({
          accountEmail,
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
        createdAt: now,
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
      toast.error(err instanceof Error ? err.message : "AI 回复失败");
    },
  });

  const handleSend = (textToSend?: string) => {
    const text = (textToSend ?? input).trim();
    if (!text || mutation.isPending) return;
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

  const handleClear = async () => {
    if (messages.length === 0) return;
    if (!window.confirm(copy.aiChatClearConfirm)) return;

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
          {messages.length > 0 && (
            <Button
              variant="ghost"
              size="icon"
              className="size-7 text-muted-foreground hover:text-destructive"
              onClick={() => void handleClear()}
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
        ) : messages.length === 0 ? (
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
          messages.map((message) => {
            const isUser = message.role === "user";
            return (
              <div
                key={message.id}
                className={cn("flex flex-col gap-1", isUser ? "items-end" : "items-start")}
              >
                <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground px-1">
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
                    "group relative min-w-0 max-w-[88%] sm:max-w-[85%] rounded-2xl px-3.5 py-2.5 text-xs sm:text-sm leading-relaxed break-words shadow-xs",
                    isUser
                      ? "bg-primary text-primary-foreground rounded-tr-xs whitespace-pre-wrap"
                      : "bg-secondary text-secondary-foreground rounded-tl-xs"
                  )}
                >
                  {isUser ? (
                    message.content
                  ) : (
                    <MarkdownContent
                      content={message.content}
                      copyLabels={{
                        copy: copy.copyAIContent,
                        copied: copy.aiChatCopySuccess,
                      }}
                    />
                  )}
                  {!isUser && (
                    <div className="mt-2 flex items-center justify-end gap-1 pt-1 border-t border-border/30 opacity-70 group-hover:opacity-100 transition-opacity">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="size-5.5 text-muted-foreground hover:text-foreground"
                        onClick={() => void handleCopy(message.id, message.content)}
                        title={copy.copyAIContent}
                      >
                        {copiedId === message.id ? (
                          <Check className="size-3 text-emerald-600 dark:text-emerald-400" />
                        ) : (
                          <CopyIcon className="size-3" />
                        )}
                      </Button>
                    </div>
                  )}
                </div>
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
        <AIChatInput
          value={input}
          onChange={setInput}
          onSend={() => handleSend()}
          placeholder={copy.aiChatPlaceholder}
          disabled={mutation.isPending}
          isPending={mutation.isPending}
          sendLabel={copy.aiChatSend}
          textareaRef={textareaRef}
          autoFocus={!isMobile}
          hint={
            <span className="text-[10px] text-muted-foreground">
              {copy.aiChatSubtitle}
            </span>
          }
        />
      </footer>
    </div>
  );
}
