import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type MutableRefObject, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Menu as MenuPrimitive } from "@base-ui/react/menu";
import { ContextMenu as ContextMenuPrimitive } from "@base-ui/react/context-menu";
import {
  Archive,
  ArrowLeft,
  CalendarDays,
  Check,
  ChevronDown,
  ChevronRight,
  Code2,
  FilePenLine,
  Folder,
  Italic,
  Link,
  Languages,
  List,
  ListOrdered,
  Mail,
  Menu,
  MessageCircle,
  Bell,
  BellOff,
  Moon,
  MoreHorizontal,
  Paperclip,
  Pencil,
  Plus,
  Search,
  RotateCcw,
  Send,
  Signature as SignatureIcon,
  Sparkles,
  Settings,
  ShieldCheck,
  Sun,
  Trash2,
  TriangleAlert,
  Underline,
  X,
  Bold,
  Bot,
} from "lucide-react";
import type { Editor } from "@tiptap/core";
import { EditorContent, useEditor } from "@tiptap/react";
import ImageExtension from "@tiptap/extension-image";
import LinkExtension from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import StarterKit from "@tiptap/starter-kit";
import UnderlineExtension from "@tiptap/extension-underline";
import { toast } from "sonner";
import { EmailSignature as EmailSignatureExtension } from "./extensions/email-signature";
import { ReplyQuote } from "./extensions/reply-quote";
import { ApiError, addAccount, addAIAgent, addAIModel, createCalendarEvent, deleteAccount, deleteAIModel, deleteConversation, deleteConversationMessage, getAccounts, getAIAgents, getAIModels, getCalendarEvents, getCapabilities, getConversation, getConversations, getFeishuWebhookSettings, getFolderMessages, getMessage, getSignatures, markConversationRead, markConversationUnread, permanentlyDeleteJunkMessage, register, restoreJunkMessage, saveConversationNote, saveFeishuWebhookSettings, saveSignatures, sendMessage, setDefaultAIModel, signIn, signOut, summarizeMailMessage, summarizeMailThread, switchAccount, switchLanguage, testAIModel, testFeishuWebhook, testSavedAIModel, updateAIAgent, updateAIModel, type AIAgent, type AIModel, type EmailSignature } from "./lib/api";
import { currentPushSubscription, disableWebPush, enableWebPush, supportsWebPush } from "./lib/push";
import { cn, formatSize, formatTime, isSentMailbox, linkifyText, splitQuotedText } from "./lib/utils";
import { Badge } from "./components/ui/badge";
import { Button } from "./components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "./components/ui/dialog";
import { Input } from "./components/ui/input";
import { Label } from "./components/ui/label";
import { Separator } from "./components/ui/separator";
import { ScrollArea } from "./components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./components/ui/select";
import { Skeleton } from "./components/ui/skeleton";
import { Textarea } from "./components/ui/textarea";
import type { CalendarEvent, ConnectedAccount, ConversationDetail, ConversationDetailResponse, ConversationMessage, ConversationSummary, ConversationListResponse, MailMessage, Mailbox, MailSummary } from "./types";

const EmailImageExtension = ImageExtension.configure({ inline: true, allowBase64: false });

const zh = {
  conversations: "对话",
  compose: "写邮件",
  search: "搜索邮件",
  folders: "文件夹",
  inboxFolder: "收件箱",
  sentFolder: "已发送",
  draftsFolder: "草稿",
  trashFolder: "已删除",
  junkFolder: "垃圾邮件",
  archiveFolder: "归档",
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
  back: "返回列表",
  showQuoted: "显示引用内容",
  quotedOnly: "邮件正文仅包含引用内容",
  resyncAttachments: "重新同步附件",
  resyncQueued: "附件重新同步已加入队列",
  addAccount: "添加账户",
  remove: "移除",
  email: "邮箱地址",
  displayName: "显示名称",
  createAccount: "创建账户",
  calendar: "日历",
  newEvent: "新建日程",
  location: "地点",
  start: "开始",
  end: "结束",
  save: "保存",
  pushNotifications: "推送通知",
  enablePush: "启用推送通知",
  disablePush: "停用推送通知",
  pushUnavailable: "当前浏览器或服务器未启用推送通知",
  feishuWebhook: "飞书 Webhook",
  feishuWebhookDescription: "刷新到新的收件箱邮件时，通过飞书自定义机器人发送通知。",
  feishuWebhookURL: "Webhook URL",
  feishuWebhookEnabled: "启用飞书通知",
  feishuWebhookSaved: "飞书 Webhook 设置已保存",
  feishuWebhookURLRequired: "启用飞书通知前请先填写 Webhook URL",
  feishuWebhookTest: "发送测试",
  feishuWebhookTesting: "发送中…",
  feishuWebhookTestSent: "飞书测试消息已发送",
  feishuWebhookUnavailable: "飞书 Webhook 仅在本地邮件同步启用时可用",
  aiSettings: "模型管理",
  aiSettingsDescription: "管理用于邮件总结的 OpenAI 模型，API Key 会加密保存。",
  addAIModel: "新增模型",
  addingAIModel: "正在添加…",
  editAIModel: "编辑模型",
  updatingAIModel: "正在更新…",
  aiModelUpdated: "模型已更新",
  aiAPIKeyKeep: "留空则保留当前 API Key",
  noAIModels: "暂无模型配置",
  defaultModel: "默认",
  setDefaultModel: "设为默认",
  aiProvider: "提供商",
  aiBaseURL: "Base URL",
  aiModel: "模型",
  aiAPIKey: "API Key",
  aiReasoningEffort: "思考等级",
  aiReasoningLow: "低",
  aiReasoningMedium: "中",
  aiModelTest: "测试模型",
  aiModelTesting: "测试中…",
  aiModelTestSuccess: "模型测试成功",
  aiSettingsSaved: "模型已添加",
  summarize: "AI 总结",
  summarizing: "正在总结…",
  summaryTitle: "AI 总结",
  mailSummaryTitle: "邮件总结",
  regenerateSummary: "重新总结",
  summaryStale: "配置已更新",
  agentSettings: "智能体",
  agentSettingsDescription: "管理智能体的纯文本提示词。",
  addAgent: "新增智能体",
  editAgent: "编辑智能体",
  agentName: "名称",
  agentPrompt: "提示词",
  noAgents: "暂无智能体",
  agentSaved: "智能体已新增",
  agentUpdated: "智能体已更新",
  savingAgent: "正在保存…",
  signatureSettings: "邮件签名",
  signatureSettingsDescription: "管理写邮件和回复时可使用的富文本签名。",
  addSignature: "新建签名",
  editSignature: "编辑签名",
  signatureName: "名称",
  signatureContent: "签名内容",
  signaturePreview: "预览",
  defaultSignature: "默认签名",
  noSignatures: "暂无邮件签名",
  noSignature: "不使用签名",
  signatureSaved: "签名已创建",
  signatureUpdated: "签名已更新",
  signatureDeleted: "签名已删除",
  deleteSignatureTitle: "删除这个签名？",
  deleteSignatureDescription: "删除后，新邮件和回复将不再使用这个签名。",
  savingSignature: "正在保存…",
  connectedAccounts: "已连接账户",
  color: "颜色",
  language: "语言",
  generalSettings: "通用设置",
  mailboxManagement: "邮箱管理",
  mailboxDescription: "管理已连接的 IMAP / SMTP 邮箱账户",
  account: "账户",
  imapServer: "IMAP 服务器",
  imapPort: "IMAP 端口",
  smtpServer: "SMTP 服务器",
  smtpPort: "SMTP 端口",
  server: "服务器",
  actions: "操作",
  noAccounts: "暂无已连接的邮箱账户",
  optional: "可选",
  adding: "正在添加…",
  addNote: "添加备注",
  noteSaveFailed: "备注保存失败",
  markUnread: "标为未读",
  markUnreadFailed: "标记未读失败",
  deleteConversation: "删除对话",
  deleteConversationTitle: "将整个对话移到已删除？",
  deleteConversationDescription: "该对话中的收件和已发送邮件都会移到邮箱服务器的已删除文件夹。",
  deleteConversationFailed: "删除对话失败",
  deleteEmail: "删除邮件",
  deleteEmailTitle: "将邮件移到已删除？",
  deleteEmailDescription: "这封邮件会移到邮箱服务器的已删除文件夹，可在 QQ 邮箱中恢复。",
  deleting: "正在删除…",
  deleteEmailFailed: "删除邮件失败",
  notSpam: "不是垃圾邮件",
  notSpamFailed: "移回收件箱失败",
  permanentDelete: "彻底删除",
  permanentDeleteTitle: "彻底删除这封邮件？",
  permanentDeleteDescription: "邮件将从邮箱服务器上永久删除，且无法恢复。",
  permanentDeleteFailed: "彻底删除失败",
  sourceCode: "显示源代码",
  richText: "返回富文本",
  removeAttachment: "移除附件",
  attachmentsTooLarge: "附件总大小不能超过 18 MB",
  invalidRecipient: "请输入有效的邮箱地址",
  removeRecipient: "删除收件人",
  sendEmail: "发送邮件",
};

const en = {
  conversations: "Conversations",
  compose: "Compose",
  search: "Search mail",
  folders: "Folders",
  inboxFolder: "Inbox",
  sentFolder: "Sent",
  draftsFolder: "Drafts",
  trashFolder: "Trash",
  junkFolder: "Junk",
  archiveFolder: "Archive",
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
  back: "Back to list",
  showQuoted: "Show quoted content",
  quotedOnly: "This email only contains quoted content",
  resyncAttachments: "Resync attachments",
  resyncQueued: "Attachment resync queued",
  addAccount: "Add account",
  remove: "Remove",
  email: "Email address",
  displayName: "Display name",
  createAccount: "Create account",
  calendar: "Calendar",
  newEvent: "New event",
  location: "Location",
  start: "Start",
  end: "End",
  save: "Save",
  pushNotifications: "Push notifications",
  enablePush: "Enable push notifications",
  disablePush: "Disable push notifications",
  pushUnavailable: "Push notifications are unavailable in this browser or server",
  feishuWebhook: "Feishu webhook",
  feishuWebhookDescription: "Send a custom bot notification when refresh finds new inbox mail.",
  feishuWebhookURL: "Webhook URL",
  feishuWebhookEnabled: "Enable Feishu notifications",
  feishuWebhookSaved: "Feishu webhook settings saved",
  feishuWebhookURLRequired: "Enter a webhook URL before enabling Feishu notifications",
  feishuWebhookTest: "Send test",
  feishuWebhookTesting: "Sending…",
  feishuWebhookTestSent: "Feishu test message sent",
  feishuWebhookUnavailable: "Feishu webhooks require local mail sync",
  aiSettings: "Model management",
  aiSettingsDescription: "Manage OpenAI models used for mail summaries. API keys are stored encrypted.",
  addAIModel: "Add model",
  addingAIModel: "Adding…",
  editAIModel: "Edit model",
  updatingAIModel: "Updating…",
  aiModelUpdated: "Model updated",
  aiAPIKeyKeep: "Leave blank to keep the current API key",
  noAIModels: "No models configured",
  defaultModel: "Default",
  setDefaultModel: "Set as default",
  aiProvider: "Provider",
  aiBaseURL: "Base URL",
  aiModel: "Model",
  aiAPIKey: "API Key",
  aiReasoningEffort: "Reasoning effort",
  aiReasoningLow: "Low",
  aiReasoningMedium: "Medium",
  aiModelTest: "Test model",
  aiModelTesting: "Testing…",
  aiModelTestSuccess: "Model test succeeded",
  aiSettingsSaved: "Model added",
  summarize: "AI summary",
  summarizing: "Summarizing…",
  summaryTitle: "AI summary",
  mailSummaryTitle: "Mail summary",
  regenerateSummary: "Regenerate summary",
  summaryStale: "Configuration changed",
  agentSettings: "Agents",
  agentSettingsDescription: "Manage plain-text prompts for agents.",
  addAgent: "Add agent",
  editAgent: "Edit agent",
  agentName: "Name",
  agentPrompt: "Prompt",
  noAgents: "No agents configured",
  agentSaved: "Agent added",
  agentUpdated: "Agent updated",
  savingAgent: "Saving…",
  signatureSettings: "Email signatures",
  signatureSettingsDescription: "Manage rich-text signatures used for new messages and replies.",
  addSignature: "New signature",
  editSignature: "Edit signature",
  signatureName: "Name",
  signatureContent: "Signature content",
  signaturePreview: "Preview",
  defaultSignature: "Default signature",
  noSignatures: "No email signatures",
  noSignature: "No signature",
  signatureSaved: "Signature created",
  signatureUpdated: "Signature updated",
  signatureDeleted: "Signature deleted",
  deleteSignatureTitle: "Delete this signature?",
  deleteSignatureDescription: "New messages and replies will no longer use this signature.",
  savingSignature: "Saving…",
  connectedAccounts: "Connected accounts",
  color: "Color",
  language: "Language",
  generalSettings: "General",
  mailboxManagement: "Mailboxes",
  mailboxDescription: "Manage connected IMAP / SMTP mail accounts",
  account: "Account",
  imapServer: "IMAP server",
  imapPort: "IMAP port",
  smtpServer: "SMTP server",
  smtpPort: "SMTP port",
  server: "Server",
  actions: "Actions",
  noAccounts: "No connected mail accounts",
  optional: "Optional",
  adding: "Adding…",
  addNote: "Add note",
  noteSaveFailed: "Could not save note",
  markUnread: "Mark as unread",
  markUnreadFailed: "Could not mark conversation as unread",
  deleteConversation: "Delete conversation",
  deleteConversationTitle: "Move the entire conversation to Trash?",
  deleteConversationDescription: "Received and sent emails in this conversation will be moved to the mail server's Trash folder.",
  deleteConversationFailed: "Could not delete conversation",
  deleteEmail: "Delete email",
  deleteEmailTitle: "Move email to Trash?",
  deleteEmailDescription: "This email will be moved to the mail server's Trash folder and can be restored from QQ Mail.",
  deleting: "Deleting…",
  deleteEmailFailed: "Could not delete email",
  notSpam: "Not spam",
  notSpamFailed: "Could not move the email to Inbox",
  permanentDelete: "Delete permanently",
  permanentDeleteTitle: "Permanently delete this email?",
  permanentDeleteDescription: "This email will be permanently removed from the mail server and cannot be recovered.",
  permanentDeleteFailed: "Could not permanently delete email",
  sourceCode: "Show source",
  richText: "Back to rich text",
  removeAttachment: "Remove attachment",
  attachmentsTooLarge: "Attachments cannot exceed 18 MB in total",
  invalidRecipient: "Enter a valid email address",
  removeRecipient: "Remove recipient",
  sendEmail: "Send email",
};

type Copy = typeof zh;

type ComposeDefaults = {
  to: string;
  subject: string;
  html?: string;
  inReplyTo?: string;
  references?: string[];
};

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

function conversationIdFromURL() {
  if (typeof window === "undefined") return null;
  return new URL(window.location.href).searchParams.get("conversation");
}

function setConversationURL(id: string | null, mode: "push" | "replace" = "push") {
  const url = new URL(window.location.href);
  if (id) url.searchParams.set("conversation", id);
  else url.searchParams.delete("conversation");
  window.history[mode === "push" ? "pushState" : "replaceState"](window.history.state, "", url);
}

function App() {
  const [path, setPath] = useState(() => window.location.pathname);
  useEffect(() => {
    const syncPath = () => setPath(window.location.pathname);
    const navigateWithinMail = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const target = event.target;
      const anchor = target instanceof Element ? target.closest("a") : null;
      if (!anchor || anchor.target || anchor.hasAttribute("download")) return;
      const url = new URL(anchor.href, window.location.href);
      const isMailRoute = url.pathname === "/inbox" ||
        url.pathname === "/calendar" ||
        url.pathname === "/calendar/week" ||
        url.pathname.startsWith("/folder/");
      if (url.origin !== window.location.origin || !isMailRoute) return;
      event.preventDefault();
      if (url.href !== window.location.href) window.history.pushState(window.history.state, "", url);
      syncPath();
    };
    window.addEventListener("popstate", syncPath);
    document.addEventListener("click", navigateWithinMail);
    return () => {
      window.removeEventListener("popstate", syncPath);
      document.removeEventListener("click", navigateWithinMail);
    };
  }, []);
  if (path === "/login" || path === "/user-login") return <LoginScreen copy={zh} />;
  if (path === "/register") return <RegisterScreen copy={zh} />;
  if (path === "/settings") return <SettingsPage copy={zh} />;
  if (path === "/calendar" || path === "/calendar/week") return <CalendarPage />;
  if (path.startsWith("/folder/")) return <FolderPage key={path} folder={decodeURIComponent(path.slice("/folder/".length))} />;
  return <InboxPage />;
}

