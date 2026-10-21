import { useEffect, useRef, useState, type FormEvent, type MutableRefObject } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  Code2,
  Copy as CopyIcon,
  FilePlus2,
  ImagePlus,
  Paperclip,
  Send,
  Signature as SignatureIcon,
  Sparkles,
  X,
} from "lucide-react";
import type { Editor } from "@tiptap/core";
import { EditorContent, useEditor } from "@tiptap/react";
import LinkExtension from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import StarterKit from "@tiptap/starter-kit";
import UnderlineExtension from "@tiptap/extension-underline";
import { toast, toastError } from "../../lib/toast";
import { EmailParagraph } from "../../extensions/email-paragraph";
import { EmailImage } from "../../extensions/email-image";
import { EmailSignature as EmailSignatureExtension } from "../../extensions/email-signature";
import { ReplyQuote } from "../../extensions/reply-quote";
import { RichTextButtons } from "./rich-text-buttons";
import { ReplyTemplates } from "./reply-templates";
import { ComposeDocumentAttachment } from "./document-editor-dialog";
import { ComposeBubble, type ComposeBubbleSize, type ComposeDraftStatus, type ComposeVariant } from "./compose-bubble";
import { usePersistedPaneHeight, viewportHeightLimit } from "./resize-handle";
import { deleteDraft, generateEmail, getSignatures, saveDraft, sendMessage } from "../../lib/api";
import { composeDraftKey, useComposeDraft } from "../../lib/compose-draft";
import { cn, formatSize } from "../../lib/utils";
import {
  MAX_COMPOSE_ATTACHMENT_BYTES,
  SUPPORTED_INLINE_IMAGE_TYPES,
  type InlineComposeImage,
  type ComposeDefaults,
  inlineImageDimensions,
  splitRecipientValues,
  uniqueRecipients,
  isValidRecipient,
  normalizeQuoteHTML,
  restoreInlineImagePreviews,
  serializeInlineImageReferences,
  serializeQuoteHTML,
  serializeComposeHTML,
  htmlToPlainText,
  setEditorSignature,
  aiConversationContext,
  replaceEditorDraft,
  draftBodyForStorage,
} from "../../lib/email-format";
import { type Copy, zh } from "../../lib/locale";
import { AIStatusIndicator } from "./ai-status-indicator";
import { useAIStateMachine } from "../../lib/ai-state-machine";
import type { ConversationMessage } from "../../types";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from "../ui/popover";
import { ScrollArea } from "../ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Separator } from "../ui/separator";
import { Textarea } from "../ui/textarea";

