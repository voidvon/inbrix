import * as React from "react";
import { ScrollArea as ScrollAreaPrimitive } from "@base-ui/react/scroll-area";
import { cn } from "../../lib/utils";

type ScrollAreaProps = ScrollAreaPrimitive.Root.Props & {
  viewportClassName?: string;
  contentClassName?: string;
  viewportRef?: React.Ref<HTMLDivElement>;
};

function ScrollArea({ className, viewportClassName, contentClassName, viewportRef, children, ...props }: ScrollAreaProps) {
  return (
    <ScrollAreaPrimitive.Root data-slot="scroll-area" className={cn("relative overflow-hidden", className)} {...props}>
      <ScrollAreaPrimitive.Viewport
        ref={viewportRef}
        data-slot="scroll-area-viewport"
        className={cn("size-full overscroll-contain outline-none", viewportClassName)}
      >
        <ScrollAreaPrimitive.Content data-slot="scroll-area-content" className={cn("min-h-full min-w-0", contentClassName)}>
          {children}
        </ScrollAreaPrimitive.Content>
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar />
    </ScrollAreaPrimitive.Root>
  );
}

function ScrollBar({ className, orientation = "vertical", ...props }: ScrollAreaPrimitive.Scrollbar.Props) {
  return (
    <ScrollAreaPrimitive.Scrollbar
      data-slot="scroll-area-scrollbar"
      orientation={orientation}
      className={cn(
        "pointer-events-none z-20 flex touch-none select-none p-0.5 opacity-0 transition-opacity duration-200 data-hovering:pointer-events-auto data-hovering:opacity-100 data-scrolling:pointer-events-auto data-scrolling:opacity-100 data-scrolling:duration-0",
        orientation === "vertical" ? "w-2.5" : "h-2.5 flex-col",
        className,
      )}
      {...props}
    >
      <ScrollAreaPrimitive.Thumb className="relative flex-1 rounded-full bg-foreground/25 transition-colors hover:bg-foreground/40" />
    </ScrollAreaPrimitive.Scrollbar>
  );
}

export { ScrollArea, ScrollBar };
