import { Fragment, useEffect, useLayoutEffect, useRef, useState, type FormEvent, type MutableRefObject, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArrowLeft,
  CalendarDays,
  Check,
  ChevronDown,
  ChevronRight,
  Code2,
  Copy as CopyIcon,
  FilePenLine,
  FileArchive,
  FileImage,
  FileSpreadsheet,
  FileText,
  Folder,
  Download,
  ExternalLink,
  Eye,
  Italic,
  ImagePlus,
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
  Paperclip,
  Pencil,
  Plus,
  Search,
  RotateCcw,
  ReplyAll,
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
import LinkExtension from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import StarterKit from "@tiptap/starter-kit";
import UnderlineExtension from "@tiptap/extension-underline";
import { toast } from "sonner";
import { EmailParagraph } from "./extensions/email-paragraph";
import { EmailImage } from "./extensions/email-image";
import { EmailSignature as EmailSignatureExtension } from "./extensions/email-signature";
import { ReplyQuote } from "./extensions/reply-quote";
import { ApiError, addAccount, addAIAgent, addAIModel, createCalendarEvent, deleteAccount, deleteAIModel, deleteConversation, deleteConversationMessage, generateEmail, getAccounts, getAIAgents, getAITaskBindings, getAIModels, getCalendarEvents, getCapabilities, getConversation, getConversations, getFeishuWebhookSettings, getFolderMessages, getMailAttachments, getMessage, getPublicSettings, getSignatures, getSystemSettings, markConversationRead, markConversationUnread, markMailMessageRead, permanentlyDeleteJunkMessage, register, restoreJunkMessage, saveAITaskBinding, saveConversationNote, saveFeishuWebhookSettings, saveSignatures, sendMessage, setDefaultAIModel, signIn, signOut, summarizeMailMessage, switchAccount, switchLanguage, testAIModel, testFeishuWebhook, testSavedAIModel, updateAIAgent, updateAIModel, updateRegistrationOpen, updateSystemUserRole, type AIAgent, type AITaskBinding, type AIModel, type EmailSignature, type SystemSettings as SystemSettingsData, type UserRole } from "./lib/api";
import { currentPushSubscription, disableWebPush, enableWebPush, supportsWebPush } from "./lib/push";
import { cn, formatSize, formatTime, isSentMailbox, linkifyText, splitQuotedText } from "./lib/utils";
import { Badge } from "./components/ui/badge";
import { Button } from "./components/ui/button";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "./components/ui/context-menu";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "./components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "./components/ui/dropdown-menu";
import { Input } from "./components/ui/input";
import { Label } from "./components/ui/label";
import { Separator } from "./components/ui/separator";
import { ScrollArea } from "./components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./components/ui/select";
import { Skeleton } from "./components/ui/skeleton";
import { Switch } from "./components/ui/switch";
import { Pagination, PaginationContent, PaginationEllipsis, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious } from "./components/ui/pagination";
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from "./components/ui/popover";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./components/ui/table";
import { Textarea } from "./components/ui/textarea";
import type { CalendarEvent, ConnectedAccount, ConversationDetail, ConversationDetailResponse, ConversationMessage, ConversationSummary, ConversationListResponse, MailAttachment, MailMessage, Mailbox, MailSummary } from "./types";

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
  attachmentManager: "附件管理",
  attachmentSearch: "搜索文件名、邮件主题或发件人",
  attachmentCount: "个附件",
  noAttachments: "暂无附件",
  allAttachmentTypes: "全部类型",
  attachmentImages: "图片",
  attachmentPDF: "PDF",
  attachmentDocuments: "文档",
  attachmentSpreadsheets: "表格",
  attachmentArchives: "压缩包",
  attachmentName: "文件名",
  attachmentMessage: "所属邮件",
  attachmentSender: "发件人",
  attachmentSize: "大小",
  attachmentDate: "日期",
  attachmentActions: "操作",
  attachmentPage: "页码",
  previewAttachment: "预览附件",
  downloadAttachment: "下载附件",
  viewOriginalMessage: "查看原邮件",
  previousPage: "上一页",
  nextPage: "下一页",
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
  replyAll: "回复全部",
  send: "发送",
  cancel: "取消",
  to: "收件人",
  cc: "抄送",
  subject: "主题",
  writeMessage: "写下邮件内容…",
  sending: "正在发送…",
  attach: "添加附件",
  insertImage: "插入图片",
  unsupportedImage: "仅支持 JPEG、PNG 和 GIF 图片",
  inlineImageMissing: "正文中有找不到原始文件的内嵌图片，请删除后重新插入",
  login: "登录",
  appAccount: "应用账号",
  password: "密码",
  loginFailed: "登录失败",
  invalidCredentials: "账号或密码错误",
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
  mailSummaryTitle: "邮件总结",
  regenerateSummary: "重新总结",
  summaryStale: "配置已更新",
  agentSettings: "智能体",
  agentSettingsDescription: "系统提示词、智能体提示词和输出标签共同组成最终提示词。",
  addAgent: "新增智能体",
  editAgent: "编辑智能体",
  agentName: "名称",
  agentPrompt: "提示词",
  agentOutputLabels: "输出标签",
  agentOutputLabelsPlaceholder: "输入标签后按回车，例如：客户",
  invalidAgentOutputLabel: "标签不能包含冒号，最多 12 个标签，每个不超过 20 个字",
  agentOutputLabelsDescription: "不填写时按普通智能体使用；邮件总结需要标签，邮件撰写可以留空。",
  removeAgentOutputLabel: "删除输出标签",
  noAgents: "暂无智能体",
  agentSaved: "智能体已新增",
  agentUpdated: "智能体已更新",
  savingAgent: "正在保存…",
  mailboxAIConfiguration: "邮箱 AI 配置",
  mailboxAIConfigurationDescription: "为每个已绑定邮箱分别配置邮件总结、邮件撰写和建议回复使用的智能体与模型。",
  mailSummaryAgent: "邮件总结智能体",
  emailDraftAgent: "邮件撰写智能体",
  replySuggestionAgent: "建议回复智能体",
  editReplySuggestionPrompt: "编辑建议回复提示词",
  replySuggestionPromptDescription: "编辑当前建议回复智能体的提示词。使用同一智能体的其他功能也会受到影响。",
  aiTask: "AI 功能",
  inheritedConfiguration: "兼容配置",
  explicitConfiguration: "已配置",
  mailboxAIConfigurationSaved: "邮箱 AI 配置已保存",
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
  systemSettings: "系统设置",
  systemSettingsDescription: "管理应用用户及其系统权限。",
  systemVersion: "系统版本",
  openRegistration: "开放注册",
  openRegistrationDescription: "允许访客创建新的应用账号。",
  registrationSettingUpdated: "注册设置已更新",
  registrationClosed: "当前系统暂未开放注册。",
  userManagement: "用户与权限",
  role: "角色",
  ordinaryUser: "普通用户",
  superAdmin: "超级管理员",
  currentUser: "当前账号",
  roleUpdated: "用户角色已更新",
  noUsers: "暂无用户",
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
  aiWriteEmail: "AI 编写邮件",
  aiWriteDescription: "补充希望 AI 如何编写这封邮件。",
  aiInstruction: "补充说明",
  aiInstructionPlaceholder: "例如：语气专业简洁，确认周二下午三点可以参会",
  includeConversation: "附带历史邮件",
  noConversationContext: "当前没有可附带的邮件聊天记录",
  generate: "生成",
  generating: "正在生成…",
  aiGenerateFailed: "邮件生成失败",
  aiGenerateRequest: "请根据当前邮件信息生成一封邮件",
  copyAIContent: "复制内容",
  copiedAIContent: "已复制",
  copyAIContentFailed: "复制失败",
  generatedDraft: "生成结果",
  regenerate: "再次生成",
  useGeneratedDraft: "采用",
  refineInstructionPlaceholder: "继续补充修改要求，例如：再简短一些，并明确回复截止时间",
  suggestedReply: "建议回复",
  generateSuggestedReply: "生成回复",
  suggestedReplyGenerating: "正在生成建议回复…",
  regenerateSuggestedReply: "重新生成建议回复",
  useSuggestedReply: "采用并回复",
  suggestedReplyFailed: "建议回复生成失败",
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
  attachmentManager: "Attachments",
  attachmentSearch: "Search filename, message subject, or sender",
  attachmentCount: "attachments",
  noAttachments: "No attachments",
  allAttachmentTypes: "All types",
  attachmentImages: "Images",
  attachmentPDF: "PDF",
  attachmentDocuments: "Documents",
  attachmentSpreadsheets: "Spreadsheets",
  attachmentArchives: "Archives",
  attachmentName: "Filename",
  attachmentMessage: "Message",
  attachmentSender: "Sender",
  attachmentSize: "Size",
  attachmentDate: "Date",
  attachmentActions: "Actions",
  attachmentPage: "Page",
  previewAttachment: "Preview attachment",
  downloadAttachment: "Download attachment",
  viewOriginalMessage: "View original message",
  previousPage: "Previous page",
  nextPage: "Next page",
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
  replyAll: "Reply all",
  send: "Send",
  cancel: "Cancel",
  to: "To",
  cc: "Cc",
  subject: "Subject",
  writeMessage: "Write your message…",
  sending: "Sending…",
  attach: "Attach file",
  insertImage: "Insert image",
  unsupportedImage: "Only JPEG, PNG, and GIF images are supported",
  inlineImageMissing: "An inline image is missing its original file. Remove it and insert it again",
  login: "Sign in",
  appAccount: "Application account",
  password: "Password",
  loginFailed: "Sign in failed",
  invalidCredentials: "Incorrect account or password",
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
  mailSummaryTitle: "Mail summary",
  regenerateSummary: "Regenerate summary",
  summaryStale: "Configuration changed",
  agentSettings: "Agents",
  agentSettingsDescription: "The system prompt, agent prompt, and output labels are combined into the final prompt.",
  addAgent: "Add agent",
  editAgent: "Edit agent",
  agentName: "Name",
  agentPrompt: "Prompt",
  agentOutputLabels: "Output labels",
  agentOutputLabelsPlaceholder: "Type a label and press Enter, e.g. Customer",
  invalidAgentOutputLabel: "Labels cannot contain a colon and must be at most 20 characters; up to 12 labels",
  agentOutputLabelsDescription: "Leave empty for a regular agent; summaries require labels, while email drafting does not.",
  removeAgentOutputLabel: "Remove output label",
  noAgents: "No agents configured",
  agentSaved: "Agent added",
  agentUpdated: "Agent updated",
  savingAgent: "Saving…",
  mailboxAIConfiguration: "Mailbox AI configuration",
  mailboxAIConfigurationDescription: "Configure the agent and model used for mail summaries, email drafting, and suggested replies in each connected mailbox.",
  mailSummaryAgent: "Mail summary agent",
  emailDraftAgent: "Email drafting agent",
  replySuggestionAgent: "Suggested reply agent",
  editReplySuggestionPrompt: "Edit suggested reply prompt",
  replySuggestionPromptDescription: "Edit the prompt for the selected suggested reply agent. Other functions using the same agent will also be affected.",
  aiTask: "AI function",
  inheritedConfiguration: "Fallback",
  explicitConfiguration: "Configured",
  mailboxAIConfigurationSaved: "Mailbox AI configuration saved",
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
  systemSettings: "System settings",
  systemSettingsDescription: "Manage application users and system permissions.",
  systemVersion: "System version",
  openRegistration: "Open registration",
  openRegistrationDescription: "Allow visitors to create new application accounts.",
  registrationSettingUpdated: "Registration setting updated",
  registrationClosed: "Registration is currently closed.",
  userManagement: "Users and permissions",
  role: "Role",
  ordinaryUser: "User",
  superAdmin: "Super administrator",
  currentUser: "Current account",
  roleUpdated: "User role updated",
  noUsers: "No users",
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
  aiWriteEmail: "Write with AI",
  aiWriteDescription: "Add guidance for how AI should write this email.",
  aiInstruction: "Additional instructions",
  aiInstructionPlaceholder: "For example: keep it concise and confirm Tuesday at 3 PM",
  includeConversation: "Include email history",
  noConversationContext: "No email conversation is available",
  generate: "Generate",
  generating: "Generating…",
  aiGenerateFailed: "Could not generate the email",
  aiGenerateRequest: "Write an email using the current details",
  copyAIContent: "Copy content",
  copiedAIContent: "Copied",
  copyAIContentFailed: "Could not copy content",
  generatedDraft: "Generated draft",
  regenerate: "Generate again",
  useGeneratedDraft: "Use draft",
  refineInstructionPlaceholder: "Add another change, for example: make it shorter and state the reply deadline",
  suggestedReply: "Suggested reply",
  generateSuggestedReply: "Generate reply",
  suggestedReplyGenerating: "Generating a suggested reply…",
  regenerateSuggestedReply: "Regenerate suggested reply",
  useSuggestedReply: "Use and reply",
  suggestedReplyFailed: "Could not generate a suggested reply",
};

type Copy = typeof zh;

const MAX_COMPOSE_ATTACHMENT_BYTES = 18 * 1024 * 1024;
const DEFAULT_INLINE_IMAGE_MAX_SIZE = 480;
const SUPPORTED_INLINE_IMAGE_TYPES = new Set(["image/jpeg", "image/png", "image/gif"]);

type InlineComposeImage = {
  contentId: string;
  file: File;
  previewURL: string;
};

