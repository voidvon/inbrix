import { useEffect, useRef, useState } from "react";
import { isAIImage, isAllowedAIAttachment } from "./ai-attachment-types";
import type { AIChatAttachment } from "../types";
import { deleteAIChatAttachment, uploadAIChatAttachment } from "./api";

export type PendingAIAttachment = {
  key: string;
  filename: string;
  preview: string;
  size: number;
  attachment?: AIChatAttachment;
  error?: string;
};

// Unsent uploads belong to a mailbox + chat draft. Scope changes cancel uploads
// and remove abandoned files; sent attachments are retained by lightweight references.
export function useAIChatAttachments(accountEmail: string, scope: string, isZh: boolean) {
  const [items, setItems] = useState<PendingAIAttachment[]>([]);
  const [error, setError] = useState<string | null>(null);
  const draft = useRef<PendingAIAttachment[]>([]);
  const current = useRef(`${accountEmail}:${scope}`);
  const scopeKey = `${accountEmail}:${scope}`;
  current.current = scopeKey;
  const generation = useRef(0);
  const submitted = useRef(new Set<string>());
  const controllers = useRef(new Map<string, AbortController>());
  const update = (value: PendingAIAttachment[]) => { draft.current = value; setItems(value); };
  useEffect(() => {
    setItems([]);
    setError(null);
    return () => {
      generation.current++;
      for (const controller of controllers.current.values()) controller.abort();
      controllers.current.clear();
      for (const item of draft.current) {
        URL.revokeObjectURL(item.preview);
        if (item.attachment && !submitted.current.has(item.attachment.id)) void deleteAIChatAttachment(accountEmail, item.attachment.id).catch(() => {});
      }
      draft.current = [];
    };
  }, [scopeKey, accountEmail]);

  const addFiles = (files: File[]) => {
    if (!accountEmail) { setError(isZh ? "请先选择邮箱" : "Select a mailbox first"); return; }
    setError(null);
    if (draft.current.length + files.length > 6) { setError(isZh ? "每条消息最多添加 6 个附件" : "Up to 6 attachments per message"); return; }
    const batch = generation.current;
    for (const file of files) {
      if (!isAllowedAIAttachment(file) || !file.size || file.size > 10 * 1024 * 1024) {
        setError(isZh ? "支持图片、PDF、Word、Excel，每个附件最大 10 MB" : "Images, PDF, Word or Excel, up to 10 MB each");
        continue;
      }
      const key = crypto.randomUUID();
      const controller = new AbortController();
      controllers.current.set(key, controller);
      update([...draft.current, { key, filename: file.name, size: file.size, preview: isAIImage(file.type) || /\.(png|jpe?g|webp)$/i.test(file.name) ? URL.createObjectURL(file) : "" }]);
      void uploadAIChatAttachment(accountEmail, file, controller.signal).then(attachment => {
        if (current.current !== scopeKey || batch !== generation.current || !draft.current.some(item => item.key === key)) {
          void deleteAIChatAttachment(accountEmail, attachment.id).catch(() => {});
          return;
        }
        update(draft.current.map(item => item.key === key ? { ...item, attachment } : item));
      }).catch(err => {
        if (current.current !== scopeKey || batch !== generation.current || controller.signal.aborted) return;
        update(draft.current.map(item => item.key === key ? { ...item, error: err instanceof Error ? err.message : "Upload failed" } : item));
      }).finally(() => { controllers.current.delete(key); });
    }
  };
  const remove = (key: string) => {
    controllers.current.get(key)?.abort();
    const item = draft.current.find(item => item.key === key);
    if (item) {
      URL.revokeObjectURL(item.preview);
      if (item.attachment && !submitted.current.has(item.attachment.id)) void deleteAIChatAttachment(accountEmail, item.attachment.id).catch(() => {});
    }
    update(draft.current.filter(item => item.key !== key));
    setError(null);
  };
  const retain = () => { for (const item of draft.current) if (item.attachment) submitted.current.add(item.attachment.id); };
  const discard = () => { for (const item of [...draft.current]) remove(item.key); };
  const clear = () => {
    for (const item of draft.current) URL.revokeObjectURL(item.preview);
    update([]);
    setError(null);
  };
  return {
    items, error, addFiles, remove, clear, retain, discard,
    attachments: items.flatMap(item => item.attachment ? [item.attachment] : []),
    blocked: items.some(item => !item.attachment),
  };
}
