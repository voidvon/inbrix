import type { DocumentTemplate } from "./document-storage";

const CANVAS_PREFIX = "__INBRIX_CANVAS_DOCUMENT__:";
const NUMBER_LABEL = /^(?:Quote No\.|Contract No\.|合同编号|报价编号)\s*[:：]?\s*$/i;

export function createDocumentNumber(type: DocumentTemplate) {
  const now = new Date();
  const date = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, "0")}${String(now.getDate()).padStart(2, "0")}`;
  return `${type === "quotation" ? "SP" : "CT"}-${date}-${crypto.randomUUID().slice(0, 8).toUpperCase()}`;
}

// Called only when creating a document from a template, never when reopening one.
export function numberDocumentTemplate(html: string, number: string): string {
  if (html.startsWith(CANVAS_PREFIX)) {
    const stored = JSON.parse(html.slice(CANVAS_PREFIX.length));
    const visit = (value: unknown): void => {
      if (Array.isArray(value)) {
        value.forEach(visit);
        return;
      }
      if (!value || typeof value !== "object") return;
      const element = value as { value?: string; tdList?: Array<{ value: Array<{ value: string }> }> };
      if (element.tdList) {
        const labelIndex = element.tdList.findIndex((cell) => NUMBER_LABEL.test(cell.value.map((item) => item.value).join("").trim()));
        const target = labelIndex >= 0 ? element.tdList[labelIndex + 1] : undefined;
        if (target?.value.length) {
          target.value = [{ ...target.value[0], value: number }];
        }
      }
      if (typeof element.value === "string") {
        element.value = element.value
          .replace(/\[(?:Contract number|合同编号|Quote number)\]/gi, number)
          .replace(/\b(?:SP|CT)-\d{8}-[A-F0-9]{8}\b/g, number);
      }
      Object.values(value).forEach(visit);
    };
    visit(stored.data);
    return CANVAS_PREFIX + JSON.stringify(stored);
  }
  const document = new DOMParser().parseFromString(html, "text/html");
  document.querySelector("[data-spirax-quotation]")?.setAttribute("data-document-number", number);
  document.querySelectorAll("p").forEach((paragraph) => {
    const label = paragraph.querySelector("strong");
    if (label && NUMBER_LABEL.test(label.textContent?.trim() || "")) {
      paragraph.replaceChildren(label, document.createTextNode(` ${number}`));
    }
  });
  return document.body.innerHTML;
}