function inlineImageDimensions(previewURL: string) {
  return new Promise<{ width: number; height: number }>((resolve, reject) => {
    const image = new Image();
    image.onload = () => {
      if (!image.naturalWidth || !image.naturalHeight) {
        reject(new Error("Image has no dimensions"));
        return;
      }
      const scale = Math.min(1, DEFAULT_INLINE_IMAGE_MAX_SIZE / Math.max(image.naturalWidth, image.naturalHeight));
      resolve({
        width: Math.max(1, Math.round(image.naturalWidth * scale)),
        height: Math.max(1, Math.round(image.naturalHeight * scale)),
      });
    };
    image.onerror = () => reject(new Error("Image could not be decoded"));
    image.src = previewURL;
  });
}

type ComposeDefaults = {
  accountEmail?: string;
  to: string;
  cc?: string;
  subject: string;
  html?: string;
  inReplyTo?: string;
  references?: string[];
  conversation?: ConversationMessage[];
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
  const stored = window.localStorage.getItem("inbrix-theme");
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
        url.pathname === "/attachments" ||
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
  // Keep the legacy URL as a compatibility/setup entry, but use the same
  // inbox shell and settings dialog as the sidebar entry point.
  if (path === "/settings") return <InboxPage />;
  if (path === "/attachments") return <AttachmentsPage />;
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
  const [settingsOpen, setSettingsOpen] = useState(() => new URLSearchParams(window.location.search).get("setup") === "1");
  const [composeDefaults, setComposeDefaults] = useState<ComposeDefaults>({ to: "", subject: "" });
  const [darkMode, setDarkMode] = useState(prefersDarkMode);
  const [deleteTarget, setDeleteTarget] = useState<ConversationSummary | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const autoReadRef = useRef(new Set<string>());
  const manuallyUnreadRef = useRef(new Set<string>());
  const debouncedSearch = useDebouncedValue(search, 250);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", darkMode);
    window.localStorage.setItem("inbrix-theme", darkMode ? "dark" : "light");
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
      void queryClient.invalidateQueries({ queryKey: ["conversation"] });
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

  const openReply = (conversation: ConversationDetail, message?: ConversationMessage, suggestedBody?: string) => {
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
      accountEmail: conversation.accountEmail,
      to: recipient,
      subject: replySubject,
      html: `${suggestedBody?.trim() ? generatedEmailHTML(suggestedBody) : "<p><br></p>"}<p>${escapeHTML(quoteLead)}</p><blockquote>${quotedHTML}</blockquote>`,
      inReplyTo: source?.messageId,
      references,
      conversation: conversation.messages,
    });
  };

  const openReplyAll = (conversation: ConversationDetail, message?: ConversationMessage, suggestedBody?: string) => {
    const source = message || conversation.messages.at(-1);
    if (!source) return;
    const subject = source.subject || conversation.subject;
    const replySubject = subject.toLowerCase().startsWith("re:") ? subject : `Re: ${subject}`;
    const self = conversation.accountEmail || conversations.data?.accountEmail || "";
    const originalTo = splitRecipientValues(source.to);
    const originalCc = splitRecipientValues(source.cc || "");
    const to = source.outgoing
      ? uniqueRecipients(originalTo, [self])
      : uniqueRecipients([source.from], [self]);
    const cc = uniqueRecipients([...originalTo, ...originalCc], [self, ...to]);
    const sender = source.fromName && source.from ? `${source.fromName} <${source.from}>` : source.from || conversation.peerEmail || "";
    const quoteLead = `On ${new Date(source.date || Date.now()).toLocaleString(locale === en ? "en" : "zh-CN")}, ${sender} wrote:`;
    const originalBody = source.body || source.preview || "";
    const references = [...(source.references || [])];
    if (source.messageId && !references.includes(source.messageId)) references.push(source.messageId);
    openCompose({
      accountEmail: conversation.accountEmail,
      to: to.join(", "),
      cc: cc.join(", "),
      subject: replySubject,
      html: `${suggestedBody?.trim() ? generatedEmailHTML(suggestedBody) : "<p><br></p>"}<p>${escapeHTML(quoteLead)}</p><blockquote>${structuredQuotedTextToHTML(originalBody)}</blockquote>`,
      inReplyTo: source.messageId,
      references,
      conversation: conversation.messages,
    });
  };

  const openNewMailForMessage = (conversation: ConversationDetail, message: ConversationMessage) => {
    const recipient = message.outgoing ? message.to : message.from || conversation.peerEmail || "";
    openCompose({ accountEmail: conversation.accountEmail, to: recipient, subject: "", conversation: conversation.messages });
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
            onReplyAll={openReplyAll}
            onNewMail={openNewMailForMessage}
            onConversationEmpty={() => {
              setSelectedId(null);
              setChatOpen(false);
              setConversationURL(null, "replace");
            }}
            className={chatOpen ? "flex" : "hidden lg:flex"}
          />
        </main>
      <ComposeDialog copy={locale} open={composeOpen} defaults={composeDefaults} accountEmail={composeDefaults.accountEmail || conversations.data?.accountEmail || ""} onOpenChange={setComposeOpen} onSent={() => {
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

function Sidebar({ copy, folders, accounts, accountEmail, calendarEnabled, currentFolder, currentView, onCompose, onSettings, open, onClose, darkMode, onToggleDarkMode }: { copy: Copy; folders: Mailbox[]; accounts: ConversationListResponse["accounts"]; accountEmail: string; calendarEnabled: boolean; currentFolder?: string; currentView?: "mail" | "calendar" | "attachments"; onCompose: () => void; onSettings: () => void; open: boolean; onClose: () => void; darkMode: boolean; onToggleDarkMode: () => void }) {
  const [foldersOpen, setFoldersOpen] = useState(true);
  const visibleFolders = folders.filter((folder) => folder.name.toLowerCase() !== "inbox" && !isSentMailbox(folder));
  const navClass = "w-full justify-start gap-2.5 px-3 text-muted-foreground";
  return (
    <aside className={cn("fixed inset-y-0 left-0 z-40 flex w-60 -translate-x-full flex-col border-r bg-sidebar px-3 py-4 transition-transform lg:static lg:z-auto lg:w-[14.375rem] lg:translate-x-0", open && "translate-x-0 ring-1 ring-foreground/10")}>
      <Button data-testid="compose-button" className="mb-4 w-full" onClick={onCompose}><Pencil />{copy.compose}</Button>
      <nav className="flex min-h-0 flex-1 flex-col gap-1">
        <Button nativeButton={false} render={<a href="/inbox" onClick={onClose} />} variant={!currentFolder && currentView !== "calendar" && currentView !== "attachments" ? "secondary" : "ghost"} size="sm" className={cn(navClass, !currentFolder && currentView !== "calendar" && currentView !== "attachments" && "bg-sidebar-accent text-sidebar-accent-foreground")}><MessageCircle /><span>{copy.conversations}</span></Button>
        <Button nativeButton={false} render={<a href="/attachments" onClick={onClose} />} variant={currentView === "attachments" ? "secondary" : "ghost"} size="sm" className={cn(navClass, currentView === "attachments" && "bg-sidebar-accent text-sidebar-accent-foreground")}><Paperclip /><span>{copy.attachmentManager}</span></Button>
        {calendarEnabled && <Button nativeButton={false} render={<a href="/calendar" onClick={onClose} />} variant={currentView === "calendar" ? "secondary" : "ghost"} size="sm" className={cn(navClass, currentView === "calendar" && "bg-sidebar-accent text-sidebar-accent-foreground")}><CalendarDays /><span>{copy.calendar}</span></Button>}
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
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" className="min-w-0 flex-1 justify-start px-2" />}>
        <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: active?.color || "#777" }} />
        <span className="min-w-0 flex-1 truncate text-left">{active?.label || accountEmail}</span>
        <ChevronDown className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" sideOffset={6} className="w-60">
        {accounts.map((account) => (
              <DropdownMenuItem key={account.email} className="gap-2 px-2 py-2" onClick={() => void selectAccount(account.email)}>
                <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: account.color || "#777" }} />
                <span className="min-w-0 flex-1"><strong className="block truncate font-medium">{account.label || account.email}</strong><small className="block truncate text-muted-foreground">{account.email}</small></span>
                {(account.isActive || account.email === accountEmail) && <Check className="size-4 shrink-0" />}
              </DropdownMenuItem>
            ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" className="px-2 py-2" onClick={() => { void signOut().then(() => window.location.assign("/user-login")); }}>{copy.signOut}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
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
  return <Button nativeButton={false} render={<a href={`/folder/${encodeURIComponent(folder.name)}`} onClick={onClose} />} variant={selected ? "secondary" : "ghost"} size="sm" className={cn("w-full justify-start gap-2.5 pl-9 text-muted-foreground", selected && "text-sidebar-accent-foreground")}><Icon /><span className="min-w-0 flex-1 truncate">{folderLabel(copy, folder)}</span>{folder.unreadCount ? <Badge variant="secondary" className="min-w-5 justify-center px-1.5 text-[10px]">{folder.unreadCount}</Badge> : null}</Button>;
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
    <ContextMenu>
      <ContextMenuTrigger className="block">
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
      </ContextMenuTrigger>
      <ContextMenuContent className="w-44">
        <ContextMenuItem className="gap-2 px-2 py-2" onClick={onMarkUnread}><Mail className="size-4" />{copy.markUnread}</ContextMenuItem>
        <ContextMenuItem variant="destructive" className="gap-2 px-2 py-2" onClick={onDelete}><Trash2 className="size-4" />{copy.deleteConversation}</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

function ChatPanel({ copy, detail, loading, error, onBack, onReply, onReplyAll, onNewMail, onConversationEmpty, className }: { copy: Copy; detail?: ConversationDetail; loading: boolean; error: Error | null; onBack: () => void; onReply: (conversation: ConversationDetail, message: ConversationMessage, suggestedBody?: string) => void; onReplyAll: (conversation: ConversationDetail, message: ConversationMessage, suggestedBody?: string) => void; onNewMail: (conversation: ConversationDetail, message: ConversationMessage) => void; onConversationEmpty: () => void; className?: string }) {
  const panelClass = cn("min-w-0 flex-1 flex-col bg-surface", className);
  if (loading) return <section className={cn(panelClass, "items-center justify-center")}><div className="text-sm text-muted-foreground">{copy.loading}</div></section>;
  if (error) return <section className={panelClass}><ErrorState copy={copy} /></section>;
  if (!detail) return <section className={panelClass}><EmptyState icon={<MessageCircle />} text={copy.selectConversation} /></section>;
  return <ChatView copy={copy} detail={detail} onBack={onBack} onReply={(message, suggestedBody) => onReply(detail, message, suggestedBody)} onReplyAll={(message, suggestedBody) => onReplyAll(detail, message, suggestedBody)} onNewMail={(message) => onNewMail(detail, message)} onConversationEmpty={onConversationEmpty} />;
}

function ChatView({ copy, detail, onBack, onReply, onReplyAll, onNewMail, onConversationEmpty }: { copy: Copy; detail: ConversationDetail; onBack: () => void; onReply: (message: ConversationMessage, suggestedBody?: string) => void; onReplyAll: (message: ConversationMessage, suggestedBody?: string) => void; onNewMail: (message: ConversationMessage) => void; onConversationEmpty: () => void }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const queryClient = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<ConversationMessage | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const latestConversationMessage = detail.messages.at(-1);
  const persistedSuggestionMessage = latestConversationMessage && !latestConversationMessage.outgoing && latestConversationMessage.suggestedReply?.status === "ready" && latestConversationMessage.suggestedReply.text.trim() ? latestConversationMessage : undefined;
  const persistedSuggestionKey = persistedSuggestionMessage ? `${persistedSuggestionMessage.folder || "INBOX"}\u0000${persistedSuggestionMessage.id}` : null;
  const [suggestionTargetKey, setSuggestionTargetKey] = useState<string | null>(persistedSuggestionKey);
  const [suggestionGeneration, setSuggestionGeneration] = useState(0);
  const aiSettings = useQuery({ queryKey: ["ai-models"], queryFn: getAIModels, retry: false });
  useEffect(() => {
    setSuggestionTargetKey(persistedSuggestionKey);
    setSuggestionGeneration(0);
  }, [detail.id]);
  useEffect(() => {
    if (!suggestionTargetKey && persistedSuggestionKey) setSuggestionTargetKey(persistedSuggestionKey);
  }, [persistedSuggestionKey, suggestionTargetKey]);
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
  const generateSuggestedReply = (message: ConversationMessage) => {
    setSuggestionTargetKey(`${message.folder || "INBOX"}\u0000${message.id}`);
    setSuggestionGeneration((value) => value + 1);
  };
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
        <div aria-hidden="true" />
      </header>
      <ScrollArea className="min-h-0 flex-1" viewportClassName="scroll-smooth" contentClassName="px-3 py-6 sm:px-[5vw] sm:py-8" viewportRef={scrollRef}>
        <div ref={contentRef}>
          {detail.messages.map((message, index) => {
            const messageKey = `${message.folder || "INBOX"}\u0000${message.id}`;
            const canGenerateReply = !message.outgoing && Boolean(detail.accountEmail) && Boolean(aiSettings.data?.models.length);
            return <Fragment key={messageKey}>
              <MessageBubble copy={copy} message={message} senderFallback={detail.peerEmail || detail.title} accountEmail={detail.accountEmail} rootRef={scrollRef} eager={index >= detail.messages.length - 3} onReply={() => onReply(message)} onReplyAll={() => onReplyAll(message)} onNewMail={() => onNewMail(message)} onDelete={() => { setDeleteError(""); setDeleteTarget(message); }} onGenerateReply={canGenerateReply ? () => generateSuggestedReply(message) : undefined} />
              {!message.outgoing && suggestionTargetKey === messageKey ? <SuggestedReplyBubble key={messageKey} copy={copy} detail={detail} message={message} generation={suggestionGeneration} onReply={(body) => onReply(message, body)} onReplyAll={(body) => onReplyAll(message, body)} /> : null}
            </Fragment>;
          })}
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

function SuggestedReplyBubble({ copy, detail, message, generation, onReply, onReplyAll }: { copy: Copy; detail: ConversationDetail; message: ConversationMessage; generation: number; onReply: (body: string) => void; onReplyAll: (body: string) => void }) {
  const queryClient = useQueryClient();
  const [body, setBody] = useState(message.suggestedReply?.text.trim() || "");
  const previousGenerationRef = useRef(generation > 0 ? generation - 1 : generation);
  const suggestion = useMutation({
    mutationFn: () => generateEmail({
      accountEmail: detail.accountEmail || "",
      taskType: "reply_suggestion",
      folder: message.folder || "INBOX",
      messageId: message.id,
      instruction: "Generate a persisted reply suggestion for this received email.",
      subject: message.subject || detail.subject,
      recipients: message.from || detail.peerEmail || "",
    }),
    onSuccess: (result) => {
      const text = result.body.trim();
      setBody(text);
      queryClient.setQueriesData<ConversationDetailResponse>({ queryKey: ["conversation"] }, (current) => {
        if (!current || current.conversation.id !== detail.id) return current;
        const messages = current.conversation.messages.map((item) => item.id === message.id && (item.folder || "INBOX") === (message.folder || "INBOX") ? { ...item, suggestedReply: { text, status: "ready" as const, updatedAt: result.updatedAt } } : item);
        return { ...current, conversation: { ...current.conversation, messages } };
      });
    },
  });
  useEffect(() => {
    if (previousGenerationRef.current === generation) return;
    previousGenerationRef.current = generation;
    suggestion.mutate();
  }, [generation]);
  return (
    <article className="mb-5">
      <div className="mb-1 flex justify-end text-right text-xs leading-tight text-muted-foreground"><span className="flex items-center gap-1.5 font-medium"><Sparkles className="size-3.5" />{copy.suggestedReply}</span></div>
      <ContextMenu>
        <ContextMenuTrigger className="block select-text">
          <div className="flex justify-end">
            <div className="min-w-0 max-w-[80%] rounded-xl border border-dashed bg-secondary px-3 py-2 text-sm leading-relaxed text-secondary-foreground">
              {suggestion.isPending && !body && <p className="flex items-center gap-2 text-muted-foreground"><Sparkles className="size-4 animate-pulse" />{copy.suggestedReplyGenerating}</p>}
              {body && <p className="whitespace-pre-wrap">{body}</p>}
              {suggestion.error && <p className="text-destructive">{suggestion.error instanceof Error ? suggestion.error.message : copy.suggestedReplyFailed}</p>}
            </div>
          </div>
        </ContextMenuTrigger>
        {body && <ContextMenuContent className="w-40">
          <ContextMenuItem className="gap-2 px-2 py-2" onClick={() => onReply(body)}><Send className="size-4" />{copy.reply}</ContextMenuItem>
          <ContextMenuItem className="gap-2 px-2 py-2" onClick={() => onReplyAll(body)}><ReplyAll className="size-4" />{copy.replyAll}</ContextMenuItem>
        </ContextMenuContent>}
      </ContextMenu>
      <div className="mt-1.5 flex min-h-7 justify-end gap-1">
        <Button type="button" variant="ghost" size="icon" className="size-7" disabled={suggestion.isPending} onClick={() => suggestion.mutate()} aria-label={copy.regenerateSuggestedReply} title={copy.regenerateSuggestedReply}><RotateCcw className={cn("size-3.5", suggestion.isPending && "animate-spin")} /></Button>
        {body && <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => onReply(body)}><Send className="size-3.5" />{copy.useSuggestedReply}</Button>}
      </div>
    </article>
  );
}

function MessageBubble({ copy, message, senderFallback, accountEmail, rootRef, eager, onReply, onReplyAll, onNewMail, onDelete, onGenerateReply }: { copy: Copy; message: ConversationMessage; senderFallback?: string; accountEmail?: string; rootRef: { current: HTMLDivElement | null }; eager: boolean; onReply: () => void; onReplyAll: () => void; onNewMail: () => void; onDelete: () => void; onGenerateReply?: () => void }) {
  const sender = message.outgoing ? copy.me : message.fromName || message.from || senderFallback || copy.conversations;
  const split = splitQuotedText(message.body || message.preview || copy.noBody);
  const visibleText = split.visible || (split.quoted ? copy.quotedOnly : copy.noBody);
  const outgoing = message.outgoing;
  const formattedDate = formatTime(message.date);
  return (
    <ContextMenu>
      <ContextMenuTrigger className="mb-5 block select-text">
        <article>
          <div className={cn("mb-1 flex min-w-0 items-start gap-2 text-xs leading-tight text-muted-foreground", outgoing && "justify-end text-right")}><div className="min-w-0"><div><span className="font-medium">{sender}</span><time className="ml-2" title={formattedDate}>{formattedDate}</time></div><div className="mt-1 max-w-full break-words text-[11px]"><span className="font-medium">{copy.to}:</span> {message.to || "-"}{message.cc && <><span className="mx-1.5">·</span><span className="font-medium">{copy.cc}:</span> {message.cc}</>}</div></div></div>
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
          <MailMessageSummary copy={copy} accountEmail={accountEmail} folder={message.folder || "INBOX"} messageId={message.id} initialSummary={message.mailSummary} outgoing={outgoing} onGenerateReply={onGenerateReply} />
        </article>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-40">
        <ContextMenuItem className="gap-2 px-2 py-2" onClick={onReply}><Send className="size-4" />{copy.reply}</ContextMenuItem>
        <ContextMenuItem className="gap-2 px-2 py-2" onClick={onReplyAll}><ReplyAll className="size-4" />{copy.replyAll}</ContextMenuItem>
        <ContextMenuItem className="gap-2 px-2 py-2" onClick={onNewMail}><Mail className="size-4" />{copy.sendEmail}</ContextMenuItem>
        <ContextMenuItem variant="destructive" className="gap-2 px-2 py-2" onClick={onDelete}><Trash2 className="size-4" />{copy.deleteEmail}</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

function MailMessageSummary({ copy, accountEmail, folder, messageId, initialSummary, outgoing = false, onGenerateReply }: { copy: Copy; accountEmail?: string; folder: string; messageId: string; initialSummary?: ConversationMessage["mailSummary"]; outgoing?: boolean; onGenerateReply?: () => void }) {
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
      {!savedSummary && <div className="flex min-h-7 items-center gap-1"><Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground" disabled={mutation.isPending || !accountEmail} onClick={() => mutation.mutate(false)}><Sparkles className="size-3.5" />{mutation.isPending ? copy.summarizing : copy.summarize}</Button>{onGenerateReply && <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground" onClick={onGenerateReply}><MessageCircle className="size-3.5" />{copy.generateSuggestedReply}</Button>}</div>}
      {savedSummary && <div className="mt-1 border-l-2 border-primary/40 bg-muted/40 px-3 py-2 text-sm leading-relaxed">
        <div className="mb-1 flex items-center justify-between gap-2"><div className="flex min-w-0 items-center gap-2"><strong className="text-xs font-medium text-muted-foreground">{copy.mailSummaryTitle}</strong>{savedSummary.stale && <span className="truncate text-[10px] text-amber-700 dark:text-amber-400">{copy.summaryStale}</span>}</div><Button type="button" variant="ghost" size="icon" className="size-6 shrink-0" disabled={mutation.isPending || !accountEmail} onClick={() => mutation.mutate(true)} aria-label={copy.regenerateSummary} title={copy.regenerateSummary}><RotateCcw className={cn("size-3.5", mutation.isPending && "animate-spin")} /></Button></div>
        <p className="whitespace-pre-wrap">{savedSummary.text}</p>
      </div>}
      {savedSummary && onGenerateReply && <Button type="button" variant="ghost" size="sm" className="mt-1 h-7 px-2 text-xs text-muted-foreground" onClick={onGenerateReply}><MessageCircle className="size-3.5" />{copy.generateSuggestedReply}</Button>}
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
        if (!doc.head || doc.head.querySelector("[data-inbrix-frame-style]")) return;
        const style = doc.createElement("style");
        style.dataset.inbrixFrameStyle = "";
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
    if (element.closest("[data-inbrix-signature]")) return;
    const tag = element.tagName.toLowerCase();
    if (tag !== "blockquote" && tag !== "includetail" && element.querySelector(quoteSelector)) return;
    let attribution = element.getAttribute("data-attribution") || "";
    const previous = element.previousElementSibling;
    if (!attribution && previous && /(?:\bwrote\s*:|写道\s*[:：]|寫道\s*[:：]|original)/i.test(previous.textContent || "")) {
      attribution = previous.textContent || "";
      previous.remove();
    }
    const wrapper = document.createElement("div");
    wrapper.dataset.inbrixReplyQuote = "";
    wrapper.dataset.attribution = quoteAttribution(attribution);
    while (element.firstChild) wrapper.appendChild(element.firstChild);
    element.replaceWith(wrapper);
  });
  const children = Array.from(document.body.children);
  const separatorIndex = children.findIndex((element) => /^[-_\s]*original(?:\s+message)?[-_\s]*$/i.test((element.textContent || "").replace(/\u00a0/g, " ").trim()));
  if (separatorIndex >= 0) {
    const wrapper = document.createElement("div");
    wrapper.dataset.inbrixReplyQuote = "";
    wrapper.dataset.attribution = quoteAttribution(children[separatorIndex].textContent || "Original message");
    children.slice(separatorIndex + 1).forEach((element) => wrapper.appendChild(element));
    children[separatorIndex].replaceWith(wrapper);
  }
  return document.body.innerHTML;
}

function serializeEmailHTML(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  document.body.querySelectorAll("p:empty").forEach((paragraph) => paragraph.appendChild(document.createElement("br")));
  return document.body.innerHTML;
}

function serializeQuoteHTML(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  Array.from(document.body.querySelectorAll("[data-inbrix-reply-quote]")).reverse().forEach((element) => {
    const attribution = element.getAttribute("data-attribution") || "Quoted message";
    const paragraph = document.createElement("p");
    paragraph.textContent = attribution.endsWith(":") ? attribution : `${attribution}:`;
    const blockquote = document.createElement("blockquote");
    while (element.firstChild) blockquote.appendChild(element.firstChild);
    element.before(paragraph);
    element.replaceWith(blockquote);
  });
  return serializeEmailHTML(document.body.innerHTML);
}

function serializeInlineImageReferences(html: string, removeMetadata: boolean) {
  const document = new DOMParser().parseFromString(html, "text/html");
  document.body.querySelectorAll<HTMLImageElement>("img[data-inline-image-id]").forEach((image) => {
    const contentId = image.dataset.inlineImageId;
    if (contentId) image.setAttribute("src", `cid:${contentId}`);
    if (removeMetadata) image.removeAttribute("data-inline-image-id");
  });
  return document.body.innerHTML;
}

function restoreInlineImagePreviews(html: string, inlineImages: InlineComposeImage[]) {
  const document = new DOMParser().parseFromString(html, "text/html");
  const previews = new Map(inlineImages.map((image) => [image.contentId, image.previewURL]));
  document.body.querySelectorAll<HTMLImageElement>("img[data-inline-image-id]").forEach((image) => {
    const previewURL = previews.get(image.dataset.inlineImageId || "");
    if (previewURL) image.setAttribute("src", previewURL);
  });
  return document.body.innerHTML;
}

function serializeComposeHTML(html: string) {
  const document = new DOMParser().parseFromString(serializeInlineImageReferences(serializeQuoteHTML(html), true), "text/html");
  Array.from(document.body.querySelectorAll("[data-inbrix-signature]")).forEach((element) => {
    element.replaceWith(...Array.from(element.childNodes));
  });
  return document.body.innerHTML;
}

function signatureNodeHTML(signature: EmailSignature) {
  return `<div data-inbrix-signature="${escapeHTML(signature.id)}" data-signature-name="${escapeHTML(signature.name)}">${signature.html.trim() || "<p></p>"}</div>`;
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

function aiConversationContext(messages: ConversationMessage[]) {
  return messages.map((message, index) => {
    const summary = message.mailSummary?.status === "ready" ? message.mailSummary.text.trim() : "";
    const body = splitQuotedText(message.body || message.preview || "").visible.trim() || (message.body || message.preview || "").trim();
    const content = summary ? `AI summary:\n${summary}` : `Message body:\n${body}`;
    return [
      `Message ${index + 1}`,
      `Date: ${message.date}`,
      `From: ${message.fromName ? `${message.fromName} <${message.from}>` : message.from}`,
      `To: ${message.to}`,
      message.cc ? `Cc: ${message.cc}` : "",
      `Subject: ${message.subject}`,
      content,
    ].filter(Boolean).join("\n");
  }).join("\n\n---\n\n");
}

function generatedEmailHTML(body: string) {
  return body.replace(/\r\n?/g, "\n").split(/\n{2,}/).map((paragraph) => {
    const content = paragraph.split("\n").map((line) => escapeHTML(line)).join("<br>");
    return `<p>${content || "<br>"}</p>`;
  }).join("");
}

function replaceEditorDraft(editor: Editor, body: string) {
  let protectedPosition: number | null = null;
  editor.state.doc.descendants((node, position) => {
    if (protectedPosition === null && (node.type.name === "emailSignature" || node.type.name === "replyQuote")) {
      protectedPosition = position;
      return false;
    }
    return protectedPosition === null;
  });
  const html = generatedEmailHTML(body);
  if (protectedPosition === null) {
    editor.commands.setContent(html);
  } else {
    editor.chain().deleteRange({ from: 0, to: protectedPosition }).insertContentAt(0, html).run();
  }
  editor.commands.focus("start");
}

function AIAssistantButton({ copy, editor, disabled, composeOpen, accountEmail, subject, recipients, conversation }: { copy: Copy; editor: Editor | null; disabled: boolean; composeOpen: boolean; accountEmail: string; subject: string; recipients: string; conversation?: ConversationMessage[] }) {
  const [open, setOpen] = useState(false);
  const [instruction, setInstruction] = useState("");
  const [includeConversation, setIncludeConversation] = useState(false);
  const [generatedBody, setGeneratedBody] = useState("");
  const [chatMessages, setChatMessages] = useState<Array<{ role: "user" | "assistant"; content: string }>>([]);
  const [copiedMessage, setCopiedMessage] = useState<number | null>(null);
  const [error, setError] = useState("");
  const chatRef = useRef<HTMLDivElement>(null);
  const messages = conversation || [];
  const hasConversation = messages.length > 0;
  const mutation = useMutation({
    mutationFn: ({ prompt, draft }: { prompt: string; draft?: string }) => generateEmail({
      accountEmail,
      instruction: prompt,
      subject,
      recipients,
      context: includeConversation ? aiConversationContext(messages) : undefined,
      draft,
    }),
    onSuccess: ({ body }) => {
      setGeneratedBody(body);
      setChatMessages((current) => [...current, { role: "assistant", content: body }]);
      setError("");
    },
    onError: (value) => setError(value instanceof Error ? value.message : copy.aiGenerateFailed),
  });

  useEffect(() => {
    setIncludeConversation(false);
    setInstruction("");
    setGeneratedBody("");
    setChatMessages([]);
    setCopiedMessage(null);
    setError("");
  }, [composeOpen, conversation]);

  useEffect(() => {
    if (!open) return;
    const element = chatRef.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [chatMessages, mutation.isPending, open]);

  const submitInstruction = () => {
    const prompt = instruction.trim();
    const displayedPrompt = prompt || copy.aiGenerateRequest;
    setChatMessages((current) => [...current, { role: "user", content: displayedPrompt }]);
    setInstruction("");
    setError("");
    mutation.mutate({ prompt, draft: generatedBody || undefined });
  };

  const copyMessage = async (content: string, index: number) => {
    try {
      await navigator.clipboard.writeText(content);
      setCopiedMessage(index);
      window.setTimeout(() => setCopiedMessage((current) => current === index ? null : current), 1600);
    } catch {
      toast.error(copy.copyAIContentFailed);
    }
  };

  const useGeneratedBody = () => {
    if (!editor || !generatedBody) return;
    replaceEditorDraft(editor, generatedBody);
    setGeneratedBody("");
    setInstruction("");
    setError("");
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={(value) => { setOpen(value); if (!value) setError(""); }}>
      <PopoverTrigger render={<Button type="button" variant={open ? "secondary" : "ghost"} size="icon" disabled={disabled || !editor} aria-label={copy.aiWriteEmail} title={copy.aiWriteEmail} />}><Sparkles /></PopoverTrigger>
      <PopoverContent side="bottom" align="start" sideOffset={8} className="w-[min(30rem,calc(100vw-2rem))] gap-0 p-4">
        <PopoverTitle className="text-sm font-semibold">{copy.aiWriteEmail}</PopoverTitle>
        <PopoverDescription className="mt-1 text-xs">{copy.aiWriteDescription}</PopoverDescription>
        {chatMessages.length > 0 && <div ref={chatRef} className="mt-4 flex max-h-72 flex-col gap-3 overflow-y-auto pr-1" aria-live="polite">
          {chatMessages.map((message, index) => message.role === "user" ? (
            <div key={index} className="group ml-10 grid justify-items-end self-end">
              <div className="whitespace-pre-wrap rounded-lg border bg-muted/40 px-3 py-2 text-sm leading-5">{message.content}</div>
              <Button type="button" variant="ghost" size="icon" className="size-7 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100" onClick={() => void copyMessage(message.content, index)} aria-label={copy.copyAIContent} title={copiedMessage === index ? copy.copiedAIContent : copy.copyAIContent}>
                {copiedMessage === index ? <Check /> : <CopyIcon />}
              </Button>
            </div>
          ) : (
            <div key={index} className="group mr-5 grid self-start">
              <div className="whitespace-pre-wrap px-1 text-sm leading-6">{message.content}</div>
              <Button type="button" variant="ghost" size="icon" className="size-7 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100" onClick={() => void copyMessage(message.content, index)} aria-label={copy.copyAIContent} title={copiedMessage === index ? copy.copiedAIContent : copy.copyAIContent}>
                {copiedMessage === index ? <Check /> : <CopyIcon />}
              </Button>
            </div>
          ))}
          {mutation.isPending && <div className="mr-5 flex w-fit items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-sm text-muted-foreground"><Sparkles className="size-4 animate-pulse" />{copy.generating}</div>}
        </div>}
        <Label className="mt-4 grid gap-1.5 text-xs" htmlFor="ai-compose-instruction">{copy.aiInstruction}<Textarea id="ai-compose-instruction" value={instruction} onChange={(event) => { setInstruction(event.target.value); setError(""); }} placeholder={generatedBody ? copy.refineInstructionPlaceholder : copy.aiInstructionPlaceholder} rows={3} disabled={mutation.isPending} onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key === "Enter") { event.preventDefault(); if (!mutation.isPending) submitInstruction(); } }} /></Label>
        {error && <p className="mt-3 text-xs text-destructive">{error}</p>}
        <div className="mt-4 flex items-center justify-between gap-3">
          <label className={cn("flex min-w-0 items-center gap-2 text-sm", !hasConversation && "text-muted-foreground")} title={!hasConversation ? copy.noConversationContext : undefined}>
            <input className="size-4 shrink-0 accent-primary" type="checkbox" checked={includeConversation} disabled={!hasConversation || mutation.isPending} onChange={(event) => setIncludeConversation(event.target.checked)} />
            <span className="truncate">{copy.includeConversation}</span>
          </label>
          <div className="flex shrink-0 gap-2">
            {generatedBody && <Button type="button" variant="ghost" size="sm" disabled={mutation.isPending} onClick={useGeneratedBody}><Check />{copy.useGeneratedDraft}</Button>}
            <Button type="button" variant={generatedBody ? "outline" : "default"} size="sm" disabled={mutation.isPending || (!instruction.trim() && !subject.trim() && !includeConversation && !generatedBody)} onClick={submitInstruction}><Send />{mutation.isPending ? copy.generating : generatedBody ? copy.regenerate : copy.generate}</Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function htmlToPlainText(html: string) {
  const body = new DOMParser().parseFromString(html, "text/html").body;
  const lines: { depth: number; text: string }[] = [{ depth: 0, text: "" }];
  const newLine = (depth: number, preserveEmpty = false) => {
    if (!lines.at(-1)?.text) {
      lines[lines.length - 1].depth = depth;
      if (preserveEmpty) lines.push({ depth, text: "" });
    }
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
    if (node.tagName === "IMG") {
      appendText(`[Image: ${node.getAttribute("alt")?.trim() || "Image"}]`, depth);
      return;
    }
    const childDepth = node.tagName === "BLOCKQUOTE" ? depth + 1 : depth;
    if (node.tagName === "BLOCKQUOTE") newLine(childDepth);
    Array.from(node.childNodes).forEach((child) => walk(child, childDepth));
    if (["P", "DIV", "LI", "BLOCKQUOTE"].includes(node.tagName)) {
      const hasVisibleContent = Boolean((node.textContent || "").trim() || node.querySelector("br, img, hr"));
      newLine(depth, !hasVisibleContent);
    }
  };
  Array.from(body.childNodes).forEach((node) => walk(node, 0));
  const result = lines
    .map(({ depth, text }) => `${depth ? `${">".repeat(depth)} ` : ""}${text.trimEnd()}`.trimEnd())
    .join("\n")
    .trimEnd();
  return result.trim() ? result : "";
}

function splitRecipientValues(value: string) {
  return value.split(/[,;\n]+/).map((item) => item.trim()).filter(Boolean);
}

function recipientAddress(value: string) {
  const match = value.trim().match(/<([^<>]+)>\s*$/);
  return (match?.[1] || value).trim().toLowerCase();
}

function uniqueRecipients(values: string[], excluded: string[] = []) {
  const seen = new Set(excluded.map(recipientAddress).filter(Boolean));
  return values.filter((value) => {
    const address = recipientAddress(value);
    if (!address || seen.has(address)) return false;
    seen.add(address);
    return true;
  });
}

function isValidRecipient(value: string) {
  const match = value.trim().match(/^(?:[^<>]*<)?([^\s<>@,;]+@[^\s<>@,;]+\.[^\s<>@,;]+)>?$/);
  return Boolean(match);
}

function RecipientTagInput({ copy, label, recipients, onChange, draftRef, autoFocus = false }: { copy: Copy; label: string; recipients: string[]; onChange: (recipients: string[]) => void; draftRef: MutableRefObject<string>; autoFocus?: boolean }) {
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
        }} onBlur={() => { if (draft.trim()) addValues(splitRecipientValues(draft)); }} placeholder={recipients.length ? "" : "name@example.com"} autoFocus={autoFocus} inputMode="email" aria-invalid={invalid} aria-label={label} />
      </div>
      {invalid && <p className="mt-1 text-xs text-destructive">{copy.invalidRecipient}</p>}
    </div>
  );
}

function ComposeDialog({ copy, open, defaults, accountEmail, onOpenChange, onSent }: { copy: Copy; open: boolean; defaults: ComposeDefaults; accountEmail: string; onOpenChange: (value: boolean) => void; onSent: () => void }) {
  const [recipients, setRecipients] = useState<string[]>([]);
  const [ccRecipients, setCcRecipients] = useState<string[]>([]);
  const [subject, setSubject] = useState("");
  const [error, setError] = useState("");
  const [sourceMode, setSourceMode] = useState(false);
  const [sourceCode, setSourceCode] = useState("");
  const [attachments, setAttachments] = useState<File[]>([]);
  const [inlineImages, setInlineImages] = useState<InlineComposeImage[]>([]);
  const inlineImagesRef = useRef<InlineComposeImage[]>([]);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const inlineImageInputRef = useRef<HTMLInputElement>(null);
  const recipientDraftRef = useRef("");
  const ccDraftRef = useRef("");
  const signatureInitializedRef = useRef(false);
  const [selectedSignatureId, setSelectedSignatureId] = useState("none");
  const editor = useEditor({ extensions: [StarterKit.configure({ blockquote: false, paragraph: false }), EmailParagraph, ReplyQuote, EmailSignatureExtension, EmailImage, UnderlineExtension, LinkExtension.configure({ openOnClick: false }), Placeholder.configure({ placeholder: copy.writeMessage })], content: "", immediatelyRender: false });
  const signatures = useQuery({ queryKey: ["signatures", accountEmail], queryFn: getSignatures, enabled: open, retry: false });
  const mutation = useMutation({ mutationFn: sendMessage });

  const replaceInlineImages = (images: InlineComposeImage[]) => {
    inlineImagesRef.current = images;
    setInlineImages(images);
  };

  const clearInlineImages = () => {
    inlineImagesRef.current.forEach((image) => URL.revokeObjectURL(image.previewURL));
    replaceInlineImages([]);
  };

  useEffect(() => () => {
    inlineImagesRef.current.forEach((image) => URL.revokeObjectURL(image.previewURL));
  }, []);

  useEffect(() => {
    if (!open && inlineImagesRef.current.length) clearInlineImages();
  }, [open]);

  useEffect(() => {
    if (!open || !editor) return;
    setRecipients(splitRecipientValues(defaults.to));
    setCcRecipients(splitRecipientValues(defaults.cc || ""));
    setSubject(defaults.subject);
    setError("");
    setSourceMode(false);
    setSourceCode(defaults.html || "");
    setSelectedSignatureId("none");
    signatureInitializedRef.current = false;
    setAttachments([]);
    clearInlineImages();
    recipientDraftRef.current = "";
    ccDraftRef.current = "";
    if (attachmentInputRef.current) attachmentInputRef.current.value = "";
    editor.commands.setContent(normalizeQuoteHTML(defaults.html || ""));
    editor.commands.focus("start");
  }, [open, defaults, editor]);

  const insertInlineImages = async (files: File[], position?: number) => {
    if (!editor || !files.length) return;
    if (files.some((file) => !SUPPORTED_INLINE_IMAGE_TYPES.has(file.type) || file.size <= 0)) {
      setError(copy.unsupportedImage);
      return;
    }
    const totalSize = [...attachments, ...inlineImages.map((image) => image.file), ...files].reduce((total, file) => total + file.size, 0);
    if (totalSize > MAX_COMPOSE_ATTACHMENT_BYTES) {
      setError(copy.attachmentsTooLarge);
      return;
    }

    const previews = files.map((file) => {
      const contentId = `${crypto.randomUUID().replaceAll("-", "")}@inbrix`;
      return { contentId, file, previewURL: URL.createObjectURL(file) };
    });
    let additions: Array<InlineComposeImage & { width: number; height: number }>;
    try {
      additions = await Promise.all(previews.map(async (image) => ({ ...image, ...await inlineImageDimensions(image.previewURL) })));
    } catch {
      previews.forEach((image) => URL.revokeObjectURL(image.previewURL));
      setError(copy.unsupportedImage);
      return;
    }
    replaceInlineImages([...inlineImages, ...additions]);
    additions.forEach((image, index) => {
      const chain = editor.chain();
      if (index === 0 && position !== undefined) chain.focus(position);
      else chain.focus();
      chain.insertContent({
        type: "image",
        attrs: { src: image.previewURL, alt: image.file.name, title: image.file.name, width: image.width, height: image.height, inlineImageId: image.contentId },
      }).run();
    });
    setError("");
  };

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
      editor.commands.setContent(normalizeQuoteHTML(restoreInlineImagePreviews(sourceCode, inlineImages)));
      editor.commands.focus("start");
      setSourceMode(false);
      return;
    }
    setSourceCode(serializeInlineImageReferences(serializeQuoteHTML(editor.getHTML()), false));
    setSourceMode(true);
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const pendingRecipients = splitRecipientValues(recipientDraftRef.current);
    const pendingCcRecipients = splitRecipientValues(ccDraftRef.current);
    const submittedRecipients = [...recipients, ...pendingRecipients.filter((value) => isValidRecipient(value))];
    const submittedCcRecipients = uniqueRecipients([...ccRecipients, ...pendingCcRecipients.filter((value) => isValidRecipient(value))], submittedRecipients);
    const htmlBody = serializeComposeHTML(sourceMode ? sourceCode : editor?.getHTML() || "");
    const plainBody = htmlToPlainText(htmlBody);
    const referencedContentIds = new Set(Array.from(new DOMParser().parseFromString(htmlBody, "text/html").querySelectorAll<HTMLImageElement>('img[src^="cid:"]')).map((image) => image.getAttribute("src")?.slice(4) || "").filter(Boolean));
    const submittedInlineImages = inlineImages.filter((image) => referencedContentIds.has(image.contentId));
    if (!submittedRecipients.length || [...pendingRecipients, ...pendingCcRecipients].some((value) => !isValidRecipient(value))) {
      setError(copy.invalidRecipient);
      return;
    }
    if (!plainBody) {
      setError(copy.noBody);
      return;
    }
    if (Array.from(referencedContentIds).some((contentId) => !submittedInlineImages.some((image) => image.contentId === contentId))) {
      setError(copy.inlineImageMissing);
      return;
    }
    const form = new FormData();
    form.set("to", submittedRecipients.join(", "));
    if (submittedCcRecipients.length) form.set("cc", submittedCcRecipients.join(", "));
    form.set("subject", subject.trim());
    form.set("body", plainBody);
    form.set("html_body", htmlBody);
    if (defaults.inReplyTo) form.set("in_reply_to", defaults.inReplyTo);
    if (defaults.references?.length) form.set("references", defaults.references.join(" "));
    if (accountEmail) form.set("account_email", accountEmail);
    attachments.forEach((file) => form.append("attachments", file, file.name));
    const inlineManifest = submittedInlineImages.map((image, index) => {
      const field = `inline_image_${index}`;
      form.append(field, image.file, image.file.name);
      return { field, contentId: image.contentId };
    });
    if (inlineManifest.length) form.set("inline_attachments", JSON.stringify(inlineManifest));
    mutation.mutate(form, { onSuccess: () => { onSent(); onOpenChange(false); }, onError: (value) => setError(value instanceof Error ? value.message : copy.loginFailed) });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="compose-dialog" className="flex h-[80vh] w-[80vw] max-w-[1200px] flex-col gap-0 overflow-hidden p-0 sm:max-w-[1200px]">
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
          <DialogHeader className="border-b px-5 py-4 pr-12 text-left"><DialogTitle className="truncate text-base">{subject || copy.writeMessage}</DialogTitle><DialogDescription className="sr-only">{copy.compose}</DialogDescription></DialogHeader>
          <div className="grid gap-3 border-b px-5 py-4"><Label className="grid gap-1.5 text-xs text-muted-foreground">{copy.to}<RecipientTagInput copy={copy} label={copy.to} recipients={recipients} onChange={setRecipients} draftRef={recipientDraftRef} autoFocus /></Label><Label className="grid gap-1.5 text-xs text-muted-foreground">{copy.cc}<RecipientTagInput copy={copy} label={copy.cc} recipients={ccRecipients} onChange={setCcRecipients} draftRef={ccDraftRef} /></Label><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="compose-subject">{copy.subject}<Input id="compose-subject" value={subject} onChange={(event) => setSubject(event.target.value)} placeholder={copy.noSubject} /></Label></div>
          <div className="flex items-center gap-1 overflow-x-auto border-b bg-muted px-4 py-1">
            <RichTextButtons editor={editor} disabled={sourceMode} />
            <Button type="button" variant="ghost" size="icon" disabled={sourceMode || !editor} onClick={() => inlineImageInputRef.current?.click()} aria-label={copy.insertImage} title={copy.insertImage}><ImagePlus /></Button>
            <Separator orientation="vertical" className="mx-1 h-5" />
            <AIAssistantButton copy={copy} editor={editor} disabled={sourceMode} composeOpen={open} accountEmail={accountEmail} subject={subject} recipients={[...recipients, ...ccRecipients].join(", ")} conversation={defaults.conversation} />
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
            : <ScrollArea className="min-h-0 flex-1" contentClassName="px-5 py-4"><EditorContent editor={editor} onPaste={(event) => {
              const images = Array.from(event.clipboardData.files).filter((file) => file.type.startsWith("image/"));
              if (!images.length) return;
              event.preventDefault();
              void insertInlineImages(images);
            }} onDrop={(event) => {
              const images = Array.from(event.dataTransfer.files).filter((file) => file.type.startsWith("image/"));
              if (!images.length) return;
              event.preventDefault();
              const position = editor?.view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos;
              void insertInlineImages(images, position);
            }} /></ScrollArea>}
          {attachments.length > 0 && <div className="flex flex-wrap gap-2 border-t px-5 py-2">{attachments.map((file, index) => <span className="flex max-w-64 items-center gap-1.5 rounded-md bg-muted px-2 py-1 text-xs" key={`${file.name}-${file.size}-${file.lastModified}-${index}`}><Paperclip className="size-3.5 shrink-0" /><span className="truncate" title={file.name}>{file.name}</span><span className="shrink-0 text-muted-foreground">{formatSize(file.size)}</span><Button type="button" variant="ghost" size="icon" className="size-5" onClick={() => setAttachments((current) => current.filter((_, itemIndex) => itemIndex !== index))} aria-label={`${copy.removeAttachment}: ${file.name}`} title={copy.removeAttachment}><X className="size-3" /></Button></span>)}</div>}
          {error && <p className="px-5 pb-2 text-xs text-destructive">{error}</p>}
          <DialogFooter className="flex-row items-center justify-between border-t px-5 py-3 sm:flex-row sm:justify-between"><input ref={attachmentInputRef} className="sr-only" type="file" multiple onChange={(event) => { const selected = Array.from(event.target.files || []); setAttachments((current) => { const next = [...current, ...selected]; if ([...next, ...inlineImages.map((image) => image.file)].reduce((total, file) => total + file.size, 0) > MAX_COMPOSE_ATTACHMENT_BYTES) { setError(copy.attachmentsTooLarge); return current; } setError(""); return next; }); event.target.value = ""; }} /><input ref={inlineImageInputRef} className="sr-only" type="file" accept="image/jpeg,image/png,image/gif" multiple onChange={(event) => { void insertInlineImages(Array.from(event.target.files || [])); event.target.value = ""; }} /><Button type="button" variant="ghost" size="sm" onClick={() => attachmentInputRef.current?.click()}><Paperclip />{copy.attach}</Button><div className="flex gap-2"><Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>{copy.cancel}</Button><Button type="submit" disabled={mutation.isPending}><Send />{mutation.isPending ? copy.sending : copy.send}</Button></div></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function LoginScreen({ copy }: { copy: Copy }) {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const publicSettings = useQuery({ queryKey: ["public-settings"], queryFn: getPublicSettings, retry: false });
  const mutation = useMutation({
    mutationFn: () => signIn(login, password),
    onSuccess: (result) => window.location.assign(result.next),
    onError: (value) => toast.error(value instanceof ApiError && value.status === 401 ? copy.invalidCredentials : copy.loginFailed),
  });
  return <main className="grid min-h-screen place-items-center bg-background p-6"><div className="grid w-full max-w-sm gap-4 rounded-xl border bg-card p-6 ring-1 ring-foreground/5"><div className="flex items-center gap-2 text-lg font-semibold"><span className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground"><Mail className="size-4" /></span><strong>Inbrix AI</strong></div><h1 className="mt-2 text-2xl font-semibold tracking-tight">{copy.login}</h1><p className="-mt-2 text-sm text-muted-foreground">{copy.appAccount}</p><form className="grid gap-4" onSubmit={(event) => { event.preventDefault(); mutation.mutate(); }}><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="login-account">{copy.appAccount}<Input id="login-account" value={login} onChange={(event) => setLogin(event.target.value)} autoComplete="username" required /></Label><Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="login-password">{copy.password}<Input id="login-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" required /></Label><Button type="submit" className="w-full" disabled={mutation.isPending}>{mutation.isPending ? copy.loading : copy.login}</Button></form>{publicSettings.data?.registrationOpen && <Button nativeButton={false} render={<a href="/register" />} variant="link" size="sm">{copy.createAccount}</Button>}</div></main>;
}

