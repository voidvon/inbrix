import { SkillSettings } from "./skill-settings";
import { AccountBrandSettings, AIBusinessProfileFields } from "./ai-business-controls";
import { emptyBusinessProfile, type AIBusinessProfile } from "../../lib/api";
import { useEffect, useRef, useState, type FormEvent, type MutableRefObject } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Bell,
  BellOff,
  BookOpen,
  Bot,
  Check,
  CircleAlert,
  Code2,
  ExternalLink,
  Info,
  Languages,
  KeyRound,
  Mail,
  Pencil,
  Plus,
  RefreshCw,
  Settings,
  ShieldCheck,
  Signature as SignatureIcon,
  Sparkles,
  Trash2,
  UserRound,
  X,
} from "lucide-react";
import { EditorContent, useEditor } from "@tiptap/react";
import LinkExtension from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import StarterKit from "@tiptap/starter-kit";
import UnderlineExtension from "@tiptap/extension-underline";
import { toast, toastError } from "../../lib/toast";
import { useQueryErrorToast } from "../../lib/use-query-error-toast";
import { EmailParagraph } from "../../extensions/email-paragraph";
import { EmailImage } from "../../extensions/email-image";
import {
  addAccount,
  addAIAgent,
  addAIModel,
  checkForUpdates,
  clearAIErrorLogs,
  deleteAccount,
  deleteAIModel,
  getAccounts,
  getAIAgents,
  getAIErrorLogs,
  getAITaskBindings,
  getAIModels,
  getCapabilities,
  getAccountFeishuWebhookSettings,
  getSignatures,
  getSystemSettings,
  getUpdateInfo,
  installUpdate,
  saveAITaskBinding,
  saveAccountFeishuWebhookSettings,
  saveSignatures,
  setDefaultAIModel,
  switchLanguage,
  testAIModel,
  testSavedAIModel,
  updateAccount,
  updateAccountPassword,
  updateAccountProfile,
  updateAIAgent,
  updateAIModel,
  updateRegistrationOpen,
  updateSystemUserRole,
  type AIAgent,
  type AIErrorLog,
  type AIModel,
  type AITaskBinding,
  type AITaskBindingInput,
  type EmailSignature,
  type SystemSettings as SystemSettingsData,
  type UpdateStatus,
  type UserRole,
} from "../../lib/api";
import { currentPushSubscription, disableWebPush, enableWebPush, supportsWebPush } from "../../lib/push";
import { cn } from "../../lib/utils";
import { htmlToPlainText, isValidRecipient, serializeEmailHTML } from "../../lib/email-format";
import { type Copy, en } from "../../lib/locale";
import type { ConnectedAccount } from "../../types";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Separator } from "../ui/separator";
import { Skeleton } from "../ui/skeleton";
import { Switch } from "../ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import { Textarea } from "../ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { RichTextButtons } from "./rich-text-buttons";
import { APIKeysSettings } from "./api-keys-settings";

export type SettingsSection = "api_keys" | "account" | "general" | "signatures" | "ai" | "agents" | "skills" | "error_logs" | "mailboxes" | "system" | "about";

export function SettingsDialog({ copy, open, onOpenChange }: { copy: Copy; open: boolean; onOpenChange: (open: boolean) => void }) {
  const [accountEditor, setAccountEditor] = useState<ConnectedAccount | null | undefined>(undefined);
  const [section, setSection] = useState<SettingsSection>("general");
  const manageAccount = (account: ConnectedAccount | null) => {
    onOpenChange(false);
    setAccountEditor(account);
  };
  const closeAccountEditor = () => {
    setAccountEditor(undefined);
    onOpenChange(true);
  };
  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent data-testid="settings-dialog" className="flex h-[calc(100dvh-2rem)] sm:h-auto sm:max-h-[calc(100dvh-3rem)] w-[calc(100%-1.5rem)] sm:w-[calc(100%-2rem)] max-w-5xl sm:max-w-5xl flex-col gap-0 overflow-hidden p-0">
          <DialogHeader className="shrink-0 border-b px-4 py-3.5 sm:px-5 sm:py-4 pr-12 text-left">
            <DialogTitle>{copy.settings}</DialogTitle>
            <DialogDescription className="sr-only">{copy.settings}</DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 sm:p-6 touch-pan-y">
            <SettingsContent copy={copy} section={section} onSectionChange={setSection} onManageAccount={manageAccount} />
          </div>
        </DialogContent>
      </Dialog>
      <AccountDialog copy={copy} open={accountEditor !== undefined} account={accountEditor || null} onOpenChange={(value) => { if (!value) closeAccountEditor(); }} />
    </>
  );
}

export function SettingsContent({ copy, section, onSectionChange, onManageAccount }: { copy: Copy; section: SettingsSection; onSectionChange: (section: SettingsSection) => void; onManageAccount: (account: ConnectedAccount | null) => void }) {
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities, retry: false });
  const isSuperAdmin = capabilities.data?.role === "super_admin";
  return (
    <div className="grid md:min-h-[32rem] md:grid-cols-[12rem_minmax(0,1fr)]">
      <nav className="flex gap-1 overflow-x-auto border-b pb-3 md:flex-col md:overflow-visible md:border-r md:border-b-0 md:pr-4 shrink-0 scrollbar-none touch-pan-x" aria-label={copy.settings}>
        <Button className="shrink-0 justify-start" variant={section === "account" ? "secondary" : "ghost"} onClick={() => onSectionChange("account")}><UserRound />{copy.accountInfo}</Button>
        <Button className="shrink-0 justify-start" variant={section === "api_keys" ? "secondary" : "ghost"} onClick={() => onSectionChange("api_keys")}><KeyRound />{copy.apiKeys}</Button>
        <Button className="shrink-0 justify-start" variant={section === "general" ? "secondary" : "ghost"} onClick={() => onSectionChange("general")}><Settings />{copy.generalSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "mailboxes" ? "secondary" : "ghost"} onClick={() => onSectionChange("mailboxes")}><Mail />{copy.mailboxManagement}</Button>
        <Button className="shrink-0 justify-start" variant={section === "signatures" ? "secondary" : "ghost"} onClick={() => onSectionChange("signatures")}><SignatureIcon />{copy.signatureSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "ai" ? "secondary" : "ghost"} onClick={() => onSectionChange("ai")}><Sparkles />{copy.aiSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "agents" ? "secondary" : "ghost"} onClick={() => onSectionChange("agents")}><Bot />{copy.agentSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "skills" ? "secondary" : "ghost"} onClick={() => onSectionChange("skills")}><BookOpen />{copy.skillSettings}</Button>
        <Button className="shrink-0 justify-start" variant={section === "error_logs" ? "secondary" : "ghost"} onClick={() => onSectionChange("error_logs")}><AlertCircle />{copy.aiErrorLogs}</Button>
        {isSuperAdmin && <Button className="shrink-0 justify-start" variant={section === "system" ? "secondary" : "ghost"} onClick={() => onSectionChange("system")}><ShieldCheck />{copy.systemSettings}</Button>}
        <Button className="shrink-0 justify-start" variant={section === "about" ? "secondary" : "ghost"} onClick={() => onSectionChange("about")}><Info />{copy.about}</Button>
      </nav>
      <div className="min-w-0 pt-5 md:pt-0 md:pl-6">
        {section === "api_keys" ? <APIKeysSettings copy={copy} /> : section === "account" ? <AccountInfoSettings copy={copy} /> : section === "general" ? <GeneralSettings copy={copy} /> : section === "signatures" ? <SignatureSettings copy={copy} /> : section === "ai" ? <AISettings copy={copy} /> : section === "agents" ? <AgentSettings copy={copy} /> : section === "skills" ? <SkillSettings copy={copy} /> : section === "error_logs" ? <AIErrorLogsSettings copy={copy} /> : section === "system" && isSuperAdmin ? <SystemSettings copy={copy} /> : section === "about" ? <AboutSettings copy={copy} isSuperAdmin={isSuperAdmin} /> : <MailboxSettings copy={copy} onManageAccount={onManageAccount} />}
      </div>
    </div>
  );
}

export function AboutSettings({ copy, isSuperAdmin }: { copy: Copy; isSuperAdmin: boolean }) {
  const queryClient = useQueryClient();
  const info = useQuery({ queryKey: ["update-info"], queryFn: getUpdateInfo, retry: false });
  const check = useMutation({
    mutationFn: checkForUpdates,
    onSuccess: (status) => {
      queryClient.setQueryData<UpdateStatus>(["update-info"], status);
      if (!status.updateAvailable) toast.success(copy.alreadyUpToDate);
    },
    onError: (value) => toast.error(value instanceof Error ? value.message : copy.loadFailed),
  });
  const install = useMutation({
    mutationFn: installUpdate,
    onSuccess: () => {
      toast.success(copy.updateRestarting);
      window.setTimeout(() => {
        const waitForRestart = window.setInterval(() => {
          void fetch("/health", { cache: "no-store" }).then((response) => {
            if (!response.ok) return;
            window.clearInterval(waitForRestart);
            window.location.reload();
          }).catch(() => undefined);
        }, 1000);
      }, 1500);
    },
    onError: (value) => toast.error(value instanceof Error ? value.message : copy.loadFailed),
  });
  const status = info.data;
  useQueryErrorToast(info.error, copy.loadFailed, "update-info-error", () => void info.refetch(), copy.retry);
  const version = status?.currentVersion || "—";
  const buttonLabel = install.isPending ? copy.updatingApplication : check.isPending ? copy.checkingForUpdates : status?.updateAvailable ? copy.updateNow : copy.checkForUpdates;
  const buttonDisabled = info.isPending || check.isPending || install.isPending || (Boolean(status?.updateAvailable) && (!isSuperAdmin || !status?.canAutoUpdate));
  const statusMessage = status?.updateAvailable
    ? `${copy.updateAvailable}: v${status.latestVersion}`
    : status && !status.canAutoUpdate ? copy.autoUpdateUnavailable : "";
  return (
    <section className="flex min-h-full flex-col">
      <div><h2 className="flex items-center gap-2 text-lg font-semibold"><Info className="size-5" />{copy.about}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.aboutDescription}</p></div>
      <div className="mt-8 border-y">
        <div className="flex flex-col gap-4 py-5 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0"><p className="text-sm text-muted-foreground">{copy.currentVersion}</p><div className="mt-1 flex flex-wrap items-center gap-2"><strong className="font-mono text-base font-semibold">v{version}</strong>{status?.updateAvailable && <Badge variant="secondary">v{status.latestVersion}</Badge>}</div>{statusMessage && <p className="mt-2 text-xs text-muted-foreground">{statusMessage}</p>}{status?.updateAvailable && !isSuperAdmin && <p className="mt-1 text-xs text-muted-foreground">{copy.adminRequiredToUpdate}</p>}{status?.updateAvailable && !status?.canAutoUpdate && <p className="mt-1 text-xs text-muted-foreground">{copy.autoUpdateUnavailable}</p>}</div>
          <Button className={cn("shrink-0", status?.updateAvailable && "bg-emerald-600 text-white hover:bg-emerald-600/90 dark:bg-emerald-600 dark:text-white dark:hover:bg-emerald-600/90")} disabled={buttonDisabled} onClick={() => status?.updateAvailable ? install.mutate() : check.mutate()}><RefreshCw className={cn((check.isPending || install.isPending) && "animate-spin")} />{buttonLabel}</Button>
        </div>
      </div>
      <div className="mt-auto pt-10"><p className="text-xs font-medium text-muted-foreground">{copy.githubRepository}</p><a className="mt-2 inline-flex min-w-0 items-center gap-2 text-sm font-medium hover:underline" href={status?.repositoryUrl || "https://github.com/voidvon/inbrix"} target="_blank" rel="noreferrer"><span className="truncate">github.com/voidvon/inbrix</span><ExternalLink className="size-3.5 shrink-0" /></a></div>
    </section>
  );
}

