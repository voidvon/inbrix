import React from "react";
import {
  AlertCircle,
  Bot,
  Brain,
  CheckCircle2,
  Clock,
  Loader2,
  PenTool,
  Sparkles,
  Wifi,
  Wrench,
} from "lucide-react";
import { type AIStateSnapshot } from "../../lib/ai-state-machine";
import { cn } from "../../lib/utils";

export interface AIStatusIndicatorProps {
  snapshot: AIStateSnapshot;
  variant?: "bubble" | "inline" | "card" | "badge";
  agentName?: string;
  className?: string;
  showTimer?: boolean;
  showSteps?: boolean;
}

export function AIStatusIndicator({
  snapshot,
  variant = "inline",
  agentName,
  className,
  showTimer = true,
  showSteps = false,
}: AIStatusIndicatorProps) {
  const { phase, statusText, detailText, elapsedSeconds, isBusy, isError, toolInfo } = snapshot;

  if (phase === "idle") return null;

  // Icon corresponding to the phase
  const renderPhaseIcon = (sizeClass = "size-3.5") => {
    switch (phase) {
      case "connecting":
        return <Wifi className={cn(sizeClass, "text-blue-500 animate-pulse")} />;
      case "thinking":
        return <Brain className={cn(sizeClass, "text-purple-500 animate-pulse")} />;
      case "tool_calling":
        return <Wrench className={cn(sizeClass, "text-amber-500 animate-spin")} />;
      case "generating":
        return <PenTool className={cn(sizeClass, "text-indigo-500 animate-bounce")} />;
      case "completed":
        return <CheckCircle2 className={cn(sizeClass, "text-emerald-500")} />;
      case "error":
        return <AlertCircle className={cn(sizeClass, "text-destructive")} />;
      default:
        return <Sparkles className={cn(sizeClass, "text-primary")} />;
    }
  };

  // Badge pill variant
  if (variant === "badge") {
    return (
      <span
        className={cn(
          "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium border shadow-2xs transition-colors",
          phase === "connecting" && "bg-blue-500/10 text-blue-700 dark:text-blue-300 border-blue-500/20",
          phase === "thinking" && "bg-purple-500/10 text-purple-700 dark:text-purple-300 border-purple-500/20",
          phase === "tool_calling" && "bg-amber-500/10 text-amber-700 dark:text-amber-300 border-amber-500/20",
          phase === "generating" && "bg-indigo-500/10 text-indigo-700 dark:text-indigo-300 border-indigo-500/20",
          phase === "completed" && "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border-emerald-500/20",
          phase === "error" && "bg-destructive/10 text-destructive border-destructive/20",
          className
        )}
      >
        {renderPhaseIcon("size-3")}
        <span>{statusText}</span>
        {showTimer && isBusy && (
          <span className="opacity-75 font-mono text-[10px]">{elapsedSeconds}</span>
        )}
      </span>
    );
  }

  // Inline minimal variant
  if (variant === "inline") {
    return (
      <div className={cn("inline-flex items-center gap-2 text-xs", className)}>
        <div className="relative flex items-center justify-center">
          {isBusy && (
            <span className="absolute -inset-0.5 rounded-full bg-primary/20 animate-ping opacity-75" />
          )}
          {renderPhaseIcon("size-3.5")}
        </div>
        <div className="flex items-center gap-1.5">
          <span className="font-medium text-foreground">{statusText}</span>
          {showTimer && isBusy && (
            <span className="flex items-center gap-0.5 text-[11px] text-muted-foreground font-mono">
              <span>·</span>
              <span>{elapsedSeconds}</span>
            </span>
          )}
        </div>
        {detailText && isBusy && (
          <span className="hidden sm:inline text-[11px] text-muted-foreground truncate max-w-[200px]">
            ({detailText})
          </span>
        )}
      </div>
    );
  }

  // Card variant (for inside panels/summaries/translations)
  if (variant === "card") {
    return (
      <div
        className={cn(
          "w-full rounded-xl border bg-card/90 p-3 shadow-xs space-y-2.5 transition-all",
          isError ? "border-destructive/30 bg-destructive/5" : "border-border/80",
          className
        )}
      >
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <div
              className={cn(
                "flex size-7 items-center justify-center rounded-lg shadow-2xs",
                phase === "connecting" && "bg-blue-500/15 text-blue-600 dark:text-blue-400",
                phase === "thinking" && "bg-purple-500/15 text-purple-600 dark:text-purple-400",
                phase === "tool_calling" && "bg-amber-500/15 text-amber-600 dark:text-amber-400",
                phase === "generating" && "bg-indigo-500/15 text-indigo-600 dark:text-indigo-400",
                phase === "completed" && "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
                phase === "error" && "bg-destructive/15 text-destructive"
              )}
            >
              {renderPhaseIcon("size-4")}
            </div>
            <div>
              <h4 className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                <span>{statusText}</span>
                {toolInfo && (
                  <span className="rounded bg-muted px-1.5 py-0.2 font-mono text-[10px] text-muted-foreground">
                    {toolInfo.name}
                  </span>
                )}
              </h4>
              <p className="text-[11px] text-muted-foreground leading-normal mt-0.5">
                {detailText}
              </p>
            </div>
          </div>

          {showTimer && (
            <div className="flex items-center gap-1 text-[11px] font-mono text-muted-foreground bg-muted/60 px-2 py-0.5 rounded-md shrink-0">
              <Clock className="size-3 opacity-60" />
              <span>{elapsedSeconds}</span>
            </div>
          )}
        </div>

        {/* State progress step indicators */}
        {showSteps && (
          <div className="pt-1 flex items-center justify-between gap-1 border-t border-border/40 text-[10px]">
            <StepDot
              label="连接"
              active={phase === "connecting"}
              done={["thinking", "tool_calling", "generating", "completed"].includes(phase)}
            />
            <div className="flex-1 h-0.5 bg-border/50 mx-1" />
            <StepDot
              label="思考"
              active={phase === "thinking" || phase === "tool_calling"}
              done={["generating", "completed"].includes(phase)}
            />
            <div className="flex-1 h-0.5 bg-border/50 mx-1" />
            <StepDot
              label="生成"
              active={phase === "generating"}
              done={phase === "completed"}
            />
          </div>
        )}
      </div>
    );
  }

  // Chat Bubble variant (for message streams and AI discussion panels)
  return (
    <div className={cn("flex flex-col items-start gap-1 max-w-[90%] sm:max-w-[85%]", className)}>
      {/* Sender Header */}
      {agentName && (
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground px-1">
          <div className="flex size-4 items-center justify-center rounded-full bg-primary/15 text-primary">
            <Bot className="size-2.5" />
          </div>
          <span>{agentName}</span>
        </div>
      )}

      {/* Bubble Container */}
      <div
        className={cn(
          "rounded-2xl rounded-tl-xs border bg-secondary/80 px-3.5 py-2.5 text-xs shadow-2xs transition-all space-y-1.5",
          isError ? "border-destructive/30 bg-destructive/10 text-destructive" : "border-border/60 text-foreground"
        )}
      >
        <div className="flex items-center gap-2">
          <div className="relative flex items-center justify-center shrink-0">
            {isBusy ? (
              <Loader2 className="size-3.5 animate-spin text-primary" />
            ) : (
              renderPhaseIcon("size-3.5")
            )}
          </div>
          <span className="font-semibold text-foreground text-xs sm:text-sm">
            {statusText}
          </span>
          {showTimer && (
            <span className="ml-auto flex items-center gap-1 rounded bg-background/60 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground shadow-2xs">
              <Clock className="size-2.5 opacity-60" />
              <span>{elapsedSeconds}</span>
            </span>
          )}
        </div>

        {/* Phase details / Subtext */}
        {detailText && (
          <p className="text-[11px] text-muted-foreground pl-5.5 leading-relaxed">
            {detailText}
          </p>
        )}

        {/* Multi-step progress dots */}
        {isBusy && (
          <div className="flex items-center gap-1.5 pl-5.5 pt-0.5">
            <span
              className={cn(
                "size-1.5 rounded-full transition-all",
                phase === "connecting"
                  ? "bg-blue-500 scale-125 animate-pulse"
                  : "bg-muted-foreground/30"
              )}
            />
            <span
              className={cn(
                "size-1.5 rounded-full transition-all",
                phase === "thinking" || phase === "tool_calling"
                  ? "bg-purple-500 scale-125 animate-pulse"
                  : "bg-muted-foreground/30"
              )}
            />
            <span
              className={cn(
                "size-1.5 rounded-full transition-all",
                phase === "generating"
                  ? "bg-indigo-500 scale-125 animate-pulse"
                  : "bg-muted-foreground/30"
              )}
            />
          </div>
        )}
      </div>
    </div>
  );
}

function StepDot({
  label,
  active,
  done,
}: {
  label: string;
  active: boolean;
  done: boolean;
}) {
  return (
    <div className="flex items-center gap-1 shrink-0">
      <span
        className={cn(
          "size-2 rounded-full",
          done && "bg-emerald-500",
          active && "bg-primary animate-pulse ring-2 ring-primary/20",
          !done && !active && "bg-muted-foreground/30"
        )}
      />
      <span
        className={cn(
          "text-[10px]",
          done && "text-emerald-600 dark:text-emerald-400 font-medium",
          active && "text-primary font-semibold",
          !done && !active && "text-muted-foreground"
        )}
      >
        {label}
      </span>
    </div>
  );
}
