import { useCallback, useEffect, useRef, useState } from "react";

export type AIPhase =
  | "idle"
  | "connecting"
  | "thinking"
  | "tool_calling"
  | "generating"
  | "completed"
  | "error";

export type AITaskType =
  | "chat"
  | "translation"
  | "reply"
  | "summary"
  | "document"
  | "generic";

export interface AIStateSnapshot {
  phase: AIPhase;
  statusText: string;
  detailText?: string;
  elapsedMs: number;
  elapsedSeconds: string;
  isBusy: boolean;
  isCompleted: boolean;
  isError: boolean;
  error?: Error | null;
  toolInfo?: { name: string; summary?: string };
  taskType: AITaskType;
}

interface PhaseConfig {
  statusText: string;
  detailText?: string;
}

type LocalePresets = Record<AITaskType, Record<AIPhase, PhaseConfig>>;

const ZH_PRESETS: LocalePresets = {
  chat: {
    idle: { statusText: "待命", detailText: "等待发送提问" },
    connecting: { statusText: "连接智能体中", detailText: "正在建立模型安全会话链路…" },
    thinking: { statusText: "深入思考中", detailText: "正在分析往来邮件与上下文意图…" },
    tool_calling: { statusText: "智能体调用工具中", detailText: "正在检索数据与执行相关任务…" },
    generating: { statusText: "组织回答中", detailText: "正在生成专业答复，即将呈现…" },
    completed: { statusText: "回答已生成", detailText: "完成" },
    error: { statusText: "生成回复失败", detailText: "请检查网络或配置后重试" },
  },
  translation: {
    idle: { statusText: "待翻译", detailText: "等待发起邮件翻译" },
    connecting: { statusText: "连接翻译引擎中", detailText: "正在初始化专业语言模型…" },
    thinking: { statusText: "语义解析中", detailText: "正在分析邮件专业术语与语境…" },
    tool_calling: { statusText: "对照术语库中", detailText: "正在核对行业规范词汇…" },
    generating: { statusText: "生成译文中", detailText: "正在组织符合商务语境的翻译结果…" },
    completed: { statusText: "翻译完成", detailText: "译文已生成" },
    error: { statusText: "翻译失败", detailText: "未能完成邮件翻译，请重试" },
  },
  reply: {
    idle: { statusText: "待生成回复", detailText: "等待生成建议回复" },
    connecting: { statusText: "准备起草助手中", detailText: "正在连接建议回复模型…" },
    thinking: { statusText: "研读邮件要点中", detailText: "正在提取发件人诉求与关键信息…" },
    tool_calling: { statusText: "核对背景信息中", detailText: "正在整理相关上下文…" },
    generating: { statusText: "起草建议回复中", detailText: "正在生成专业得体的回复建议…" },
    completed: { statusText: "建议回复已就绪", detailText: "已生成回复草稿" },
    error: { statusText: "生成回复失败", detailText: "未能生成建议回复，请重试" },
  },
  summary: {
    idle: { statusText: "待提炼摘要", detailText: "等待提取邮件摘要" },
    connecting: { statusText: "连接摘要模型中", detailText: "正在准备语言分析链路…" },
    thinking: { statusText: "提炼核心要点中", detailText: "正在阅读长邮件并梳理行动项…" },
    tool_calling: { statusText: "整理时间线中", detailText: "正在关联历史往来记录…" },
    generating: { statusText: "整理结构化摘要中", detailText: "正在归纳结论与待办事项…" },
    completed: { statusText: "摘要已生成", detailText: "提炼完成" },
    error: { statusText: "提炼摘要失败", detailText: "未能生成邮件摘要，请重试" },
  },
  document: {
    idle: { statusText: "待起草文档", detailText: "等待文档指令" },
    connecting: { statusText: "连接商务文档助手中", detailText: "正在加载专业模板与规则库…" },
    thinking: { statusText: "核对条款与规格中", detailText: "正在计算价格、税率并校对合同要件…" },
    tool_calling: { statusText: "执行核算工具中", detailText: "正在核对产品参数并生成结构化表格…" },
    generating: { statusText: "生成正式文档中", detailText: "正在排版并输出标准格式文档…" },
    completed: { statusText: "文档生成完成", detailText: "可预览或在编辑器中继续调整" },
    error: { statusText: "文档生成失败", detailText: "请核对要求后重试" },
  },
  generic: {
    idle: { statusText: "就绪", detailText: "等待操作" },
    connecting: { statusText: "连接 AI 引擎中", detailText: "正在建立模型通信…" },
    thinking: { statusText: "深度思考中", detailText: "正在理解输入并规划执行方案…" },
    tool_calling: { statusText: "工具执行中", detailText: "正在调用辅助工具处理数据…" },
    generating: { statusText: "生成结果中", detailText: "即将输出内容…" },
    completed: { statusText: "执行完成", detailText: "已完成" },
    error: { statusText: "执行失败", detailText: "遇到错误，请重试" },
  },
};

