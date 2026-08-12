import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArrowLeft,
  ChevronDown,
  ChevronRight,
  FilePenLine,
  Folder,
  Italic,
  Link,
  List,
  ListOrdered,
  Mail,
  Menu,
  MessageCircle,
  Moon,
  MoreHorizontal,
  Paperclip,
  Pencil,
  RefreshCw,
  Search,
  Send,
  Settings,
  Sun,
  Trash2,
  TriangleAlert,
  Underline,
  X,
  Bold,
} from "lucide-react";
import { EditorContent, useEditor } from "@tiptap/react";
import LinkExtension from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import StarterKit from "@tiptap/starter-kit";
import UnderlineExtension from "@tiptap/extension-underline";
import { ApiError, getConversation, getConversations, sendMessage, signIn, signOut, switchLanguage } from "./lib/api";
import { cn, formatFullDate, formatMailDate, formatSize, initials, isSentMailbox, linkifyText } from "./lib/utils";
import { Avatar, AvatarFallback } from "./components/ui/avatar";
import { Badge } from "./components/ui/badge";
import { Button } from "./components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "./components/ui/dialog";
import { Input } from "./components/ui/input";
import { Label } from "./components/ui/label";
import { Separator } from "./components/ui/separator";
import { Skeleton } from "./components/ui/skeleton";
import type { ConversationDetail, ConversationMessage, ConversationSummary, ConversationListResponse, Mailbox } from "./types";

const zh = {
  conversations: "对话",
  compose: "写邮件",
  search: "搜索邮件",
  folders: "文件夹",
  settings: "设置",
  darkMode: "深色模式",
  lightMode: "浅色模式",
  signOut: "退出登录",
  refresh: "刷新对话",
  messages: "条消息",
  noConversations: "暂无对话",
  selectConversation: "选择一个对话",
  noSubject: "无主题",
  me: "我",
  reply: "回复",
  send: "发送",
  cancel: "取消",
  to: "收件人",
  subject: "主题",
  writeMessage: "写下邮件内容…",
  sending: "正在发送…",
  attach: "添加附件",
  login: "登录",
  appAccount: "应用账号",
  password: "密码",
  loginFailed: "登录失败",
  loading: "正在加载…",
  loadFailed: "本地会话加载失败",
  retry: "重试",
  unread: "未读",
  noBody: "邮件没有正文",
};

const en = {
  conversations: "Conversations",
  compose: "Compose",
  search: "Search mail",
  folders: "Folders",
  settings: "Settings",
  darkMode: "Dark mode",
  lightMode: "Light mode",
  signOut: "Sign out",
  refresh: "Refresh conversations",
  messages: "messages",
  noConversations: "No conversations",
  selectConversation: "Select a conversation",
  noSubject: "No subject",
  me: "Me",
  reply: "Reply",
  send: "Send",
  cancel: "Cancel",
  to: "To",
  subject: "Subject",
  writeMessage: "Write your message…",
  sending: "Sending…",
  attach: "Attach file",
  login: "Sign in",
  appAccount: "Application account",
  password: "Password",
  loginFailed: "Sign in failed",
  loading: "Loading…",
  loadFailed: "Could not load local conversations",
  retry: "Retry",
  unread: "unread",
  noBody: "This email has no body",
};

type Copy = typeof zh;