export function AIAssistantButton({
  copy,
  editor,
  disabled,
  composeOpen,
  accountEmail,
  subject,
  recipients,
  conversation,
}: {
  copy: Copy;
  editor: Editor | null;
  disabled: boolean;
  composeOpen: boolean;
  accountEmail: string;
  subject: string;
  recipients: string;
  conversation?: ConversationMessage[];
}) {
  const [open, setOpen] = useState(false);
  const [instruction, setInstruction] = useState("");
  const [includeConversation, setIncludeConversation] = useState(false);
  const [generatedBody, setGeneratedBody] = useState("");
  const [chatMessages, setChatMessages] = useState<Array<{ role: "user" | "assistant"; content: string }>>([]);
  const [copiedMessage, setCopiedMessage] = useState<number | null>(null);
  const chatRef = useRef<HTMLDivElement>(null);
  const messages = conversation || [];
  const hasConversation = messages.length > 0;
  const isZh = copy === zh;
  const aiState = useAIStateMachine({
    taskType: "reply",
    locale: isZh ? "zh" : "en",
  });

  const mutation = useMutation({
    mutationFn: ({ prompt, draft }: { prompt: string; draft?: string }) =>
      aiState.run(() =>
        generateEmail({
          accountEmail,
          instruction: prompt,
          subject,
          recipients,
          context: includeConversation ? aiConversationContext(messages) : undefined,
          draft,
        })
      ),
    onSuccess: ({ body }) => {
      setGeneratedBody(body);
      setChatMessages((current) => [...current, { role: "assistant", content: body }]);
    },
    onError: (value) => toastError(value, copy.aiGenerateFailed),
  });

  useEffect(() => {
    setIncludeConversation(false);
    setInstruction("");
    setGeneratedBody("");
    setChatMessages([]);
    setCopiedMessage(null);
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
    mutation.mutate({ prompt, draft: generatedBody || undefined });
  };

  const copyMessage = async (content: string, index: number) => {
    try {
      await navigator.clipboard.writeText(content);
      setCopiedMessage(index);
      window.setTimeout(() => setCopiedMessage((current) => (current === index ? null : current)), 1600);
    } catch {
      toast.error(copy.copyAIContentFailed);
    }
  };

  const useGeneratedBody = () => {
    if (!editor || !generatedBody) return;
    replaceEditorDraft(editor, generatedBody);
    setGeneratedBody("");
    setInstruction("");
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(value) => {
        setOpen(value);
      }}
    >
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant={open ? "secondary" : "ghost"}
            size="icon"
            disabled={disabled || !editor}
            aria-label={copy.aiWriteEmail}
            title={copy.aiWriteEmail}
          />
        }
      >
        <Sparkles />
      </PopoverTrigger>
      <PopoverContent side="bottom" align="start" sideOffset={8} className="w-[min(30rem,calc(100vw-2rem))] gap-0 p-4">
        <PopoverTitle className="text-sm font-semibold">{copy.aiWriteEmail}</PopoverTitle>
        <PopoverDescription className="mt-1 text-xs">{copy.aiWriteDescription}</PopoverDescription>
        {chatMessages.length > 0 && (
          <div ref={chatRef} className="mt-4 flex max-h-72 flex-col gap-3 overflow-y-auto pr-1" aria-live="polite">
            {chatMessages.map((message, index) =>
              message.role === "user" ? (
                <div key={index} className="group ml-10 grid justify-items-end self-end">
                  <div className="whitespace-pre-wrap rounded-lg border bg-muted/40 px-3 py-2 text-sm leading-5">{message.content}</div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="size-7 text-muted-foreground opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100 focus-visible:opacity-100"
                    onClick={() => void copyMessage(message.content, index)}
                    aria-label={copy.copyAIContent}
                    title={copiedMessage === index ? copy.copiedAIContent : copy.copyAIContent}
                  >
                    {copiedMessage === index ? <Check /> : <CopyIcon />}
                  </Button>
                </div>
              ) : (
                <div key={index} className="group mr-5 grid self-start">
                  <div className="whitespace-pre-wrap px-1 text-sm leading-6">{message.content}</div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="size-7 text-muted-foreground opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100 focus-visible:opacity-100"
                    onClick={() => void copyMessage(message.content, index)}
                    aria-label={copy.copyAIContent}
                    title={copiedMessage === index ? copy.copiedAIContent : copy.copyAIContent}
                  >
                    {copiedMessage === index ? <Check /> : <CopyIcon />}
                  </Button>
                </div>
              )
            )}
            {mutation.isPending && (
              <div className="mr-5 my-1">
                <AIStatusIndicator
                  snapshot={aiState.snapshot}
                  variant="card"
                  showTimer
                  showSteps
                />
              </div>
            )}
          </div>
        )}
        <Label className="mt-4 grid gap-1.5 text-xs" htmlFor="ai-compose-instruction">
          {copy.aiInstruction}
          <Textarea
            id="ai-compose-instruction"
            value={instruction}
            onChange={(event) => {
              setInstruction(event.target.value);
            }}
            placeholder={generatedBody ? copy.refineInstructionPlaceholder : copy.aiInstructionPlaceholder}
            rows={3}
            disabled={mutation.isPending}
            onKeyDown={(event) => {
              if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
                event.preventDefault();
                if (!mutation.isPending) submitInstruction();
              }
            }}
          />
        </Label>
        <div className="mt-4 flex items-center justify-between gap-3">
          <label className={cn("flex min-w-0 items-center gap-2 text-sm", !hasConversation && "text-muted-foreground")} title={!hasConversation ? copy.noConversationContext : undefined}>
            <input className="size-4 shrink-0 accent-primary" type="checkbox" checked={includeConversation} disabled={!hasConversation || mutation.isPending} onChange={(event) => setIncludeConversation(event.target.checked)} />
            <span className="truncate">{copy.includeConversation}</span>
          </label>
          <div className="flex shrink-0 gap-2">
            {generatedBody && (
              <Button type="button" variant="ghost" size="sm" disabled={mutation.isPending} onClick={useGeneratedBody}>
                <Check />
                {copy.useGeneratedDraft}
              </Button>
            )}
            <Button
              type="button"
              variant={generatedBody ? "outline" : "default"}
              size="sm"
              disabled={mutation.isPending || (!instruction.trim() && !subject.trim() && !includeConversation && !generatedBody)}
              onClick={submitInstruction}
            >
              <Send />
              {mutation.isPending ? copy.generating : generatedBody ? copy.regenerate : copy.generate}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function RecipientTagInput({
  copy,
  label,
  recipients,
  onChange,
  draftRef,
  autoFocus = false,
}: {
  copy: Copy;
  label: string;
  recipients: string[];
  onChange: (recipients: string[]) => void;
  draftRef: MutableRefObject<string>;
  autoFocus?: boolean;
}) {
  const [draft, setDraft] = useState("");
  const [invalid, setInvalid] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const addValues = (values: string[]) => {
    const candidates = values.map((value) => value.trim()).filter(Boolean);
    if (!candidates.length) return true;
    if (candidates.some((value) => !isValidRecipient(value))) {
      setInvalid(true);
      toast.error(copy.invalidRecipient);
      return false;
    }
    const existing = new Set(recipients.map((value) => value.toLowerCase()));
    onChange([
      ...recipients,
      ...candidates.filter((value) => {
        const key = value.toLowerCase();
        if (existing.has(key)) return false;
        existing.add(key);
        return true;
      }),
    ]);
    setDraft("");
    draftRef.current = "";
    setInvalid(false);
    return true;
  };

  return (
    <div>
      <div
        className={cn(
          "flex h-8 flex-nowrap items-center gap-1 overflow-x-auto rounded-lg border border-input bg-transparent px-2 py-0.5 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50",
          invalid && "border-destructive focus-within:border-destructive focus-within:ring-destructive/20"
        )}
        onClick={() => inputRef.current?.focus()}
      >
        {recipients.map((recipient, index) => (
          <span className="flex max-w-full items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs" key={`${recipient}-${index}`}>
            <span className="truncate" title={recipient}>
              {recipient}
            </span>
            <button
              type="button"
              className="grid size-4 shrink-0 place-items-center rounded-sm text-muted-foreground hover:bg-background hover:text-foreground"
              onClick={(event) => {
                event.stopPropagation();
                onChange(recipients.filter((_, itemIndex) => itemIndex !== index));
              }}
              aria-label={`${copy.removeRecipient}: ${recipient}`}
              title={copy.removeRecipient}
            >
              <X className="size-3" />
            </button>
          </span>
        ))}
        <input
          ref={inputRef}
          className="h-6 min-w-8 flex-1 bg-transparent px-0.5 text-sm outline-none placeholder:text-muted-foreground"
          value={draft}
          onChange={(event) => {
            setDraft(event.target.value);
            draftRef.current = event.target.value;
            if (invalid) setInvalid(false);
          }}
          onKeyDown={(event) => {
            if (["Enter", ",", ";", "Tab"].includes(event.key) && draft.trim()) {
              event.preventDefault();
              addValues(splitRecipientValues(draft));
            } else if (event.key === "Backspace" && !draft && recipients.length) {
              onChange(recipients.slice(0, -1));
            }
          }}
          onPaste={(event) => {
            const value = event.clipboardData.getData("text");
            if (!/[,;\n]/.test(value)) return;
            event.preventDefault();
            addValues(splitRecipientValues(value));
          }}
          onBlur={() => {
            if (draft.trim()) addValues(splitRecipientValues(draft));
          }}
          placeholder={recipients.length ? "" : "name@example.com"}
          autoFocus={autoFocus}
          inputMode="email"
          aria-invalid={invalid}
          aria-label={label}
        />
      </div>
    </div>
  );
}

