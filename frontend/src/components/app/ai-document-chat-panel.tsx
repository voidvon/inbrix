import { AIQuestionCard } from "./ai-question-card";
import { getAIChatQuestion } from "../../lib/ai-chat-parts";
import { useAIChatAttachments } from "../../lib/use-ai-chat-attachments";
import { useAIDocumentSync } from "../../lib/use-ai-document-sync";
import { useQueryErrorToast } from "../../lib/use-query-error-toast";
import { AIChatImages } from "./ai-chat-images";
import type { AIChatAttachment } from "../../types";
import { ConversationDocumentCard } from "./conversation-document-card";
import { AIBusinessToolbar } from "./ai-business-controls";
import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Tags,
  ArrowLeft,
  BookmarkCheck,
  Bot,
  Calculator,
  ChevronDown,
  ChevronRight,
  Copy as CopyIcon,
  ExternalLink,
  FileSpreadsheet,
  FileText,
  History,
  Mail,
  Plus,
  Search,
  Sparkles,
  Trash2,
  Wrench,
} from "lucide-react";
import { toast } from "../../lib/toast";
import {
  chatWithAIAgent, ApiError,
  stopAIGeneration,
  getAIAgents,
  getAITaskBindings,
  createAIChatSession, deleteAIChatSession, getAIChatSessions, getAIChatSession, updateAIChatSession,
  type AIBusinessContext,
  type AIAgentChatRequest,
} from "../../lib/api";
import { copyToClipboard } from "../../lib/email-format";
import {
  type AIDocumentGeneratedData, type AIDocumentMessage,
  resolveGeneratedDocument, createInitialSession, toAIDocumentSession,
} from "../../lib/ai-document-storage";
import {
  persistServerDocument,
  type DocumentTemplate,
  type StoredDocument,
} from "../../lib/document-storage";
import { cn, formatTime } from "../../lib/utils";
import { type Copy, zh } from "../../lib/locale";
import { Badge } from "../ui/badge";
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
import { MarkdownContent } from "./markdown-content";
import { useAIStateMachine } from "../../lib/ai-state-machine";

