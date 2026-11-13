import { useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import type { Editor } from "@tiptap/core";
import { Check, Copy, Send, Sparkles } from "lucide-react";
import { generateEmail, type EmailWritingMessage } from "../../lib/api";
import { aiConversationContext, htmlToPlainText, replaceEditorDraft } from "../../lib/email-format";
import { useAIStateMachine } from "../../lib/ai-state-machine";
import { type Copy as LocaleCopy, zh } from "../../lib/locale";
import { toast, toastError } from "../../lib/toast";
import { cn } from "../../lib/utils";
import type { ConversationMessage } from "../../types";
import { Button } from "../ui/button";
import { Label } from "../ui/label";
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from "../ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Textarea } from "../ui/textarea";
import { AIStatusIndicator } from "./ai-status-indicator";

function currentEditorBody(editor: Editor | null) {
  if (!editor) return "";
  const document = new DOMParser().parseFromString(editor.getHTML(), "text/html");
  document.querySelectorAll("[data-inbrix-signature], [data-inbrix-reply-quote]").forEach((node) => node.remove());
  return htmlToPlainText(document.body.innerHTML);
}

export function AIAssistantButton({ copy, editor, disabled, composeOpen, conversationId, accountEmail, subject, recipients, conversation }: {
  copy: LocaleCopy;
  editor: Editor | null;
  disabled: boolean;
  composeOpen: boolean;
  conversationId: string;
  accountEmail: string;
  subject: string;
  recipients: string;
  conversation?: ConversationMessage[];
}) {
  const [open, setOpen] = useState(false);
  const [instruction, setInstruction] = useState("");
  const [includeConversation, setIncludeConversation] = useState(Boolean(conversation?.length));
  const [replyLanguage, setReplyLanguage] = useState("auto");
  const [messages, setMessages] = useState<EmailWritingMessage[]>([]);
  const [candidate, setCandidate] = useState("");
  const [copiedMessage, setCopiedMessage] = useState<number | null>(null);
  const chatRef = useRef<HTMLDivElement>(null);
  const generation = useRef(0);
  const lastEditorBody = useRef<string | null>(null);
  const isZh = copy === zh;
  const aiState = useAIStateMachine({ taskType: "reply", locale: isZh ? "zh" : "en" });
  const hasConversation = Boolean(conversation?.length);
  const languages = [
    { value: "auto", label: copy.aiReplyLanguageAuto },
    { value: "en", label: "English" },
    { value: "zh-CN", label: "简体中文" },
    { value: "ja", label: "日本語" },
    { value: "de", label: "Deutsch" },
    { value: "fr", label: "Français" },
    { value: "es", label: "Español" },
    { value: "ru", label: "Русский" },
  ];

  const mutation = useMutation({
    mutationFn: ({ prompt, history, draft }: { prompt: string; history: EmailWritingMessage[]; draft: string; generation: number }) =>
      aiState.run(() => generateEmail({
        accountEmail, instruction: prompt, subject, recipients,
        context: includeConversation && conversation ? aiConversationContext(conversation) : undefined,
        draft: draft || undefined, responseFormat: "structured", replyLanguage, messages: history,
      })),
    onSuccess: (result, request) => {
      if (request.generation !== generation.current) return;
      if (result.body) setCandidate(result.body);
      setMessages((current) => [...current, { role: "assistant", content: result.message || "", body: result.body, translation: result.translation }]);
    },
    onError: (error, request) => {
      if (request.generation !== generation.current) return;
      setMessages(request.history);
      setInstruction(request.prompt);
      toastError(error, copy.aiGenerateFailed);
    },
  });

  // Mail refreshes can replace the conversation array while a turn is running.
  // Only a different compose session should discard the writing discussion.
  useEffect(() => {
    generation.current++;
    setOpen(false);
    setInstruction("");
    setMessages([]);
    setCandidate("");
    setCopiedMessage(null);
    setReplyLanguage("auto");
    lastEditorBody.current = null;
    setIncludeConversation(Boolean(conversation?.length));
  }, [composeOpen, accountEmail, conversationId]);

  useEffect(() => () => { generation.current++; }, []);

  useEffect(() => {
    if (open && chatRef.current) chatRef.current.scrollTop = chatRef.current.scrollHeight;
  }, [messages, mutation.isPending, open]);

  const submit = () => {
    if (mutation.isPending) return;
    const editorBody = currentEditorBody(editor);
    const draft = candidate && editorBody === lastEditorBody.current ? candidate : editorBody;
    const prompt = instruction.trim() || copy.aiGenerateRequest;
    if (!instruction.trim() && !subject.trim() && !draft && !(includeConversation && hasConversation)) return;
    setMessages((current) => [...current, { role: "user", content: prompt }]);
    setInstruction("");
    lastEditorBody.current = editorBody;
    mutation.mutate({ prompt, history: messages, draft, generation: generation.current });
  };

  const copyBody = async (text: string, index: number) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedMessage(index);
      window.setTimeout(() => setCopiedMessage((current) => current === index ? null : current), 1600);
    } catch {
      toast.error(copy.copyAIContentFailed);
    }
  };

  const adopt = (body: string) => {
    if (!editor || !body || mutation.isPending || disabled) return;
    replaceEditorDraft(editor, body);
    setCandidate(body);
    lastEditorBody.current = currentEditorBody(editor);
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={<Button type="button" variant={open ? "secondary" : "ghost"} size="icon" disabled={disabled || !editor} aria-label={copy.aiWriteEmail} title={copy.aiWriteEmail} />}>
        <Sparkles />
      </PopoverTrigger>
      <PopoverContent side="bottom" align="start" sideOffset={8} className="flex max-h-[min(44rem,var(--available-height),calc(100dvh-2rem))] w-[min(34rem,calc(100vw-2rem))] flex-col gap-0 overflow-hidden p-4" data-testid="email-writing-assistant">
        <PopoverTitle className="shrink-0 text-sm font-semibold">{copy.aiWriteEmail}</PopoverTitle>
        <PopoverDescription className="mt-1 shrink-0 text-xs">{copy.aiWriteDescription}</PopoverDescription>
        {messages.length > 0 && (
          <div ref={chatRef} className="mt-4 flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain pr-1" aria-live="polite">
            {messages.map((message, index) => message.role === "user" ? (
              <div key={index} className="ml-10 self-end whitespace-pre-wrap break-words rounded-lg border bg-muted/40 px-3 py-2 text-sm leading-5">{message.content}</div>
            ) : (
              <div key={index} className="grid shrink-0 gap-3 text-sm leading-6" data-testid="email-writing-answer">
                {message.content && <div className="whitespace-pre-wrap break-words" data-testid="email-writing-discussion">{message.content}</div>}
                {message.body && (
                  <div className="overflow-hidden rounded-lg border">
                    <div className="bg-muted/40 px-3 py-1.5 text-xs font-medium text-muted-foreground">{copy.aiEmailBody}</div>
                    <div className="whitespace-pre-wrap break-words px-3 py-2" data-testid="email-writing-body">{message.body}</div>
                    {message.translation && (
                      <div className="border-t bg-muted/20 px-3 py-2">
                        <div className="mb-1 text-xs font-medium text-muted-foreground">{copy.aiEmailTranslation}</div>
                        <div className="whitespace-pre-wrap break-words text-muted-foreground" data-testid="email-writing-translation">{message.translation}</div>
                      </div>
                    )}
                    <div className="flex justify-end gap-1 border-t px-2 py-1">
                      <Button type="button" variant="ghost" size="sm" onClick={() => void copyBody(message.body!, index)} aria-label={copy.copyAIContent} title={copy.copyAIContent}>
                        {copiedMessage === index ? <Check /> : <Copy />}
                        {copiedMessage === index ? copy.copiedAIContent : copy.copyAIContent}
                      </Button>
                      <Button type="button" variant="outline" size="sm" disabled={mutation.isPending || disabled} onClick={() => adopt(message.body!)}>
                        <Check />{copy.useGeneratedDraft}
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            ))}
            {mutation.isPending && <div className="shrink-0"><AIStatusIndicator snapshot={aiState.snapshot} variant="card" showTimer showSteps /></div>}
          </div>
        )}
        <div className="mt-4 grid shrink-0 gap-3 border-t pt-3">
          <div className="flex items-center justify-between gap-2">
            <Label htmlFor="ai-compose-language" className="shrink-0 text-xs">{copy.aiReplyLanguage}</Label>
            <Select value={replyLanguage} onValueChange={(value) => { if (value) setReplyLanguage(value); }} disabled={mutation.isPending} items={languages}>
              <SelectTrigger id="ai-compose-language" size="sm" className="w-56 max-w-[70%]"><SelectValue /></SelectTrigger>
              <SelectContent>{languages.map((language) => <SelectItem key={language.value} value={language.value}>{language.label}</SelectItem>)}</SelectContent>
            </Select>
          </div>
          <Label className="grid gap-1.5 text-xs" htmlFor="ai-compose-instruction">
            {copy.aiInstruction}
            <Textarea id="ai-compose-instruction" aria-label={copy.aiInstruction} value={instruction} onChange={(event) => setInstruction(event.target.value)} placeholder={messages.length ? copy.refineInstructionPlaceholder : copy.aiInstructionPlaceholder} rows={2} disabled={mutation.isPending}
              onKeyDown={(event) => {
                if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && !event.nativeEvent.isComposing) { event.preventDefault(); submit(); }
              }} />
          </Label>
          <div className="flex items-center justify-between gap-3">
            <label className={cn("flex min-w-0 items-center gap-2 text-xs", !hasConversation && "text-muted-foreground")} title={!hasConversation ? copy.noConversationContext : undefined}>
              <input className="size-4 shrink-0 accent-primary" type="checkbox" checked={includeConversation && hasConversation} disabled={!hasConversation || mutation.isPending} onChange={(event) => setIncludeConversation(event.target.checked)} />
              <span className="truncate">{copy.includeConversation}</span>
            </label>
            <Button type="button" size="sm" disabled={mutation.isPending || (!instruction.trim() && !subject.trim() && !(includeConversation && hasConversation) && !candidate && !currentEditorBody(editor))} onClick={submit}>
              <Send />{mutation.isPending ? copy.generating : messages.length ? copy.regenerate : copy.generate}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
