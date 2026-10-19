import { ChevronDown, ChevronUp, Maximize2, Minimize2, Trash2, X } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "../ui/button";
import { cn } from "../../lib/utils";
import type { Copy } from "../../lib/locale";

/** How much room a floating compose window is taking up. */
export type ComposeBubbleSize = "normal" | "expanded" | "minimized";

export type ComposeDraftStatus = "idle" | "saving" | "saved" | "error";

/**
 * Where the compose surface lives.
 *
 * `inline` is a reply: it belongs to the conversation it answers, so it takes the
 * place of the message it will become — the last entry in the transcript, under
 * the newest message. The mailbox stays fully usable around it.
 *
 * `floating` is a brand new message, which has no transcript to belong to, so it
 * is anchored to the bottom-right corner of the window.
 */
export type ComposeVariant = "inline" | "floating";

/**
 * The chrome around a compose window: title, draft state, and the controls. The
 * fields, editor and footer are passed in as children so the compose state
 * machine stays in one place.
 */
export function ComposeBubble({
  copy,
  variant,
  title,
  size,
  draftStatus,
  draftSavedAt,
  canDiscard,
  onSizeChange,
  onClose,
  onDiscard,
  children,
}: {
  copy: Copy;
  variant: ComposeVariant;
  title: string;
  size: ComposeBubbleSize;
  draftStatus: ComposeDraftStatus;
  draftSavedAt?: string;
  canDiscard: boolean;
  onSizeChange: (size: ComposeBubbleSize) => void;
  onClose: () => void;
  onDiscard: () => void;
  children: ReactNode;
}) {
  const inline = variant === "inline";
  // An inline window has nowhere to collapse to — closing it puts the draft back
  // in the transcript — so it only offers close.
  const minimized = !inline && size === "minimized";
  const expanded = !inline && size === "expanded";

  const statusLabel =
    draftStatus === "saving" ? copy.draftSaving
    : draftStatus === "error" ? copy.draftSaveFailed
    : draftStatus === "saved" ? (draftSavedAt ? `${copy.draftSaved} · ${draftSavedAt}` : copy.draftSaved)
    : "";

  return (
    <section
      role="dialog"
      aria-label={title}
      aria-modal={false}
      data-testid={inline ? "compose-inline" : "compose-bubble"}
      data-size={inline ? "inline" : size}
      className={cn(
        "flex flex-col overflow-hidden border bg-background",
        inline
          ? "mb-5 w-full rounded-xl shadow-xs"
          : cn(
              "fixed z-50 shadow-2xl",
              "inset-x-0 bottom-0 rounded-t-xl border-b-0",
              "sm:inset-x-auto sm:right-4 sm:bottom-4 sm:rounded-xl sm:border-b",
              minimized
                ? "sm:h-auto"
                : cn("h-[88dvh]", expanded ? "sm:h-[min(46rem,calc(100dvh-2rem))]" : "sm:h-[min(34rem,calc(100dvh-2rem))]"),
              expanded ? "sm:w-[min(64rem,calc(100vw-2rem))]" : "sm:w-[min(42rem,calc(100vw-2rem))]",
            ),
      )}
    >
      <header className="flex shrink-0 items-center gap-1 border-b bg-muted/60 py-1.5 pr-1.5 pl-3">
        {inline ? (
          <span className="flex min-w-0 flex-1 items-center gap-2 py-1">
            <span className="truncate text-sm font-medium">{title}</span>
          </span>
        ) : (
          <button
            type="button"
            className="flex min-w-0 flex-1 items-center gap-2 rounded-sm py-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            onClick={() => onSizeChange(minimized ? "normal" : "minimized")}
            aria-expanded={!minimized}
            data-testid="compose-bubble-toggle"
            title={minimized ? copy.composeRestore : copy.composeMinimize}
          >
            {minimized ? <ChevronUp className="size-4 shrink-0 text-muted-foreground" /> : <ChevronDown className="size-4 shrink-0 text-muted-foreground" />}
            <span className="truncate text-sm font-medium">{title}</span>
          </button>
        )}
        {statusLabel && (
          <span
            data-testid="compose-draft-status"
            data-status={draftStatus}
            className={cn("shrink-0 text-[11px] tabular-nums", draftStatus === "error" ? "text-destructive" : "text-muted-foreground")}
            title={copy.draftSyncedHint}
          >
            {statusLabel}
          </span>
        )}
        {!inline && (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-7"
            onClick={() => onSizeChange(expanded ? "normal" : "expanded")}
            aria-label={expanded ? copy.composeShrink : copy.composeExpand}
            title={expanded ? copy.composeShrink : copy.composeExpand}
            data-testid="compose-bubble-expand"
          >
            {expanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
          </Button>
        )}
        {canDiscard && (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-7 text-muted-foreground hover:text-destructive"
            onClick={onDiscard}
            aria-label={copy.draftDiscard}
            title={copy.draftDiscard}
            data-testid={inline ? "compose-inline-discard" : "compose-bubble-discard"}
          >
            <Trash2 className="size-4" />
          </Button>
        )}
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7"
          onClick={onClose}
          aria-label={copy.composeClose}
          title={copy.composeClose}
          data-testid={inline ? "compose-inline-close" : "compose-bubble-close"}
        >
          <X className="size-4" />
        </Button>
      </header>
      {/* Kept mounted while minimized so the editor, its content and the
          attachment list survive a collapse. */}
      <div className={cn("flex min-h-0 flex-1 flex-col", minimized && "hidden")}>{children}</div>
    </section>
  );
}