export function SystemSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const settings = useQuery({ queryKey: ["system-settings"], queryFn: getSystemSettings, retry: false });
  const updateRegistration = useMutation({
    mutationFn: updateRegistrationOpen,
    onSuccess: (updated) => {
      queryClient.setQueryData<SystemSettingsData>(["system-settings"], (current) => current ? { ...current, registrationOpen: updated.registrationOpen } : current);
      queryClient.setQueryData(["public-settings"], updated);
      toast.success(copy.registrationSettingUpdated);
    },
    onError: (value) => toast.error(value instanceof Error ? value.message : copy.loadFailed),
  });
  const updateRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: UserRole }) => updateSystemUserRole(id, role),
    onSuccess: (updated) => {
      queryClient.setQueryData<SystemSettingsData>(["system-settings"], (current) => current ? {
        ...current,
        users: current.users.map((user) => user.id === updated.id ? updated : user),
      } : current);
      toast.success(copy.roleUpdated);
    },
    onError: (value) => toast.error(value instanceof Error ? value.message : copy.loadFailed),
  });
  useQueryErrorToast(settings.error, copy.loadFailed, "system-settings-error", () => void settings.refetch(), copy.retry);
  return (
    <section className="min-w-0">
      <h2 className="text-lg font-semibold">{copy.systemSettings}</h2>
      <p className="mt-1 text-sm text-muted-foreground">{copy.systemSettingsDescription}</p>
      {settings.isPending && <div className="mt-6 grid gap-3"><Skeleton className="h-16 w-full" /><Skeleton className="h-40 w-full" /></div>}
      {settings.data && <>
        <div className="mt-6 flex items-center justify-between border-y py-4 text-sm"><span className="text-muted-foreground">{copy.systemVersion}</span><strong className="font-mono font-medium">{settings.data.version}</strong></div>
        <div className="flex items-center justify-between gap-6 border-b py-4">
          <div><Label htmlFor="open-registration">{copy.openRegistration}</Label><p className="mt-1 text-xs text-muted-foreground">{copy.openRegistrationDescription}</p></div>
          <Switch id="open-registration" checked={settings.data.registrationOpen} disabled={updateRegistration.isPending} onCheckedChange={(checked) => updateRegistration.mutate(checked)} />
        </div>
        <div className="mt-6">
          <h3 className="text-sm font-semibold">{copy.userManagement}</h3>
          <div className="mt-3 overflow-hidden rounded-lg border">
            <Table className="min-w-[38rem] table-fixed">
              <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[44%] px-4">{copy.account}</TableHead><TableHead className="w-[28%] px-4">{copy.role}</TableHead><TableHead className="w-[28%] px-4">{copy.actions}</TableHead></TableRow></TableHeader>
              <TableBody>
                {settings.data.users.map((user) => {
                  const isCurrent = user.id === settings.data.currentUserId;
                  return <TableRow key={user.id}>
                    <TableCell className="px-4 py-3"><strong className="block truncate font-medium">{user.displayName || user.login}</strong><span className="block truncate text-xs text-muted-foreground">{user.login}</span></TableCell>
                    <TableCell className="px-4 py-3"><Badge variant={user.role === "super_admin" ? "default" : "secondary"}>{user.role === "super_admin" ? copy.superAdmin : copy.ordinaryUser}</Badge></TableCell>
                    <TableCell className="px-4 py-3">
                      {isCurrent ? <span className="text-xs text-muted-foreground">{copy.currentUser}</span> : <Select value={user.role} disabled={updateRole.isPending} onValueChange={(role) => updateRole.mutate({ id: user.id, role: role as UserRole })}><SelectTrigger className="w-full" aria-label={`${user.login} ${copy.role}`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="user">{copy.ordinaryUser}</SelectItem><SelectItem value="super_admin">{copy.superAdmin}</SelectItem></SelectContent></Select>}
                    </TableCell>
                  </TableRow>;
                })}
                {!settings.data.users.length && <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.noUsers}</TableCell></TableRow>}
              </TableBody>
            </Table>
          </div>
        </div>
      </>}
    </section>
  );
}

export function SignatureSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const signatures = useQuery({ queryKey: ["signatures"], queryFn: getSignatures, retry: false });
  useQueryErrorToast(signatures.error, copy.loadFailed, "signatures-error", () => void signatures.refetch(), copy.retry);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<EmailSignature | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<EmailSignature | null>(null);
  const [name, setName] = useState("");
  const [isDefault, setIsDefault] = useState(false);
  const [sourceMode, setSourceMode] = useState(false);
  const [sourceCode, setSourceCode] = useState("");
  const [invalid, setInvalid] = useState<{ name?: boolean; content?: boolean }>({});
  const editor = useEditor({ extensions: [StarterKit.configure({ paragraph: false }), EmailParagraph, EmailImage, UnderlineExtension, LinkExtension.configure({ openOnClick: false }), Placeholder.configure({ placeholder: copy.signatureContent })], content: "", immediatelyRender: false, onUpdate: () => { setInvalid((current) => current.content ? { ...current, content: false } : current); } });
  const persist = useMutation({
    mutationFn: ({ items }: { items: EmailSignature[]; operation: "create" | "update" | "delete" }) => saveSignatures(items),
    onSuccess: async (_, variables) => {
      setOpen(false);
      setEditing(null);
      setDeleteTarget(null);
      setInvalid({});
      toast.success(variables.operation === "create" ? copy.signatureSaved : variables.operation === "update" ? copy.signatureUpdated : copy.signatureDeleted);
      await queryClient.invalidateQueries({ queryKey: ["signatures"] });
    },
    onError: (value) => toastError(value, copy.loadFailed),
  });

  useEffect(() => {
    if (!open || !editor) return;
    const html = editing?.html || "";
    setSourceMode(false);
    setSourceCode(html);
    editor.commands.setContent(html);
    editor.commands.focus("start");
  }, [open, editing, editor]);

  const toggleSignatureSource = () => {
    if (!editor) return;
    if (sourceMode) {
      editor.commands.setContent(sourceCode);
      setSourceMode(false);
      editor.commands.focus("start");
      return;
    }
    setSourceCode(serializeEmailHTML(editor.getHTML()));
    setSourceMode(true);
  };

  const openAdd = () => {
    setEditing(null);
    setName("");
    setIsDefault(!signatures.data?.signatures.length);
    setInvalid({});
    setOpen(true);
  };
  const openEdit = (signature: EmailSignature) => {
    setEditing(signature);
    setName(signature.name);
    setIsDefault(Boolean(signature.default));
    setInvalid({});
    setOpen(true);
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const html = serializeEmailHTML(sourceMode ? sourceCode : editor?.getHTML() || "");
    const missingName = !name.trim();
    const missingContent = sourceMode ? !sourceCode.trim() : !editor || editor.isEmpty;
    if (missingName || missingContent) {
      setInvalid({ name: missingName, content: missingContent });
      toast.error(missingName && missingContent ? copy.requiredFieldsMissing : missingName ? copy.signatureNameRequired : copy.signatureContentRequired);
      return;
    }
    setInvalid({});
    const current = signatures.data?.signatures || [];
    const makeDefault = isDefault || current.length === 0;
    const nextItem: EmailSignature = { id: editing?.id || "", name: name.trim(), html, default: makeDefault };
    const next = editing
      ? current.map((item) => item.id === editing.id ? nextItem : { ...item, default: makeDefault ? false : item.default })
      : [...current.map((item) => ({ ...item, default: makeDefault ? false : item.default })), nextItem];
    persist.mutate({ items: next, operation: editing ? "update" : "create" });
  };
  const removeSignature = () => {
    if (!deleteTarget) return;
    const remaining = (signatures.data?.signatures || []).filter((item) => item.id !== deleteTarget.id);
    const next = remaining.length && !remaining.some((item) => item.default)
      ? remaining.map((item, index) => ({ ...item, default: index === 0 }))
      : remaining;
    persist.mutate({ items: next, operation: "delete" });
  };

  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="flex items-center gap-2 text-lg font-semibold"><SignatureIcon className="size-5" />{copy.signatureSettings}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.signatureSettingsDescription}</p></div>
        <Button onClick={openAdd}><Plus />{copy.addSignature}</Button>
      </div>
      <div className="mt-5 overflow-hidden rounded-lg border">
        <Table className="min-w-[36rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[28%] px-4">{copy.signatureName}</TableHead><TableHead className="px-4">{copy.signaturePreview}</TableHead><TableHead className="w-24 px-4 text-right">{copy.actions}</TableHead></TableRow></TableHeader>
          <TableBody>{signatures.isPending ? <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.loading}</TableCell></TableRow> : signatures.data?.signatures.length ? signatures.data.signatures.map((signature) => <TableRow key={signature.id}><TableCell className="px-4 py-3"><div className="flex items-center gap-2"><span className="truncate font-medium">{signature.name}</span>{signature.default && <Badge>{copy.defaultSignature}</Badge>}</div></TableCell><TableCell className="px-4 py-3 text-muted-foreground whitespace-normal"><p className="line-clamp-2 whitespace-pre-line">{htmlToPlainText(signature.html) || "-"}</p></TableCell><TableCell className="px-4 py-3"><div className="flex justify-end"><Button variant="ghost" size="icon" onClick={() => openEdit(signature)} aria-label={copy.editSignature} title={copy.editSignature}><Pencil /></Button><Button variant="ghost" size="icon" className="text-destructive" onClick={() => setDeleteTarget(signature)} aria-label={copy.remove} title={copy.remove}><Trash2 /></Button></div></TableCell></TableRow>) : signatures.isError ? null : <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.noSignatures}</TableCell></TableRow>}</TableBody>
        </Table>
      </div>
      <Dialog open={open} onOpenChange={(next) => { if (!persist.isPending) setOpen(next); }}>
        <DialogContent className="flex h-[calc(100dvh-2rem)] sm:h-auto sm:max-h-[calc(100dvh-3rem)] w-[calc(100%-1.5rem)] sm:w-auto flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
          <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12"><DialogTitle>{editing ? copy.editSignature : copy.addSignature}</DialogTitle><DialogDescription>{copy.signatureSettingsDescription}</DialogDescription></DialogHeader>
          <form className="flex min-h-0 flex-1 flex-col" noValidate onSubmit={submit}>
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 sm:p-5 grid gap-4 touch-pan-y">
              <div className="grid gap-2"><Label htmlFor="signature-name">{copy.signatureName}</Label><Input id="signature-name" value={name} aria-invalid={invalid.name} onChange={(event) => { setName(event.target.value); if (invalid.name) setInvalid((current) => ({ ...current, name: false })); }} disabled={persist.isPending} required /></div>
              <div className="grid gap-2">
                <Label>{copy.signatureContent}</Label>
                <div className={cn("overflow-hidden rounded-lg border bg-background", invalid.content && "border-destructive ring-3 ring-destructive/20")}>
                  <div className="flex items-center gap-1 border-b bg-muted px-2 py-1"><Button type="button" variant={sourceMode ? "secondary" : "ghost"} size="icon" disabled={persist.isPending || !editor} onClick={toggleSignatureSource} aria-label={sourceMode ? copy.richText : copy.sourceCode} title={sourceMode ? copy.richText : copy.sourceCode}><Code2 /></Button><Separator orientation="vertical" className="mx-1 h-5" /><RichTextButtons editor={editor} disabled={persist.isPending || sourceMode} /></div>
                  {sourceMode
                    ? <textarea className="min-h-64 w-full resize-y bg-background px-4 py-3 font-mono text-sm leading-6 outline-none" value={sourceCode} onChange={(event) => { setSourceCode(event.target.value); if (invalid.content) setInvalid((current) => ({ ...current, content: false })); }} spellCheck={false} aria-label={copy.sourceCode} aria-invalid={invalid.content} />
                    : <div className="max-h-72 overflow-y-auto px-4 py-3"><EditorContent editor={editor} /></div>}
                </div>
              </div>
              <label className="flex items-center gap-2 text-sm"><input className="size-4 accent-primary" type="checkbox" checked={isDefault} disabled={persist.isPending} onChange={(event) => setIsDefault(event.target.checked)} />{copy.defaultSignature}</label>
            </div>
            <DialogFooter className="shrink-0 border-t px-5 py-3"><Button type="button" variant="ghost" disabled={persist.isPending} onClick={() => setOpen(false)}>{copy.cancel}</Button><Button type="submit" disabled={persist.isPending}>{persist.isPending ? copy.savingSignature : copy.save}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(next) => { if (!next && !persist.isPending) setDeleteTarget(null); }}>
        <DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>{copy.deleteSignatureTitle}</DialogTitle><DialogDescription>{copy.deleteSignatureDescription}</DialogDescription></DialogHeader><DialogFooter><Button type="button" variant="ghost" disabled={persist.isPending} onClick={() => setDeleteTarget(null)}>{copy.cancel}</Button><Button type="button" variant="destructive" disabled={persist.isPending} onClick={removeSignature}><Trash2 />{persist.isPending ? copy.deleting : copy.remove}</Button></DialogFooter></DialogContent>
      </Dialog>
    </section>
  );
}

