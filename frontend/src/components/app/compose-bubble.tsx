import { ChevronDown, ChevronUp, Trash2, X } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "../ui/button";
import { HeightResizeHandle } from "./resize-handle";
import { cn } from "../../lib/utils";
import type { Copy } from "../../lib/locale";

/** A floating compose window can be put away, leaving only its title bar. */
export type ComposeBubbleSize = "normal" | "minimized";

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
 *
 * The height is the caller's, and the edge it is dragged by depends on where the
 * window sits: a floating one grows upward from its anchored bottom edge, while
 * an inline one grows downward into the transcript.
 */
export function ComposeBubble({
  copy,
  variant,
  title,
  size,
  height,
  minHeight,
  maxHeight,
  onHeightChange,
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
  height: number;
  minHeight: number;
  maxHeight: number;
  onHeightChange: (height: number) => void;
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
      style={minimized ? undefined : { height }}
      className={cn(
        // Deliberately NOT overflow-hidden: the drag edge reaches just past the
        // border, and clipping here would cut its hit area down to a sliver.
        // The inner wrapper does the clipping instead.
        "relative flex flex-col border bg-background",
        inline
          ? "mb-5 w-full rounded-xl shadow-xs"
          : cn(
              "fixed z-50 shadow-2xl",
              "inset-x-0 bottom-0 rounded-t-xl border-b-0",
              "sm:inset-x-auto sm:right-4 sm:bottom-4 sm:rounded-xl sm:border-b",
              // A height restored from storage can outlive the viewport it was
              // chosen in; the CSS bound keeps the window on screen regardless.
              !minimized && "max-h-[calc(100dvh-2rem)]",
              minimized && "sm:h-auto",
              "sm:w-[min(42rem,calc(100vw-2rem))]",
            ),
      )}
    >
      {!minimized && (
        <HeightResizeHandle
          label={copy.composeResize}
          onResize={onHeightChange}
          height={height}
          minHeight={minHeight}
          maxHeight={maxHeight}
          side={inline ? "bottom" : "top"}
        />
      )}
      {/* Clips the rounded corners. Only the body collapses, so the title bar
          stays visible when the window is put away. */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-[inherit]">
        <header className="flex shrink-0 items-center gap-1 border-b bg-muted/60 py-1 pr-1 pl-3">
          {inline ? (
            <span className="flex min-w-0 flex-1 items-center gap-2 py-0.5">
              <span className="truncate text-sm font-medium">{title}</span>
            </span>
          ) : (
            <button
              type="button"
              className="flex min-w-0 flex-1 items-center gap-2 rounded-sm py-0.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
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
      </div>
    </section>
  );
}