function InboxPage() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(conversationIdFromURL);
  const [chatOpen, setChatOpen] = useState(() => Boolean(conversationIdFromURL()));
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [composeDefaults, setComposeDefaults] = useState<ComposeDefaults>({ to: "", subject: "" });
  const [darkMode, setDarkMode] = useState(prefersDarkMode);
  const [deleteTarget, setDeleteTarget] = useState<ConversationSummary | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const autoReadRef = useRef(new Set<string>());
  const manuallyUnreadRef = useRef(new Set<string>());
  const debouncedSearch = useDebouncedValue(search, 250);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", darkMode);
    window.localStorage.setItem("lilmail-theme", darkMode ? "dark" : "light");
  }, [darkMode]);

  useEffect(() => {
    const restoreConversationFromURL = () => {
      const id = conversationIdFromURL();
      setSelectedId(id);
      setChatOpen(Boolean(id) || window.innerWidth > 1023);
    };
    window.addEventListener("popstate", restoreConversationFromURL);
    return () => window.removeEventListener("popstate", restoreConversationFromURL);
  }, []);

  const conversations = useQuery({
    queryKey: ["conversations", debouncedSearch],
    queryFn: () => getConversations(debouncedSearch),
    refetchInterval: 30_000,
  });
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities });

  useEffect(() => {
    if (!capabilities.data?.notifications) return;
    const events = new EventSource("/events", { withCredentials: true });
    events.onmessage = (event) => {
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
      if (!("Notification" in window) || Notification.permission !== "granted" || document.visibilityState === "visible") return;
      try {
        const payload = JSON.parse(String(event.data)) as { from?: string; subject?: string };
        new Notification(payload.from ? `New mail from ${payload.from}` : "New mail", { body: payload.subject || "" });
      } catch {
        // A malformed optional notification must not interrupt inbox refreshes.
      }
    };
    return () => events.close();
  }, [capabilities.data?.notifications, queryClient]);
  const locale = useLocale(conversations.data?.locale);
  const detail = useQuery({
    queryKey: ["conversation", selectedId],
    queryFn: () => getConversation(selectedId!),
    enabled: Boolean(selectedId),
  });
  const deleteMutation = useMutation({
    mutationFn: (conversation: ConversationSummary) => deleteConversation(conversation.id),
    onSuccess: async (_, conversation) => {
      setDeleteTarget(null);
      setDeleteError("");
      if (selectedId === conversation.id) {
        setSelectedId(null);
        setChatOpen(false);
        setConversationURL(null, "replace");
      }
      await queryClient.invalidateQueries({ queryKey: ["conversations"] });
      queryClient.removeQueries({ queryKey: ["conversation", conversation.id] });
    },
    onError: (value) => setDeleteError(value instanceof Error ? value.message : locale.deleteConversationFailed),
  });

  useEffect(() => {
    const conversation = detail.data?.conversation;
    if (!conversation || manuallyUnreadRef.current.has(conversation.id)) return;
    const unread = conversation.messages.filter((message) => !message.outgoing && !message.flags?.some((flag) => flag.toLowerCase() === "\\seen"));
    if (unread.length === 0) return;
    const fingerprint = `${conversation.id}:${unread.map((message) => `${message.folder || "INBOX"}/${message.id}`).join(",")}`;
    if (autoReadRef.current.has(fingerprint)) return;
    autoReadRef.current.add(fingerprint);
    void markConversationRead(conversation.id).then(async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["conversations"] }),
        queryClient.invalidateQueries({ queryKey: ["conversation", conversation.id] }),
      ]);
    }).catch(() => {
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
    });
  }, [detail.data?.conversation, queryClient]);

  const markUnread = async (conversation: ConversationSummary) => {
    manuallyUnreadRef.current.add(conversation.id);
    try {
      await markConversationUnread(conversation.id);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["conversations"] }),
        queryClient.invalidateQueries({ queryKey: ["conversation", conversation.id] }),
      ]);
    } catch (value) {
      manuallyUnreadRef.current.delete(conversation.id);
      toast.error(value instanceof Error ? value.message : locale.markUnreadFailed);
    }
  };

  useEffect(() => {
    if (!conversations.data) return;
    const items = conversations.data?.conversations || [];
    if (selectedId && !items.some((item) => item.id === selectedId)) {
      setSelectedId(null);
      setChatOpen(false);
      if (conversationIdFromURL() === selectedId) setConversationURL(null, "replace");
      return;
    }
    if (!selectedId && items.length > 0 && window.innerWidth > 1023) {
      setSelectedId(items[0].id);
      setChatOpen(true);
    }
  }, [conversations.data, selectedId]);

  const openCompose = (defaults: ComposeDefaults = { to: "", subject: "" }) => {
    setComposeDefaults(defaults);
    setComposeOpen(true);
    setSidebarOpen(false);
  };

  const openReply = (conversation: ConversationDetail, message?: ConversationMessage) => {
    const source = message || conversation.messages.at(-1);
    const subject = source?.subject || conversation.subject;
    const replySubject = subject.toLowerCase().startsWith("re:") ? subject : `Re: ${subject}`;
    const recipient = source?.outgoing ? source.to : source?.from || conversation.peerEmail || "";
    const sender = source?.fromName && source.from ? `${source.fromName} <${source.from}>` : source?.from || conversation.peerEmail || "";
    const quoteLead = `On ${new Date(source?.date || Date.now()).toLocaleString(locale === en ? "en" : "zh-CN")}, ${sender} wrote:`;
    const originalBody = source?.body || source?.preview || "";
    const quotedHTML = structuredQuotedTextToHTML(originalBody);
    const references = [...(source?.references || [])];
    if (source?.messageId && !references.includes(source.messageId)) references.push(source.messageId);
    openCompose({
      to: recipient,
      subject: replySubject,
      html: `<p><br></p><p>${escapeHTML(quoteLead)}</p><blockquote>${quotedHTML}</blockquote>`,
      inReplyTo: source?.messageId,
      references,
    });
  };

  const openNewMailForMessage = (conversation: ConversationDetail, message: ConversationMessage) => {
    const recipient = message.outgoing ? message.to : message.from || conversation.peerEmail || "";
    openCompose({ to: recipient, subject: "" });
  };

  const authenticated = conversations.error instanceof ApiError && conversations.error.status === 401;
  if (authenticated) return <LoginScreen copy={locale} />;

  return (
    <div className="flex h-screen min-h-[32.5rem] overflow-hidden bg-background">
        {sidebarOpen && <button className="fixed inset-0 z-30 bg-black/10 supports-backdrop-filter:backdrop-blur-xs lg:hidden" aria-label={locale.cancel} onClick={() => setSidebarOpen(false)} />}
        <Sidebar copy={locale} folders={conversations.data?.folders || []} accounts={conversations.data?.accounts || []} accountEmail={conversations.data?.accountEmail || ""} calendarEnabled={capabilities.data?.calendar === true} onCompose={() => openCompose()} onSettings={() => { setSidebarOpen(false); setSettingsOpen(true); }} open={sidebarOpen} onClose={() => setSidebarOpen(false)} darkMode={darkMode} onToggleDarkMode={() => setDarkMode((value) => !value)} />
        <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
          <ConversationList
            copy={locale}
            data={conversations.data}
            search={search}
            onSearch={setSearch}
            onMenu={() => setSidebarOpen(true)}
            loading={conversations.isPending}
            error={conversations.error}
            selectedId={selectedId}
            onSelect={(id) => {
              manuallyUnreadRef.current.delete(id);
              setSelectedId(id);
              setChatOpen(true);
              setSidebarOpen(false);
              if (conversationIdFromURL() !== id) setConversationURL(id);
            }}
            onMarkUnread={(conversation) => void markUnread(conversation)}
            onDelete={(conversation) => { setDeleteError(""); setDeleteTarget(conversation); }}
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
            onNewMail={openNewMailForMessage}
            onConversationEmpty={() => {
              setSelectedId(null);
              setChatOpen(false);
              setConversationURL(null, "replace");
            }}
            className={chatOpen ? "flex" : "hidden lg:flex"}
          />
        </main>
      <ComposeDialog copy={locale} open={composeOpen} defaults={composeDefaults} accountEmail={conversations.data?.accountEmail || ""} onOpenChange={setComposeOpen} onSent={() => {
        void queryClient.invalidateQueries({ queryKey: ["conversations"] });
        void queryClient.invalidateQueries({ queryKey: ["conversation"] });
      }} />
      <SettingsDialog copy={locale} open={settingsOpen} onOpenChange={setSettingsOpen} />
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open && !deleteMutation.isPending) { setDeleteTarget(null); setDeleteError(""); } }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{locale.deleteConversationTitle}</DialogTitle>
            <DialogDescription>{locale.deleteConversationDescription}</DialogDescription>
          </DialogHeader>
          {deleteError && <p className="text-sm text-destructive">{deleteError}</p>}
          <DialogFooter>
            <Button variant="ghost" disabled={deleteMutation.isPending} onClick={() => setDeleteTarget(null)}>{locale.cancel}</Button>
            <Button variant="destructive" disabled={deleteMutation.isPending || !deleteTarget} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}><Trash2 />{deleteMutation.isPending ? locale.deleting : locale.deleteConversation}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Sidebar({ copy, folders, accounts, accountEmail, calendarEnabled, currentFolder, currentView, onCompose, onSettings, open, onClose, darkMode, onToggleDarkMode }: { copy: Copy; folders: Mailbox[]; accounts: ConversationListResponse["accounts"]; accountEmail: string; calendarEnabled: boolean; currentFolder?: string; currentView?: "mail" | "calendar"; onCompose: () => void; onSettings: () => void; open: boolean; onClose: () => void; darkMode: boolean; onToggleDarkMode: () => void }) {
  const [foldersOpen, setFoldersOpen] = useState(true);
  const visibleFolders = folders.filter((folder) => folder.name.toLowerCase() !== "inbox" && !isSentMailbox(folder));
  const navClass = "w-full justify-start gap-2.5 px-3 text-muted-foreground";
  return (
    <aside className={cn("fixed inset-y-0 left-0 z-40 flex w-60 -translate-x-full flex-col border-r bg-sidebar px-3 py-4 transition-transform lg:static lg:z-auto lg:w-[14.375rem] lg:translate-x-0", open && "translate-x-0 ring-1 ring-foreground/10")}>
      <Button data-testid="compose-button" className="mb-4 w-full" onClick={onCompose}><Pencil />{copy.compose}</Button>
      <nav className="flex min-h-0 flex-1 flex-col gap-1">
        <Button asChild variant={!currentFolder && currentView !== "calendar" ? "secondary" : "ghost"} size="sm" className={cn(navClass, !currentFolder && currentView !== "calendar" && "bg-sidebar-accent text-sidebar-accent-foreground")}><a href="/inbox" onClick={onClose}><MessageCircle /><span>{copy.conversations}</span></a></Button>
        {calendarEnabled && <Button asChild variant={currentView === "calendar" ? "secondary" : "ghost"} size="sm" className={cn(navClass, currentView === "calendar" && "bg-sidebar-accent text-sidebar-accent-foreground")}><a href="/calendar" onClick={onClose}><CalendarDays /><span>{copy.calendar}</span></a></Button>}
        <Button variant="ghost" size="sm" className={cn(navClass, "mt-2 text-xs uppercase tracking-wide text-muted-foreground")} onClick={() => setFoldersOpen((value) => !value)}><ChevronRight className={cn("transition-transform", foldersOpen && "rotate-90")} /><span>{copy.folders}</span></Button>
        {foldersOpen && <ScrollArea className="min-h-0 flex-1" contentClassName="flex flex-col gap-1">{visibleFolders.map((folder) => <FolderLink key={folder.name} copy={copy} folder={folder} selected={folder.name === currentFolder} onClose={onClose} />)}{!visibleFolders.length && <span className="px-9 py-2 text-xs text-muted-foreground">{copy.noConversations}</span>}</ScrollArea>}
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

function AccountMenu({ copy, accounts, accountEmail }: { copy: Copy; accounts: ConversationListResponse["accounts"]; accountEmail: string }) {
  const active = accounts.find((account) => account.isActive || account.email === accountEmail);
  const selectAccount = async (email: string) => {
    if (email === accountEmail) return;
    await switchAccount(email);
    window.location.assign("/inbox");
  };
  return (
    <MenuPrimitive.Root>
      <MenuPrimitive.Trigger render={<Button variant="ghost" className="min-w-0 flex-1 justify-start px-2" />}>
        <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: active?.color || "#777" }} />
        <span className="min-w-0 flex-1 truncate text-left">{active?.label || accountEmail}</span>
        <ChevronDown className="size-3.5" />
      </MenuPrimitive.Trigger>
      <MenuPrimitive.Portal>
        <MenuPrimitive.Positioner side="top" align="start" sideOffset={6} className="z-[70]">
          <MenuPrimitive.Popup className="w-60 origin-[var(--transform-origin)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95">
            {accounts.map((account) => (
              <MenuPrimitive.Item key={account.email} className="flex cursor-default items-center gap-2 rounded-md px-2 py-2 text-sm outline-none data-highlighted:bg-muted" onClick={() => void selectAccount(account.email)}>
                <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: account.color || "#777" }} />
                <span className="min-w-0 flex-1"><strong className="block truncate font-medium">{account.label || account.email}</strong><small className="block truncate text-muted-foreground">{account.email}</small></span>
                {(account.isActive || account.email === accountEmail) && <Check className="size-4 shrink-0" />}
              </MenuPrimitive.Item>
            ))}
            <MenuPrimitive.Separator className="my-1 h-px bg-border" />
            <MenuPrimitive.Item className="flex cursor-default items-center rounded-md px-2 py-2 text-sm text-destructive outline-none data-highlighted:bg-muted" onClick={() => { void signOut().then(() => window.location.assign("/user-login")); }}>{copy.signOut}</MenuPrimitive.Item>
          </MenuPrimitive.Popup>
        </MenuPrimitive.Positioner>
      </MenuPrimitive.Portal>
    </MenuPrimitive.Root>
  );
}

function folderKind(folder: Mailbox) {
  const name = folder.name.toLowerCase();
  const attributes = folder.attributes.map((attribute) => attribute.trim().toLowerCase());
  if (attributes.includes("\\inbox") || name === "inbox") return "inbox";
  if (attributes.includes("\\sent") || ["sent", "sent items", "sent mail", "sent messages"].includes(name)) return "sent";
  if (attributes.includes("\\drafts") || name === "draft" || name === "drafts") return "drafts";
  if (attributes.includes("\\trash") || ["trash", "deleted", "deleted items", "deleted messages", "bin"].includes(name)) return "trash";
  if (attributes.includes("\\junk") || ["junk", "junk mail", "junk email", "junk e-mail", "spam", "bulk mail"].includes(name)) return "junk";
  if (attributes.includes("\\archive") || name === "archive" || name === "archives") return "archive";
  return "custom";
}

function folderLabel(copy: Copy, folder: Mailbox) {
  const labels = {
    inbox: copy.inboxFolder,
    sent: copy.sentFolder,
    drafts: copy.draftsFolder,
    trash: copy.trashFolder,
    junk: copy.junkFolder,
    archive: copy.archiveFolder,
    custom: folder.name,
  };
  return labels[folderKind(folder)];
}

function FolderLink({ copy, folder, selected = false, onClose }: { copy: Copy; folder: Mailbox; selected?: boolean; onClose: () => void }) {
  const kind = folderKind(folder);
  const Icon = kind === "trash" ? Trash2 : kind === "junk" ? TriangleAlert : kind === "drafts" ? FilePenLine : kind === "archive" ? Archive : Folder;
  return <Button asChild variant={selected ? "secondary" : "ghost"} size="sm" className={cn("w-full justify-start gap-2.5 pl-9 text-muted-foreground", selected && "text-sidebar-accent-foreground")}><a href={`/folder/${encodeURIComponent(folder.name)}`} onClick={onClose}><Icon /><span className="min-w-0 flex-1 truncate">{folderLabel(copy, folder)}</span>{folder.unreadCount ? <Badge variant="secondary" className="min-w-5 justify-center px-1.5 text-[10px]">{folder.unreadCount}</Badge> : null}</a></Button>;
}

function ConversationList({ copy, data, search, onSearch, onMenu, loading, error, selectedId, onSelect, onMarkUnread, onDelete, onRefresh, className }: { copy: Copy; data?: ConversationListResponse; search: string; onSearch: (value: string) => void; onMenu: () => void; loading: boolean; error: Error | null; selectedId: string | null; onSelect: (id: string) => void; onMarkUnread: (conversation: ConversationSummary) => void; onDelete: (conversation: ConversationSummary) => void; onRefresh: () => void; className?: string }) {
  const rows = data?.conversations || [];
  return (
    <section data-testid="conversation-list" className={cn("min-w-0 flex-1 flex-col border-r bg-card lg:w-[23.125rem] lg:flex-none", className)}>
      <div className="border-b bg-card px-3 py-3">
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={onMenu} aria-label={copy.folders} title={copy.folders}><Menu /></Button>
          <div className="relative flex min-w-0 flex-1 items-center">
          <Search className="pointer-events-none absolute left-3 size-4 text-muted-foreground" aria-hidden="true" />
          <Input data-testid="mail-search" type="search" className="h-9 bg-muted/60 pl-9 pr-9" value={search} onChange={(event) => onSearch(event.target.value)} placeholder={copy.search} aria-label={copy.search} />
          {search && <Button variant="ghost" size="icon" className="absolute right-1 size-7" onClick={() => onSearch("")} aria-label={copy.cancel} title={copy.cancel}><X /></Button>}
          </div>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        {loading && <ListSkeleton />}
        {!loading && error && <ErrorState copy={copy} onRetry={onRefresh} />}
        {!loading && !error && rows.length === 0 && <EmptyState icon={<MessageCircle />} text={search ? copy.noConversations : copy.noConversations} />}
        {!loading && !error && rows.map((conversation) => <ConversationRow key={conversation.id} copy={copy} conversation={conversation} selected={conversation.id === selectedId} onClick={() => onSelect(conversation.id)} onMarkUnread={() => onMarkUnread(conversation)} onDelete={() => onDelete(conversation)} />)}
      </ScrollArea>
    </section>
  );
}

