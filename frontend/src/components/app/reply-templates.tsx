import { useState } from "react";
import type { Editor } from "@tiptap/core";
import { closeHistory } from "@tiptap/pm/history";
import { TextSelection } from "@tiptap/pm/state";
import { FileText, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Textarea } from "../ui/textarea";

type ReplyTemplate = { id: string; name: string; body: string };
const STORAGE_KEY = "inbrix-reply-templates";
const isProtected = (name: string) => name === "replyQuote" || name === "emailSignature";

function defaults(chinese: boolean): ReplyTemplate[] {
  return chinese ? [
    { id: "quotation", name: "发送报价单", body: "您好，\n\n感谢您的询价，请查收附件中的报价单。\n如有任何问题或需要调整的地方，欢迎随时与我联系。\n\n期待您的回复，谢谢！" },
    { id: "received", name: "确认收到", body: "您好，\n\n您的邮件已收到，感谢您提供的信息。\n我们会尽快核实相关内容，并及时向您反馈。\n\n谢谢！" },
    { id: "follow-up", name: "进度跟进", body: "您好，\n\n想跟进一下此前沟通事项的最新进展。\n如需我们补充资料或提供协助，请随时告知。\n\n期待您的回复，谢谢！" },
  ] : [
    { id: "quotation", name: "Send quotation", body: "Hello,\n\nThank you for your inquiry. Please find our quotation attached.\nPlease let me know if you have any questions or would like any changes.\n\nThank you, and I look forward to hearing from you." },
    { id: "received", name: "Acknowledge receipt", body: "Hello,\n\nThank you for your email and the information provided.\nWe will review the details and get back to you shortly.\n\nThank you!" },
    { id: "follow-up", name: "Follow up", body: "Hello,\n\nI am following up on our previous discussion.\nPlease let me know if you have any updates or need additional information from us.\n\nI look forward to hearing from you." },
  ];
}

function readTemplates(chinese: boolean): ReplyTemplate[] {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (raw === null) return defaults(chinese);
  const value: unknown = JSON.parse(raw);
  if (!Array.isArray(value)) throw new Error("Invalid template library");
  return value.filter((item: unknown): item is ReplyTemplate => {
    if (!item || typeof item !== "object") return false;
    const template = item as Partial<ReplyTemplate>;
    return typeof template.id === "string" && typeof template.name === "string" && !!template.name.trim() &&
      typeof template.body === "string" && !!template.body.trim();
  });
}

function currentReplyText(editor: Editor) {
  const blocks: string[] = [];
  editor.state.doc.forEach((node) => {
    if (!isProtected(node.type.name)) blocks.push(node.textBetween(0, node.content.size, "\n", "\n"));
  });
  return blocks.join("\n").trim();
}

function fillReply(editor: Editor, body: string) {
  // One history step replaces only the reply, preserving the original quote and signature.
  const { state } = editor;
  const preserved: typeof state.doc[] = [];
  state.doc.forEach((node) => { if (isProtected(node.type.name)) preserved.push(node); });
  const paragraphs = body.replace(/\r\n?/g, "\n").split("\n").map((line) =>
    state.schema.nodes.paragraph.create(null, line ? state.schema.text(line) : undefined));
  const transaction = closeHistory(state.tr).replaceWith(0, state.doc.content.size, [...paragraphs, ...preserved]);
  transaction.setSelection(TextSelection.near(transaction.doc.resolve(1)));
  editor.view.dispatch(transaction);
  editor.view.dispatch(closeHistory(editor.state.tr));
}