function RegisterScreen({ copy }: { copy: Copy }) {
  const [form, setForm] = useState({ login: "", displayName: "", password: "", confirmation: "" });
  const [error, setError] = useState("");
  const publicSettings = useQuery({ queryKey: ["public-settings"], queryFn: getPublicSettings, retry: false });
  const mutation = useMutation({ mutationFn: () => register(form.login, form.displayName, form.password, form.confirmation), onSuccess: (result) => window.location.assign(result.next), onError: (value) => setError(value instanceof Error ? value.message : copy.loginFailed) });
  const field = (key: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) => setForm((value) => ({ ...value, [key]: event.target.value }));
  return <main className="grid min-h-screen place-items-center bg-background p-6"><div className="grid w-full max-w-sm gap-4 rounded-lg border bg-card p-6"><div className="flex items-center gap-2 text-lg font-semibold"><span className="grid size-8 place-items-center rounded-md bg-primary text-primary-foreground"><Mail className="size-4" /></span>Inbrix AI</div><h1 className="text-xl font-semibold">{copy.createAccount}</h1>{publicSettings.isPending ? <Skeleton className="h-48 w-full" /> : publicSettings.data?.registrationOpen ? <form className="grid gap-3" onSubmit={(event) => { event.preventDefault(); setError(""); mutation.mutate(); }}><Label className="grid gap-1.5">{copy.appAccount}<Input value={form.login} onChange={field("login")} required /></Label><Label className="grid gap-1.5">{copy.displayName}<Input value={form.displayName} onChange={field("displayName")} /></Label><Label className="grid gap-1.5">{copy.password}<Input type="password" minLength={8} value={form.password} onChange={field("password")} required /></Label><Label className="grid gap-1.5">{copy.password}<Input type="password" minLength={8} value={form.confirmation} onChange={field("confirmation")} required /></Label>{error && <p className="text-xs text-destructive">{error}</p>}<Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? copy.loading : copy.createAccount}</Button></form> : <p className="py-6 text-sm text-muted-foreground">{copy.registrationClosed}</p>}<Button nativeButton={false} render={<a href="/login" />} variant="link">{copy.login}</Button></div></main>;
}

