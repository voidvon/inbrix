import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import CanvasEditor, { BackgroundRepeat, BackgroundSize, EditorMode, ElementType, ImageDisplay, ListStyle, ListType, PageMode, VerticalAlign } from "@hufe921/canvas-editor";
import type { DocumentStamp } from "./document-stamps";
import { createSpiraxQuotationCanvasBackground, createSpiraxQuotationCanvasDocument, controlText, type SpiraxQuotationValues, type QuotationItem } from "./spirax-quotation-canvas";

const CANVAS_DOCUMENT_PREFIX = "__INBRIX_CANVAS_DOCUMENT__:";
const MAX_DOCUMENT_ATTACHMENT_BYTES = 3 * 1024 * 1024;

type StoredCanvasDocument = {
  template?: "spirax-quotation" | "document";
  data: ReturnType<CanvasEditor["command"]["getValue"]>["data"];
  options?: ReturnType<CanvasEditor["command"]["getValue"]>["options"];
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

function extractPlainText(elements: Array<ReturnType<CanvasEditor["command"]["getValue"]>["data"]["main"][number]> | undefined): string {
  if (!elements) return "";
  let text = "";
  for (const el of elements) {
    if (el.value) text += el.value;
    else if (el.control?.value) text += extractPlainText(el.control.value);
  }
  return text.trim();
}

function hasControls(elements: Array<ReturnType<CanvasEditor["command"]["getValue"]>["data"]["main"][number]>): boolean {
  for (const el of elements) {
    if (el.type === ElementType.CONTROL || el.control?.conceptId) return true;
    if (el.trList) {
      for (const tr of el.trList) {
        for (const td of tr.tdList || []) {
          if (td.value && hasControls(td.value)) return true;
        }
      }
    }
    if (el.valueList && hasControls(el.valueList)) return true;
  }
  return false;
}

function extractLegacySpiraxValues(main: Array<ReturnType<CanvasEditor["command"]["getValue"]>["data"]["main"][number]>): SpiraxQuotationValues {
  const values: SpiraxQuotationValues = {};
  const visit = (elements: typeof main) => {
    for (const el of elements) {
      if (el.type === ElementType.CONTROL && el.control?.conceptId) {
        let textVal = "";
        if (el.control.value && Array.isArray(el.control.value)) {
          textVal = el.control.value.map((v) => v.value || "").join("").trim();
        }
        if (textVal) {
          values[el.control.conceptId] = textVal;
        }
      }
      if (el.type === ElementType.TABLE && el.trList) {
        for (const tr of el.trList) {
          if (!tr.tdList) continue;
          for (const td of tr.tdList) {
            if (td.value) visit(td.value);
          }
          if (tr.tdList.length === 2 || tr.tdList.length === 3) {
            const label = extractPlainText(tr.tdList[0].value);
            const val = extractPlainText(tr.tdList[tr.tdList.length - 1].value);
            if (!val || val === label) continue;
            if (label.includes("Quote No.")) values.quote_number = val;
            else if (label.includes("Issue Date")) values.issue_date = val;
            else if (label.includes("Currency")) values.currency = val;
            else if (label.includes("Validity")) values.validity = val;
            else if (label.includes("Lead Time")) values.lead_time = val;
            else if (label.includes("Payment Terms")) values.payment_terms = val;
            else if (label.includes("Notes")) values.notes = val;
          }
          if (tr.tdList.length === 5) {
            const first = extractPlainText(tr.tdList[0].value);
            if (first && !first.includes("MODEL")) {
              values.item_model = first;
              values.item_description = extractPlainText(tr.tdList[1].value);
              values.item_qty = extractPlainText(tr.tdList[2].value);
              values.item_price = extractPlainText(tr.tdList[3].value);
              values.item_amount = extractPlainText(tr.tdList[4].value);
            }
          }
        }
      }
      if (el.valueList) {
        visit(el.valueList);
      }
    }
  };
  visit(main);

  const extractedItems: QuotationItem[] = [];
  let index = 0;
  while (`item_model_${index}` in values) {
    extractedItems.push({
      model: values[`item_model_${index}`] || "",
      description: values[`item_description_${index}`] || "",
      qty: values[`item_qty_${index}`] || "",
      price: values[`item_price_${index}`] || "",
      amount: values[`item_amount_${index}`] || "",
    });
    index++;
  }
  if (extractedItems.length > 0) {
    values.items = extractedItems;
  } else if (values.item_model) {
    values.items = [
      {
        model: values.item_model,
        description: values.item_description || "",
        qty: values.item_qty || "",
        price: values.item_price || "",
        amount: values.item_amount || "",
      },
    ];
  }

  return values;
}

function migrateLegacySpiraxDocument(data: StoredCanvasDocument["data"]): StoredCanvasDocument["data"] {
  if (hasControls(data.main)) {
    return data;
  }
  const extracted = extractLegacySpiraxValues(data.main);
  return {
    ...data,
    main: createSpiraxQuotationCanvasDocument("", "", extracted),
  };
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
  importDocx: (file: File) => Promise<void>;
  exportDocx: (name: string) => Promise<void>;
  insertAttachment: (file: File) => Promise<void>;
  insertStamp: (stamp: DocumentStamp, width: number) => void;
  getPageImages: () => Promise<string[]>;
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
  getControls: () => Array<{ conceptId: string; placeholder?: string; value?: string }>;
  setControlValues: (values: Array<{ conceptId: string; value: string }>) => boolean;
  applyDocumentUpdates: (payload: { values?: Record<string, string>; items?: QuotationItem[] }) => boolean;
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

function quotationNumber(html: string) {
  return new DOMParser().parseFromString(html, "text/html")
    .querySelector("[data-spirax-quotation]")?.getAttribute("data-document-number") || undefined;
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
  const templateRef = useRef(isSpiraxTemplate);

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
      // Canvas rowMargin adds space above and below each line; it is not CSS line-height.
      defaultRowMargin: isSpiraxTemplate ? 0.35 : 1.25,
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
    templateRef.current = isSpiraxTemplate;
    if (storedDocument) {
      if (storedDocument.options) editor.command.executeUpdateOptions(storedDocument.options);
      editor.command.executeSetValue(isSpiraxTemplate ? migrateLegacySpiraxDocument(storedDocument.data) : storedDocument.data);
    } else if (isSpiraxTemplate) {
      editor.command.executeSetValue({ main: createSpiraxQuotationCanvasDocument(new Date().toLocaleDateString(locale), quotationNumber(initialHTML)) });
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
      const { data, options } = editor.command.getValue();
      return `${CANVAS_DOCUMENT_PREFIX}${JSON.stringify({ template: templateRef.current ? "spirax-quotation" : "document", data, options })}`;
    },
    importDocx: async (file) => {
      const editor = editorRef.current;
      if (!editor) return;
      const { importDocumentDocx } = await import("./document-docx");
      const snapshot = await importDocumentDocx(file);
      if (editorRef.current !== editor) return;
      editor.command.executeUpdateOptions(snapshot.options);
      editor.command.executeSetValue(snapshot.data);
      templateRef.current = false;
    },
    exportDocx: async (name) => {
      const editor = editorRef.current;
      if (!editor) return;
      const snapshot = editor.command.getValue();
      const { exportDocumentDocx } = await import("./document-docx");
      await exportDocumentDocx(snapshot, name);
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
    insertStamp: (stamp, width) => {
      const editor = editorRef.current;
      if (!editor) throw new Error("Editor unavailable");
      const range = editor.command.getRange();
      editor.command.executeFocus(range && range.startIndex >= 0 ? { range } : undefined);
      const id = editor.command.executeImage({
        value: stamp.value, width, height: width * stamp.height / stamp.width,
        imgDisplay: ImageDisplay.FLOAT_TOP,
        extension: { stampId: stamp.id, stampName: stamp.name },
      });
      if (!id) throw new Error("Unable to insert stamp");
    },
    getPageImages: async () => {
      const editor = editorRef.current;
      if (!editor) throw new Error("Editor unavailable");
      await document.fonts.ready;
      return editor.command.getImage({ pixelRatio: 2, mode: EditorMode.PRINT });
    },
    setHTML: (html) => {
      const editor = editorRef.current;
      if (!editor) return;
      const stored = parseStoredCanvasDocument(html);
      const isSpiraxPlaceholder = html.includes("data-spirax-quotation") && !html.includes("<table");
      const isContract = html.includes("合同") || html.includes("CONTRACT");
      const spirax = isSpiraxPlaceholder || stored?.template === "spirax-quotation" || (!isContract && templateRef.current);
      templateRef.current = spirax;
      editor.command.executeUpdateOptions({
        margins: spirax ? [38, 38, 90, 38] : [64, 68, 64, 68],
        defaultSize: spirax ? 11 : 16,
        defaultRowMargin: spirax ? 0.35 : 1.25,
        background: {
          image: spirax ? createSpiraxQuotationCanvasBackground() : "",
          size: BackgroundSize.COVER,
          repeat: BackgroundRepeat.NO_REPEAT,
        },
        table: { tdPadding: spirax ? [0, 4, 0, 4] : [5, 5, 5, 5], defaultTrMinHeight: spirax ? 12 : 24 },
      });
      if (stored) {
        if (stored.options) editor.command.executeUpdateOptions(stored.options);
        editor.command.executeSetValue(spirax ? migrateLegacySpiraxDocument(stored.data) : stored.data);
      } else if (spirax && isSpiraxPlaceholder) {
        editor.command.executeSetValue({ main: createSpiraxQuotationCanvasDocument(new Date().toLocaleDateString(locale), quotationNumber(html)) });
      } else {
        editor.command.executeSetHTML({ main: normalizeDocumentHTML(html) });
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
    getControls: () => {
      const editor = editorRef.current;
      if (!editor) return [];
      const map = new Map<string, { conceptId: string; placeholder?: string; value?: string }>();

      // 1. Check native controls via getControlList
      try {
        const list = editor.command.getControlList() || [];
        for (const el of list) {
          const conceptId = el.control?.conceptId;
          if (!conceptId || map.has(conceptId)) continue;
          let textVal = "";
          try {
            const val = editor.command.getControlValue({ conceptId });
            if (val && val.length > 0) {
              textVal = (val[0]?.value ?? val[0]?.innerText ?? "").trim();
            }
          } catch {
            // ignore
          }
          map.set(conceptId, {
            conceptId,
            placeholder: el.control?.placeholder || conceptId,
            value: textVal,
          });
        }
      } catch {
        // ignore
      }

      // 2. Also inspect the document element tree to catch any controls or bracketed placeholder variables
      try {
        const snapshot = editor.command.getValue();
        const traverse = (elements: Array<ReturnType<CanvasEditor["command"]["getValue"]>["data"]["main"][number]>) => {
          for (const el of elements) {
            if (el.type === ElementType.CONTROL && el.control?.conceptId) {
              const id = el.control.conceptId;
              if (!map.has(id)) {
                let textVal = "";
                if (el.control.value && Array.isArray(el.control.value)) {
                  textVal = el.control.value.map((v) => v.value || "").join("").trim();
                }
                map.set(id, {
                  conceptId: id,
                  placeholder: el.control.placeholder || id,
                  value: textVal,
                });
              }
            } else if (el.value && typeof el.value === "string") {
              const matches = el.value.matchAll(/\[([^\]\n]{2,30})\]/g);
              for (const m of matches) {
                const fullTag = m[0];
                const label = m[1];
                if (!map.has(fullTag)) {
                  map.set(fullTag, {
                    conceptId: fullTag,
                    placeholder: label,
                    value: fullTag,
                  });
                }
              }
            }
            if (el.trList) {
              for (const tr of el.trList) {
                for (const td of tr.tdList || []) {
                  if (td.value) traverse(td.value);
                }
              }
            }
            if (el.valueList) {
              traverse(el.valueList);
            }
          }
        };
        if (snapshot.data?.main) traverse(snapshot.data.main);
      } catch {
        // ignore
      }

      return Array.from(map.values());
    },
    setControlValues: (values) => {
      const editor = editorRef.current;
      if (!editor || !values.length) return false;
      let anyApplied = false;

      const nativeUpdates = values.filter((v) => !v.conceptId.startsWith("["));
      const bracketUpdates = values.filter((v) => v.conceptId.startsWith("[") && v.conceptId.endsWith("]"));

      if (nativeUpdates.length > 0) {
        try {
          editor.command.executeSetControlValueList(
            nativeUpdates.map((item) => ({
              conceptId: item.conceptId,
              value: item.value,
              isSubmitHistory: true,
            }))
          );
          anyApplied = true;
        } catch (error) {
          console.error("executeSetControlValueList failed", error);
        }
      }

      if (bracketUpdates.length > 0) {
        try {
          const snapshot = editor.command.getValue();
          const repMap = new Map<string, string>(bracketUpdates.map((u) => [u.conceptId, u.value]));
          const replaceInElements = (elements: Array<ReturnType<CanvasEditor["command"]["getValue"]>["data"]["main"][number]>) => {
            for (const el of elements) {
              if (el.value && typeof el.value === "string") {
                for (const [tag, replacement] of repMap.entries()) {
                  if (el.value.includes(tag)) {
                    el.value = el.value.replaceAll(tag, replacement);
                    anyApplied = true;
                  }
                }
              }
              if (el.trList) {
                for (const tr of el.trList) {
                  for (const td of tr.tdList || []) {
                    if (td.value) replaceInElements(td.value);
                  }
                }
              }
              if (el.valueList) {
                replaceInElements(el.valueList);
              }
            }
          };
          if (snapshot.data?.main) {
            replaceInElements(snapshot.data.main);
            editor.command.executeSetValue(snapshot.data);
          }
        } catch (error) {
          console.error("bracket replacement failed", error);
        }
      }

      return anyApplied;
    },
    applyDocumentUpdates: (payload) => {
      const editor = editorRef.current;
      if (!editor) return false;
      const { values = {}, items } = payload;
      let applied = false;

      if (templateRef.current && items && items.length > 0) {
        try {
          const snapshot = editor.command.getValue();
          const existing = extractLegacySpiraxValues(snapshot.data.main);
          const merged: SpiraxQuotationValues = { ...existing, ...values };
          const newMain = createSpiraxQuotationCanvasDocument(
            merged.issue_date || "",
            merged.quote_number || "",
            merged,
            items
          );
          editor.command.executeSetValue({ main: newMain });
          applied = true;
          return applied;
        } catch (err) {
          console.error("Failed to apply quotation document items", err);
        }
      }

      if (Object.keys(values).length > 0) {
        const nativeUpdates = Object.entries(values).map(([conceptId, value]) => ({ conceptId, value: String(value) }));
        const nativeFiltered = nativeUpdates.filter((v) => !v.conceptId.startsWith("["));
        const bracketUpdates = nativeUpdates.filter((v) => v.conceptId.startsWith("[") && v.conceptId.endsWith("]"));

        if (nativeFiltered.length > 0) {
          try {
            editor.command.executeSetControlValueList(
              nativeFiltered.map((item) => ({
                conceptId: item.conceptId,
                value: item.value,
                isSubmitHistory: true,
              }))
            );
            applied = true;
          } catch (error) {
            console.error("executeSetControlValueList failed", error);
          }
        }

        if (bracketUpdates.length > 0) {
          try {
            const snapshot = editor.command.getValue();
            const repMap = new Map<string, string>(bracketUpdates.map((u) => [u.conceptId, u.value]));
            const replaceInElements = (elements: Array<ReturnType<CanvasEditor["command"]["getValue"]>["data"]["main"][number]>) => {
              for (const el of elements) {
                if (el.value && typeof el.value === "string") {
                  for (const [tag, replacement] of repMap.entries()) {
                    if (el.value.includes(tag)) {
                      el.value = el.value.replaceAll(tag, replacement);
                      applied = true;
                    }
                  }
                }
                if (el.trList) {
                  for (const tr of el.trList) {
                    for (const td of tr.tdList || []) {
                      if (td.value) replaceInElements(td.value);
                    }
                  }
                }
                if (el.valueList) {
                  replaceInElements(el.valueList);
                }
              }
            };
            if (snapshot.data?.main) {
              replaceInElements(snapshot.data.main);
              editor.command.executeSetValue(snapshot.data);
            }
          } catch (error) {
            console.error("bracket replacement failed", error);
          }
        }
      }

      return applied;
    },
  }), [isSpiraxTemplate, locale]);

  return <div ref={containerRef} className="canvas-document-editor" data-document-template={isSpiraxTemplate ? "spirax-quotation" : undefined} />;
});