export function splitOutputLabels(value: string) {
  return value.split(/[,，;；、\n]+/).map((item) => item.trim()).filter(Boolean);
}

export function validOutputLabel(value: string) {
  return Boolean(value.trim()) && Array.from(value.trim()).length <= 20 && !/[\r\n：:]/.test(value);
}

export function AgentOutputLabelInput({ copy, labels, onChange, draftRef, disabled }: { copy: Copy; labels: string[]; onChange: (labels: string[]) => void; draftRef: MutableRefObject<string>; disabled: boolean }) {
  const [draft, setDraft] = useState("");
  const [invalid, setInvalid] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const addValues = (values: string[]) => {
    const candidates = values.map((value) => value.trim()).filter(Boolean);
    if (!candidates.length) return true;
    if (labels.length + candidates.length > 12 || candidates.some((value) => !validOutputLabel(value))) {
      setInvalid(true);
      toast.error(copy.invalidAgentOutputLabel);
      return false;
    }
    const existing = new Set(labels);
    const next = [...labels];
    for (const value of candidates) {
      if (!existing.has(value)) {
        existing.add(value);
        next.push(value);
      }
    }
    if (next.length > 12) {
      setInvalid(true);
      toast.error(copy.invalidAgentOutputLabel);
      return false;
    }
    onChange(next);
    setDraft("");
    draftRef.current = "";
    setInvalid(false);
    return true;
  };
  return (
    <div>
      <div className={cn("flex min-h-9 flex-wrap items-center gap-1.5 rounded-lg border border-input bg-transparent px-2 py-1 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50", invalid && "border-destructive focus-within:border-destructive focus-within:ring-destructive/20", disabled && "cursor-not-allowed opacity-50")} onClick={() => inputRef.current?.focus()}>
        {labels.map((label, index) => <span className="flex max-w-full items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-sm" key={`${label}-${index}`}><span className="truncate" title={label}>{label}</span><button type="button" className="grid size-4 shrink-0 place-items-center rounded-sm text-muted-foreground hover:bg-background hover:text-foreground" disabled={disabled} onClick={(event) => { event.stopPropagation(); onChange(labels.filter((_, itemIndex) => itemIndex !== index)); }} aria-label={`${copy.removeAgentOutputLabel}: ${label}`} title={copy.removeAgentOutputLabel}><X className="size-3" /></button></span>)}
        <input ref={inputRef} className="h-6 min-w-40 flex-1 bg-transparent px-0.5 text-sm outline-none placeholder:text-muted-foreground" value={draft} disabled={disabled || labels.length >= 12} onChange={(event) => { setDraft(event.target.value); draftRef.current = event.target.value; if (invalid) setInvalid(false); }} onKeyDown={(event) => {
          if (["Enter", ",", "，", ";", "；", "Tab"].includes(event.key) && draft.trim()) {
            event.preventDefault();
            addValues(splitOutputLabels(draft));
          } else if (event.key === "Backspace" && !draft && labels.length) {
            onChange(labels.slice(0, -1));
          }
        }} onPaste={(event) => {
          const value = event.clipboardData.getData("text");
          if (!/[,，;；、\n]/.test(value)) return;
          event.preventDefault();
          addValues(splitOutputLabels(value));
        }} onBlur={() => { if (draft.trim()) addValues(splitOutputLabels(draft)); }} placeholder={labels.length ? "" : copy.agentOutputLabelsPlaceholder} aria-invalid={invalid} aria-label={copy.agentOutputLabels} />
      </div>
    </div>
  );
}