type AttachmentKind = "all" | "images" | "pdf" | "documents" | "spreadsheets" | "archives";
type AttachmentPageItem = number | "start-ellipsis" | "end-ellipsis";

function attachmentPageItems(currentPage: number, pageCount: number): AttachmentPageItem[] {
  if (pageCount <= 7) return Array.from({ length: pageCount }, (_, index) => index + 1);
  const pages: AttachmentPageItem[] = [1];
  if (currentPage > 4) pages.push("start-ellipsis");
  for (let page = Math.max(2, currentPage - 1); page <= Math.min(pageCount - 1, currentPage + 1); page += 1) pages.push(page);
  if (currentPage < pageCount - 3) pages.push("end-ellipsis");
  pages.push(pageCount);
  return pages;
}

function attachmentFileIcon(attachment: MailAttachment) {
  const type = attachment.contentType.toLowerCase();
  const filename = attachment.filename.toLowerCase();
  if (type.startsWith("image/")) return FileImage;
  if (type.includes("spreadsheet") || type.includes("excel") || /\.(?:xls|xlsx|csv)$/.test(filename)) return FileSpreadsheet;
  if (type.includes("zip") || type.includes("rar") || type.includes("7z") || /\.(?:zip|rar|7z|gz)$/.test(filename)) return FileArchive;
  return FileText;
}

