import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Mail,
  Menu,
  Search,
  ShieldCheck,
  Trash2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import {
  getFolderMessages,
  getMessage,
  markMailMessageRead,
  permanentlyDeleteMessage,
  restoreJunkMessage,
} from "../../lib/api";
import { flagsMarkedSeen, folderKind, folderLabel, messageIsUnread } from "../../lib/mailbox";
import type { Copy } from "../../lib/locale";
import { cn, formatTime } from "../../lib/utils";
import type { MailMessage } from "../../types";
import { useShell } from "./shell-context";
import { EmptyState, ErrorState, ListSkeleton } from "./common-states";
import { MailDetail } from "./chat-view";
import { Button } from "../ui/button";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "../ui/context-menu";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { ScrollArea } from "../ui/scroll-area";

export function FolderMessageRow({
  copy,
  message,
  address,
  selected,
  canNotSpam,
  canPermanentDelete,
  actionPending,
  onSelect,
  onNotSpam,
  onPermanentDelete,
}: {
  copy: Copy;
  message: MailMessage;
  address: string;
  selected: boolean;
  canNotSpam: boolean;
  canPermanentDelete: boolean;
  actionPending: boolean;
  onSelect: () => void;
  onNotSpam: () => void;
  onPermanentDelete: () => void;
}) {
  const unread = messageIsUnread(message);
  const row = (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "block w-full border-b bg-card px-4 py-3 text-left transition-colors hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50",
        selected && "border-l-2 border-l-foreground bg-muted pl-[0.875rem]"
      )}
    >
      <span className="flex items-baseline justify-between gap-2">
        <strong className={cn("min-w-0 truncate text-sm", unread ? "font-semibold text-foreground" : "font-medium text-muted-foreground")}>
          {address}
        </strong>
        <span className="flex shrink-0 items-center gap-2">
          {unread && <span className="size-1.5 rounded-full bg-primary" aria-label={copy.unread} title={copy.unread} />}
          <time className="text-[10px] text-muted-foreground">{formatTime(message.date)}</time>
        </span>
      </span>
      <span className={cn("mt-1 block truncate text-xs", unread ? "text-foreground/80" : "text-muted-foreground/70")}>
        {message.subject || copy.noSubject}
      </span>
    </button>
  );

  if (!canNotSpam && !canPermanentDelete) return row;

  return (
    <ContextMenu>
      <ContextMenuTrigger className="block">{row}</ContextMenuTrigger>
      <ContextMenuContent className="w-44">
        {canNotSpam && (
          <ContextMenuItem disabled={actionPending} className="gap-2 px-2 py-2" onClick={onNotSpam}>
            <ShieldCheck className="size-4" />
            {copy.notSpam}
          </ContextMenuItem>
        )}
        {canPermanentDelete && (
          <ContextMenuItem variant="destructive" disabled={actionPending} className="gap-2 px-2 py-2" onClick={onPermanentDelete}>
            <Trash2 className="size-4" />
            {copy.permanentDelete}
          </ContextMenuItem>
        )}
      </ContextMenuContent>
    </ContextMenu>
  );
}