function ConversationRow({ copy, conversation, selected, onClick, onMarkUnread, onDelete }: { copy: Copy; conversation: ConversationSummary; selected: boolean; onClick: () => void; onMarkUnread: () => void; onDelete: () => void }) {
  const queryClient = useQueryClient();
  const inputRef = useRef<HTMLInputElement>(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(conversation.note || "");
  const [error, setError] = useState("");
  const cancelRef = useRef(false);
  useEffect(() => {
    if (!editing) setDraft(conversation.note || "");
  }, [conversation.note, editing]);
  const beginEditing = () => {
    setError("");
    setEditing(true);
    window.requestAnimationFrame(() => inputRef.current?.focus());
  };
  const saveNote = async () => {
    if (cancelRef.current) {
      cancelRef.current = false;
      return;
    }
    const note = draft.trim();
    setEditing(false);
    if (note === (conversation.note || "")) return;
    try {
      await saveConversationNote(conversation.id, note);
      setError("");
      queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) => current ? {
        ...current,
        conversations: current.conversations.map((item) => item.id === conversation.id ? { ...item, note } : item),
      } : current);
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
    } catch {
      setError(copy.noteSaveFailed);
      setEditing(true);
      window.requestAnimationFrame(() => inputRef.current?.focus());
    }
  };
  const cancelEditing = () => {
    cancelRef.current = true;
    setDraft(conversation.note || "");
    setError("");
    setEditing(false);
    inputRef.current?.blur();
  };
  return (
    <ContextMenuPrimitive.Root>
      <ContextMenuPrimitive.Trigger className="block">
      <article data-testid="conversation-row" className={cn("relative w-full max-w-full overflow-hidden border-b bg-card px-4 py-3 transition-colors hover:bg-muted", selected && "border-l-2 border-l-foreground bg-muted pl-[0.875rem]")}>
      <button className="flex w-full min-w-0 max-w-full items-start gap-3 overflow-hidden text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50" onClick={onClick} type="button">
        <span className="min-w-0 max-w-full flex-1 overflow-hidden">
          <span className="flex min-w-0 max-w-full items-baseline justify-between gap-2 overflow-hidden"><strong className="min-w-0 flex-1 truncate text-sm font-semibold">{conversation.peerEmail || conversation.title || copy.conversations}</strong><time className="shrink-0 text-[10px] text-muted-foreground">{formatTime(conversation.date)}</time></span>
          <span className="mt-1 block max-w-full truncate text-xs text-muted-foreground/70">{conversation.preview || copy.noBody}</span>
        </span>
        {conversation.unreadCount > 0 && <Badge title={`${conversation.unreadCount} ${copy.unread}`} className="mt-0.5 min-w-5 justify-center px-1.5 text-[10px] leading-4">{conversation.unreadCount}</Badge>}
      </button>
      <div className="mt-1.5 h-4 w-full overflow-hidden">
        {editing ? (
          <Input ref={inputRef} className="block h-full rounded-none border-0 bg-transparent px-0 py-0 text-xs leading-4 shadow-none focus-visible:border-transparent focus-visible:ring-0" value={draft} maxLength={200} onChange={(event) => setDraft(event.target.value)} onBlur={() => void saveNote()} onKeyDown={(event) => { if (event.key === "Enter") event.currentTarget.blur(); if (event.key === "Escape") cancelEditing(); }} aria-label={copy.addNote} />
        ) : (
          <div className="group/note relative h-full w-full">
            <button type="button" className={cn("block h-full w-full truncate pr-6 text-left text-xs leading-4", conversation.note ? "text-primary" : "text-muted-foreground/60")} onClick={onClick}>{conversation.note || copy.addNote}</button>
            <button type="button" className="absolute top-0 right-0 grid size-4 place-items-center text-muted-foreground opacity-0 transition-opacity group-hover/note:opacity-100 hover:text-foreground focus-visible:opacity-100" onClick={beginEditing} aria-label={copy.addNote} title={copy.addNote}><Pencil className="size-3" /></button>
          </div>
        )}
      </div>
      {error && <p className="mt-1 text-xs text-destructive">{error}</p>}
      </article>
      </ContextMenuPrimitive.Trigger>
      <ContextMenuPrimitive.Portal>
        <ContextMenuPrimitive.Positioner className="z-40 outline-none">
          <ContextMenuPrimitive.Popup className="w-44 origin-[var(--transform-origin)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95">
            <ContextMenuPrimitive.Item className="flex cursor-default items-center gap-2 rounded-md px-2 py-2 text-sm outline-none data-highlighted:bg-muted" onClick={onMarkUnread}><Mail className="size-4" />{copy.markUnread}</ContextMenuPrimitive.Item>
            <ContextMenuPrimitive.Item className="flex cursor-default items-center gap-2 rounded-md px-2 py-2 text-sm text-destructive outline-none data-highlighted:bg-muted" onClick={onDelete}><Trash2 className="size-4" />{copy.deleteConversation}</ContextMenuPrimitive.Item>
          </ContextMenuPrimitive.Popup>
        </ContextMenuPrimitive.Positioner>
      </ContextMenuPrimitive.Portal>
    </ContextMenuPrimitive.Root>
  );
}

function ChatPanel({ copy, detail, loading, error, onBack, onReply, onNewMail, onConversationEmpty, className }: { copy: Copy; detail?: ConversationDetail; loading: boolean; error: Error | null; onBack: () => void; onReply: (conversation: ConversationDetail, message: ConversationMessage) => void; onNewMail: (conversation: ConversationDetail, message: ConversationMessage) => void; onConversationEmpty: () => void; className?: string }) {
  const panelClass = cn("min-w-0 flex-1 flex-col bg-surface", className);
  if (loading) return <section className={cn(panelClass, "items-center justify-center")}><div className="text-sm text-muted-foreground">{copy.loading}</div></section>;
  if (error) return <section className={panelClass}><ErrorState copy={copy} /></section>;
  if (!detail) return <section className={panelClass}><EmptyState icon={<MessageCircle />} text={copy.selectConversation} /></section>;
  return <ChatView copy={copy} detail={detail} onBack={onBack} onReply={(message) => onReply(detail, message)} onNewMail={(message) => onNewMail(detail, message)} onConversationEmpty={onConversationEmpty} />;
}

function ChatView({ copy, detail, onBack, onReply, onNewMail, onConversationEmpty }: { copy: Copy; detail: ConversationDetail; onBack: () => void; onReply: (message: ConversationMessage) => void; onNewMail: (message: ConversationMessage) => void; onConversationEmpty: () => void }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const queryClient = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<ConversationMessage | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const [summary, setSummary] = useState("");
  const aiSettings = useQuery({ queryKey: ["ai-models"], queryFn: getAIModels, retry: false });
  const summarizeMutation = useMutation({
    mutationFn: () => summarizeMailThread(detail.messages.map((message) => [
      `From: ${message.fromName ? `${message.fromName} <${message.from}>` : message.from}`,
      `To: ${message.to}`,
      `Date: ${message.date}`,
      `Subject: ${message.subject}`,
      "",
      message.body || message.preview,
    ].join("\n")).join("\n\n---\n\n")),
    onSuccess: (result) => setSummary(result.summary),
  });
  useEffect(() => {
    setSummary("");
    summarizeMutation.reset();
  }, [detail.id]);
  const deleteMutation = useMutation({
    mutationFn: (message: ConversationMessage) => deleteConversationMessage(detail.id, message.id, message.folder || "INBOX"),
    onSuccess: async () => {
      setDeleteTarget(null);
      setDeleteError("");
      if (detail.messages.length === 1) onConversationEmpty();
      await queryClient.invalidateQueries({ queryKey: ["conversations"] });
      if (detail.messages.length > 1) await queryClient.invalidateQueries({ queryKey: ["conversation", detail.id] });
    },
    onError: (value) => setDeleteError(value instanceof Error ? value.message : copy.deleteEmailFailed),
  });
  useLayoutEffect(() => {
    const scroll = scrollRef.current;
    const content = contentRef.current;
    if (!scroll || !content) return;
    const stickToBottom = { current: true };
    const jumpToLatest = () => {
      const previousBehavior = scroll.style.scrollBehavior;
      scroll.style.scrollBehavior = "auto";
      scroll.scrollTop = scroll.scrollHeight;
      scroll.style.scrollBehavior = previousBehavior;
    };
    const updateStickiness = () => {
      stickToBottom.current = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 80;
    };
    const keepLatestVisible = () => {
      if (stickToBottom.current) jumpToLatest();
    };

    scroll.addEventListener("scroll", updateStickiness, { passive: true });
    const resizeObserver = new ResizeObserver(keepLatestVisible);
    jumpToLatest();
    resizeObserver.observe(content);
    const frame = window.requestAnimationFrame(jumpToLatest);
    return () => {
      window.cancelAnimationFrame(frame);
      scroll.removeEventListener("scroll", updateStickiness);
      resizeObserver.disconnect();
    };
  }, [detail.id, detail.messages.length]);

  return (
    <section data-testid="conversation-detail" className="flex min-w-0 flex-1 flex-col">
      <header className="grid min-h-[4.5rem] grid-cols-[minmax(0,1fr)_minmax(0,2fr)_minmax(0,1fr)] items-center border-b bg-card px-3 py-3 sm:px-5">
        <div className="flex min-w-0 items-center justify-start"><Button variant="ghost" size="icon" className="lg:hidden" onClick={onBack} aria-label={copy.cancel} title={copy.cancel}><ArrowLeft /></Button></div>
        <div className="min-w-0 text-center"><h2 className="truncate text-sm font-semibold">{detail.title || copy.conversations}</h2><p className="mt-1 truncate text-xs text-muted-foreground">{detail.subject || copy.noSubject}<span className="px-1.5">·</span>{detail.count} {copy.messages}</p></div>
        <div className="flex min-w-0 items-center justify-end">{aiSettings.data?.models.length ? <Button variant="outline" size="sm" disabled={summarizeMutation.isPending} onClick={() => summarizeMutation.mutate()}><Sparkles />{summarizeMutation.isPending ? copy.summarizing : copy.summarize}</Button> : <Button variant="ghost" size="icon" className="hidden sm:inline-flex" aria-label="More" title="More"><MoreHorizontal /></Button>}</div>
      </header>
      <ScrollArea className="min-h-0 flex-1" viewportClassName="scroll-smooth" contentClassName="px-3 py-6 sm:px-[5vw] sm:py-8" viewportRef={scrollRef}>
        <div ref={contentRef}>
          {(summary || summarizeMutation.error) && <div className="mx-auto mb-6 max-w-3xl rounded-xl border bg-card p-4 shadow-sm"><div className="flex items-center justify-between gap-3"><h3 className="flex items-center gap-2 text-sm font-semibold"><Sparkles className="size-4" />{copy.summaryTitle}</h3><Button variant="ghost" size="icon" className="size-7" onClick={() => { setSummary(""); summarizeMutation.reset(); }} aria-label={copy.cancel}><X /></Button></div>{summary ? <p className="mt-3 whitespace-pre-wrap text-sm leading-relaxed">{summary}</p> : <p className="mt-3 text-sm text-destructive">{summarizeMutation.error instanceof Error ? summarizeMutation.error.message : copy.loadFailed}</p>}</div>}
          {detail.messages.map((message, index) => <MessageBubble key={`${message.folder || "inbox"}-${message.id}`} copy={copy} message={message} accountEmail={detail.accountEmail} rootRef={scrollRef} eager={index >= detail.messages.length - 3} onReply={() => onReply(message)} onNewMail={() => onNewMail(message)} onDelete={() => { setDeleteError(""); setDeleteTarget(message); }} />)}
        </div>
      </ScrollArea>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open && !deleteMutation.isPending) { setDeleteTarget(null); setDeleteError(""); } }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{copy.deleteEmailTitle}</DialogTitle>
            <DialogDescription>{copy.deleteEmailDescription}</DialogDescription>
          </DialogHeader>
          {deleteError && <p className="text-sm text-destructive">{deleteError}</p>}
          <DialogFooter>
            <Button variant="ghost" disabled={deleteMutation.isPending} onClick={() => setDeleteTarget(null)}>{copy.cancel}</Button>
            <Button variant="destructive" disabled={deleteMutation.isPending || !deleteTarget} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}><Trash2 />{deleteMutation.isPending ? copy.deleting : copy.deleteEmail}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function MessageBubble({ copy, message, accountEmail, rootRef, eager, onReply, onNewMail, onDelete }: { copy: Copy; message: ConversationMessage; accountEmail?: string; rootRef: { current: HTMLDivElement | null }; eager: boolean; onReply: () => void; onNewMail: () => void; onDelete: () => void }) {
  const sender = message.outgoing ? copy.me : message.fromName || message.from;
  const split = splitQuotedText(message.body || message.preview || copy.noBody);
  const visibleText = split.visible || (split.quoted ? copy.quotedOnly : copy.noBody);
  const outgoing = message.outgoing;
  const formattedDate = formatTime(message.date);
  return (
    <ContextMenuPrimitive.Root>
      <ContextMenuPrimitive.Trigger className="mb-5 block select-text">
        <article>
          <div className={cn("mb-1 flex items-center gap-2 text-xs leading-tight text-muted-foreground", outgoing && "justify-end")}><span className="font-medium">{sender}</span><time title={formattedDate}>{formattedDate}</time></div>
          <div className={cn("flex items-end", outgoing && "justify-end")}>
            <div className={cn("min-w-0 max-w-[80%] overflow-x-auto rounded-xl border border-transparent bg-secondary px-3 py-2 text-sm leading-relaxed text-secondary-foreground", message.html && "w-full")}>
              {message.html ? <EmailHTMLFrame html={message.html} title={message.subject || copy.noSubject} rootRef={rootRef} eager={eager} /> : <div className="whitespace-pre-wrap">{renderLinkifiedText(visibleText)}</div>}
              {!message.html && split.quoted && <details className="mt-2 border-t border-border/60 pt-2 text-muted-foreground">
                <summary className="flex cursor-pointer list-none items-center gap-1 text-xs [&::-webkit-details-marker]:hidden"><ChevronDown className="size-3.5" />{copy.showQuoted}</summary>
                <div className="mt-2 border-l-2 border-border pl-2 whitespace-pre-wrap">{renderLinkifiedText(split.quoted)}</div>
              </details>}
              {message.attachments?.length ? <><Separator className="my-2 opacity-50" /><div className="grid gap-1.5">{message.attachments.map((attachment) => <a className="flex min-w-0 items-center gap-1.5 text-xs text-primary" key={attachment.id} href={`/api/attachment/${encodeURIComponent(attachment.id)}?account_email=${encodeURIComponent(accountEmail || "")}`}><Paperclip className="size-3.5 shrink-0" /><span className="min-w-0 truncate">{attachment.filename}</span><small className="shrink-0 text-muted-foreground">{formatSize(attachment.size)}</small></a>)}</div></> : null}
            </div>
          </div>
          <MailMessageSummary copy={copy} accountEmail={accountEmail} folder={message.folder || "INBOX"} messageId={message.id} initialSummary={message.mailSummary} outgoing={outgoing} />
        </article>
      </ContextMenuPrimitive.Trigger>
      <ContextMenuPrimitive.Portal>
        <ContextMenuPrimitive.Positioner className="z-40 outline-none">
          <ContextMenuPrimitive.Popup className="w-40 origin-[var(--transform-origin)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95">
            <ContextMenuPrimitive.Item className="flex cursor-default items-center gap-2 rounded-md px-2 py-2 text-sm outline-none data-highlighted:bg-muted" onClick={onReply}><Send className="size-4" />{copy.reply}</ContextMenuPrimitive.Item>
            <ContextMenuPrimitive.Item className="flex cursor-default items-center gap-2 rounded-md px-2 py-2 text-sm outline-none data-highlighted:bg-muted" onClick={onNewMail}><Mail className="size-4" />{copy.sendEmail}</ContextMenuPrimitive.Item>
            <ContextMenuPrimitive.Item className="flex cursor-default items-center gap-2 rounded-md px-2 py-2 text-sm text-destructive outline-none data-highlighted:bg-muted" onClick={onDelete}><Trash2 className="size-4" />{copy.deleteEmail}</ContextMenuPrimitive.Item>
          </ContextMenuPrimitive.Popup>
        </ContextMenuPrimitive.Positioner>
      </ContextMenuPrimitive.Portal>
    </ContextMenuPrimitive.Root>
  );
}