function useDebouncedValue(value: string, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

function useLocale(value?: string): Copy {
  return value === "en" ? en : zh;
}

function prefersDarkMode() {
  if (typeof window === "undefined") return false;
  const stored = window.localStorage.getItem("lilmail-theme");
  if (stored === "dark") return true;
  if (stored === "light") return false;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function MailAvatar({ label, small = false }: { label: string; small?: boolean }) {
  return (
    <Avatar className={cn(small ? "size-6" : "size-8")}>
      <AvatarFallback>{label}</AvatarFallback>
    </Avatar>
  );
}

function App() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [chatOpen, setChatOpen] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [composeDefaults, setComposeDefaults] = useState({ to: "", subject: "" });
  const [darkMode, setDarkMode] = useState(prefersDarkMode);
  const debouncedSearch = useDebouncedValue(search, 250);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", darkMode);
    window.localStorage.setItem("lilmail-theme", darkMode ? "dark" : "light");
  }, [darkMode]);

  const conversations = useQuery({
    queryKey: ["conversations", debouncedSearch],
    queryFn: () => getConversations(debouncedSearch),
    refetchInterval: 30_000,
  });
  const locale = useLocale(conversations.data?.locale);
  const detail = useQuery({
    queryKey: ["conversation", selectedId],
    queryFn: () => getConversation(selectedId!),
    enabled: Boolean(selectedId),
  });

  useEffect(() => {
    const items = conversations.data?.conversations || [];
    if (selectedId && !items.some((item) => item.id === selectedId)) {
      setSelectedId(null);
      setChatOpen(false);
    }
    if (!selectedId && items.length > 0 && window.innerWidth > 1023) {
      setSelectedId(items[0].id);
      setChatOpen(true);
    }
  }, [conversations.data, selectedId]);

  const openCompose = (defaults = { to: "", subject: "" }) => {
    setComposeDefaults(defaults);
    setComposeOpen(true);
    setSidebarOpen(false);
  };

  const openReply = (conversation: ConversationDetail) => {
    const latest = conversation.messages.at(-1);
    const replySubject = conversation.subject.toLowerCase().startsWith("re:") ? conversation.subject : `Re: ${conversation.subject}`;
    openCompose({ to: latest?.from || conversation.peerEmail || "", subject: replySubject });
  };

  const authenticated = conversations.error instanceof ApiError && conversations.error.status === 401;
  if (authenticated) return <LoginScreen copy={locale} />;

  return (
    <div className="flex min-h-screen flex-col bg-background">
      <Topbar copy={locale} email={conversations.data?.accountEmail || ""} search={search} onSearch={setSearch} onMenu={() => setSidebarOpen(true)} onCompose={() => openCompose()} />
      <div className="flex h-[calc(100vh-3.5rem)] min-h-[32.5rem] overflow-hidden">
        {sidebarOpen && <button className="fixed inset-x-0 top-14 bottom-0 z-30 bg-black/10 supports-backdrop-filter:backdrop-blur-xs lg:hidden" aria-label={locale.cancel} onClick={() => setSidebarOpen(false)} />}
        <Sidebar copy={locale} folders={conversations.data?.folders || []} onCompose={() => openCompose()} open={sidebarOpen} onClose={() => setSidebarOpen(false)} darkMode={darkMode} onToggleDarkMode={() => setDarkMode((value) => !value)} />
        <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
          <ConversationList
            copy={locale}
            data={conversations.data}
            search={search}
            loading={conversations.isPending}
            error={conversations.error}
            selectedId={selectedId}
            onSelect={(id) => {
              setSelectedId(id);
              setChatOpen(true);
              setSidebarOpen(false);
            }}
            onRefresh={() => void conversations.refetch()}
            className={chatOpen ? "hidden lg:flex" : "flex"}
          />
          <ChatPanel
            copy={locale}
            detail={detail.data?.conversation}
            loading={detail.isPending && Boolean(selectedId)}
            error={detail.error}
            onBack={() => setChatOpen(false)}
            onReply={openReply}
            className={chatOpen ? "flex" : "hidden lg:flex"}
          />
        </main>
      </div>
      <ComposeDialog copy={locale} open={composeOpen} defaults={composeDefaults} accountEmail={conversations.data?.accountEmail || ""} onOpenChange={setComposeOpen} onSent={() => void queryClient.invalidateQueries({ queryKey: ["conversations"] })} />
    </div>
  );
}

