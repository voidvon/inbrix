import { useCallback, useLayoutEffect, useRef, useState } from "react";

// Plain full-screen pages need the same exit lifetime that Dialog provides:
// retain the page and its data until the actual CSS animation has finished.
export function useMobilePageClose<T extends HTMLElement>(onClosed: () => void) {
  const ref = useRef<T>(null);
  const [closing, setClosing] = useState(false);
  const pending = useRef(false);
  const onClosedRef = useRef(onClosed);
  onClosedRef.current = onClosed;

  const close = useCallback(() => {
    if (pending.current) return;
    if (!ref.current || !window.matchMedia("(width < 64rem) and (prefers-reduced-motion: no-preference)").matches) {
      onClosedRef.current();
      return;
    }
    pending.current = true;
    setClosing(true);
  }, []);

  useLayoutEffect(() => {
    if (!closing) return;
    let active = true;
    const animations = ref.current?.getAnimations() ?? [];
    // Also completes if motion is disabled, the viewport changes, or the
    // animation is cancelled. No timer needs to match the CSS duration.
    void Promise.allSettled(animations.map((animation) => animation.finished)).then(() => {
      if (!active) return;
      pending.current = false;
      setClosing(false);
      onClosedRef.current();
    });
    return () => { active = false; };
  }, [closing]);

  return { ref, closing, close };
}
