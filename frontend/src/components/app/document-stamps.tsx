import { useEffect, useRef, useState } from "react";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { Stamp, Upload, Trash2, X, Loader2 } from "lucide-react";
import { toast } from "../../lib/toast";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { listDocumentStamps, saveDocumentStamp, deleteDocumentStamp, type ServerDocumentStamp } from "../../lib/api";

export type DocumentStamp = {
  id: string;
  name: string;
  value: string;
  width: number;
  height: number;
  insertWidth?: number; // width in mm, default 40
};

const DEFAULT_STAMP_WIDTH_MM = 40;

function toStamp(stamp: ServerDocumentStamp): DocumentStamp {
  return { id: stamp.id, name: stamp.name, value: stamp.dataUrl || stamp.value || "", width: stamp.width || 100, height: stamp.height || 100, insertWidth: stamp.insertWidth || DEFAULT_STAMP_WIDTH_MM };
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
    return {
      id: crypto.randomUUID(),
      name: file.name.replace(/\.[^.]+$/, ""),
      value: canvas.toDataURL("image/png"),
      width: canvas.width,
      height: canvas.height,
      insertWidth: DEFAULT_STAMP_WIDTH_MM,
    };
  } finally {
    bitmap.close();
  }
}

export function DocumentStampManager({
  chinese,
  disabled,
  onInsert,
}: {
  chinese: boolean;
  disabled?: boolean;
  onInsert?: (stamp: DocumentStamp, width: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const [stamps, setStamps] = useState<DocumentStamp[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const label = chinese ? "印章管理" : "Stamp library";

  useEffect(() => {
    if (!open) { setStamps([]); return; }
    let active = true;
    setStamps([]);
    setLoading(true);
    void listDocumentStamps().then(({ stamps }) => {
      if (!Array.isArray(stamps)) throw new Error(chinese ? "印章数据无效" : "Invalid stamp response");
      if (active) setStamps(stamps.map(toStamp));
    }).catch(error => {
      if (active) toast.error(error instanceof Error ? error.message : chinese ? "印章加载失败" : "Failed to load stamps");
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [open, chinese]);

  const remove = async (stamp: DocumentStamp) => {
    setBusy(true);
    try {
      await deleteDocumentStamp(stamp.id);
      setStamps(current => current.filter(item => item.id !== stamp.id));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : chinese ? "删除印章失败" : "Failed to delete stamp");
    } finally { setBusy(false); }
  };
  const updateWidth = async (stamp: DocumentStamp, input: HTMLInputElement) => {
    const width = Math.round(Number(input.value));
    const previous = stamp.insertWidth || DEFAULT_STAMP_WIDTH_MM;
    if (!Number.isFinite(width) || width < 10 || width > 150) { input.value = String(previous); return; }
    if (width === previous) return;
    setBusy(true);
    try {
      const { stamp: saved } = await saveDocumentStamp({ ...stamp, insertWidth: width });
      setStamps(current => current.map(item => item.id === saved.id ? toStamp(saved) : item));
      input.value = String(saved.insertWidth);
    } catch (error) {
      input.value = String(previous);
      toast.error(error instanceof Error ? error.message : chinese ? "印章宽度保存失败" : "Failed to save stamp width");
    } finally { setBusy(false); }
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
      const { stamp: saved } = await saveDocumentStamp({
        id: stamp.id,
        name: stamp.name,
        value: stamp.value,
        width: stamp.width,
        height: stamp.height,
        insertWidth: stamp.insertWidth,
        aspectRatio: stamp.width / stamp.height,
      });
      setStamps(current => [...current, toStamp(saved)]);
      toast.success(chinese ? "印章已添加" : "Stamp added");
    } catch (err) {
      console.error("Stamp upload/save failed", err);
      toast.error(chinese ? "印章保存失败，请检查网络或服务端连接。" : "Failed to save stamp.");
    } finally {
      setBusy(false);
    }
  };

  const insert = (stamp: DocumentStamp) => {
    const widthMm =
      typeof stamp.insertWidth === "number" && Number.isFinite(stamp.insertWidth) && stamp.insertWidth > 0
        ? stamp.insertWidth
        : DEFAULT_STAMP_WIDTH_MM;
    const pxWidth = (widthMm * 794) / 210;
    onInsert?.(stamp, pxWidth);
    setOpen(false);
  };

  return (
    <>
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={disabled}
        onClick={() => {
          setOpen(true);
        }}
      >
        <Stamp />
        <span>{label}</span>
      </Button>
      <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
        <DialogPrimitive.Portal>
          {/* Modal backdrop overlay: forceRender ensures it displays even when nested inside document dialog */}
          <DialogPrimitive.Backdrop
            forceRender
            className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs duration-150 data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0"
          />
          <DialogPrimitive.Popup
            data-slot="dialog-content"
            className="fixed top-1/2 left-1/2 z-50 grid w-full max-w-[calc(100%-1.5rem)] sm:max-w-xl -translate-x-1/2 -translate-y-1/2 gap-4 rounded-xl bg-popover p-4 sm:p-5 text-sm text-popover-foreground shadow-xl ring-1 ring-foreground/10 duration-150 outline-none max-h-[calc(100dvh-2rem)] overflow-y-auto data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95"
          >
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <DialogPrimitive.Title className="text-base font-semibold text-foreground">
                  {label}
                </DialogPrimitive.Title>
                {loading && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
              </div>
              <DialogPrimitive.Close render={<Button variant="ghost" size="icon" className="size-8" />}>
                <X className="size-4" />
                <span className="sr-only">Close</span>
              </DialogPrimitive.Close>
            </div>

            {/* Upload Button: simplified without outer border and padding */}
            <div className="flex items-center">
              <input
                ref={inputRef}
                type="file"
                className="sr-only"
                accept="image/png,image/jpeg,image/webp"
                aria-label={chinese ? "上传印章图片" : "Upload stamp image"}
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  void upload(file);
                }}
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={busy || loading}
                onClick={() => inputRef.current?.click()}
                className="gap-1.5"
              >
                <Upload className="size-4" />
                <span>{busy ? (chinese ? "上传中…" : "Uploading…") : (chinese ? "上传印章" : "Upload stamp")}</span>
              </Button>
            </div>

            {loading && !stamps.length ? (
              <div className="flex flex-col items-center justify-center gap-2.5 py-12 text-muted-foreground">
                <Loader2 className="size-6 animate-spin text-primary" />
                <span className="text-sm">{chinese ? "正在加载印章…" : "Loading stamps…"}</span>
              </div>
            ) : !stamps.length ? (
              <p className="py-8 text-center text-sm text-muted-foreground">
                {chinese ? "暂无印章，上传后即可在文档中使用。" : "Upload your first stamp to use it in documents."}
              </p>
            ) : null}

            {/* Stamp List: All controls on a single row */}
            {Boolean(stamps.length) && (
              <div className="grid gap-2.5">
              {stamps.map((stamp) => (
                <div
                  key={stamp.id}
                  className="flex flex-wrap sm:flex-nowrap items-center justify-between gap-3 rounded-lg border p-2.5 bg-card shadow-2xs"
                >
                  {/* Left: Stamp thumbnail preview & width configuration */}
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="grid size-14 shrink-0 place-items-center overflow-hidden rounded bg-white p-1 border">
                      <img src={stamp.value} alt={stamp.name || "stamp"} className="max-h-full max-w-full object-contain" />
                    </div>
                    <div className="flex items-center gap-1.5 shrink-0">
                      <Label className="text-xs text-muted-foreground whitespace-nowrap">
                        {chinese ? "宽度：" : "Width:"}
                      </Label>
                      <div className="flex items-center gap-1">
                        <Input
                          type="number"
                          min={10}
                          max={150}
                          defaultValue={stamp.insertWidth ?? DEFAULT_STAMP_WIDTH_MM}
                          disabled={busy}
                          aria-label={chinese ? "印章宽度（毫米）" : "Stamp width (mm)"}
                          className="h-8 w-18 text-xs text-center"
                          onBlur={event => void updateWidth(stamp, event.currentTarget)}
                        />
                        <span className="text-xs text-muted-foreground">mm</span>
                      </div>
                    </div>
                  </div>

                  {/* Right: Insert button & Delete icon button */}
                  <div className="flex items-center gap-2 shrink-0">
                    {onInsert && (
                      <Button
                        type="button"
                        size="sm"
                        className="h-8 px-3 text-xs font-medium"
                        disabled={busy}
                        onClick={() => insert(stamp)}
                      >
                        {chinese ? "插入文档" : "Insert into document"}
                      </Button>
                    )}
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="size-8 text-muted-foreground hover:text-destructive hover:bg-destructive/10 shrink-0"
                      disabled={busy}
                      aria-label={chinese ? "删除印章" : "Delete stamp"}
                      title={chinese ? "删除印章" : "Delete stamp"}
                      onClick={() => {
                        if (window.confirm(chinese ? "确认删除该印章？" : "Delete this stamp?")) {
                          void remove(stamp);
                        }
                      }}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
            )}
          </DialogPrimitive.Popup>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </>
  );
}
