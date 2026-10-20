import { useEffect, useState, type CSSProperties, type PointerEvent } from "react";
import { cn } from "../../lib/utils";

type ResizeHandleProps = {
  label: string;
  onResize: (width: number) => void;
  width: number;
  minWidth: number;
  maxWidth: number;
  side?: "left" | "right";
};

export function usePersistedPaneWidth(storageKey: string, initialWidth: number, minWidth: number, maxWidth: number) {
  const [width, setWidth] = useState(() => {
    if (typeof window === "undefined") return initialWidth;
    const raw = window.localStorage.getItem(storageKey);
    if (raw === null) return initialWidth;
    const saved = Number(raw);
    const value = Number.isFinite(saved) ? saved : initialWidth;
    return Math.min(maxWidth, Math.max(minWidth, value));
  });

  useEffect(() => {
    if (typeof window !== "undefined") {
      window.localStorage.setItem(storageKey, String(width));
    }
  }, [storageKey, width]);

  return [width, setWidth] as const;
}

/**
 * The height counterpart of usePersistedPaneWidth, for surfaces the user sizes
 * vertically. A compose window's editor is the only part of it that can
 * usefully give or take room, so the height the user settles on is worth
 * remembering across sessions.
 */
export function usePersistedPaneHeight(storageKey: string, initialHeight: number, minHeight: number, maxHeight: number) {
  const [height, setHeight] = useState(() => {
    if (typeof window === "undefined") return initialHeight;
    const raw = window.localStorage.getItem(storageKey);
    if (raw === null) return initialHeight;
    const saved = Number(raw);
    const value = Number.isFinite(saved) ? saved : initialHeight;
    return Math.min(maxHeight, Math.max(minHeight, value));
  });

  useEffect(() => {
    if (typeof window !== "undefined") {
      window.localStorage.setItem(storageKey, String(height));
    }
  }, [storageKey, height]);

  return [height, setHeight] as const;
}

/** How much of the viewport a dragged height may claim, leaving the app visible. */
export function viewportHeightLimit() {
  if (typeof window === "undefined") return 900;
  return Math.max(320, window.innerHeight - 32);
}

export function HeightResizeHandle({ label, onResize, height, minHeight, maxHeight, side = "top" }: {
  label: string;
  onResize: (height: number) => void;
  height: number;
  minHeight: number;
  maxHeight: number;
  /** Which edge is dragged. Dragging it away from the surface makes it taller. */
  side?: "top" | "bottom";
}) {
  const resize = (nextHeight: number) => onResize(Math.min(maxHeight, Math.max(minHeight, Math.round(nextHeight))));

  const startResize = (event: PointerEvent<HTMLButtonElement>) => {
    if (event.pointerType === "mouse" && event.button !== 0) return;
    const startY = event.clientY;
    const startHeight = height;
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);

    const move = (moveEvent: globalThis.PointerEvent) => {
      const delta = side === "top" ? startY - moveEvent.clientY : moveEvent.clientY - startY;
      resize(startHeight + delta);
    };
    const stop = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
  };

  return (
    <button
      type="button"
      data-testid="compose-height-handle"
      className={cn(
        "group absolute inset-x-0 z-20 h-0 cursor-row-resize touch-none outline-none",
        side === "top" ? "top-0" : "bottom-0",
      )}
      aria-label={label}
      aria-orientation="horizontal"
      aria-valuemin={minHeight}
      aria-valuemax={maxHeight}
      aria-valuenow={height}
      role="separator"
      onPointerDown={startResize}
      onKeyDown={(event) => {
        if (event.key === "ArrowUp") {
          event.preventDefault();
          resize(side === "top" ? height + 24 : height - 24);
        }
        if (event.key === "ArrowDown") {
          event.preventDefault();
          resize(side === "top" ? height - 24 : height + 24);
        }
        if (event.key === "Home") {
          event.preventDefault();
          resize(minHeight);
        }
        if (event.key === "End") {
          event.preventDefault();
          resize(maxHeight);
        }
      }}
    >
      <span className={cn("absolute inset-x-0 h-2", side === "top" ? "-top-1" : "-bottom-1")}>
        <span
          className={cn(
            "absolute inset-x-0 h-px bg-transparent transition-colors group-hover:bg-primary/50 group-focus-visible:bg-primary group-active:bg-primary",
            side === "top" ? "top-1/2" : "bottom-1/2",
          )}
        />
      </span>
    </button>
  );
}

export function ResizeHandle({ label, onResize, width, minWidth, maxWidth, side = "left" }: ResizeHandleProps) {
  const resize = (nextWidth: number) => onResize(Math.min(maxWidth, Math.max(minWidth, Math.round(nextWidth))));

  const startResize = (event: PointerEvent<HTMLButtonElement>) => {
    if (event.pointerType === "mouse" && event.button !== 0) return;
    const startX = event.clientX;
    const startWidth = width;
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);

    const move = (moveEvent: globalThis.PointerEvent) => {
      const delta = side === "right" ? startX - moveEvent.clientX : moveEvent.clientX - startX;
      resize(startWidth + delta);
    };
    const stop = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
  };

  return (
    <button
      type="button"
      className="group relative z-20 hidden w-0 shrink-0 cursor-col-resize touch-none outline-none lg:block"
      style={{ "--resize-handle-offset": "-0.375rem" } as CSSProperties}
      aria-label={label}
      aria-orientation="vertical"
      aria-valuemin={minWidth}
      aria-valuemax={maxWidth}
      aria-valuenow={width}
      role="separator"
      onPointerDown={startResize}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft") {
          event.preventDefault();
          resize(side === "right" ? width + 16 : width - 16);
        }
        if (event.key === "ArrowRight") {
          event.preventDefault();
          resize(side === "right" ? width - 16 : width + 16);
        }
        if (event.key === "Home") {
          event.preventDefault();
          resize(minWidth);
        }
        if (event.key === "End") {
          event.preventDefault();
          resize(maxWidth);
        }
      }}
    >
      <span className="absolute inset-y-0 left-(--resize-handle-offset) w-3">
        <span className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-transparent transition-colors group-hover:bg-primary/50 group-focus-visible:bg-primary group-active:bg-primary" />
      </span>
    </button>
  );
}
