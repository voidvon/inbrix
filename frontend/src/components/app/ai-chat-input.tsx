import React, { useEffect, useLayoutEffect, useRef, useState } from "react";
import { ArrowUp, ImagePlus, Loader2, X } from "lucide-react";
import { Button } from "../ui/button";
import type { useAIChatAttachments } from "../../lib/use-ai-chat-attachments";
import { cn } from "../../lib/utils";

export interface AIChatInputProps {
  imageAttachments?: ReturnType<typeof useAIChatAttachments>;
  isZh?: boolean;
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
  imageAttachments,
  isZh = true,
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
  const fileRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
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
    // Reserve space for send/upload buttons, gaps and padding.
    const availableWidth = Math.max(60, containerWidth - (imageAttachments ? 112 : 72));

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

  const canSend = !disabled && !isPending && !imageAttachments?.blocked && (value.trim().length > 0 || Boolean(imageAttachments?.attachments.length));

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
      onDragOver={event => {
        if (!imageAttachments || disabled || isPending || !event.dataTransfer.types.includes("Files")) return;
        event.preventDefault(); setDragging(true);
      }}
      onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragging(false); }}
      onDrop={event => {
        if (!imageAttachments) return;
        event.preventDefault(); setDragging(false);
        if (!disabled && !isPending) imageAttachments.addFiles(Array.from(event.dataTransfer.files));
      }}
      onPaste={event => {
        if (!imageAttachments || disabled || isPending) return;
        const files = Array.from(event.clipboardData.files);
        if (files.length) { event.preventDefault(); imageAttachments.addFiles(files); }
      }}
      className={cn(
        "relative rounded-2xl border border-input/70 bg-background shadow-xs transition-all focus-within:border-primary/40 focus-within:ring-2 focus-within:ring-primary/15",
        "flex flex-col gap-1.5 px-3 py-2",
        dragging && "border-primary ring-2 ring-primary/30",
        className
      )}
    >
      {imageAttachments && <>
        <input ref={fileRef} type="file" accept="image/png,image/jpeg,image/webp" multiple className="hidden" aria-label={isZh ? "选择聊天图片" : "Choose chat images"} disabled={disabled || isPending} onChange={event => { imageAttachments.addFiles(Array.from(event.target.files || [])); event.target.value = ""; }} />
        {imageAttachments.items.length > 0 && <div className="flex max-h-44 flex-wrap gap-2 overflow-y-auto py-1" data-testid="ai-image-uploads">
          {imageAttachments.items.map(item => <div key={item.key} className="relative w-20 rounded-lg border bg-muted p-1">
            <img src={item.preview} alt={item.filename} className="h-16 w-full rounded object-cover" />
            {!item.attachment && !item.error && <span className="absolute inset-0 flex items-center justify-center rounded bg-background/60"><Loader2 className="size-5 animate-spin" /></span>}
            <Button type="button" size="icon" variant="secondary" className="absolute -right-1 -top-1 size-5 rounded-full" disabled={disabled || isPending} aria-label={`${isZh ? "移除图片" : "Remove image"}: ${item.filename}`} onClick={() => imageAttachments.remove(item.key)}><X className="size-3" /></Button>
            <p className="truncate pt-1 text-[10px]" title={item.filename}>{item.filename}</p>
            {item.error && <p role="alert" className="max-h-16 overflow-auto break-words text-[10px] text-destructive">{item.error}</p>}
          </div>)}
        </div>}
        {imageAttachments.error && <p role="alert" className="text-xs text-destructive">{imageAttachments.error}</p>}
      </>}
      <div className={cn("flex gap-2", isMultiLine ? "flex-col" : "items-center")}>
      {imageAttachments && !isMultiLine && <Button type="button" size="icon" variant="ghost" className="size-7 shrink-0" disabled={disabled || isPending || imageAttachments.items.length >= 6} onClick={() => fileRef.current?.click()} aria-label={isZh ? "添加图片" : "Add images"} title={isZh ? "添加图片，也可粘贴或拖入（最多 6 张，每张 10 MB）" : "Add, paste or drop images (6 max, 10 MB each)"}><ImagePlus className="size-4" /></Button>}
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
        <div className="flex items-center justify-between gap-2 pt-0.5 shrink-0 border-t border-border/30">
          {imageAttachments && <Button type="button" size="icon" variant="ghost" className="size-7 shrink-0" disabled={disabled || isPending || imageAttachments.items.length >= 6} onClick={() => fileRef.current?.click()} aria-label={isZh ? "添加图片" : "Add images"}><ImagePlus className="size-4" /></Button>}
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
    </div>
  );
}