function MailMessageSummary({ copy, accountEmail, folder, messageId, initialSummary, outgoing = false }: { copy: Copy; accountEmail?: string; folder: string; messageId: string; initialSummary?: ConversationMessage["mailSummary"]; outgoing?: boolean }) {
  const queryClient = useQueryClient();
  const [savedSummary, setSavedSummary] = useState(initialSummary);
  const mutation = useMutation({
    mutationFn: (regenerate: boolean) => summarizeMailMessage(accountEmail || "", folder, messageId, regenerate),
    onSuccess: (result) => {
      const summary: MailSummary = { text: result.summary, status: result.status, stale: result.stale, updatedAt: result.updatedAt };
      setSavedSummary(summary);
      queryClient.setQueryData<MailMessage>(["message", folder, messageId], (current) => current ? { ...current, mailSummary: summary } : current);
      queryClient.setQueriesData<ConversationDetailResponse>({ queryKey: ["conversation"] }, (current) => {
        if (!current || (accountEmail && current.conversation.accountEmail !== accountEmail)) return current;
        let changed = false;
        const messages = current.conversation.messages.map((message) => {
          if (message.id !== messageId || (message.folder || "INBOX") !== folder) return message;
          changed = true;
          return { ...message, mailSummary: summary };
        });
        return changed ? { ...current, conversation: { ...current.conversation, messages } } : current;
      });
    },
  });
  useEffect(() => {
    setSavedSummary(initialSummary);
    mutation.reset();
  }, [accountEmail, folder, messageId, initialSummary]);
  return (
    <div className={cn("mt-1.5 max-w-[80%]", outgoing && "ml-auto")}>
      {!savedSummary && <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground" disabled={mutation.isPending || !accountEmail} onClick={() => mutation.mutate(false)}><Sparkles className="size-3.5" />{mutation.isPending ? copy.summarizing : copy.summarize}</Button>}
      {savedSummary && <div className="mt-1 border-l-2 border-primary/40 bg-muted/40 px-3 py-2 text-sm leading-relaxed">
        <div className="mb-1 flex items-center justify-between gap-2"><div className="flex min-w-0 items-center gap-2"><strong className="text-xs font-medium text-muted-foreground">{copy.mailSummaryTitle}</strong>{savedSummary.stale && <span className="truncate text-[10px] text-amber-700 dark:text-amber-400">{copy.summaryStale}</span>}</div><Button type="button" variant="ghost" size="icon" className="size-6 shrink-0" disabled={mutation.isPending || !accountEmail} onClick={() => mutation.mutate(true)} aria-label={copy.regenerateSummary} title={copy.regenerateSummary}><RotateCcw className={cn("size-3.5", mutation.isPending && "animate-spin")} /></Button></div>
        <p className="whitespace-pre-wrap">{savedSummary.text}</p>
      </div>}
      {mutation.error && <p className="mt-1 px-2 text-xs text-destructive">{mutation.error instanceof Error ? mutation.error.message : copy.loadFailed}</p>}
    </div>
  );
}

function EmailHTMLFrame({ html, title, rootRef, eager }: { html: string; title: string; rootRef: { current: HTMLDivElement | null }; eager: boolean }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [enabled, setEnabled] = useState(eager);

  useEffect(() => {
    if (eager) setEnabled(true);
  }, [eager]);

  useEffect(() => {
    if (enabled) return;
    const host = hostRef.current;
    if (!host || !("IntersectionObserver" in window)) {
      setEnabled(true);
      return;
    }

    const observer = new IntersectionObserver(([entry]) => {
      if (!entry?.isIntersecting) return;
      setEnabled(true);
      observer.disconnect();
    }, { root: rootRef.current, rootMargin: "1200px 0px" });
    observer.observe(host);
    return () => observer.disconnect();
  }, [enabled, rootRef]);

  useLayoutEffect(() => {
    if (!enabled) return;
    const frame = frameRef.current;
    if (!frame) return;

    let mounted = true;
    let measureFrame = 0;
    let resizeObserver: ResizeObserver | null = null;
    let mutationObserver: MutationObserver | null = null;
    let resourceCleanup: (() => void) | null = null;
    let documentProbeTimer = 0;
    let lastHeight = 0;

    // The document is written synchronously below, so the first real body
    // height does not depend on the iframe load event.
    frame.style.height = "1px";

    const measureNow = () => {
      if (!mounted) return;
      const doc = frame.contentDocument;
      const root = doc?.documentElement;
      const body = doc?.body;
      if (!root || !body) return;

      const height = Math.max(body.scrollHeight, body.offsetHeight, body.getBoundingClientRect().height, 48);
      if (height === lastHeight) return;
      lastHeight = height;
      frame.style.height = `${height}px`;
    };

    const measure = () => {
      if (!mounted) return;
      window.cancelAnimationFrame(measureFrame);
      measureFrame = window.requestAnimationFrame(measureNow);
    };

    const clearDocumentObservers = () => {
      resizeObserver?.disconnect();
      mutationObserver?.disconnect();
      resizeObserver = null;
      mutationObserver = null;
      resourceCleanup?.();
      resourceCleanup = null;
    };

    const observeDocument = (doc: Document) => {
      clearDocumentObservers();

      const installTransparentBackground = () => {
        if (!doc.head || doc.head.querySelector("[data-lilmail-frame-style]")) return;
        const style = doc.createElement("style");
        style.dataset.lilmailFrameStyle = "";
        style.textContent = "html, body { background-color: transparent !important; }";
        doc.head.append(style);
      };

      installTransparentBackground();
      resizeObserver = new ResizeObserver(measure);
      resizeObserver.observe(doc.documentElement);
      if (doc.body) resizeObserver.observe(doc.body);
      mutationObserver = new MutationObserver(() => {
        installTransparentBackground();
        measure();
      });
      mutationObserver.observe(doc, { childList: true, subtree: true, characterData: true });

      const onResource = (event: Event) => {
        if (event.target instanceof HTMLImageElement) measure();
      };
      const onContextMenu = (event: MouseEvent) => {
        event.preventDefault();
        const frameRect = frame.getBoundingClientRect();
        frame.dispatchEvent(new MouseEvent("contextmenu", {
          bubbles: true,
          cancelable: true,
          clientX: frameRect.left + event.clientX,
          clientY: frameRect.top + event.clientY,
          button: 2,
          buttons: 2,
        }));
      };
      doc.addEventListener("load", onResource, true);
      doc.addEventListener("error", onResource, true);
      doc.addEventListener("contextmenu", onContextMenu);
      resourceCleanup = () => {
        doc.removeEventListener("load", onResource, true);
        doc.removeEventListener("error", onResource, true);
        doc.removeEventListener("contextmenu", onContextMenu);
      };
      void doc.fonts?.ready.then(measure, measure);
      measure();
    };

    const doc = frame.contentDocument;
    if (!doc) return clearDocumentObservers;
    doc.open();
    doc.write(html);
    doc.close();
    observeDocument(doc);
    measureNow();

    // A few short probes cover browsers that replace the initial about:blank
    // document after close(). They are independent of image loading.
    let probePasses = 0;
    const probeDocument = () => {
      if (!mounted) return;
      const current = frame.contentDocument;
      if (current && current !== doc) {
        observeDocument(current);
      }
      measureNow();
      probePasses += 1;
      if (mounted && probePasses < 20) {
        documentProbeTimer = window.setTimeout(probeDocument, 50);
      }
    };
    probeDocument();

    return () => {
      mounted = false;
      window.cancelAnimationFrame(measureFrame);
      window.clearTimeout(documentProbeTimer);
      clearDocumentObservers();
    };
  }, [enabled, html]);

  return <div ref={hostRef} className="min-h-12 w-full min-w-0">{enabled && <iframe ref={frameRef} className="block w-full min-w-0 border-0 bg-transparent" scrolling="auto" sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox" title={title} />}</div>;
}

function renderLinkifiedText(text: string) {
  return linkifyText(text).map((part, index) => typeof part === "string"
    ? <span key={index}>{part}</span>
    : <a className="text-foreground underline underline-offset-3" key={index} href={part.href} target={part.href.startsWith("mailto:") ? undefined : "_blank"} rel="noreferrer">{part.value}</a>);
}

function escapeHTML(value: string) {
  return value.replace(/[&<>"']/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character] || character);
}

function quoteAttribution(value: string) {
  return value.replace(/\s+/g, " ").trim().replace(/:$/, "") || "Quoted message";
}

function quotedTextToHTML(text: string) {
  const root = document.createElement("div");
  const containers: HTMLElement[] = [root];
  let currentDepth = 0;
  for (const rawLine of text.replace(/\r\n?/g, "\n").split("\n")) {
    const match = rawLine.match(/^\s*(>+)\s?(.*)$/);
    const depth = match ? match[1].length : 0;
    const value = match ? match[2] : rawLine;
    while (currentDepth < depth) {
      const quote = document.createElement("blockquote");
      containers[currentDepth].appendChild(quote);
      containers.push(quote);
      currentDepth += 1;
    }
    while (currentDepth > depth) {
      containers.pop();
      currentDepth -= 1;
    }
    const line = document.createElement("div");
    if (value) line.textContent = value;
    else line.appendChild(document.createElement("br"));
    containers[currentDepth].appendChild(line);
  }
  return root.innerHTML;
}

function structuredQuotedTextToHTML(text: string): string {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  const attribution = /^\s*(?:on\s+.+\bwrote\s*:|(?:在|於)\s*.+(?:写道|寫道)\s*[:：])\s*$/i;
  const original = /^\s*[-_]{2,}\s*(?:original(?:\s+message)?|原始邮件|原始郵件)\s*[-_]{2,}\s*$/i;
  const boundary = lines.findIndex((line, index) => index > 0 && (attribution.test(line) || original.test(line.replace(/\u00a0/g, " "))));
  if (boundary < 0) return quotedTextToHTML(text);
  const current = quotedTextToHTML(lines.slice(0, boundary).join("\n"));
  const lead = lines[boundary].trim();
  const tailLines = lines.slice(boundary + 1);
  const nonEmptyTail = tailLines.filter((line) => line.trim());
  const tail = nonEmptyTail.length > 0 && nonEmptyTail.every((line) => /^\s*>/.test(line))
    ? tailLines.map((line) => line.replace(/^\s*>\s?/, "")).join("\n")
    : tailLines.join("\n");
  return `${current}<p>${escapeHTML(lead)}</p><blockquote>${structuredQuotedTextToHTML(tail)}</blockquote>`;
}

function normalizeQuoteHTML(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  const quoteSelector = "blockquote, includetail, .gmail_quote, .yahoo_quoted, .protonmail_quote, .outlook_quote, .quoted-text, .quotedcontent, .original-message";
  Array.from(document.body.querySelectorAll(quoteSelector)).reverse().forEach((element) => {
    if (element.closest("[data-lilmail-signature]")) return;
    const tag = element.tagName.toLowerCase();
    if (tag !== "blockquote" && tag !== "includetail" && element.querySelector(quoteSelector)) return;
    let attribution = element.getAttribute("data-attribution") || "";
    const previous = element.previousElementSibling;
    if (!attribution && previous && /(?:\bwrote\s*:|写道\s*[:：]|寫道\s*[:：]|original)/i.test(previous.textContent || "")) {
      attribution = previous.textContent || "";
      previous.remove();
    }
    const wrapper = document.createElement("div");
    wrapper.dataset.lilmailReplyQuote = "";
    wrapper.dataset.attribution = quoteAttribution(attribution);
    while (element.firstChild) wrapper.appendChild(element.firstChild);
    element.replaceWith(wrapper);
  });
  const children = Array.from(document.body.children);
  const separatorIndex = children.findIndex((element) => /^[-_\s]*original(?:\s+message)?[-_\s]*$/i.test((element.textContent || "").replace(/\u00a0/g, " ").trim()));
  if (separatorIndex >= 0) {
    const wrapper = document.createElement("div");
    wrapper.dataset.lilmailReplyQuote = "";
    wrapper.dataset.attribution = quoteAttribution(children[separatorIndex].textContent || "Original message");
    children.slice(separatorIndex + 1).forEach((element) => wrapper.appendChild(element));
    children[separatorIndex].replaceWith(wrapper);
  }
  return document.body.innerHTML;
}

function serializeQuoteHTML(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  Array.from(document.body.querySelectorAll("[data-lilmail-reply-quote]")).reverse().forEach((element) => {
    const attribution = element.getAttribute("data-attribution") || "Quoted message";
    const paragraph = document.createElement("p");
    paragraph.textContent = attribution.endsWith(":") ? attribution : `${attribution}:`;
    const blockquote = document.createElement("blockquote");
    while (element.firstChild) blockquote.appendChild(element.firstChild);
    element.before(paragraph);
    element.replaceWith(blockquote);
  });
  return document.body.innerHTML;
}

function serializeComposeHTML(html: string) {
  const document = new DOMParser().parseFromString(serializeQuoteHTML(html), "text/html");
  Array.from(document.body.querySelectorAll("[data-lilmail-signature]")).forEach((element) => {
    element.replaceWith(...Array.from(element.childNodes));
  });
  return document.body.innerHTML;
}

function signatureNodeHTML(signature: EmailSignature) {
  return `<div data-lilmail-signature="${escapeHTML(signature.id)}" data-signature-name="${escapeHTML(signature.name)}">${signature.html.trim() || "<p></p>"}</div>`;
}

function setEditorSignature(editor: Editor, signature: EmailSignature | null) {
  let existingRange: { from: number; to: number } | null = null;
  let quotePosition: number | null = null;
  editor.state.doc.descendants((node, position) => {
    if (!existingRange && node.type.name === "emailSignature") {
      existingRange = { from: position, to: position + node.nodeSize };
      return false;
    }
    if (quotePosition === null && node.type.name === "replyQuote") {
      quotePosition = position;
      return false;
    }
    return true;
  });

  if (existingRange) {
    if (signature) editor.commands.insertContentAt(existingRange, signatureNodeHTML(signature));
    else editor.commands.deleteRange(existingRange);
    return;
  }
  if (signature) editor.commands.insertContentAt(quotePosition ?? editor.state.doc.content.size, signatureNodeHTML(signature));
}

function RichTextButtons({ editor, disabled = false }: { editor: Editor | null; disabled?: boolean }) {
  return (
    <>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleBold().run()} aria-label="Bold" title="Bold"><Bold /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleItalic().run()} aria-label="Italic" title="Italic"><Italic /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleUnderline().run()} aria-label="Underline" title="Underline"><Underline /></Button>
      <Separator orientation="vertical" className="mx-1 h-5" />
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleBulletList().run()} aria-label="Bullet list" title="Bullet list"><List /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => editor?.chain().focus().toggleOrderedList().run()} aria-label="Numbered list" title="Numbered list"><ListOrdered /></Button>
      <Button type="button" variant="ghost" size="icon" disabled={disabled || !editor} onClick={() => { const href = window.prompt("URL"); if (href) editor?.chain().focus().setLink({ href }).run(); }} aria-label="Link" title="Link"><Link /></Button>
    </>
  );
}

