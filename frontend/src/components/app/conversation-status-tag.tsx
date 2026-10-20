import { Check } from "lucide-react";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "../ui/dropdown-menu";
import { cn } from "../../lib/utils";
import type { ConversationSummary } from "../../types";

export type ConversationStatus = ConversationSummary["status"];
export type EditableConversationStatus = Exclude<ConversationStatus, "draft">;

export const draftStatusClassName = "border-sky-500 bg-sky-400 text-sky-950 dark:border-sky-300 dark:bg-sky-400 dark:text-sky-950";

const statusStyles: Record<ConversationStatus, string> = {
  answered: "border-emerald-300 bg-emerald-100 text-emerald-800 dark:border-emerald-400/60 dark:bg-emerald-400/25 dark:text-emerald-200",
  unanswered: "border-amber-300 bg-amber-100 text-amber-800 dark:border-amber-400/60 dark:bg-amber-400/25 dark:text-amber-200",
  no_action: "border-slate-300 bg-slate-100 text-slate-700 dark:border-slate-400/60 dark:bg-slate-400/25 dark:text-slate-200",
  draft: draftStatusClassName,
};

export function ConversationStatusTag({ value, labels, draftHint, disabled, onChange }: {
  value: ConversationStatus;
  labels: Record<ConversationStatus, string>;
  draftHint: string;
  disabled?: boolean;
  onChange: (value: EditableConversationStatus) => void;
}) {
  const statuses: EditableConversationStatus[] = ["answered", "unanswered", "no_action"];
  const tagClassName = cn("inline-flex h-5 shrink-0 items-center rounded-full border px-2 text-[10px] font-medium leading-none outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-60", statusStyles[value]);
  // Draft is derived from an actual unsent email, so it cannot be manually
  // cleared while that email is still waiting to be sent or discarded.
  if (value === "draft") {
    return <span data-testid="conversation-status" data-status={value} className={cn(tagClassName, "font-semibold")} title={draftHint}>{labels.draft}</span>;
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        render={<button type="button" />}
        data-testid="conversation-status"
        data-status={value}
        className={tagClassName}
        onClick={(event) => event.stopPropagation()}
        aria-label={labels[value]}
      >
        {labels[value]}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-32">
        {statuses.map((status) => (
          <DropdownMenuItem key={status} onClick={() => onChange(status)}>
            <span className={cn("size-2 rounded-full", statusStyles[status])} />
            <span className="flex-1">{labels[status]}</span>
            {status === value && <Check className="size-3.5" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
