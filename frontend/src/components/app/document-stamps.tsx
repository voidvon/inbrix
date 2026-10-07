import { useRef, useState } from "react";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { Stamp, Upload, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { listDocumentStamps, saveDocumentStamp, deleteDocumentStamp } from "../../lib/api";

export type DocumentStamp = {
  id: string;
  name: string;
  value: string;
  width: number;
  height: number;
  insertWidth?: number; // width in mm, default 40
};

const STORAGE_KEY = "inbrix-document-stamps";
const DEFAULT_STAMP_WIDTH_MM = 40;

function readStamps(): DocumentStamp[] {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) || "[]");
    if (!Array.isArray(stored)) return [];
    return stored
      .filter((item: unknown): item is DocumentStamp => {
        if (!item || typeof item !== "object") return false;
        const stamp = item as Partial<DocumentStamp>;
        return (
          typeof stamp.id === "string" &&
          typeof stamp.name === "string" &&
          typeof stamp.value === "string" &&
          stamp.value.startsWith("data:image/png;base64,") &&
          typeof stamp.width === "number" &&
          Number.isFinite(stamp.width) &&
          stamp.width > 0 &&
          typeof stamp.height === "number" &&
          Number.isFinite(stamp.height) &&
          stamp.height > 0
        );
      })
      .map((s) => ({
        ...s,
        insertWidth:
          typeof s.insertWidth === "number" && Number.isFinite(s.insertWidth) && s.insertWidth > 0
            ? s.insertWidth
            : DEFAULT_STAMP_WIDTH_MM,
      }));
  } catch {
    return [];
  }
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
  const [busy, setBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const label = chinese ? "印章管理" : "Stamp library";

  const persist = (next: DocumentStamp[], deletedId?: string) => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
      // Ignore local storage quota errors
    }
    setStamps(next);
    if (deletedId) {
      void deleteDocumentStamp(deletedId).catch((err) => console.warn("Failed to delete stamp from server", err));
    }
    return true;
  };

  const loadStamps = async () => {
    try {
      const res = await listDocumentStamps();
      if (Array.isArray(res.stamps)) {
        const converted: DocumentStamp[] = res.stamps.map((s) => ({
          id: s.id,
          name: s.name,
          value: s.value || s.dataUrl || "",
          width: s.width || 100,
          height: s.height || 100,
          insertWidth: s.insertWidth || DEFAULT_STAMP_WIDTH_MM,
        }));
        setStamps(converted);
        try {
          localStorage.setItem(STORAGE_KEY, JSON.stringify(converted));
        } catch {}
        return;
      }
    } catch (err) {
      console.warn("Could not load stamps from server, falling back to local storage", err);
    }
    setStamps(readStamps());
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
      await saveDocumentStamp({
        id: stamp.id,
        name: stamp.name,
        value: stamp.value,
        width: stamp.width,
        height: stamp.height,
        insertWidth: stamp.insertWidth,
        aspectRatio: stamp.width / stamp.height,
      });
      const next = [...stamps, stamp];
      persist(next);
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
          void loadStamps();
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
              <DialogPrimitive.Title className="text-base font-semibold text-foreground">
                {label}
              </DialogPrimitive.Title>
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
                disabled={busy}
                onClick={() => inputRef.current?.click()}
                className="gap-1.5"
              >
                <Upload className="size-4" />
                <span>{busy ? (chinese ? "上传中…" : "Uploading…") : (chinese ? "上传印章" : "Upload stamp")}</span>
              </Button>
            </div>

            {!stamps.length && (
              <p className="py-8 text-center text-sm text-muted-foreground">
                {chinese ? "暂无印章，上传后即可在文档中使用。" : "Upload your first stamp to use it in documents."}
              </p>
            )}

            {/* Stamp List: All controls on a single row */}
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
                          onChange={(event) => {
                            const val = Number(event.target.value);
                            if (Number.isFinite(val) && val >= 10 && val <= 150) {
                              persist(stamps.map((item) => (item.id === stamp.id ? { ...item, insertWidth: val } : item)));
                              void saveDocumentStamp({
                                id: stamp.id,
                                name: stamp.name,
                                value: stamp.value,
                                width: stamp.width,
                                height: stamp.height,
                                insertWidth: val,
                              }).catch(() => {});
                            }
                          }}
                          onBlur={(event) => {
                            const val = Number(event.target.value);
                            const safeVal =
                              Number.isFinite(val) && val >= 10 && val <= 150
                                ? val
                                : (stamp.insertWidth ?? DEFAULT_STAMP_WIDTH_MM);
                            event.target.value = String(safeVal);
                            persist(stamps.map((item) => (item.id === stamp.id ? { ...item, insertWidth: safeVal } : item)));
                            void saveDocumentStamp({
                              id: stamp.id,
                              name: stamp.name,
                              value: stamp.value,
                              width: stamp.width,
                              height: stamp.height,
                              insertWidth: safeVal,
                            }).catch(() => {});
                          }}
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
                          persist(stamps.filter((item) => item.id !== stamp.id), stamp.id);
                        }
                      }}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </DialogPrimitive.Popup>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </>
  );
}