const EN_PRESETS: LocalePresets = {
  chat: {
    idle: { statusText: "Ready", detailText: "Awaiting user prompt" },
    connecting: { statusText: "Connecting to Agent", detailText: "Establishing secure session…" },
    thinking: { statusText: "Reasoning & Analyzing", detailText: "Examining email thread & context…" },
    tool_calling: { statusText: "Invoking Agent Tools", detailText: "Retrieving data and executing tools…" },
    generating: { statusText: "Synthesizing Response", detailText: "Formulating response…" },
    completed: { statusText: "Response Generated", detailText: "Completed" },
    error: { statusText: "Generation Failed", detailText: "Please check your network and retry" },
  },
  translation: {
    idle: { statusText: "Ready to translate", detailText: "Awaiting translation" },
    connecting: { statusText: "Connecting to Translation Engine", detailText: "Initializing model…" },
    thinking: { statusText: "Analyzing Source Context", detailText: "Examining terminology & tone…" },
    tool_calling: { statusText: "Checking Glossaries", detailText: "Looking up terms…" },
    generating: { statusText: "Translating Content", detailText: "Drafting natural translation…" },
    completed: { statusText: "Translation Completed", detailText: "Done" },
    error: { statusText: "Translation Failed", detailText: "Could not translate message" },
  },
  reply: {
    idle: { statusText: "Ready", detailText: "Awaiting reply request" },
    connecting: { statusText: "Preparing Assistant", detailText: "Connecting to reply model…" },
    thinking: { statusText: "Reading Thread Highlights", detailText: "Extracting key sender asks…" },
    tool_calling: { statusText: "Checking Context", detailText: "Reviewing history…" },
    generating: { statusText: "Drafting Suggested Reply", detailText: "Composing professional draft…" },
    completed: { statusText: "Reply Ready", detailText: "Draft created" },
    error: { statusText: "Drafting Failed", detailText: "Could not generate reply" },
  },
  summary: {
    idle: { statusText: "Ready", detailText: "Awaiting summary request" },
    connecting: { statusText: "Connecting to Summary Model", detailText: "Preparing pipeline…" },
    thinking: { statusText: "Extracting Key Takeaways", detailText: "Reading email and action items…" },
    tool_calling: { statusText: "Aggregating Timeline", detailText: "Correlating history…" },
    generating: { statusText: "Structuring Summary", detailText: "Organizing bullet points…" },
    completed: { statusText: "Summary Generated", detailText: "Done" },
    error: { statusText: "Summary Failed", detailText: "Could not generate summary" },
  },
  document: {
    idle: { statusText: "Ready", detailText: "Awaiting document prompt" },
    connecting: { statusText: "Connecting to Document Assistant", detailText: "Loading business templates…" },
    thinking: { statusText: "Verifying Clauses & Items", detailText: "Checking pricing, taxes & terms…" },
    tool_calling: { statusText: "Running Calculations", detailText: "Structuring table data…" },
    generating: { statusText: "Generating Formal Document", detailText: "Formatting document output…" },
    completed: { statusText: "Document Ready", detailText: "Ready for preview or edit" },
    error: { statusText: "Document Generation Failed", detailText: "Please retry" },
  },
  generic: {
    idle: { statusText: "Ready", detailText: "Awaiting action" },
    connecting: { statusText: "Connecting to AI", detailText: "Establishing connection…" },
    thinking: { statusText: "Deep Thinking", detailText: "Analyzing input and planning…" },
    tool_calling: { statusText: "Running Tools", detailText: "Executing helper tools…" },
    generating: { statusText: "Generating Output", detailText: "Writing results…" },
    completed: { statusText: "Completed", detailText: "Done" },
    error: { statusText: "Execution Failed", detailText: "An error occurred" },
  },
};