/** How long typing must pause before the draft is pushed to the server. */
const DRAFT_AUTOSAVE_MS = 1200;

// The compose window's height is the user's to choose; these are only the
// starting point and the floor. The default is deliberately generous because the
// rich-text area is what suffers when it is not: every fixed row above it —
// header, recipients, subject, toolbar, footer — is taken out of this budget.
const COMPOSE_FLOATING_HEIGHT = 660;
const COMPOSE_INLINE_HEIGHT = 580;
const COMPOSE_MIN_HEIGHT = 300;

/**
 * Whether two Reply clicks describe the same compose window. The reply text is
 * excluded on purpose: writing an AI answer into a window that is already open
 * only changes the body, and reloading the window for it would throw away a
 * subject edit that has not reached the server yet.
 */
function sameComposeWindow(a: ComposeDefaults, b: ComposeDefaults) {
  return (
    a.conversationId === b.conversationId &&
    a.to === b.to &&
    (a.cc || "") === (b.cc || "") &&
    a.subject === b.subject &&
    (a.html || "") === (b.html || "") &&
    (a.inReplyTo || "") === (b.inReplyTo || "") &&
    (a.replacesOutboxId || "") === (b.replacesOutboxId || "") &&
    (a.references || []).join(" ") === (b.references || []).join(" ")
  );
}

/**
 * What "the draft has not changed" means: the exact values that would be
 * written. Comparing this against the last known-saved values is what keeps
 * merely opening a reply from creating a draft row, and keeps the flush on
 * close from re-sending content the server already has.
 */
function draftSignature(to: string, cc: string, subject: string, html: string) {
  return JSON.stringify([to.trim(), cc.trim(), subject.trim(), html]);
}

function formatDraftTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

