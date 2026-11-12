import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { getMailIgnoreRules, previewMailIgnoreRules, saveMailIgnoreRules, type MailIgnoreAction, type MailIgnoreCondition, type MailIgnoreRule } from "../../lib/api";
import type { Copy } from "../../lib/locale";
import { toast } from "../../lib/toast";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Switch } from "../ui/switch";

export function MailIgnoreRulesSettings({ accountId, copy }: { accountId: string; copy: Copy }) {
  return <Dialog>
    <div className="flex min-w-0 items-center justify-between gap-4 border-t pt-4">
      <div className="min-w-0"><h3 className="text-sm font-medium">{copy.ignoreRules}</h3><p className="mt-1 text-xs text-muted-foreground">{copy.ignoreRulesSummary}</p></div>
      <DialogTrigger render={<Button type="button" variant="outline" size="sm" className="shrink-0" />}>{copy.ignoreManage}</DialogTrigger>
    </div>
    <DialogContent className="flex max-h-[calc(100dvh-2rem)] w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-h-[calc(100dvh-3rem)] sm:max-w-3xl">
      <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12">
        <DialogTitle>{copy.ignoreRules}</DialogTitle>
        <DialogDescription>{copy.ignoreRulesDescription}</DialogDescription>
      </DialogHeader>
      <div className="min-h-0 overflow-y-auto overscroll-contain p-4 touch-pan-y sm:p-5">
        <MailIgnoreRulesPanel accountId={accountId} copy={copy} />
      </div>
    </DialogContent>
  </Dialog>;
}

function MailIgnoreRulesPanel({ accountId, copy }: { accountId: string; copy: Copy }) {
  const query = useQuery({ queryKey: ["mail-ignore-rules", accountId], queryFn: () => getMailIgnoreRules(accountId), staleTime: Infinity, retry: false });
  return <section className="grid min-w-0 gap-3" aria-label={copy.ignoreRules} data-testid="mail-ignore-rules">
    {query.isPending ? <p className="text-xs text-muted-foreground">{copy.loading}</p> : query.isError ? <div role="alert" className="text-sm text-destructive">{copy.loadFailed}<Button type="button" variant="link" onClick={() => void query.refetch()}>{copy.ignoreRetry}</Button></div> :
      <MailIgnoreRulesEditor key={`${accountId}:${query.dataUpdatedAt}`} accountId={accountId} initialRules={query.data.rules} copy={copy} />}
  </section>;
}