function htmlToPlainText(html: string) {
  const body = new DOMParser().parseFromString(html, "text/html").body;
  const lines: { depth: number; text: string }[] = [{ depth: 0, text: "" }];
  const newLine = (depth: number) => {
    if (!lines.at(-1)?.text) lines[lines.length - 1].depth = depth;
    else lines.push({ depth, text: "" });
  };
  const appendText = (value: string, depth: number) => {
    value.split("\n").forEach((part, index) => {
      if (index > 0) lines.push({ depth, text: "" });
      const line = lines[lines.length - 1];
      if (!line.text) line.depth = depth;
      line.text += part;
    });
  };
  const walk = (node: Node, depth: number) => {
    if (node.nodeType === Node.TEXT_NODE) {
      appendText(node.textContent || "", depth);
      return;
    }
    if (!(node instanceof HTMLElement)) return;
    if (node.tagName === "BR") {
      lines.push({ depth, text: "" });
      return;
    }
    const childDepth = node.tagName === "BLOCKQUOTE" ? depth + 1 : depth;
    if (node.tagName === "BLOCKQUOTE") newLine(childDepth);
    Array.from(node.childNodes).forEach((child) => walk(child, childDepth));
    if (["P", "DIV", "LI", "BLOCKQUOTE"].includes(node.tagName)) newLine(depth);
  };
  Array.from(body.childNodes).forEach((node) => walk(node, 0));
  return lines
    .map(({ depth, text }) => `${depth ? `${">".repeat(depth)} ` : ""}${text.trimEnd()}`.trimEnd())
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function splitRecipientValues(value: string) {
  return value.split(/[,;\n]+/).map((item) => item.trim()).filter(Boolean);
}

function isValidRecipient(value: string) {
  const match = value.trim().match(/^(?:[^<>]*<)?([^\s<>@,;]+@[^\s<>@,;]+\.[^\s<>@,;]+)>?$/);
  return Boolean(match);
}

function RecipientTagInput({ copy, recipients, onChange, draftRef, autoFocus = false }: { copy: Copy; recipients: string[]; onChange: (recipients: string[]) => void; draftRef: MutableRefObject<string>; autoFocus?: boolean }) {
  const [draft, setDraft] = useState("");
  const [invalid, setInvalid] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const addValues = (values: string[]) => {
    const candidates = values.map((value) => value.trim()).filter(Boolean);
    if (!candidates.length) return true;
    if (candidates.some((value) => !isValidRecipient(value))) {
      setInvalid(true);
      return false;
    }
    const existing = new Set(recipients.map((value) => value.toLowerCase()));
    onChange([...recipients, ...candidates.filter((value) => {
      const key = value.toLowerCase();
      if (existing.has(key)) return false;
      existing.add(key);
      return true;
    })]);
    setDraft("");
    draftRef.current = "";
    setInvalid(false);
    return true;
  };

  return (
    <div>
      <div className={cn("flex min-h-8 flex-wrap items-center gap-1.5 rounded-lg border border-input bg-transparent px-2 py-1 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50", invalid && "border-destructive focus-within:border-destructive focus-within:ring-destructive/20")} onClick={() => inputRef.current?.focus()}>
        {recipients.map((recipient, index) => <span className="flex max-w-full items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-sm" key={`${recipient}-${index}`}><span className="truncate" title={recipient}>{recipient}</span><button type="button" className="grid size-4 shrink-0 place-items-center rounded-sm text-muted-foreground hover:bg-background hover:text-foreground" onClick={(event) => { event.stopPropagation(); onChange(recipients.filter((_, itemIndex) => itemIndex !== index)); }} aria-label={`${copy.removeRecipient}: ${recipient}`} title={copy.removeRecipient}><X className="size-3" /></button></span>)}
        <input ref={inputRef} className="h-6 min-w-32 flex-1 bg-transparent px-0.5 text-sm outline-none placeholder:text-muted-foreground" value={draft} onChange={(event) => { setDraft(event.target.value); draftRef.current = event.target.value; if (invalid) setInvalid(false); }} onKeyDown={(event) => {
          if (["Enter", ",", ";", "Tab"].includes(event.key) && draft.trim()) {
            event.preventDefault();
            addValues(splitRecipientValues(draft));
          } else if (event.key === "Backspace" && !draft && recipients.length) {
            onChange(recipients.slice(0, -1));
          }
        }} onPaste={(event) => {
          const value = event.clipboardData.getData("text");
          if (!/[,;\n]/.test(value)) return;
          event.preventDefault();
          addValues(splitRecipientValues(value));
        }} onBlur={() => { if (draft.trim()) addValues(splitRecipientValues(draft)); }} placeholder={recipients.length ? "" : "name@example.com"} autoFocus={autoFocus} inputMode="email" aria-invalid={invalid} aria-label={copy.to} />
      </div>
      {invalid && <p className="mt-1 text-xs text-destructive">{copy.invalidRecipient}</p>}
    </div>
  );
}

function ComposeDialog({ copy, open, defaults, accountEmail, onOpenChange, onSent }: { copy: Copy; open: boolean; defaults: ComposeDefaults; accountEmail: string; onOpenChange: (value: boolean) => void; onSent: () => void }) {
  const [recipients, setRecipients] = useState<string[]>([]);
  const [subject, setSubject] = useState("");
  const [error, setError] = useState("");
  const [sourceMode, setSourceMode] = useState(false);
  const [sourceCode, setSourceCode] = useState("");
  const [attachments, setAttachments] = useState<File[]>([]);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const recipientDraftRef = useRef("");
  const signatureInitializedRef = useRef(false);
  const [selectedSignatureId, setSelectedSignatureId] = useState("none");
  const editor = useEditor({ extensions: [StarterKit.configure({ blockquote: false }), ReplyQuote, EmailSignatureExtension, EmailImageExtension, UnderlineExtension, LinkExtension.configure({ openOnClick: false }), Placeholder.configure({ placeholder: copy.writeMessage })], content: "", immediatelyRender: false });
  const signatures = useQuery({ queryKey: ["signatures", accountEmail], queryFn: getSignatures, enabled: open, retry: false });
  const mutation = useMutation({ mutationFn: sendMessage });

  useEffect(() => {
    if (!open || !editor) return;
    setRecipients(splitRecipientValues(defaults.to));
    setSubject(defaults.subject);
    setError("");
    setSourceMode(false);
    setSourceCode(defaults.html || "");
    setSelectedSignatureId("none");
    signatureInitializedRef.current = false;
    setAttachments([]);
    recipientDraftRef.current = "";
    if (attachmentInputRef.current) attachmentInputRef.current.value = "";
    editor.commands.setContent(normalizeQuoteHTML(defaults.html || ""));
    editor.commands.focus("start");
  }, [open, defaults, editor]);

  useEffect(() => {
    if (!open || !editor || !signatures.data || signatureInitializedRef.current) return;
    const signature = signatures.data.signatures.find((item) => item.default) || signatures.data.signatures[0] || null;
    setEditorSignature(editor, signature);
    setSelectedSignatureId(signature?.id || "none");
    signatureInitializedRef.current = true;
  }, [open, editor, signatures.data]);

  const toggleSourceMode = () => {
    if (!editor) return;
    if (sourceMode) {
      editor.commands.setContent(normalizeQuoteHTML(sourceCode));
      editor.commands.focus("start");
      setSourceMode(false);
      return;
    }
    setSourceCode(serializeQuoteHTML(editor.getHTML()));
    setSourceMode(true);
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const pendingRecipients = splitRecipientValues(recipientDraftRef.current);
    const submittedRecipients = [...recipients, ...pendingRecipients.filter((value) => isValidRecipient(value))];
    const htmlBody = serializeComposeHTML(sourceMode ? sourceCode : editor?.getHTML() || "");
    const plainBody = htmlToPlainText(htmlBody);
    if (!submittedRecipients.length || pendingRecipients.some((value) => !isValidRecipient(value))) {
      setError(copy.invalidRecipient);
      return;
    }
    if (!plainBody) {
      setError(copy.noBody);
      return;
    }
    const form = new FormData();
    form.set("to", submittedRecipients.join(", "));
    form.set("subject", subject.trim());
    form.set("body", plainBody);
    form.set("html_body", htmlBody);
    if (defaults.inReplyTo) form.set("in_reply_to", defaults.inReplyTo);
    if (defaults.references?.length) form.set("references", defaults.references.join(" "));
    if (accountEmail) form.set("account_email", accountEmail);
    attachments.forEach((file) => form.append("attachments", file, file.name));
    mutation.mutate(form, { onSuccess: () => { onSent(); onOpenChange(false); }, onError: (value) => setError(value instanceof Error ? value.message : copy.loginFailed) });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="compose-dialog" className="flex h-[80vh] w-[80vw] max-w-[1200px] flex-col gap-0 overflow-hidden p-0 sm:max-w-[1200px]">
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
          <DialogHeader className="border-b px-5 py-4 pr-12 text-left"><DialogTitle className="truncate text-base">{subject || copy.writeMessage}</DialogTitle><DialogDescription className="sr-only">{copy.compose}</DialogDescription></DialogHeader>
          <div className="grid gap-3 border-b px-5 py-4"><Label className="grid gap-1.5 text-xs text-muted-foreground">{copy.to}<RecipientTagInput copy={copy} recipients={recipients} onChange={setRecipients} draftRef={recipientDraftRef} autoFocus /></Label><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="compose-subject">{copy.subject}<Input id="compose-subject" value={subject} onChange={(event) => setSubject(event.target.value)} placeholder={copy.noSubject} /></Label></div>
          <div className="flex items-center gap-1 overflow-x-auto border-b bg-muted px-4 py-1">
            <RichTextButtons editor={editor} disabled={sourceMode} />
            <div className="ml-auto flex shrink-0 items-center gap-1">
              <Select value={selectedSignatureId} disabled={sourceMode || signatures.isPending} onValueChange={(value) => {
                if (!value) return;
                setSelectedSignatureId(value);
                if (editor) setEditorSignature(editor, signatures.data?.signatures.find((item) => item.id === value) || null);
              }}>
                <SelectTrigger className="h-8 w-44" aria-label={copy.signatureSettings} title={copy.signatureSettings}><SignatureIcon /><SelectValue>{selectedSignatureId === "none" ? copy.noSignature : signatures.data?.signatures.find((item) => item.id === selectedSignatureId)?.name || copy.noSignature}</SelectValue></SelectTrigger>
                <SelectContent><SelectItem value="none">{copy.noSignature}</SelectItem>{signatures.data?.signatures.map((signature) => <SelectItem key={signature.id} value={signature.id}>{signature.name}</SelectItem>)}</SelectContent>
              </Select>
              <Button type="button" variant={sourceMode ? "secondary" : "ghost"} size="sm" onClick={toggleSourceMode} aria-label={sourceMode ? copy.richText : copy.sourceCode} title={sourceMode ? copy.richText : copy.sourceCode}><Code2 />{sourceMode ? copy.richText : copy.sourceCode}</Button>
            </div>
          </div>
          {sourceMode
            ? <textarea className="min-h-0 flex-1 resize-none bg-background px-5 py-4 font-mono text-sm leading-6 outline-none" value={sourceCode} onChange={(event) => setSourceCode(event.target.value)} spellCheck={false} aria-label={copy.sourceCode} />
            : <ScrollArea className="min-h-0 flex-1" contentClassName="px-5 py-4"><EditorContent editor={editor} /></ScrollArea>}
          {attachments.length > 0 && <div className="flex flex-wrap gap-2 border-t px-5 py-2">{attachments.map((file, index) => <span className="flex max-w-64 items-center gap-1.5 rounded-md bg-muted px-2 py-1 text-xs" key={`${file.name}-${file.size}-${file.lastModified}-${index}`}><Paperclip className="size-3.5 shrink-0" /><span className="truncate" title={file.name}>{file.name}</span><span className="shrink-0 text-muted-foreground">{formatSize(file.size)}</span><Button type="button" variant="ghost" size="icon" className="size-5" onClick={() => setAttachments((current) => current.filter((_, itemIndex) => itemIndex !== index))} aria-label={`${copy.removeAttachment}: ${file.name}`} title={copy.removeAttachment}><X className="size-3" /></Button></span>)}</div>}
          {error && <p className="px-5 pb-2 text-xs text-destructive">{error}</p>}
          <DialogFooter className="flex-row items-center justify-between border-t px-5 py-3 sm:flex-row sm:justify-between"><input ref={attachmentInputRef} className="sr-only" type="file" multiple onChange={(event) => { const selected = Array.from(event.target.files || []); setAttachments((current) => { const next = [...current, ...selected]; if (next.reduce((total, file) => total + file.size, 0) > 18 * 1024 * 1024) { setError(copy.attachmentsTooLarge); return current; } setError(""); return next; }); event.target.value = ""; }} /><Button type="button" variant="ghost" size="sm" onClick={() => attachmentInputRef.current?.click()}><Paperclip />{copy.attach}</Button><div className="flex gap-2"><Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>{copy.cancel}</Button><Button type="submit" disabled={mutation.isPending}><Send />{mutation.isPending ? copy.sending : copy.send}</Button></div></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function LoginScreen({ copy }: { copy: Copy }) {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const mutation = useMutation({ mutationFn: () => signIn(login, password), onSuccess: (result) => window.location.assign(result.next), onError: (value) => setError(value instanceof Error ? value.message : copy.loginFailed) });
  return <main className="grid min-h-screen place-items-center bg-background p-6"><div className="grid w-full max-w-sm gap-4 rounded-xl border bg-card p-6 ring-1 ring-foreground/5"><div className="flex items-center gap-2 text-lg font-semibold"><span className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground"><Mail className="size-4" /></span><strong>lilmail</strong></div><h1 className="mt-2 text-2xl font-semibold tracking-tight">{copy.login}</h1><p className="-mt-2 text-sm text-muted-foreground">{copy.appAccount}</p><form className="grid gap-4" onSubmit={(event) => { event.preventDefault(); mutation.mutate(); }}><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="login-account">{copy.appAccount}<Input id="login-account" value={login} onChange={(event) => setLogin(event.target.value)} autoComplete="username" required /></Label><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="login-password">{copy.password}<Input id="login-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" required /></Label>{error && <p className="text-xs text-destructive">{error}</p>}<Button type="submit" className="w-full" disabled={mutation.isPending}>{mutation.isPending ? copy.loading : copy.login}</Button></form><Button asChild variant="link" size="sm"><a href="/register">Create an application account</a></Button></div></main>;
}

function RegisterScreen({ copy }: { copy: Copy }) {
  const [form, setForm] = useState({ login: "", displayName: "", password: "", confirmation: "" });
  const [error, setError] = useState("");
  const mutation = useMutation({ mutationFn: () => register(form.login, form.displayName, form.password, form.confirmation), onSuccess: (result) => window.location.assign(result.next), onError: (value) => setError(value instanceof Error ? value.message : copy.loginFailed) });
  const field = (key: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) => setForm((value) => ({ ...value, [key]: event.target.value }));
  return <main className="grid min-h-screen place-items-center bg-background p-6"><div className="grid w-full max-w-sm gap-4 rounded-lg border bg-card p-6"><div className="flex items-center gap-2 text-lg font-semibold"><span className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground"><Mail className="size-4" /></span>lilmail</div><h1 className="text-xl font-semibold">{copy.createAccount}</h1><form className="grid gap-3" onSubmit={(event) => { event.preventDefault(); setError(""); mutation.mutate(); }}><Label className="grid gap-1.5">{copy.appAccount}<Input value={form.login} onChange={field("login")} required /></Label><Label className="grid gap-1.5">{copy.displayName}<Input value={form.displayName} onChange={field("displayName")} /></Label><Label className="grid gap-1.5">{copy.password}<Input type="password" minLength={8} value={form.password} onChange={field("password")} required /></Label><Label className="grid gap-1.5">{copy.password}<Input type="password" minLength={8} value={form.confirmation} onChange={field("confirmation")} required /></Label>{error && <p className="text-xs text-destructive">{error}</p>}<Button disabled={mutation.isPending}>{mutation.isPending ? copy.loading : copy.createAccount}</Button></form><Button asChild variant="link"><a href="/login">{copy.login}</a></Button></div></main>;
}

function PageHeader({ title, action }: { title: string; action?: ReactNode }) {
  return <header className="flex h-14 items-center justify-between border-b bg-card px-4 lg:px-6"><a className="flex items-center gap-2 font-semibold" href="/inbox"><span className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground"><Mail className="size-4" /></span>lilmail</a><h1 className="text-sm font-semibold">{title}</h1><div>{action}</div></header>;
}

function FolderMessageRow({ copy, message, address, selected, junkActions, actionPending, onSelect, onNotSpam, onPermanentDelete }: { copy: Copy; message: MailMessage; address: string; selected: boolean; junkActions: boolean; actionPending: boolean; onSelect: () => void; onNotSpam: () => void; onPermanentDelete: () => void }) {
  const row = <button type="button" onClick={onSelect} className={cn("block w-full border-b bg-card px-4 py-3 text-left transition-colors hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50", selected && "border-l-2 border-l-foreground bg-muted pl-[0.875rem]")}><span className="flex items-baseline justify-between gap-2"><strong className="min-w-0 truncate text-sm font-semibold">{address}</strong><time className="shrink-0 text-[10px] text-muted-foreground">{formatTime(message.date)}</time></span><span className="mt-1 block truncate text-xs text-muted-foreground/70">{message.subject || copy.noSubject}</span></button>;
  if (!junkActions) return row;
  return (
    <ContextMenuPrimitive.Root>
      <ContextMenuPrimitive.Trigger className="block">{row}</ContextMenuPrimitive.Trigger>
      <ContextMenuPrimitive.Portal>
        <ContextMenuPrimitive.Positioner className="z-40 outline-none">
          <ContextMenuPrimitive.Popup className="w-44 origin-[var(--transform-origin)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95">
            <ContextMenuPrimitive.Item disabled={actionPending} className="flex cursor-default items-center gap-2 rounded-md px-2 py-2 text-sm outline-none data-highlighted:bg-muted data-disabled:pointer-events-none data-disabled:opacity-50" onClick={onNotSpam}><ShieldCheck className="size-4" />{copy.notSpam}</ContextMenuPrimitive.Item>
            <ContextMenuPrimitive.Item disabled={actionPending} className="flex cursor-default items-center gap-2 rounded-md px-2 py-2 text-sm text-destructive outline-none data-highlighted:bg-muted data-disabled:pointer-events-none data-disabled:opacity-50" onClick={onPermanentDelete}><Trash2 className="size-4" />{copy.permanentDelete}</ContextMenuPrimitive.Item>
          </ContextMenuPrimitive.Popup>
        </ContextMenuPrimitive.Positioner>
      </ContextMenuPrimitive.Portal>
    </ContextMenuPrimitive.Root>
  );
}

function FolderPage({ folder }: { folder: string }) {
  const queryClient = useQueryClient();
  const metadata = useQuery({ queryKey: ["conversations", "folder-shell"], queryFn: () => getConversations() });
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities });
  const locale = useLocale(metadata.data?.locale);
  const [selected, setSelected] = useState<string | null>(() => new URL(window.location.href).searchParams.get("message"));
  const [detailOpen, setDetailOpen] = useState(() => Boolean(new URL(window.location.href).searchParams.get("message")));
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [darkMode, setDarkMode] = useState(prefersDarkMode);
  const [permanentDeleteTarget, setPermanentDeleteTarget] = useState<MailMessage | null>(null);
  const [permanentDeleteError, setPermanentDeleteError] = useState("");
  const list = useQuery({ queryKey: ["folder", folder], queryFn: () => getFolderMessages(folder) });
  const detail = useQuery({ queryKey: ["message", folder, selected], queryFn: () => getMessage(folder, selected!), enabled: Boolean(selected) });
  const select = (id: string) => { setSelected(id); setDetailOpen(true); const url = new URL(window.location.href); url.searchParams.set("message", id); window.history.pushState({}, "", url); };
  const closeDetail = () => { setDetailOpen(false); setSelected(null); const url = new URL(window.location.href); url.searchParams.delete("message"); window.history.pushState({}, "", url); };
  const currentMailbox = metadata.data?.folders.find((mailbox) => mailbox.name === folder) || { name: folder, delimiter: "/", attributes: [] };
  const folderTitle = folderLabel(locale, currentMailbox);
  const isJunkFolder = folderKind(currentMailbox) === "junk";
  const messages = (list.data?.messages || []).filter((message) => {
    const query = search.trim().toLowerCase();
    return !query || [message.from, message.fromName, message.to, message.subject, message.preview].some((value) => value?.toLowerCase().includes(query));
  });

  const messageAddress = (message: MailMessage) => {
    const from = message.from?.trim() || "";
    const accountEmail = message.accountEmail?.trim() || metadata.data?.accountEmail?.trim() || "";
    return accountEmail && from.toLowerCase() === accountEmail.toLowerCase()
      ? message.to || from || locale.me
      : from || message.to || locale.me;
  };
  const removeMessageFromView = async (message: MailMessage) => {
    if (selected === message.id) closeDetail();
    queryClient.removeQueries({ queryKey: ["message", folder, message.id] });
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["folder", folder] }),
      queryClient.invalidateQueries({ queryKey: ["conversations"] }),
    ]);
  };
  const restoreMutation = useMutation({
    mutationFn: (message: MailMessage) => restoreJunkMessage(folder, message.id, message.accountEmail || metadata.data?.accountEmail),
    onSuccess: async (_, message) => {
      await removeMessageFromView(message);
    },
    onError: (value) => toast.error(value instanceof Error ? value.message : locale.notSpamFailed),
  });
  const permanentDeleteMutation = useMutation({
    mutationFn: (message: MailMessage) => permanentlyDeleteJunkMessage(folder, message.id, message.accountEmail || metadata.data?.accountEmail),
    onSuccess: async (_, message) => {
      setPermanentDeleteTarget(null);
      setPermanentDeleteError("");
      await removeMessageFromView(message);
    },
    onError: (value) => setPermanentDeleteError(value instanceof Error ? value.message : locale.permanentDeleteFailed),
  });
  useEffect(() => {
    document.documentElement.classList.toggle("dark", darkMode);
    window.localStorage.setItem("lilmail-theme", darkMode ? "dark" : "light");
  }, [darkMode]);
  useEffect(() => {
    const restoreMessageFromURL = () => {
      const id = new URL(window.location.href).searchParams.get("message");
      setSelected(id);
      setDetailOpen(Boolean(id));
    };
    window.addEventListener("popstate", restoreMessageFromURL);
    return () => window.removeEventListener("popstate", restoreMessageFromURL);
  }, []);
  const authenticated = (metadata.error instanceof ApiError && metadata.error.status === 401) || (list.error instanceof ApiError && list.error.status === 401);
  if (authenticated) return <LoginScreen copy={locale} />;
  return (
    <div className="flex h-screen min-h-[32.5rem] overflow-hidden bg-background">
      {sidebarOpen && <button className="fixed inset-0 z-30 bg-black/10 supports-backdrop-filter:backdrop-blur-xs lg:hidden" aria-label={locale.cancel} onClick={() => setSidebarOpen(false)} />}
      <Sidebar copy={locale} folders={metadata.data?.folders || []} accounts={metadata.data?.accounts || []} accountEmail={metadata.data?.accountEmail || ""} calendarEnabled={capabilities.data?.calendar === true} currentFolder={folder} onCompose={() => setComposeOpen(true)} onSettings={() => { setSidebarOpen(false); setSettingsOpen(true); }} open={sidebarOpen} onClose={() => setSidebarOpen(false)} darkMode={darkMode} onToggleDarkMode={() => setDarkMode((value) => !value)} />
      <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
        <section className={cn("min-w-0 flex-1 flex-col border-r bg-card lg:w-[23.125rem] lg:flex-none", detailOpen ? "hidden lg:flex" : "flex")}>
          <div className="border-b bg-card px-3 py-3">
            <div className="flex items-center gap-2">
              <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={() => setSidebarOpen(true)} aria-label={locale.folders} title={locale.folders}><Menu /></Button>
              <div className="relative flex min-w-0 flex-1 items-center"><Search className="pointer-events-none absolute left-3 size-4 text-muted-foreground" /><Input type="search" className="h-9 bg-muted/60 pl-9 pr-9" value={search} onChange={(event) => setSearch(event.target.value)} placeholder={`${locale.search} · ${folderTitle}`} aria-label={locale.search} />{search && <Button variant="ghost" size="icon" className="absolute right-1 size-7" onClick={() => setSearch("")} aria-label={locale.cancel}><X /></Button>}</div>
            </div>
          </div>
          <ScrollArea className="min-h-0 flex-1">
            {list.isPending && <ListSkeleton />}
            {!list.isPending && list.error && <ErrorState copy={locale} onRetry={() => void list.refetch()} />}
            {!list.isPending && !list.error && messages.length === 0 && <EmptyState icon={<Mail />} text={locale.noConversations} />}
            {messages.map((message) => <FolderMessageRow key={message.id} copy={locale} message={message} address={messageAddress(message)} selected={selected === message.id} junkActions={isJunkFolder} actionPending={restoreMutation.isPending || permanentDeleteMutation.isPending} onSelect={() => select(message.id)} onNotSpam={() => restoreMutation.mutate(message)} onPermanentDelete={() => { setPermanentDeleteError(""); setPermanentDeleteTarget(message); }} />)}
          </ScrollArea>
        </section>
        <section className={cn("min-w-0 flex-1 flex-col bg-surface", detailOpen ? "flex" : "hidden lg:flex")}>
          {detailOpen && <header className="grid min-h-[4.5rem] grid-cols-[minmax(0,1fr)_minmax(0,2fr)_minmax(0,1fr)] items-center border-b bg-card px-3 py-3 sm:px-5"><div><Button variant="ghost" size="icon" className="lg:hidden" onClick={closeDetail} aria-label={locale.back}><ArrowLeft /></Button></div><h2 className="truncate text-center text-sm font-semibold">{detail.data?.subject || folderTitle}</h2><div /></header>}
          <ScrollArea className="min-h-0 flex-1" contentClassName="px-3 py-6 sm:px-[5vw] sm:py-8">{detail.isPending && selected ? <div className="grid h-full place-items-center text-sm text-muted-foreground">{locale.loading}</div> : detail.error ? <ErrorState copy={locale} onRetry={() => void detail.refetch()} /> : detail.data ? <MailDetail copy={locale} message={detail.data} /> : <EmptyState icon={<Mail />} text={locale.selectConversation} />}</ScrollArea>
        </section>
      </main>
      <ComposeDialog copy={locale} open={composeOpen} defaults={{ to: "", subject: "" }} accountEmail={metadata.data?.accountEmail || ""} onOpenChange={setComposeOpen} onSent={() => void list.refetch()} />
      <SettingsDialog copy={locale} open={settingsOpen} onOpenChange={setSettingsOpen} />
      <Dialog open={Boolean(permanentDeleteTarget)} onOpenChange={(open) => { if (!open && !permanentDeleteMutation.isPending) { setPermanentDeleteTarget(null); setPermanentDeleteError(""); } }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader><DialogTitle>{locale.permanentDeleteTitle}</DialogTitle><DialogDescription>{locale.permanentDeleteDescription}</DialogDescription></DialogHeader>
          {permanentDeleteError && <p className="text-sm text-destructive">{permanentDeleteError}</p>}
          <DialogFooter><Button variant="ghost" disabled={permanentDeleteMutation.isPending} onClick={() => setPermanentDeleteTarget(null)}>{locale.cancel}</Button><Button variant="destructive" disabled={permanentDeleteMutation.isPending || !permanentDeleteTarget} onClick={() => permanentDeleteTarget && permanentDeleteMutation.mutate(permanentDeleteTarget)}><Trash2 />{permanentDeleteMutation.isPending ? locale.deleting : locale.permanentDelete}</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function MailDetail({ copy, message }: { copy: Copy; message: MailMessage }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  return <article ref={scrollRef} className="mx-auto min-w-0 max-w-4xl"><div className="mb-1 flex items-baseline gap-2 text-xs text-muted-foreground"><span className="font-medium">{message.fromName || message.from}</span><time>{formatTime(message.date)}</time></div><div className="min-w-0 max-w-[80%] overflow-x-auto rounded-xl border bg-background px-3 py-2 text-sm leading-relaxed">{message.html ? <EmailHTMLFrame html={message.html} title={message.subject || copy.noSubject} rootRef={scrollRef} eager /> : <div className="whitespace-pre-wrap">{renderLinkifiedText(message.body || message.preview || copy.noBody)}</div>}{message.attachments?.length ? <><Separator className="my-2 opacity-50" /><div className="grid gap-1.5">{message.attachments.map((item) => <a className="flex min-w-0 items-center gap-1.5 text-xs text-primary" key={item.id || item.partId} href={`/api/attachment/${encodeURIComponent(item.id)}?account_email=${encodeURIComponent(message.accountEmail || "")}`}><Paperclip className="size-3.5 shrink-0" /><span className="truncate">{item.filename}</span><small className="shrink-0 text-muted-foreground">{formatSize(item.size)}</small></a>)}</div></> : null}</div><MailMessageSummary copy={copy} accountEmail={message.accountEmail} folder={message.folder || "INBOX"} messageId={message.id} initialSummary={message.mailSummary} /></article>;
}

function SettingsPage({ copy }: { copy: Copy }) {
  return <div className="min-h-screen bg-background"><PageHeader title={copy.settings} action={<Button asChild variant="ghost" size="sm"><a href="/inbox">{copy.conversations}</a></Button>} /><main className="mx-auto max-w-4xl p-4 py-8 lg:p-8"><SettingsContent copy={copy} /></main></div>;
}

function SettingsDialog({ copy, open, onOpenChange }: { copy: Copy; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="settings-dialog" className="flex max-h-[calc(100vh-2rem)] w-[calc(100%-2rem)] max-w-5xl flex-col gap-0 overflow-hidden p-0 sm:max-h-[calc(100vh-3rem)] sm:max-w-5xl">
        <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12 text-left">
          <DialogTitle>{copy.settings}</DialogTitle>
          <DialogDescription className="sr-only">{copy.settings}</DialogDescription>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1" contentClassName="p-5 sm:p-6">
          <SettingsContent copy={copy} />
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

function SettingsContent({ copy }: { copy: Copy }) {
  const [section, setSection] = useState<"general" | "signatures" | "ai" | "agents" | "mailboxes">("general");
  return (
    <div className="grid min-h-[32rem] md:grid-cols-[12rem_minmax(0,1fr)]">
      <nav className="flex gap-1 overflow-x-auto border-b pb-4 md:flex-col md:overflow-visible md:border-r md:border-b-0 md:pr-4" aria-label={copy.settings}>
        <Button className="shrink-0 justify-start" variant={section === "general" ? "secondary" : "ghost"} onClick={() => setSection("general")}><Settings />{copy.generalSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "signatures" ? "secondary" : "ghost"} onClick={() => setSection("signatures")}><SignatureIcon />{copy.signatureSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "ai" ? "secondary" : "ghost"} onClick={() => setSection("ai")}><Sparkles />{copy.aiSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "agents" ? "secondary" : "ghost"} onClick={() => setSection("agents")}><Bot />{copy.agentSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "mailboxes" ? "secondary" : "ghost"} onClick={() => setSection("mailboxes")}><Mail />{copy.mailboxManagement}</Button>
      </nav>
      <div className="min-w-0 pt-5 md:pt-0 md:pl-6">
        {section === "general" ? <GeneralSettings copy={copy} /> : section === "signatures" ? <SignatureSettings copy={copy} /> : section === "ai" ? <AISettings copy={copy} /> : section === "agents" ? <AgentSettings copy={copy} /> : <MailboxSettings copy={copy} />}
      </div>
    </div>
  );
}

function SignatureSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const signatures = useQuery({ queryKey: ["signatures"], queryFn: getSignatures, retry: false });
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<EmailSignature | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<EmailSignature | null>(null);
  const [name, setName] = useState("");
  const [isDefault, setIsDefault] = useState(false);
  const [contentEmpty, setContentEmpty] = useState(true);
  const [sourceMode, setSourceMode] = useState(false);
  const [sourceCode, setSourceCode] = useState("");
  const [error, setError] = useState("");
  const editor = useEditor({ extensions: [StarterKit, EmailImageExtension, UnderlineExtension, LinkExtension.configure({ openOnClick: false }), Placeholder.configure({ placeholder: copy.signatureContent })], content: "", immediatelyRender: false, onUpdate: ({ editor: currentEditor }) => setContentEmpty(currentEditor.isEmpty) });
  const persist = useMutation({
    mutationFn: ({ items }: { items: EmailSignature[]; operation: "create" | "update" | "delete" }) => saveSignatures(items),
    onSuccess: async (_, variables) => {
      setOpen(false);
      setEditing(null);
      setDeleteTarget(null);
      setError("");
      toast.success(variables.operation === "create" ? copy.signatureSaved : variables.operation === "update" ? copy.signatureUpdated : copy.signatureDeleted);
      await queryClient.invalidateQueries({ queryKey: ["signatures"] });
    },
    onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed),
  });

  useEffect(() => {
    if (!open || !editor) return;
    const html = editing?.html || "";
    setSourceMode(false);
    setSourceCode(html);
    editor.commands.setContent(html);
    setContentEmpty(editor.isEmpty);
    editor.commands.focus("start");
  }, [open, editing, editor]);

  const toggleSignatureSource = () => {
    if (!editor) return;
    if (sourceMode) {
      editor.commands.setContent(sourceCode);
      setContentEmpty(editor.isEmpty);
      setSourceMode(false);
      editor.commands.focus("start");
      return;
    }
    setSourceCode(editor.getHTML());
    setContentEmpty(editor.isEmpty);
    setSourceMode(true);
  };

  const openAdd = () => {
    setEditing(null);
    setName("");
    setIsDefault(!signatures.data?.signatures.length);
    setError("");
    setOpen(true);
  };
  const openEdit = (signature: EmailSignature) => {
    setEditing(signature);
    setName(signature.name);
    setIsDefault(Boolean(signature.default));
    setError("");
    setOpen(true);
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const html = sourceMode ? sourceCode : editor?.getHTML() || "";
    if (!name.trim() || !editor || (sourceMode ? !sourceCode.trim() : editor.isEmpty)) return;
    const current = signatures.data?.signatures || [];
    const makeDefault = isDefault || current.length === 0;
    const nextItem: EmailSignature = { id: editing?.id || "", name: name.trim(), html, default: makeDefault };
    const next = editing
      ? current.map((item) => item.id === editing.id ? nextItem : { ...item, default: makeDefault ? false : item.default })
      : [...current.map((item) => ({ ...item, default: makeDefault ? false : item.default })), nextItem];
    persist.mutate({ items: next, operation: editing ? "update" : "create" });
  };
  const removeSignature = () => {
    if (!deleteTarget) return;
    const remaining = (signatures.data?.signatures || []).filter((item) => item.id !== deleteTarget.id);
    const next = remaining.length && !remaining.some((item) => item.default)
      ? remaining.map((item, index) => ({ ...item, default: index === 0 }))
      : remaining;
    setError("");
    persist.mutate({ items: next, operation: "delete" });
  };

  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="flex items-center gap-2 text-lg font-semibold"><SignatureIcon className="size-5" />{copy.signatureSettings}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.signatureSettingsDescription}</p></div>
        <Button onClick={openAdd}><Plus />{copy.addSignature}</Button>
      </div>
      <div className="mt-5 overflow-x-auto rounded-lg border">
        <table className="w-full min-w-[36rem] text-left text-sm">
          <thead className="border-b bg-muted/60 text-xs text-muted-foreground"><tr><th className="px-4 py-2.5 font-medium">{copy.signatureName}</th><th className="px-4 py-2.5 font-medium">{copy.signaturePreview}</th><th className="px-4 py-2.5 text-right font-medium">{copy.actions}</th></tr></thead>
          <tbody>{signatures.isPending ? <tr><td className="px-4 py-8 text-center text-muted-foreground" colSpan={3}>{copy.loading}</td></tr> : signatures.data?.signatures.length ? signatures.data.signatures.map((signature) => <tr className="border-b last:border-b-0" key={signature.id}><td className="px-4 py-3"><div className="flex items-center gap-2"><span className="font-medium">{signature.name}</span>{signature.default && <Badge>{copy.defaultSignature}</Badge>}</div></td><td className="max-w-md px-4 py-3 text-muted-foreground"><p className="line-clamp-2 whitespace-pre-line">{htmlToPlainText(signature.html) || "-"}</p></td><td className="px-4 py-3"><div className="flex justify-end"><Button variant="ghost" size="icon" onClick={() => openEdit(signature)} aria-label={copy.editSignature} title={copy.editSignature}><Pencil /></Button><Button variant="ghost" size="icon" className="text-destructive" onClick={() => { setError(""); setDeleteTarget(signature); }} aria-label={copy.remove} title={copy.remove}><Trash2 /></Button></div></td></tr>) : <tr><td className="px-4 py-8 text-center text-muted-foreground" colSpan={3}>{copy.noSignatures}</td></tr>}</tbody>
        </table>
      </div>
      {(signatures.isError || error) && <p className="mt-3 text-xs text-destructive">{error || (signatures.error instanceof Error ? signatures.error.message : copy.loadFailed)}</p>}
      <Dialog open={open} onOpenChange={(next) => { if (!persist.isPending) setOpen(next); }}>
        <DialogContent className="flex max-h-[calc(100vh-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12"><DialogTitle>{editing ? copy.editSignature : copy.addSignature}</DialogTitle><DialogDescription>{copy.signatureSettingsDescription}</DialogDescription></DialogHeader>
          <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
            <ScrollArea className="min-h-0 flex-1" contentClassName="grid gap-4 p-5">
              <div className="grid gap-2"><Label htmlFor="signature-name">{copy.signatureName}</Label><Input id="signature-name" value={name} onChange={(event) => setName(event.target.value)} disabled={persist.isPending} required /></div>
              <div className="grid gap-2">
                <Label>{copy.signatureContent}</Label>
                <div className="overflow-hidden rounded-lg border bg-background">
                  <div className="flex items-center gap-1 border-b bg-muted px-2 py-1"><Button type="button" variant={sourceMode ? "secondary" : "ghost"} size="icon" disabled={persist.isPending || !editor} onClick={toggleSignatureSource} aria-label={sourceMode ? copy.richText : copy.sourceCode} title={sourceMode ? copy.richText : copy.sourceCode}><Code2 /></Button><Separator orientation="vertical" className="mx-1 h-5" /><RichTextButtons editor={editor} disabled={persist.isPending || sourceMode} /></div>
                  {sourceMode
                    ? <textarea className="min-h-64 w-full resize-y bg-background px-4 py-3 font-mono text-sm leading-6 outline-none" value={sourceCode} onChange={(event) => { setSourceCode(event.target.value); setContentEmpty(!event.target.value.trim()); }} spellCheck={false} aria-label={copy.sourceCode} />
                    : <div className="max-h-72 overflow-y-auto px-4 py-3"><EditorContent editor={editor} /></div>}
                </div>
              </div>
              <label className="flex items-center gap-2 text-sm"><input className="size-4 accent-primary" type="checkbox" checked={isDefault} disabled={persist.isPending} onChange={(event) => setIsDefault(event.target.checked)} />{copy.defaultSignature}</label>
              {error && <p className="text-xs text-destructive">{error}</p>}
            </ScrollArea>
            <DialogFooter className="shrink-0 border-t px-5 py-3"><Button type="button" variant="ghost" disabled={persist.isPending} onClick={() => setOpen(false)}>{copy.cancel}</Button><Button type="submit" disabled={persist.isPending || !name.trim() || contentEmpty}>{persist.isPending ? copy.savingSignature : copy.save}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(next) => { if (!next && !persist.isPending) setDeleteTarget(null); }}>
        <DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>{copy.deleteSignatureTitle}</DialogTitle><DialogDescription>{copy.deleteSignatureDescription}</DialogDescription></DialogHeader>{error && <p className="text-xs text-destructive">{error}</p>}<DialogFooter><Button type="button" variant="ghost" disabled={persist.isPending} onClick={() => setDeleteTarget(null)}>{copy.cancel}</Button><Button type="button" variant="destructive" disabled={persist.isPending} onClick={removeSignature}><Trash2 />{persist.isPending ? copy.deleting : copy.remove}</Button></DialogFooter></DialogContent>
      </Dialog>
    </section>
  );
}

function AgentSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const agents = useQuery({ queryKey: ["ai-agents"], queryFn: getAIAgents, retry: false });
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<AIAgent | null>(null);
  const [name, setName] = useState("");
  const [prompt, setPrompt] = useState("");
  const [error, setError] = useState("");
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["ai-agents"] });
  const create = useMutation({
    mutationFn: addAIAgent,
    onSuccess: () => { setOpen(false); setEditing(null); setError(""); toast.success(copy.agentSaved); refresh(); },
    onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed),
  });
  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: Parameters<typeof updateAIAgent>[1] }) => updateAIAgent(id, input),
    onSuccess: () => { setOpen(false); setEditing(null); setError(""); toast.success(copy.agentUpdated); refresh(); },
    onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed),
  });
  const openAdd = () => { setEditing(null); setName(""); setPrompt(""); setError(""); setOpen(true); };
  const openEdit = (agent: AIAgent) => { setEditing(agent); setName(agent.name); setPrompt(agent.prompt); setError(""); setOpen(true); };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    setError("");
    const input = { name: name.trim(), prompt: prompt.trim() };
    if (editing) update.mutate({ id: editing.id, input });
    else create.mutate(input);
  };
  const pending = create.isPending || update.isPending;
  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="flex items-center gap-2 text-lg font-semibold"><Bot className="size-5" />{copy.agentSettings}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.agentSettingsDescription}</p></div>
        <Button onClick={openAdd}><Plus />{copy.addAgent}</Button>
      </div>
      <div className="mt-5 overflow-x-auto rounded-lg border">
        <table className="w-full min-w-[36rem] text-left text-sm">
          <thead className="border-b bg-muted/60 text-xs text-muted-foreground"><tr><th className="px-4 py-2.5 font-medium">{copy.agentName}</th><th className="px-4 py-2.5 font-medium">{copy.agentPrompt}</th><th className="px-4 py-2.5 text-right font-medium">{copy.actions}</th></tr></thead>
          <tbody>{agents.isPending ? <tr><td className="px-4 py-8 text-center text-muted-foreground" colSpan={3}>{copy.loading}</td></tr> : agents.data?.agents.length ? agents.data.agents.map((agent) => <tr className="border-b last:border-b-0" key={agent.id}><td className="px-4 py-3 font-medium">{agent.name}</td><td className="max-w-md px-4 py-3 text-muted-foreground"><p className="line-clamp-2 whitespace-pre-line">{agent.prompt}</p></td><td className="px-4 py-3"><div className="flex justify-end"><Button variant="ghost" size="icon" onClick={() => openEdit(agent)} aria-label={copy.editAgent} title={copy.editAgent}><Pencil /></Button></div></td></tr>) : <tr><td className="px-4 py-8 text-center text-muted-foreground" colSpan={3}>{copy.noAgents}</td></tr>}</tbody>
        </table>
      </div>
      {(agents.isError || error) && <p className="mt-3 text-xs text-destructive">{error || (agents.error instanceof Error ? agents.error.message : copy.loadFailed)}</p>}
      <Dialog open={open} onOpenChange={(next) => { setOpen(next); if (!next) setEditing(null); }}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader><DialogTitle>{editing ? copy.editAgent : copy.addAgent}</DialogTitle><DialogDescription>{copy.agentSettingsDescription}</DialogDescription></DialogHeader>
          <form className="grid gap-4" onSubmit={submit}>
            <div className="grid gap-2"><Label htmlFor="agent-name">{copy.agentName}</Label><Input id="agent-name" value={name} onChange={(event) => setName(event.target.value)} disabled={pending} required /></div>
            <div className="grid gap-2"><Label htmlFor="agent-prompt">{copy.agentPrompt}</Label><Textarea id="agent-prompt" className="min-h-64 resize-y" value={prompt} onChange={(event) => setPrompt(event.target.value)} disabled={pending} required /></div>
            {(create.isError || update.isError) && error && <p className="text-xs text-destructive">{error}</p>}
            <DialogFooter><Button type="button" variant="ghost" onClick={() => setOpen(false)}>{copy.cancel}</Button><Button type="submit" disabled={pending || !name.trim() || !prompt.trim()}>{pending ? copy.savingAgent : editing ? copy.editAgent : copy.addAgent}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function AISettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const models = useQuery({ queryKey: ["ai-models"], queryFn: getAIModels, retry: false });
  const [addOpen, setAddOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<AIModel | null>(null);
  const [baseURL, setBaseURL] = useState("https://api.openai.com/v1");
  const [model, setModel] = useState("gpt-5.6-sol");
  const [apiKey, setAPIKey] = useState("");
  const [reasoningEffort, setReasoningEffort] = useState<"low" | "medium">("medium");
  const [error, setError] = useState("");
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["ai-models"] });
  };
  const add = useMutation({
    mutationFn: addAIModel,
    onSuccess: () => {
      setAPIKey("");
      setModel("gpt-5.6-sol");
      setBaseURL("https://api.openai.com/v1");
      setReasoningEffort("medium");
      setAddOpen(false);
      setError("");
      toast.success(copy.aiSettingsSaved);
      refresh();
    },
    onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed),
  });
  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: Parameters<typeof updateAIModel>[1] }) => updateAIModel(id, input),
    onSuccess: () => {
      setAPIKey("");
      setEditingModel(null);
      setAddOpen(false);
      setError("");
      toast.success(copy.aiModelUpdated);
      refresh();
    },
    onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed),
  });
  const remove = useMutation({ mutationFn: deleteAIModel, onSuccess: () => { setError(""); refresh(); }, onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed) });
  const makeDefault = useMutation({ mutationFn: setDefaultAIModel, onSuccess: () => { setError(""); refresh(); }, onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed) });
  const test = useMutation({
    mutationFn: ({ input, id }: { input: Parameters<typeof testAIModel>[0]; id?: string }) => id ? testSavedAIModel(id, input) : testAIModel(input),
    onSuccess: (result) => {
      setError("");
      toast.success(copy.aiModelTestSuccess, { description: `${result.latencyMs} ms · ${result.output}` });
    },
    onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed),
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    setError("");
    const input = { baseUrl: baseURL.trim(), model: model.trim(), apiKey: apiKey.trim(), reasoningEffort };
    if (editingModel) update.mutate({ id: editingModel.id, input });
    else add.mutate(input);
  };
  const openAdd = () => {
    setEditingModel(null);
    setBaseURL("https://api.openai.com/v1");
    setModel("gpt-5.6-sol");
    setReasoningEffort("medium");
    setAPIKey("");
    setError("");
    setAddOpen(true);
  };
  const openEdit = (item: AIModel) => {
    setEditingModel(item);
    setBaseURL(item.baseUrl);
    setModel(item.model);
    setReasoningEffort(item.reasoningEffort);
    setAPIKey("");
    setError("");
    setAddOpen(true);
  };
  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="flex items-center gap-2 text-lg font-semibold"><Sparkles className="size-5" />{copy.aiSettings}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.aiSettingsDescription}</p></div>
        <Button onClick={openAdd}><Plus />{copy.addAIModel}</Button>
      </div>
      <div className="mt-5 overflow-x-auto rounded-lg border">
        <table className="w-full min-w-[40rem] text-left text-sm">
          <thead className="border-b bg-muted/60 text-xs text-muted-foreground"><tr><th className="px-4 py-2.5 font-medium">{copy.aiModel}</th><th className="px-4 py-2.5 font-medium">{copy.aiProvider}</th><th className="px-4 py-2.5 font-medium">{copy.aiReasoningEffort}</th><th className="px-4 py-2.5 font-medium">{copy.aiBaseURL}</th><th className="px-4 py-2.5 text-right font-medium">{copy.actions}</th></tr></thead>
          <tbody>{models.isPending ? <tr><td className="px-4 py-8 text-center text-muted-foreground" colSpan={5}>{copy.loading}</td></tr> : models.data?.models.length ? models.data.models.map((item) => <tr className="border-b last:border-b-0" key={item.id}><td className="px-4 py-3"><div className="flex items-center gap-2"><span className="font-medium">{item.model}</span>{item.isDefault && <Badge>{copy.defaultModel}</Badge>}</div></td><td className="px-4 py-3">OpenAI</td><td className="px-4 py-3">{item.reasoningEffort === "low" ? copy.aiReasoningLow : copy.aiReasoningMedium}</td><td className="max-w-64 truncate px-4 py-3 text-muted-foreground" title={item.baseUrl}>{item.baseUrl}</td><td className="px-4 py-3"><div className="flex justify-end gap-2">{!item.isDefault && <Button variant="outline" size="sm" disabled={makeDefault.isPending} onClick={() => makeDefault.mutate(item.id)}>{copy.setDefaultModel}</Button>}<Button variant="ghost" size="icon" onClick={() => openEdit(item)} aria-label={copy.editAIModel} title={copy.editAIModel}><Pencil /></Button><Button variant="ghost" size="icon" className="text-destructive" disabled={remove.isPending} onClick={() => remove.mutate(item.id)} aria-label={copy.remove} title={copy.remove}><Trash2 /></Button></div></td></tr>) : <tr><td className="px-4 py-8 text-center text-muted-foreground" colSpan={5}>{copy.noAIModels}</td></tr>}</tbody>
        </table>
      </div>
      {(models.isError || error) && <p className="mt-3 text-xs text-destructive">{error || (models.error instanceof Error ? models.error.message : copy.loadFailed)}</p>}
      <Dialog open={addOpen} onOpenChange={(open) => { setAddOpen(open); if (!open) setEditingModel(null); }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader><DialogTitle>{editingModel ? copy.editAIModel : copy.addAIModel}</DialogTitle><DialogDescription>{copy.aiSettingsDescription}</DialogDescription></DialogHeader>
          <form className="grid gap-4" onSubmit={submit}>
            <div className="grid gap-2"><Label htmlFor="add-ai-provider">{copy.aiProvider}</Label><Input id="add-ai-provider" value="OpenAI" disabled /></div>
            <div className="grid gap-2"><Label htmlFor="add-ai-base-url">{copy.aiBaseURL}</Label><Input id="add-ai-base-url" type="url" value={baseURL} required disabled={add.isPending || update.isPending || test.isPending} onChange={(event) => setBaseURL(event.target.value)} placeholder="https://api.openai.com/v1" /></div>
            <div className="grid gap-2"><Label htmlFor="add-ai-model">{copy.aiModel}</Label><Input id="add-ai-model" value={model} required disabled={add.isPending || update.isPending || test.isPending} onChange={(event) => setModel(event.target.value)} placeholder="gpt-5.6-sol" /></div>
            <div className="grid gap-2"><Label htmlFor="add-ai-reasoning">{copy.aiReasoningEffort}</Label><Select value={reasoningEffort} onValueChange={(value) => setReasoningEffort(value as "low" | "medium")} disabled={add.isPending || update.isPending || test.isPending}><SelectTrigger id="add-ai-reasoning" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="low">{copy.aiReasoningLow}</SelectItem><SelectItem value="medium">{copy.aiReasoningMedium}</SelectItem></SelectContent></Select></div>
            <div className="grid gap-2"><Label htmlFor="add-ai-api-key">{copy.aiAPIKey}</Label><Input id="add-ai-api-key" type="password" value={apiKey} required={!editingModel} disabled={add.isPending || update.isPending || test.isPending} onChange={(event) => setAPIKey(event.target.value)} placeholder={editingModel ? copy.aiAPIKeyKeep : "sk-..."} autoComplete="off" /></div>
            {(add.isError || update.isError || test.isError) && error && <p className="text-xs text-destructive">{error}</p>}
            <DialogFooter><Button type="button" variant="ghost" onClick={() => setAddOpen(false)}>{copy.cancel}</Button><Button type="button" variant="outline" disabled={test.isPending || add.isPending || update.isPending || !baseURL.trim() || !model.trim() || (!editingModel && !apiKey.trim())} onClick={() => { setError(""); test.mutate({ id: editingModel?.id, input: { baseUrl: baseURL.trim(), model: model.trim(), apiKey: apiKey.trim(), reasoningEffort } }); }}>{test.isPending ? copy.aiModelTesting : copy.aiModelTest}</Button><Button type="submit" disabled={add.isPending || update.isPending || test.isPending || !baseURL.trim() || !model.trim() || (!editingModel && !apiKey.trim())}>{editingModel ? (update.isPending ? copy.updatingAIModel : copy.editAIModel) : (add.isPending ? copy.addingAIModel : copy.addAIModel)}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function GeneralSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities, retry: false });
  const webhook = useQuery({ queryKey: ["feishu-webhook"], queryFn: getFeishuWebhookSettings, retry: false });
  const [webhookEnabled, setWebhookEnabled] = useState(false);
  const [webhookURL, setWebhookURL] = useState("");
  const [webhookMessage, setWebhookMessage] = useState("");
  const [pushEnabled, setPushEnabled] = useState(false);
  const [pushMessage, setPushMessage] = useState("");
  useEffect(() => { void currentPushSubscription().then((subscription) => setPushEnabled(Boolean(subscription))); }, []);
  useEffect(() => {
    if (!webhook.data) return;
    setWebhookEnabled(webhook.data.enabled);
    setWebhookURL(webhook.data.url);
  }, [webhook.data]);
  const saveWebhook = useMutation({
    mutationFn: saveFeishuWebhookSettings,
    onSuccess: (settings) => {
      queryClient.setQueryData(["feishu-webhook"], settings);
      setWebhookMessage("");
      toast.success(copy.feishuWebhookSaved);
    },
    onError: (value) => setWebhookMessage(value instanceof Error ? value.message : copy.loadFailed),
  });
  const testWebhook = useMutation({
    mutationFn: testFeishuWebhook,
    onSuccess: () => {
      setWebhookMessage("");
      toast.success(copy.feishuWebhookTestSent);
    },
    onError: (value) => setWebhookMessage(value instanceof Error ? value.message : copy.loadFailed),
  });
  const persistWebhook = (enabled = webhookEnabled, url = webhookURL) => {
    const settings = { enabled, url: url.trim() };
    if (settings.enabled && !settings.url) {
      setWebhookMessage(copy.feishuWebhookURLRequired);
      return;
    }
    if (webhook.data?.enabled === settings.enabled && webhook.data.url === settings.url) return;
    setWebhookMessage("");
    saveWebhook.mutate(settings);
  };
  const togglePush = async () => {
    setPushMessage("");
    try {
      if (pushEnabled) await disableWebPush();
      else await enableWebPush(navigator.language);
      setPushEnabled(!pushEnabled);
    } catch (value) {
      setPushMessage(value instanceof Error ? value.message : copy.pushUnavailable);
    }
  };
  const pushAvailable = supportsWebPush() && capabilities.data?.webPush === true;
  const currentLanguage = copy === en ? "en" : "zh-CN";
  return (
    <div className="max-w-2xl">
      <h2 className="text-lg font-semibold">{copy.generalSettings}</h2>
      <section className="mt-6">
        <h3 className="flex items-center gap-2 text-sm font-semibold"><Languages className="size-4" />{copy.language}</h3>
        <div className="mt-3 inline-flex items-center rounded-lg border bg-muted p-0.5">
          <Button className={cn(currentLanguage === "zh-CN" && "bg-background shadow-sm ring-1 ring-border hover:bg-background")} variant="ghost" size="sm" aria-pressed={currentLanguage === "zh-CN"} onClick={() => switchLanguage("zh-CN")}>{currentLanguage === "zh-CN" && <Check />}简体中文</Button>
          <Button className={cn(currentLanguage === "en" && "bg-background shadow-sm ring-1 ring-border hover:bg-background")} variant="ghost" size="sm" aria-pressed={currentLanguage === "en"} onClick={() => switchLanguage("en")}>{currentLanguage === "en" && <Check />}English</Button>
        </div>
      </section>
      <section className="mt-7 border-t pt-6">
        <h3 className="text-sm font-semibold">{copy.pushNotifications}</h3>
        <Button className="mt-3 max-w-full whitespace-normal" variant="secondary" disabled={!pushAvailable} onClick={() => void togglePush()}>{pushEnabled ? <BellOff /> : <Bell />}{pushEnabled ? copy.disablePush : copy.enablePush}</Button>
        {pushMessage && <p className="mt-2 text-xs text-destructive">{pushMessage}</p>}
        {!pushAvailable && !capabilities.isPending && <p className="mt-2 text-xs text-muted-foreground">{copy.pushUnavailable}</p>}
      </section>
      <section className="mt-7 border-t pt-6">
        <h3 className="flex items-center gap-2 text-sm font-semibold"><MessageCircle className="size-4" />{copy.feishuWebhook}</h3>
        <p className="mt-1 text-sm text-muted-foreground">{copy.feishuWebhookDescription}</p>
        <div className="mt-4 grid max-w-xl gap-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <label className="flex items-center gap-2 text-sm">
              <input className="size-4 accent-primary" type="checkbox" checked={webhookEnabled} disabled={webhook.isPending || saveWebhook.isPending} onChange={(event) => { setWebhookEnabled(event.target.checked); setWebhookMessage(""); }} onBlur={() => persistWebhook()} />
              {copy.feishuWebhookEnabled}
            </label>
            <Button type="button" variant="outline" size="sm" disabled={webhook.isPending || testWebhook.isPending || !webhookURL.trim()} onClick={() => { setWebhookMessage(""); testWebhook.mutate(webhookURL.trim()); }}><Send />{testWebhook.isPending ? copy.feishuWebhookTesting : copy.feishuWebhookTest}</Button>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="feishu-webhook-url">{copy.feishuWebhookURL}</Label>
            <Input id="feishu-webhook-url" type="url" inputMode="url" autoComplete="off" placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..." value={webhookURL} disabled={webhook.isPending || saveWebhook.isPending} onChange={(event) => { setWebhookURL(event.target.value); setWebhookMessage(""); }} onBlur={() => persistWebhook()} onKeyDown={(event) => { if (event.key === "Enter") event.currentTarget.blur(); }} />
          </div>
          {webhook.isError && <p className="text-xs text-destructive">{webhook.error instanceof Error ? webhook.error.message : copy.feishuWebhookUnavailable}</p>}
          {webhookMessage && <p className={cn("text-xs", saveWebhook.isError || testWebhook.isError ? "text-destructive" : "text-muted-foreground")}>{webhookMessage}</p>}
        </div>
      </section>
    </div>
  );
}

