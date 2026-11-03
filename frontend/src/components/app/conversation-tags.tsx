import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Loader2, Plus, Tag } from "lucide-react";
import { saveConversationTags } from "../../lib/api";
import type { Copy } from "../../lib/locale";
import { toastError } from "../../lib/toast";
import type { ConversationDetailResponse, ConversationListResponse } from "../../types";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from "../ui/popover";

export function ConversationTagNames({ tags = [] }: { tags?: string[] }) {
  if (!tags.length) return null;
  return (
    <span data-testid="conversation-tags" title={tags.join(" · ")} className="inline-flex h-5 min-w-0 max-w-[42%] shrink-0 items-center gap-1 rounded-full border border-violet-200 bg-violet-50 px-1.5 text-[10px] font-medium text-violet-700 dark:border-violet-400/40 dark:bg-violet-400/15 dark:text-violet-200">
      <span className="truncate">{tags[0]}</span>
      {tags.length > 1 && <span>+{tags.length - 1}</span>}
    </span>
  );
}

export function ConversationTags({ id, tags = [], options, copy }: {
  id: string;
  tags?: string[];
  options: string[];
  copy: Copy;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const mutation = useMutation({
    mutationFn: (next: string[]) => saveConversationTags(id, next),
    onSuccess: async ({ tags: saved }) => {
      setName("");
      await queryClient.cancelQueries({ queryKey: ["conversations"] });
      queryClient.setQueriesData<ConversationListResponse>({ queryKey: ["conversations"] }, (current) => current ? {
        ...current,
        tags: [...new Set([...(current.tags || []), ...saved])].sort(),
        conversations: current.conversations.map((item) => item.id === id ? { ...item, tags: saved } : item),
      } : current);
      queryClient.setQueriesData<ConversationDetailResponse>({ queryKey: ["conversation", id] }, (current) => current ? {
        ...current, conversation: { ...current.conversation, tags: saved },
      } : current);
      void queryClient.invalidateQueries({ queryKey: ["conversations"] });
      void queryClient.invalidateQueries({ queryKey: ["conversation", id] });
    },
    onError: (error) => toastError(error, copy.tagSaveFailed),
  });
  const choices = [...new Set([...options, ...tags])].sort();
  const trimmedName = name.trim();
  const atLimit = tags.length >= 10;

  return (
    <Popover>
      <PopoverTrigger
        render={<Button variant="ghost" size="sm" />}
        data-testid="edit-conversation-tags"
        aria-label={copy.editConversationTags}
        title={copy.editConversationTags}
        className="h-8 gap-1.5 text-xs text-muted-foreground hover:text-foreground"
      >
        <Tag className="size-3.5" />
        <span className="hidden sm:inline">{copy.conversationTags}</span>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-64 max-w-[calc(100vw-2rem)]">
        <PopoverTitle>{copy.conversationTags}</PopoverTitle>
        <div className="max-h-52 overflow-y-auto">
          {choices.map((tag) => (
            <Button key={tag} variant="ghost" size="sm" className="w-full justify-start" aria-pressed={tags.includes(tag)} disabled={mutation.isPending || (!tags.includes(tag) && atLimit)}
              onClick={() => mutation.mutate(tags.includes(tag) ? tags.filter((value) => value !== tag) : [...tags, tag])}>
              <span className="truncate">{tag}</span>
              {tags.includes(tag) && <Check className="ml-auto size-3.5" />}
            </Button>
          ))}
        </div>
        <form className="flex gap-1.5 border-t pt-2" onSubmit={(event) => {
          event.preventDefault();
          if (trimmedName && !mutation.isPending && !atLimit && !tags.includes(trimmedName)) mutation.mutate([...tags, trimmedName]);
        }}>
          <Input aria-label={copy.newConversationTag} placeholder={copy.newConversationTag} value={name} maxLength={32} disabled={mutation.isPending || atLimit} onChange={(event) => setName(event.target.value)} className="h-8 min-w-0 flex-1" />
          <Button type="submit" size="icon-sm" aria-label={copy.createConversationTag} title={copy.createConversationTag} disabled={!trimmedName || mutation.isPending || atLimit || tags.includes(trimmedName)}>
            {mutation.isPending ? <Loader2 className="animate-spin" /> : <Plus />}
          </Button>
        </form>
        <p className="text-[10px] text-muted-foreground">{copy.tagLimitHint}</p>
      </PopoverContent>
    </Popover>
  );
}
