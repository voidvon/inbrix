import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BookOpen, Check, LoaderCircle, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { addAISkill, deleteAISkill, getAISkills, updateAISkill, useMailSummarySkill, type AISkill, type AISkillInput } from "../../lib/api";
import { type Copy, zh } from "../../lib/locale";
import { toast, toastError } from "../../lib/toast";
import { useQueryErrorToast } from "../../lib/use-query-error-toast";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Switch } from "../ui/switch";
import { Textarea } from "../ui/textarea";
import { ProductSelect } from "./product-select";

const emptySkill: AISkillInput = { name: "", description: "", instructions: "", kind: "chat", dependsOn: [], enabled: true };

export function SkillSettings({ copy }: { copy: Copy }) {
  const isZh = copy === zh;
  const queryClient = useQueryClient();
  const skills = useQuery({ queryKey: ["ai-skills"], queryFn: getAISkills, retry: false });
  const [editor, setEditor] = useState<{ skill: AISkill | null; input: AISkillInput } | null>(null);
  const [removing, setRemoving] = useState<AISkill | null>(null);
  const text = isZh ? {
    title: "Skill 管理", description: "维护可复用的任务流程。保存后，聊天和邮件总结会在下一次执行时使用更新后的流程。",
    add: "新增 Skill", edit: "编辑 Skill", remove: "删除 Skill", name: "名称", id: "标识", idHint: "留空自动生成；或使用小写字母、数字、下划线与连字符。", descriptionLabel: "说明", instructions: "流程指令", kind: "用途", chat: "对话任务", summary: "邮件总结", enabled: "启用", builtIn: "内置", activeSummary: "当前邮件总结", useSummary: "用于邮件总结", dependencies: "依赖的 Skill", noDependencies: "此用途暂无其他 Skill", empty: "暂无 Skill", saved: "Skill 已保存", deleted: "Skill 已删除", bound: "邮件总结流程已切换", removeDescription: "删除后不再用于新的执行。被其他 Skill 引用或正在用于邮件总结的流程需要先解除引用。", api: "支持通过 API Key 调用 Skill 接口，自动维护流程配置。", apiLink: "查看 API 文档", defaultHint: "邮件总结流程应用于你的所有邮箱；模型、业务提示词和输出标签仍按邮箱配置。", invalid: "请填写名称和流程指令。", missing: "依赖已不可用，请取消选用后保存。",
  } : {
    title: "Skills", description: "Manage reusable workflows. Saved changes apply to the next chat or email analysis run.",
    add: "Add skill", edit: "Edit skill", remove: "Delete skill", name: "Name", id: "ID", idHint: "Leave blank to generate, or use lowercase letters, digits, underscores and hyphens.", descriptionLabel: "Description", instructions: "Workflow instructions", kind: "Purpose", chat: "Chat task", summary: "Mail summary", enabled: "Enabled", builtIn: "Built-in", activeSummary: "Active mail summary", useSummary: "Use for mail summary", dependencies: "Skill dependencies", noDependencies: "No other skills for this purpose", empty: "No skills yet", saved: "Skill saved", deleted: "Skill deleted", bound: "Mail summary workflow updated", removeDescription: "Deleted skills are unavailable to new runs. Remove dependent references or change the active summary workflow before deleting an in-use skill.", api: "Use your API Key with the skills endpoints to maintain workflows from scripts.", apiLink: "API reference", defaultHint: "The summary workflow applies to all your mailboxes; each mailbox keeps its own model, business prompt and output labels.", invalid: "Enter a name and workflow instructions.", missing: "A dependency is unavailable. Remove it before saving.",
  };
  useQueryErrorToast(skills.error, copy.loadFailed, "ai-skills-error", () => void skills.refetch(), copy.retry);
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["ai-skills"] });
    void queryClient.invalidateQueries({ queryKey: ["ai-agents"] });
  };
  const save = useMutation({
    mutationFn: (value: NonNullable<typeof editor>) => value.skill
      ? updateAISkill(value.skill.id, { ...value.input, revision: value.skill.revision })
      : addAISkill(value.input),
    onSuccess: () => { setEditor(null); refresh(); toast.success(text.saved); },
    onError: error => toastError(error, copy.loadFailed),
  });
  const remove = useMutation({
    mutationFn: (skill: AISkill) => deleteAISkill(skill.id, skill.revision),
    onSuccess: () => { setRemoving(null); refresh(); toast.success(text.deleted); },
    onError: error => toastError(error, copy.loadFailed),
  });
  const toggle = useMutation({
    mutationFn: (skill: AISkill) => updateAISkill(skill.id, { enabled: !skill.enabled, revision: skill.revision }),
    onSuccess: refresh,
    onError: error => toastError(error, copy.loadFailed),
  });
  const bind = useMutation({
    mutationFn: (skill: AISkill) => useMailSummarySkill(skill.id, skill.revision),
    onSuccess: () => { refresh(); toast.success(text.bound); },
    onError: error => toastError(error, copy.loadFailed),
  });
  const pending = save.isPending || remove.isPending || toggle.isPending || bind.isPending;
  const patch = (input: Partial<AISkillInput>) => setEditor(value => value ? { ...value, input: { ...value.input, ...input } } : null);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!editor) return;
    if (!editor.input.name.trim() || !editor.input.instructions.trim()) { toast.error(text.invalid); return; }
    save.mutate(editor);
  };
  const records = skills.data?.skills || [];
  const available = records.filter(skill => skill.id !== editor?.skill?.id && skill.kind === editor?.input.kind);
  const selectedDependencies = editor?.input.dependsOn || [];
  const dependencyOptions = [...available.map(skill => ({ id: skill.id, name: skill.name, missing: false })), ...selectedDependencies.filter(id => !available.some(skill => skill.id === id)).map(id => ({ id, name: id, missing: true }))];
  return <section className="grid min-w-0 gap-5" data-testid="skill-settings">
    <div className="flex flex-col items-start justify-between gap-3 sm:flex-row">
      <div className="min-w-0 flex-1"><h2 className="flex items-center gap-2 text-lg font-semibold"><BookOpen className="size-5" />{text.title}</h2><p className="mt-1 text-sm text-muted-foreground">{text.description}</p></div>
      <div className="flex gap-2"><Button variant="outline" size="icon" aria-label={copy.retry} disabled={skills.isFetching || pending} onClick={() => void skills.refetch()}><RefreshCw className={skills.isFetching ? "animate-spin" : undefined} /></Button><Button disabled={pending} onClick={() => setEditor({ skill: null, input: { ...emptySkill, dependsOn: [] } })}><Plus />{text.add}</Button></div>
    </div>
    <div className="divide-y rounded-lg border">
      {skills.isPending ? <p className="p-5 text-center text-sm text-muted-foreground">{copy.loading}</p> : records.map(skill => {
        const active = skill.id === skills.data?.mailSummarySkillId;
        return <article key={skill.id} className="grid gap-3 p-4" data-skill-id={skill.id}>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h3 className="break-words text-sm font-semibold">{skill.name}</h3><Badge variant="outline">{skill.kind === "chat" ? text.chat : text.summary}</Badge>{skill.builtIn && <Badge variant="secondary">{text.builtIn}</Badge>}{active && <Badge><Check className="size-3" />{text.activeSummary}</Badge>}</div><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{skill.id}</p></div>
            <div className="flex items-center gap-1"><Switch checked={skill.enabled} disabled={pending || active} aria-label={`${text.enabled} ${skill.name}`} onCheckedChange={() => toggle.mutate(skill)} /><Button variant="ghost" size="icon" aria-label={`${text.edit} ${skill.name}`} disabled={pending} onClick={() => setEditor({ skill, input: { name: skill.name, description: skill.description, instructions: skill.instructions, kind: skill.kind, dependsOn: [...skill.dependsOn], enabled: skill.enabled } })}><Pencil /></Button><Button variant="ghost" size="icon" className="text-destructive" aria-label={`${text.remove} ${skill.name}`} disabled={pending || active} onClick={() => setRemoving(skill)}><Trash2 /></Button></div>
          </div>
          {skill.description && <p className="break-words text-sm text-muted-foreground">{skill.description}</p>}
          {skill.dependsOn.length > 0 && <p className="break-words text-xs text-muted-foreground">{text.dependencies}: {skill.dependsOn.map(id => records.find(item => item.id === id)?.name || id).join(" · ")}</p>}
          {skill.kind === "mail_summary" && !active && <Button className="justify-self-start" size="sm" variant="outline" disabled={pending || !skill.enabled} onClick={() => bind.mutate(skill)}>{text.useSummary}</Button>}
        </article>;
      })}
      {skills.data?.skills.length === 0 && <p className="p-5 text-center text-sm text-muted-foreground">{text.empty}</p>}
    </div>
    <div className="grid gap-2 rounded-lg border bg-muted/30 p-4 text-sm text-muted-foreground"><p>{text.defaultHint}</p><p>{text.api} <a className="underline underline-offset-4" href="/api/automation/reference?section=Skills" target="_blank" rel="noreferrer">{text.apiLink}</a></p><code className="break-all text-xs">/api/settings/ai/skills</code></div>
    <Dialog open={Boolean(editor)} onOpenChange={open => { if (!open && !save.isPending) setEditor(null); }}>
      <DialogContent className="flex max-h-[calc(100dvh-2rem)] w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="shrink-0 border-b px-5 py-4 pr-12"><DialogTitle>{editor?.skill ? text.edit : text.add}</DialogTitle><DialogDescription>{text.description}</DialogDescription></DialogHeader>
        <form className="grid min-h-0 gap-4 overflow-y-auto overscroll-contain p-5" onSubmit={submit}>
          <div className="grid gap-2"><Label htmlFor="skill-name">{text.name}</Label><Input id="skill-name" value={editor?.input.name || ""} maxLength={100} disabled={save.isPending} required onChange={event => patch({ name: event.target.value })} /></div>
          {!editor?.skill && <div className="grid gap-2"><Label htmlFor="skill-id">{text.id}</Label><Input id="skill-id" value={editor?.input.id || ""} maxLength={64} pattern="[a-z][a-z0-9_-]*" disabled={save.isPending} onChange={event => patch({ id: event.target.value })} /><p className="text-xs text-muted-foreground">{text.idHint}</p></div>}
          <div className="grid gap-2"><Label>{text.kind}</Label><ProductSelect label={text.kind} value={editor?.input.kind || "chat"} disabled={save.isPending || Boolean(editor?.skill)} onValueChange={kind => patch({ kind: kind as AISkillInput["kind"], dependsOn: [] })} options={[{ value: "chat", label: text.chat }, { value: "mail_summary", label: text.summary }]} /></div>
          <div className="grid gap-2"><Label htmlFor="skill-description">{text.descriptionLabel}</Label><Input id="skill-description" value={editor?.input.description || ""} maxLength={400} disabled={save.isPending} onChange={event => patch({ description: event.target.value })} /></div>
          <fieldset className="grid gap-2 rounded-lg border p-3"><legend className="px-1 text-sm font-medium">{text.dependencies}</legend>{dependencyOptions.length ? dependencyOptions.map(skill => <div className="flex items-center justify-between gap-3 text-sm" key={skill.id}><span className="min-w-0 break-words">{skill.name}{skill.missing && <span className="block text-xs text-destructive">{text.missing}</span>}</span><Switch checked={selectedDependencies.includes(skill.id)} aria-label={`${text.dependencies} ${skill.name}`} disabled={save.isPending} onCheckedChange={checked => patch({ dependsOn: checked ? [...selectedDependencies, skill.id] : selectedDependencies.filter(id => id !== skill.id) })} /></div>) : <p className="text-xs text-muted-foreground">{text.noDependencies}</p>}</fieldset>
          <div className="grid gap-2"><Label htmlFor="skill-instructions">{text.instructions}</Label><Textarea id="skill-instructions" className="min-h-52 resize-y font-mono text-sm" value={editor?.input.instructions || ""} disabled={save.isPending} required onChange={event => patch({ instructions: event.target.value })} /></div>
          <div className="flex items-center justify-between"><Label htmlFor="skill-enabled">{text.enabled}</Label><Switch id="skill-enabled" checked={editor?.input.enabled ?? true} disabled={save.isPending || editor?.skill?.id === skills.data?.mailSummarySkillId} onCheckedChange={enabled => patch({ enabled })} /></div>
          <DialogFooter><Button type="button" variant="ghost" disabled={save.isPending} onClick={() => setEditor(null)}>{copy.cancel}</Button><Button type="submit" disabled={save.isPending}>{save.isPending && <LoaderCircle className="animate-spin" />}{copy.save}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
    <Dialog open={Boolean(removing)} onOpenChange={open => { if (!open && !remove.isPending) setRemoving(null); }}>
      <DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>{text.remove}</DialogTitle><DialogDescription>{text.removeDescription}</DialogDescription></DialogHeader><p className="break-words text-sm font-medium">{removing?.name}</p><DialogFooter><Button variant="ghost" disabled={remove.isPending} onClick={() => setRemoving(null)}>{copy.cancel}</Button><Button variant="destructive" disabled={remove.isPending} onClick={() => { if (removing) remove.mutate(removing); }}>{remove.isPending && <LoaderCircle className="animate-spin" />}{text.remove}</Button></DialogFooter></DialogContent>
    </Dialog>
  </section>;
}
