import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
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
  Loader2,
  Mail,
  Plus,
  Search,
  Sparkles,
  Trash2,
  Wrench,
} from "lucide-react";
import { toast } from "sonner";
import {
  chatWithAIAgent,
  getAIAgents,
  type AIAgentChatRequest,
} from "../../lib/api";
import { copyToClipboard } from "../../lib/email-format";
import {
  type AIDocumentGeneratedData,
  type AIDocumentMessage,
  type AIDocumentSession,
  convertGeneratedToStoredDocument,
  createNewAIDocumentSession,
  deleteAIDocumentSession,
  getActiveSessionId,
  readAIDocumentSessions,
  saveMessageToSession,
  setActiveSessionId,
  updateSessionAgent,
} from "../../lib/ai-document-storage";
import {
  cleanDocumentVariablesForConversation,
  documentTemplateHTML,
  isSellerConcept,
  persistServerDocument,
  type DocumentTemplate,
  type StoredDocument,
} from "../../lib/document-storage";
import { cn, formatTime } from "../../lib/utils";
import { type Copy, zh } from "../../lib/locale";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { AIChatInput } from "./ai-chat-input";
import { MarkdownContent } from "./markdown-content";

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
  const [sessions, setSessions] = useState<AIDocumentSession[]>(() => readAIDocumentSessions(copy));
  const [activeId, setActiveId] = useState<string>(() => getActiveSessionId() || sessions[0]?.id || "default-ai-session");
  const [input, setInput] = useState("");
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});
  const scrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const currentSession = sessions.find((s) => s.id === activeId) || sessions[0];

  // Fetch user configured AI Agents from backend
  const { data: agentsData } = useQuery({
    queryKey: ["ai-agents"],
    queryFn: getAIAgents,
    staleTime: 60_000,
  });
  const agents = agentsData?.agents || [];
  const currentAgent = agents.find((a) => a.id === currentSession?.agentId);

  const scrollToBottom = () => {
    window.requestAnimationFrame(() => {
      if (scrollRef.current) {
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
      }
    });
  };

  useEffect(() => {
    scrollToBottom();
  }, [currentSession?.messages?.length, activeId]);

  const selectSession = (id: string) => {
    setActiveId(id);
    setActiveSessionId(id);
  };

  const handleNewSession = (type: DocumentTemplate = "quotation") => {
    const s = createNewAIDocumentSession(copy, type);
    setSessions(readAIDocumentSessions(copy));
    setActiveId(s.id);
  };

  const handleDeleteSession = (sessionId: string) => {
    if (!window.confirm(copy.aiDocumentDeleteSessionConfirm)) return;
    const remaining = deleteAIDocumentSession(copy, sessionId);
    setSessions(remaining);
    setActiveId(remaining[0]?.id || "default-ai-session");
    toast.success(isZh ? "对话记录已删除" : "Chat session deleted");
  };

  const handleSelectAgent = (agentId?: string) => {
    const updated = updateSessionAgent(copy, currentSession.id, agentId);
    setSessions(updated);
    toast.success(
      isZh
        ? `已切换智能体角色为：${agents.find((a) => a.id === agentId)?.name || "默认文档助手"}`
        : `Switched agent persona`
    );
  };

  const toggleToolExpand = (msgId: string) => {
    setExpandedTools((prev) => ({ ...prev, [msgId]: !prev[msgId] }));
  };

  // Agent Chat Mutation with Tool Calling support
  const mutation = useMutation({
    mutationFn: async (instructionText: string) => {
      if (!accountEmail) {
        throw new Error(isZh ? "未检测到登录账户邮箱，请先配置邮箱账户" : "Account email required");
      }

      const text = instructionText.trim();
      const lastDocMsg = [...(currentSession?.messages || [])].reverse().find((m) => m.document);
      const prevDoc = lastDocMsg?.document;

      // Identify whether this is the start of the conversation or drafting a brand new document
      const userMsgCount = (currentSession?.messages || []).filter((m) => m.role === "user").length;
      const isStartOfConversation = userMsgCount === 0;
      const isExplicitNewDraft =
        /(?:重新|起草|制作|生成|新建|创建|开一份|来一份).*(?:报价|合同|协议)/i.test(text) &&
        !/(?:修改|调整|基于|在.*基础)/i.test(text);

      let activeDocForAgent = prevDoc;
      if (activeDocForAgent && (isStartOfConversation || isExplicitNewDraft)) {
        // Clean out pre-filled template data (products, customer info, memos) while strictly PRESERVING Seller info!
        activeDocForAgent = cleanDocumentVariablesForConversation(activeDocForAgent);
      }

      // Prepare previous conversation history for the AI Agent
      const apiMessages: AIAgentChatRequest["messages"] = (currentSession?.messages || [])
        .filter((m) => m.id !== "welcome-msg")
        .map((m) => ({
          role: m.role,
          content: m.content,
          toolCalls: m.toolCalls,
          document: m.document
            ? {
                id: m.document.id,
                type: m.document.type,
                title: m.document.name,
                counterparty: m.document.company || "",
                items: m.document.items,
                values: m.document.values,
              }
            : undefined,
        }));

      // Append current user message
      apiMessages.push({ role: "user", content: text });

      const res = await chatWithAIAgent({
        accountEmail,
        agentId: currentSession.agentId,
        messages: apiMessages,
        activeDocument: activeDocForAgent
          ? {
              type: activeDocForAgent.type,
              title: activeDocForAgent.name,
              counterparty: activeDocForAgent.company || "",
              items: activeDocForAgent.items,
              values: activeDocForAgent.values,
            }
          : undefined,
      });

      return { res, text, prevDoc };
    },
    onSuccess: ({ res, prevDoc }) => {
      let generatedDoc: AIDocumentGeneratedData | undefined = undefined;

      if (res.document) {
        const doc = res.document;
        const docType: DocumentTemplate = doc.type === "contract" ? "contract" : "quotation";
        const company = doc.counterparty || (isZh ? "客户单位" : "Client");
        const title =
          doc.title ||
          `${company} ${docType === "quotation" ? (isZh ? "标准报价单" : "Quotation") : (isZh ? "购销合同" : "Sales Contract")}`;

        // Ensure Seller information from previous document or template is preserved
        const mergedValues: Record<string, string> = { ...(doc.values || {}) };
        if (prevDoc?.values) {
          for (const [k, v] of Object.entries(prevDoc.values)) {
            if (isSellerConcept(k) && !mergedValues[k]) {
              mergedValues[k] = v;
            }
          }
        }

        generatedDoc = {
          id: doc.id || crypto.randomUUID(),
          type: docType,
          name: title,
          html: doc.html || prevDoc?.html || documentTemplateHTML(docType, copy),
          company,
          values: mergedValues,
          items: doc.items,
          terms: doc.terms,
          notes: doc.notes,
          subtotal: doc.subtotal,
          taxAmount: doc.taxAmount,
          total: doc.total,
          summary: doc.items?.length
            ? `${doc.items.length} ${isZh ? "项产品明细" : "items"}`
            : undefined,
          updatedAt: new Date().toISOString(),
        };
      }

      const assistantMsg: AIDocumentMessage = {
        id: `assistant-${Date.now()}`,
        role: "assistant",
        content: res.content || (isZh ? "已为您处理完成。" : "Done."),
        createdAt: new Date().toISOString(),
        toolCalls: res.toolCalls,
        document: generatedDoc,
      };

      const updatedSessions = saveMessageToSession(copy, currentSession.id, assistantMsg);
      setSessions(updatedSessions);

      if (res.toolCalls && res.toolCalls.length > 0) {
        toast.success(
          isZh
            ? `已完成 ${res.toolCalls.length} 个智能工具调用`
            : `Executed ${res.toolCalls.length} tool calls`
        );
      } else if (generatedDoc) {
        toast.success(isZh ? "文档已成功生成并记录" : "Document generated and saved to chat");
      }
    },
    onError: (err) => {
      const errMsg = err instanceof Error ? err.message : copy.aiDocumentFailed;
      const assistantMsg: AIDocumentMessage = {
        id: `assistant-err-${Date.now()}`,
        role: "assistant",
        content: isZh
          ? `抱歉，Agent 执行过程中遇到错误：${errMsg}。请检查 AI 设置或换个更具体的描述重试。`
          : `Agent execution failed: ${errMsg}`,
        createdAt: new Date().toISOString(),
        error: errMsg,
      };
      const updatedSessions = saveMessageToSession(copy, currentSession.id, assistantMsg);
      setSessions(updatedSessions);
      toast.error(errMsg);
    },
  });

  const handleSend = () => {
    const trimmed = input.trim();
    if (!trimmed || mutation.isPending) return;

    const userMsg: AIDocumentMessage = {
      id: `user-${Date.now()}`,
      role: "user",
      content: trimmed,
      createdAt: new Date().toISOString(),
    };

    saveMessageToSession(copy, currentSession.id, userMsg);
    setSessions(readAIDocumentSessions(copy));
    setInput("");

    mutation.mutate(trimmed);
  };

  const handleSaveToDocs = async (doc: AIDocumentGeneratedData) => {
    const storedDoc = convertGeneratedToStoredDocument(doc);
    try {
      await persistServerDocument(storedDoc);
      toast.success(copy.aiDocumentSavedToDocuments);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : copy.loadFailed);
    }
  };

  const handleOpenInEditor = (doc: AIDocumentGeneratedData) => {
    const stored = convertGeneratedToStoredDocument(doc);
    onOpenDocument?.(stored);
  };

  const quickPrompts = [
    {
      label: isZh ? "🔍 检索邮件制作报价单" : "🔍 Search email to quote",
      text: isZh
        ? "请检索我最近关于轴承/减压阀的客户询价邮件，核算价格并生成一份正式报价单"
        : "Search my emails for valve inquiries, calculate pricing and generate a quotation",
    },
    {
      label: copy.aiDocumentQuickQuotation,
      text: isZh
        ? "为【上海宝钢工程】制作一份标准报价单：减压阀 DP27 5台（单价4,500元），疏水阀 TD42 10台（单价380元），按13%税率精确核算，交期15天"
        : "Draft a quotation for Baosteel: 5 valves DP27 ($650 each), 10 steam traps TD42 ($55 each), 13% tax, 15 days lead time",
    },
    {
      label: copy.aiDocumentQuickContract,
      text: isZh
        ? "为【中石化物资部】起草一份标准购销合同：预付30%，到货验收合格后支付65%，留5%质保金，质保期12个月，总金额15.8万元"
        : "Draft a sales contract: 30% advance payment, 65% on delivery, 5% retention, 12 months warranty",
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
            <div className="flex items-center gap-2">
              <h2 className="truncate text-sm font-semibold">{copy.aiDocument}</h2>
              <Badge variant="secondary" className="px-1.5 py-0 text-[10px] font-normal text-muted-foreground">
                Agent
              </Badge>
            </div>
            <p className="truncate text-xs text-muted-foreground">{currentSession?.title || copy.aiDocumentSubtitle}</p>
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
                <Button variant="outline" size="sm" className="h-8 gap-1.5 px-2.5 text-xs">
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
                  className={cn("flex cursor-pointer items-center justify-between gap-2 px-2 py-1.5 text-xs", s.id === activeId && "bg-muted font-medium")}
                  onClick={() => selectSession(s.id)}
                >
                  <span className="min-w-0 flex-1 truncate">{s.title}</span>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{formatTime(s.updatedAt)}</span>
                </DropdownMenuItem>
              ))}
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className="cursor-pointer gap-2 text-xs text-primary font-medium"
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
            onClick={() => handleDeleteSession(currentSession.id)}
            aria-label={copy.aiDocumentClearChat}
            title={copy.aiDocumentClearChat}
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </header>

      {/* Messages Scroll Area */}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
        <div className="mx-auto flex max-w-3xl flex-col gap-4">
          {currentSession?.messages?.map((msg) => (
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
                            {tc.name === "calculate_pricing" && <Calculator className="size-3.5 text-emerald-500" />}
                            {tc.name === "generate_document" && <FileSpreadsheet className="size-3.5 text-purple-500" />}
                            {!["search_emails", "read_email", "calculate_pricing", "generate_document"].includes(tc.name) && (
                              <Wrench className="size-3.5 text-muted-foreground" />
                            )}
                            <span className="font-medium text-foreground">
                              {tc.name === "search_emails"
                                ? (isZh ? "检索邮箱邮件" : "Search Emails")
                                : tc.name === "read_email"
                                ? (isZh ? "读取邮件正文" : "Read Email")
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

                {/* Assistant or User Content Bubble */}
                <div
                  className={cn(
                    "min-w-0 rounded-2xl px-4 py-2.5 text-sm leading-relaxed shadow-2xs break-words",
                    msg.role === "user"
                      ? "rounded-tr-xs bg-primary text-primary-foreground whitespace-pre-wrap"
                      : "rounded-tl-xs border bg-card text-foreground"
                  )}
                >
                  {msg.role === "assistant" ? (
                    <MarkdownContent
                      content={msg.content}
                      copyLabels={{
                        copy: copy.copyAIContent,
                        copied: copy.aiChatCopySuccess,
                      }}
                    />
                  ) : (
                    <p>{msg.content}</p>
                  )}
                </div>

                {/* Interactive Document Preview Card */}
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
                        onClick={() => handleOpenInEditor(msg.document!)}
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
            <div className="flex gap-3">
              <div className="flex size-7 shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-indigo-500 to-purple-600 text-white shadow-xs">
                <Sparkles className="size-3.5 animate-spin" />
              </div>
              <div className="flex max-w-[85%] flex-col">
                <div className="flex items-center gap-2 rounded-2xl rounded-tl-xs border bg-card px-4 py-3 text-sm text-muted-foreground shadow-2xs">
                  <Loader2 className="size-4 animate-spin text-primary" />
                  <span>{isZh ? "AI Agent 正在分析需求、调取工具并处理文档..." : "AI Agent is reasoning and drafting..."}</span>
                </div>
              </div>
            </div>
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
          <AIChatInput
            textareaRef={textareaRef}
            value={input}
            onChange={setInput}
            onSend={handleSend}
            placeholder={copy.aiDocumentPromptPlaceholder}
            disabled={mutation.isPending}
            isPending={mutation.isPending}
            sendLabel={copy.send}
            hint={isZh ? "按 Enter 发送，Shift+Enter 换行" : "Enter to send, Shift+Enter for new line"}
          />
        </div>
      </footer>
    </section>
  );
}
