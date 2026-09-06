import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import CanvasEditor, { BackgroundRepeat, BackgroundSize, ImageDisplay, ListStyle, ListType, PageMode, VerticalAlign } from "@hufe921/canvas-editor";
import { createSpiraxQuotationCanvasBackground, createSpiraxQuotationCanvasDocument } from "./spirax-quotation-canvas";

const CANVAS_DOCUMENT_PREFIX = "__INBRIX_CANVAS_DOCUMENT__:";
const MAX_DOCUMENT_ATTACHMENT_BYTES = 3 * 1024 * 1024;

type StoredCanvasDocument = {
  template?: "spirax-quotation" | "document";
  data: ReturnType<CanvasEditor["command"]["getValue"]>["data"];
};

function parseStoredCanvasDocument(value: string): StoredCanvasDocument | null {
  if (!value.startsWith(CANVAS_DOCUMENT_PREFIX)) return null;
  try {
    const parsed: unknown = JSON.parse(value.slice(CANVAS_DOCUMENT_PREFIX.length));
    if (!parsed || typeof parsed !== "object" || !("data" in parsed)) return null;
    return parsed as StoredCanvasDocument;
  } catch {
    return null;
  }
}

function containsText(value: unknown, text: string): boolean {
  if (typeof value === "string") return value.includes(text);
  if (Array.isArray(value)) return value.some((item) => containsText(item, text));
  if (!value || typeof value !== "object") return false;
  return Object.values(value).some((item) => containsText(item, text));
}

function migrateLegacySpiraxDocument(data: StoredCanvasDocument["data"]) {
  let main = [...data.main];
  const legacyFooterIndex = data.main.findIndex((element) => containsText(element, "Prepared by Shane Zhao"));
  if (legacyFooterIndex >= 0) {
    const footerStartIndex = Math.max(0, legacyFooterIndex - 3);
    main.splice(footerStartIndex, legacyFooterIndex - footerStartIndex + 1);
  }
  const heroIndex = main.findIndex((element) => containsText(element, "Quote No."));
  const hero = main[heroIndex];
  const heroRow = hero?.trList?.[0];
  if (hero && heroRow?.tdList?.[1]) {
    let rightCell = { ...heroRow.tdList[1], verticalAlign: VerticalAlign.TOP };
    const currentMeta = rightCell.value.find((element) => element.trList?.some((row) => containsText(row, "Quote No.")));
    const referenceHero = createSpiraxQuotationCanvasDocument("").find((element) => containsText(element, "Quote No."));
    const referenceMeta = referenceHero?.trList?.[0]?.tdList?.[1]?.value.find((element) => element.trList?.some((row) => containsText(row, "Quote No.")));
    if (currentMeta?.trList && referenceMeta?.trList) {
      const labels = ["Quote No.", "Issue Date", "Currency", "Validity"];
      const trList = referenceMeta.trList.map((referenceRow) => {
        const label = labels.find((item) => containsText(referenceRow, item));
        const currentRow = label ? currentMeta.trList?.find((row) => containsText(row, label)) : undefined;
        const currentValueCell = currentRow?.tdList?.[currentRow.tdList.length - 1];
        const referenceValueIndex = referenceRow.tdList ? referenceRow.tdList.length - 1 : -1;
        if (!currentValueCell || referenceValueIndex < 0) return referenceRow;
        const tdList = referenceRow.tdList.map((td, index) => index === referenceValueIndex ? { ...td, value: currentValueCell.value } : td);
        return { ...referenceRow, tdList };
      });
      const nextMeta = { ...referenceMeta, trList };
      rightCell = { ...rightCell, value: rightCell.value.map((element) => element === currentMeta ? nextMeta : element) };
    }
    const tdList = heroRow.tdList.map((td, index) => index === 1 ? rightCell : td);
    const trList = [{ ...heroRow, height: 178, minHeight: 178, tdList }, ...hero.trList!.slice(1)];
    main = main.map((element, index) => index === heroIndex ? { ...element, trList } : element);
  }
  return { ...data, main };
}