export function AgentSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const agents = useQuery({ queryKey: ["ai-agents"], queryFn: getAIAgents, retry: false });
  useQueryErrorToast(agents.error, copy.loadFailed, "ai-agents-error", () => void agents.refetch(), copy.retry);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<AIAgent | null>(null);
  const [businessProfile, setBusinessProfile] = useState<AIBusinessProfile>(emptyBusinessProfile);
  const [name, setName] = useState("");
  const [prompt, setPrompt] = useState("");
  const [outputLabels, setOutputLabels] = useState<string[]>(["客户", "需求", "要求", "问题"]);
  const outputLabelDraftRef = useRef("");
  const [invalid, setInvalid] = useState<{ name?: boolean; prompt?: boolean }>({});
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["ai-agents"] });
  const create = useMutation({
    mutationFn: addAIAgent,
    onSuccess: () => { setOpen(false); setEditing(null); setInvalid({}); toast.success(copy.agentSaved); refresh(); },
    onError: (value) => toastError(value, copy.loadFailed),
  });
  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: Parameters<typeof updateAIAgent>[1] }) => updateAIAgent(id, input),
    onSuccess: () => { setOpen(false); setEditing(null); setInvalid({}); toast.success(copy.agentUpdated); refresh(); },
    onError: (value) => toastError(value, copy.loadFailed),
  });
  const openAdd = () => { setBusinessProfile(emptyBusinessProfile); setEditing(null); setName(""); setPrompt(""); setOutputLabels(["客户", "需求", "要求", "问题"]); outputLabelDraftRef.current = ""; setInvalid({}); setOpen(true); };
  const openEdit = (agent: AIAgent) => { setBusinessProfile(agent.businessProfile); setEditing(agent); setName(agent.name); setPrompt(agent.prompt); setOutputLabels(agent.outputLabels); outputLabelDraftRef.current = ""; setInvalid({}); setOpen(true); };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const pendingLabels = splitOutputLabels(outputLabelDraftRef.current);
    if (pendingLabels.some((label) => !validOutputLabel(label))) {
      toast.error(copy.invalidAgentOutputLabel);
      return;
    }
    const submittedLabels = [...outputLabels];
    for (const label of pendingLabels) if (!submittedLabels.includes(label)) submittedLabels.push(label);
    if (submittedLabels.length > 12) {
      toast.error(copy.invalidAgentOutputLabel);
      return;
    }
    const missingName = !name.trim();
    const missingPrompt = !prompt.trim();
    if (missingName || missingPrompt) {
      setInvalid({ name: missingName, prompt: missingPrompt });
      toast.error(missingName && missingPrompt ? copy.requiredFieldsMissing : missingName ? copy.agentNameRequired : copy.agentPromptRequired);
      return;
    }
    setInvalid({});
    const input = { name: name.trim(), prompt: prompt.trim(), outputLabels: submittedLabels, businessProfile };
    if (editing) update.mutate({ id: editing.id, input });
    else create.mutate(input);
  };
  const pending = create.isPending || update.isPending;
  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="flex items-center gap-2 text-lg font-semibold"><Bot className="size-5" />{copy.agentSettings}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.agentSettingsDescription}</p></div>
        <Button onClick={openAdd}><Plus />{copy.addAgent}</Button>
      </div>
      <div className="mt-5 overflow-hidden rounded-lg border">
        <Table className="min-w-[36rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[28%] px-4">{copy.agentName}</TableHead><TableHead className="px-4">{copy.agentPrompt}</TableHead><TableHead className="w-20 px-4 text-right">{copy.actions}</TableHead></TableRow></TableHeader>
          <TableBody>{agents.isPending ? <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.loading}</TableCell></TableRow> : agents.data?.agents.length ? agents.data.agents.map((agent) => <TableRow key={agent.id}><TableCell className="px-4 py-3 font-medium"><span className="block truncate">{agent.name}</span><span className="mt-1 block truncate text-xs text-muted-foreground">{agent.businessProfile.company}</span></TableCell><TableCell className="px-4 py-3 text-muted-foreground whitespace-normal"><p className="line-clamp-2 whitespace-pre-line">{agent.prompt}</p><p className="mt-1 truncate text-xs" title={agent.outputLabels.join(" · ")}>{agent.outputLabels.join(" · ")}</p></TableCell><TableCell className="px-4 py-3"><div className="flex justify-end"><Button variant="ghost" size="icon" onClick={() => openEdit(agent)} aria-label={copy.editAgent} title={copy.editAgent}><Pencil /></Button></div></TableCell></TableRow>) : agents.isError ? null : <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={3}>{copy.noAgents}</TableCell></TableRow>}</TableBody>
        </Table>
      </div>
      <Dialog open={open} onOpenChange={(next) => { setOpen(next); if (!next) setEditing(null); }}>
        <DialogContent className="flex max-h-[calc(100dvh-2rem)] w-[calc(100%-1.5rem)] sm:w-auto sm:max-w-xl flex-col gap-0 overflow-hidden p-0">
          <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12"><DialogTitle>{editing ? copy.editAgent : copy.addAgent}</DialogTitle><DialogDescription>{copy.agentSettingsDescription}</DialogDescription></DialogHeader>
          <form className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 sm:p-5 grid gap-4 touch-pan-y" noValidate onSubmit={submit}>
            <div className="grid gap-2"><Label htmlFor="agent-name">{copy.agentName}</Label><Input id="agent-name" value={name} aria-invalid={invalid.name} onChange={(event) => { setName(event.target.value); if (invalid.name) setInvalid((current) => ({ ...current, name: false })); }} disabled={pending} required /></div>
            <AIBusinessProfileFields copy={copy} value={businessProfile} onChange={setBusinessProfile} disabled={pending} />
            <div className="grid gap-2"><div><Label>{copy.agentOutputLabels}</Label><p className="mt-1 text-xs text-muted-foreground">{copy.agentOutputLabelsDescription}</p></div><AgentOutputLabelInput copy={copy} labels={outputLabels} onChange={setOutputLabels} draftRef={outputLabelDraftRef} disabled={pending} /></div>
            <div className="grid gap-2"><Label htmlFor="agent-prompt">{copy.agentPrompt}</Label><Textarea id="agent-prompt" className="min-h-36 sm:min-h-48 resize-y" value={prompt} aria-invalid={invalid.prompt} onChange={(event) => { setPrompt(event.target.value); if (invalid.prompt) setInvalid((current) => ({ ...current, prompt: false })); }} disabled={pending} required /></div>
            <DialogFooter className="pt-2"><Button type="button" variant="ghost" onClick={() => setOpen(false)}>{copy.cancel}</Button><Button type="submit" disabled={pending}>{pending ? copy.savingAgent : editing ? copy.editAgent : copy.addAgent}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export function AIErrorLogsSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const errorLogs = useQuery({
    queryKey: ["ai-error-logs"],
    queryFn: () => getAIErrorLogs(100),
    retry: false,
  });
  useQueryErrorToast(errorLogs.error, copy.loadFailed, "ai-error-logs-error", () => void errorLogs.refetch(), copy.retry);
  const [detailLog, setDetailLog] = useState<AIErrorLog | null>(null);

  const clearMutation = useMutation({
    mutationFn: clearAIErrorLogs,
    onSuccess: () => {
      toast.success(copy.errorLogsCleared);
      void queryClient.invalidateQueries({ queryKey: ["ai-error-logs"] });
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : copy.loadFailed);
    },
  });

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["ai-error-logs"] });
  };

  const formatTime = (timeStr: string) => {
    if (!timeStr) return "-";
    try {
      const d = new Date(timeStr);
      const pad = (n: number) => String(n).padStart(2, "0");
      return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
    } catch {
      return timeStr;
    }
  };

  const taskBadge = (task: string) => {
    switch (task) {
      case "mail_summary":
        return <Badge variant="outline" className="border-blue-500/30 bg-blue-500/10 text-blue-600 dark:text-blue-400">{copy.mailSummaryAgent || copy.mailSummaryTitle || "邮件总结"}</Badge>;
      case "email_draft":
        return <Badge variant="outline" className="border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">{copy.emailDraftAgent || "邮件撰写"}</Badge>;
      case "reply_suggestion":
        return <Badge variant="outline" className="border-purple-500/30 bg-purple-500/10 text-purple-600 dark:text-purple-400">{copy.replySuggestionAgent || "建议回复"}</Badge>;
      case "document_generation":
        return <Badge variant="outline" className="border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400">文档生成</Badge>;
      case "model_test":
        return <Badge variant="outline" className="border-muted-foreground/30 bg-muted/40 text-muted-foreground">模型测试</Badge>;
      default:
        return <Badge variant="outline">{task || "-"}</Badge>;
    }
  };

  const logs = errorLogs.data?.logs || [];

  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 text-lg font-semibold">
            <AlertCircle className="size-5 text-destructive" />
            {copy.aiErrorLogs}
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">{copy.aiErrorLogsDescription}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={refresh}
            disabled={errorLogs.isFetching}
            className="h-8 gap-1.5 text-xs"
          >
            <RefreshCw className={cn("size-3.5", errorLogs.isFetching && "animate-spin")} />
            {copy.refreshLogs || "刷新"}
          </Button>
          {logs.length > 0 && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                if (window.confirm(copy.clearErrorLogsConfirm)) {
                  clearMutation.mutate();
                }
              }}
              disabled={clearMutation.isPending}
              className="h-8 gap-1.5 text-xs text-destructive hover:bg-destructive/10"
            >
              <Trash2 className="size-3.5" />
              {copy.clearErrorLogs}
            </Button>
          )}
        </div>
      </div>

      <div className="mt-4 overflow-hidden rounded-lg border">
        <Table className="min-w-[42rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground">
            <TableRow className="hover:bg-transparent">
              <TableHead className="w-[20%] px-4">{copy.errorLogTime}</TableHead>
              <TableHead className="w-[18%] px-4">{copy.errorLogTask}</TableHead>
              <TableHead className="w-[22%] px-4">{copy.errorLogAccount}</TableHead>
              <TableHead className="w-[18%] px-4">{copy.errorLogModel} / {copy.errorLogAgent}</TableHead>
              <TableHead className="px-4">{copy.errorLogMessage}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {errorLogs.isPending ? (
              <TableRow>
                <TableCell className="h-24 text-center text-muted-foreground" colSpan={5}>
                  {copy.loading}
                </TableCell>
              </TableRow>
            ) : logs.length > 0 ? (
              logs.map((log) => (
                <TableRow
                  key={log.id}
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={() => setDetailLog(log)}
                >
                  <TableCell className="px-4 py-2.5 text-xs font-mono text-muted-foreground">
                    {formatTime(log.createdAt)}
                  </TableCell>
                  <TableCell className="px-4 py-2.5">
                    {taskBadge(log.taskType)}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 text-xs font-mono truncate text-muted-foreground">
                    {log.accountEmail || "-"}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 text-xs text-muted-foreground truncate">
                    {log.modelName || log.agentName ? (
                      <span title={`${log.modelName} ${log.agentName ? `(${log.agentName})` : ""}`}>
                        {log.modelName || log.agentName}
                      </span>
                    ) : (
                      "-"
                    )}
                  </TableCell>
                  <TableCell className="px-4 py-2.5 text-xs text-destructive">
                    <p className="line-clamp-1 break-all font-mono" title={log.errorMessage}>
                      {log.errorMessage}
                    </p>
                  </TableCell>
                </TableRow>
              ))
            ) : errorLogs.isError ? null : (
              <TableRow>
                <TableCell className="h-24 text-center text-muted-foreground" colSpan={5}>
                  {copy.noErrorLogs}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <Dialog open={Boolean(detailLog)} onOpenChange={(next) => !next && setDetailLog(null)}>
        <DialogContent className="flex max-h-[calc(100dvh-2rem)] w-[calc(100%-1.5rem)] sm:w-auto sm:max-w-lg flex-col gap-0 overflow-hidden p-0">
          <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12">
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <AlertCircle className="size-5" />
              {copy.errorLogDetails}
            </DialogTitle>
            <DialogDescription>
              {detailLog ? `${formatTime(detailLog.createdAt)} · ${detailLog.taskType}` : ""}
            </DialogDescription>
          </DialogHeader>
          {detailLog && (
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 sm:p-5 grid gap-3 text-sm touch-pan-y">
              <div className="grid grid-cols-3 gap-2 rounded-md bg-muted/50 p-3 text-xs">
                <div>
                  <span className="text-muted-foreground block">{copy.errorLogTask}</span>
                  <span className="font-medium">{detailLog.taskType}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block">{copy.errorLogAccount}</span>
                  <span className="font-medium truncate block" title={detailLog.accountEmail}>{detailLog.accountEmail || "-"}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block">{copy.errorLogModel}</span>
                  <span className="font-medium truncate block" title={detailLog.modelName}>{detailLog.modelName || "-"}</span>
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label className="text-xs text-muted-foreground">{copy.errorLogMessage}</Label>
                <div className="max-h-60 overflow-y-auto rounded-md border bg-muted/30 p-3 font-mono text-xs text-destructive whitespace-pre-wrap break-all select-text">
                  {detailLog.errorMessage}
                </div>
              </div>
            </div>
          )}
          <DialogFooter className="shrink-0 border-t px-5 py-3">
            <Button variant="ghost" onClick={() => setDetailLog(null)}>
              {copy.cancel}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

// The UI presents the wire protocol ("API format") rather than vendor-specific
// providers. These helpers map it to the protocol keys stored in the DB.
type APIFormat = "chat_completions" | "responses" | "gemini";

function apiFormatForProvider(provider: AIModel["provider"]): APIFormat {
  if (provider === "openai") return "responses";
  if (provider === "gemini") return "gemini";
  return "chat_completions";
}

function providerForAPIFormat(format: APIFormat): AIModel["provider"] {
  if (format === "responses") return "openai";
  if (format === "gemini") return "gemini";
  return "openai-compatible";
}

function apiFormatLabel(provider: AIModel["provider"]): string {
  if (provider === "openai") return "Responses";
  if (provider === "gemini") return "Gemini";
  return "Chat Completions";
}

export function AISettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const models = useQuery({ queryKey: ["ai-models"], queryFn: getAIModels, retry: false });
  useQueryErrorToast(models.error, copy.loadFailed, "ai-models-error", () => void models.refetch(), copy.retry);
  const [addOpen, setAddOpen] = useState(false);
  const [editingModel, setEditingModel] = useState<AIModel | null>(null);
  const [apiFormat, setAPIFormat] = useState<APIFormat>("chat_completions");
  const [baseURL, setBaseURL] = useState("https://api.openai.com/v1");
  const [model, setModel] = useState("gpt-5.6-sol");
  const [apiKey, setAPIKey] = useState("");
  const [reasoningEffort, setReasoningEffort] = useState<"low" | "medium" | "high">("medium");
  const [extraParams, setExtraParams] = useState("");
  const [supportsImages, setSupportsImages] = useState(true);
  const [invalid, setInvalid] = useState<{ baseUrl?: boolean; model?: boolean; apiKey?: boolean; extraParams?: boolean }>({});
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["ai-models"] });
  };
  const handleAPIFormatChange = (nextFormat: APIFormat) => {
    setAPIFormat(nextFormat);
    if (nextFormat === "gemini") {
      if (baseURL === "https://api.openai.com/v1" || !baseURL) {
        setBaseURL("https://generativelanguage.googleapis.com");
      }
      if (model === "gpt-5.6-sol" || !model) {
        setModel("gemini-3.8-flash");
      }
      return;
    }
    if (nextFormat === "responses") {
      if (baseURL === "https://generativelanguage.googleapis.com" || !baseURL) {
        setBaseURL("https://api.openai.com/v1");
      }
      if (model === "gemini-3.8-flash" || !model) {
        setModel("gpt-5.6-sol");
      }
      return;
    }
    // Chat Completions is the generic OpenAI-compatible mode. Keep custom
    // gateway values as-is; only replace Google's native presets.
    if (baseURL === "https://generativelanguage.googleapis.com") {
      setBaseURL("https://api.openai.com/v1");
    }
    if (model === "gemini-3.8-flash") {
      setModel("gpt-5.6-sol");
    }
  };
  const add = useMutation({
    mutationFn: addAIModel,
    onSuccess: () => {
      setAPIKey("");
      setAPIFormat("chat_completions");
      setModel("gpt-5.6-sol");
      setBaseURL("https://api.openai.com/v1");
      setReasoningEffort("medium");
      setAddOpen(false);
      setInvalid({});
      toast.success(copy.aiSettingsSaved);
      refresh();
    },
    onError: (value) => toastError(value, copy.loadFailed),
  });
  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: Parameters<typeof updateAIModel>[1] }) => updateAIModel(id, input),
    onSuccess: () => {
      setAPIKey("");
      setEditingModel(null);
      setAddOpen(false);
      setInvalid({});
      toast.success(copy.aiModelUpdated);
      refresh();
    },
    onError: (value) => toastError(value, copy.loadFailed),
  });
  const remove = useMutation({ mutationFn: deleteAIModel, onSuccess: () => refresh(), onError: (value) => toastError(value, copy.loadFailed) });
  const makeDefault = useMutation({ mutationFn: setDefaultAIModel, onSuccess: () => refresh(), onError: (value) => toastError(value, copy.loadFailed) });
  const test = useMutation({
    mutationFn: ({ input, id }: { input: Parameters<typeof testAIModel>[0]; id?: string }) => id ? testSavedAIModel(id, input) : testAIModel(input),
    onSuccess: (result) => {
      toast.success(copy.aiModelTestSuccess, { description: `${result.latencyMs} ms · ${result.output}` });
    },
    onError: (value) => toastError(value, copy.loadFailed),
  });
  const validate = () => {
    const next: { baseUrl?: boolean; model?: boolean; apiKey?: boolean; extraParams?: boolean } = {};
    const messages: string[] = [];
    if (!baseURL.trim()) { next.baseUrl = true; messages.push(copy.aiBaseURLRequired); }
    if (!model.trim()) { next.model = true; messages.push(copy.aiModelNameRequired); }
    if (!editingModel && !apiKey.trim()) { next.apiKey = true; messages.push(copy.aiAPIKeyRequired); }
    if (extraParams.trim()) {
      try {
        const parsed: unknown = JSON.parse(extraParams);
        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("not an object");
      } catch {
        next.extraParams = true;
        messages.push(copy.aiExtraParamsInvalid);
      }
    }
    setInvalid(next);
    if (messages.length) toast.error(messages.length === 1 ? messages[0] : copy.requiredFieldsMissing);
    return messages.length === 0;
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!validate()) return;
    const input = { provider: providerForAPIFormat(apiFormat), baseUrl: baseURL.trim(), model: model.trim(), apiKey: apiKey.trim(), reasoningEffort, extraParams: extraParams.trim() || "{}", supportsImages };
    if (editingModel) update.mutate({ id: editingModel.id, input });
    else add.mutate(input);
  };
  const openAdd = () => {
    setEditingModel(null);
    setAPIFormat("chat_completions");
    setBaseURL("https://api.openai.com/v1");
    setModel("gpt-5.6-sol");
    setReasoningEffort("medium");
    setExtraParams("");
    setSupportsImages(true);
    setAPIKey("");
    setInvalid({});
    setAddOpen(true);
  };
  const openEdit = (item: AIModel) => {
    setEditingModel(item);
    setAPIFormat(apiFormatForProvider(item.provider));
    setBaseURL(item.baseUrl);
    setModel(item.model);
    setReasoningEffort(item.reasoningEffort);
    setExtraParams(item.extraParams && item.extraParams !== "{}" ? item.extraParams : "");
    setSupportsImages(item.supportsImages);
    setAPIKey("");
    setInvalid({});
    setAddOpen(true);
  };
  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="flex items-center gap-2 text-lg font-semibold"><Sparkles className="size-5" />{copy.aiSettings}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.aiSettingsDescription}</p></div>
        <Button onClick={openAdd}><Plus />{copy.addAIModel}</Button>
      </div>
      <div className="mt-5 overflow-hidden rounded-lg border">
        <Table className="min-w-[46rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[19%] px-4">{copy.aiModel}</TableHead><TableHead className="w-[16%] px-4">{copy.aiAPIFormat}</TableHead><TableHead className="w-[12%] px-4">{copy.aiReasoningEffort}</TableHead><TableHead className="px-4">{copy.aiBaseURL}</TableHead><TableHead className="w-52 px-4 text-right">{copy.actions}</TableHead></TableRow></TableHeader>
          <TableBody>{models.isPending ? <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={5}>{copy.loading}</TableCell></TableRow> : models.data?.models.length ? models.data.models.map((item) => <TableRow key={item.id}><TableCell className="px-4 py-3"><div className="flex min-w-0 items-center gap-2"><span className="truncate font-medium">{item.model}</span>{item.isDefault && <Badge>{copy.defaultModel}</Badge>}</div></TableCell><TableCell className="px-4 py-3">{apiFormatLabel(item.provider)}</TableCell><TableCell className="px-4 py-3">{apiFormatForProvider(item.provider) === "chat_completions" ? "—" : item.reasoningEffort === "low" ? copy.aiReasoningLow : item.reasoningEffort === "medium" ? copy.aiReasoningMedium : copy.aiReasoningHigh}</TableCell><TableCell className="px-4 py-3 text-muted-foreground"><span className="block truncate" title={item.baseUrl}>{item.baseUrl}</span></TableCell><TableCell className="px-4 py-3"><div className="flex justify-end gap-1">{!item.isDefault && <Button variant="outline" size="sm" disabled={makeDefault.isPending} onClick={() => makeDefault.mutate(item.id)}>{copy.setDefaultModel}</Button>}<Button variant="ghost" size="icon" onClick={() => openEdit(item)} aria-label={copy.editAIModel} title={copy.editAIModel}><Pencil /></Button><Button variant="ghost" size="icon" className="text-destructive" disabled={remove.isPending} onClick={() => remove.mutate(item.id)} aria-label={copy.remove} title={copy.remove}><Trash2 /></Button></div></TableCell></TableRow>) : models.isError ? null : <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={5}>{copy.noAIModels}</TableCell></TableRow>}</TableBody>
        </Table>
      </div>
      <Dialog open={addOpen} onOpenChange={(open) => { setAddOpen(open); if (!open) setEditingModel(null); }}>
        <DialogContent className="flex max-h-[calc(100dvh-2rem)] w-[calc(100%-1.5rem)] sm:w-auto sm:max-w-md flex-col gap-0 overflow-hidden p-0">
          <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12"><DialogTitle>{editingModel ? copy.editAIModel : copy.addAIModel}</DialogTitle></DialogHeader>
          <form className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 sm:p-5 grid gap-4 touch-pan-y" noValidate onSubmit={submit}>
            <div className="grid gap-2"><div className="flex items-center gap-1.5"><Label htmlFor="add-ai-format">{copy.aiAPIFormat}</Label><Tooltip><TooltipTrigger type="button" closeOnClick={false} className="inline-flex size-4 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={copy.aiAPIFormatHint}><CircleAlert className="size-3.5" /></TooltipTrigger><TooltipContent side="right" align="start" className="max-w-sm whitespace-pre-line text-pretty text-left">{copy.aiAPIFormatHint}</TooltipContent></Tooltip></div><Select value={apiFormat} onValueChange={(val) => handleAPIFormatChange(val as APIFormat)} disabled={add.isPending || update.isPending || test.isPending}><SelectTrigger id="add-ai-format" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="chat_completions">OpenAI Chat Completions</SelectItem><SelectItem value="responses">OpenAI Responses</SelectItem><SelectItem value="gemini">Google Gemini</SelectItem></SelectContent></Select></div>
            <div className="grid gap-2"><Label htmlFor="add-ai-base-url">{copy.aiBaseURL}</Label><Input id="add-ai-base-url" type="url" value={baseURL} required aria-invalid={invalid.baseUrl} disabled={add.isPending || update.isPending || test.isPending} onChange={(event) => { setBaseURL(event.target.value); if (invalid.baseUrl) setInvalid((current) => ({ ...current, baseUrl: false })); }} placeholder={apiFormat === "gemini" ? "https://generativelanguage.googleapis.com" : apiFormat === "responses" ? "https://api.openai.com/v1" : "https://api.example.com/v1"} /></div>
            <div className="grid gap-2"><Label htmlFor="add-ai-model">{copy.aiModel}</Label><Input id="add-ai-model" value={model} required aria-invalid={invalid.model} disabled={add.isPending || update.isPending || test.isPending} onChange={(event) => { setModel(event.target.value); if (invalid.model) setInvalid((current) => ({ ...current, model: false })); }} placeholder={apiFormat === "gemini" ? "gemini-3.8-flash" : apiFormat === "responses" ? "gpt-5.6-sol" : "provider/model-name"} /></div>
            {apiFormat !== "chat_completions" && <div className="grid gap-2"><Label htmlFor="add-ai-reasoning">{copy.aiReasoningEffort}</Label><Select value={reasoningEffort} onValueChange={(value) => setReasoningEffort(value as "low" | "medium" | "high")} disabled={add.isPending || update.isPending || test.isPending}><SelectTrigger id="add-ai-reasoning" className="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="low">{copy.aiReasoningLow}</SelectItem><SelectItem value="medium">{copy.aiReasoningMedium}</SelectItem><SelectItem value="high">{copy.aiReasoningHigh}</SelectItem></SelectContent></Select></div>}
            <div className="flex items-center justify-between gap-4"><div className="grid gap-1.5"><Label htmlFor="add-ai-supports-images">{copy.aiSupportsImages}</Label><p id="add-ai-supports-images-hint" className="text-xs text-muted-foreground">{copy.aiSupportsImagesHint}</p></div><Switch id="add-ai-supports-images" aria-describedby="add-ai-supports-images-hint" checked={supportsImages} onCheckedChange={setSupportsImages} disabled={add.isPending || update.isPending || test.isPending} /></div>
            <div className="grid gap-2"><Label htmlFor="add-ai-api-key">{copy.aiAPIKey}</Label><Input id="add-ai-api-key" type="password" value={apiKey} required={!editingModel} aria-invalid={invalid.apiKey} disabled={add.isPending || update.isPending || test.isPending} onChange={(event) => { setAPIKey(event.target.value); if (invalid.apiKey) setInvalid((current) => ({ ...current, apiKey: false })); }} placeholder={editingModel ? copy.aiAPIKeyKeep : (apiFormat === "gemini" ? "AIzaSy..." : "sk-...")} autoComplete="off" /></div>
            <div className="grid gap-2"><div className="flex items-center gap-1.5"><Label htmlFor="add-ai-extra-params">{copy.aiExtraParams}</Label><Tooltip><TooltipTrigger type="button" closeOnClick={false} className="inline-flex size-4 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={copy.aiExtraParamsHint}><CircleAlert className="size-3.5" /></TooltipTrigger><TooltipContent side="right" align="start" className="max-w-sm whitespace-pre-line text-pretty text-left">{copy.aiExtraParamsHint}</TooltipContent></Tooltip></div><Textarea id="add-ai-extra-params" className="min-h-24 resize-y font-mono text-xs" value={extraParams} aria-invalid={invalid.extraParams} disabled={add.isPending || update.isPending || test.isPending} onChange={(event) => { setExtraParams(event.target.value); if (invalid.extraParams) setInvalid((current) => ({ ...current, extraParams: false })); }} placeholder={'{"reasoning_effort":"high"}'} /></div>
            <DialogFooter className="pt-2"><Button type="button" variant="ghost" onClick={() => setAddOpen(false)}>{copy.cancel}</Button><Button type="button" variant="outline" disabled={test.isPending || add.isPending || update.isPending || !baseURL.trim() || !model.trim() || (!editingModel && !apiKey.trim())} onClick={() => { if (!validate()) return; test.mutate({ id: editingModel?.id, input: { provider: providerForAPIFormat(apiFormat), baseUrl: baseURL.trim(), model: model.trim(), apiKey: apiKey.trim(), reasoningEffort, extraParams: extraParams.trim() || "{}" } }); }}>{test.isPending ? copy.aiModelTesting : copy.aiModelTest}</Button><Button type="submit" disabled={add.isPending || update.isPending || test.isPending}>{editingModel ? (update.isPending ? copy.updatingAIModel : copy.editAIModel) : (add.isPending ? copy.addingAIModel : copy.addAIModel)}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export function AccountInfoSettings({ copy }: { copy: Copy }) {
  const queryClient = useQueryClient();
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities, retry: false });
  const [displayName, setDisplayName] = useState("");
  const [passwords, setPasswords] = useState({ current: "", next: "", confirmation: "" });
  const [passwordInvalid, setPasswordInvalid] = useState<{ current?: boolean; next?: boolean; confirmation?: boolean }>({});
  useEffect(() => {
    if (capabilities.data?.currentUser) setDisplayName(capabilities.data.currentUser.displayName);
  }, [capabilities.data?.currentUser]);
  const update = useMutation({
    mutationFn: (value: string) => updateAccountProfile(value),
    onSuccess: (user) => {
      queryClient.setQueryData(["capabilities"], (current: { notifications: boolean; webPush: boolean; calendar: boolean; role: UserRole; currentUser?: { login: string; displayName: string; role: UserRole } } | undefined) => current ? { ...current, role: user.role, currentUser: { login: user.login, displayName: user.displayName, role: user.role } } : current);
      toast.success(copy.accountInfoSaved);
    },
    onError: (value) => toastError(value, copy.loadFailed),
  });
  const currentUser = capabilities.data?.currentUser;
  const roleLabel = (currentUser?.role || capabilities.data?.role) === "super_admin" ? copy.superAdmin : copy.ordinaryUser;
  const updatePassword = useMutation({
    mutationFn: () => updateAccountPassword(passwords.current, passwords.next, passwords.confirmation),
    onSuccess: () => {
      setPasswords({ current: "", next: "", confirmation: "" });
      setPasswordInvalid({});
      toast.success(copy.passwordUpdated);
    },
    onError: (value) => toastError(value, copy.loadFailed),
  });
  const submitPassword = (event: FormEvent) => {
    event.preventDefault();
    const missingCurrent = !passwords.current;
    const missingNext = passwords.next.length < 8;
    const missingConfirmation = passwords.confirmation.length < 8;
    if (missingCurrent || missingNext || missingConfirmation) {
      setPasswordInvalid({ current: missingCurrent, next: missingNext, confirmation: missingConfirmation });
      toast.error(missingCurrent || missingConfirmation ? copy.requiredFieldsMissing : copy.passwordRequirements);
      return;
    }
    if (passwords.next !== passwords.confirmation) {
      setPasswordInvalid({ next: true, confirmation: true });
      toast.error(copy.passwordMismatch);
      return;
    }
    setPasswordInvalid({});
    updatePassword.mutate();
  };
  return (
    <section className="max-w-2xl">
      <div><h2 className="flex items-center gap-2 text-lg font-semibold"><UserRound className="size-5" />{copy.accountInfo}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.accountInfoDescription}</p></div>
      <div className="mt-6 grid max-w-md gap-5">
        <div className="grid gap-1"><p className="text-xs text-muted-foreground">{copy.accountLogin}</p><p className="truncate text-sm font-medium">{capabilities.isPending ? copy.loading : currentUser?.login || "-"}</p></div>
        <div className="grid max-w-xs gap-2"><Label htmlFor="account-nickname">{copy.nickname}</Label><Input id="account-nickname" className="w-64 max-w-full" value={displayName} maxLength={80} disabled={capabilities.isPending || update.isPending} onChange={(event) => setDisplayName(event.target.value)} onBlur={() => { const next = displayName.trim(); if (next !== (currentUser?.displayName || "").trim()) update.mutate(next); }} /></div>
        <div className="grid gap-1"><p className="text-xs text-muted-foreground">{copy.role}</p><p className="text-sm font-medium">{capabilities.isPending ? copy.loading : roleLabel}</p></div>
      </div>
      <form className="mt-8 grid max-w-xs gap-4 border-t pt-6" noValidate onSubmit={submitPassword}>
        <div><h3 className="text-sm font-semibold">{copy.changePassword}</h3><p className="mt-1 text-xs text-muted-foreground">{copy.passwordRequirements}</p></div>
        <div className="grid gap-2"><Label htmlFor="current-password">{copy.currentPassword}</Label><Input id="current-password" className="w-64 max-w-full" type="password" autoComplete="current-password" value={passwords.current} aria-invalid={passwordInvalid.current} disabled={updatePassword.isPending} onChange={(event) => { setPasswords((current) => ({ ...current, current: event.target.value })); if (passwordInvalid.current) setPasswordInvalid((current) => ({ ...current, current: false })); }} required /></div>
        <div className="grid gap-2"><Label htmlFor="new-password">{copy.newPassword}</Label><Input id="new-password" className="w-64 max-w-full" type="password" autoComplete="new-password" minLength={8} value={passwords.next} aria-invalid={passwordInvalid.next} disabled={updatePassword.isPending} onChange={(event) => { setPasswords((current) => ({ ...current, next: event.target.value })); if (passwordInvalid.next) setPasswordInvalid((current) => ({ ...current, next: false })); }} required /></div>
        <div className="grid gap-2"><Label htmlFor="confirm-new-password">{copy.confirmNewPassword}</Label><Input id="confirm-new-password" className="w-64 max-w-full" type="password" autoComplete="new-password" minLength={8} value={passwords.confirmation} aria-invalid={passwordInvalid.confirmation} disabled={updatePassword.isPending} onChange={(event) => { setPasswords((current) => ({ ...current, confirmation: event.target.value })); if (passwordInvalid.confirmation) setPasswordInvalid((current) => ({ ...current, confirmation: false })); }} required /></div>
        <div><Button type="submit" disabled={updatePassword.isPending}>{updatePassword.isPending ? copy.updatingPassword : copy.changePassword}</Button></div>
      </form>
    </section>
  );
}

