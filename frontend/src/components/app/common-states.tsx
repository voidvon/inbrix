import type { ReactNode } from "react";
import { TriangleAlert } from "lucide-react";
import { Button } from "../ui/button";
import { Skeleton } from "../ui/skeleton";
import type { Copy } from "../../lib/locale";

export function ListSkeleton() {
  return (
    <div className="grid">
      {Array.from({ length: 7 }, (_, index) => (
        <div className="grid gap-2 border-b px-4 py-3" key={index}>
          <Skeleton className="h-2.5 w-2/5" />
          <Skeleton className="h-2.5 w-4/5" />
        </div>
      ))}
    </div>
  );
}

export function ErrorState({ copy, onRetry }: { copy: Copy; onRetry?: () => void }) {
  return (
    <div className="flex min-h-56 flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">
      <TriangleAlert className="size-8 text-primary" />
      <strong className="text-sm font-medium">{copy.loadFailed}</strong>
      {onRetry && (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          {copy.retry}
        </Button>
      )}
    </div>
  );
}

export function EmptyState({ icon, text }: { icon: ReactNode; text: string }) {
  return (
    <div className="flex min-h-56 flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">
      {icon}
      <p className="m-0 text-sm">{text}</p>
    </div>
  );
}
