import { useState } from "react";
import type { Editor } from "@tiptap/core";
import { closeHistory } from "@tiptap/pm/history";
import { TextSelection } from "@tiptap/pm/state";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FileText, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { getReplyTemplates, saveReplyTemplates, type ReplyTemplate } from "../../lib/api";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Textarea } from "../ui/textarea";

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

export function ReplyTemplates({
  editor,
  chinese,
  disabled,
  accountEmail,
}: {
  editor: Editor | null;
  chinese: boolean;
  disabled?: boolean;
  accountEmail?: string;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [draft, setDraft] = useState<ReplyTemplate | null>(null);
  const [dirty, setDirty] = useState(false);
  const text = (zh: string, en: string) => chinese ? zh : en;
  const discardDraft = () => !dirty || window.confirm(text("放弃尚未保存的模板修改？", "Discard unsaved template changes?"));

  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["reply-templates", accountEmail],
    queryFn: getReplyTemplates,
    enabled: open,
    staleTime: 60_000,
  });

  const saveMutation = useMutation({
    mutationFn: saveReplyTemplates,
    onSuccess: (data) => {
      queryClient.setQueryData(["reply-templates", accountEmail], data);
    },
  });

  const templates: ReplyTemplate[] = query.data?.configured
    ? query.data.templates
    : defaults(chinese);

  const edit = (next: ReplyTemplate) => {
    if (discardDraft()) { setDraft(next); setDirty(false); }
  };

  const handleOpen = () => {
    setSearch("");
    setDraft(null);
    setDirty(false);
    setOpen(true);
  };

  const handleSaveDraft = () => {
    if (!draft || !draft.name.trim() || !draft.body.trim()) return;
    const saved = { ...draft, id: draft.id || crypto.randomUUID(), name: draft.name.trim(), body: draft.body.trim() };
    const next = [saved, ...templates.filter((item) => item.id !== saved.id)];
    saveMutation.mutate(next, {
      onSuccess: () => {
        setDraft(saved);
        setDirty(false);
        setSearch("");
        toast.success(text("模板已保存", "Template saved"));
      },
      onError: () => {
        toast.error(text("模板保存失败，请检查网络连接或稍后重试。", "Could not save templates. Please check your network connection and try again."));
      },
    });
  };

  const handleDelete = (template: ReplyTemplate) => {
    if (!window.confirm(text(`删除模板“${template.name}”？`, `Delete template “${template.name}”?`))) return;
    const next = templates.filter((item) => item.id !== template.id);
    saveMutation.mutate(next, {
      onSuccess: () => {
        if (draft?.id === template.id) {
          setDraft(null);
          setDirty(false);
        }
      },
      onError: () => {
        toast.error(text("删除模板失败，请检查网络连接或稍后重试。", "Could not delete template. Please check your network connection and try again."));
      },
    });
  };

  return <>
    <Button
      type="button"
      variant="ghost"
      size="sm"
      disabled={disabled || !editor}
      onClick={handleOpen}
    >
      <FileText />{text("回复模板", "Reply templates")}
    </Button>
    <Dialog open={open} onOpenChange={(value) => { if (value || discardDraft()) setOpen(value); }}>
      <DialogContent data-testid="reply-template-dialog" className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{text("回复模板", "Reply templates")}</DialogTitle>
          <DialogDescription>
            {text(
              "选择模板替换回复正文，保留签名、原邮件引用及附件，可撤销。模板已云端同步至您的账户，可在各设备使用。",
              "Choose a template to replace the reply body, keeping the signature, quoted email and attachments. You can undo this. Templates are synced to your account across devices."
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={saveMutation.isPending}
            onClick={() => edit({ id: "", name: "", body: "" })}
          >
            <Plus />{text("新建模板", "New template")}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={saveMutation.isPending}
            onClick={() => {
              if (!editor) return;
              const body = currentReplyText(editor);
              if (!body) { toast.error(text("当前回复正文为空。", "The reply body is empty.")); return; }
              edit({ id: "", name: "", body });
            }}
          >
            {text("将当前正文存为模板", "Save current reply as template")}
          </Button>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid content-start gap-2">
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={text("搜索模板", "Search templates")}
              aria-label={text("搜索模板", "Search templates")}
            />
            <div className="grid max-h-80 gap-2 overflow-y-auto">
              {query.isPending && (
                <div className="flex items-center justify-center py-6 text-muted-foreground gap-2">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span>{text("正在加载模板...", "Loading templates...")}</span>
                </div>
              )}
              {!query.isPending && templates.filter((template) => `${template.name}\n${template.body}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())).map((template) => <div key={template.id} className="rounded-lg border p-3">
                <p className="truncate font-medium">{template.name}</p>
                <p className="my-2 line-clamp-3 whitespace-pre-wrap text-xs text-muted-foreground">{template.body}</p>
                <div className="flex gap-1">
                  <Button
                    type="button"
                    size="sm"
                    aria-label={`${text("使用模板", "Use template")}: ${template.name}`}
                    onClick={() => {
                      if (!editor || !discardDraft()) return;
                      fillReply(editor, template.body);
                      setOpen(false);
                      toast.success(text("已填充回复模板", "Reply template applied"), { action: { label: text("撤销", "Undo"), onClick: () => { if (!editor.isDestroyed) editor.commands.undo(); } } });
                    }}
                  >
                    {text("使用模板", "Use template")}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    disabled={saveMutation.isPending}
                    aria-label={`${text("编辑模板", "Edit template")}: ${template.name}`}
                    onClick={() => edit({ ...template })}
                  >
                    <Pencil />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    disabled={saveMutation.isPending}
                    aria-label={`${text("删除模板", "Delete template")}: ${template.name}`}
                    onClick={() => handleDelete(template)}
                  >
                    <Trash2 />
                  </Button>
                </div>
              </div>)}
              {!query.isPending && !templates.some((template) => `${template.name}\n${template.body}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())) && (
                <p className="py-6 text-center text-muted-foreground">{text("暂无匹配的模板", "No matching templates")}</p>
              )}
            </div>
          </div>
          {draft ? <div className="grid content-start gap-3">
            <Label className="grid gap-1.5">{text("模板名称", "Template name")}<Input aria-label={text("模板名称", "Template name")} value={draft.name} maxLength={80} onChange={(event) => { setDraft({ ...draft, name: event.target.value }); setDirty(true); }} /></Label>
            <Label className="grid gap-1.5">{text("模板正文", "Template body")}<Textarea aria-label={text("模板正文", "Template body")} className="min-h-56" value={draft.body} maxLength={20000} onChange={(event) => { setDraft({ ...draft, body: event.target.value }); setDirty(true); }} /></Label>
            <Button
              type="button"
              disabled={saveMutation.isPending || !draft.name.trim() || !draft.body.trim()}
              onClick={handleSaveDraft}
            >
              {saveMutation.isPending ? text("保存中...", "Saving...") : text("保存模板", "Save template")}
            </Button>
          </div> : <p className="rounded-lg bg-muted/40 p-4 text-sm text-muted-foreground">{text("可直接使用左侧模板，或新建、编辑自己的常用回复。发送报价单模板前，请先添加实际报价附件。", "Use a template from the list or create your own replies. Attach the actual quotation before using the quotation reply template.")}</p>}
        </div>
      </DialogContent>
    </Dialog>
  </>;
}
