import { useEffect, useState } from "react";
import { AlertTriangle, Download, ExternalLink, Loader2 } from "lucide-react";
import type { Copy } from "../../lib/locale";
import { formatSize } from "../../lib/utils";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";

/** A previewable attachment resolved to the exact URLs the dialog should load. */
export type AttachmentPreviewTarget = {
  id: string;
  filename: string;
  contentType?: string;
  size?: number;
  /** Serves the bytes with `Content-Disposition: inline` so the browser renders them. */
  previewURL: string;
  /** Serves the bytes as a download. */
  downloadURL?: string;
};

export type AttachmentPreviewKind = "image" | "pdf";

/**
 * Decide how to render an attachment from its declared format.
 *
 * PDFs go to the browser's built-in viewer. Everything else is treated as an
 * image — including attachments with no usable format at all — and falls back
 * to a download prompt when the bytes turn out not to be an image.
 */
export function attachmentPreviewKind(contentType?: string, filename?: string): AttachmentPreviewKind {
  const type = (contentType || "").trim().toLowerCase();
  const name = (filename || "").trim().toLowerCase();
  if (type === "application/pdf" || type.includes("pdf") || name.endsWith(".pdf")) return "pdf";
  return "image";
}

export function attachmentPreviewKindLabel(kind: AttachmentPreviewKind, copy: Copy) {
  return kind === "pdf" ? copy.attachmentPDF : copy.attachmentImages;
}

/** The inline URL for a mirrored (IMAP) attachment. */
export function mirroredAttachmentPreviewURL(id: string, accountEmail?: string) {
  const query = new URLSearchParams();
  if (accountEmail) query.set("account_email", accountEmail);
  query.set("inline", "true");
  return `/api/attachment/${encodeURIComponent(id)}?${query.toString()}`;
}

/** The download URL for a mirrored (IMAP) attachment. */
export function mirroredAttachmentDownloadURL(id: string, accountEmail?: string) {
  const query = new URLSearchParams();
  if (accountEmail) query.set("account_email", accountEmail);
  const suffix = query.toString();
  return `/api/attachment/${encodeURIComponent(id)}${suffix ? `?${suffix}` : ""}`;
}

export function AttachmentPreviewDialog({
  copy,
  target,
  onOpenChange,
}: {
  copy: Copy;
  target: AttachmentPreviewTarget | null;
  onOpenChange: (open: boolean) => void;
}) {
  const [status, setStatus] = useState<"loading" | "ready" | "failed">("loading");
  const [slow, setSlow] = useState(false);
  const kind = attachmentPreviewKind(target?.contentType, target?.filename);
  const previewKey = target ? `${target.id}:${target.previewURL}` : "";

  // A new attachment (or a new URL for the same one) restarts the load state.
  // The timer turns an indefinite spinner into an explanation: a frame that is
  // served as a download never fires load, so nothing else would tell the user
  // why the preview stayed empty.
  useEffect(() => {
    setStatus("loading");
    setSlow(false);
    const timer = window.setTimeout(() => setSlow(true), 6000);
    return () => window.clearTimeout(timer);
  }, [previewKey]);

  return (
    <Dialog open={Boolean(target)} onOpenChange={onOpenChange}>
      <DialogContent data-testid="attachment-preview-dialog" className="gap-3 sm:max-w-4xl" aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle className="truncate pr-8" title={target?.filename}>
            {target?.filename || copy.previewAttachment}
          </DialogTitle>
          <DialogDescription data-testid="attachment-preview-meta">
            {[target?.size === undefined ? "" : formatSize(target.size), target ? attachmentPreviewKindLabel(kind, copy) : ""].filter(Boolean).join(" · ")}
          </DialogDescription>
        </DialogHeader>
        <div className="relative grid min-h-[40vh] place-items-center overflow-hidden rounded-lg border bg-muted/40">
          {target && kind === "pdf" && status !== "failed" && (
            <iframe
              data-testid="attachment-preview-pdf"
              className="h-[70vh] w-full"
              src={target.previewURL}
              title={target.filename || copy.previewAttachment}
              onLoad={() => setStatus("ready")}
              onError={() => setStatus("failed")}
            />
          )}
          {target && kind === "image" && status !== "failed" && (
            <img
              data-testid="attachment-preview-image"
              className="mx-auto max-h-[70vh] w-auto max-w-full object-contain"
              src={target.previewURL}
              alt={target.filename || copy.previewAttachment}
              onLoad={() => setStatus("ready")}
              onError={() => setStatus("failed")}
            />
          )}
          {target && status === "failed" && (
            <div data-testid="attachment-preview-unsupported" className="flex flex-col items-center gap-2 px-6 py-12 text-center">
              <AlertTriangle className="size-6 text-muted-foreground" />
              <p className="text-sm font-medium">{copy.previewUnsupported}</p>
              <p className="max-w-sm text-xs text-muted-foreground">{copy.previewUnsupportedHint}</p>
            </div>
          )}
          {target && status === "loading" && (
            <div className="pointer-events-none absolute flex items-center gap-2 text-xs text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" />
              <span>{copy.previewLoading}</span>
            </div>
          )}
        </div>
        {target && status === "loading" && slow && (
          <p data-testid="attachment-preview-slow" className="-mt-1 text-center text-xs text-muted-foreground">{copy.previewSlow}</p>
        )}
        <DialogFooter>
          {target?.downloadURL && (
            <Button nativeButton={false} render={<a href={target.downloadURL} download={target.filename} />} variant="outline" size="sm" data-testid="attachment-preview-download">
              <Download />
              <span>{copy.downloadAttachment}</span>
            </Button>
          )}
          {target && (
            <Button nativeButton={false} render={<a href={target.previewURL} target="_blank" rel="noreferrer" />} variant="outline" size="sm" data-testid="attachment-preview-open">
              <ExternalLink />
              <span>{copy.openInNewWindow}</span>
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
