import { Check } from "lucide-react";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "../ui/dropdown-menu";
import { cn } from "../../lib/utils";
import type { ConversationSummary } from "../../types";

export type ConversationStatus = ConversationSummary["status"];

const statusStyles: Record<ConversationStatus, string> = {
  answered: "border-emerald-600/25 bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  unanswered: "border-amber-600/25 bg-amber-500/15 text-amber-700 dark:text-amber-300",
  no_action: "border-slate-500/25 bg-slate-500/15 text-slate-600 dark:text-slate-300",
};

export function ConversationStatusTag({ value, labels, disabled, onChange }: {
  value: ConversationStatus;
  labels: Record<ConversationStatus, string>;
  disabled?: boolean;
  onChange: (value: ConversationStatus) => void;
}) {
  const statuses: ConversationStatus[] = ["answered", "unanswered", "no_action"];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        render={<button type="button" />}
        className={cn("inline-flex h-5 shrink-0 items-center rounded-full border px-2 text-[10px] font-medium leading-none outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-60", statusStyles[value])}
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