function Topbar({ copy, email, search, onSearch, onMenu, onCompose }: { copy: Copy; email: string; search: string; onSearch: (value: string) => void; onMenu: () => void; onCompose: () => void }) {
  return (
    <header className="relative z-20 flex h-14 shrink-0 items-center gap-3 border-b bg-card px-3 lg:px-5">
      <Button variant="ghost" size="icon" className="lg:hidden" onClick={onMenu} aria-label={copy.folders} title={copy.folders}><Menu /></Button>
      <a className="flex shrink-0 items-center gap-2 text-base font-semibold tracking-tight" href="/inbox" aria-label="Lilmail">
        <span className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground"><Mail className="size-4" /></span>
        <span className="hidden sm:inline">lilmail</span>
      </a>
      <div className="relative mx-auto flex w-full max-w-xl flex-1 items-center">
        <Search className="pointer-events-none absolute left-3 size-4 text-muted-foreground" aria-hidden="true" />
        <Input className="bg-muted/50 pl-9 pr-10" value={search} onChange={(event) => onSearch(event.target.value)} placeholder={copy.search} aria-label={copy.search} />
        {search && <Button variant="ghost" size="icon" className="absolute right-1 size-7" onClick={() => onSearch("")} aria-label={copy.cancel} title={copy.cancel}><X /></Button>}
      </div>
      <div className="hidden shrink-0 items-center gap-2 lg:flex">
        {email && <Badge variant="secondary" className="max-w-52 truncate font-normal text-muted-foreground"><span className="size-2 shrink-0 rounded-full bg-foreground/50" />{email}<ChevronDown className="size-3 shrink-0" /></Badge>}
        <div className="flex items-center rounded-lg border bg-muted p-0.5">
          <Button variant={copy === zh ? "secondary" : "ghost"} size="sm" onClick={() => switchLanguage("zh-CN")}>简体中文</Button>
          <Button variant={copy === en ? "secondary" : "ghost"} size="sm" onClick={() => switchLanguage("en")}>English</Button>
        </div>
        <Button variant="ghost" size="icon" onClick={() => window.location.assign("/settings")} aria-label={copy.settings} title={copy.settings}><Settings /></Button>
        <Button variant="link" size="sm" className="px-2 text-primary" onClick={() => { void signOut().then(() => window.location.assign("/user-login")); }}>{copy.signOut}</Button>
      </div>
      <Button variant="default" size="icon" className="lg:hidden" onClick={onCompose} aria-label={copy.compose} title={copy.compose}><Pencil /></Button>
    </header>
  );
}

function Sidebar({ copy, folders, onCompose, open, onClose, darkMode, onToggleDarkMode }: { copy: Copy; folders: Mailbox[]; onCompose: () => void; open: boolean; onClose: () => void; darkMode: boolean; onToggleDarkMode: () => void }) {
  const [foldersOpen, setFoldersOpen] = useState(true);
  const visibleFolders = folders.filter((folder) => folder.name.toLowerCase() !== "inbox" && !isSentMailbox(folder));
  const navClass = "w-full justify-start gap-2.5 px-3 text-muted-foreground";
  return (
    <aside className={cn("fixed top-14 bottom-0 left-0 z-40 flex w-60 -translate-x-full flex-col border-r bg-sidebar px-3 py-4 transition-transform lg:static lg:z-auto lg:w-[14.375rem] lg:translate-x-0", open && "translate-x-0 ring-1 ring-foreground/10")}>
      <Button className="mb-4 w-full" onClick={onCompose}><Pencil />{copy.compose}</Button>
      <nav className="flex min-h-0 flex-1 flex-col gap-1">
        <Button asChild variant="secondary" size="sm" className={cn(navClass, "bg-sidebar-accent text-sidebar-accent-foreground")}><a href="/inbox" onClick={onClose}><MessageCircle /><span>{copy.conversations}</span></a></Button>
        <Button variant="ghost" size="sm" className={cn(navClass, "mt-2 text-xs uppercase tracking-wide text-muted-foreground")} onClick={() => setFoldersOpen((value) => !value)}><ChevronRight className={cn("transition-transform", foldersOpen && "rotate-90")} /><span>{copy.folders}</span></Button>
        {foldersOpen && <div className="flex flex-col gap-1">{visibleFolders.map((folder) => <FolderLink key={folder.name} folder={folder} onClose={onClose} />)}{!visibleFolders.length && <span className="px-9 py-2 text-xs text-muted-foreground">{copy.noConversations}</span>}</div>}
        <div className="mt-auto flex items-center justify-end gap-1 border-t pt-3">
          <Button asChild variant="ghost" size="icon" aria-label={copy.settings} title={copy.settings}>
            <a href="/settings" onClick={onClose}><Settings /></a>
          </Button>
          <Button variant="ghost" size="icon" onClick={onToggleDarkMode} aria-label={darkMode ? copy.lightMode : copy.darkMode} title={darkMode ? copy.lightMode : copy.darkMode}>
            {darkMode ? <Sun /> : <Moon />}
          </Button>
        </div>
      </nav>
    </aside>
  );
}

