import type { ReactNode } from "react";
import { Paperclip } from "lucide-react";
import type { ConversationSummary } from "../../types";
import type { Copy } from "../../lib/locale";
import { formatTime } from "../../lib/utils";
import { Badge } from "../ui/badge";
import { ConversationTagNames } from "./conversation-tags";

/** Shared name and latest preview for inbox rows and conversation search hits. */
export function ConversationSummaryLines({ copy, conversation, renderText = text => text }: {
  copy: Copy;
  conversation: ConversationSummary;
  renderText?: (text: string) => ReactNode;
}) {
  const name = conversation.primaryPeerEmail || conversation.peerEmail || conversation.title || copy.conversations;
  return <>
    <div className="flex min-w-0 items-center gap-2">
      <strong className="min-w-0 flex-1 truncate text-sm font-semibold">{renderText(name)}</strong>
      <time className="shrink-0 text-[10px] text-muted-foreground">{formatTime(conversation.date)}</time>
      {conversation.unreadCount > 0 && (
        <Badge title={`${conversation.unreadCount} ${copy.unread}`} className="h-4 min-w-4 shrink-0 justify-center px-1 py-0 text-[10px] leading-none">
          {conversation.unreadCount}
        </Badge>
      )}
    </div>
    <div className="mt-1 flex min-w-0 items-center gap-2">
      {conversation.latestHasAttachments && (
        <span role="img" aria-label={copy.latestMailHasAttachments} title={copy.latestMailHasAttachments} className="shrink-0 text-muted-foreground/70">
          <Paperclip className="size-3.5" aria-hidden="true" />
        </span>
      )}
      <p className="min-w-0 flex-1 truncate text-xs text-muted-foreground/70">{renderText(conversation.preview || copy.noBody)}</p>
      <ConversationTagNames tags={conversation.tags || []} />
    </div>
  </>;
}
