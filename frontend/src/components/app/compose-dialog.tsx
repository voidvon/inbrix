import { useEffect, useRef, useState, type FormEvent, type MutableRefObject } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
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
import { toast } from "sonner";
import { EmailParagraph } from "../../extensions/email-paragraph";
import { EmailImage } from "../../extensions/email-image";
import { EmailSignature as EmailSignatureExtension } from "../../extensions/email-signature";
import { ReplyQuote } from "../../extensions/reply-quote";
import { RichTextButtons } from "./rich-text-buttons";
import { ReplyTemplates } from "./reply-templates";
import { ComposeDocumentAttachment } from "./document-editor-dialog";
import { generateEmail, getSignatures, sendMessage } from "../../lib/api";
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
} from "../../lib/email-format";
import { type Copy, zh } from "../../lib/locale";
import type { ConversationMessage } from "../../types";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
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
  const [error, setError] = useState("");
  const chatRef = useRef<HTMLDivElement>(null);
  const messages = conversation || [];
  const hasConversation = messages.length > 0;
  const mutation = useMutation({
    mutationFn: ({ prompt, draft }: { prompt: string; draft?: string }) =>
      generateEmail({
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
    setError("");
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(value) => {
        setOpen(value);
        if (!value) setError("");
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
                    className="size-7 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
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
                    className="size-7 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
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
              <div className="mr-5 flex w-fit items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
                <Sparkles className="size-4 animate-pulse" />
                {copy.generating}
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
              setError("");
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
        {error && <p className="mt-3 text-xs text-destructive">{error}</p>}
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
          "flex min-h-8 flex-wrap items-center gap-1.5 rounded-lg border border-input bg-transparent px-2 py-1 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50",
          invalid && "border-destructive focus-within:border-destructive focus-within:ring-destructive/20"
        )}
        onClick={() => inputRef.current?.focus()}
      >
        {recipients.map((recipient, index) => (
          <span className="flex max-w-full items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-sm" key={`${recipient}-${index}`}>
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
          className="h-6 min-w-32 flex-1 bg-transparent px-0.5 text-sm outline-none placeholder:text-muted-foreground"
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
      {invalid && <p className="mt-1 text-xs text-destructive">{copy.invalidRecipient}</p>}
    </div>
  );
}

export function ComposeDialog({
  copy,
  open,
  defaults,
  accountEmail,
  onOpenChange,
  onSent,
}: {
  copy: Copy;
  open: boolean;
  defaults: ComposeDefaults;
  accountEmail: string;
  onOpenChange: (value: boolean) => void;
  onSent: () => void;
}) {
  const [recipients, setRecipients] = useState<string[]>([]);
  const [ccRecipients, setCcRecipients] = useState<string[]>([]);
  const [subject, setSubject] = useState("");
  const [error, setError] = useState("");
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
  const recipientDraftRef = useRef("");
  const ccDraftRef = useRef("");
  const signatureInitializedRef = useRef(false);
  const [selectedSignatureId, setSelectedSignatureId] = useState("none");
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
  });
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

  useEffect(
    () => () => {
      inlineImagesRef.current.forEach((image) => URL.revokeObjectURL(image.previewURL));
    },
    []
  );

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
    setDocumentAction(null);
    setDocumentBusy(false);
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
      additions = await Promise.all(previews.map(async (image) => ({ ...image, ...(await inlineImageDimensions(image.previewURL)) })));
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
      chain
        .insertContent({
          type: "image",
          attrs: { src: image.previewURL, alt: image.file.name, title: image.file.name, width: image.width, height: image.height, inlineImageId: image.contentId },
        })
        .run();
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
    if (documentBusy || documentAction || mutation.isPending) return;
    const pendingRecipients = splitRecipientValues(recipientDraftRef.current);
    const pendingCcRecipients = splitRecipientValues(ccDraftRef.current);
    const submittedRecipients = [...recipients, ...pendingRecipients.filter((value) => isValidRecipient(value))];
    const submittedCcRecipients = uniqueRecipients([...ccRecipients, ...pendingCcRecipients.filter((value) => isValidRecipient(value))], submittedRecipients);
    const htmlBody = serializeComposeHTML(sourceMode ? sourceCode : editor?.getHTML() || "");
    const plainBody = htmlToPlainText(htmlBody);
    const referencedContentIds = new Set(
      Array.from(new DOMParser().parseFromString(htmlBody, "text/html").querySelectorAll<HTMLImageElement>('img[src^="cid:"]'))
        .map((image) => image.getAttribute("src")?.slice(4) || "")
        .filter(Boolean)
    );
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
    if (defaults.conversationId) form.set("conversation_id", defaults.conversationId);
    if (accountEmail) form.set("account_email", accountEmail);
    attachments.forEach((file) => form.append("attachments", file, file.name));
    const inlineManifest = submittedInlineImages.map((image, index) => {
      const field = `inline_image_${index}`;
      form.append(field, image.file, image.file.name);
      return { field, contentId: image.contentId };
    });
    if (inlineManifest.length) form.set("inline_attachments", JSON.stringify(inlineManifest));
    mutation.mutate(form, {
      onSuccess: () => {
        onSent();
        onOpenChange(false);
      },
      onError: (value) => setError(value instanceof Error ? value.message : copy.loginFailed),
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="compose-dialog" className="flex h-[80vh] w-[80vw] max-w-[1200px] flex-col gap-0 overflow-hidden p-0 sm:max-w-[1200px]">
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit}>
          <DialogHeader className="border-b px-5 py-4 pr-12 text-left">
            <DialogTitle className="truncate text-base">{subject || copy.writeMessage}</DialogTitle>
            <DialogDescription className="sr-only">{copy.compose}</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3 border-b px-5 py-4">
            <Label className="grid gap-1.5 text-xs text-muted-foreground">
              {copy.to}
              <RecipientTagInput copy={copy} label={copy.to} recipients={recipients} onChange={setRecipients} draftRef={recipientDraftRef} autoFocus />
            </Label>
            <Label className="grid gap-1.5 text-xs text-muted-foreground">
              {copy.cc}
              <RecipientTagInput copy={copy} label={copy.cc} recipients={ccRecipients} onChange={setCcRecipients} draftRef={ccDraftRef} />
            </Label>
            <Label className="grid gap-1.5 text-xs text-muted-foreground" htmlFor="compose-subject">
              {copy.subject}
              <Input id="compose-subject" value={subject} onChange={(event) => setSubject(event.target.value)} placeholder={copy.noSubject} />
            </Label>
          </div>
          <div className="flex items-center gap-1 overflow-x-auto border-b bg-muted px-4 py-1">
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
            {open && <ReplyTemplates editor={editor} chinese={copy === zh} disabled={sourceMode || mutation.isPending || documentBusy} />}
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
              className="min-h-0 flex-1 resize-none bg-background px-5 py-4 font-mono text-sm leading-6 outline-none"
              value={sourceCode}
              onChange={(event) => setSourceCode(event.target.value)}
              spellCheck={false}
              aria-label={copy.sourceCode}
            />
          ) : (
            <ScrollArea className="min-h-0 flex-1" contentClassName="px-5 py-4">
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
            <div className="flex flex-wrap gap-2 border-t px-5 py-2">
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
          {error && <p className="px-5 pb-2 text-xs text-destructive">{error}</p>}
          <DialogFooter className="flex-row items-center justify-between border-t px-5 py-3 sm:flex-row sm:justify-between">
            <input
              ref={attachmentInputRef}
              className="sr-only"
              type="file"
              multiple
              onChange={(event) => {
                const selected = Array.from(event.target.files || []);
                setAttachments((current) => {
                  const next = [...current, ...selected];
                  if ([...next, ...inlineImages.map((image) => image.file)].reduce((total, file) => total + file.size, 0) > MAX_COMPOSE_ATTACHMENT_BYTES) {
                    setError(copy.attachmentsTooLarge);
                    return current;
                  }
                  setError("");
                  return next;
                });
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
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                {copy.cancel}
              </Button>
              <Button type="submit" disabled={mutation.isPending || documentBusy || documentAction !== null}>
                <Send />
                {mutation.isPending ? copy.sending : copy.send}
              </Button>
            </div>
          </DialogFooter>
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
              setError("");
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