export function AIDocumentChatPanel({
  copy,
  accountEmail,
  onBack,
  onOpenDocument,
  className,
}: {
  copy: Copy;
  accountEmail: string;
  onBack?: () => void;
  onOpenDocument?: (doc: StoredDocument) => void;
  className?: string;
}) {
  const isZh = copy === zh;
  const queryClient = useQueryClient();
  useAIDocumentSync();
  const sessionsQuery = useInfiniteQuery({
    queryKey: ["ai-document-sessions", accountEmail],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => getAIChatSessions(accountEmail, pageParam),
    getNextPageParam: page => page.nextCursor,
    refetchInterval: query => query.state.status !== "error" && query.state.data?.pages.some(page => page.sessions.some(s => s.lastRun?.status === "pending")) ? 3000 : false,
    enabled: !!accountEmail,
  });
  const summaries = [...new Map((!sessionsQuery.isError && sessionsQuery.data?.pages.flatMap(page => page.sessions) || []).map(session => [session.id, session])).values()];
  const [activeId, setActiveId] = useState("");
  const selectedId = activeId || summaries[0]?.id || "";
  const sessionQuery = useQuery({
    queryKey: ["ai-document-session", accountEmail, selectedId],
    queryFn: () => getAIChatSession(accountEmail, selectedId),
    enabled: !!accountEmail && !!selectedId,
    refetchInterval: query => query.state.status !== "error" && query.state.data?.lastRun?.status === "pending" ? 3000 : false,
  });
  const sessionData = sessionQuery.isError ? undefined : sessionQuery.data;
  const sessions = summaries.map(session => toAIDocumentSession(session, copy));
  const requestRef = useRef<AbortController | null>(null);
  const [stopping, setStopping] = useState(false);
  const [stopped, setStopped] = useState(false);
  const retryRef = useRef<{ key: string; payload: AIAgentChatRequest } | null>(null);
  useEffect(() => {
    const run = sessionQuery.data?.lastRun;
    const pending = retryRef.current?.payload;
    if (run?.status === "failed" && !run.retryable && (run.requestId === pending?.requestId || run.requestId === pending?.retryRequestId)) retryRef.current = null;
  }, [sessionQuery.data]);
  const activeAccount = useRef(accountEmail); activeAccount.current = accountEmail;
  const [input, setInput] = useState("");
  const imageAttachments = useAIChatAttachments(accountEmail, "document-chat", isZh);
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});
  const scrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const currentSession = sessionData ? toAIDocumentSession(sessionData, copy) : sessions.find(s => s.id === selectedId) || createInitialSession(copy);
  const invalidateSession = async (scope: string, id?: string) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["ai-document-sessions", scope] }),
      queryClient.invalidateQueries({ queryKey: ["ai-document-session", scope, ...(id ? [id] : [])] }),
    ]);
  };

  useEffect(() => {
    setActiveId(""); setInput(""); retryRef.current = null;
    return () => { requestRef.current?.abort(); requestRef.current = null; };
  }, [accountEmail]);

  useEffect(() => {
    const available = sessionsQuery.data?.pages.flatMap(page => page.sessions);
    if (available && !available.some(session => session.id === activeId)) setActiveId(available[0]?.id || "");
  }, [sessionsQuery.data, activeId]);

  useEffect(() => {
    const summary = sessionsQuery.data?.pages.flatMap(page => page.sessions).find(s => s.id === selectedId);
    if (summary && sessionQuery.data && summary.version > sessionQuery.data.version) {
      void queryClient.invalidateQueries({ queryKey: ["ai-document-session", accountEmail, selectedId] });
    }
  }, [sessionsQuery.data, sessionQuery.data, queryClient, accountEmail, selectedId]);

  const olderMessages = useMutation({
    mutationFn: async () => ({ scope: accountEmail, id: selectedId, page: await getAIChatSession(accountEmail, selectedId, sessionQuery.data?.nextBefore) }),
    onSuccess: ({ scope, id, page }) => queryClient.setQueryData(["ai-document-session", scope, id], (previous: typeof sessionQuery.data) => previous?.version === page.version ? { ...previous, messages: [...page.messages, ...previous.messages], hasMoreMessages: page.hasMoreMessages, nextBefore: page.nextBefore } : previous),
    onError: (err: Error) => toast.error(err.message),
  });

  // Fetch user configured AI Agents from backend
  const agentsQuery = useQuery({
    queryKey: ["document-ai-agents", accountEmail],
    queryFn: getAIAgents,
  });
  const agents = agentsQuery.isError ? [] : agentsQuery.data?.agents || [];
  const bindings = useQuery({ queryKey: ["document-ai-bindings", accountEmail], queryFn: getAITaskBindings });
  useQueryErrorToast(agentsQuery.error, copy.loadFailed, "document-agents-error", () => void agentsQuery.refetch(), copy.retry);
  useQueryErrorToast(bindings.error, copy.loadFailed, "document-bindings-error", () => void bindings.refetch(), copy.retry);
  const defaultAgentId = (bindings.isError ? undefined : bindings.data)?.bindings.find(b => b.accountEmail === accountEmail && b.taskType === "email_draft")?.agentId || "";
  const businessContext = { ...(currentSession.businessContext || { agentId: currentSession.agentId || "", skillId: "", brands: [] }), agentId: currentSession.agentId || defaultAgentId };
  const currentAgent = agents.find((a) => a.id === businessContext.agentId);

  const scrollToBottom = () => {
    window.requestAnimationFrame(() => {
      if (scrollRef.current) {
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
      }
    });
  };

  useEffect(() => {
    scrollToBottom();
  }, [currentSession.messages.at(-1)?.id, activeId]);

  const selectSession = (id: string) => {
    imageAttachments.discard();
    setStopped(false);
    setActiveId(id); setInput(""); retryRef.current = null;
  };
  const [deleteSessionId, setDeleteSessionId] = useState<string | null>(null);
  const sessionMutation = useMutation({
    mutationFn: async (action: { type: "create"; documentType: DocumentTemplate } | { type: "delete"; id: string } | { type: "business"; value: AIBusinessContext }) => {
      const scope = accountEmail;
      let id = currentSession.id;
      if (!id || action.type === "create") {
        const documentType = action.type === "create" ? action.documentType : currentSession.documentType;
        const session = await createAIChatSession(scope, documentType, isZh ? "文档助手对话" : "Document Assistant");
        id = session.id;
        queryClient.setQueryData(["ai-document-session", scope, id], session);
        queryClient.setQueryData(["ai-document-sessions", scope], (previous: typeof sessionsQuery.data) => previous ? { ...previous, pages: previous.pages.map((page, index) => index === 0 ? { ...page, sessions: [session, ...page.sessions] } : page) } : { pages: [{ sessions: [session] }], pageParams: [undefined] });
      }
      if (action.type === "delete") {
        const session = sessions.find(s => s.id === action.id);
        if (session) await deleteAIChatSession(scope, session.id, session.version);
        id = sessions.find(s => s.id !== action.id)?.id || "";
      } else if (action.type === "business") {
        await updateAIChatSession(scope, id, currentSession.id ? currentSession.version : 0, action.value);
      }
      return { scope, id, type: action.type };
    },
    onSuccess: async ({ scope, id, type }) => {
      if (activeAccount.current === scope) {
        if (type !== "business") { imageAttachments.discard(); setInput(""); }
        setActiveId(id); setDeleteSessionId(null); retryRef.current = null;
      }
      if (type === "delete") await queryClient.invalidateQueries({ queryKey: ["ai-document-sessions", scope] });
      else await invalidateSession(scope, id);
    },
    onError: (err: Error) => { toast.error(err.message); void sessionsQuery.refetch(); },
  });
  const handleNewSession = (documentType: DocumentTemplate = "quotation") => sessionMutation.mutate({ type: "create", documentType });
  const handleDeleteSession = (id: string) => { if (id) setDeleteSessionId(id); };
  const confirmDeleteSession = () => { if (deleteSessionId) sessionMutation.mutate({ type: "delete", id: deleteSessionId }); };
  const handleSelectAgent = (agentId?: string) => sessionMutation.mutate({ type: "business", value: { ...businessContext, agentId: agentId || "" } });

  const toggleToolExpand = (msgId: string) => {
    setExpandedTools((prev) => ({ ...prev, [msgId]: !prev[msgId] }));
  };

  const aiState = useAIStateMachine({
    taskType: "document",
    locale: isZh ? "zh" : "en",
  });

  // Agent Chat Mutation with Tool Calling support
  const mutation = useMutation({
    onMutate: () => ({ scope: accountEmail }),
    mutationFn: async ({ text, attachments, retryRequestId }: { text: string; attachments: AIChatAttachment[]; retryRequestId?: string }) => {
      if (!accountEmail) {
        throw new Error(isZh ? "未检测到登录账户邮箱，请先配置邮箱账户" : "Account email required");
      }

      const scope = accountEmail;
      const controller = new AbortController(); requestRef.current = controller;
      let sessionId = currentSession.id;
      let version = currentSession.version;
      if (!sessionId) {
        const session = await createAIChatSession(scope, currentSession.documentType, currentSession.title);
        sessionId = session.id; version = session.version;
        if (activeAccount.current === scope) setActiveId(sessionId);
        queryClient.setQueryData(["ai-document-session", scope, sessionId], session);
        queryClient.setQueryData(["ai-document-sessions", scope], (previous: typeof sessionsQuery.data) => previous ? { ...previous, pages: previous.pages.map((page, index) => index === 0 ? { ...page, sessions: [session, ...page.sessions] } : page) } : { pages: [{ sessions: [session] }], pageParams: [undefined] });
      }
      controller.signal.throwIfAborted();
      const key = JSON.stringify([scope, sessionId, text, attachments]);
      const payload: AIAgentChatRequest = retryRequestId ? { accountEmail: scope, sessionId, retryRequestId, messages: [] } : retryRef.current?.key === key ? retryRef.current.payload : {
        accountEmail: scope, sessionId, requestId: crypto.randomUUID(), version,
        businessContext, messages: [{ role: "user", content: text, attachments }],

      };
      retryRef.current = { key, payload };
      return await aiState.run(async () => {
        const res = await chatWithAIAgent(payload, { signal: controller.signal });
        controller.signal.throwIfAborted();
        if (res.toolCalls && res.toolCalls.length > 0) {
          const first = res.toolCalls[0];
          aiState.setToolCall(first.name, first.summary);
        }

        return { res, scope, sessionId, retried: !!retryRequestId };
      });
    },
    onSuccess: async ({ scope, sessionId, retried }) => {
      if (activeAccount.current === scope) { if (!retried) imageAttachments.clear(); retryRef.current = null; }
      await invalidateSession(scope, sessionId);
    },
    onError: async (err: Error, { text, retryRequestId }, context) => {
      if (activeAccount.current !== context?.scope) return;
      if (err.name !== "AbortError") { if (!retryRequestId) setInput(text); toast.error(err.message); }
      const fresh = await sessionQuery.refetch();
      await sessionsQuery.refetch();
      const payload = retryRef.current?.payload;
      const run = fresh.data?.lastRun;
      if (err instanceof ApiError && err.status === 409 && (run?.requestId !== payload?.requestId || run?.status === "failed" && !run.retryable)) retryRef.current = null;
    },
  });
  const lastRun = sessionData?.lastRun;
  const submitted = mutation.variables;
  const accepted = lastRun?.requestId === (retryRef.current?.payload.requestId || retryRef.current?.payload.retryRequestId);
  const displayedMessages: AIDocumentMessage[] = sessionsQuery.isError || sessionQuery.isError ? [] : [...currentSession.messages,
    ...(mutation.isPending && submitted && !accepted ? [{ id: "pending-user", role: "user" as const, content: submitted.text, attachments: submitted.attachments, createdAt: new Date().toISOString() }] : []),
  ];
  const pendingQuestion = getAIChatQuestion(displayedMessages);
  const busy = stopping || agentsQuery.isError || bindings.isError || lastRun?.status === "pending" || mutation.isPending || sessionMutation.isPending || sessionsQuery.isPending || sessionsQuery.isError || (!!selectedId && (sessionQuery.isFetching || sessionQuery.isPending || sessionQuery.isError));
  const handleSend = (answer?: string) => {
    const text = (answer ?? input).trim();
    if ((!text && !imageAttachments.attachments.length) || imageAttachments.blocked || busy) return;
    imageAttachments.retain();
    setStopped(false);
    setInput(""); mutation.mutate({ text, attachments: imageAttachments.attachments });
  };
  const handleStop = async () => {
    if (stopping) return;
    const payload = retryRef.current?.payload;
    const requestId = payload?.requestId || payload?.retryRequestId || (lastRun?.status === "pending" ? lastRun.requestId : undefined);
    requestRef.current?.abort();
    setStopping(true);
    setStopped(true);
    aiState.reset();
    try {
      if (requestId) await stopAIGeneration(accountEmail, requestId, payload?.sessionId || selectedId);
      await invalidateSession(accountEmail, payload?.sessionId || selectedId);
    } catch (error) {
      setStopped(false);
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      retryRef.current = null;
      setStopping(false);
    }
  };

  const handleSaveToDocs = async (doc: AIDocumentGeneratedData) => {
    try {
      const storedDoc = await resolveGeneratedDocument(doc);
      if (!storedDoc.revision) await persistServerDocument(storedDoc);
      toast.success(copy.aiDocumentSavedToDocuments);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : copy.loadFailed);
    }
  };

  const handleOpenInEditor = async (doc: AIDocumentGeneratedData) => {
    try {
      const stored = await resolveGeneratedDocument(doc);
      onOpenDocument?.(stored);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : copy.loadFailed);
    }
  };

  const quickPrompts = [
    {
      label: isZh ? "🔍 检索邮件制作报价单" : "🔍 Search email to quote",
      text: isZh
        ? "请分析相关客户询价邮件，列出每项产品的品牌、规格、数量及缺失参数，再查询产品库匹配产品与价格，制作待审核的报价草稿"
        : "Analyze the customer inquiry emails, identify brands, specifications, quantities and missing requirements, then verify catalog products/prices and draft a quotation",
    },
    {
      label: copy.aiDocumentQuickQuotation,
      text: isZh
        ? "请根据我在对话中提供的客户需求制作报价草稿。先核对产品规格和价格，缺少品牌、规格、数量或商业条款时请先列出待确认信息。"
        : "Draft a quotation from the requirements in this conversation. Verify specifications/prices and identify missing brands, specifications, quantities or terms first.",
    },
    {
      label: copy.aiDocumentQuickContract,
      text: isZh
        ? "请基于我指定的已保存报价单起草销售合同，沿用报价明细和约定价格；如有多个候选报价，请让我选择，并列出需要确认的合同条款。"
        : "Draft a sales contract from the saved quotation I designate, retaining its line items and agreed prices. Ask me to choose if ambiguous and identify missing contract terms.",
    },
    {
      label: copy.aiDocumentQuickUpdateTerms,
      text: isZh
        ? "将交货期修改为合同生效后45天，付款方式改为发货前100%全款结清"
        : "Change delivery time to 45 days after contract, 100% prepayment before delivery",
    },
  ];

  return (
    <section
      data-testid="ai-document-chat-panel"
      className={cn("flex min-w-0 flex-1 flex-col overflow-hidden bg-background", className)}
    >
      {/* Top Header */}
      <header className="flex h-14 shrink-0 items-center justify-between border-b bg-card px-3 sm:px-4">
        <div className="flex min-w-0 items-center gap-2">
          {onBack && (
            <Button
              variant="ghost"
              size="icon"
              className="lg:hidden"
              onClick={onBack}
              aria-label={copy.back}
              title={copy.back}
            >
              <ArrowLeft />
            </Button>
          )}
          <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-gradient-to-br from-indigo-500 to-purple-600 text-white shadow-xs">
            <Sparkles className="size-4" />
          </div>
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <h2 className="truncate text-sm font-semibold"><span className="sm:hidden">AI</span><span className="hidden sm:inline">{copy.aiDocument}</span></h2>
              <Badge variant="secondary" className="hidden sm:inline-flex px-1.5 py-0 text-[10px] font-normal text-muted-foreground">
                Agent
              </Badge>
            </div>
            <p className="hidden sm:block truncate text-xs text-muted-foreground">{currentSession?.title || copy.aiDocumentSubtitle}</p>
          </div>
        </div>

        <div className="flex items-center gap-1.5">
          {/* AI Agent Persona Switcher */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="outline" size="sm" className="h-8 gap-1.5 px-2.5 text-xs">
                  <Bot className="size-3.5 text-indigo-500" />
                  <span className="max-w-28 truncate">
                    {currentAgent ? currentAgent.name : isZh ? "默认文档助手" : "Default Agent"}
                  </span>
                  <ChevronDown className="size-3 text-muted-foreground" />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-56">
              <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground">
                {isZh ? "选择 AI 智能体角色" : "Select AI Agent"}
              </div>
              <DropdownMenuItem
                className={cn("cursor-pointer gap-2 text-xs", !currentSession?.agentId && "bg-muted font-medium")}
                disabled={busy}
                onClick={() => handleSelectAgent(undefined)}
              >
                <Sparkles className="size-3.5 text-indigo-500" />
                <span>{isZh ? "默认文档助手（全能专家）" : "Default Document Agent"}</span>
              </DropdownMenuItem>
              {agents.length > 0 && <DropdownMenuSeparator />}
              {agents.map((ag) => (
                <DropdownMenuItem
                  key={ag.id}
                  className={cn("cursor-pointer gap-2 text-xs", currentSession?.agentId === ag.id && "bg-muted font-medium")}
                  disabled={busy}
                onClick={() => handleSelectAgent(ag.id)}
                >
                  <Bot className="size-3.5 text-primary" />
                  <span className="truncate">{ag.name}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Session Switcher */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="outline" size="sm" className="h-8 gap-1.5 px-2.5 text-xs" aria-label={copy.aiDocumentHistory}>
                  <History className="size-3.5" />
                  <span className="hidden sm:inline">{copy.aiDocumentHistory}</span>
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-64 max-h-80 overflow-y-auto">
              <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground">{copy.aiDocumentHistory}</div>
              {sessions.map((s) => (
                <DropdownMenuItem
                  key={s.id}
                  className={cn("flex cursor-pointer items-center justify-between gap-2 px-2 py-1.5 text-xs", s.id === currentSession.id && "bg-muted font-medium")}
                  disabled={busy}
                    onClick={() => selectSession(s.id)}
                >
                  <span className="min-w-0 flex-1 truncate">{s.title}</span>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{formatTime(s.updatedAt)}</span>
                </DropdownMenuItem>
              ))}
              {sessionsQuery.hasNextPage && <DropdownMenuItem closeOnClick={false} disabled={sessionsQuery.isFetchingNextPage} onClick={() => void sessionsQuery.fetchNextPage()}>{isZh ? "加载更多会话" : "Load more chats"}</DropdownMenuItem>}
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className="cursor-pointer gap-2 text-xs text-primary font-medium"
                disabled={busy}
                onClick={() => handleNewSession("quotation")}
              >
                <Plus className="size-3.5" />
                {copy.aiDocumentNewChat}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <Button
            variant="ghost"
            size="sm"
            className="h-8 gap-1 px-2 text-xs"
            disabled={busy}
                onClick={() => handleNewSession(currentSession?.documentType || "quotation")}
            title={copy.aiDocumentNewChat}
          >
            <Plus className="size-3.5" />
            <span className="hidden md:inline">{copy.aiDocumentNewChat}</span>
          </Button>

          <Button
            variant="ghost"
            size="icon"
            className="size-8 text-muted-foreground hover:text-destructive"
            disabled={busy}
            onClick={() => handleDeleteSession(currentSession.id)}
            aria-label={copy.aiDocumentClearChat}
            title={copy.aiDocumentClearChat}
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </header>

      {sessionsQuery.isError && <div role="alert" className="px-4 py-2 text-sm text-destructive">{sessionsQuery.error.message}<Button variant="ghost" onClick={() => void sessionsQuery.refetch()}>{isZh ? "重试" : "Retry"}</Button></div>}
      {!mutation.isPending && lastRun?.status === "pending" && <p role="status" className="px-4 py-2 text-xs text-muted-foreground">{isZh ? "会话正在生成回复…" : "A reply is being generated…"}</p>}
      {sessionQuery.isError && <div role="alert" className="px-4 py-2 text-sm text-destructive">{sessionQuery.error.message}<Button variant="ghost" onClick={() => void sessionQuery.refetch()}>{isZh ? "重试" : "Retry"}</Button></div>}
      {lastRun?.retryable && <div className="flex items-center justify-between gap-2 px-4 py-2 text-xs text-muted-foreground"><span>{isZh ? "上次回复未完成，可继续重试。" : "The last reply failed. You can retry it."}</span><Button variant="outline" size="sm" disabled={busy} onClick={() => mutation.mutate({ text: "", attachments: [], retryRequestId: lastRun.requestId })}>{isZh ? "重试上次回复" : "Retry last reply"}</Button></div>}
      {/* Messages Scroll Area */}
      <AIBusinessToolbar copy={copy} accountEmail={accountEmail} value={businessContext} disabled={busy} onChange={value => sessionMutation.mutate({ type: "business", value })} />
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
        {sessionData?.hasMoreMessages && <Button variant="ghost" size="sm" disabled={olderMessages.isPending} onClick={() => olderMessages.mutate()}>{isZh ? "加载更早消息" : "Load earlier messages"}</Button>}
        <div className="mx-auto flex max-w-3xl flex-col gap-4">
          {displayedMessages.map((msg) => (
            <div
              key={msg.id}
              className={cn("flex gap-3", msg.role === "user" ? "flex-row-reverse" : "flex-row")}
            >
              {msg.role === "assistant" && (
                <div className="flex size-7 shrink-0 select-none items-center justify-center rounded-full bg-gradient-to-br from-indigo-500 to-purple-600 text-white shadow-xs">
                  <Sparkles className="size-3.5" />
                </div>
              )}

              <div className={cn("flex max-w-[85%] flex-col sm:max-w-[78%]", msg.role === "user" && "items-end")}>
                {/* Tool Calling Execution Trail */}
                {msg.toolCalls && msg.toolCalls.length > 0 && (
                  <div className="mb-2 w-full rounded-xl border border-border/80 bg-muted/20 p-2.5 text-xs">
                    <button
                      type="button"
                      className="flex w-full items-center justify-between text-left font-medium text-foreground hover:opacity-80 cursor-pointer"
                      onClick={() => toggleToolExpand(msg.id)}
                    >
                      <span className="flex items-center gap-1.5 text-xs font-semibold text-indigo-600 dark:text-indigo-400">
                        <Wrench className="size-3.5" />
                        {isZh ? `智能 Agent 工具调用 (${msg.toolCalls.length})` : `Agent Tool Calls (${msg.toolCalls.length})`}
                      </span>
                      <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
                        <span>{expandedTools[msg.id] ? (isZh ? "收起明细" : "Collapse") : (isZh ? "查看详情" : "Details")}</span>
                        {expandedTools[msg.id] ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
                      </span>
                    </button>
                    <div className="mt-2 space-y-1.5">
                      {msg.toolCalls.map((tc, idx) => (
                        <div key={idx} className="flex flex-col gap-1 rounded-lg border bg-background/90 p-2 text-xs shadow-2xs">
                          <div className="flex items-center gap-1.5">
                            {tc.name === "search_emails" && <Search className="size-3.5 text-blue-500" />}
                            {tc.name === "read_email" && <Mail className="size-3.5 text-amber-500" />}
                            {tc.name === "lookup_price" && <Tags className="size-3.5 text-cyan-600" />}
                            {tc.name === "calculate_pricing" && <Calculator className="size-3.5 text-emerald-500" />}
                            {tc.name === "generate_document" && <FileSpreadsheet className="size-3.5 text-purple-500" />}
                            {!["search_emails", "read_email", "lookup_price", "calculate_pricing", "generate_document"].includes(tc.name) && (
                              <Wrench className="size-3.5 text-muted-foreground" />
                            )}
                            <span className="font-medium text-foreground">
                              {tc.name === "ask_user"
                                ? (isZh ? "向你提问" : "Ask for clarification")
                                : tc.name === "search_emails"
                                ? (isZh ? "检索邮箱邮件" : "Search Emails")
                                : tc.name === "read_email"
                                ? (isZh ? "读取邮件正文" : "Read Email")
                                : tc.name === "lookup_price"
                                ? (isZh ? "查询价格表" : "Look Up Price")
                                : tc.name === "calculate_pricing"
                                ? (isZh ? "核算价格与税费" : "Calculate Pricing")
                                : tc.name === "generate_document"
                                ? (isZh ? "生成结构化文档" : "Generate Document")
                                : tc.name}
                            </span>
                            <Badge
                              variant="outline"
                              className={cn(
                                "ml-auto text-[10px] px-1 py-0",
                                tc.status === "error"
                                  ? "text-destructive border-destructive/30 bg-destructive/10"
                                  : "text-emerald-600 border-emerald-300/40 bg-emerald-50 dark:bg-emerald-950/40"
                              )}
                            >
                              {tc.status === "error" ? (isZh ? "出错" : "Error") : (isZh ? "执行成功" : "Success")}
                            </Badge>
                          </div>
                          {tc.summary && (
                            <p className="text-[11px] text-muted-foreground leading-normal pl-5">
                              {tc.summary}
                            </p>
                          )}
                          {expandedTools[msg.id] && (
                            <div className="mt-1 pl-5">
                              <pre className="max-h-36 overflow-auto rounded bg-muted/60 p-2 font-mono text-[10px] text-foreground leading-tight">
                                {JSON.stringify({ arguments: tc.args, result: tc.result }, null, 2)}
                              </pre>
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                <AIChatImages attachments={msg.attachments} accountEmail={accountEmail} />
                {/* Assistant or User Content Bubble */}
                {(msg.role === "assistant" || Boolean(msg.content.trim())) && <div
                  className={cn(
                    "min-w-0 rounded-2xl px-4 py-2.5 text-sm leading-relaxed shadow-2xs break-words select-text cursor-text",
                    msg.role === "user"
                      ? "rounded-tr-xs bg-primary text-primary-foreground whitespace-pre-wrap selection:bg-primary-foreground/30 selection:text-primary-foreground"
                      : "rounded-tl-xs border bg-card text-foreground"
                  )}
                >
                  {msg.role === "assistant" ? (
                    <MarkdownContent
                      content={sanitizeAssistantContent(msg.content, msg.toolCalls, isZh)}
                      copyLabels={{
                        copy: copy.copyAIContent,
                        copied: copy.aiChatCopySuccess,
                      }}
                    />
                  ) : (
                    <p>{msg.content}</p>
                  )}
                </div>}

                {/* Interactive Document Preview Card */}
                {msg.documentAction && <ConversationDocumentCard copy={copy} accountEmail={accountEmail} action={msg.documentAction} />}
                {msg.document && (
                  <div className="mt-2.5 w-full rounded-xl border bg-card p-3.5 shadow-xs transition-shadow hover:shadow-md">
                    <div className="flex items-start justify-between gap-2 border-b pb-2.5">
                      <div className="flex items-center gap-2">
                        {msg.document.type === "quotation" ? (
                          <div className="flex size-7 items-center justify-center rounded-md bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-300">
                            <FileSpreadsheet className="size-4" />
                          </div>
                        ) : (
                          <div className="flex size-7 items-center justify-center rounded-md bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
                            <FileText className="size-4" />
                          </div>
                        )}
                        <div>
                          <h4 className="text-sm font-semibold">{msg.document.name}</h4>
                          <span className="text-xs text-muted-foreground">
                            {msg.document.company ? `${msg.document.company} · ` : ""}
                            {msg.document.type === "quotation" ? (isZh ? "标准报价单" : "Quotation") : (isZh ? "购销合同" : "Contract")}
                          </span>
                        </div>
                      </div>
                      <Badge variant="outline" className="text-[10px]">
                        {msg.document.type === "quotation" ? copy.quotation : copy.contract}
                      </Badge>
                    </div>

                    {/* Preview details if available */}
                    {msg.document.items && msg.document.items.length > 0 && (
                      <div className="mt-2.5 overflow-x-auto rounded-md border bg-muted/40 p-2 text-xs">
                        <div className="mb-1 text-[11px] font-medium text-muted-foreground">
                          {isZh ? `产品明细（共 ${msg.document.items.length} 项）：` : "Product Line Items:"}
                        </div>
                        <table className="w-full text-left">
                          <thead>
                            <tr className="border-b text-[10px] text-muted-foreground">
                              <th className="pb-1">{isZh ? "型号" : "Model"}</th>
                              <th className="pb-1 text-center">{isZh ? "数量" : "Qty"}</th>
                              <th className="pb-1 text-right">{isZh ? "单价" : "Price"}</th>
                              <th className="pb-1 text-right">{isZh ? "金额" : "Amount"}</th>
                            </tr>
                          </thead>
                          <tbody>
                            {msg.document.items.slice(0, 4).map((it, idx) => (
                              <tr key={idx} className="border-b/50">
                                <td className="py-1 font-mono text-[11px]">{it.model || it.description}</td>
                                <td className="py-1 text-center">{it.qty}</td>
                                <td className="py-1 text-right">{it.price}</td>
                                <td className="py-1 text-right font-medium">{it.amount}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                        {msg.document.total && (
                          <div className="mt-1.5 flex justify-end gap-2 border-t pt-1 font-semibold text-foreground">
                            <span>{isZh ? "总金额:" : "Total:"}</span>
                            <span>¥{msg.document.total}</span>
                          </div>
                        )}
                      </div>
                    )}

                    {/* Terms preview if available */}
                    {msg.document.terms && msg.document.terms.length > 0 && (
                      <div className="mt-2 text-[11px] text-muted-foreground">
                        <span className="font-medium text-foreground">{isZh ? "主要条款: " : "Terms: "}</span>
                        {msg.document.terms.slice(0, 2).join(" · ")}
                      </div>
                    )}

                    {/* Action buttons on generated card */}
                    <div className="mt-3 flex flex-wrap items-center gap-1.5">
                      <Button
                        type="button"
                        size="sm"
                        className="h-7 gap-1 px-2.5 text-xs shadow-none cursor-pointer"
                        onClick={() => void handleOpenInEditor(msg.document!)}
                      >
                        <ExternalLink className="size-3" />
                        {copy.aiDocumentOpenInEditor}
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="h-7 gap-1 px-2.5 text-xs shadow-none cursor-pointer"
                        onClick={() => void handleSaveToDocs(msg.document!)}
                      >
                        <BookmarkCheck className="size-3" />
                        {copy.aiDocumentSaveToDocuments}
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-7 gap-1 px-2 text-xs cursor-pointer"
                        onClick={() => {
                          const textToCopy = msg.document?.items
                            ? `${msg.document.name}\n${msg.document.items.map((i) => `${i.model || i.description} x ${i.qty} = ${i.amount}`).join("\n")}`
                            : msg.document?.name || "";
                          void copyToClipboard(textToCopy);
                          toast.success(isZh ? "文档摘要已复制" : "Copied to clipboard");
                        }}
                      >
                        <CopyIcon className="size-3" />
                        {isZh ? "复制" : "Copy"}
                      </Button>
                    </div>
                  </div>
                )}

                <span className="mt-1 text-[10px] text-muted-foreground">
                  {formatTime(msg.createdAt)}
                </span>
              </div>
            </div>
          ))}

          {/* AI Drafting State */}
          {mutation.isPending && (
            <AIStatusIndicator
              snapshot={aiState.snapshot}
              variant="bubble"
              agentName={currentAgent?.name || (isZh ? "商务文档助手" : "Document Assistant")}
              showTimer
            />
          )}
        </div>
      </div>

      {/* Bottom Area: Suggestions + Text Input */}
      <footer className="shrink-0 border-t bg-card p-2.5 sm:p-4 pb-[max(0.625rem,env(safe-area-inset-bottom))]">
        <div className="mx-auto max-w-3xl">
          {/* Quick Suggestion Chips */}
          <div className="mb-2 flex items-center gap-1.5 overflow-x-auto pb-1 text-xs scrollbar-none touch-pan-x">
            {quickPrompts.map((p, idx) => (
              <button
                key={idx}
                type="button"
                className="shrink-0 cursor-pointer rounded-full border bg-background px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:bg-muted hover:text-foreground"
                onClick={() => {
                  setInput(p.text);
                  textareaRef.current?.focus();
                }}
              >
                {p.label}
              </button>
            ))}
          </div>

          {/* Input Box */}
          {stopped && <p role="status" className="mb-2 text-xs text-muted-foreground">{isZh ? "已停止生成。" : "Generation stopped."}</p>}
          {pendingQuestion ? (
            <AIQuestionCard key={displayedMessages.at(-1)?.id} question={pendingQuestion} isZh={isZh} disabled={busy} onSubmit={answer => handleSend(answer)} />
          ) : (
            <AIChatInput
              imageAttachments={imageAttachments}
              isZh={isZh}
              textareaRef={textareaRef}
              value={input}
              onChange={setInput}
              onSend={() => handleSend()}
              onStop={() => void handleStop()}
              isStopping={stopping}
              placeholder={copy.aiDocumentPromptPlaceholder}
              disabled={busy}
              isPending={mutation.isPending || stopping || lastRun?.status === "pending"}
              sendLabel={copy.send}
              hint={isZh ? "按 Enter 发送，Shift+Enter 换行" : "Enter to send, Shift+Enter for new line"}
            />
          )}
        </div>
      </footer>

      {/* Delete Chat Session Confirmation Dialog */}
      <Dialog open={Boolean(deleteSessionId)} onOpenChange={(open) => !open && setDeleteSessionId(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{copy.aiDocumentClearChat}</DialogTitle>
            <DialogDescription>
              {copy.aiDocumentDeleteSessionConfirm}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="gap-2 sm:gap-0">
            <Button
              type="button"
              variant="ghost"
              onClick={() => setDeleteSessionId(null)}
            >
              {copy.cancel}
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={confirmDeleteSession}
            >
              <Trash2 className="size-4" />
              {isZh ? "确认删除" : "Delete"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function sanitizeAssistantContent(
  content?: string,
  toolCalls?: { name: string; summary?: string }[],
  isZh?: boolean
): string {
  if (!content) {
    if (toolCalls && toolCalls.length > 0) {
      return isZh
        ? `已完成 ${toolCalls.length} 项智能工具调用与数据检索。`
        : `Completed ${toolCalls.length} tool calls and data retrieval.`;
    }
    return isZh ? "已为您处理完成。" : "Done.";
  }

  // Strip ```tool_call ... ``` or ```json:tool_call ... ``` fences
  const stripped = content.replace(/```(?:tool_call|json:tool_call|toolCall)[\s\S]*?```/gi, "").trim();
  if (!stripped) {
    if (toolCalls && toolCalls.length > 0) {
      return isZh
        ? `已完成 ${toolCalls.length} 项智能工具调用与数据检索。`
        : `Completed ${toolCalls.length} tool calls and data retrieval.`;
    }
    return isZh ? "已为您处理完成。" : "Done.";
  }
  return stripped;
}