export function GeneralSettings({ copy }: { copy: Copy }) {
  const capabilities = useQuery({ queryKey: ["capabilities"], queryFn: getCapabilities, retry: false });
  const [pushEnabled, setPushEnabled] = useState(false);
  useEffect(() => { void currentPushSubscription().then((subscription) => setPushEnabled(Boolean(subscription))); }, []);
  const togglePush = async () => {
    try {
      if (pushEnabled) await disableWebPush();
      else await enableWebPush(navigator.language);
      setPushEnabled(!pushEnabled);
    } catch (value) {
      toastError(value, copy.pushUnavailable);
    }
  };
  const pushAvailable = supportsWebPush() && capabilities.data?.webPush === true;
  const currentLanguage = copy === en ? "en" : "zh-CN";
  return (
    <div className="max-w-2xl">
      <h2 className="text-lg font-semibold">{copy.generalSettings}</h2>
      <section className="mt-6">
        <h3 className="flex items-center gap-2 text-sm font-semibold"><Languages className="size-4" />{copy.language}</h3>
        <div className="mt-3 inline-flex items-center rounded-lg border bg-muted p-0.5">
          <Button className={cn(currentLanguage === "zh-CN" && "bg-background shadow-sm ring-1 ring-border hover:bg-background")} variant="ghost" size="sm" aria-pressed={currentLanguage === "zh-CN"} onClick={() => switchLanguage("zh-CN")}>{currentLanguage === "zh-CN" && <Check />}简体中文</Button>
          <Button className={cn(currentLanguage === "en" && "bg-background shadow-sm ring-1 ring-border hover:bg-background")} variant="ghost" size="sm" aria-pressed={currentLanguage === "en"} onClick={() => switchLanguage("en")}>{currentLanguage === "en" && <Check />}English</Button>
        </div>
      </section>
      <section className="mt-7 border-t pt-6">
        <h3 className="text-sm font-semibold">{copy.pushNotifications}</h3>
        <Button className="mt-3 max-w-full whitespace-normal" variant="secondary" disabled={!pushAvailable} onClick={() => void togglePush()}>{pushEnabled ? <BellOff /> : <Bell />}{pushEnabled ? copy.disablePush : copy.enablePush}</Button>
        {!pushAvailable && !capabilities.isPending && <p className="mt-2 text-xs text-muted-foreground">{copy.pushUnavailable}</p>}
      </section>
    </div>
  );
}

