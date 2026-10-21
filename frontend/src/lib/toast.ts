import { toast as sonnerToast, type ExternalToast } from "sonner";

/**
 * Every application toast is anchored to the top center of the viewport. The
 * position lives here (and in AppToaster) instead of at each call site, so
 * notifications cannot drift apart as new screens are added.
 */
const POSITION = "top-center" as const;

function withDefaults(options?: ExternalToast): ExternalToast {
  return { position: POSITION, ...options };
}

type SonnerToast = typeof sonnerToast;

/**
 * Application toast API. It mirrors sonner's API, but every call defaults to
 * the shared top-center position. Import this instead of `sonner` directly.
 */
export const toast = Object.assign(
  (message: Parameters<SonnerToast>[0], options?: ExternalToast) => sonnerToast(message, withDefaults(options)),
  {
    success: (message: Parameters<SonnerToast["success"]>[0], options?: ExternalToast) =>
      sonnerToast.success(message, withDefaults(options)),
    info: (message: Parameters<SonnerToast["info"]>[0], options?: ExternalToast) =>
      sonnerToast.info(message, withDefaults(options)),
    warning: (message: Parameters<SonnerToast["warning"]>[0], options?: ExternalToast) =>
      sonnerToast.warning(message, withDefaults(options)),
    error: (message: Parameters<SonnerToast["error"]>[0], options?: ExternalToast) =>
      sonnerToast.error(message, withDefaults(options)),
    message: (message: Parameters<SonnerToast["message"]>[0], options?: ExternalToast) =>
      sonnerToast.message(message, withDefaults(options)),
    loading: (message: Parameters<SonnerToast["loading"]>[0], options?: ExternalToast) =>
      sonnerToast.loading(message, withDefaults(options)),
    custom: (jsx: Parameters<SonnerToast["custom"]>[0], options?: ExternalToast) =>
      sonnerToast.custom(jsx, withDefaults(options)),
    promise: ((promise: Parameters<SonnerToast["promise"]>[0], options?: Parameters<SonnerToast["promise"]>[1]) =>
      sonnerToast.promise(promise, { position: POSITION, ...options })) as SonnerToast["promise"],
    dismiss: sonnerToast.dismiss,
    getHistory: sonnerToast.getHistory,
    getToasts: sonnerToast.getToasts,
  }
) as SonnerToast;

/**
 * Turns an unknown thrown value into the message users should see, falling
 * back to a localized default when the server sent nothing useful.
 */
export function toastError(value: unknown, fallback: string, options?: ExternalToast): void {
  const message = value instanceof Error && value.message ? value.message : fallback;
  toast.error(message, options);
}
