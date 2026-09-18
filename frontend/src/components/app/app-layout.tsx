import { useEffect, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  CalendarDays,
  Check,
  ChevronDown,
  ChevronRight,
  FilePenLine,
  Folder,
  MessageCircle,
  Moon,
  Paperclip,
  Pencil,
  Settings,
  Sun,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import { ApiError, getCapabilities, getConversations, signOut, switchAccount } from "../../lib/api";
import { cn, isSentMailbox } from "../../lib/utils";
import { prefersDarkMode } from "../../lib/theme";
import { folderKind, folderLabel } from "../../lib/mailbox";
import { type Copy, useLocale } from "../../lib/locale";
import type { ComposeDefaults } from "../../lib/email-format";
import type { ConversationListResponse, Mailbox } from "../../types";
import { ShellContext, type ShellContextType } from "./shell-context";
import { ComposeDialog } from "./compose-dialog";
import { SettingsDialog } from "./settings-dialog";
import { LoginScreen } from "./auth-screens";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { ScrollArea } from "../ui/scroll-area";
import { Skeleton } from "../ui/skeleton";
import { ResizeHandle, usePersistedPaneWidth } from "./resize-handle";

export function FolderLink({ copy, folder, selected = false, onClose }: { copy: Copy; folder: Mailbox; selected?: boolean; onClose: () => void }) {
  const kind = folderKind(folder);
  const Icon = kind === "trash" ? Trash2 : kind === "junk" ? TriangleAlert : kind === "drafts" ? FilePenLine : kind === "archive" ? Archive : Folder;
  return (
    <Button
      nativeButton={false}
      render={<a href={`/folder/${encodeURIComponent(folder.name)}`} onClick={onClose} />}
      variant={selected ? "secondary" : "ghost"}
      size="sm"
      className={cn("w-full justify-start gap-2.5 pl-9 text-muted-foreground", selected && "text-sidebar-accent-foreground")}
    >
      <Icon />
      <span className="min-w-0 flex-1 truncate">{folderLabel(copy, folder)}</span>
      {folder.unreadCount ? <Badge variant="secondary" className="min-w-5 justify-center px-1.5 text-[10px]">{folder.unreadCount}</Badge> : null}
    </Button>
  );
}

export function AccountMenu({ copy, accounts, accountEmail }: { copy: Copy; accounts: ConversationListResponse["accounts"]; accountEmail: string }) {
  const active = accounts.find((account) => account.isActive || account.email === accountEmail);
  const selectAccount = async (account: ConversationListResponse["accounts"][number]) => {
    if (account.email === accountEmail) return;
    await switchAccount(account.id);
    window.location.assign("/inbox");
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" className="min-w-0 flex-1 justify-start px-2" />}>
        <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: active?.color || "#777" }} />
        <span className="min-w-0 flex-1 truncate text-left">{active?.label || accountEmail}</span>
        <ChevronDown className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" sideOffset={6} className="w-60">
        {accounts.map((account) => (
          <DropdownMenuItem key={account.id} className="gap-2 px-2 py-2" onClick={() => void selectAccount(account)}>
            <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: account.color || "#777" }} />
            <span className="min-w-0 flex-1">
              <strong className="block truncate font-medium">{account.label || account.email}</strong>
              <small className="block truncate text-muted-foreground">{account.email}</small>
            </span>
            {(account.isActive || account.email === accountEmail) && <Check className="size-4 shrink-0" />}
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          className="px-2 py-2"
          onClick={() => { void signOut().then(() => window.location.assign("/user-login")); }}
        >
          {copy.signOut}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function Sidebar({
  copy,
  folders,
  accounts,
  accountEmail,
  calendarEnabled,
  currentFolder,
  currentView,
  loading = false,
  onCompose,
  onSettings,
  open,
  onClose,
  darkMode,
  onToggleDarkMode,
  desktopWidth,
}: {
  copy: Copy;
  folders: Mailbox[];
  accounts: ConversationListResponse["accounts"];
  accountEmail: string;
  calendarEnabled: boolean;
  currentFolder?: string;
  currentView?: "mail" | "calendar" | "attachments" | "documents";
  loading?: boolean;
  onCompose: () => void;
  onSettings: () => void;
  open: boolean;
  onClose: () => void;
  darkMode: boolean;
  onToggleDarkMode: () => void;
  desktopWidth: number;
}) {
  const [foldersOpen, setFoldersOpen] = useState(true);
  const visibleFolders = folders.filter((folder) => folder.name.toLowerCase() !== "inbox" && !isSentMailbox(folder));
  const navClass = "w-full justify-start gap-2.5 px-3 text-muted-foreground";
  return (
    <aside style={{ "--sidebar-width": `${desktopWidth}px` } as React.CSSProperties} className={cn("fixed inset-y-0 left-0 z-40 flex w-60 -translate-x-full flex-col border-r bg-sidebar px-3 py-4 transition-transform lg:static lg:z-auto lg:w-(--sidebar-width) lg:shrink-0 lg:translate-x-0", open && "translate-x-0 ring-1 ring-foreground/10")}>
      <Button data-testid="compose-button" className="mb-4 w-full" onClick={onCompose}><Pencil />{copy.compose}</Button>
      <nav className="flex min-h-0 flex-1 flex-col gap-1">
        <Button nativeButton={false} render={<a href="/inbox" onClick={onClose} />} variant={!currentFolder && currentView !== "calendar" && currentView !== "attachments" && currentView !== "documents" ? "secondary" : "ghost"} size="sm" className={cn(navClass, !currentFolder && currentView !== "calendar" && currentView !== "attachments" && currentView !== "documents" && "bg-sidebar-accent text-sidebar-accent-foreground")}><MessageCircle /><span>{copy.conversations}</span></Button>
        <Button nativeButton={false} render={<a href="/documents" onClick={onClose} />} variant={currentView === "documents" ? "secondary" : "ghost"} size="sm" className={cn(navClass, currentView === "documents" && "bg-sidebar-accent text-sidebar-accent-foreground")}><FilePenLine /><span>{copy.documents}</span></Button>
        <Button nativeButton={false} render={<a href="/attachments" onClick={onClose} />} variant={currentView === "attachments" ? "secondary" : "ghost"} size="sm" className={cn(navClass, currentView === "attachments" && "bg-sidebar-accent text-sidebar-accent-foreground")}><Paperclip /><span>{copy.attachmentManager}</span></Button>
        {calendarEnabled && <Button nativeButton={false} render={<a href="/calendar" onClick={onClose} />} variant={currentView === "calendar" ? "secondary" : "ghost"} size="sm" className={cn(navClass, currentView === "calendar" && "bg-sidebar-accent text-sidebar-accent-foreground")}><CalendarDays /><span>{copy.calendar}</span></Button>}
        <Button variant="ghost" size="sm" className={cn(navClass, "mt-2 text-xs uppercase tracking-wide text-muted-foreground")} onClick={() => setFoldersOpen((value) => !value)}><ChevronRight className={cn("transition-transform", foldersOpen && "rotate-90")} /><span>{copy.folders}</span></Button>
        {foldersOpen && (
          <ScrollArea className="min-h-0 flex-1" contentClassName="flex flex-col gap-1">
            {visibleFolders.map((folder) => <FolderLink key={folder.name} copy={copy} folder={folder} selected={folder.name === currentFolder} onClose={onClose} />)}
            {loading && !visibleFolders.length ? (
              <div className="grid gap-2 px-9 py-2">
                <Skeleton className="h-4 w-20" />
                <Skeleton className="h-4 w-16" />
                <Skeleton className="h-4 w-24" />
              </div>
            ) : !visibleFolders.length ? (
              <span className="px-9 py-2 text-xs text-muted-foreground">{copy.noConversations}</span>
            ) : null}
          </ScrollArea>
        )}
        <div className="mt-auto flex min-w-0 items-center justify-start gap-1 border-t pt-3">
          <AccountMenu copy={copy} accounts={accounts} accountEmail={accountEmail} />
          <Button variant="ghost" size="icon" onClick={onSettings} aria-label={copy.settings} title={copy.settings}><Settings /></Button>
          <Button variant="ghost" size="icon" onClick={onToggleDarkMode} aria-label={darkMode ? copy.lightMode : copy.darkMode} title={darkMode ? copy.lightMode : copy.darkMode}>
            {darkMode ? <Sun /> : <Moon />}
          </Button>
        </div>
      </nav>
    </aside>
  );
}

export function AppLayout({ path, children }: { path: string; children: ReactNode }) {
  const queryClient = useQueryClient();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [composeDefaults, setComposeDefaults] = useState<ComposeDefaults>({ to: "", subject: "" });
  const [settingsOpen, setSettingsOpen] = useState(() => new URLSearchParams(window.location.search).get("setup") === "1");
  const [darkMode, setDarkMode] = useState(prefersDarkMode);
  const [sidebarWidth, setSidebarWidth] = usePersistedPaneWidth("inbrix-sidebar-width", 230, 180, 360);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", darkMode);
    window.localStorage.setItem("inbrix-theme", darkMode ? "dark" : "light");
  }, [darkMode]);

  const shell = useQuery({
    queryKey: ["mailbox-shell"],
    queryFn: () => getConversations(),
    staleTime: 60_000,
    refetchInterval: 30_000,
  });

  const capabilities = useQuery({
    queryKey: ["capabilities"],
    queryFn: getCapabilities,
    staleTime: 60_000,
  });

  useEffect(() => {
    if (!capabilities.data?.notifications) return;
    const events = new EventSource("/events", { withCredentials: true });
    events.onmessage = (event) => {
      void queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] });
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
      void queryClient.invalidateQueries({ queryKey: ["conversation"] });
      void queryClient.invalidateQueries({ queryKey: ["folder"] });
      void queryClient.invalidateQueries({ queryKey: ["attachments"] });
      try {
        const payload = JSON.parse(String(event.data)) as { from?: string; subject?: string };
        if ("Notification" in window && Notification.permission === "granted" && document.visibilityState !== "visible") {
          new Notification(payload.from ? `New mail from ${payload.from}` : "New mail", { body: payload.subject || "" });
        }
      } catch {
        // A malformed optional notification must not interrupt inbox refreshes.
      }
    };
    return () => events.close();
  }, [capabilities.data?.notifications, queryClient]);

  const copy = useLocale(shell.data?.locale);

  const openCompose = (defaults: ComposeDefaults = { to: "", subject: "" }) => {
    setComposeDefaults(defaults);
    setComposeOpen(true);
    setSidebarOpen(false);
  };

  const openSettings = () => {
    setSidebarOpen(false);
    setSettingsOpen(true);
  };

  const openMobileMenu = () => {
    setSidebarOpen(true);
  };

  if (shell.error instanceof ApiError && shell.error.status === 401) {
    return <LoginScreen copy={copy} />;
  }

  let currentView: "mail" | "calendar" | "attachments" | "documents" = "mail";
  let currentFolder: string | undefined = undefined;

  if (path === "/documents" || path.startsWith("/documents/")) {
    currentView = "documents";
  } else if (path === "/attachments") {
    currentView = "attachments";
  } else if (path === "/calendar" || path === "/calendar/week") {
    currentView = "calendar";
  } else if (path.startsWith("/folder/")) {
    currentView = "mail";
    currentFolder = decodeURIComponent(path.slice("/folder/".length));
  } else {
    currentView = "mail";
  }

  const contextValue: ShellContextType = {
    copy,
    accountEmail: shell.data?.accountEmail || "",
    folders: shell.data?.folders || [],
    accounts: shell.data?.accounts || [],
    openMobileMenu,
    openCompose,
    openSettings,
  };

  return (
    <ShellContext.Provider value={contextValue}>
      <div className="flex h-screen min-h-[32.5rem] overflow-hidden bg-background">
        {sidebarOpen && (
          <button
            className="fixed inset-0 z-30 bg-black/10 supports-backdrop-filter:backdrop-blur-xs lg:hidden"
            aria-label={copy.cancel}
            onClick={() => setSidebarOpen(false)}
          />
        )}
        <Sidebar
          copy={copy}
          folders={shell.data?.folders || []}
          accounts={shell.data?.accounts || []}
          accountEmail={shell.data?.accountEmail || ""}
          calendarEnabled={capabilities.data?.calendar === true}
          currentFolder={currentFolder}
          currentView={currentView}
          loading={shell.isPending}
          onCompose={() => openCompose()}
          onSettings={openSettings}
          open={sidebarOpen}
          onClose={() => setSidebarOpen(false)}
          darkMode={darkMode}
          onToggleDarkMode={() => setDarkMode((value) => !value)}
          desktopWidth={sidebarWidth}
        />
        <ResizeHandle label="Resize navigation" width={sidebarWidth} minWidth={180} maxWidth={360} onResize={setSidebarWidth} />
        {children}
        <ComposeDialog
          copy={copy}
          open={composeOpen}
          defaults={composeDefaults}
          accountEmail={composeDefaults.accountEmail || shell.data?.accountEmail || ""}
          onOpenChange={setComposeOpen}
          onSent={() => {
            void queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] });
            void queryClient.invalidateQueries({ queryKey: ["conversations"] });
            void queryClient.invalidateQueries({ queryKey: ["conversation"] });
            void queryClient.invalidateQueries({ queryKey: ["folder"] });
            void queryClient.invalidateQueries({ queryKey: ["attachments"] });
          }}
        />
        <SettingsDialog copy={copy} open={settingsOpen} onOpenChange={setSettingsOpen} />
      </div>
    </ShellContext.Provider>
  );
}
