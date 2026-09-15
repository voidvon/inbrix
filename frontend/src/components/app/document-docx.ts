import CanvasEditor, { BackgroundSize, ElementType, ImageDisplay, WatermarkType, type IElement } from "@hufe921/canvas-editor";
import docxPlugin from "@hufe921/canvas-editor-plugin-docx";
import { strFromU8, unzipSync } from "fflate";

type Snapshot = ReturnType<CanvasEditor["command"]["getValue"]>;
export const MAX_DOCX_BYTES = 10 * 1024 * 1024;

async function pngImage(source: string, width: number, height: number) {
  const image = new Image();
  image.crossOrigin = "anonymous";
  image.src = source;
  await image.decode();
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(width * 2));
  canvas.height = Math.max(1, Math.round(height * 2));
  const context = canvas.getContext("2d");
  if (!context) throw new Error("Cannot render document image");
  context.drawImage(image, 0, 0, canvas.width, canvas.height);
  return canvas.toDataURL("image/png");
}

async function embedImages(elements: IElement[]) {
  for (const element of elements) {
    if (element.type === ElementType.IMAGE && !/^data:image\/(png|jpe?g|gif|bmp);base64,/i.test(element.value)) {
      element.value = await pngImage(element.value, element.width || 100, element.height || 100);
    }
    if (element.valueList) await embedImages(element.valueList);
    for (const row of element.trList || []) {
      for (const cell of row.tdList) await embedImages(cell.value);
    }
  }
}

async function withTemporaryEditor<T>(snapshot: Snapshot | undefined, action: (editor: CanvasEditor) => Promise<T>) {
  const container = document.createElement("div");
  container.style.cssText = "position:fixed;left:-20000px;top:0;pointer-events:none;";
  container.setAttribute("aria-hidden", "true");
  document.body.append(container);
  let editor: CanvasEditor | undefined;
  try {
    editor = new CanvasEditor(container, snapshot?.data || [{ value: "" }], snapshot?.options);
    editor.use(docxPlugin);
    return await action(editor);
  } finally {
    editor?.destroy();
    container.remove();
  }
}

export async function exportDocumentDocx(current: Snapshot, name: string) {
  const snapshot = structuredClone(current);
  const { data, options } = snapshot;
  data.header ||= [];
  data.footer ||= [];
  if (options.background?.image) {
    const width = options.width || 794;
    const height = options.height || 1123;
    data.header.unshift({
      type: ElementType.IMAGE,
      value: await pngImage(options.background.image, width, height),
      width,
      height,
      imgDisplay: ImageDisplay.FLOAT_BOTTOM,
      imgFloatPosition: { x: 0, y: 0 },
    });
  }
  await embedImages(data.header);
  await embedImages(data.main);
  await embedImages(data.footer);
  await withTemporaryEditor(snapshot, async (editor) => {
    await editor.command.executeExportDocx({ fileName: (name || "document").replace(/\.docx$/i, "").replace(/[\\/:*?"<>|]/g, "-") });
  });
}

export async function importDocumentDocx(file: File) {
  if (!/\.docx$/i.test(file.name) || file.size > MAX_DOCX_BYTES) throw new Error("Invalid DOCX file");
  const arrayBuffer = await file.arrayBuffer();
  const files = unzipSync(new Uint8Array(arrayBuffer), { filter: (entry) => /^word\/header\d+\.xml$/.test(entry.name) });
  const pageBackgroundSizes = Object.values(files).flatMap((bytes) => {
    const document = new DOMParser().parseFromString(strFromU8(bytes), "application/xml");
    return Array.from(document.getElementsByTagName("wp:anchor")).filter((anchor) => anchor.getAttribute("behindDoc") === "1").map((anchor) => {
      const extent = anchor.getElementsByTagName("wp:extent")[0];
      return { width: Number(extent?.getAttribute("cx")) / 9525, height: Number(extent?.getAttribute("cy")) / 9525 };
    });
  });
  return withTemporaryEditor(undefined, async (editor) => {
    // The plugin's declaration says void, but its implementation returns a Promise.
    await Promise.resolve(editor.command.executeImportDocx({ arrayBuffer }));
    const options = editor.command.getValue().options;
    // The plugin interprets full-page behind-text artwork as an image watermark.
    // Canvas watermarks rotate artwork, so restore full-page images as backgrounds.
    const watermark = options.watermark;
    if (watermark?.type === WatermarkType.IMAGE && watermark.data && pageBackgroundSizes.some((size) =>
      Math.abs(size.width - (options.width || 794)) < 2 &&
      Math.abs(size.height - (options.height || 1123)) < 2)) {
      editor.command.executeUpdateOptions({
        background: { image: watermark.data, size: BackgroundSize.COVER },
        watermark: { data: "" },
      });
    }
    return editor.command.getValue();
  });
}
