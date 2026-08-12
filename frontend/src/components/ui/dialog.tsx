import * as React from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { cn } from "../../lib/utils";

type DialogContextValue = { onOpenChange?: (open: boolean) => void };
const DialogContext = React.createContext<DialogContextValue>({});

function Dialog({ open = false, onOpenChange, children }: { open?: boolean; onOpenChange?: (open: boolean) => void; children: React.ReactNode }) {
  React.useEffect(() => {
    if (!open) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onOpenChange?.(false);
    };
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open, onOpenChange]);

  return <DialogContext.Provider value={{ onOpenChange }}>{open ? children : null}</DialogContext.Provider>;
}

function DialogContent({ className, children, showCloseButton = true, ...props }: React.ComponentProps<"div"> & { showCloseButton?: boolean }) {
  const { onOpenChange } = React.useContext(DialogContext);
  if (typeof document === "undefined") return null;

  return createPortal(
    <>
      <div data-slot="dialog-overlay" className="fixed inset-0 z-50 bg-black/10 supports-backdrop-filter:backdrop-blur-xs" onMouseDown={() => onOpenChange?.(false)} />
      <div
        data-slot="dialog-content"
        role="dialog"
        aria-modal="true"
        className={cn("fixed top-1/2 left-1/2 z-50 grid max-h-[calc(100vh-2rem)] w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 gap-4 overflow-auto rounded-xl bg-popover p-4 text-sm text-popover-foreground shadow-lg ring-1 ring-foreground/10", className)}
        onMouseDown={(event) => event.stopPropagation()}
        {...props}
      >
        {children}
        {showCloseButton && (
          <button type="button" data-slot="dialog-close" className="absolute top-2 right-2 inline-flex size-8 items-center justify-center rounded-lg opacity-70 outline-none transition-opacity hover:opacity-100 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50" onClick={() => onOpenChange?.(false)} aria-label="Close">
            <X className="size-4" />
          </button>
        )}
      </div>
    </>,
    document.body,
  );
}

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="dialog-header" className={cn("flex flex-col gap-2", className)} {...props} />;
}

function DialogFooter({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="dialog-footer" className={cn("flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)} {...props} />;
}

function DialogTitle({ className, ...props }: React.ComponentProps<"h2">) {
  return <h2 data-slot="dialog-title" className={cn("text-base leading-none font-medium", className)} {...props} />;
}

function DialogDescription({ className, ...props }: React.ComponentProps<"p">) {
  return <p data-slot="dialog-description" className={cn("text-sm text-muted-foreground", className)} {...props} />;
}

export { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle };
