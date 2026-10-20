import { PenLine } from "lucide-react";
import { Badge } from "../ui/badge";
import { cn, formatTime } from "../../lib/utils";
import type { ComposeDraft } from "../../types";
import type { Copy } from "../../lib/locale";
import { draftStatusClassName } from "./conversation-status-tag";

/**
 * The bottom-most entry of a transcript when a reply has been started but not
 * sent. It stands exactly where the sent message will appear — same outgoing
 * header, same width — so the draft reads as part of the conversation rather
 * than as a window floating over it.
 *
 * Clicking it reopens the editor; the editor itself replaces this row, so a
 * conversation only ever shows one of the two.
 */
export function ThreadDraftEntry({ copy, draft, onOpen, className }: {
  copy: Copy;
  draft: ComposeDraft;
  onOpen: () => void;
  className?: string;
}) {
  const preview = draft.bodyText.trim();

  return (
    <article data-testid="thread-draft-entry" data-draft-id={draft.id} className={cn("mb-5", className)}>
      <div className="mb-1 flex min-w-0 items-start gap-2 text-xs leading-tight text-muted-foreground justify-end text-right">
        <div className="min-w-0">
          <div className="flex items-center justify-end gap-2">
            <span className="font-medium">{copy.me}</span>
            <Badge variant="outline" className={cn("h-4 px-1.5 text-[10px] font-semibold", draftStatusClassName)} data-testid="thread-draft-label">
              {copy.draftBadge}
            </Badge>
            <time>{formatTime(draft.updatedAt)}</time>
          </div>
          <div className="mt-1 max-w-full break-words text-[11px]">
            <span className="font-medium">{copy.to}:</span> {draft.to || "-"}
            {draft.cc && (
              <>
                <span className="mx-1.5">·</span>
                <span className="font-medium">{copy.cc}:</span> {draft.cc}
              </>
            )}
          </div>
        </div>
      </div>
      <div className="flex items-end justify-end gap-2">
        <button
          type="button"
          onClick={onOpen}
          title={copy.draftOpenHint}
          className="min-w-0 w-full cursor-pointer rounded-xl border border-dashed border-primary/40 bg-primary/5 px-3 py-2 text-left text-sm leading-relaxed text-foreground transition-colors hover:bg-primary/10 focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-hidden"
        >
          <span className="line-clamp-4 whitespace-pre-wrap text-muted-foreground">{preview || copy.noBody}</span>
          <span className="mt-1.5 flex items-center gap-1 text-[11px] font-medium text-primary">
            <PenLine className="size-3" />
            {copy.draftContinue}
          </span>
        </button>
      </div>
    </article>
  );
}
