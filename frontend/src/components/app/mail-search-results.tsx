import { useMemo } from "react";
import { MessageCircle, Search } from "lucide-react";
import type { MailSearchResult } from "../../types";
import type { Copy } from "../../lib/locale";
import { isSentMailbox, cn, formatTime } from "../../lib/utils";
import { EmptyState, ErrorState, ListSkeleton } from "./common-states";
import { Badge } from "../ui/badge";

/** Everything the conversation-list pane needs to render search results. */
export type MailSearchState = {
  query: string;
  terms: string[];
  results: MailSearchResult[];
  hasMore: boolean;
  loading: boolean;
  error: Error | null;
  /** Identity of the message currently open, when it came from these results. */
  selectedKey?: string;
  onSelect: (result: MailSearchResult) => void;
  onRetry: () => void;
};

/** Stable identity for one search result row. */
export function mailSearchResultKey(result: MailSearchResult) {
  return `${result.conversationId}:${result.folder}:${result.messageId}`;
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Renders text with the matched search terms emphasized. Terms come from the
 * server, so highlighting follows exactly what the search matched.
 */
export function HighlightedText({ text, terms, className }: { text: string; terms: string[]; className?: string }) {
  const pattern = useMemo(() => {
    const usable = terms.map((term) => term.trim()).filter(Boolean).sort((a, b) => b.length - a.length);
    if (!usable.length) return null;
    return new RegExp(`(${usable.map(escapeRegExp).join("|")})`, "gi");
  }, [terms]);

  if (!pattern || !text) return <span className={className}>{text}</span>;

  const parts = text.split(pattern);
  return (
    <span className={className}>
      {parts.map((part, index) =>
        index % 2 === 1 ? (
          <mark key={index} className="rounded-[3px] bg-amber-200/70 px-0.5 text-inherit dark:bg-amber-300/30">
            {part}
          </mark>
        ) : (
          part
        )
      )}
    </span>
  );
}

/** Folder label for a raw folder name coming back from the search API. */
function searchFolderLabel(copy: Copy, folder: string) {
  const name = folder || "INBOX";
  if (name.toLowerCase() === "inbox") return copy.inboxFolder;
  if (name.toLowerCase() === "outbox") return copy.draftsFolder;
  if (isSentMailbox({ name, attributes: [] })) return copy.sentFolder;
  return name;
}

export function MailSearchResultRow({
  copy,
  result,
  terms,
  selected,
  onSelect,
}: {
  copy: Copy;
  result: MailSearchResult;
  terms: string[];
  selected: boolean;
  onSelect: () => void;
}) {
  const sender = result.outgoing ? copy.me : result.fromName || result.from || result.conversationTitle;
  return (
    <button
      type="button"
      data-testid="mail-search-result"
      onClick={onSelect}
      className={cn(
        "block w-full border-b bg-card px-4 py-3 text-left transition-colors hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50",
        selected && "border-l-2 border-l-foreground bg-muted pl-[0.875rem]"
      )}
    >
      <span className="flex min-w-0 items-baseline justify-between gap-2">
        <strong className="min-w-0 flex-1 truncate text-sm font-semibold">{sender}</strong>
        <time className="shrink-0 text-[10px] text-muted-foreground">{formatTime(result.date)}</time>
      </span>
      <span className="mt-1 block max-w-full truncate text-xs font-medium text-foreground/90">
        <HighlightedText text={result.subject || copy.noSubject} terms={terms} />
      </span>
      <span className="mt-1 block max-w-full text-xs leading-5 text-muted-foreground/80 line-clamp-2">
        <HighlightedText text={result.snippet || copy.noBody} terms={terms} />
      </span>
      <span className="mt-1.5 flex min-w-0 items-center gap-1.5 text-[10px] text-muted-foreground">
        <Badge variant="secondary" className="px-1.5 py-0 text-[10px] font-normal">
          {searchFolderLabel(copy, result.folder)}
        </Badge>
        {result.accountLabel && result.accountLabel !== result.accountEmail ? (
          <span className="flex min-w-0 items-center gap-1">
            <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: result.accountColor || "#777" }} />
            <span className="truncate">{result.accountLabel}</span>
          </span>
        ) : null}
        <span className="ml-auto flex shrink-0 items-center gap-1">
          {result.matchedSubject && <span>{copy.searchMatchedSubject}</span>}
          {result.matchedSubject && result.matchedBody && <span>·</span>}
          {result.matchedBody && <span>{copy.searchMatchedBody}</span>}
        </span>
      </span>
    </button>
  );
}

export function MailSearchResults({ copy, state }: { copy: Copy; state: MailSearchState }) {
  if (state.error && !state.results.length) {
    return <ErrorState copy={copy} onRetry={state.onRetry} />;
  }
  if (state.loading && !state.results.length) {
    return <ListSkeleton />;
  }
  if (!state.results.length) {
    return <EmptyState icon={<Search />} text={copy.searchNoResults} />;
  }
  return (
    <div data-testid="mail-search-results">
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b bg-card/95 px-4 py-2 text-xs text-muted-foreground backdrop-blur-xs">
        <Search className="size-3.5 shrink-0" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate">
          <strong className="font-medium text-foreground">{state.results.length}</strong> {copy.searchResultCount}
        </span>
        {state.loading && <span className="shrink-0">{copy.loading}</span>}
      </div>
      {state.results.map((result) => (
        <MailSearchResultRow
          key={mailSearchResultKey(result)}
          copy={copy}
          result={result}
          terms={state.terms}
          selected={state.selectedKey === mailSearchResultKey(result)}
          onSelect={() => state.onSelect(result)}
        />
      ))}
      {state.hasMore && (
        <p className="flex items-center gap-1.5 px-4 py-3 text-xs text-muted-foreground">
          <MessageCircle className="size-3.5 shrink-0" aria-hidden="true" />
          {copy.searchMoreResults}
        </p>
      )}
    </div>
  );
}
