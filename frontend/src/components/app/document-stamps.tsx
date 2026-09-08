import { useRef, useState } from "react";
import { Stamp, Upload, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "../ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";

export type DocumentStamp = { id: string; name: string; value: string; width: number; height: number };
const STORAGE_KEY = "inbrix-document-stamps";

function readStamps(): DocumentStamp[] {
  const stored: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) || "[]");
  if (!Array.isArray(stored)) throw new Error("Invalid stamp library");
  return stored.filter((item: unknown): item is DocumentStamp => {
    if (!item || typeof item !== "object") return false;
    const stamp = item as Partial<DocumentStamp>;
    return typeof stamp.id === "string" && typeof stamp.name === "string" &&
      typeof stamp.value === "string" && stamp.value.startsWith("data:image/png;base64,") &&
      typeof stamp.width === "number" && Number.isFinite(stamp.width) && stamp.width > 0 &&
      typeof stamp.height === "number" && Number.isFinite(stamp.height) && stamp.height > 0;
  });
}

async function prepareStamp(file: File): Promise<DocumentStamp> {
  const bitmap = await createImageBitmap(file);
  try {
    const scale = Math.min(1, 600 / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("Canvas unavailable");
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    return { id: crypto.randomUUID(), name: file.name.replace(/\.[^.]+$/, ""), value: canvas.toDataURL("image/png"), width: canvas.width, height: canvas.height };
  } finally {
    bitmap.close();
  }
}

export function DocumentStampManager({ chinese, disabled, onInsert }: {
  chinese: boolean; disabled?: boolean; onInsert?: (stamp: DocumentStamp, width: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const [stamps, setStamps] = useState<DocumentStamp[]>([]);
  const [busy, setBusy] = useState(false);
  const [width, setWidth] = useState(40);
  const inputRef = useRef<HTMLInputElement>(null);
  const label = chinese ? "印章管理" : "Stamp library";
  const storageError = chinese ? "印章库保存失败，请检查浏览器存储空间或权限。" : "Could not save stamps. Check browser storage space and permissions.";
  const persist = (next: DocumentStamp[]) => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
      setStamps(next);
      return true;
    } catch {
      toast.error(storageError);
      return false;
    }
  };
  const upload = async (file?: File) => {
    if (!file) return;
    if (!/^image\/(png|jpeg|webp)$/.test(file.type) || file.size > 3 * 1024 * 1024) {
      toast.error(chinese ? "请选择不超过 3 MB 的 PNG、JPG 或 WebP 图片。" : "Choose a PNG, JPG or WebP image up to 3 MB.");
      return;
    }
    setBusy(true);
    try {
      const stamp = await prepareStamp(file);
      if (persist([...stamps, stamp])) toast.success(chinese ? "印章已添加" : "Stamp added");
    } catch {
      toast.error(chinese ? "图片无法读取，请选择有效的印章图片。" : "Unable to read this image. Choose a valid stamp image.");
    } finally { setBusy(false); }
  };
  return <>
    <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => {
      try { setStamps(readStamps()); setOpen(true); }
      catch { toast.error(chinese ? "无法读取印章库，请检查浏览器存储。" : "Unable to read the stamp library. Check browser storage."); }
    }}><Stamp />{label}</Button>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader><DialogTitle>{label}</DialogTitle><DialogDescription>{chinese
          ? "印章保存在当前浏览器，供报价单与合同共用。推荐上传透明背景 PNG；删除印章不会影响已保存的文档。"
          : "Stamps are stored in this browser and shared by quotations and contracts. Transparent PNG is recommended. Deleting a stamp does not change saved documents."}</DialogDescription></DialogHeader>
        <div className="flex flex-wrap items-end gap-3">
          <input ref={inputRef} type="file" className="sr-only" accept="image/png,image/jpeg,image/webp" aria-label={chinese ? "上传印章图片" : "Upload stamp image"} onChange={(event) => { const file = event.target.files?.[0]; event.target.value = ""; void upload(file); }} />
          <Button type="button" variant="outline" disabled={busy} onClick={() => inputRef.current?.click()}><Upload />{busy ? (chinese ? "上传中…" : "Uploading…") : (chinese ? "上传印章" : "Upload stamp")}</Button>
          {onInsert && <Label className="grid gap-1">{chinese ? "插入宽度（毫米）" : "Insert width (mm)"}<Input type="number" min={10} max={100} value={width || ""} onChange={(event) => setWidth(Number(event.target.value))} className="w-32" /></Label>}
        </div>
        {onInsert && <p className="text-sm text-muted-foreground">{chinese ? "插入前先点击盖章位置；插入后可拖动印章、拖拽边框调整大小。" : "Click the stamping position first. After insertion, drag the stamp to move it or drag its handles to resize."}</p>}
        {!stamps.length && <p className="py-8 text-center text-sm text-muted-foreground">{chinese ? "暂无印章，上传后即可在文档中使用。" : "Upload your first stamp to use it in documents."}</p>}
        <div className="grid gap-3">{stamps.map((stamp) => <div key={stamp.id} className="flex items-center gap-3 rounded-lg border p-3">
          <img src={stamp.value} alt={stamp.name} className="size-20 shrink-0 rounded bg-white object-contain p-1" />
          <div className="grid min-w-0 flex-1 gap-2">
            <Input aria-label={chinese ? "印章名称" : "Stamp name"} defaultValue={stamp.name} maxLength={80} disabled={busy} onBlur={(event) => {
              const name = event.target.value.trim() || stamp.name;
              if (name !== stamp.name && !persist(stamps.map((item) => item.id === stamp.id ? { ...item, name } : item))) event.target.value = stamp.name;
              else event.target.value = name;
            }} />
            <div className="flex gap-2">
              {onInsert && <Button type="button" size="sm" disabled={busy || !Number.isFinite(width) || width < 10 || width > 100} onClick={() => {
                try { onInsert(stamp, width * 794 / 210); setOpen(false); }
                catch { toast.error(chinese ? "插入失败，请先在文档中选择盖章位置。" : "Select a position in the document and try again."); }
              }}>{chinese ? "插入文档" : "Insert into document"}</Button>}
              <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => {
                if (window.confirm(chinese ? `删除印章“${stamp.name}”？` : `Delete stamp “${stamp.name}”?`)) persist(stamps.filter((item) => item.id !== stamp.id));
              }}><Trash2 />{chinese ? "删除" : "Delete"}</Button>
            </div>
          </div>
        </div>)}</div>
      </DialogContent>
    </Dialog>
  </>;
}