function FolderLink({ folder, onClose }: { folder: Mailbox; onClose: () => void }) {
  const name = folder.name.toLowerCase();
  const Icon = name.includes("trash") || name.includes("deleted") ? Trash2 : name.includes("junk") || name.includes("spam") ? TriangleAlert : name.includes("draft") ? FilePenLine : name.includes("archive") ? Archive : Folder;
  return <Button asChild variant="ghost" size="sm" className="w-full justify-start gap-2.5 pl-9 text-muted-foreground"><a href={`/folder/${encodeURIComponent(folder.name)}`} onClick={onClose}><Icon /><span className="min-w-0 flex-1 truncate">{folder.name}</span>{folder.unreadCount ? <Badge variant="secondary" className="min-w-5 justify-center px-1.5 text-[10px]">{folder.unreadCount}</Badge> : null}</a></Button>;
}

function ConversationList({ copy, data, search, loading, error, selectedId, onSelect, onRefresh, className }: { copy: Copy; data?: ConversationListResponse; search: string; loading: boolean; error: Error | null; selectedId: string | null; onSelect: (id: string) => void; onRefresh: () => void; className?: string }) {
  const rows = data?.conversations || [];
  return (
    <section className={cn("min-w-0 flex-1 flex-col border-r bg-card lg:w-[23.125rem] lg:flex-none", className)}>
      <div className="flex min-h-[4.5rem] items-center justify-between border-b px-4 py-3">
        <div><h1 className="text-base font-semibold tracking-tight">{copy.conversations}</h1><p className="mt-1 text-xs text-muted-foreground">{rows.length} {copy.conversations.toLowerCase()}</p></div>
        <Button variant="ghost" size="icon" onClick={onRefresh} aria-label={copy.refresh} title={copy.refresh}><RefreshCw /></Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {loading && <ListSkeleton />}
        {!loading && error && <ErrorState copy={copy} onRetry={onRefresh} />}
        {!loading && !error && rows.length === 0 && <EmptyState icon={<MessageCircle />} text={search ? copy.noConversations : copy.noConversations} />}
        {!loading && !error && rows.map((conversation) => <ConversationRow key={conversation.id} copy={copy} conversation={conversation} selected={conversation.id === selectedId} onClick={() => onSelect(conversation.id)} />)}
      </div>
    </section>
  );
}

function ConversationRow({ copy, conversation, selected, onClick }: { copy: Copy; conversation: ConversationSummary; selected: boolean; onClick: () => void }) {
  return (
    <button className={cn("relative flex w-full items-start gap-3 border-b bg-card px-4 py-3 text-left transition-colors hover:bg-muted focus-visible:z-10 focus-visible:ring-3 focus-visible:ring-ring/50", selected && "border-l-2 border-l-foreground bg-muted pl-[0.875rem]")} onClick={onClick} type="button">
      <MailAvatar label={initials(conversation.title, conversation.peerEmail)} />
      <span className="min-w-0 flex-1">
        <span className="flex items-baseline justify-between gap-2"><strong className="min-w-0 truncate text-sm font-semibold">{conversation.title || copy.conversations}</strong><time className="shrink-0 text-[10px] text-muted-foreground">{formatMailDate(conversation.date, "zh-CN")}</time></span>
        <span className="mt-1 block truncate text-xs text-muted-foreground">{conversation.subject || copy.noSubject}</span>
        <span className="mt-1 block truncate text-xs text-muted-foreground/70">{conversation.preview || copy.noBody}</span>
      </span>
      {conversation.unreadCount > 0 && <Badge title={`${conversation.unreadCount} ${copy.unread}`} className="mt-0.5 min-w-5 justify-center px-1.5 text-[10px] leading-4">{conversation.unreadCount}</Badge>}
    </button>
  );
}