export function ReplyTemplates({ editor, chinese, disabled }: { editor: Editor | null; chinese: boolean; disabled?: boolean }) {
  const [open, setOpen] = useState(false);
  const [templates, setTemplates] = useState<ReplyTemplate[]>([]);
  const [search, setSearch] = useState("");
  const [draft, setDraft] = useState<ReplyTemplate | null>(null);
  const [dirty, setDirty] = useState(false);
  const text = (zh: string, en: string) => chinese ? zh : en;
  const discardDraft = () => !dirty || window.confirm(text("放弃尚未保存的模板修改？", "Discard unsaved template changes?"));
  const persist = (next: ReplyTemplate[]) => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(next)); setTemplates(next); return true; }
    catch { toast.error(text("模板保存失败，请检查浏览器存储空间或权限。", "Could not save templates. Check browser storage space and permissions.")); return false; }
  };
  const edit = (next: ReplyTemplate) => {
    if (discardDraft()) { setDraft(next); setDirty(false); }
  };
  return <>
    <Button type="button" variant="ghost" size="sm" disabled={disabled || !editor} onClick={() => {
      try { setTemplates(readTemplates(chinese)); setSearch(""); setDraft(null); setDirty(false); setOpen(true); }
      catch { toast.error(text("无法读取回复模板，请检查浏览器存储。", "Unable to read reply templates. Check browser storage.")); }
    }}><FileText />{text("回复模板", "Reply templates")}</Button>
    <Dialog open={open} onOpenChange={(value) => { if (value || discardDraft()) setOpen(value); }}>
      <DialogContent data-testid="reply-template-dialog" className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader><DialogTitle>{text("回复模板", "Reply templates")}</DialogTitle><DialogDescription>{text("选择模板替换回复正文，保留签名、原邮件引用及附件，可撤销。模板以纯文本保存在当前浏览器。", "Choose a template to replace the reply body, keeping the signature, quoted email and attachments. You can undo this. Templates are saved as plain text in this browser.")}</DialogDescription></DialogHeader>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => edit({ id: "", name: "", body: "" })}><Plus />{text("新建模板", "New template")}</Button>
          <Button type="button" variant="outline" size="sm" onClick={() => {
            if (!editor) return;
            const body = currentReplyText(editor);
            if (!body) { toast.error(text("当前回复正文为空。", "The reply body is empty.")); return; }
            edit({ id: "", name: "", body });
          }}>{text("将当前正文存为模板", "Save current reply as template")}</Button>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid content-start gap-2">
            <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder={text("搜索模板", "Search templates")} aria-label={text("搜索模板", "Search templates")} />
            <div className="grid max-h-80 gap-2 overflow-y-auto">
              {templates.filter((template) => `${template.name}\n${template.body}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())).map((template) => <div key={template.id} className="rounded-lg border p-3">
                <p className="truncate font-medium">{template.name}</p>
                <p className="my-2 line-clamp-3 whitespace-pre-wrap text-xs text-muted-foreground">{template.body}</p>
                <div className="flex gap-1">
                  <Button type="button" size="sm" aria-label={`${text("使用模板", "Use template")}: ${template.name}`} onClick={() => {
                    if (!editor || !discardDraft()) return;
                    fillReply(editor, template.body); setOpen(false);
                    toast.success(text("已填充回复模板", "Reply template applied"), { action: { label: text("撤销", "Undo"), onClick: () => { if (!editor.isDestroyed) editor.commands.undo(); } } });
                  }}>{text("使用模板", "Use template")}</Button>
                  <Button type="button" variant="ghost" size="icon-sm" aria-label={`${text("编辑模板", "Edit template")}: ${template.name}`} onClick={() => edit({ ...template })}><Pencil /></Button>
                  <Button type="button" variant="ghost" size="icon-sm" aria-label={`${text("删除模板", "Delete template")}: ${template.name}`} onClick={() => {
                    if (window.confirm(text(`删除模板“${template.name}”？`, `Delete template “${template.name}”?`)) && persist(templates.filter((item) => item.id !== template.id))) {
                      if (draft?.id === template.id) { setDraft(null); setDirty(false); }
                    }
                  }}><Trash2 /></Button>
                </div>
              </div>)}
              {!templates.some((template) => `${template.name}\n${template.body}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())) && <p className="py-6 text-center text-muted-foreground">{text("暂无匹配的模板", "No matching templates")}</p>}
            </div>
          </div>
          {draft ? <div className="grid content-start gap-3">
            <Label className="grid gap-1.5">{text("模板名称", "Template name")}<Input aria-label={text("模板名称", "Template name")} value={draft.name} maxLength={80} onChange={(event) => { setDraft({ ...draft, name: event.target.value }); setDirty(true); }} /></Label>
            <Label className="grid gap-1.5">{text("模板正文", "Template body")}<Textarea aria-label={text("模板正文", "Template body")} className="min-h-56" value={draft.body} maxLength={20000} onChange={(event) => { setDraft({ ...draft, body: event.target.value }); setDirty(true); }} /></Label>
            <Button type="button" disabled={!draft.name.trim() || !draft.body.trim()} onClick={() => {
              const saved = { ...draft, id: draft.id || crypto.randomUUID(), name: draft.name.trim(), body: draft.body.trim() };
              if (persist([saved, ...templates.filter((template) => template.id !== saved.id)])) { setDraft(saved); setDirty(false); setSearch(""); toast.success(text("模板已保存", "Template saved")); }
            }}>{text("保存模板", "Save template")}</Button>
          </div> : <p className="rounded-lg bg-muted/40 p-4 text-sm text-muted-foreground">{text("可直接使用左侧模板，或新建、编辑自己的常用回复。发送报价单模板前，请先添加实际报价附件。", "Use a template from the list or create your own replies. Attach the actual quotation before using the quotation reply template.")}</p>}
        </div>
      </DialogContent>
    </Dialog>
  </>;
}
