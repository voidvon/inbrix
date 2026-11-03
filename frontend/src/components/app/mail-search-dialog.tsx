import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, Search, X } from "lucide-react";
import { searchMailMessages } from "../../lib/api";
import type { Copy } from "../../lib/locale";
import { useDebouncedValue } from "../../lib/utils";
import type { MailSearchResult } from "../../types";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { MailSearchResults, mailSearchResultKey } from "./mail-search-results";

export function MailSearchDialog({ open, onOpenChange, copy, onSelect }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  copy: Copy;
  onSelect: (result: MailSearchResult) => void;
}) {
  const [search, setSearch] = useState("");
  const [selection, setSelection] = useState({ query: "", index: 0 });
  const resultsRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const query = useDebouncedValue(search.trim(), 250);
  const waiting = search.trim() !== query;
  const mailSearch = useQuery({
    queryKey: ["mail-search", query],
    queryFn: () => searchMailMessages(query),
    enabled: open && query.length > 0,
    retry: 1,
  });
  const results = waiting ? [] : mailSearch.data?.results || [];
  const activeIndex = Math.min(selection.query === query ? selection.index : 0, Math.max(0, results.length - 1));

  useEffect(() => {
    resultsRef.current?.querySelectorAll<HTMLElement>('[data-testid="mail-search-result"]')[activeIndex]?.scrollIntoView({ block: "nearest" });
  }, [activeIndex, query, results.length]);

  const select = (result: MailSearchResult) => {
    onOpenChange(false);
    onSelect(result);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="mail-search-dialog" showCloseButton={false} initialFocus={inputRef}
        className="top-[12dvh] flex max-h-[76dvh] -translate-y-0 flex-col gap-0 overflow-hidden rounded-2xl p-0 shadow-2xl sm:max-w-2xl"
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return;
          if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
            event.preventDefault();
            onOpenChange(false);
            return;
          }
          if ((event.key === "ArrowDown" || event.key === "ArrowUp") && results.length) {
            event.preventDefault();
            const direction = event.key === "ArrowDown" ? 1 : -1;
            setSelection({ query, index: (activeIndex + direction + results.length) % results.length });
            inputRef.current?.focus();
          } else if (event.key === "Enter" && event.target === inputRef.current && results[activeIndex]) {
            event.preventDefault();
            select(results[activeIndex]);
          }
        }}>
        <DialogTitle className="sr-only">{copy.search}</DialogTitle>
        <DialogDescription className="sr-only">{copy.searchSpotlightHint}</DialogDescription>
        <div className="flex shrink-0 items-center gap-3 border-b px-4 py-3 sm:px-5 sm:py-4">
          {mailSearch.isFetching && search.trim() ? <Loader2 className="size-5 shrink-0 animate-spin text-muted-foreground" /> : <Search className="size-5 shrink-0 text-muted-foreground" />}
          <Input ref={inputRef} data-testid="mail-search" type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder={copy.searchHint} aria-label={copy.search}
            className="h-10 min-w-0 flex-1 border-0 bg-transparent px-0 text-base shadow-none focus-visible:ring-0 dark:bg-transparent sm:text-lg" />
          <Button variant="ghost" size="icon-sm" onClick={() => onOpenChange(false)} aria-label={copy.cancel} title={copy.cancel}><X /></Button>
        </div>
        <div ref={resultsRef} className="min-h-0 overflow-y-auto overscroll-contain" aria-label={copy.searchResults} aria-live="polite" aria-busy={waiting || mailSearch.isFetching}>
          {search.trim() ? <MailSearchResults copy={copy} state={{
            query,
            terms: mailSearch.data?.terms || [],
            results,
            hasMore: !waiting && (mailSearch.data?.hasMore || false),
            loading: waiting || mailSearch.isFetching,
            error: waiting ? null : mailSearch.error,
            selectedKey: results[activeIndex] ? mailSearchResultKey(results[activeIndex]) : undefined,
            onSelect: select,
            onRetry: () => void mailSearch.refetch(),
          }} /> : <div className="flex flex-col items-center gap-3 px-6 py-12 text-center text-sm text-muted-foreground"><Search className="size-8 opacity-40" /><p>{copy.searchSpotlightHint}</p></div>}
        </div>
        <div className="flex shrink-0 items-center justify-between gap-3 border-t bg-muted/30 px-4 py-2 text-[10px] text-muted-foreground sm:px-5">
          <span>{copy.searchKeyboardHint}</span><kbd className="rounded border px-1.5 py-0.5">⌘ / Ctrl K</kbd>
        </div>
      </DialogContent>
    </Dialog>
  );
}
