import React, { useEffect, useLayoutEffect, useRef, useState } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { Button } from "../ui/button";
import { cn } from "../../lib/utils";

export interface AIChatInputProps {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  placeholder?: string;
  disabled?: boolean;
  isPending?: boolean;
  hint?: React.ReactNode;
  icon?: React.ReactNode;
  sendLabel?: string;
  className?: string;
  textareaRef?: React.RefObject<HTMLTextAreaElement | null>;
  autoFocus?: boolean;
}

/**
 * Modern AI Chat Input Box modeled after OpenAI (ChatGPT) and Google Gemini:
 * - Default / Single-line state:
 *   Compact single-row layout (height ~40px). Left is a 1-line text input, right is the send button.
 * - Multi-line state:
 *   When text fills 1 row or wraps to multiple lines, layout automatically switches to stacked (上下布局).
 *   Textarea is at the top, auto-growing with line count (2 lines = ~44px, 3 lines = ~66px, up to max 140px).
 *   Send button and keyboard hints are positioned at the bottom row.
 * - Automatically snaps back to single-row layout when text is deleted back to 1 line.
 */
export function AIChatInput({
  value,
  onChange,
  onSend,
  placeholder,
  disabled = false,
  isPending = false,
  hint,
  icon,
  sendLabel = "Send",
  className,
  textareaRef: externalTextareaRef,
  autoFocus = false,
}: AIChatInputProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const internalTextareaRef = useRef<HTMLTextAreaElement>(null);
  const textareaRef = externalTextareaRef || internalTextareaRef;

  const [isMultiLine, setIsMultiLine] = useState(false);
  const [textareaHeight, setTextareaHeight] = useState(22);

  // Check whether text exceeds single-line capacity or contains newlines
  const checkShouldBeMultiLine = (): boolean => {
    if (!value) return false;
    if (value.includes("\n")) return true;

    const container = containerRef.current;
    const textarea = textareaRef.current;
    if (!container || !textarea) return false;

    const containerWidth = container.clientWidth;
    if (containerWidth <= 0) return false;

    // In single-line mode:
    // available width = containerWidth - sendButton(32px) - gap(8px) - paddingX(24px) - safety(8px) = containerWidth - 72px
    const availableWidth = Math.max(60, containerWidth - 72);

    try {
      const style = window.getComputedStyle(textarea);
      const fontSize = style.fontSize || "14px";
      const fontFamily = style.fontFamily || "sans-serif";
      const font = `${fontSize} ${fontFamily}`;

      const canvas =
        (AIChatInput as unknown as { _canvas?: HTMLCanvasElement })._canvas ||
        ((AIChatInput as unknown as { _canvas?: HTMLCanvasElement })._canvas = document.createElement("canvas"));
      const ctx = canvas.getContext("2d");
      if (ctx) {
        ctx.font = font;
        const textWidth = ctx.measureText(value).width;
        return textWidth > availableWidth;
      }
    } catch {
      return value.length * 8 > availableWidth;
    }
    return false;
  };

  // Evaluate single-line vs multi-line whenever value changes
  useEffect(() => {
    const nextMulti = checkShouldBeMultiLine();
    setIsMultiLine(nextMulti);
  }, [value]);

  // Re-evaluate on window/container resize
  useEffect(() => {
    const container = containerRef.current;
    if (!container || typeof ResizeObserver === "undefined") return;

    const ro = new ResizeObserver(() => {
      const nextMulti = checkShouldBeMultiLine();
      setIsMultiLine(nextMulti);
    });

    ro.observe(container);
    return () => ro.disconnect();
  }, [value]);

  // Adjust height dynamically in multi-line mode according to actual text height
  useLayoutEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) return;

    if (!isMultiLine) {
      setTextareaHeight(22);
      textarea.style.height = "22px";
      return;
    }

    // Reset height to auto first so scrollHeight accurately reflects content height
    textarea.style.height = "auto";
    const scrollH = textarea.scrollHeight;
    // 2 lines ≈ 44px, scaling up to max 140px
    const clamped = Math.min(Math.max(scrollH, 44), 140);
    setTextareaHeight(clamped);
    textarea.style.height = `${clamped}px`;
  }, [value, isMultiLine, textareaRef]);

  const canSend = !disabled && !isPending && value.trim().length > 0;

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Send on Enter without shift (and not during IME composition)
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      if (canSend) {
        onSend();
      }
    }
  };

  const sendButton = (
    <Button
      type="button"
      size="sm"
      disabled={!canSend}
      onClick={onSend}
      aria-label={sendLabel}
      title={sendLabel}
      className={cn(
        "size-7.5 sm:size-8 rounded-full p-0 shrink-0 transition-all",
        canSend
          ? "bg-primary text-primary-foreground shadow-2xs hover:bg-primary/90"
          : "bg-muted text-muted-foreground/40 opacity-50 cursor-not-allowed shadow-none"
      )}
    >
      {isPending ? (
        <Loader2 className="size-3.5 sm:size-4 animate-spin" />
      ) : (
        icon || <ArrowUp className="size-3.5 sm:size-4" strokeWidth={2.5} />
      )}
    </Button>
  );

  return (
    <div
      ref={containerRef}
      className={cn(
        "relative rounded-2xl border border-input/70 bg-background shadow-xs transition-all focus-within:border-primary/40 focus-within:ring-2 focus-within:ring-primary/15",
        isMultiLine
          ? "flex flex-col gap-1.5 px-3 pt-2.5 pb-2"
          : "flex flex-row items-center gap-2 px-3 py-1.5 sm:py-2",
        className
      )}
    >
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={handleKeyDown}
        rows={isMultiLine ? 2 : 1}
        placeholder={placeholder}
        disabled={disabled || isPending}
        autoFocus={autoFocus}
        style={
          isMultiLine
            ? { height: `${textareaHeight}px`, maxHeight: "140px" }
            : { height: "22px", overflow: "hidden" }
        }
        className={cn(
          "resize-none border-0 bg-transparent p-0 text-xs sm:text-sm leading-[22px] placeholder:text-muted-foreground/70 shadow-none outline-none focus:outline-none focus-visible:ring-0",
          isMultiLine ? "w-full overflow-y-auto" : "flex-1 min-w-0"
        )}
      />

      {isMultiLine ? (
        <div className="flex items-center justify-between pt-0.5 shrink-0 border-t border-border/30">
          <div className="min-w-0 flex-1 pr-2">
            {hint && (
              <span className="block truncate text-[10px] sm:text-[11px] text-muted-foreground select-none">
                {hint}
              </span>
            )}
          </div>
          {sendButton}
        </div>
      ) : (
        sendButton
      )}
    </div>
  );
}
