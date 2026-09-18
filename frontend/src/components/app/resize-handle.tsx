import { useEffect, useState, type CSSProperties, type PointerEvent } from "react";

type ResizeHandleProps = {
  label: string;
  onResize: (width: number) => void;
  width: number;
  minWidth: number;
  maxWidth: number;
};

export function usePersistedPaneWidth(storageKey: string, initialWidth: number, minWidth: number, maxWidth: number) {
  const [width, setWidth] = useState(() => {
    const saved = Number(window.localStorage.getItem(storageKey));
    const value = Number.isFinite(saved) ? saved : initialWidth;
    return Math.min(maxWidth, Math.max(minWidth, value));
  });

  useEffect(() => {
    window.localStorage.setItem(storageKey, String(width));
  }, [storageKey, width]);

  return [width, setWidth] as const;
}

export function ResizeHandle({ label, onResize, width, minWidth, maxWidth }: ResizeHandleProps) {
  const resize = (nextWidth: number) => onResize(Math.min(maxWidth, Math.max(minWidth, Math.round(nextWidth))));

  const startResize = (event: PointerEvent<HTMLButtonElement>) => {
    if (event.pointerType === "mouse" && event.button !== 0) return;
    const startX = event.clientX;
    const startWidth = width;
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);

    const move = (moveEvent: globalThis.PointerEvent) => resize(startWidth + moveEvent.clientX - startX);
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
        if (event.key === "ArrowLeft") { event.preventDefault(); resize(width - 16); }
        if (event.key === "ArrowRight") { event.preventDefault(); resize(width + 16); }
        if (event.key === "Home") { event.preventDefault(); resize(minWidth); }
        if (event.key === "End") { event.preventDefault(); resize(maxWidth); }
      }}
    >
      <span className="absolute inset-y-0 left-(--resize-handle-offset) w-3">
        <span className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-transparent transition-colors group-hover:bg-primary/50 group-focus-visible:bg-primary group-active:bg-primary" />
      </span>
    </button>
  );
}