function attachmentDownloadURL(attachment: MailAttachment, inline = false) {
  const query = new URLSearchParams({ account_email: attachment.accountEmail });
  if (inline) query.set("inline", "true");
  return `/api/attachment/${encodeURIComponent(attachment.id)}?${query.toString()}`;
}

function attachmentMessageURL(attachment: MailAttachment) {
  return `/folder/${encodeURIComponent(attachment.folder)}?message=${encodeURIComponent(attachment.messageId)}`;
}

function AttachmentsPage() {
  const metadata = useQuery({ queryKey: ["conversations", "attachment-shell"], queryFn: () => getConversations() });
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities });
  const locale = useLocale(metadata.data?.locale);
  const [search, setSearch] = useState("");
  const [kind, setKind] = useState<AttachmentKind>("all");
  const [offset, setOffset] = useState(0);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [darkMode, setDarkMode] = useState(prefersDarkMode);
  const debouncedSearch = useDebouncedValue(search, 250);
  const attachments = useQuery({
    queryKey: ["attachments", debouncedSearch, kind, offset],
    queryFn: () => getMailAttachments(debouncedSearch, kind, offset),
  });
  useEffect(() => {
    document.documentElement.classList.toggle("dark", darkMode);
    window.localStorage.setItem("inbrix-theme", darkMode ? "dark" : "light");
  }, [darkMode]);

  const authenticated = (metadata.error instanceof ApiError && metadata.error.status === 401) || (attachments.error instanceof ApiError && attachments.error.status === 401);
  if (authenticated) return <LoginScreen copy={locale} />;
  const kinds: Array<{ value: AttachmentKind; label: string }> = [
    { value: "all", label: locale.allAttachmentTypes },
    { value: "images", label: locale.attachmentImages },
    { value: "pdf", label: locale.attachmentPDF },
    { value: "documents", label: locale.attachmentDocuments },
    { value: "spreadsheets", label: locale.attachmentSpreadsheets },
    { value: "archives", label: locale.attachmentArchives },
  ];
  const pageSize = attachments.data?.limit || 100;
  const pageCount = Math.max(1, Math.ceil((attachments.data?.total || 0) / pageSize));
  const currentPage = Math.min(pageCount, Math.floor(offset / pageSize) + 1);
  const changePage = (page: number) => setOffset((page - 1) * pageSize);
  return (
    <div className="flex h-screen min-h-[32.5rem] overflow-hidden bg-background">
      {sidebarOpen && <button className="fixed inset-0 z-30 bg-black/10 supports-backdrop-filter:backdrop-blur-xs lg:hidden" aria-label={locale.cancel} onClick={() => setSidebarOpen(false)} />}
      <Sidebar copy={locale} folders={metadata.data?.folders || []} accounts={metadata.data?.accounts || []} accountEmail={metadata.data?.accountEmail || ""} calendarEnabled={capabilities.data?.calendar === true} currentView="attachments" onCompose={() => setComposeOpen(true)} onSettings={() => { setSidebarOpen(false); setSettingsOpen(true); }} open={sidebarOpen} onClose={() => setSidebarOpen(false)} darkMode={darkMode} onToggleDarkMode={() => setDarkMode((value) => !value)} />
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex min-h-14 items-center gap-3 border-b bg-card px-3 py-2 sm:px-5">
          <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={() => setSidebarOpen(true)} aria-label={locale.folders} title={locale.folders}><Menu /></Button>
          <div className="min-w-0 flex-1"><h1 className="truncate text-sm font-semibold">{locale.attachmentManager}</h1><p className="text-xs text-muted-foreground">{attachments.data?.total || 0} {locale.attachmentCount}</p></div>
        </header>
        <div className="flex flex-col items-start justify-start gap-2 border-b bg-card px-3 py-3 sm:flex-row sm:items-center sm:px-5">
          <div className="relative flex h-8 w-full min-w-0 items-center sm:w-[220px] sm:flex-none"><Search className="pointer-events-none absolute left-3 size-4 text-muted-foreground" /><Input type="search" className="h-8 w-full bg-muted/60 pl-9 pr-9" value={search} onChange={(event) => { setSearch(event.target.value); setOffset(0); }} placeholder={locale.attachmentSearch} aria-label={locale.attachmentSearch} />{search && <Button variant="ghost" size="icon" className="absolute right-1 size-7" onClick={() => { setSearch(""); setOffset(0); }} aria-label={locale.cancel}><X /></Button>}</div>
          <Select value={kind} onValueChange={(value) => { setKind(value as AttachmentKind); setOffset(0); }}><SelectTrigger className="h-8 w-full min-w-0 sm:w-48" aria-label={locale.allAttachmentTypes}><SelectValue>{kinds.find((item) => item.value === kind)?.label}</SelectValue></SelectTrigger><SelectContent>{kinds.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent></Select>
        </div>
        <div className="min-h-0 flex-1 overflow-auto [&_[data-slot=table-container]]:overflow-visible">
          {attachments.isPending && <ListSkeleton />}
          {!attachments.isPending && attachments.error && <ErrorState copy={locale} onRetry={() => void attachments.refetch()} />}
          {!attachments.isPending && !attachments.error && !attachments.data?.attachments.length && <EmptyState icon={<Paperclip />} text={locale.noAttachments} />}
          {!!attachments.data?.attachments.length && <Table className="min-w-[780px] table-fixed">
            <TableHeader className="sticky top-0 z-10 bg-background">
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-[30%] px-5">{locale.attachmentName}</TableHead>
                <TableHead className="w-[27%]">{locale.attachmentMessage}</TableHead>
                <TableHead className="w-[17%]">{locale.attachmentSender}</TableHead>
                <TableHead className="w-[9%] text-right">{locale.attachmentSize}</TableHead>
                <TableHead className="w-[8rem] text-right">{locale.attachmentDate}</TableHead>
                <TableHead className="w-[7.5rem] pr-5 text-right"><span className="sr-only">{locale.attachmentActions}</span></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {attachments.data.attachments.map((attachment) => {
                const Icon = attachmentFileIcon(attachment);
                return <TableRow key={`${attachment.folder}/${attachment.messageId}/${attachment.partId || attachment.id}`}>
                  <TableCell className="px-5 py-2.5"><div className="flex min-w-0 items-center gap-2.5"><span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground"><Icon className="size-4" /></span><strong className="min-w-0 truncate text-sm font-medium" title={attachment.filename}>{attachment.filename || locale.noSubject}</strong></div></TableCell>
                  <TableCell><a className="block truncate text-sm hover:underline" href={attachmentMessageURL(attachment)} title={attachment.messageSubject}>{attachment.messageSubject || locale.noSubject}</a></TableCell>
                  <TableCell><span className="block truncate text-sm text-muted-foreground" title={attachment.fromName || attachment.messageFrom}>{attachment.fromName || attachment.messageFrom}</span></TableCell>
                  <TableCell className="text-right text-sm text-muted-foreground">{formatSize(attachment.size)}</TableCell>
                  <TableCell className="w-[8rem] max-w-[8rem] text-right text-xs text-muted-foreground"><time className="block truncate" title={formatTime(attachment.messageDate)}>{formatTime(attachment.messageDate)}</time></TableCell>
                  <TableCell className="pr-5"><div className="flex items-center justify-end gap-0.5"><Button nativeButton={false} render={<a href={attachmentDownloadURL(attachment, true)} target="_blank" rel="noreferrer" aria-label={locale.previewAttachment} title={locale.previewAttachment} />} variant="ghost" size="icon" className="size-8"><Eye /></Button><Button nativeButton={false} render={<a href={attachmentDownloadURL(attachment)} aria-label={locale.downloadAttachment} title={locale.downloadAttachment} />} variant="ghost" size="icon" className="size-8"><Download /></Button><Button nativeButton={false} render={<a href={attachmentMessageURL(attachment)} aria-label={locale.viewOriginalMessage} title={locale.viewOriginalMessage} />} variant="ghost" size="icon" className="size-8"><ExternalLink /></Button></div></TableCell>
                </TableRow>;
              })}
            </TableBody>
          </Table>}
        </div>
        {attachments.data && attachments.data.total > 0 && <footer className="flex min-h-11 flex-col items-center justify-between gap-1 px-3 py-1.5 sm:flex-row sm:px-5">
          <span className="shrink-0 text-xs text-muted-foreground">{offset + 1}-{Math.min(offset + attachments.data.attachments.length, attachments.data.total)} / {attachments.data.total}</span>
          <Pagination className="mx-0 w-auto justify-end">
            <PaginationContent>
              <PaginationItem><PaginationPrevious href="#" text={locale.previousPage} aria-label={locale.previousPage} aria-disabled={currentPage === 1} tabIndex={currentPage === 1 ? -1 : undefined} className={cn(currentPage === 1 && "pointer-events-none opacity-50")} onClick={(event) => { event.preventDefault(); if (currentPage > 1) changePage(currentPage - 1); }} /></PaginationItem>
              {attachmentPageItems(currentPage, pageCount).map((item) => typeof item === "number"
                ? <PaginationItem key={item}><PaginationLink href="#" isActive={item === currentPage} aria-label={`${locale.attachmentPage} ${item}`} onClick={(event) => { event.preventDefault(); changePage(item); }}>{item}</PaginationLink></PaginationItem>
                : <PaginationItem key={item}><PaginationEllipsis /></PaginationItem>)}
              <PaginationItem><PaginationNext href="#" text={locale.nextPage} aria-label={locale.nextPage} aria-disabled={currentPage === pageCount} tabIndex={currentPage === pageCount ? -1 : undefined} className={cn(currentPage === pageCount && "pointer-events-none opacity-50")} onClick={(event) => { event.preventDefault(); if (currentPage < pageCount) changePage(currentPage + 1); }} /></PaginationItem>
            </PaginationContent>
          </Pagination>
        </footer>}
      </main>
      <ComposeDialog copy={locale} open={composeOpen} defaults={{ to: "", subject: "" }} accountEmail={metadata.data?.accountEmail || ""} onOpenChange={setComposeOpen} onSent={() => void attachments.refetch()} />
      <SettingsDialog copy={locale} open={settingsOpen} onOpenChange={setSettingsOpen} />
    </div>
  );
}