function ChatPanel({ copy, detail, loading, error, onBack, onReply, className }: { copy: Copy; detail?: ConversationDetail; loading: boolean; error: Error | null; onBack: () => void; onReply: (conversation: ConversationDetail) => void; className?: string }) {
  const panelClass = cn("min-w-0 flex-1 flex-col bg-surface", className);
  if (loading) return <section className={cn(panelClass, "items-center justify-center")}><div className="text-sm text-muted-foreground">{copy.loading}</div></section>;
  if (error) return <section className={panelClass}><ErrorState copy={copy} /></section>;
  if (!detail) return <section className={panelClass}><EmptyState icon={<MessageCircle />} text={copy.selectConversation} /></section>;
  return <ChatView copy={copy} detail={detail} onBack={onBack} onReply={() => onReply(detail)} />;
}

function ChatView({ copy, detail, onBack, onReply }: { copy: Copy; detail: ConversationDetail; onBack: () => void; onReply: () => void }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const scroll = scrollRef.current;
    if (!scroll) return;
    const jumpToLatest = () => {
      const previousBehavior = scroll.style.scrollBehavior;
      scroll.style.scrollBehavior = "auto";
      scroll.scrollTop = scroll.scrollHeight;
      scroll.style.scrollBehavior = previousBehavior;
    };
    jumpToLatest();
    const frame = window.requestAnimationFrame(jumpToLatest);
    return () => window.cancelAnimationFrame(frame);
  }, [detail.id, detail.messages.length]);

  return (
    <section className="flex min-w-0 flex-1 flex-col">
      <header className="flex min-h-[4.5rem] items-center gap-3 border-b bg-card px-3 py-3 sm:px-5">
        <Button variant="ghost" size="icon" className="lg:hidden" onClick={onBack} aria-label={copy.cancel} title={copy.cancel}><ArrowLeft /></Button>
        <MailAvatar label={initials(detail.title, detail.peerEmail)} />
        <div className="min-w-0 flex-1"><h2 className="truncate text-sm font-semibold">{detail.title || copy.conversations}</h2><p className="mt-1 truncate text-xs text-muted-foreground">{detail.subject || copy.noSubject}<span className="px-1.5">·</span>{detail.count} {copy.messages}</p></div>
        <div className="flex items-center gap-1"><Button variant="ghost" size="icon" onClick={onReply} aria-label={copy.reply} title={copy.reply}><Send /></Button><Button variant="ghost" size="icon" className="hidden sm:inline-flex" aria-label="More" title="More"><MoreHorizontal /></Button></div>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto scroll-smooth px-3 py-6 sm:px-[5vw] sm:py-8" ref={scrollRef}>
        {detail.messages.map((message) => <MessageBubble key={`${message.folder || "inbox"}-${message.id}`} copy={copy} message={message} accountEmail={detail.accountEmail} />)}
      </div>
      <div className="hidden items-center justify-between border-t bg-card px-5 py-2 text-xs text-muted-foreground lg:flex"><span>{copy.reply}</span><Button variant="secondary" size="sm" onClick={onReply}><Send />{copy.reply}</Button></div>
    </section>
  );
}