function MailIgnoreRulesEditor({ accountId, initialRules, copy }: { accountId: string; initialRules: MailIgnoreRule[]; copy: Copy }) {
  const queryClient = useQueryClient();
  const [rules, setRules] = useState(initialRules);
  const [error, setError] = useState("");
  const dirty = JSON.stringify(rules) !== JSON.stringify(initialRules);
  const save = useMutation({
    mutationFn: () => saveMailIgnoreRules(accountId, rules),
    onSuccess: (data) => { queryClient.setQueryData(["mail-ignore-rules", accountId], data); toast.success(copy.ignoreSaved); },
    onError: (value: Error) => setError(value.message),
  });
  const preview = useMutation({ mutationFn: () => previewMailIgnoreRules(accountId, rules), onError: (value: Error) => setError(value.message) });
  const busy = save.isPending || preview.isPending;
  const actions: { value: MailIgnoreAction; label: string }[] = [{ value: "analysis", label: copy.ignoreAnalysis }, { value: "reply", label: copy.ignoreReply }, { value: "notification", label: copy.ignoreNotification }];
  const fields: { value: MailIgnoreCondition["field"]; label: string }[] = [{ value: "sender", label: copy.ignoreSender }, { value: "sender_domain", label: copy.ignoreSenderDomain }, { value: "subject", label: copy.subject }, { value: "message_id_domain", label: copy.ignoreMessageIDDomain }];
  const change = (next: MailIgnoreRule[]) => { setRules(next); setError(""); preview.reset(); };
  const update = (index: number, value: Partial<MailIgnoreRule>) => change(rules.map((rule, i) => i === index ? { ...rule, ...value } : rule));
  const valid = rules.every((rule) => rule.name.trim() && rule.actions.length && rule.conditions.length && rule.conditions.every((condition) => condition.value.trim()));
  return <div className="grid min-w-0 gap-3" onKeyDown={(event) => { if (event.key === "Enter" && event.target instanceof HTMLInputElement) event.preventDefault(); }}>
    {!rules.length && <p className="text-xs text-muted-foreground">{copy.ignoreEmpty}</p>}
    {rules.map((rule, index) => <div key={rule.id} className="grid min-w-0 gap-3 rounded-lg border p-3" data-testid="ignore-rule">
      <div className="flex min-w-0 items-center gap-2">
        <Input className="min-w-0 flex-1" aria-label={copy.ignoreName} value={rule.name} maxLength={80} disabled={busy} onChange={(event) => update(index, { name: event.target.value })} />
        <Switch aria-label={`${copy.ignoreEnabled}: ${rule.name}`} checked={rule.enabled} disabled={busy} onCheckedChange={(enabled) => update(index, { enabled })} />
        <Button type="button" variant="ghost" size="icon-sm" aria-label={`${copy.ignoreDelete}: ${rule.name}`} disabled={busy} onClick={() => change(rules.filter((_, i) => i !== index))}><Trash2 /></Button>
      </div>
      <p className="text-xs text-muted-foreground">{copy.ignoreAllConditions}</p>
      {rule.conditions.map((condition, conditionIndex) => <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 sm:grid-cols-[11rem_6rem_minmax(0,1fr)_auto]" key={conditionIndex}>
        <Select value={condition.field} disabled={busy} onValueChange={(value) => { if (value) update(index, { conditions: rule.conditions.map((item, i) => i === conditionIndex ? { ...item, field: value, operator: "equals" } : item) }); }}>
          <SelectTrigger className="w-full" aria-label={copy.ignoreField}><SelectValue>{fields.find((field) => field.value === condition.field)?.label}</SelectValue></SelectTrigger>
          <SelectContent>{fields.map((field) => <SelectItem key={field.value} value={field.value}>{field.label}</SelectItem>)}</SelectContent>
        </Select>
        <Select value={condition.operator} disabled={busy || condition.field !== "subject"} onValueChange={(value) => { if (value) update(index, { conditions: rule.conditions.map((item, i) => i === conditionIndex ? { ...item, operator: value } : item) }); }}>
          <SelectTrigger className="w-full" aria-label={copy.ignoreOperator}><SelectValue>{condition.operator === "contains" ? copy.ignoreContains : copy.ignoreEquals}</SelectValue></SelectTrigger>
          <SelectContent><SelectItem value="equals">{copy.ignoreEquals}</SelectItem><SelectItem value="contains">{copy.ignoreContains}</SelectItem></SelectContent>
        </Select>
        <Input className="min-w-0" aria-label={copy.ignoreValue} value={condition.value} maxLength={300} disabled={busy} placeholder={condition.field === "subject" ? copy.subject : condition.field === "sender" ? "sender@example.com" : "example.com"} onChange={(event) => update(index, { conditions: rule.conditions.map((item, i) => i === conditionIndex ? { ...item, value: event.target.value } : item) })} />
        <Button type="button" variant="ghost" size="icon-sm" aria-label={copy.ignoreDeleteCondition} disabled={busy || rule.conditions.length === 1} onClick={() => update(index, { conditions: rule.conditions.filter((_, i) => i !== conditionIndex) })}><Trash2 /></Button>
      </div>)}
      <Button type="button" className="justify-self-start" variant="ghost" size="sm" disabled={busy || rule.conditions.length >= 8} onClick={() => update(index, { conditions: [...rule.conditions, { field: "subject", operator: "equals", value: "" }] })}><Plus />{copy.ignoreAddCondition}</Button>
      <div className="flex flex-wrap gap-x-4 gap-y-2 border-t pt-3">{actions.map((action) => <Label key={action.value} className="flex items-center gap-2 text-xs"><Switch checked={rule.actions.includes(action.value)} disabled={busy} onCheckedChange={(checked) => update(index, { actions: checked ? [...rule.actions, action.value] : rule.actions.filter((item) => item !== action.value) })} />{action.label}</Label>)}</div>
    </div>)}
    {error && <p role="alert" className="break-words text-xs text-destructive">{error}</p>}
    <div className="flex flex-wrap items-center gap-2">
      <Button type="button" variant="outline" size="sm" disabled={busy || rules.length >= 50} onClick={() => change([...rules, { id: crypto.randomUUID(), name: copy.ignoreNewName, enabled: true, conditions: [{ field: "sender", operator: "equals", value: "" }], actions: ["analysis", "reply", "notification"] }])}><Plus />{copy.ignoreAddRule}</Button>
      <Button type="button" variant="outline" size="sm" disabled={busy || !valid} onClick={() => { setError(""); preview.mutate(); }}>{preview.isPending ? copy.loading : copy.ignorePreview}</Button>
      <Button type="button" size="sm" disabled={busy || !valid || !dirty} onClick={() => { setError(""); save.mutate(); }}>{save.isPending ? copy.loading : copy.ignoreSave}</Button>
      {dirty && <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => change(initialRules)}>{copy.ignoreDiscard}</Button>}
    </div>
    {dirty && <p className="text-xs text-muted-foreground">{copy.ignoreUnsaved}</p>}
    {preview.data && <div className="grid min-w-0 gap-2 rounded-lg bg-muted/50 p-3" aria-live="polite">
      <p className="text-xs">{copy.ignorePreviewCount.replace("{total}", String(preview.data.total)).replace("{scanned}", String(preview.data.scanned))}</p>
      <p className="text-xs text-muted-foreground">{copy.ignorePreviewHint}</p>
      {preview.data.messages.map((message) => <div key={`${message.folder}/${message.messageId}`} className="min-w-0 border-t pt-2 text-xs">
        <p className="break-words font-medium">{message.subject || copy.noSubject}</p>
        <p className="break-words text-muted-foreground">{message.from || copy.ignoreUnknownSender} · {new Date(message.date).toLocaleString()}</p>
        <p className="break-words">{[...new Set(message.matches.map((match) => match.ruleName))].join(" · ")}</p>
        <p className="text-muted-foreground">{message.matches.map((match) => actions.find((action) => action.value === match.action)?.label).join(" · ")}</p>
      </div>)}
    </div>}
  </div>;
}
