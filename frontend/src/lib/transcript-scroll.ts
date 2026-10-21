// Email iframes measure asynchronously. Their height and the transcript's scroll
// correction must be applied together; waiting for an outer ResizeObserver can
// expose the new height for a frame before the viewport catches up.
const restorers = new WeakMap<HTMLElement, () => void>();

export type TranscriptScrollAnchor = {
  key: string;
  top: number;
  text?: { frame: HTMLIFrameElement; range: Range; offset: number };
};

export function captureTranscriptAnchor(viewport: HTMLElement, message: HTMLElement): TranscriptScrollAnchor {
  const viewportTop = viewport.getBoundingClientRect().top;
  const anchor: TranscriptScrollAnchor = { key: message.dataset.messageKey!, top: message.getBoundingClientRect().top - viewportTop };
  const frame = message.querySelector("iframe");
  const doc = frame?.contentDocument;
  if (!frame || !doc) return anchor;
  const rect = frame.getBoundingClientRect();
  const y = viewportTop + 24;
  if (y < rect.top || y >= rect.bottom) return anchor;
  // A long email can itself span the viewport. Anchor the text being read
  // inside it, so an image above that text cannot push it out of view.
  const x = Math.min(24, rect.width / 2);
  let range = doc.caretRangeFromPoint?.(x, y - rect.top);
  if (!range) {
    const caret = doc.caretPositionFromPoint?.(x, y - rect.top);
    if (caret) {
      range = doc.createRange();
      range.setStart(caret.offsetNode, caret.offset);
      range.collapse(true);
    }
  }
  if (range?.startContainer.nodeType === Node.TEXT_NODE && range.getBoundingClientRect().height > 0) {
    const top = rect.top + range.getBoundingClientRect().top - viewportTop;
    anchor.text = { frame, range, offset: top - anchor.top };
    anchor.top = top;
  }
  return anchor;
}

export function transcriptAnchorTop(viewport: HTMLElement, message: HTMLElement, anchor: TranscriptScrollAnchor) {
  if (anchor.text?.range.startContainer.isConnected && anchor.text.frame.isConnected) {
    return anchor.text.frame.getBoundingClientRect().top + anchor.text.range.getBoundingClientRect().top - viewport.getBoundingClientRect().top;
  }
  return message.getBoundingClientRect().top - viewport.getBoundingClientRect().top + (anchor.text?.offset || 0);
}

export function registerTranscriptScroll(viewport: HTMLElement, restore: () => void) {
  restorers.set(viewport, restore);
  return () => { restorers.delete(viewport); };
}

export function preserveTranscriptScroll(viewport: HTMLElement | null, change: () => void) {
  change();
  if (viewport) restorers.get(viewport)?.();
}
