import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy as CopyIcon, History, KeyRound, LoaderCircle, Plus, RefreshCw, RotateCw, Trash2 } from "lucide-react";
import { createUserAPIKey, getUserAPIKeyEvents, getUserAPIKeys, revokeUserAPIKey, rotateUserAPIKey, type UserAPIKey, type UserAPIKeyWithSecret } from "../../lib/api";
import { type Copy, en } from "../../lib/locale";
import { toast, toastError } from "../../lib/toast";
import { useQueryErrorToast } from "../../lib/use-query-error-toast";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { ScrollArea } from "../ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";

export function APIKeysSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const keys = useQuery({ queryKey: ["user-api-keys"], queryFn: getUserAPIKeys, retry: false });
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [expiry, setExpiry] = useState("90");
  const [customExpiry, setCustomExpiry] = useState("");
  const [pending, setPending] = useState(false);
  const [secret, setSecret] = useState<UserAPIKeyWithSecret | null>(null);
  const [action, setAction] = useState<{ key: UserAPIKey; kind: "rotate" | "revoke" } | null>(null);
  const [history, setHistory] = useState<UserAPIKey | null>(null);
  const events = useQuery({ queryKey: ["user-api-key-events", history?.id], queryFn: () => getUserAPIKeyEvents(history!.id), enabled: Boolean(history), retry: false });
  useQueryErrorToast(keys.error, copy.apiKeyLoadFailed, "user-api-keys-error", () => void keys.refetch(), copy.retry);
  useQueryErrorToast(events.error, copy.apiKeyLoadFailed, "user-api-key-events-error", () => void events.refetch(), copy.retry);

  const formatTime = (value: string | null) => value ? new Date(value).toLocaleString(copy === en ? "en" : "zh-CN") : "—";
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["user-api-keys"] });
    void queryClient.invalidateQueries({ queryKey: ["user-api-key-events"] });
  };

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    let expiresAt: string | null = null;
    if (expiry !== "never") {
      const date = expiry === "custom" ? new Date(customExpiry) : new Date(Date.now() + Number(expiry) * 86_400_000);
      if (!Number.isFinite(date.getTime()) || date.getTime() <= Date.now()) {
        toast.error(copy.apiKeyExpiryInvalid);
        return;
      }
      expiresAt = date.toISOString();
    }
    setPending(true);
    try {
      // Keep secrets out of React Query's caches and browser storage.
      const created = await createUserAPIKey({ name: name.trim(), expiresAt });
      setSecret(created);
      setCreateOpen(false);
      setName("");
      setCustomExpiry("");
      setExpiry("90");
      refresh();
      toast.success(copy.apiKeyCreatedToast);
    } catch (error) {
      toastError(error, copy.apiKeyLoadFailed);
    } finally {
      setPending(false);
    }
  }

  async function confirmAction() {
    if (!action) return;
    setPending(true);
    try {
      if (action.kind === "rotate") {
        setSecret(await rotateUserAPIKey(action.key.id));
        toast.success(copy.apiKeyRotatedToast);
      } else {
        await revokeUserAPIKey(action.key.id);
        toast.success(copy.apiKeyRevokedToast);
      }
      setAction(null);
      refresh();
    } catch (error) {
      toastError(error, copy.apiKeyLoadFailed);
    } finally {
      setPending(false);
    }
  }

  const statusLabel = (key: UserAPIKey) => key.status === "active" ? copy.apiKeyActive : key.status === "expired" ? copy.apiKeyExpired : copy.apiKeyRevoked;
  const actions = (key: UserAPIKey) => <div className="flex shrink-0 justify-end gap-1"><Button variant="ghost" size="icon" onClick={() => setHistory(key)} aria-label={copy.apiKeyHistory} title={copy.apiKeyHistory}><History /></Button>{key.status === "active" && <Button variant="ghost" size="icon" onClick={() => setAction({ key, kind: "rotate" })} aria-label={copy.apiKeyRotate} title={copy.apiKeyRotate}><RotateCw /></Button>}{key.status !== "revoked" && <Button variant="ghost" size="icon" className="text-destructive" onClick={() => setAction({ key, kind: "revoke" })} aria-label={copy.apiKeyRevoke} title={copy.apiKeyRevoke}><Trash2 /></Button>}</div>;

  return (
    <section className="grid min-w-0 grid-cols-1 gap-5" data-testid="api-keys-settings">
      <div className="flex flex-col items-start justify-between gap-3 sm:flex-row">
        <div className="min-w-0 flex-1"><h2 className="flex items-center gap-2 text-lg font-semibold"><KeyRound className="size-5" />{copy.apiKeys}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.apiKeysDescription}</p></div>
        <div className="flex gap-2"><Button variant="outline" size="icon" onClick={refresh} aria-label={copy.retry} disabled={keys.isFetching}><RefreshCw className={keys.isFetching ? "animate-spin" : undefined} /></Button><Button onClick={() => setCreateOpen(true)}><Plus />{copy.apiKeyCreate}</Button></div>
      </div>
      <div className="min-w-0 rounded-lg border">
        <div className="divide-y sm:hidden">
          {keys.isPending ? <p className="p-5 text-center text-sm">{copy.loading}</p> : keys.data?.keys.map((key) => <article key={key.id} className="grid gap-3 p-4">
            <div className="flex items-start justify-between gap-2"><p className="min-w-0 break-words text-sm font-medium">{key.name}</p><Badge variant={key.status === "revoked" ? "destructive" : key.status === "expired" ? "secondary" : "outline"}>{statusLabel(key)}</Badge></div>
            <p className="break-all font-mono text-xs text-muted-foreground">{key.prefix}…</p>
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs text-muted-foreground"><dt>{copy.apiKeyExpiry}</dt><dd className="break-words text-right">{key.expiresAt ? formatTime(key.expiresAt) : copy.apiKeyNever}</dd><dt>{copy.apiKeyLastUsed}</dt><dd className="break-words text-right">{key.lastUsedAt ? formatTime(key.lastUsedAt) : copy.apiKeyUnused}{key.lastUsedIP && <p className="mt-1">{key.lastUsedIP}</p>}</dd><dt>{copy.apiKeyCreated}</dt><dd className="break-words text-right">{formatTime(key.createdAt)}</dd></dl>
            {actions(key)}
          </article>)}
          {keys.data?.keys.length === 0 && <p className="p-5 text-center text-sm text-muted-foreground">{copy.apiKeysEmpty}</p>}
        </div>
        <div className="hidden min-w-0 sm:block">
        <Table>
          <TableHeader><TableRow><TableHead>{copy.apiKeyName}</TableHead><TableHead>{copy.apiKeyExpiry}</TableHead><TableHead>{copy.apiKeyLastUsed}</TableHead><TableHead className="w-28"><span className="sr-only">{copy.apiKeyHistory}</span></TableHead></TableRow></TableHeader>
          <TableBody>
            {keys.isPending ? <TableRow><TableCell colSpan={4} className="h-24 text-center">{copy.loading}</TableCell></TableRow> : keys.data?.keys.map((key) => (
              <TableRow key={key.id}>
                <TableCell className="max-w-56 whitespace-normal"><p className="break-words font-medium">{key.name}</p><p className="mt-1 font-mono text-xs text-muted-foreground">{key.prefix}…</p><Badge variant={key.status === "revoked" ? "destructive" : key.status === "expired" ? "secondary" : "outline"} className="mt-2">{statusLabel(key)}</Badge></TableCell>
                <TableCell className="whitespace-normal text-xs text-muted-foreground">{key.expiresAt ? formatTime(key.expiresAt) : copy.apiKeyNever}<p className="mt-1">{copy.apiKeyCreated}: {formatTime(key.createdAt)}</p></TableCell>
                <TableCell className="whitespace-normal text-xs text-muted-foreground">{key.lastUsedAt ? formatTime(key.lastUsedAt) : copy.apiKeyUnused}{key.lastUsedIP && <p className="mt-1">{key.lastUsedIP}</p>}</TableCell>
                <TableCell>{actions(key)}</TableCell>
              </TableRow>
            ))}
            {keys.data?.keys.length === 0 && <TableRow><TableCell colSpan={4} className="h-24 text-center text-muted-foreground">{copy.apiKeysEmpty}</TableCell></TableRow>}
          </TableBody>
        </Table>
        </div>
      </div>
      <div className="grid min-w-0 gap-2 rounded-lg border bg-muted/30 p-4"><h3 className="text-sm font-medium">{copy.apiKeyUsage}</h3><p className="text-sm text-muted-foreground">{copy.apiKeyUsageDescription}</p><code className="break-all whitespace-pre-wrap text-xs">{`curl '${window.location.origin}/api/capabilities' \\\n  -H 'Authorization: Bearer <API_KEY>'`}</code></div>

      <Dialog open={createOpen} onOpenChange={(open) => { if (!pending) setCreateOpen(open); }}>
        <DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>{copy.apiKeyCreate}</DialogTitle><DialogDescription>{copy.apiKeysDescription}</DialogDescription></DialogHeader>
          <form onSubmit={(event) => void create(event)} className="grid gap-4">
            <div className="grid gap-2"><Label htmlFor="user-api-key-name">{copy.apiKeyName}</Label><Input id="user-api-key-name" placeholder={copy.apiKeyNamePlaceholder} value={name} onChange={(event) => setName(event.target.value)} maxLength={120} required disabled={pending} autoComplete="off" /></div>
            <div className="grid gap-2"><Label htmlFor="user-api-key-expiry">{copy.apiKeyExpiry}</Label><Select value={expiry} onValueChange={(value) => { if (value) setExpiry(value); }} disabled={pending}><SelectTrigger id="user-api-key-expiry" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="30">{copy.apiKeyDays30}</SelectItem><SelectItem value="90">{copy.apiKeyDays90}</SelectItem><SelectItem value="365">{copy.apiKeyDays365}</SelectItem><SelectItem value="never">{copy.apiKeyNever}</SelectItem><SelectItem value="custom">{copy.apiKeyCustomExpiry}</SelectItem></SelectContent></Select></div>
            {expiry === "custom" && <div className="grid gap-2"><Label htmlFor="user-api-key-custom-expiry">{copy.apiKeyCustomExpiry}</Label><Input id="user-api-key-custom-expiry" type="datetime-local" value={customExpiry} onChange={(event) => setCustomExpiry(event.target.value)} required disabled={pending} /></div>}
            <DialogFooter><Button type="button" variant="ghost" onClick={() => setCreateOpen(false)} disabled={pending}>{copy.cancel}</Button><Button type="submit" disabled={pending || !name.trim()}>{pending && <LoaderCircle className="animate-spin" />}{copy.apiKeyCreate}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(secret)} onOpenChange={(open) => { if (!open) setSecret(null); }}>
        <DialogContent className="sm:max-w-lg"><DialogHeader><DialogTitle>{copy.apiKeySecretTitle}</DialogTitle><DialogDescription>{copy.apiKeySecretDescription}</DialogDescription></DialogHeader><p className="text-sm font-medium">{secret?.name}</p><code className="select-all break-all rounded-md border bg-muted p-3 font-mono text-sm">{secret?.key}</code><DialogFooter><Button variant="outline" onClick={() => { if (secret) void navigator.clipboard.writeText(secret.key).then(() => toast.success(copy.aiChatCopySuccess)).catch((error: unknown) => toastError(error, copy.apiKeyLoadFailed)); }}><CopyIcon />{copy.apiKeyCopy}</Button><Button onClick={() => setSecret(null)}>{copy.apiKeyDone}</Button></DialogFooter></DialogContent>
      </Dialog>

      <Dialog open={Boolean(action)} onOpenChange={(open) => { if (!open && !pending) setAction(null); }}>
        <DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>{action?.kind === "rotate" ? copy.apiKeyRotateTitle : copy.apiKeyRevokeTitle}</DialogTitle><DialogDescription>{action?.kind === "rotate" ? copy.apiKeyRotateDescription : copy.apiKeyRevokeDescription}</DialogDescription></DialogHeader><p className="break-words text-sm font-medium">{action?.key.name}</p><DialogFooter><Button variant="ghost" onClick={() => setAction(null)} disabled={pending}>{copy.cancel}</Button><Button variant={action?.kind === "revoke" ? "destructive" : "default"} onClick={() => void confirmAction()} disabled={pending}>{pending && <LoaderCircle className="animate-spin" />}{action?.kind === "rotate" ? copy.apiKeyRotate : copy.apiKeyRevoke}</Button></DialogFooter></DialogContent>
      </Dialog>

      <Dialog open={Boolean(history)} onOpenChange={(open) => { if (!open) setHistory(null); }}>
        <DialogContent className="sm:max-w-lg"><DialogHeader><DialogTitle>{copy.apiKeyHistory}</DialogTitle><DialogDescription>{history?.name}</DialogDescription></DialogHeader><ScrollArea className="h-72" contentClassName="grid gap-3 pr-3">{events.isPending ? <p className="text-sm text-muted-foreground">{copy.loading}</p> : events.data?.events.map((event) => <div key={event.id} className="flex flex-wrap items-start justify-between gap-2 border-b pb-3 text-sm"><div><p>{event.type === "created" ? copy.apiKeyEventCreated : event.type === "rotated" ? copy.apiKeyEventRotated : copy.apiKeyEventRevoked}</p><p className="mt-1 text-xs text-muted-foreground">{event.clientIP}{event.reason && ` · ${event.reason}`}</p></div><time className="text-xs text-muted-foreground" dateTime={event.createdAt}>{formatTime(event.createdAt)}</time></div>)}</ScrollArea></DialogContent>
      </Dialog>
    </section>
  );
}