const emptyAccountForm = { email: "", password: "", label: "", color: "#4f46e5", imap_server: "", imap_port: 993, smtp_server: "", smtp_port: 587 };

function MailboxSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: getAccounts, retry: false });
  const [addOpen, setAddOpen] = useState(false);
  const [error, setError] = useState("");
  const refreshAccountData = () => {
    void queryClient.invalidateQueries({ queryKey: ["accounts"] });
    void queryClient.invalidateQueries({ queryKey: ["conversations"] });
  };
  const remove = useMutation({ mutationFn: deleteAccount, onSuccess: () => { setError(""); refreshAccountData(); }, onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed) });
  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="text-lg font-semibold">{copy.mailboxManagement}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.mailboxDescription}</p></div>
        <Button onClick={() => setAddOpen(true)}><Plus />{copy.addAccount}</Button>
      </div>
      <div className="mt-5 overflow-x-auto rounded-lg border">
        <table className="w-full min-w-[44rem] text-left text-sm">
          <thead className="border-b bg-muted/60 text-xs text-muted-foreground"><tr><th className="px-4 py-2.5 font-medium">{copy.account}</th><th className="px-4 py-2.5 font-medium">IMAP</th><th className="px-4 py-2.5 font-medium">SMTP</th><th className="px-4 py-2.5 text-right font-medium">{copy.actions}</th></tr></thead>
          <tbody>
            {accounts.data?.accounts.map((account: ConnectedAccount) => (
              <tr className="border-b last:border-b-0" key={account.email}>
                <td className="px-4 py-3"><div className="flex min-w-0 items-center gap-2.5"><span className="size-3 shrink-0 rounded-full" style={{ backgroundColor: account.color || "#777" }} /><div className="min-w-0"><strong className="block max-w-52 truncate font-medium">{account.label || account.email}</strong><span className="block max-w-52 truncate text-xs text-muted-foreground">{account.email}</span></div></div></td>
                <td className="px-4 py-3 whitespace-nowrap">{account.imapServer}{account.imapPort ? `:${account.imapPort}` : ""}</td>
                <td className="px-4 py-3 whitespace-nowrap">{account.smtpServer || "-"}{account.smtpPort ? `:${account.smtpPort}` : ""}</td>
                <td className="px-4 py-3 text-right"><Button variant="ghost" size="sm" disabled={remove.isPending} onClick={() => remove.mutate(account.email)}>{copy.remove}</Button></td>
              </tr>
            ))}
            {!accounts.isPending && !accounts.data?.accounts.length && <tr><td className="px-4 py-10 text-center text-muted-foreground" colSpan={4}>{copy.noAccounts}</td></tr>}
          </tbody>
        </table>
      </div>
      {accounts.isPending && <p className="py-3 text-sm text-muted-foreground">{copy.loading}</p>}
      {accounts.error && <p className="py-3 text-sm text-destructive">{accounts.error.message}</p>}
      {error && <p className="py-2 text-xs text-destructive">{error}</p>}
      <AddAccountDialog copy={copy} open={addOpen} onOpenChange={setAddOpen} onAdded={refreshAccountData} />
    </section>
  );
}