export const emptyAccountForm = { email: "", password: "", label: "", color: "#4f46e5", imap_server: "", imap_port: 993, smtp_server: "", smtp_port: 587 };

export function MailboxSettings({ copy, onManageAccount }: { copy: Copy; onManageAccount: (account: ConnectedAccount | null) => void }) {
  const queryClient = useQueryClient();
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: getAccounts, retry: false });
  useQueryErrorToast(accounts.error, copy.loadFailed, "accounts-error", () => void accounts.refetch(), copy.retry);
  const refreshAccountData = () => {
    void queryClient.invalidateQueries({ queryKey: ["accounts"] });
    void queryClient.invalidateQueries({ queryKey: ["conversations"] });
    void queryClient.invalidateQueries({ queryKey: ["ai-task-bindings"] });
  };
  const remove = useMutation({ mutationFn: deleteAccount, onSuccess: () => refreshAccountData(), onError: (value) => toastError(value, copy.loadFailed) });
  return (
    <section className="min-w-0">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h2 className="text-lg font-semibold">{copy.mailboxManagement}</h2><p className="mt-1 text-sm text-muted-foreground">{copy.mailboxDescription}</p></div>
        <Button onClick={() => onManageAccount(null)}><Plus />{copy.addAccount}</Button>
      </div>
      <div className="mt-5 overflow-hidden rounded-lg border">
        <Table className="min-w-[44rem] table-fixed">
          <TableHeader className="bg-muted/60 text-xs text-muted-foreground"><TableRow className="hover:bg-transparent"><TableHead className="w-[34%] px-4">{copy.account}</TableHead><TableHead className="w-[24%] px-4">IMAP</TableHead><TableHead className="w-[24%] px-4">SMTP</TableHead><TableHead className="w-[18%] px-4 text-right">{copy.actions}</TableHead></TableRow></TableHeader>
          <TableBody>
            {accounts.isPending && <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={4}>{copy.loading}</TableCell></TableRow>}
            {accounts.data?.accounts.map((account: ConnectedAccount) => (
              <TableRow key={account.id}>
                <TableCell className="px-4 py-3"><div className="flex min-w-0 items-center gap-2.5"><span className="size-3 shrink-0 rounded-full" style={{ backgroundColor: account.color || "#777" }} /><div className="min-w-0"><strong className="block truncate font-medium">{account.label || account.email}</strong><span className="block truncate text-xs text-muted-foreground">{account.email}</span></div></div></TableCell>
                <TableCell className="px-4 py-3"><span className="block truncate" title={`${account.imapServer}${account.imapPort ? `:${account.imapPort}` : ""}`}>{account.imapServer}{account.imapPort ? `:${account.imapPort}` : ""}</span></TableCell>
                <TableCell className="px-4 py-3"><span className="block truncate" title={`${account.smtpServer || "-"}${account.smtpPort ? `:${account.smtpPort}` : ""}`}>{account.smtpServer || "-"}{account.smtpPort ? `:${account.smtpPort}` : ""}</span></TableCell>
                <TableCell className="px-4 py-3"><div className="flex items-center justify-end gap-1"><Button variant="ghost" size="icon" className="size-8" disabled={remove.isPending} onClick={() => onManageAccount(account)} aria-label={copy.editAccount} title={copy.editAccount}><Pencil /></Button><Button variant="ghost" size="icon" className="size-8 text-destructive hover:text-destructive" disabled={remove.isPending} onClick={() => remove.mutate(account.id)} aria-label={copy.remove} title={copy.remove}><Trash2 /></Button></div></TableCell>
              </TableRow>
            ))}
            {!accounts.isPending && !accounts.isError && !accounts.data?.accounts.length && <TableRow><TableCell className="h-24 text-center text-muted-foreground" colSpan={4}>{copy.noAccounts}</TableCell></TableRow>}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}

