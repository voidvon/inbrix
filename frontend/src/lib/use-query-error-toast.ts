import { useEffect, useRef } from "react";
import { toast, toastError } from "./toast";

/**
 * Reports a query's error through the global toast instead of rendering a
 * second copy inside the page. A stable toast id means a refetch that fails
 * again updates the same notification instead of stacking duplicates, and the
 * notification is dismissed once the query recovers.
 *
 * Pass a retry callback and label to keep failed loads actionable after the
 * inline error has been removed.
 */
export function useQueryErrorToast(
  error: unknown,
  fallback: string,
  id: string,
  retry?: () => void,
  retryLabel?: string,
): void {
  // Callers usually pass an inline arrow, so keep it in a ref to avoid
  // re-running the effect (and re-toasting) on every render.
  const retryRef = useRef(retry);
  retryRef.current = retry;

  useEffect(() => {
    if (!error) {
      toast.dismiss(id);
      return;
    }
    toastError(error, fallback, {
      id,
      duration: 6000,
      action: retryRef.current && retryLabel
        ? { label: retryLabel, onClick: () => retryRef.current?.() }
        : undefined,
    });
  }, [error, fallback, id, retryLabel]);
}