export function getAIPhaseConfig(
  taskType: AITaskType,
  phase: AIPhase,
  locale: "zh" | "en" = "zh",
  overrides?: Partial<Record<AIPhase, Partial<PhaseConfig>>>
): PhaseConfig {
  const dictionary = locale === "zh" ? ZH_PRESETS : EN_PRESETS;
  const taskDict = dictionary[taskType] || dictionary.generic;
  const base = taskDict[phase] || dictionary.generic[phase];
  const override = overrides?.[phase];

  return {
    statusText: override?.statusText || base.statusText,
    detailText: override?.detailText !== undefined ? override.detailText : base.detailText,
  };
}

export interface UseAIStateMachineOptions {
  taskType?: AITaskType;
  locale?: "zh" | "en";
  initialPhase?: AIPhase;
  autoProgress?: boolean;
  delays?: {
    thinkingMs?: number;
    generatingMs?: number;
  };
  customLabels?: Partial<Record<AIPhase, Partial<PhaseConfig>>>;
  onPhaseChange?: (phase: AIPhase, snapshot: AIStateSnapshot) => void;
}

export function useAIStateMachine(options: UseAIStateMachineOptions = {}) {
  const {
    taskType = "generic",
    locale = "zh",
    initialPhase = "idle",
    autoProgress = true,
    delays = { thinkingMs: 1100, generatingMs: 3800 },
    customLabels,
    onPhaseChange,
  } = options;

  const [phase, setPhase] = useState<AIPhase>(initialPhase);
  const [detailOverride, setDetailOverride] = useState<string | undefined>(undefined);
  const [statusOverride, setStatusOverride] = useState<string | undefined>(undefined);
  const [toolInfo, setToolInfo] = useState<{ name: string; summary?: string } | undefined>(undefined);
  const [error, setError] = useState<Error | null>(null);
  const [elapsedMs, setElapsedMs] = useState(0);

  const startTimeRef = useRef<number | null>(null);
  const timerIntervalRef = useRef<number | null>(null);
  const autoProgressTimersRef = useRef<number[]>([]);
  const isAutoPacedRef = useRef(false);

  const isBusy = ["connecting", "thinking", "tool_calling", "generating"].includes(phase);
  const isCompleted = phase === "completed";
  const isError = phase === "error";

  // Stop all timers
  const clearAllTimers = useCallback(() => {
    if (timerIntervalRef.current) {
      window.clearInterval(timerIntervalRef.current);
      timerIntervalRef.current = null;
    }
    autoProgressTimersRef.current.forEach((t) => window.clearTimeout(t));
    autoProgressTimersRef.current = [];
  }, []);

  // Format seconds elapsed
  const formatSeconds = (ms: number): string => {
    const s = (ms / 1000).toFixed(1);
    return `${s}s`;
  };

  // Build snapshot
  const baseConfig = getAIPhaseConfig(taskType, phase, locale, customLabels);
  const snapshot: AIStateSnapshot = {
    phase,
    statusText: statusOverride || baseConfig.statusText,
    detailText: detailOverride !== undefined ? detailOverride : baseConfig.detailText,
    elapsedMs,
    elapsedSeconds: formatSeconds(elapsedMs),
    isBusy,
    isCompleted,
    isError,
    error,
    toolInfo,
    taskType,
  };

  // State Transition function
  const transition = useCallback(
    (
      nextPhase: AIPhase,
      meta?: {
        statusText?: string;
        detailText?: string;
        tool?: { name: string; summary?: string };
        error?: Error | null;
      }
    ) => {
      setPhase(nextPhase);

      if (meta?.statusText !== undefined) setStatusOverride(meta.statusText);
      else setStatusOverride(undefined);

      if (meta?.detailText !== undefined) setDetailOverride(meta.detailText);
      else setDetailOverride(undefined);

      if (meta?.tool) setToolInfo(meta.tool);
      if (meta?.error !== undefined) setError(meta.error);

      // Manage start/stop of elapsed timer
      const nextIsBusy = ["connecting", "thinking", "tool_calling", "generating"].includes(nextPhase);

      if (nextIsBusy) {
        if (!startTimeRef.current) {
          startTimeRef.current = Date.now();
          setElapsedMs(0);
          if (timerIntervalRef.current) window.clearInterval(timerIntervalRef.current);
          timerIntervalRef.current = window.setInterval(() => {
            if (startTimeRef.current) {
              setElapsedMs(Date.now() - startTimeRef.current);
            }
          }, 100);
        }
      } else {
        // Freeze final elapsed time
        if (startTimeRef.current) {
          setElapsedMs(Date.now() - startTimeRef.current);
          startTimeRef.current = null;
        }
        if (timerIntervalRef.current) {
          window.clearInterval(timerIntervalRef.current);
          timerIntervalRef.current = null;
        }
        autoProgressTimersRef.current.forEach((t) => window.clearTimeout(t));
        autoProgressTimersRef.current = [];
      }
    },
    []
  );

  useEffect(() => {
    onPhaseChange?.(phase, snapshot);
  }, [phase, onPhaseChange]);

  useEffect(() => {
    return () => {
      clearAllTimers();
    };
  }, [clearAllTimers]);

  // Shortcut for tool calls
  const setToolCall = useCallback(
    (name: string, summary?: string) => {
      isAutoPacedRef.current = false;
      autoProgressTimersRef.current.forEach((t) => window.clearTimeout(t));
      transition("tool_calling", {
        tool: { name, summary },
        detailText: summary || `正在执行: ${name}`,
      });
    },
    [transition]
  );

  // Reset to idle
  const reset = useCallback(() => {
    clearAllTimers();
    startTimeRef.current = null;
    setElapsedMs(0);
    setError(null);
    setToolInfo(undefined);
    setStatusOverride(undefined);
    setDetailOverride(undefined);
    setPhase("idle");
  }, [clearAllTimers]);

  // Universal runner: wrap any AI Promise in the state machine
  const run = useCallback(
    async <T,>(
      promiseFn: () => Promise<T>,
      runOptions?: {
        initialDetail?: string;
        successDetail?: string;
      }
    ): Promise<T> => {
      reset();
      isAutoPacedRef.current = autoProgress;

      transition("connecting", { detailText: runOptions?.initialDetail });

      if (autoProgress) {
        // Schedule natural transitions if the promise takes longer
        const thinkingDelay = delays.thinkingMs ?? 1100;
        const generatingDelay = delays.generatingMs ?? 3800;

        const t1 = window.setTimeout(() => {
          if (isAutoPacedRef.current) {
            transition("thinking");
          }
        }, thinkingDelay);

        const t2 = window.setTimeout(() => {
          if (isAutoPacedRef.current) {
            transition("generating");
          }
        }, generatingDelay);

        autoProgressTimersRef.current = [t1, t2];
      }

      try {
        const result = await promiseFn();
        isAutoPacedRef.current = false;
        clearAllTimers();
        transition("completed", { detailText: runOptions?.successDetail });
        return result;
      } catch (err) {
        isAutoPacedRef.current = false;
        clearAllTimers();
        const errorObj = err instanceof Error ? err : new Error(String(err));
        transition("error", {
          error: errorObj,
          detailText: errorObj.message || "请求发生异常",
        });
        throw err;
      }
    },
    [autoProgress, delays, reset, transition, clearAllTimers]
  );

  return {
    snapshot,
    phase,
    isBusy,
    isCompleted,
    isError,
    statusText: snapshot.statusText,
    detailText: snapshot.detailText,
    elapsedMs,
    elapsedSeconds: snapshot.elapsedSeconds,
    error,
    toolInfo,
    transition,
    setToolCall,
    reset,
    run,
  };
}
