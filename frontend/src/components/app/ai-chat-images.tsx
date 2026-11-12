import { FileText, Download } from "lucide-react";
import { attachmentSize, isAIImage } from "../../lib/ai-attachment-types";
import { useState } from "react";
import type { AIChatAttachment } from "../../types";
import { aiChatAttachmentURL } from "../../lib/api";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "../ui/dialog";

export function AIChatImages({ attachments, accountEmail }: { attachments?: AIChatAttachment[]; accountEmail: string }) {
  const [selected, setSelected] = useState<AIChatAttachment | null>(null);
  if (!attachments?.length || !accountEmail) return null;
  return <>
    <div className="flex max-w-full flex-wrap items-start gap-2" data-testid="ai-message-images">
      {attachments.map(image => !isAIImage(image.contentType) ? <a key={image.id} href={aiChatAttachmentURL(accountEmail, image.id)} download={image.filename} className="flex max-w-full items-center gap-2 rounded-xl border bg-muted px-3 py-2 hover:bg-muted/70" title={image.filename}>
        <FileText className="size-6 shrink-0 text-muted-foreground" />
        <span className="min-w-0"><span className="block max-w-56 truncate text-sm">{image.filename}</span><span className="block text-xs text-muted-foreground">{attachmentSize(image.size)}</span></span>
        <Download className="size-4 shrink-0 text-muted-foreground" />
      </a> : <button type="button" key={image.id} onClick={() => setSelected(image)} className="max-w-full overflow-hidden rounded-xl border bg-muted" aria-label={image.filename} title={image.filename}>
        <img src={aiChatAttachmentURL(accountEmail, image.id)} alt={image.filename} className="h-28 max-w-44 object-contain sm:h-36 sm:max-w-56" loading="lazy" />
      </button>)}
    </div>
    <Dialog open={Boolean(selected)} onOpenChange={open => !open && setSelected(null)}>
      <DialogContent className="max-h-[90dvh] overflow-auto sm:max-w-4xl" aria-describedby={undefined}>
        <DialogHeader><DialogTitle className="break-all pr-6">{selected?.filename}</DialogTitle></DialogHeader>
        {selected && <img src={aiChatAttachmentURL(accountEmail, selected.id)} alt={selected.filename} className="mx-auto max-h-[75dvh] max-w-full object-contain" />}
      </DialogContent>
    </Dialog>
  </>;
}
