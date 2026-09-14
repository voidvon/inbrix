import { useEffect, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  Download,
  ExternalLink,
  Eye,
  FileArchive,
  FileImage,
  FileSpreadsheet,
  FileText,
  Menu,
  Paperclip,
  Search,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { getMailAttachments } from "../../lib/api";
import { cn, formatSize, formatTime, paginationPageItems, useDebouncedValue } from "../../lib/utils";
import type { MailAttachment } from "../../types";
import { useShell } from "./shell-context";
import { EmptyState, ErrorState, ListSkeleton } from "./common-states";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "../ui/pagination";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";

export type AttachmentKind = "all" | "images" | "pdf" | "documents" | "spreadsheets" | "archives";

export function attachmentFileIcon(attachment: MailAttachment) {
  const type = attachment.contentType.toLowerCase();
  const filename = attachment.filename.toLowerCase();
  if (type.startsWith("image/")) return FileImage;
  if (type.includes("spreadsheet") || type.includes("excel") || /\.(?:xls|xlsx|csv)$/.test(filename)) return FileSpreadsheet;
  if (type.includes("zip") || type.includes("rar") || type.includes("7z") || /\.(?:zip|rar|7z|gz)$/.test(filename)) return FileArchive;
  return FileText;
}

export function attachmentDownloadURL(attachment: MailAttachment, inline = false) {
  const query = new URLSearchParams({ account_email: attachment.accountEmail });
  if (inline) query.set("inline", "true");
  return `/api/attachment/${encodeURIComponent(attachment.id)}?${query.toString()}`;
}

export function attachmentMessageURL(attachment: MailAttachment) {
  return `/folder/${encodeURIComponent(attachment.folder)}?message=${encodeURIComponent(attachment.messageId)}`;
}

export function AttachmentsPage() {
  const { copy: locale, openMobileMenu } = useShell();
  const [search, setSearch] = useState("");
  const [kind, setKind] = useState<AttachmentKind>("all");
  const [offset, setOffset] = useState(0);
  const debouncedSearch = useDebouncedValue(search, 250);
  const attachments = useQuery({
    queryKey: ["attachments", debouncedSearch, kind, offset],
    queryFn: () => getMailAttachments(debouncedSearch, kind, offset),
    placeholderData: keepPreviousData,
    retry: 1,
  });

  useEffect(() => {
    if (attachments.error && attachments.data) {
      toast.error(attachments.error instanceof Error ? attachments.error.message : locale.loadFailed, {
        id: "attachments-refresh-error",
        action: {
          label: locale.retry,
          onClick: () => void attachments.refetch(),
        },
      });
    } else if (!attachments.error) {
      toast.dismiss("attachments-refresh-error");
    }
  }, [attachments.error, attachments.data, locale.loadFailed, locale.retry]);

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
    <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
      <header className="flex min-h-14 items-center gap-3 border-b bg-card px-3 py-2 sm:px-5">
        <Button variant="ghost" size="icon" className="shrink-0 lg:hidden" onClick={openMobileMenu} aria-label={locale.folders} title={locale.folders}>
          <Menu />
        </Button>
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-sm font-semibold">{locale.attachmentManager}</h1>
          <p className="text-xs text-muted-foreground">{attachments.data?.total || 0} {locale.attachmentCount}</p>
        </div>
      </header>
      <div className="flex flex-col items-start justify-start gap-2 border-b bg-card px-3 py-3 sm:flex-row sm:items-center sm:px-5">
        <div className="relative flex h-8 w-full min-w-0 items-center sm:w-[220px] sm:flex-none">
          <Search className="pointer-events-none absolute left-3 size-4 text-muted-foreground" />
          <Input
            type="search"
            className="h-8 w-full bg-muted/60 pl-9 pr-9"
            value={search}
            onChange={(event) => { setSearch(event.target.value); setOffset(0); }}
            placeholder={locale.attachmentSearch}
            aria-label={locale.attachmentSearch}
          />
          {search && (
            <Button variant="ghost" size="icon" className="absolute right-1 size-7" onClick={() => { setSearch(""); setOffset(0); }} aria-label={locale.cancel}>
              <X />
            </Button>
          )}
        </div>
        <Select value={kind} onValueChange={(value) => { setKind(value as AttachmentKind); setOffset(0); }}>
          <SelectTrigger className="h-8 w-full min-w-0 sm:w-48" aria-label={locale.allAttachmentTypes}>
            <SelectValue>{kinds.find((item) => item.value === kind)?.label}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {kinds.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
      <div className="min-h-0 flex-1 overflow-auto [&_[data-slot=table-container]]:overflow-visible">
        {!attachments.data && !attachments.error && <ListSkeleton />}
        {!attachments.data && attachments.error && <ErrorState copy={locale} onRetry={() => void attachments.refetch()} />}
        {attachments.data && !attachments.data?.attachments.length && <EmptyState icon={<Paperclip />} text={locale.noAttachments} />}
        {Boolean(attachments.data?.attachments.length) && (
          <Table className="min-w-[780px] table-fixed">
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
              {attachments.data?.attachments.map((attachment) => {
                const Icon = attachmentFileIcon(attachment);
                return (
                  <TableRow key={`${attachment.folder}/${attachment.messageId}/${attachment.partId || attachment.id}`}>
                    <TableCell className="px-5 py-2.5">
                      <div className="flex min-w-0 items-center gap-2.5">
                        <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
                          <Icon className="size-4" />
                        </span>
                        <strong className="min-w-0 truncate text-sm font-medium" title={attachment.filename}>
                          {attachment.filename || locale.noSubject}
                        </strong>
                      </div>
                    </TableCell>
                    <TableCell>
                      <a className="block truncate text-sm hover:underline" href={attachmentMessageURL(attachment)} title={attachment.messageSubject}>
                        {attachment.messageSubject || locale.noSubject}
                      </a>
                    </TableCell>
                    <TableCell>
                      <span className="block truncate text-sm text-muted-foreground" title={attachment.fromName || attachment.messageFrom}>
                        {attachment.fromName || attachment.messageFrom}
                      </span>
                    </TableCell>
                    <TableCell className="text-right text-sm text-muted-foreground">{formatSize(attachment.size)}</TableCell>
                    <TableCell className="w-[8rem] max-w-[8rem] text-right text-xs text-muted-foreground">
                      <time className="block truncate" title={formatTime(attachment.messageDate)}>{formatTime(attachment.messageDate)}</time>
                    </TableCell>
                    <TableCell className="pr-5">
                      <div className="flex items-center justify-end gap-0.5">
                        <Button nativeButton={false} render={<a href={attachmentDownloadURL(attachment, true)} target="_blank" rel="noreferrer" aria-label={locale.previewAttachment} title={locale.previewAttachment} />} variant="ghost" size="icon" className="size-8">
                          <Eye />
                        </Button>
                        <Button nativeButton={false} render={<a href={attachmentDownloadURL(attachment)} aria-label={locale.downloadAttachment} title={locale.downloadAttachment} />} variant="ghost" size="icon" className="size-8">
                          <Download />
                        </Button>
                        <Button nativeButton={false} render={<a href={attachmentMessageURL(attachment)} aria-label={locale.viewOriginalMessage} title={locale.viewOriginalMessage} />} variant="ghost" size="icon" className="size-8">
                          <ExternalLink />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </div>
      {attachments.data && attachments.data.total > 0 && (
        <footer className="flex min-h-11 flex-col items-center justify-between gap-1 px-3 py-1.5 sm:flex-row sm:px-5">
          <span className="shrink-0 text-xs text-muted-foreground">
            {offset + 1}-{Math.min(offset + attachments.data.attachments.length, attachments.data.total)} / {attachments.data.total}
          </span>
          <Pagination className="mx-0 w-auto justify-end">
            <PaginationContent>
              <PaginationItem>
                <PaginationPrevious
                  href="#"
                  text={locale.previousPage}
                  aria-label={locale.previousPage}
                  aria-disabled={currentPage === 1}
                  tabIndex={currentPage === 1 ? -1 : undefined}
                  className={cn(currentPage === 1 && "pointer-events-none opacity-50")}
                  onClick={(event) => { event.preventDefault(); if (currentPage > 1) changePage(currentPage - 1); }}
                />
              </PaginationItem>
              {paginationPageItems(currentPage, pageCount).map((item) => typeof item === "number" ? (
                <PaginationItem key={item}>
                  <PaginationLink
                    href="#"
                    isActive={item === currentPage}
                    aria-label={`${locale.attachmentPage} ${item}`}
                    onClick={(event) => { event.preventDefault(); changePage(item); }}
                  >
                    {item}
                  </PaginationLink>
                </PaginationItem>
              ) : (
                <PaginationItem key={item}><PaginationEllipsis /></PaginationItem>
              ))}
              <PaginationItem>
                <PaginationNext
                  href="#"
                  text={locale.nextPage}
                  aria-label={locale.nextPage}
                  aria-disabled={currentPage === pageCount}
                  tabIndex={currentPage === pageCount ? -1 : undefined}
                  className={cn(currentPage === pageCount && "pointer-events-none opacity-50")}
                  onClick={(event) => { event.preventDefault(); if (currentPage < pageCount) changePage(currentPage + 1); }}
                />
              </PaginationItem>
            </PaginationContent>
          </Pagination>
        </footer>
      )}
    </main>
  );
}
