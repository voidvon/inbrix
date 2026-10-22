export type Mailbox = {
  name: string;
  delimiter: string;
  attributes: string[];
  unreadCount?: number;
};

export type MailAccount = {
  id: string;
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

export type MailTranslation = {
  text: string;
  status: "ready" | "generating" | "failed";
  updatedAt?: string;
};

export type SuggestedReply = {
  text: string;
  status: "ready";
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
	status: "answered" | "unanswered" | "no_action" | "draft";
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
  mailTranslation?: MailTranslation;
  mailTranslationAll?: MailTranslation;
  suggestedReply?: SuggestedReply;
  sendRetryable?: boolean;
  sendStatus?: "sending" | "failed" | "unknown" | "sent";
  sendError?: string;
};

/** One tool the conversation agent ran while answering (e.g. a price lookup). */
export type ConversationAIToolCall = {
  id?: string;
  name: string;
  summary?: string;
  status?: "success" | "error";
};

/** A quotation/contract the conversation agent drafted, awaiting the user's save. */
export type ConversationAIDocumentDraft = {
  type: "quotation" | "contract";
  title: string;
  counterparty: string;
  contact?: string;
  currency?: string;
  items?: Array<{ productId?: string; productVersion?: number; picture?: string; model?: string; description?: string; qty?: string; price?: string; amount?: string }>;
  terms?: string[];
  notes?: string;
  total?: string;
  values?: Record<string, string>;
  html?: string;
};

export type ConversationAIDocumentAction = {
  kind: "update" | "delete";
  documentId: string;
  type?: "quotation" | "contract";
  name?: string;
  company?: string;
  values?: Record<string, string>;
  items?: Array<{ productId?: string; productVersion?: number; picture?: string; model?: string; description?: string; qty?: string; price?: string; amount?: string }>;
  summary?: string;
};

export type ConversationAIChatMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  /** Source references allow later turns to re-read mail without saving bodies in chat. */
  sources?: Array<{ uid: string; folder: string }>;
  createdAt: string;
  toolCalls?: ConversationAIToolCall[];
  /** Set when the agent drafted a document to save. */
  document?: ConversationAIDocumentDraft;
  /** Set when the agent proposed changing a saved document. */
  documentAction?: ConversationAIDocumentAction;
};

export type ConversationDetail = {
  id: string;
  title: string;
  peerEmail?: string;
  subject: string;
  note?: string;
  count: number;
  accountEmail?: string;
  accountLabel?: string;
  accountColor?: string;
  messages: ConversationMessage[];
  /** Whether messages older than this loaded window remain. */
  hasMore?: boolean;
  /** Opaque boundary for loading older messages or refreshing this window. */
  oldestCursor?: string;
  /** Whether the loaded window stops before the latest message. */
  hasNewer?: boolean;
  newestCursor?: string;
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
  /** Monotonic per-user change counter; echoed on reconnect. */
  revision: number;
  /** True for the compact "nothing changed" response. */
  unchanged?: boolean;
  /** Opaque cursor for the next page; absent on the last page. */
  nextCursor?: string;
  /** Whether another page exists after this one. */
  hasMore?: boolean;
  /** Number of conversations matching the query before paging. */
  total?: number;
  /** Mailbox-wide unread message count, not just the loaded rows. */
  unreadCount?: number;
};

export type ConversationDetailResponse = {
  /** Client-only navigation identity; retained by optimistic cache updates. */
  windowTarget?: string;
  conversation: ConversationDetail;
};

/** One matching message from the full-text mail search. */
export type MailSearchResult = {
  conversationId: string;
  messageId: string;
  folder: string;
  subject: string;
  from: string;
  fromName?: string;
  to: string;
  date: string;
  snippet: string;
  conversationTitle: string;
  peerEmail?: string;
  accountEmail?: string;
  accountLabel?: string;
  accountColor?: string;
  outgoing: boolean;
  matchedSubject: boolean;
  matchedBody: boolean;
};

export type MailSearchResponse = {
  query: string;
  /** Normalized terms the server matched, used for highlighting. */
  terms: string[];
  results: MailSearchResult[];
  hasMore: boolean;
};

/** A user-owned, spreadsheet-shaped price table. */
export type PriceList = {
  id: string;
  name: string;
  description: string;
  version: number;
  rowCount: number;
  columnCount: number;
  createdAt: string;
  updatedAt: string;
};

export type PriceColumnType = "text" | "number" | "date" | "select";

/**
 * Column roles are what keep a free-form table machine-readable: they tell the
 * quotation editor and the AI which column holds the model and the price.
 */
export type PriceColumnRole =
  | ""
  | "model"
  | "name"
  | "unit"
  | "price"
  | "currency"
  | "tax"
  | "moq"
  | "lead_time";

export type PriceColumn = {
  id?: string;
  key: string;
  label: string;
  type: PriceColumnType;
  role: PriceColumnRole;
  width: number;
  position?: number;
  options?: string[];
};

export type PriceRow = {
  id: string;
  position?: number;
  data: Record<string, string>;
  updatedAt?: string;
};

export type PriceListDetailResponse = {
  priceList: PriceList;
  columns: PriceColumn[];
};

export type PriceRowsResponse = {
  rows: PriceRow[];
  total: number;
  limit: number;
  offset: number;
};

export type ComposeDraftMode = "new" | "reply" | "reply_all";

/**
 * An unsent compose window stored on the server. It is kept there rather than in
 * browser storage so the same account can pick a half-written reply up from a
 * different computer.
 */
export type ComposeDraft = {
  id: string;
  accountEmail: string;
  conversationId: string;
  mode: ComposeDraftMode;
  inReplyTo: string;
  references: string;
  to: string;
  cc: string;
  subject: string;
  bodyHtml: string;
  bodyText: string;
  createdAt: string;
  updatedAt: string;
};

export type ComposeDraftInput = {
  id?: string;
  accountEmail?: string;
  conversationId?: string;
  mode?: ComposeDraftMode;
  inReplyTo?: string;
  references?: string;
  to?: string;
  cc?: string;
  subject?: string;
  bodyHtml?: string;
  bodyText?: string;
};

export type ComposeDraftListResponse = {
  drafts: ComposeDraft[];
};

export type ComposeDraftResponse = {
  draft: ComposeDraft;
};

export type PriceLookupEntry = {
  listId: string;
  listName: string;
  rowId: string;
  model: string;
  name: string;
  unit: string;
  currency: string;
  price: number;
  priceText: string;
  taxRate: number;
  moq?: string;
  leadTime?: string;
};

export type PriceImportResult = {
  headers: string[];
  added: number;
  updated: number;
  skipped: number;
  createdColumns?: PriceColumn[];
  preview?: string[][];
  errors?: string[];
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
  id: string;
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