function MessageBubble({ copy, message, accountEmail }: { copy: Copy; message: ConversationMessage; accountEmail?: string }) {
  const sender = message.outgoing ? copy.me : message.fromName || message.from;
  const parts = linkifyText(message.body || message.preview || copy.noBody);
  const outgoing = message.outgoing;
  return (
    <article className="mx-auto mb-5 max-w-3xl">
      <div className={cn("mb-1 px-3 text-xs leading-tight font-medium text-muted-foreground", outgoing ? "text-right" : "")}>{sender}</div>
      <div className={cn("flex items-end gap-2", outgoing && "justify-end")}>
        {!outgoing && <MailAvatar small label={initials(message.fromName, message.from)} />}
        <div className={cn("max-w-[80%] overflow-hidden rounded-xl border px-3 py-2 text-sm leading-relaxed sm:max-w-[70ch]", outgoing ? "border-transparent bg-secondary text-secondary-foreground" : "border-border bg-background text-foreground")}>
          <div className="[overflow-wrap:anywhere] whitespace-pre-wrap">{parts.map((part, index) => typeof part === "string" ? <span key={index}>{part}</span> : <a className="text-foreground underline underline-offset-3" key={index} href={part.href} target={part.href.startsWith("mailto:") ? undefined : "_blank"} rel="noreferrer">{part.value}</a>)}</div>
          {message.hasAttachments && message.attachments?.length ? <><Separator className="my-2 opacity-50" /><div className="grid gap-1.5">{message.attachments.map((attachment) => <a className="flex min-w-0 items-center gap-1.5 text-xs text-primary" key={attachment.id} href={`/api/attachment/${encodeURIComponent(attachment.id)}?account_email=${encodeURIComponent(accountEmail || "")}`}><Paperclip className="size-3.5 shrink-0" /><span className="min-w-0 truncate">{attachment.filename}</span><small className="shrink-0 text-muted-foreground">{formatSize(attachment.size)}</small></a>)}</div></> : null}
        </div>
        {outgoing && <MailAvatar small label={initials("", accountEmail)} />}
      </div>
      <time className={cn("mt-1 block px-3 text-xs text-muted-foreground", outgoing ? "text-right" : "")} title={formatFullDate(message.date, "zh-CN")}>{formatFullDate(message.date, "zh-CN")}</time>
    </article>
  );
}