function messageIsUnread(message: Pick<MailMessage, "flags">) {
  return !message.flags?.some((flag) => flag.toLowerCase() === "\\seen");
}

function flagsMarkedSeen(flags: string[] = []) {
  return flags.some((flag) => flag.toLowerCase() === "\\seen") ? flags : [...flags, "\\Seen"];
}

function FolderMessageRow({ copy, message, address, selected, junkActions, actionPending, onSelect, onNotSpam, onPermanentDelete }: { copy: Copy; message: MailMessage; address: string; selected: boolean; junkActions: boolean; actionPending: boolean; onSelect: () => void; onNotSpam: () => void; onPermanentDelete: () => void }) {
  const unread = messageIsUnread(message);
  const row = <button type="button" onClick={onSelect} className={cn("block w-full border-b bg-card px-4 py-3 text-left transition-colors hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50", selected && "border-l-2 border-l-foreground bg-muted pl-[0.875rem]")}><span className="flex items-baseline justify-between gap-2"><strong className={cn("min-w-0 truncate text-sm", unread ? "font-semibold text-foreground" : "font-medium text-muted-foreground")}>{address}</strong><span className="flex shrink-0 items-center gap-2">{unread && <span className="size-1.5 rounded-full bg-primary" aria-label={copy.unread} title={copy.unread} />}<time className="text-[10px] text-muted-foreground">{formatTime(message.date)}</time></span></span><span className={cn("mt-1 block truncate text-xs", unread ? "text-foreground/80" : "text-muted-foreground/70")}>{message.subject || copy.noSubject}</span></button>;
  if (!junkActions) return row;
  return (
    <ContextMenu>
      <ContextMenuTrigger className="block">{row}</ContextMenuTrigger>
      <ContextMenuContent className="w-44">
        <ContextMenuItem disabled={actionPending} className="gap-2 px-2 py-2" onClick={onNotSpam}><ShieldCheck className="size-4" />{copy.notSpam}</ContextMenuItem>
        <ContextMenuItem variant="destructive" disabled={actionPending} className="gap-2 px-2 py-2" onClick={onPermanentDelete}><Trash2 className="size-4" />{copy.permanentDelete}</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
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
  const markingReadRef = useRef(new Set<string>());
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
    window.localStorage.setItem("inbrix-theme", darkMode ? "dark" : "light");
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
  useEffect(() => {
    if (!selected) return;
    const message = list.data?.messages.find((item) => item.id === selected);
    if (!message || !messageIsUnread(message)) return;
    const accountEmail = message.accountEmail || metadata.data?.accountEmail || "";
    const key = `${accountEmail}/${folder}/${message.id}`;
    if (markingReadRef.current.has(key)) return;
    markingReadRef.current.add(key);
    void markMailMessageRead(folder, message.id, accountEmail).then(() => {
      queryClient.setQueryData<{ messages: MailMessage[]; syncComplete: boolean; syncError?: string }>(["folder", folder], (current) => current ? {
        ...current,
        messages: current.messages.map((item) => item.id === message.id ? { ...item, flags: flagsMarkedSeen(item.flags) } : item),
      } : current);
      queryClient.setQueryData<MailMessage>(["message", folder, message.id], (current) => current ? { ...current, flags: flagsMarkedSeen(current.flags) } : current);
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
    }).catch((value) => {
      markingReadRef.current.delete(key);
      toast.error(value instanceof Error ? value.message : locale.loadFailed);
    });
  }, [folder, list.data?.messages, locale.loadFailed, metadata.data?.accountEmail, queryClient, selected]);
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
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities, retry: false });
  const isSuperAdmin = capabilities.data?.role === "super_admin";
  const [section, setSection] = useState<"general" | "signatures" | "ai" | "agents" | "mailboxes" | "system">("general");
  return (
    <div className="grid min-h-[32rem] md:grid-cols-[12rem_minmax(0,1fr)]">
      <nav className="flex gap-1 overflow-x-auto border-b pb-4 md:flex-col md:overflow-visible md:border-r md:border-b-0 md:pr-4" aria-label={copy.settings}>
        <Button className="shrink-0 justify-start" variant={section === "general" ? "secondary" : "ghost"} onClick={() => setSection("general")}><Settings />{copy.generalSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "signatures" ? "secondary" : "ghost"} onClick={() => setSection("signatures")}><SignatureIcon />{copy.signatureSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "ai" ? "secondary" : "ghost"} onClick={() => setSection("ai")}><Sparkles />{copy.aiSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "agents" ? "secondary" : "ghost"} onClick={() => setSection("agents")}><Bot />{copy.agentSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "mailboxes" ? "secondary" : "ghost"} onClick={() => setSection("mailboxes")}><Mail />{copy.mailboxManagement}</Button>
        {isSuperAdmin && <Button className="shrink-0 justify-start" variant={section === "system" ? "secondary" : "ghost"} onClick={() => setSection("system")}><ShieldCheck />{copy.systemSettings}</Button>}
      </nav>
      <div className="min-w-0 pt-5 md:pt-0 md:pl-6">
        {section === "general" ? <GeneralSettings copy={copy} /> : section === "signatures" ? <SignatureSettings copy={copy} /> : section === "ai" ? <AISettings copy={copy} /> : section === "agents" ? <AgentSettings copy={copy} /> : section === "system" && isSuperAdmin ? <SystemSettings copy={copy} /> : <MailboxSettings copy={copy} />}
      </div>
    </div>
  );
}

function SystemSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ["system-settings"], queryFn: getSystemSettings, retry: false });
  const updateRegistration = useMutation({
    mutationFn: updateRegistrationOpen,
    onSuccess: (updated) => {
      queryClient.setQueryData<SystemSettingsData>(["system-settings"], (current) => current ? { ...current, registrationOpen: updated.registrationOpen } : current);
      queryClient.setQueryData(["public-settings"], updated);
      toast.success(copy.registrationSettingUpdated);
    },
    onError: (value) => toast.error(value instanceof Error ? value.message : copy.loadFailed),
  });
  const updateRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: UserRole }) => updateSystemUserRole(id, role),
    onSuccess: (updated) => {
      queryClient.setQueryData<SystemSettingsData>(["system-settings"], (current) => current ? {
        ...current,
        users: current.users.map((user) => user.id === updated.id ? updated : user),
      } : current);
      toast.success(copy.roleUpdated);
    },
    onError: (value) => toast.error(value instanceof Error ? value.message : copy.loadFailed),
  });
  return (
    <section className="min-w-0">
      <h2 className="text-lg font-semibold">{copy.systemSettings}</h2>
      <p className="mt-1 text-sm text-muted-foreground">{copy.systemSettingsDescription}</p>
      {settings.isPending && <div className="mt-6 grid gap-3"><Skeleton className="h-16 w-full" /><Skeleton className="h-40 w-full" /></div>}
      {settings.error && <p className="mt-6 text-sm text-destructive">{settings.error.message}</p>}
      {settings.data && <>
        <div className="mt-6 flex items-center justify-between border-y py-4 text-sm"><span className="text-muted-foreground">{copy.systemVersion}</span><strong className="font-mono font-medium">{settings.data.version}</strong></div>
        <div className="flex items-center justify-between gap-6 border-b py-4">
          <div><Label htmlFor="open-registration">{copy.openRegistration}</Label><p className="mt-1 text-xs text-muted-foreground">{copy.openRegistrationDescription}</p></div>
          <Switch id="open-registration" checked={settings.data.registrationOpen} disabled={updateRegistration.isPending} onCheckedChange={(checked) => updateRegistration.mutate(checked)} />
        </div>
        <div className="mt-6">
          <h3 className="text-sm font-semibold">{copy.userManagement}</h3>
          <div className="mt-3 overflow-hidden rounded-lg border">
            <Table className="min-w-[38rem] table-fixed">
              <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[44%] px-4">{copy.account}</TableHead><TableHead className="w-[28%] px-4">{copy.role}</TableHead><TableHead className="w-[28%] px-4">{copy.actions}</TableHead></TableRow></TableHeader>
              <TableBody>
                {settings.data.users.map((user) => {
                  const isCurrent = user.id === settings.data.currentUserId;
                  return <TableRow key={user.id}>
                    <TableCell className="px-4 py-3"><strong className="block truncate font-medium">{user.displayName || user.login}</strong><span className="block truncate text-xs text-muted-foreground">{user.login}</span></TableCell>
                    <TableCell className="px-4 py-3"><Badge variant={user.role === "super_admin" ? "default" : "secondary"}>{user.role === "super_admin" ? copy.superAdmin : copy.ordinaryUser}</Badge></TableCell>
                    <TableCell className="px-4 py-3">
                      {isCurrent ? <span className="text-xs text-muted-foreground">{copy.currentUser}</span> : <Select value={user.role} disabled={updateRole.isPending} onValueChange={(role) => updateRole.mutate({ id: user.id, role: role as UserRole })}><SelectTrigger className="w-full" aria-label={`${user.login} ${copy.role}`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="user">{copy.ordinaryUser}</SelectItem><SelectItem value="super_admin">{copy.superAdmin}</SelectItem></SelectContent></Select>}
                    </TableCell>
                  </TableRow>;
                })}
                {!settings.data.users.length && <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.noUsers}</TableCell></TableRow>}
              </TableBody>
            </Table>
          </div>
        </div>
      </>}
    </section>
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
  const editor = useEditor({ extensions: [StarterKit.configure({ paragraph: false }), EmailParagraph, EmailImage, UnderlineExtension, LinkExtension.configure({ openOnClick: false }), Placeholder.configure({ placeholder: copy.signatureContent })], content: "", immediatelyRender: false, onUpdate: ({ editor: currentEditor }) => setContentEmpty(currentEditor.isEmpty) });
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
    setSourceCode(serializeEmailHTML(editor.getHTML()));
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
    const html = serializeEmailHTML(sourceMode ? sourceCode : editor?.getHTML() || "");
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
      <div className="mt-5 overflow-hidden rounded-lg border">
        <Table className="min-w-[36rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[28%] px-4">{copy.signatureName}</TableHead><TableHead className="px-4">{copy.signaturePreview}</TableHead><TableHead className="w-24 px-4 text-right">{copy.actions}</TableHead></TableRow></TableHeader>
          <TableBody>{signatures.isPending ? <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.loading}</TableCell></TableRow> : signatures.data?.signatures.length ? signatures.data.signatures.map((signature) => <TableRow key={signature.id}><TableCell className="px-4 py-3"><div className="flex items-center gap-2"><span className="truncate font-medium">{signature.name}</span>{signature.default && <Badge>{copy.defaultSignature}</Badge>}</div></TableCell><TableCell className="px-4 py-3 text-muted-foreground whitespace-normal"><p className="line-clamp-2 whitespace-pre-line">{htmlToPlainText(signature.html) || "-"}</p></TableCell><TableCell className="px-4 py-3"><div className="flex justify-end"><Button variant="ghost" size="icon" onClick={() => openEdit(signature)} aria-label={copy.editSignature} title={copy.editSignature}><Pencil /></Button><Button variant="ghost" size="icon" className="text-destructive" onClick={() => { setError(""); setDeleteTarget(signature); }} aria-label={copy.remove} title={copy.remove}><Trash2 /></Button></div></TableCell></TableRow>) : <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.noSignatures}</TableCell></TableRow>}</TableBody>
        </Table>
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

function splitOutputLabels(value: string) {
  return value.split(/[,，;；、\n]+/).map((item) => item.trim()).filter(Boolean);
}

function validOutputLabel(value: string) {
  return Boolean(value.trim()) && Array.from(value.trim()).length <= 20 && !/[\r\n：:]/.test(value);
}

function AgentOutputLabelInput({ copy, labels, onChange, draftRef, disabled }: { copy: Copy; labels: string[]; onChange: (labels: string[]) => void; draftRef: MutableRefObject<string>; disabled: boolean }) {
  const [draft, setDraft] = useState("");
  const [invalid, setInvalid] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const addValues = (values: string[]) => {
    const candidates = values.map((value) => value.trim()).filter(Boolean);
    if (!candidates.length) return true;
    if (labels.length + candidates.length > 12 || candidates.some((value) => !validOutputLabel(value))) {
      setInvalid(true);
      return false;
    }
    const existing = new Set(labels);
    const next = [...labels];
    for (const value of candidates) {
      if (!existing.has(value)) {
        existing.add(value);
        next.push(value);
      }
    }
    if (next.length > 12) {
      setInvalid(true);
      return false;
    }
    onChange(next);
    setDraft("");
    draftRef.current = "";
    setInvalid(false);
    return true;
  };
  return (
    <div>
      <div className={cn("flex min-h-9 flex-wrap items-center gap-1.5 rounded-lg border border-input bg-transparent px-2 py-1 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50", invalid && "border-destructive focus-within:border-destructive focus-within:ring-destructive/20", disabled && "cursor-not-allowed opacity-50")} onClick={() => inputRef.current?.focus()}>
        {labels.map((label, index) => <span className="flex max-w-full items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-sm" key={`${label}-${index}`}><span className="truncate" title={label}>{label}</span><button type="button" className="grid size-4 shrink-0 place-items-center rounded-sm text-muted-foreground hover:bg-background hover:text-foreground" disabled={disabled} onClick={(event) => { event.stopPropagation(); onChange(labels.filter((_, itemIndex) => itemIndex !== index)); }} aria-label={`${copy.removeAgentOutputLabel}: ${label}`} title={copy.removeAgentOutputLabel}><X className="size-3" /></button></span>)}
        <input ref={inputRef} className="h-6 min-w-40 flex-1 bg-transparent px-0.5 text-sm outline-none placeholder:text-muted-foreground" value={draft} disabled={disabled || labels.length >= 12} onChange={(event) => { setDraft(event.target.value); draftRef.current = event.target.value; if (invalid) setInvalid(false); }} onKeyDown={(event) => {
          if (["Enter", ",", "，", ";", "；", "Tab"].includes(event.key) && draft.trim()) {
            event.preventDefault();
            addValues(splitOutputLabels(draft));
          } else if (event.key === "Backspace" && !draft && labels.length) {
            onChange(labels.slice(0, -1));
          }
        }} onPaste={(event) => {
          const value = event.clipboardData.getData("text");
          if (!/[,，;；、\n]/.test(value)) return;
          event.preventDefault();
          addValues(splitOutputLabels(value));
        }} onBlur={() => { if (draft.trim()) addValues(splitOutputLabels(draft)); }} placeholder={labels.length ? "" : copy.agentOutputLabelsPlaceholder} aria-invalid={invalid} aria-label={copy.agentOutputLabels} />
      </div>
      {invalid && <p className="mt-1 text-xs text-destructive">{copy.invalidAgentOutputLabel}</p>}
    </div>
  );
}

function AgentSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const agents = useQuery({ queryKey: ["ai-agents"], queryFn: getAIAgents, retry: false });
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<AIAgent | null>(null);
  const [name, setName] = useState("");
  const [prompt, setPrompt] = useState("");
  const [outputLabels, setOutputLabels] = useState<string[]>(["客户", "需求", "要求", "问题"]);
  const outputLabelDraftRef = useRef("");
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
  const openAdd = () => { setEditing(null); setName(""); setPrompt(""); setOutputLabels(["客户", "需求", "要求", "问题"]); outputLabelDraftRef.current = ""; setError(""); setOpen(true); };
  const openEdit = (agent: AIAgent) => { setEditing(agent); setName(agent.name); setPrompt(agent.prompt); setOutputLabels(agent.outputLabels); outputLabelDraftRef.current = ""; setError(""); setOpen(true); };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    setError("");
    const pendingLabels = splitOutputLabels(outputLabelDraftRef.current);
    if (pendingLabels.some((label) => !validOutputLabel(label))) {
      setError(copy.invalidAgentOutputLabel);
      return;
    }
    const submittedLabels = [...outputLabels];
    for (const label of pendingLabels) if (!submittedLabels.includes(label)) submittedLabels.push(label);
    if (submittedLabels.length > 12) {
      setError(copy.invalidAgentOutputLabel);
      return;
    }
    const input = { name: name.trim(), prompt: prompt.trim(), outputLabels: submittedLabels };
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
      <div className="mt-5 overflow-hidden rounded-lg border">
        <Table className="min-w-[36rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[28%] px-4">{copy.agentName}</TableHead><TableHead className="px-4">{copy.agentPrompt}</TableHead><TableHead className="w-20 px-4 text-right">{copy.actions}</TableHead></TableRow></TableHeader>
          <TableBody>{agents.isPending ? <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.loading}</TableCell></TableRow> : agents.data?.agents.length ? agents.data.agents.map((agent) => <TableRow key={agent.id}><TableCell className="px-4 py-3 font-medium"><span className="block truncate">{agent.name}</span></TableCell><TableCell className="px-4 py-3 text-muted-foreground whitespace-normal"><p className="line-clamp-2 whitespace-pre-line">{agent.prompt}</p><p className="mt-1 truncate text-xs" title={agent.outputLabels.join(" · ")}>{agent.outputLabels.join(" · ")}</p></TableCell><TableCell className="px-4 py-3"><div className="flex justify-end"><Button variant="ghost" size="icon" onClick={() => openEdit(agent)} aria-label={copy.editAgent} title={copy.editAgent}><Pencil /></Button></div></TableCell></TableRow>) : <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.noAgents}</TableCell></TableRow>}</TableBody>
        </Table>
      </div>
      {(agents.isError || error) && <p className="mt-3 text-xs text-destructive">{error || (agents.error instanceof Error ? agents.error.message : copy.loadFailed)}</p>}
      <Dialog open={open} onOpenChange={(next) => { setOpen(next); if (!next) setEditing(null); }}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader><DialogTitle>{editing ? copy.editAgent : copy.addAgent}</DialogTitle><DialogDescription>{copy.agentSettingsDescription}</DialogDescription></DialogHeader>
          <form className="grid gap-4" onSubmit={submit}>
            <div className="grid gap-2"><Label htmlFor="agent-name">{copy.agentName}</Label><Input id="agent-name" value={name} onChange={(event) => setName(event.target.value)} disabled={pending} required /></div>
            <div className="grid gap-2"><div><Label>{copy.agentOutputLabels}</Label><p className="mt-1 text-xs text-muted-foreground">{copy.agentOutputLabelsDescription}</p></div><AgentOutputLabelInput copy={copy} labels={outputLabels} onChange={setOutputLabels} draftRef={outputLabelDraftRef} disabled={pending} /></div>
            <div className="grid gap-2"><Label htmlFor="agent-prompt">{copy.agentPrompt}</Label><Textarea id="agent-prompt" className="min-h-64 resize-y" value={prompt} onChange={(event) => setPrompt(event.target.value)} disabled={pending} required /></div>
            {(create.isError || update.isError) && error && <p className="text-xs text-destructive">{error}</p>}
            <DialogFooter><Button type="button" variant="ghost" onClick={() => setOpen(false)}>{copy.cancel}</Button><Button type="submit" disabled={pending || !name.trim() || !prompt.trim()}>{pending ? copy.savingAgent : editing ? copy.editAgent : copy.addAgent}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function MailboxAITaskSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const agents = useQuery({ queryKey: ["ai-agents"], queryFn: getAIAgents, retry: false });
  const models = useQuery({ queryKey: ["ai-models"], queryFn: getAIModels, retry: false });
  const bindings = useQuery({ queryKey: ["ai-task-bindings"], queryFn: getAITaskBindings, retry: false });
  const [drafts, setDrafts] = useState<Record<string, { agentId: string; modelId: string }>>({});
  const [error, setError] = useState("");
  const [promptAgent, setPromptAgent] = useState<AIAgent | null>(null);
  const [promptDraft, setPromptDraft] = useState("");

  useEffect(() => {
    if (!bindings.data) return;
    setDrafts(Object.fromEntries(bindings.data.bindings.map((binding) => [`${binding.accountEmail}\u0000${binding.taskType}`, { agentId: binding.agentId, modelId: binding.modelId }])));
  }, [bindings.data]);

  const saveBinding = useMutation({
    mutationFn: saveAITaskBinding,
    onSuccess: () => {
      setError("");
      toast.success(copy.mailboxAIConfigurationSaved);
      void queryClient.invalidateQueries({ queryKey: ["ai-task-bindings"] });
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
      void queryClient.invalidateQueries({ queryKey: ["suggested-reply"] });
    },
    onError: (value) => {
      const message = value instanceof Error ? value.message : copy.loadFailed;
      setError(message);
      toast.error(message);
      void queryClient.invalidateQueries({ queryKey: ["ai-task-bindings"] });
    },
  });
  const updatePrompt = useMutation({
    mutationFn: () => updateAIAgent(promptAgent!.id, { name: promptAgent!.name, prompt: promptDraft.trim(), outputLabels: promptAgent!.outputLabels }),
    onSuccess: () => {
      setPromptAgent(null);
      setPromptDraft("");
      setError("");
      toast.success(copy.agentUpdated);
      void queryClient.invalidateQueries({ queryKey: ["ai-agents"] });
      void queryClient.invalidateQueries({ queryKey: ["suggested-reply"] });
    },
    onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed),
  });
  const updateAndSave = (binding: AITaskBinding, field: "agentId" | "modelId", value: string) => {
    const key = `${binding.accountEmail}\u0000${binding.taskType}`;
    const current = drafts[key] || { agentId: binding.agentId, modelId: binding.modelId };
    const next = { ...current, [field]: value };
    setDrafts((items) => ({ ...items, [key]: next }));
    if (!next.agentId || !next.modelId) return;
    setError("");
    saveBinding.mutate({ accountEmail: binding.accountEmail, taskType: binding.taskType, ...next });
  };
  const loadError = bindings.error instanceof Error ? bindings.error.message : models.error instanceof Error ? models.error.message : agents.error instanceof Error ? agents.error.message : "";

  return (
    <section className="mt-8 border-t pt-6">
      <div>
        <h3 className="text-base font-semibold">{copy.mailboxAIConfiguration}</h3>
        <p className="mt-1 text-sm text-muted-foreground">{copy.mailboxAIConfigurationDescription}</p>
      </div>
      <div className="mt-4 overflow-hidden rounded-lg border">
        <Table className="min-w-[48rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground">
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-[26%] px-4">{copy.email}</TableHead>
              <TableHead className="w-[20%] px-4">{copy.aiTask}</TableHead>
              <TableHead className="w-[27%] px-4">{copy.agentSettings}</TableHead>
              <TableHead className="w-[27%] px-4">{copy.aiModel}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(bindings.isPending || models.isPending || agents.isPending) ? (
              <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={4}>{copy.loading}</TableCell></TableRow>
            ) : bindings.data?.bindings.length ? bindings.data.bindings.map((binding) => {
              const draft = drafts[`${binding.accountEmail}\u0000${binding.taskType}`] || { agentId: binding.agentId, modelId: binding.modelId };
              const saving = saveBinding.isPending && saveBinding.variables?.accountEmail === binding.accountEmail && saveBinding.variables?.taskType === binding.taskType;
              const availableAgents = agents.data?.agents || [];
              const selectableAgents = binding.taskType === "mail_summary" ? availableAgents.filter((agent) => agent.outputLabels.length > 0) : availableAgents;
              const selectedAgent = selectableAgents.find((agent) => agent.id === draft.agentId);
              return (
                <TableRow key={`${binding.accountEmail}:${binding.taskType}`}>
                  <TableCell className="px-4 py-3">
                    <div className="min-w-0"><span className="block truncate font-medium" title={binding.accountEmail}>{binding.accountEmail}</span><span className="mt-1 block text-xs text-muted-foreground">{binding.explicit ? copy.explicitConfiguration : copy.inheritedConfiguration}</span></div>
                  </TableCell>
                  <TableCell className="px-4 py-3 text-sm">{binding.taskType === "mail_summary" ? copy.mailSummaryAgent : binding.taskType === "reply_suggestion" ? copy.replySuggestionAgent : copy.emailDraftAgent}</TableCell>
                  <TableCell className="px-4 py-3">
                    <div className="flex items-center gap-1">
                      <Select value={draft.agentId} onValueChange={(value) => updateAndSave(binding, "agentId", value || "")} disabled={!selectableAgents.length || saving}>
                        <SelectTrigger className="min-w-0 flex-1"><SelectValue>{selectedAgent?.name || copy.noAgents}</SelectValue></SelectTrigger>
                        <SelectContent>{selectableAgents.map((agent) => <SelectItem key={agent.id} value={agent.id}>{agent.name}</SelectItem>)}</SelectContent>
                      </Select>
                      {binding.taskType === "reply_suggestion" && <Button type="button" variant="ghost" size="icon" className="shrink-0" disabled={!selectedAgent || saving} onClick={() => { if (selectedAgent) { setPromptAgent(selectedAgent); setPromptDraft(selectedAgent.prompt); setError(""); } }} aria-label={copy.editReplySuggestionPrompt} title={copy.editReplySuggestionPrompt}><Pencil /></Button>}
                    </div>
                  </TableCell>
                  <TableCell className="px-4 py-3">
                    <Select value={draft.modelId} onValueChange={(value) => updateAndSave(binding, "modelId", value || "")} disabled={!models.data?.models.length || saving}>
                      <SelectTrigger className="w-full"><SelectValue>{models.data?.models.find((model) => model.id === draft.modelId)?.model || copy.noAIModels}</SelectValue></SelectTrigger>
                      <SelectContent>{models.data?.models.map((model) => <SelectItem key={model.id} value={model.id}>{model.model}</SelectItem>)}</SelectContent>
                    </Select>
                  </TableCell>
                </TableRow>
              );
            }) : (
              <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={4}>{copy.noAccounts}</TableCell></TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {(bindings.isError || models.isError || agents.isError || error) && <p className="mt-3 text-xs text-destructive">{error || loadError || copy.loadFailed}</p>}
      <Dialog open={Boolean(promptAgent)} onOpenChange={(open) => { if (!open && !updatePrompt.isPending) { setPromptAgent(null); setPromptDraft(""); } }}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader><DialogTitle>{copy.editReplySuggestionPrompt}</DialogTitle><DialogDescription>{copy.replySuggestionPromptDescription}</DialogDescription></DialogHeader>
          <form className="grid gap-4" onSubmit={(event) => { event.preventDefault(); if (promptDraft.trim()) updatePrompt.mutate(); }}>
            <div className="grid gap-2"><Label htmlFor="reply-suggestion-prompt">{copy.agentPrompt}</Label><Textarea id="reply-suggestion-prompt" className="min-h-64 resize-y" value={promptDraft} onChange={(event) => { setPromptDraft(event.target.value); setError(""); }} disabled={updatePrompt.isPending} required /></div>
            {updatePrompt.isError && error && <p className="text-xs text-destructive">{error}</p>}
            <DialogFooter><Button type="button" variant="ghost" disabled={updatePrompt.isPending} onClick={() => { setPromptAgent(null); setPromptDraft(""); }}>{copy.cancel}</Button><Button type="submit" disabled={updatePrompt.isPending || !promptDraft.trim()}>{updatePrompt.isPending ? copy.savingAgent : copy.save}</Button></DialogFooter>
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
      <div className="mt-5 overflow-hidden rounded-lg border">
        <Table className="min-w-[46rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[19%] px-4">{copy.aiModel}</TableHead><TableHead className="w-[12%] px-4">{copy.aiProvider}</TableHead><TableHead className="w-[16%] px-4">{copy.aiReasoningEffort}</TableHead><TableHead className="px-4">{copy.aiBaseURL}</TableHead><TableHead className="w-52 px-4 text-right">{copy.actions}</TableHead></TableRow></TableHeader>
          <TableBody>{models.isPending ? <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={5}>{copy.loading}</TableCell></TableRow> : models.data?.models.length ? models.data.models.map((item) => <TableRow key={item.id}><TableCell className="px-4 py-3"><div className="flex min-w-0 items-center gap-2"><span className="truncate font-medium">{item.model}</span>{item.isDefault && <Badge>{copy.defaultModel}</Badge>}</div></TableCell><TableCell className="px-4 py-3">OpenAI</TableCell><TableCell className="px-4 py-3">{item.reasoningEffort === "low" ? copy.aiReasoningLow : copy.aiReasoningMedium}</TableCell><TableCell className="px-4 py-3 text-muted-foreground"><span className="block truncate" title={item.baseUrl}>{item.baseUrl}</span></TableCell><TableCell className="px-4 py-3"><div className="flex justify-end gap-1">{!item.isDefault && <Button variant="outline" size="sm" disabled={makeDefault.isPending} onClick={() => makeDefault.mutate(item.id)}>{copy.setDefaultModel}</Button>}<Button variant="ghost" size="icon" onClick={() => openEdit(item)} aria-label={copy.editAIModel} title={copy.editAIModel}><Pencil /></Button><Button variant="ghost" size="icon" className="text-destructive" disabled={remove.isPending} onClick={() => remove.mutate(item.id)} aria-label={copy.remove} title={copy.remove}><Trash2 /></Button></div></TableCell></TableRow>) : <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={5}>{copy.noAIModels}</TableCell></TableRow>}</TableBody>
        </Table>
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
    void queryClient.invalidateQueries({ queryKey: ["ai-task-bindings"] });
  };
  const remove = useMutation({ mutationFn: deleteAccount, onSuccess: () => { setError(""); refreshAccountData(); }, onError: (value) => setError(value instanceof Error ? value.message : copy.loadFailed) });
  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="text-lg font-semibold">{copy.mailboxManagement}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.mailboxDescription}</p></div>
        <Button onClick={() => setAddOpen(true)}><Plus />{copy.addAccount}</Button>
      </div>
      <div className="mt-5 overflow-hidden rounded-lg border">
        <Table className="min-w-[44rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[34%] px-4">{copy.account}</TableHead><TableHead className="w-[24%] px-4">IMAP</TableHead><TableHead className="w-[24%] px-4">SMTP</TableHead><TableHead className="w-[18%] px-4 text-right">{copy.actions}</TableHead></TableRow></TableHeader>
          <TableBody>
            {accounts.isPending && <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={4}>{copy.loading}</TableCell></TableRow>}
            {accounts.data?.accounts.map((account: ConnectedAccount) => (
              <TableRow key={account.email}>
                <TableCell className="px-4 py-3"><div className="flex min-w-0 items-center gap-2.5"><span className="size-3 shrink-0 rounded-full" style={{ backgroundColor: account.color || "#777" }} /><div className="min-w-0"><strong className="block truncate font-medium">{account.label || account.email}</strong><span className="block truncate text-xs text-muted-foreground">{account.email}</span></div></div></TableCell>
                <TableCell className="px-4 py-3"><span className="block truncate" title={`${account.imapServer}${account.imapPort ? `:${account.imapPort}` : ""}`}>{account.imapServer}{account.imapPort ? `:${account.imapPort}` : ""}</span></TableCell>
                <TableCell className="px-4 py-3"><span className="block truncate" title={`${account.smtpServer || "-"}${account.smtpPort ? `:${account.smtpPort}` : ""}`}>{account.smtpServer || "-"}{account.smtpPort ? `:${account.smtpPort}` : ""}</span></TableCell>
                <TableCell className="px-4 py-3 text-right"><Button variant="ghost" size="sm" disabled={remove.isPending} onClick={() => remove.mutate(account.email)}>{copy.remove}</Button></TableCell>
              </TableRow>
            ))}
            {!accounts.isPending && !accounts.data?.accounts.length && <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={4}>{copy.noAccounts}</TableCell></TableRow>}
          </TableBody>
        </Table>
      </div>
      {accounts.error && <p className="py-3 text-sm text-destructive">{accounts.error.message}</p>}
      {error && <p className="py-2 text-xs text-destructive">{error}</p>}
      <MailboxAITaskSettings copy={copy} />
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
    window.localStorage.setItem("inbrix-theme", darkMode ? "dark" : "light");
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
