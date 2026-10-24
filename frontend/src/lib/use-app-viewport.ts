import { useLayoutEffect } from "react";

// Mobile keyboards can shrink and pan the visual viewport without changing
// 100dvh. Keep the app shell and its mobile overlays inside the visible area.
export function useAppViewport(active: boolean) {
  useLayoutEffect(() => {
    if (!active) return;
    const root = document.documentElement;
    const viewport = window.visualViewport;
    const sync = () => {
      // Preserve native panning while the user is pinch-zooming.
      if (viewport && viewport.scale !== 1) return;
      root.style.setProperty("--app-viewport-height", `${viewport?.height ?? window.innerHeight}px`);
      root.style.setProperty("--app-viewport-top", `${viewport?.offsetTop ?? 0}px`);
    };
    root.classList.add("app-viewport-active");
    sync();
    viewport?.addEventListener("resize", sync);
    viewport?.addEventListener("scroll", sync);
    window.addEventListener("resize", sync);
    return () => {
      viewport?.removeEventListener("resize", sync);
      viewport?.removeEventListener("scroll", sync);
      window.removeEventListener("resize", sync);
      root.classList.remove("app-viewport-active");
      root.style.removeProperty("--app-viewport-height");
      root.style.removeProperty("--app-viewport-top");
    };
  }, [active]);
}