function ComposeDialog({ copy, open, defaults, accountEmail, onOpenChange, onSent }: { copy: Copy; open: boolean; defaults: { to: string; subject: string }; accountEmail: string; onOpenChange: (value: boolean) => void; onSent: () => void }) {
  const [to, setTo] = useState("");
  const [subject, setSubject] = useState("");
  const [error, setError] = useState("");
  const editor = useEditor({ extensions: [StarterKit, UnderlineExtension, LinkExtension.configure({ openOnClick: false }), Placeholder.configure({ placeholder: copy.writeMessage })], content: "", immediatelyRender: false });
  const mutation = useMutation({ mutationFn: sendMessage });

  useEffect(() => {
    if (!open || !editor) return;
    setTo(defaults.to);
    setSubject(defaults.subject);
    setError("");
    editor.commands.clearContent(true);
    editor.commands.focus("start");
  }, [open, defaults, editor]);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const plainBody = editor?.getText().trim() || "";
    const htmlBody = editor?.getHTML() || "";
    if (!to.trim() || !plainBody) {
      setError(copy.noBody);
      return;
    }
    const form = new FormData();
    form.set("to", to.trim());
    form.set("subject", subject.trim());
    form.set("body", plainBody);
    form.set("html_body", htmlBody);
    if (accountEmail) form.set("account_email", accountEmail);
    mutation.mutate(form, { onSuccess: () => { onSent(); onOpenChange(false); }, onError: (value) => setError(value instanceof Error ? value.message : copy.loginFailed) });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[calc(100vh-2rem)] w-[calc(100%-2rem)] max-w-2xl flex-col gap-0 overflow-hidden p-0 sm:max-h-[calc(100vh-3rem)]">
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
          <DialogHeader className="border-b px-5 py-4 pr-12 text-left"><DialogTitle className="truncate text-base">{subject || copy.writeMessage}</DialogTitle><DialogDescription className="sr-only">{copy.compose}</DialogDescription></DialogHeader>
          <div className="grid gap-3 border-b px-5 py-4"><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="compose-to">{copy.to}<Input id="compose-to" value={to} onChange={(event) => setTo(event.target.value)} placeholder="name@example.com" autoFocus /></Label><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="compose-subject">{copy.subject}<Input id="compose-subject" value={subject} onChange={(event) => setSubject(event.target.value)} placeholder={copy.noSubject} /></Label></div>
          <div className="flex items-center gap-1 border-b bg-muted px-4 py-1"><Button type="button" variant="ghost" size="icon" onClick={() => editor?.chain().focus().toggleBold().run()} aria-label="Bold" title="Bold"><Bold /></Button><Button type="button" variant="ghost" size="icon" onClick={() => editor?.chain().focus().toggleItalic().run()} aria-label="Italic" title="Italic"><Italic /></Button><Button type="button" variant="ghost" size="icon" onClick={() => editor?.chain().focus().toggleUnderline().run()} aria-label="Underline" title="Underline"><Underline /></Button><Separator orientation="vertical" className="mx-1 h-5" /><Button type="button" variant="ghost" size="icon" onClick={() => editor?.chain().focus().toggleBulletList().run()} aria-label="Bullet list" title="Bullet list"><List /></Button><Button type="button" variant="ghost" size="icon" onClick={() => editor?.chain().focus().toggleOrderedList().run()} aria-label="Numbered list" title="Numbered list"><ListOrdered /></Button><Button type="button" variant="ghost" size="icon" onClick={() => { const href = window.prompt("URL"); if (href) editor?.chain().focus().setLink({ href }).run(); }} aria-label="Link" title="Link"><Link /></Button></div>
          <EditorContent editor={editor} className="min-h-[18rem] flex-1 overflow-y-auto px-5 py-4" />
          {error && <p className="px-5 pb-2 text-xs text-destructive">{error}</p>}
          <DialogFooter className="flex-row items-center justify-between border-t px-5 py-3 sm:flex-row sm:justify-between"><Button type="button" variant="ghost" size="sm"><Paperclip />{copy.attach}</Button><div className="flex gap-2"><Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>{copy.cancel}</Button><Button type="submit" disabled={mutation.isPending}><Send />{mutation.isPending ? copy.sending : copy.send}</Button></div></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function LoginScreen({ copy }: { copy: Copy }) {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const mutation = useMutation({ mutationFn: () => signIn(login, password), onSuccess: () => window.location.reload(), onError: (value) => setError(value instanceof Error ? value.message : copy.loginFailed) });
  return <main className="grid min-h-screen place-items-center bg-background p-6"><div className="grid w-full max-w-sm gap-4 rounded-xl border bg-card p-6 ring-1 ring-foreground/5"><div className="flex items-center gap-2 text-lg font-semibold"><span className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground"><Mail className="size-4" /></span><strong>lilmail</strong></div><h1 className="mt-2 text-2xl font-semibold tracking-tight">{copy.login}</h1><p className="-mt-2 text-sm text-muted-foreground">{copy.appAccount}</p><form className="grid gap-4" onSubmit={(event) => { event.preventDefault(); mutation.mutate(); }}><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="login-account">{copy.appAccount}<Input id="login-account" value={login} onChange={(event) => setLogin(event.target.value)} autoComplete="username" required /></Label><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="login-password">{copy.password}<Input id="login-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" required /></Label>{error && <p className="text-xs text-destructive">{error}</p>}<Button type="submit" className="w-full" disabled={mutation.isPending}>{mutation.isPending ? copy.loading : copy.login}</Button></form><Button asChild variant="link" size="sm"><a href="/register">Create an application account</a></Button></div></main>;
}

function ListSkeleton() {
  return <div className="grid">{Array.from({ length: 7 }, (_, index) => <div className="flex gap-3 border-b px-4 py-3" key={index}><Skeleton className="size-9 shrink-0 rounded-full" /><div className="grid flex-1 gap-2 pt-1"><Skeleton className="h-2.5 w-2/5" /><Skeleton className="h-2.5 w-3/5" /><Skeleton className="h-2.5 w-4/5" /></div></div>)}</div>;
}

function ErrorState({ copy, onRetry }: { copy: Copy; onRetry?: () => void }) {
  return <div className="flex min-h-56 flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground"><TriangleAlert className="size-8 text-primary" /><strong className="text-sm font-medium">{copy.loadFailed}</strong>{onRetry && <Button variant="secondary" size="sm" onClick={onRetry}>{copy.retry}</Button>}</div>;
}

function EmptyState({ icon, text }: { icon: ReactNode; text: string }) {
  return <div className="flex min-h-56 flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">{icon}<p className="m-0 text-sm">{text}</p></div>;
}

export default App;