function AddAccountDialog({ copy, open, onOpenChange, onAdded }: { copy: Copy; open: boolean; onOpenChange: (open: boolean) => void; onAdded: () => void }) {
  const [form, setForm] = useState({ ...emptyAccountForm });
  const [error, setError] = useState("");
  const changeOpen = (value: boolean) => {
    if (!value) setError("");
    onOpenChange(value);
  };
  const add = useMutation({ mutationFn: () => addAccount(form), onSuccess: () => { setForm({ ...emptyAccountForm }); setError(""); onAdded(); onOpenChange(false); }, onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed) });
  const field = (name: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) => {
    const value = event.target.type === "number" ? Number(event.target.value) : event.target.value;
    setForm((current) => ({ ...current, [name]: value }));
  };
  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent className="flex max-h-[calc(100vh-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12"><DialogTitle>{copy.addAccount}</DialogTitle><DialogDescription>{copy.mailboxDescription}</DialogDescription></DialogHeader>
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={(event) => { event.preventDefault(); setError(""); add.mutate(); }}>
          <ScrollArea className="min-h-0 flex-1" contentClassName="grid gap-5 p-5">
            <div className="grid gap-3 sm:grid-cols-2">
              <Label className="grid gap-1.5">{copy.email}<Input type="email" value={form.email} onChange={field("email")} autoComplete="email" required /></Label>
              <Label className="grid gap-1.5">{copy.password}<Input type="password" value={form.password} onChange={field("password")} autoComplete="new-password" required /></Label>
              <Label className="grid gap-1.5">{copy.displayName} ({copy.optional})<Input value={form.label} onChange={field("label")} /></Label>
              <Label className="grid gap-1.5">{copy.color}<Input className="p-1" type="color" value={form.color} onChange={field("color")} /></Label>
            </div>
            <div className="grid gap-3 border-t pt-5 sm:grid-cols-[minmax(0,1fr)_8rem]">
              <Label className="grid gap-1.5">{copy.imapServer}<Input value={form.imap_server} onChange={field("imap_server")} placeholder="imap.example.com" /></Label>
              <Label className="grid gap-1.5">{copy.imapPort}<Input type="number" min={1} max={65535} value={form.imap_port} onChange={field("imap_port")} required /></Label>
              <Label className="grid gap-1.5">{copy.smtpServer}<Input value={form.smtp_server} onChange={field("smtp_server")} placeholder="smtp.example.com" /></Label>
              <Label className="grid gap-1.5">{copy.smtpPort}<Input type="number" min={1} max={65535} value={form.smtp_port} onChange={field("smtp_port")} required /></Label>
            </div>
            {error && <p className="text-xs text-destructive">{error}</p>}
          </ScrollArea>
          <DialogFooter className="shrink-0 border-t px-5 py-3"><Button type="button" variant="ghost" onClick={() => changeOpen(false)}>{copy.cancel}</Button><Button type="submit" disabled={add.isPending}><Plus />{add.isPending ? copy.adding : copy.addAccount}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function CalendarPage() {
  const queryClient = useQueryClient();
  const metadata = useQuery({ queryKey: ["conversations", "calendar-shell"], queryFn: () => getConversations() });
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities });
  const copy = useLocale(metadata.data?.locale);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [darkMode, setDarkMode] = useState(prefersDarkMode);
  const now = new Date();
  const start = new Date(now.getFullYear(), now.getMonth(), 1).toISOString();
  const end = new Date(now.getFullYear(), now.getMonth() + 1, 1).toISOString();
  const events = useQuery({ queryKey: ["calendar", start], queryFn: () => getCalendarEvents(start, end), retry: false });
  const [form, setForm] = useState({ summary: "", location: "", start: "", end: "" });
  const create = useMutation({ mutationFn: () => createCalendarEvent({ ...form, start: new Date(form.start).toISOString(), end: new Date(form.end).toISOString(), allDay: false }), onSuccess: () => { setForm({ summary: "", location: "", start: "", end: "" }); void queryClient.invalidateQueries({ queryKey: ["calendar"] }); } });
  useEffect(() => {
    document.documentElement.classList.toggle("dark", darkMode);
    window.localStorage.setItem("lilmail-theme", darkMode ? "dark" : "light");
  }, [darkMode]);
  if (metadata.error instanceof ApiError && metadata.error.status === 401) return <LoginScreen copy={copy} />;
  return (
    <div className="flex h-screen min-h-[32.5rem] overflow-hidden bg-background">
      {sidebarOpen && <button className="fixed inset-0 z-30 bg-black/10 supports-backdrop-filter:backdrop-blur-xs lg:hidden" aria-label={copy.cancel} onClick={() => setSidebarOpen(false)} />}
      <Sidebar copy={copy} folders={metadata.data?.folders || []} accounts={metadata.data?.accounts || []} accountEmail={metadata.data?.accountEmail || ""} calendarEnabled={capabilities.data?.calendar === true} currentView="calendar" onCompose={() => setComposeOpen(true)} onSettings={() => { setSidebarOpen(false); setSettingsOpen(true); }} open={sidebarOpen} onClose={() => setSidebarOpen(false)} darkMode={darkMode} onToggleDarkMode={() => setDarkMode((value) => !value)} />
      <ScrollArea className="min-w-0 flex-1 bg-background" contentClassName="min-h-full" render={<main />}>
        <header className="sticky top-0 z-10 flex h-14 items-center gap-3 border-b bg-card px-3 sm:px-5">
          <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={() => setSidebarOpen(true)} aria-label={copy.folders} title={copy.folders}><Menu /></Button>
          <h1 className="text-sm font-semibold">{copy.calendar}</h1>
        </header>
        <div className="mx-auto grid max-w-5xl gap-8 p-4 py-8 lg:grid-cols-[minmax(0,1fr)_22rem] lg:p-8">
          <section><h2 className="text-lg font-semibold">{now.toLocaleDateString(copy === en ? "en" : "zh-CN", { month: "long", year: "numeric" })}</h2><div className="mt-5 grid gap-2">{events.data?.events.map((event: CalendarEvent) => <article className="grid grid-cols-[6rem_minmax(0,1fr)] gap-4 border-b py-3" key={event.uid}><time className="text-xs text-muted-foreground">{formatTime(event.start)}</time><div className="min-w-0"><strong className="block truncate text-sm">{event.summary}</strong>{event.location && <p className="mt-1 truncate text-xs text-muted-foreground">{event.location}</p>}</div></article>)}{events.isPending && <p>{copy.loading}</p>}{events.error && <p className="text-sm text-destructive">{events.error.message}</p>}</div></section>
          <form className="grid content-start gap-3 border-t pt-6 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-8" onSubmit={(event) => { event.preventDefault(); create.mutate(); }}><h2 className="font-semibold">{copy.newEvent}</h2><Label className="grid gap-1.5">{copy.subject}<Input value={form.summary} onChange={(event) => setForm({ ...form, summary: event.target.value })} required /></Label><Label className="grid gap-1.5">{copy.location}<Input value={form.location} onChange={(event) => setForm({ ...form, location: event.target.value })} /></Label><Label className="grid gap-1.5">{copy.start}<Input type="datetime-local" value={form.start} onChange={(event) => setForm({ ...form, start: event.target.value })} required /></Label><Label className="grid gap-1.5">{copy.end}<Input type="datetime-local" value={form.end} onChange={(event) => setForm({ ...form, end: event.target.value })} required /></Label><Button disabled={create.isPending}>{copy.save}</Button></form>
        </div>
      </ScrollArea>
      <ComposeDialog copy={copy} open={composeOpen} defaults={{ to: "", subject: "" }} accountEmail={metadata.data?.accountEmail || ""} onOpenChange={setComposeOpen} onSent={() => void queryClient.invalidateQueries({ queryKey: ["conversations"] })} />
      <SettingsDialog copy={copy} open={settingsOpen} onOpenChange={setSettingsOpen} />
    </div>
  );
}

function ListSkeleton() {
  return <div className="grid">{Array.from({ length: 7 }, (_, index) => <div className="grid gap-2 border-b px-4 py-3" key={index}><Skeleton className="h-2.5 w-2/5" /><Skeleton className="h-2.5 w-4/5" /></div>)}</div>;
}

function ErrorState({ copy, onRetry }: { copy: Copy; onRetry?: () => void }) {
  return <div className="flex min-h-56 flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground"><TriangleAlert className="size-8 text-primary" /><strong className="text-sm font-medium">{copy.loadFailed}</strong>{onRetry && <Button variant="secondary" size="sm" onClick={onRetry}>{copy.retry}</Button>}</div>;
}

function EmptyState({ icon, text }: { icon: ReactNode; text: string }) {
  return <div className="flex min-h-56 flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">{icon}<p className="m-0 text-sm">{text}</p></div>;
}

export default App;