export function FolderPage({ folder }: { folder: string }) {
  const queryClient = useQueryClient();
  const { copy: locale, accountEmail, folders, openMobileMenu } = useShell();
  const [selected, setSelected] = useState<string | null>(() => new URL(window.location.href).searchParams.get("message"));
  const [detailOpen, setDetailOpen] = useState(() => Boolean(new URL(window.location.href).searchParams.get("message")));
  const [search, setSearch] = useState("");
  const [permanentDeleteTarget, setPermanentDeleteTarget] = useState<MailMessage | null>(null);
  const [permanentDeleteError, setPermanentDeleteError] = useState("");
  const markingReadRef = useRef(new Set<string>());
  const list = useQuery({ queryKey: ["folder", folder], queryFn: () => getFolderMessages(folder), retry: 1 });
  const detail = useQuery({
    queryKey: ["message", folder, selected],
    queryFn: () => getMessage(folder, selected!),
    enabled: Boolean(selected),
    retry: 1,
  });

  useEffect(() => {
    if (list.error && list.data) {
      toast.error(list.error instanceof Error ? list.error.message : locale.loadFailed, {
        id: `folder-${folder}-refresh-error`,
        action: {
          label: locale.retry,
          onClick: () => void list.refetch(),
        },
      });
    } else if (!list.error) {
      toast.dismiss(`folder-${folder}-refresh-error`);
    }
  }, [folder, list.error, list.data, locale.loadFailed, locale.retry]);

  useEffect(() => {
    if (detail.error && detail.data) {
      toast.error(detail.error instanceof Error ? detail.error.message : locale.loadFailed, {
        id: `folder-${folder}-message-error`,
        action: {
          label: locale.retry,
          onClick: () => void detail.refetch(),
        },
      });
    } else if (!detail.error) {
      toast.dismiss(`folder-${folder}-message-error`);
    }
  }, [detail.error, detail.data, folder, locale.loadFailed, locale.retry]);

  const select = (id: string) => {
    setSelected(id);
    setDetailOpen(true);
    const url = new URL(window.location.href);
    url.searchParams.set("message", id);
    window.history.pushState({}, "", url);
  };

  const closeDetail = () => {
    setDetailOpen(false);
    setSelected(null);
    const url = new URL(window.location.href);
    url.searchParams.delete("message");
    window.history.pushState({}, "", url);
  };

  const currentMailbox = folders.find((mailbox) => mailbox.name === folder) || { name: folder, delimiter: "/", attributes: [] };
  const folderTitle = folderLabel(locale, currentMailbox);
  const isJunkFolder = folderKind(currentMailbox) === "junk";
  const isTrashFolder = folderKind(currentMailbox) === "trash";
  const canPermanentDelete = isJunkFolder || isTrashFolder;
  const messages = (list.data?.messages || []).filter((message) => {
    const query = search.trim().toLowerCase();
    return !query || [message.from, message.fromName, message.to, message.subject, message.preview].some((value) => value?.toLowerCase().includes(query));
  });

  const messageAddress = (message: MailMessage) => {
    const from = message.from?.trim() || "";
    const effectiveEmail = message.accountEmail?.trim() || accountEmail.trim();
    return effectiveEmail && from.toLowerCase() === effectiveEmail.toLowerCase()
      ? message.to || from || locale.me
      : from || message.to || locale.me;
  };

  const removeMessageFromView = async (message: MailMessage) => {
    if (selected === message.id) closeDetail();
    queryClient.removeQueries({ queryKey: ["message", folder, message.id] });
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["folder", folder] }),
      queryClient.invalidateQueries({ queryKey: ["conversations"] }),
      queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] }),
    ]);
  };

  const restoreMutation = useMutation({
    mutationFn: (message: MailMessage) => restoreJunkMessage(folder, message.id, message.accountEmail || accountEmail),
    onSuccess: async (_, message) => {
      await removeMessageFromView(message);
    },
    onError: (value) => toast.error(value instanceof Error ? value.message : locale.notSpamFailed),
  });

  const permanentDeleteMutation = useMutation({
    mutationFn: (message: MailMessage) => permanentlyDeleteMessage(folder, message.id, message.accountEmail || accountEmail),
    onSuccess: async (_, message) => {
      setPermanentDeleteTarget(null);
      setPermanentDeleteError("");
      await removeMessageFromView(message);
    },
    onError: (value) => setPermanentDeleteError(value instanceof Error ? value.message : locale.permanentDeleteFailed),
  });

  useEffect(() => {
    const restoreMessageFromURL = () => {
      const id = new URL(window.location.href).searchParams.get("message");
      setSelected(id);
      setDetailOpen(Boolean(id));
    };
    window.addEventListener("popstate", restoreMessageFromURL);
    return () => window.removeEventListener("popstate", restoreMessageFromURL);
  }, []);

  useEffect(() => {
    markingReadRef.current.clear();
  }, [selected]);

  useEffect(() => {
    if (!selected) return;
    const message = list.data?.messages.find((item) => item.id === selected);
    if (!message || !messageIsUnread(message)) return;
    const effectiveEmail = message.accountEmail || accountEmail || "";
    const key = `${effectiveEmail}/${folder}/${message.id}`;
    if (markingReadRef.current.has(key)) return;
    markingReadRef.current.add(key);
    const previousList = queryClient.getQueryData<{ messages: MailMessage[]; syncComplete: boolean; syncError?: string }>(["folder", folder]);
    const previousDetail = queryClient.getQueryData<MailMessage>(["message", folder, message.id]);
    queryClient.setQueryData<{ messages: MailMessage[]; syncComplete: boolean; syncError?: string }>(["folder", folder], (current) => current ? {
      ...current,
      messages: current.messages.map((item) => item.id === message.id ? { ...item, flags: flagsMarkedSeen(item.flags) } : item),
    } : current);
    queryClient.setQueryData<MailMessage>(["message", folder, message.id], (current) => current ? { ...current, flags: flagsMarkedSeen(current.flags) } : current);
    void markMailMessageRead(folder, message.id, effectiveEmail).then(() => {
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
      void queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] });
    }).catch((value) => {
      queryClient.setQueryData(["folder", folder], previousList);
      queryClient.setQueryData(["message", folder, message.id], previousDetail);
      toast.error(value instanceof Error ? value.message : locale.loadFailed);
    });
  }, [accountEmail, folder, list.data?.messages, locale.loadFailed, queryClient, selected]);

  return (
    <>
      <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
        <section className={cn("min-w-0 flex-1 flex-col border-r bg-card lg:w-[23.125rem] lg:flex-none", detailOpen ? "hidden lg:flex" : "flex")}>
          <div className="border-b bg-card px-3 py-3">
            <div className="flex items-center gap-2">
              <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={openMobileMenu} aria-label={locale.folders} title={locale.folders}>
                <Menu />
              </Button>
              <div className="relative flex min-w-0 flex-1 items-center">
                <Search className="pointer-events-none absolute left-3 size-4 text-muted-foreground" />
                <Input
                  type="search"
                  className="h-9 bg-muted/60 pl-9 pr-9"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={`${locale.search} · ${folderTitle}`}
                  aria-label={locale.search}
                />
                {search && (
                  <Button variant="ghost" size="icon" className="absolute right-1 size-7" onClick={() => setSearch("")} aria-label={locale.cancel}>
                    <X />
                  </Button>
                )}
              </div>
            </div>
          </div>
          <ScrollArea className="min-h-0 flex-1">
            {!list.data && !list.error && <ListSkeleton />}
            {!list.data && list.error && <ErrorState copy={locale} onRetry={() => void list.refetch()} />}
            {list.data && messages.length === 0 && <EmptyState icon={<Mail />} text={locale.noConversations} />}
            {list.data && messages.map((message) => (
              <FolderMessageRow
                key={message.id}
                copy={locale}
                message={message}
                address={messageAddress(message)}
                selected={selected === message.id}
                canNotSpam={isJunkFolder}
                canPermanentDelete={canPermanentDelete}
                actionPending={restoreMutation.isPending || permanentDeleteMutation.isPending}
                onSelect={() => select(message.id)}
                onNotSpam={() => restoreMutation.mutate(message)}
                onPermanentDelete={() => { setPermanentDeleteError(""); setPermanentDeleteTarget(message); }}
              />
            ))}
          </ScrollArea>
        </section>
        <section className={cn("min-w-0 flex-1 flex-col bg-surface", detailOpen ? "flex" : "hidden lg:flex")}>
          {detailOpen && (
            <header className="grid min-h-[4.5rem] grid-cols-[minmax(0,1fr)_minmax(0,2fr)_minmax(0,1fr)] items-center border-b bg-card px-3 py-3 sm:px-5">
              <div>
                <Button variant="ghost" size="icon" className="lg:hidden" onClick={closeDetail} aria-label={locale.back}>
                  <ArrowLeft />
                </Button>
              </div>
              <h2 className="truncate text-center text-sm font-semibold">{detail.data?.subject || folderTitle}</h2>
              <div className="flex items-center justify-end gap-1">
                {isJunkFolder && detail.data && (
                  <Button variant="ghost" size="sm" disabled={restoreMutation.isPending} onClick={() => restoreMutation.mutate(detail.data)} title={locale.notSpam}>
                    <ShieldCheck className="size-4" />
                    <span className="hidden sm:inline">{locale.notSpam}</span>
                  </Button>
                )}
                {canPermanentDelete && detail.data && (
                  <Button variant="ghost" size="icon" title={locale.permanentDelete} aria-label={locale.permanentDelete} disabled={permanentDeleteMutation.isPending} onClick={() => { setPermanentDeleteError(""); setPermanentDeleteTarget(detail.data); }}>
                    <Trash2 className="size-4" />
                  </Button>
                )}
              </div>
            </header>
          )}
          <ScrollArea className="min-h-0 flex-1" contentClassName="px-3 py-6 sm:px-[5vw] sm:py-8">
            {detail.isPending && !detail.data && selected ? (
              <div className="grid h-full place-items-center text-sm text-muted-foreground">{locale.loading}</div>
            ) : detail.error && !detail.data ? (
              <ErrorState copy={locale} onRetry={() => void detail.refetch()} />
            ) : detail.data ? (
              <MailDetail copy={locale} message={detail.data} />
            ) : (
              <EmptyState icon={<Mail />} text={locale.selectConversation} />
            )}
          </ScrollArea>
        </section>
      </main>
      <Dialog open={Boolean(permanentDeleteTarget)} onOpenChange={(open) => { if (!open && !permanentDeleteMutation.isPending) { setPermanentDeleteTarget(null); setPermanentDeleteError(""); } }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{locale.permanentDeleteTitle}</DialogTitle>
            <DialogDescription>{locale.permanentDeleteDescription}</DialogDescription>
          </DialogHeader>
          {permanentDeleteError && <p className="text-sm text-destructive">{permanentDeleteError}</p>}
          <DialogFooter>
            <Button variant="ghost" disabled={permanentDeleteMutation.isPending} onClick={() => setPermanentDeleteTarget(null)}>
              {locale.cancel}
            </Button>
            <Button variant="destructive" disabled={permanentDeleteMutation.isPending || !permanentDeleteTarget} onClick={() => permanentDeleteTarget && permanentDeleteMutation.mutate(permanentDeleteTarget)}>
              <Trash2 />
              {permanentDeleteMutation.isPending ? locale.deleting : locale.permanentDelete}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
