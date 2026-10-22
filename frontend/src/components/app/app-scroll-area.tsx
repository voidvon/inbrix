import type { Ref } from "react";
import { ScrollArea as ScrollAreaPrimitive } from "@base-ui/react/scroll-area";
import { cn } from "../../lib/utils";
import { ScrollBar } from "../ui/scroll-area";

type AppScrollAreaProps = ScrollAreaPrimitive.Root.Props & {
  viewportClassName?: string;
  contentClassName?: string;
  viewportRef?: Ref<HTMLDivElement>;
};

// Chat needs a viewport ref for scroll restoration. Compose Base UI primitives
// here because the official shadcn wrapper does not expose its viewport props.
export function AppScrollArea({ className, viewportClassName, contentClassName, viewportRef, children, ...props }: AppScrollAreaProps) {
  return (
    <ScrollAreaPrimitive.Root data-slot="scroll-area" className={cn("relative overflow-hidden", className)} {...props}>
      <ScrollAreaPrimitive.Viewport
        ref={viewportRef}
        data-slot="scroll-area-viewport"
        className={cn("size-full overscroll-contain rounded-[inherit] outline-none focus-visible:ring-3 focus-visible:ring-ring/50", viewportClassName)}
        style={{ overflowX: "hidden" }}
      >
        <ScrollAreaPrimitive.Content
          data-slot="scroll-area-content"
          className={cn("min-h-full min-w-0 max-w-full", contentClassName)}
          style={{ width: "100%", minWidth: "100%", maxWidth: "100%" }}
        >
          {children}
        </ScrollAreaPrimitive.Content>
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar className="pointer-events-none z-20 opacity-0 transition-opacity duration-200 data-hovering:pointer-events-auto data-hovering:opacity-100 data-scrolling:pointer-events-auto data-scrolling:opacity-100 data-scrolling:duration-0" />
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  );
}