function AIBindingRow({
  copy,
  accountEmail,
  taskType,
  label,
  binding,
  agents,
  models,
  pending,
  onSave,
}: {
  copy: Copy;
  accountEmail: string;
  taskType: "mail_summary" | "email_draft" | "reply_suggestion";
  label: string;
  binding?: AITaskBinding;
  agents: AIAgent[];
  models: AIModel[];
  pending: boolean;
  onSave: (input: AITaskBindingInput) => void;
}) {
  // The binding only exists on the server once BOTH an agent and a model are
  // set, so a first selection that would leave the pair incomplete is kept
  // here until the user picks the other half.
  const [agentId, setAgentId] = useState(binding?.agentId || "");
  const [modelId, setModelId] = useState(binding?.modelId || "");
  const [enabled, setEnabled] = useState(binding?.enabled !== false);
  const [invalid, setInvalid] = useState(false);

  useEffect(() => {
    setAgentId(binding?.agentId || "");
    setModelId(binding?.modelId || "");
    setEnabled(binding?.enabled !== false);
    setInvalid(false);
  }, [binding?.agentId, binding?.modelId, binding?.enabled]);

  const persist = (next: { agentId: string; modelId: string; enabled: boolean }) => {
    if (!next.agentId || !next.modelId) {
      setInvalid(true);
      toast.error(copy.bindingRequired);
      return;
    }
    setInvalid(false);
    onSave({ accountEmail, taskType, agentId: next.agentId, modelId: next.modelId, enabled: next.enabled });
  };

  return (
    <TableRow>
      <TableCell className="px-3 py-3 font-medium">{label}</TableCell>
      <TableCell className="px-3 py-3">
        <Select
          value={agentId}
          disabled={!enabled || !agents.length || pending}
          onValueChange={(value) => {
            const nextAgentId = value || "";
            setAgentId(nextAgentId);
            persist({ agentId: nextAgentId, modelId, enabled });
          }}
        >
          <SelectTrigger className="w-full" aria-invalid={invalid && !agentId}>
            <SelectValue>{agents.find((item) => item.id === agentId)?.name || copy.noAgents}</SelectValue>
          </SelectTrigger>
          <SelectContent>{agents.map((item) => <SelectItem value={item.id} key={item.id}>{item.name}</SelectItem>)}</SelectContent>
        </Select>
      </TableCell>
      <TableCell className="px-3 py-3">
        <Select
          value={modelId}
          disabled={!enabled || !models.length || pending}
          onValueChange={(value) => {
            const nextModelId = value || "";
            setModelId(nextModelId);
            persist({ agentId, modelId: nextModelId, enabled });
          }}
        >
          <SelectTrigger className="w-full" aria-invalid={invalid && !modelId}>
            <SelectValue>{models.find((item) => item.id === modelId)?.model || copy.noAIModels}</SelectValue>
          </SelectTrigger>
          <SelectContent>{models.map((item) => <SelectItem value={item.id} key={item.id}>{item.model}</SelectItem>)}</SelectContent>
        </Select>
      </TableCell>
      <TableCell className="px-3 py-3 text-center">
        <Switch
          aria-label={label}
          checked={enabled}
          disabled={!agentId || !modelId || pending}
          onCheckedChange={(checked) => {
            setEnabled(checked);
            persist({ agentId, modelId, enabled: checked });
          }}
        />
      </TableCell>
    </TableRow>
  );
}