function readFileAsDataURL(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(typeof reader.result === "string" ? reader.result : "");
    reader.onerror = () => reject(reader.error || new Error("Unable to read attachment"));
    reader.readAsDataURL(file);
  });
}

function loadBrowserImage(source: string) {
  return new Promise<HTMLImageElement>((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("Unable to decode image"));
    image.src = source;
  });
}

async function prepareEmbeddedImage(file: File) {
  const original = await readFileAsDataURL(file);
  const image = await loadBrowserImage(original);
  const naturalWidth = image.naturalWidth || image.width || 640;
  const naturalHeight = image.naturalHeight || image.height || 360;
  const sourceScale = Math.min(1, 1600 / naturalWidth, 1600 / naturalHeight);
  let value = original;
  if (!file.type.includes("svg") && (sourceScale < 1 || file.size > 900 * 1024)) {
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(naturalWidth * sourceScale));
    canvas.height = Math.max(1, Math.round(naturalHeight * sourceScale));
    canvas.getContext("2d")?.drawImage(image, 0, 0, canvas.width, canvas.height);
    value = canvas.toDataURL(file.type === "image/png" && file.size <= 1_500_000 ? "image/png" : "image/webp", 0.86);
  }
  const displayScale = Math.min(1, 560 / naturalWidth, 680 / naturalHeight);
  return {
    value,
    width: Math.max(24, Math.round(naturalWidth * displayScale)),
    height: Math.max(24, Math.round(naturalHeight * displayScale)),
  };
}

export type CanvasDocumentEditorHandle = {
  getHTML: () => string;
  getDocument: () => string;
  insertAttachment: (file: File) => Promise<void>;
  setHTML: (html: string) => void;
  focus: () => void;
  bold: () => void;
  italic: () => void;
  underline: () => void;
  bulletList: () => void;
  orderedList: () => void;
  insertLink: (label: string, url: string) => void;
  insertTable: (rows: number, columns: number) => void;
  undo: () => void;
  redo: () => void;
  print: () => Promise<void>;
};

type CanvasDocumentEditorProps = {
  initialHTML: string;
  locale: string;
  onReady?: () => void;
};

function normalizeDocumentHTML(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  document.body.querySelectorAll<HTMLElement>("*").forEach((element) => {
    if (!element.style.color) element.style.color = element.tagName === "A" ? "#2563eb" : "#111827";
  });
  document.body.querySelectorAll<HTMLTableCellElement>("th, td").forEach((cell) => {
    Array.from(cell.childNodes).forEach((node) => {
      if (node.nodeType !== Node.TEXT_NODE || !node.textContent?.trim()) return;
      const span = document.createElement("span");
      span.style.color = cell.style.color || "#111827";
      span.textContent = node.textContent;
      node.replaceWith(span);
    });
  });
  return `<div style="color:#111827;font-family:Arial;font-size:16px">${document.body.innerHTML}</div>`;
}