export function ComposeDialog({
  copy,
  open,
  variant,
  defaults,
  accountEmail,
  onOpenChange,
}: {
  copy: Copy;
  open: boolean;
  /** A reply sits in the transcript it answers; a new message floats. */
  variant: ComposeVariant;
  defaults: ComposeDefaults;
  accountEmail: string;
  onOpenChange: (value: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [recipients, setRecipients] = useState<string[]>([]);
  const [ccRecipients, setCcRecipients] = useState<string[]>([]);
  const [subject, setSubject] = useState("");
  const [invalid, setInvalid] = useState<{ subject?: boolean; body?: boolean }>({});
  const [sourceMode, setSourceMode] = useState(false);
  const [sourceCode, setSourceCode] = useState("");
  const [attachments, setAttachments] = useState<File[]>([]);
  const attachmentsRef = useRef<File[]>([]);
  attachmentsRef.current = attachments;
  const [documentBusy, setDocumentBusy] = useState(false);
  const [documentAction, setDocumentAction] = useState<"create" | "import" | null>(null);
  const [inlineImages, setInlineImages] = useState<InlineComposeImage[]>([]);
  const inlineImagesRef = useRef<InlineComposeImage[]>([]);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const inlineImageInputRef = useRef<HTMLInputElement>(null);
  const ccDraftRef = useRef("");
  const signatureInitializedRef = useRef(false);
  const [selectedSignatureId, setSelectedSignatureId] = useState("none");

  // --- server-side draft state -------------------------------------------
  const [bubbleSize, setBubbleSize] = useState<ComposeBubbleSize>("normal");
  // The window's height, remembered per variant: the inline reply sits in a
  // transcript and the floating one in the corner, and neither wants the other's
  // size. The editor takes whatever is left after the fixed rows.
  const [composeHeight, setComposeHeight] = usePersistedPaneHeight(
    variant === "inline" ? "inbrix-compose-inline-height" : "inbrix-compose-height",
    variant === "inline" ? COMPOSE_INLINE_HEIGHT : COMPOSE_FLOATING_HEIGHT,
    COMPOSE_MIN_HEIGHT,
    viewportHeightLimit(),
  );
  const [draftStatus, setDraftStatus] = useState<ComposeDraftStatus>("idle");
  const [draftSavedAt, setDraftSavedAt] = useState("");
  const [hasDraft, setHasDraft] = useState(false);
  // The handle the server gave us; empty until the first successful save.
  const draftIdRef = useRef("");
  // The compose window the editor currently holds. `defaults` is a fresh
  // object per Reply click, so its identity tells us two things at once: that
  // the editor is already filled for this window (which stops a background
  // refetch from overwriting typing), and which conversation a pending save
  // belongs to.
  const activeWindowRef = useRef<{ defaults: ComposeDefaults; conversationId: string } | null>(null);
  const lastSavedRef = useRef("");
  const autosaveTimerRef = useRef<number | null>(null);
  // Reassigned every render so a debounced timer or the flush-on-close always
  // saves the CURRENT state, not the state captured when the timer was set.
  const saveDraftRef = useRef<() => void>(() => {});
  const savingRef = useRef(false);
  const pendingSaveRef = useRef(false);
  // Set when the window is thrown away while a save is in flight, so the row that
  // save writes cannot outlive the discard.
  const discardedRef = useRef(false);
  // A draft fetch that never settles must not leave an unusable compose window.
  const [draftWaitExpired, setDraftWaitExpired] = useState(false);

  const cancelAutosave = () => {
    if (autosaveTimerRef.current !== null) {
      window.clearTimeout(autosaveTimerRef.current);
      autosaveTimerRef.current = null;
    }
  };

  // Armed on every keystroke and every field edit. Only a timer is set here:
  // serializing the editor and issuing the request happen once typing pauses,
  // so a fast typist costs one request instead of one per character.
  const scheduleDraftSave = () => {
    cancelAutosave();
    autosaveTimerRef.current = window.setTimeout(() => {
      autosaveTimerRef.current = null;
      saveDraftRef.current();
    }, DRAFT_AUTOSAVE_MS);
  };

  const conversationId = defaults.conversationId || "";
  // Always fresh: a draft written on another computer must win over a warm local
  // cache, so the editor re-reads the server before it is filled.
  const draftQuery = useComposeDraft(accountEmail, conversationId, { enabled: open, alwaysFresh: true });
  const storedDraft = draftQuery.data ?? null;
  const draftLoaded = draftWaitExpired || (draftQuery.isFetched && !draftQuery.isFetching);

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ blockquote: false, paragraph: false }),
      EmailParagraph,
      ReplyQuote,
      EmailSignatureExtension,
      EmailImage,
      UnderlineExtension,
      LinkExtension.configure({ openOnClick: false }),
      Placeholder.configure({ placeholder: copy.writeMessage }),
    ],
    content: "",
    immediatelyRender: false,
    onUpdate: () => {
      setInvalid((current) => current.body ? { ...current, body: false } : current);
      scheduleDraftSave();
    },
  });
  const signatures = useQuery({ queryKey: ["signatures", accountEmail], queryFn: getSignatures, enabled: open, retry: false });
  const mutation = useMutation({ mutationFn: sendMessage });
  const requestIdRef = useRef("");
  const draftHeadersRef = useRef<{ inReplyTo?: string; references?: string[] }>({});
  useEffect(() => { if (open) requestIdRef.current = ""; }, [open]);

  const persistDraft = (target: { defaults: ComposeDefaults; conversationId: string }, payload: { to: string; cc: string; subject: string; html: string; signature: string }) => {
    // One write at a time. A second change that arrives while a save is in
    // flight is remembered and sent once that save settles, so an older body can
    // never land on top of a newer one.
    if (savingRef.current) {
      pendingSaveRef.current = true;
      return;
    }
    savingRef.current = true;
    setDraftStatus("saving");
    // Deliberately a plain promise rather than a useMutation: closing the editor
    // unmounts this component, and React Query stops delivering a mutation
    // observer's callbacks once its component is gone. Closing keeps the draft,
    // so this bookkeeping — above all telling the transcript to re-read — has to
    // run anyway.
    void saveDraft({
      id: draftIdRef.current || undefined,
      accountEmail,
      conversationId: target.conversationId || undefined,
      mode: !draftHeadersRef.current.inReplyTo ? "new" : payload.cc ? "reply_all" : "reply",
      inReplyTo: draftHeadersRef.current.inReplyTo,
      references: draftHeadersRef.current.references?.join(" ") || undefined,
      to: payload.to,
      cc: payload.cc,
      subject: payload.subject,
      bodyHtml: payload.html,
      bodyText: htmlToPlainText(payload.html),
    })
      .then((data) => {
        if (discardedRef.current) {
          // The window was discarded while this save was in flight. Put the row
          // back out of reach rather than letting it outlive the discard.
          void deleteDraft(data.draft.id).catch(() => undefined);
          return;
        }
        // The transcript's draft entry mirrors what the server now holds, so
        // this runs even when the window has already been closed — closing
        // keeps the draft, and the reader still has to see it.
        void queryClient.invalidateQueries({ queryKey: composeDraftKey(accountEmail, conversationId) });
        void queryClient.invalidateQueries({ queryKey: ["conversations"] });
        // The user may have opened another conversation while this save was in
        // flight; the window's own state belongs to the window that issued it.
        if (activeWindowRef.current !== target) return;
        draftIdRef.current = data.draft.id;
        lastSavedRef.current = payload.signature;
        setHasDraft(true);
        setDraftStatus("saved");
        setDraftSavedAt(formatDraftTime(data.draft.updatedAt));
      })
      .catch(() => {
        if (activeWindowRef.current === target) setDraftStatus("error");
      })
      .finally(() => {
        savingRef.current = false;
        if (pendingSaveRef.current) {
          pendingSaveRef.current = false;
          saveDraftRef.current();
        }
      });
  };

  // Reassigned on every render, so whoever calls it — the debounce timer or the
  // flush on close — reads the state as it is now.
  saveDraftRef.current = () => {
    const target = activeWindowRef.current;
    if (!open || !editor || !target) return;
    const to = recipients.join(", ");
    const cc = ccRecipients.join(", ");
    const trimmedSubject = subject.trim();
    const html = draftBodyForStorage(sourceMode ? sourceCode : editor.getHTML());
    const signature = draftSignature(to, cc, trimmedSubject, html);
    if (signature === lastSavedRef.current) return;
    // An empty window is not worth a row: opening Reply and closing it again
    // must not leave a draft behind.
    if (!to && !cc && !trimmedSubject && !htmlToPlainText(html).trim()) return;
    persistDraft(target, { to, cc, subject: trimmedSubject, html, signature });
  };

  const flushDraft = () => {
    cancelAutosave();
    saveDraftRef.current();
  };

  // A window can also be unmounted rather than closed — the reply moves into
  // another conversation's transcript, or the mailbox goes away entirely — and
  // anything typed inside the debounce window still has to reach the server.
  useEffect(() => () => saveDraftRef.current(), []);

  const forgetDraft = async () => {
    cancelAutosave();
    discardedRef.current = true;
    const id = draftIdRef.current;
    draftIdRef.current = "";
    lastSavedRef.current = "";
    savingRef.current = false;
    pendingSaveRef.current = false;
    setHasDraft(false);
    if (id) {
      try {
        await deleteDraft(id);
      } catch {
        // The draft is already unreachable from this window; a failed delete
        // must not block closing it.
      }
    }
    void queryClient.invalidateQueries({ queryKey: composeDraftKey(accountEmail, conversationId) });
    void queryClient.invalidateQueries({ queryKey: ["conversations"] });
    void queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] });
  };

  const handleClose = () => {
    // Closing keeps the draft — that is the whole point of storing it on the
    // server — but anything typed inside the debounce window is flushed first.
    flushDraft();
    activeWindowRef.current = null;
    setDraftStatus("idle");
    setDraftSavedAt("");
    onOpenChange(false);
  };

  const discardDraft = () => {
    if (!window.confirm(copy.draftDiscardConfirm)) return;
    activeWindowRef.current = null;
    void forgetDraft();
    setDraftStatus("idle");
    setDraftSavedAt("");
    onOpenChange(false);
  };

  const replaceInlineImages = (images: InlineComposeImage[]) => {
    inlineImagesRef.current = images;
    setInlineImages(images);
  };

  const clearInlineImages = () => {
    inlineImagesRef.current.forEach((image) => URL.revokeObjectURL(image.previewURL));
    replaceInlineImages([]);
  };

  useEffect(
    () => () => {
      inlineImagesRef.current.forEach((image) => URL.revokeObjectURL(image.previewURL));
    },
    []
  );

  useEffect(() => {
    if (!open && inlineImagesRef.current.length) clearInlineImages();
  }, [open]);

  // Bound how long the editor waits for the stored draft. A request that never
  // settles must degrade to today's behaviour (a blank reply template) rather
  // than an unusable compose window.
  useEffect(() => {
    if (!open || !conversationId) return;
    setDraftWaitExpired(false);
    const timer = window.setTimeout(() => setDraftWaitExpired(true), 3000);
    return () => window.clearTimeout(timer);
  }, [open, conversationId, defaults]);

  // Fill the editor once the stored draft is known. A draft written on another
  // computer replaces the blank reply template; without one the template stands.
  //
  // `activeWindowRef` pins this to one compose window: `defaults` is a fresh
  // object per Reply click, so a background refetch can never re-fill the editor
  // and wipe out what the user is typing.
  useEffect(() => {
    if (!open || !editor) return;
    const previous = activeWindowRef.current;
    if (previous?.defaults === defaults) return;
    // The same window, with a new reply text: apply it in place instead of
    // reloading the draft, so the subject and whatever has been typed survive.
    if (previous && sameComposeWindow(previous.defaults, defaults) && defaults.replaceBody?.trim()) {
      activeWindowRef.current = { defaults, conversationId };
      replaceEditorDraft(editor, defaults.replaceBody);
      return;
    }
    if (!draftLoaded) return;
    // The bubble is not modal, so the user can click Reply on another
    // conversation while this one is still open. Whatever was typed into the
    // window being replaced is saved under ITS conversation first.
    if (previous) saveDraftRef.current();
    const stored = storedDraft;
    draftHeadersRef.current = stored ? {
      inReplyTo: stored.inReplyTo || undefined,
      references: stored.references.trim().split(/\s+/).filter(Boolean),
    } : { inReplyTo: defaults.inReplyTo, references: defaults.references };
    const to = stored?.to.trim() || defaults.to;
    const cc = stored ? stored.cc : defaults.cc || "";
    const subjectValue = stored ? stored.subject : defaults.subject;
    const html = normalizeQuoteHTML(stored ? stored.bodyHtml : defaults.html || "");
    cancelAutosave();
    discardedRef.current = false;
    setRecipients(splitRecipientValues(to));
    setCcRecipients(splitRecipientValues(cc));
    setSubject(subjectValue);
    setInvalid({});
    setSourceMode(false);
    setSourceCode(html);
    setSelectedSignatureId("none");
    signatureInitializedRef.current = false;
    setAttachments([]);
    setDocumentAction(null);
    setDocumentBusy(false);
    clearInlineImages();
    ccDraftRef.current = "";
    if (attachmentInputRef.current) attachmentInputRef.current.value = "";
    editor.commands.setContent(html, { emitUpdate: false });
    editor.commands.focus("start");
    draftIdRef.current = stored?.id || "";
    setHasDraft(Boolean(stored));
    setDraftStatus(stored ? "saved" : "idle");
    setDraftSavedAt(stored ? formatDraftTime(stored.updatedAt) : "");
    // Baseline taken from the editor itself, not from the input HTML: the editor
    // normalizes what it is given, and a baseline that does not match what the
    // next save would serialize would write a draft the moment a reply opens.
    lastSavedRef.current = draftSignature(to, cc, subjectValue, draftBodyForStorage(editor.getHTML()));
    activeWindowRef.current = { defaults, conversationId };
    // Applied AFTER the baseline is taken, so it counts as an edit and the draft
    // is written out on the next autosave — that is what makes "write this
    // answer into my reply" also work when there was no draft to begin with.
    if (defaults.replaceBody?.trim()) replaceEditorDraft(editor, defaults.replaceBody);
  }, [open, defaults, editor, draftLoaded, storedDraft, conversationId]);

  // Field edits are not editor updates, so they arm the autosave separately.
  useEffect(() => {
    if (!open) return;
    scheduleDraftSave();
  }, [open, defaults, recipients, ccRecipients, subject, sourceMode, sourceCode]);

  const insertInlineImages = async (files: File[], position?: number) => {
    if (!editor || !files.length) return;
    if (files.some((file) => !SUPPORTED_INLINE_IMAGE_TYPES.has(file.type) || file.size <= 0)) {
      toast.error(copy.unsupportedImage);
      return;
    }
    const totalSize = [...attachments, ...inlineImages.map((image) => image.file), ...files].reduce((total, file) => total + file.size, 0);
    if (totalSize > MAX_COMPOSE_ATTACHMENT_BYTES) {
      toast.error(copy.attachmentsTooLarge);
      return;
    }

    const previews = files.map((file) => {
      const contentId = `${crypto.randomUUID().replaceAll("-", "")}@inbrix`;
      return { contentId, file, previewURL: URL.createObjectURL(file) };
    });
    let additions: Array<InlineComposeImage & { width: number; height: number }>;
    try {
      additions = await Promise.all(previews.map(async (image) => ({ ...image, ...(await inlineImageDimensions(image.previewURL)) })));
    } catch {
      previews.forEach((image) => URL.revokeObjectURL(image.previewURL));
      toast.error(copy.unsupportedImage);
      return;
    }
    replaceInlineImages([...inlineImages, ...additions]);
    additions.forEach((image, index) => {
      const chain = editor.chain();
      if (index === 0 && position !== undefined) chain.focus(position);
      else chain.focus();
      chain
        .insertContent({
          type: "image",
          attrs: { src: image.previewURL, alt: image.file.name, title: image.file.name, width: image.width, height: image.height, inlineImageId: image.contentId },
        })
        .run();
    });
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
    if (documentBusy || documentAction || mutation.isPending) return;
    const pendingCcRecipients = splitRecipientValues(ccDraftRef.current);
    const submittedRecipients = recipients;
    const submittedCcRecipients = uniqueRecipients([...ccRecipients, ...pendingCcRecipients.filter((value) => isValidRecipient(value))], submittedRecipients);
    const htmlBody = serializeComposeHTML(sourceMode ? sourceCode : editor?.getHTML() || "");
    const plainBody = htmlToPlainText(htmlBody);
    const referencedContentIds = new Set(
      Array.from(new DOMParser().parseFromString(htmlBody, "text/html").querySelectorAll<HTMLImageElement>('img[src^="cid:"]'))
        .map((image) => image.getAttribute("src")?.slice(4) || "")
        .filter(Boolean)
    );
    const submittedInlineImages = inlineImages.filter((image) => referencedContentIds.has(image.contentId));
    if (!submittedRecipients.length) {
      toast.error(copy.recipientRequired);
      return;
    }
    if ([...submittedRecipients, ...pendingCcRecipients].some((value) => !isValidRecipient(value))) {
      toast.error(copy.invalidRecipient);
      return;
    }
    const nextInvalid: { subject?: boolean; body?: boolean } = {};
    if (!subject.trim()) nextInvalid.subject = true;
    if (!plainBody) nextInvalid.body = true;
    if (nextInvalid.subject || nextInvalid.body) {
      setInvalid(nextInvalid);
      toast.error(nextInvalid.subject && nextInvalid.body ? copy.requiredFieldsMissing : nextInvalid.subject ? copy.subjectRequired : copy.noBody);
      return;
    }
    if (Array.from(referencedContentIds).some((contentId) => !submittedInlineImages.some((image) => image.contentId === contentId))) {
      toast.error(copy.inlineImageMissing);
      return;
    }
    setInvalid({});
    const form = new FormData();
    form.set("to", submittedRecipients.join(", "));
    if (submittedCcRecipients.length) form.set("cc", submittedCcRecipients.join(", "));
    form.set("subject", subject.trim());
    form.set("body", plainBody);
    form.set("html_body", htmlBody);
    if (draftHeadersRef.current.inReplyTo) form.set("in_reply_to", draftHeadersRef.current.inReplyTo);
    if (draftHeadersRef.current.references?.length) form.set("references", draftHeadersRef.current.references.join(" "));
    if (defaults.conversationId) form.set("conversation_id", defaults.conversationId);
    if (accountEmail) form.set("account_email", accountEmail);
    attachments.forEach((file) => form.append("attachments", file, file.name));
    const inlineManifest = submittedInlineImages.map((image, index) => {
      const field = `inline_image_${index}`;
      form.append(field, image.file, image.file.name);
      return { field, contentId: image.contentId };
    });
    if (inlineManifest.length) form.set("inline_attachments", JSON.stringify(inlineManifest));

    form.set("request_id", requestIdRef.current ||= crypto.randomUUID());
    if (defaults.replacesOutboxId) form.set("replaces_outbox_id", defaults.replacesOutboxId);

    mutation.mutate(form, {
      onSuccess: () => {
        // The reply is queued, so its draft is spent. The server discards it too
        // (a tab that dies mid-send must not leave one behind); this is the
        // client's own half of that promise, and covers a new message, which the
        // server cannot find by conversation.
        activeWindowRef.current = null;
        void forgetDraft();
        setDraftStatus("idle");
        setDraftSavedAt("");
        onOpenChange(false);
        void queryClient.invalidateQueries({ queryKey: ["mailbox-shell"] });
        void queryClient.invalidateQueries({ queryKey: ["conversations"] });
        void queryClient.invalidateQueries({ queryKey: ["conversation"] });
        void queryClient.invalidateQueries({ queryKey: ["folder"] });
        void queryClient.invalidateQueries({ queryKey: ["attachments"] });
      },
      onError: (value) => toastError(value, copy.sendFailed),
    });
  };

  if (!open) return null;

  return (
    <ComposeBubble
      copy={copy}
      variant={variant}
      title={subject.trim() || copy.writeMessage}
      size={bubbleSize}
      height={composeHeight}
      minHeight={COMPOSE_MIN_HEIGHT}
      maxHeight={viewportHeightLimit()}
      onHeightChange={setComposeHeight}
      draftStatus={draftStatus}
      draftSavedAt={draftSavedAt}
      canDiscard={hasDraft}
      onSizeChange={setBubbleSize}
      onClose={handleClose}
      onDiscard={discardDraft}
    >
      <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
        <div className="grid gap-1.5 border-b px-3 py-2">
          <Label className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-2 text-xs text-muted-foreground">
            <span className="whitespace-nowrap">{copy.cc}</span>
            <RecipientTagInput copy={copy} label={copy.cc} recipients={ccRecipients} onChange={setCcRecipients} draftRef={ccDraftRef} />
          </Label>
          <Label className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-2 text-xs text-muted-foreground" htmlFor="compose-subject">
            <span className="whitespace-nowrap">{copy.subject}</span>
            <Input id="compose-subject" className="h-8" value={subject} aria-invalid={invalid.subject} onChange={(event) => { setSubject(event.target.value); if (invalid.subject) setInvalid((current) => ({ ...current, subject: false })); }} placeholder={copy.noSubject} />
          </Label>
        </div>
        <div className="flex shrink-0 items-center gap-1 overflow-x-auto border-b bg-muted px-3 py-0.5">
          <RichTextButtons editor={editor} disabled={sourceMode} />
          <Button type="button" variant="ghost" size="icon" disabled={sourceMode || !editor} onClick={() => inlineImageInputRef.current?.click()} aria-label={copy.insertImage} title={copy.insertImage}>
            <ImagePlus />
          </Button>
          <Separator orientation="vertical" className="mx-1 h-5" />
          <AIAssistantButton
            copy={copy}
            editor={editor}
            disabled={sourceMode}
            composeOpen={open}
            accountEmail={accountEmail}
            subject={subject}
            recipients={[...recipients, ...ccRecipients].join(", ")}
            conversation={defaults.conversation}
          />
          {open && <ReplyTemplates editor={editor} chinese={copy === zh} disabled={sourceMode || mutation.isPending || documentBusy} accountEmail={accountEmail} />}
          <Separator orientation="vertical" className="mx-1 h-5" />
          <Button type="button" variant="ghost" size="sm" disabled={mutation.isPending || documentBusy} onClick={() => setDocumentAction("create")}>
            <FilePlus2 />
            {copy === zh ? "新建报价单" : "New quotation"}
          </Button>
          <Button type="button" variant="ghost" size="sm" disabled={mutation.isPending || documentBusy} onClick={() => setDocumentAction("import")}>
            <Paperclip />
            {copy === zh ? "引入附件" : "Import attachment"}
          </Button>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <Select
              value={selectedSignatureId}
              disabled={sourceMode || signatures.isPending}
              onValueChange={(value) => {
                if (!value) return;
                setSelectedSignatureId(value);
                if (editor) setEditorSignature(editor, signatures.data?.signatures.find((item) => item.id === value) || null);
              }}
            >
              <SelectTrigger className="h-8 w-44" aria-label={copy.signatureSettings} title={copy.signatureSettings}>
                <SignatureIcon />
                <SelectValue>{selectedSignatureId === "none" ? copy.noSignature : signatures.data?.signatures.find((item) => item.id === selectedSignatureId)?.name || copy.noSignature}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">{copy.noSignature}</SelectItem>
                {signatures.data?.signatures.map((signature) => (
                  <SelectItem key={signature.id} value={signature.id}>
                    {signature.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button type="button" variant={sourceMode ? "secondary" : "ghost"} size="sm" onClick={toggleSourceMode} aria-label={sourceMode ? copy.richText : copy.sourceCode} title={sourceMode ? copy.richText : copy.sourceCode}>
              <Code2 />
              {sourceMode ? copy.richText : copy.sourceCode}
            </Button>
          </div>
        </div>
        {sourceMode ? (
          <textarea
            className={cn("min-h-0 flex-1 resize-none bg-background px-4 py-3 font-mono text-sm leading-6 outline-none", invalid.body && "ring-3 ring-destructive/20")}
            value={sourceCode}
            onChange={(event) => { setSourceCode(event.target.value); if (invalid.body) setInvalid((current) => ({ ...current, body: false })); }}
            spellCheck={false}
            aria-label={copy.sourceCode}
            aria-invalid={invalid.body}
          />
        ) : (
          <ScrollArea data-testid="compose-editor-area" className={cn("min-h-0 flex-1", invalid.body && "ring-3 ring-destructive/20")} contentClassName="px-4 py-3">
            <EditorContent
              editor={editor}
              onPaste={(event) => {
                const images = Array.from(event.clipboardData.files).filter((file) => file.type.startsWith("image/"));
                if (!images.length) return;
                event.preventDefault();
                void insertInlineImages(images);
              }}
              onDrop={(event) => {
                const images = Array.from(event.dataTransfer.files).filter((file) => file.type.startsWith("image/"));
                if (!images.length) return;
                event.preventDefault();
                const position = editor?.view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos;
                void insertInlineImages(images, position);
              }}
            />
          </ScrollArea>
        )}
        {attachments.length > 0 && (
          <div className="flex shrink-0 flex-wrap gap-2 border-t px-3 py-1.5">
            {attachments.map((file, index) => (
              <span className="flex max-w-64 items-center gap-1.5 rounded-md bg-muted px-2 py-1 text-xs" key={`${file.name}-${file.size}-${file.lastModified}-${index}`}>
                <Paperclip className="size-3.5 shrink-0" />
                <span className="truncate" title={file.name}>
                  {file.name}
                </span>
                <span className="shrink-0 text-muted-foreground">{formatSize(file.size)}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-5"
                  onClick={() => setAttachments((current) => current.filter((_, itemIndex) => itemIndex !== index))}
                  aria-label={`${copy.removeAttachment}: ${file.name}`}
                  title={copy.removeAttachment}
                >
                  <X className="size-3" />
                </Button>
              </span>
            ))}
          </div>
        )}
        <div className="flex shrink-0 flex-row items-center justify-between gap-2 border-t px-3 py-2">
          <input
            ref={attachmentInputRef}
            className="sr-only"
            type="file"
            multiple
            onChange={(event) => {
              const selected = Array.from(event.target.files || []);
              const next = [...attachmentsRef.current, ...selected];
              if ([...next, ...inlineImagesRef.current.map((image) => image.file)].reduce((total, file) => total + file.size, 0) > MAX_COMPOSE_ATTACHMENT_BYTES) {
                toast.error(copy.attachmentsTooLarge);
              } else {
                setAttachments(next);
              }
              event.target.value = "";
            }}
          />
          <input
            ref={inlineImageInputRef}
            className="sr-only"
            type="file"
            accept="image/jpeg,image/png,image/gif"
            multiple
            onChange={(event) => {
              void insertInlineImages(Array.from(event.target.files || []));
              event.target.value = "";
            }}
          />
          <Button type="button" variant="ghost" size="sm" onClick={() => attachmentInputRef.current?.click()}>
            <Paperclip />
            {copy.attach}
          </Button>
          <div className="flex gap-2">
            <Button type="button" variant="ghost" onClick={handleClose}>
              {copy.composeCloseKeep}
            </Button>
            <Button type="submit" disabled={mutation.isPending || documentBusy || documentAction !== null}>
              <Send />
              {mutation.isPending ? copy.sending : copy.send}
            </Button>
          </div>
        </div>
      </form>
      {open && documentAction && (
        <ComposeDocumentAttachment
          copy={copy}
          accountEmail={accountEmail}
          action={documentAction}
          onClose={() => {
            setDocumentAction(null);
            setDocumentBusy(false);
          }}
          onBusyChange={setDocumentBusy}
          onAttach={(file) => {
            const files = [...attachmentsRef.current, file];
            if ([...files, ...inlineImagesRef.current.map((image) => image.file)].reduce((total, item) => total + item.size, 0) > MAX_COMPOSE_ATTACHMENT_BYTES) throw new Error(copy.attachmentsTooLarge);
            attachmentsRef.current = files;
            setAttachments(files);
          }}
        />
      )}
    </ComposeBubble>
  );
}
