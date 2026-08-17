export type Mailbox = {
  name: string;
  delimiter: string;
  attributes: string[];
  unreadCount?: number;
};

export type MailAccount = {
  email: string;
  label: string;
  color?: string;
  isActive: boolean;
};

export type Attachment = {
  id: string;
  partId: string;
  filename: string;
  contentType: string;
  size: number;
  isInline: boolean;
  contentId?: string;
};

export type MailSummary = {
  text: string;
  status: "ready" | "generating" | "failed";
  stale: boolean;
  updatedAt?: string;
};

export type ConversationSummary = {
  id: string;
  title: string;
  peerEmail?: string;
  subject: string;
  preview: string;
  date: string;
  count: number;
  unreadCount: number;
  hasAttachments: boolean;
  accountEmail?: string;
  accountLabel?: string;
  accountColor?: string;
  note?: string;
};

export type ConversationMessage = {
  id: string;
  folder?: string;
  from: string;
  fromName?: string;
  to: string;
  cc?: string;
  subject: string;
  preview: string;
  body: string;
  html?: string;
  date: string;
  hasAttachments: boolean;
  flags?: string[];
  attachments?: Attachment[];
  messageId?: string;
  inReplyTo?: string;
  references?: string[];
  outgoing: boolean;
  mailSummary?: MailSummary;
};

export type ConversationDetail = {
  id: string;
  title: string;
  peerEmail?: string;
  subject: string;
  count: number;
  accountEmail?: string;
  accountLabel?: string;
  accountColor?: string;
  messages: ConversationMessage[];
};

export type ConversationListResponse = {
  conversations: ConversationSummary[];
  folders: Mailbox[];
  accounts: MailAccount[];
  accountEmail: string;
  accountErrors?: { accountEmail: string; message: string }[];
  locale: string;
  unified: boolean;
  unifiedAvailable: boolean;
};

export type ConversationDetailResponse = {
  conversation: ConversationDetail;
};

export type MailMessage = Omit<ConversationMessage, "outgoing"> & {
  accountEmail?: string;
};

export type MailAttachment = {
  id: string;
  partId: string;
  filename: string;
  contentType: string;
  size: number;
  folder: string;
  messageId: string;
  messageDate: string;
  messageFrom: string;
  fromName?: string;
  messageSubject: string;
  accountEmail: string;
};

export type MailAttachmentListResponse = {
  attachments: MailAttachment[];
  total: number;
  limit: number;
  offset: number;
  nextOffset?: number;
};

export type ConnectedAccount = {
  email: string;
  label: string;
  color?: string;
  imapServer: string;
  imapPort?: number;
  smtpServer?: string;
  smtpPort?: number;
};

export type CalendarEvent = {
  uid: string;
  summary: string;
  description?: string;
  location?: string;
  start: string;
  end: string;
  allDay: boolean;
  path?: string;
};