export const CanvasDocumentEditor = forwardRef<CanvasDocumentEditorHandle, CanvasDocumentEditorProps>(function CanvasDocumentEditor(
  { initialHTML, locale, onReady },
  forwardedRef,
) {
  const containerRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<CanvasEditor | null>(null);
  const readyRef = useRef(onReady);
  const storedDocument = parseStoredCanvasDocument(initialHTML);
  const isSpiraxTemplate = initialHTML.includes("data-spirax-quotation") || storedDocument?.template === "spirax-quotation";

  useEffect(() => {
    readyRef.current = onReady;
  }, [onReady]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const editor = new CanvasEditor(container, [{ value: "" }], {
      locale: locale.startsWith("zh") ? "zhCN" : "en",
      pageMode: PageMode.PAGING,
      width: 794,
      height: 1123,
      margins: isSpiraxTemplate ? [38, 38, 90, 38] : [64, 68, 64, 68],
      defaultFont: "Arial",
      defaultColor: "#111827",
      defaultSize: isSpiraxTemplate ? 11 : 16,
      defaultRowMargin: isSpiraxTemplate ? 1.05 : 1.25,
      pageGap: 16,
      table: {
        tdPadding: isSpiraxTemplate ? [0, 4, 0, 4] : [5, 5, 5, 5],
        defaultTrMinHeight: isSpiraxTemplate ? 12 : 24,
        defaultBorderColor: "#d7dfec",
      },
      background: isSpiraxTemplate ? {
        image: createSpiraxQuotationCanvasBackground(),
        size: BackgroundSize.COVER,
        repeat: BackgroundRepeat.NO_REPEAT,
      } : undefined,
      placeholder: { data: "" },
    });
    editorRef.current = editor;
    if (storedDocument) {
      editor.command.executeSetValue(isSpiraxTemplate ? migrateLegacySpiraxDocument(storedDocument.data) : storedDocument.data);
    } else if (isSpiraxTemplate) {
      editor.command.executeSetValue({ main: createSpiraxQuotationCanvasDocument(new Date().toLocaleDateString(locale)) });
    } else {
      editor.command.executeSetHTML({ main: normalizeDocumentHTML(initialHTML) });
    }
    readyRef.current?.();

    return () => {
      editor.destroy();
      editorRef.current = null;
    };
  }, [initialHTML, isSpiraxTemplate, locale]);

  useImperativeHandle(forwardedRef, () => ({
    getHTML: () => editorRef.current?.command.getHTML().main || "",
    getDocument: () => {
      const editor = editorRef.current;
      if (!editor) return "";
      return `${CANVAS_DOCUMENT_PREFIX}${JSON.stringify({ template: isSpiraxTemplate ? "spirax-quotation" : "document", data: editor.command.getValue().data })}`;
    },
    insertAttachment: async (file) => {
      const editor = editorRef.current;
      if (!editor) return;
      if (file.size > MAX_DOCUMENT_ATTACHMENT_BYTES) throw new Error("Attachment exceeds the 3 MB document limit");
      if (file.type.startsWith("image/") || /\.svg$/i.test(file.name)) {
        const image = await prepareEmbeddedImage(file);
        editor.command.executeFocus();
        const imageId = editor.command.executeImage({ ...image, imgDisplay: ImageDisplay.INLINE, extension: { attachmentName: file.name, attachmentType: file.type, attachmentSize: file.size } });
        if (!imageId) throw new Error("Unable to insert image at the current cursor position");
        return;
      }
      const url = await readFileAsDataURL(file);
      editor.command.executeFocus();
      editor.command.executeHyperlink({
        url,
        valueList: [{ value: `📎 ${file.name} (${Math.max(1, Math.round(file.size / 1024))} KB)`, color: "#174db2", underline: true }],
      });
    },
    setHTML: (html) => {
      if (html.includes("data-spirax-quotation")) {
        editorRef.current?.command.executeSetValue({ main: createSpiraxQuotationCanvasDocument(new Date().toLocaleDateString(locale)) });
      } else {
        editorRef.current?.command.executeSetHTML({ main: normalizeDocumentHTML(html) });
      }
    },
    focus: () => editorRef.current?.command.executeFocus(),
    bold: () => editorRef.current?.command.executeBold(),
    italic: () => editorRef.current?.command.executeItalic(),
    underline: () => editorRef.current?.command.executeUnderline(),
    bulletList: () => editorRef.current?.command.executeList(ListType.UL, ListStyle.DISC),
    orderedList: () => editorRef.current?.command.executeList(ListType.OL),
    insertLink: (label, url) => editorRef.current?.command.executeHyperlink({ url, valueList: [{ value: label }] }),
    insertTable: (rows, columns) => editorRef.current?.command.executeInsertTable(rows, columns),
    undo: () => editorRef.current?.command.executeUndo(),
    redo: () => editorRef.current?.command.executeRedo(),
    print: async () => { await editorRef.current?.command.executePrint(); },
  }), [isSpiraxTemplate, locale]);

  return <div ref={containerRef} className="canvas-document-editor" data-document-template={isSpiraxTemplate ? "spirax-quotation" : undefined} />;
});