export function AccountDialog({ copy, open, account, onOpenChange }: { copy: Copy; open: boolean; account: ConnectedAccount | null; onOpenChange: (open: boolean) => void }) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState({ ...emptyAccountForm });
  const [invalid, setInvalid] = useState<Record<string, boolean>>({});
  const bindings = useQuery({ queryKey: ["ai-task-bindings"], queryFn: getAITaskBindings, enabled: open && Boolean(account), retry: false });
  const agents = useQuery({ queryKey: ["ai-agents"], queryFn: getAIAgents, enabled: open && Boolean(account), retry: false });
  const models = useQuery({ queryKey: ["ai-models"], queryFn: getAIModels, enabled: open && Boolean(account), retry: false });
  const accountIdentifier = account?.id;
  const webhook = useQuery({ queryKey: ["account-feishu-webhook", accountIdentifier], queryFn: () => getAccountFeishuWebhookSettings(accountIdentifier!), enabled: open && Boolean(accountIdentifier), retry: false });
  const saveBinding = useMutation({ mutationFn: saveAITaskBinding, onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ["ai-task-bindings"] }); }, onError: (value) => toastError(value, copy.loadFailed) });
  const saveWebhook = useMutation({ mutationFn: (value: { enabled: boolean; url: string }) => saveAccountFeishuWebhookSettings(accountIdentifier!, value), onSuccess: (value) => queryClient.setQueryData(["account-feishu-webhook", accountIdentifier], value), onError: (value) => toastError(value, copy.loadFailed) });
  useEffect(() => {
    if (!open) return;
    setForm(account ? {
      email: account.email,
      password: "",
      label: account.label || "",
      color: account.color || "#4f46e5",
      imap_server: account.imapServer || "",
      imap_port: account.imapPort || 993,
      smtp_server: account.smtpServer || "",
      smtp_port: account.smtpPort || 587,
    } : { ...emptyAccountForm });
    setInvalid({});
  }, [account, open]);
  const changeOpen = (value: boolean) => {
    if (!value) setInvalid({});
    onOpenChange(value);
  };
  const persist = useMutation({ mutationFn: () => account ? updateAccount(account.id, form) : addAccount(form), onSuccess: () => { setForm({ ...emptyAccountForm }); setInvalid({}); void queryClient.invalidateQueries({ queryKey: ["accounts"] }); void queryClient.invalidateQueries({ queryKey: ["conversations"] }); void queryClient.invalidateQueries({ queryKey: ["ai-task-bindings"] }); onOpenChange(false); if (account) toast.success(copy.accountUpdated); }, onError: (value) => toastError(value, copy.loadFailed) });
  const clearInvalid = (name: string) => setInvalid((current) => {
    if (!current[name]) return current;
    const next = { ...current };
    delete next[name];
    return next;
  });
  const field = (name: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) => {
    const value = event.target.type === "number" ? Number(event.target.value) : event.target.value;
    setForm((current) => ({ ...current, [name]: value }));
    clearInvalid(name);
  };
  const webhookURL = webhook.data?.url || "";
  const webhookInvalid = Boolean(invalid.webhook);
  const validWebhookURL = (value: string) => {
    try {
      const parsed = new URL(value);
      return parsed.protocol === "http:" || parsed.protocol === "https:";
    } catch {
      return false;
    }
  };
  const setWebhookEnabled = (nextEnabled: boolean) => {
    if (nextEnabled && !webhookURL.trim()) {
      setInvalid((current) => ({ ...current, webhook: true }));
      toast.error(copy.feishuWebhookURLRequired);
      return;
    }
    if (nextEnabled && !validWebhookURL(webhookURL.trim())) {
      setInvalid((current) => ({ ...current, webhook: true }));
      toast.error(copy.webhookURLInvalid);
      return;
    }
    clearInvalid("webhook");
    saveWebhook.mutate({ enabled: nextEnabled, url: webhookURL });
  };
  const saveWebhookField = () => {
    const url = webhookURL.trim();
    if (webhook.data?.enabled && !url) {
      setInvalid((current) => ({ ...current, webhook: true }));
      toast.error(copy.feishuWebhookURLRequired);
      return;
    }
    if (url && !validWebhookURL(url)) {
      setInvalid((current) => ({ ...current, webhook: true }));
      toast.error(copy.webhookURLInvalid);
      return;
    }
    clearInvalid("webhook");
    if (webhook.data) saveWebhook.mutate({ enabled: webhook.data.enabled || false, url });
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const nextInvalid: Record<string, boolean> = {};
    const messages: string[] = [];
    if (!form.email.trim()) {
      nextInvalid.email = true;
      messages.push(copy.accountEmailRequired);
    } else if (!isValidRecipient(form.email.trim())) {
      nextInvalid.email = true;
      messages.push(copy.invalidRecipient);
    }
    if (!account && !form.password.trim()) {
      nextInvalid.password = true;
      messages.push(copy.accountPasswordRequired);
    }
    for (const port of ["imap_port", "smtp_port"] as const) {
      const value = Number(form[port]);
      if (!Number.isInteger(value) || value < 1 || value > 65535) {
        nextInvalid[port] = true;
        messages.push(copy.invalidPort);
      }
    }
    if (messages.length) {
      setInvalid(nextInvalid);
      toast.error(messages.length === 1 ? messages[0] : copy.requiredFieldsMissing);
      return;
    }
    setInvalid({});
    persist.mutate();
  };
  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent className="flex h-[calc(100dvh-2rem)] sm:h-auto sm:max-h-[calc(100dvh-3rem)] w-[calc(100%-1.5rem)] sm:w-auto flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12"><DialogTitle>{account ? copy.editAccount : copy.addAccount}</DialogTitle><DialogDescription>{copy.mailboxDescription}</DialogDescription></DialogHeader>
        <form className="flex min-h-0 flex-1 flex-col" noValidate onSubmit={submit}>
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 sm:p-5 grid gap-5 touch-pan-y">
            <div className="grid gap-3 sm:grid-cols-2">
              <Label className="grid gap-1.5">{copy.email}<Input type="email" value={form.email} aria-invalid={invalid.email} onChange={field("email")} autoComplete="email" disabled={Boolean(account)} required /></Label>
              <Label className="grid gap-1.5">{copy.password}{account && <span className="text-xs font-normal text-muted-foreground">{copy.passwordKeep}</span>}<Input type="password" value={form.password} aria-invalid={invalid.password} onChange={field("password")} autoComplete="new-password" required={!account} /></Label>
              <Label className="grid gap-1.5">{copy.displayName} ({copy.optional})<Input value={form.label} onChange={field("label")} /></Label>
              <Label className="grid gap-1.5">{copy.color}<Input className="p-1" type="color" value={form.color} onChange={field("color")} /></Label>
            </div>
            <div className="grid gap-3 border-t pt-5 sm:grid-cols-[minmax(0,1fr)_8rem]">
              <Label className="grid gap-1.5">{copy.imapServer}<Input value={form.imap_server} onChange={field("imap_server")} placeholder="imap.example.com" /></Label>
              <Label className="grid gap-1.5">{copy.imapPort}<Input type="number" min={1} max={65535} value={form.imap_port} aria-invalid={invalid.imap_port} onChange={field("imap_port")} required /></Label>
              <Label className="grid gap-1.5">{copy.smtpServer}<Input value={form.smtp_server} onChange={field("smtp_server")} placeholder="smtp.example.com" /></Label>
              <Label className="grid gap-1.5">{copy.smtpPort}<Input type="number" min={1} max={65535} value={form.smtp_port} aria-invalid={invalid.smtp_port} onChange={field("smtp_port")} required /></Label>
            </div>
            {account && <div className="grid gap-4 border-t pt-5">
              <AccountBrandSettings key={account.id} copy={copy} accountId={account.id} />
              <div><h3 className="text-sm font-semibold">{copy.mailboxAIConfiguration}</h3><p className="mt-1 text-xs text-muted-foreground">{copy.mailboxAIConfigurationDescription}</p></div>
              <div className="overflow-x-auto rounded-lg border">
                <Table className="min-w-[38rem] table-fixed">
                  <TableHeader className="bg-muted/60 text-xs text-muted-foreground">
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="w-[17%] px-3">{copy.aiTask}</TableHead>
                      <TableHead className="w-[35%] px-3">{copy.agentSettings}</TableHead>
                      <TableHead className="w-[35%] px-3">{copy.aiModel}</TableHead>
                      <TableHead className="w-[13%] px-3 text-center">{copy.enabled}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(["mail_summary", "email_draft", "reply_suggestion"] as const).map((taskType) => {
                      const binding = bindings.data?.bindings.find((item) => item.accountEmail === account.email && item.taskType === taskType);
                      const label = taskType === "mail_summary" ? copy.mailSummaryAgent : taskType === "email_draft" ? copy.emailDraftAgent : copy.replySuggestionAgent;
                      const availableAgents = agents.data?.agents || [];
                      return <AIBindingRow
                        key={taskType}
                        copy={copy}
                        accountEmail={account.email}
                        taskType={taskType}
                        label={label}
                        binding={binding}
                        agents={availableAgents}
                        models={models.data?.models || []}
                        pending={saveBinding.isPending}
                        onSave={saveBinding.mutate}
                      />;
                    })}
                  </TableBody>
                </Table>
              </div>
              <div className="grid gap-3 border-t pt-4"><div className="flex items-center justify-between gap-3"><div><Label>{copy.feishuWebhookEnabled}</Label><p className="mt-1 text-xs text-muted-foreground">{copy.feishuWebhookDescription}</p></div><Switch checked={webhook.data?.enabled || false} disabled={webhook.isPending || saveWebhook.isPending} onCheckedChange={setWebhookEnabled} /></div><Input type="url" aria-invalid={webhookInvalid} placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..." value={webhookURL} disabled={webhook.isPending || saveWebhook.isPending} onChange={(event) => { queryClient.setQueryData(["account-feishu-webhook", accountIdentifier], { enabled: webhook.data?.enabled || false, url: event.target.value }); clearInvalid("webhook"); }} onBlur={saveWebhookField} /></div>
            </div>}
          </div>
          <DialogFooter className="shrink-0 border-t px-5 py-3"><Button type="button" variant="ghost" disabled={persist.isPending} onClick={() => changeOpen(false)}>{copy.cancel}</Button><Button type="submit" disabled={persist.isPending}>{account ? <Pencil /> : <Plus />}{persist.isPending ? (account ? copy.savingAccount : copy.adding) : account ? copy.editAccount : copy.addAccount}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
