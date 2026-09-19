import type { DocumentTemplate } from "./document-storage";

const CANVAS_PREFIX = "__INBRIX_CANVAS_DOCUMENT__:";
const NUMBER_LABEL = /^(?:Quote No\.|Contract No\.|PI Number|PI \/ Contract No\.|合同编号|报价编号)\s*[:：]?\s*$/i;

export function createDocumentNumber(type: DocumentTemplate) {
  const now = new Date();
  if (type === "contract") {
    const yy = String(now.getFullYear()).slice(-2);
    const mm = String(now.getMonth() + 1).padStart(2, "0");
    const dd = String(now.getDate()).padStart(2, "0");
    const rand = String(Math.floor(1000 + Math.random() * 9000));
    return `CT${yy}${mm}${dd}${rand}`;
  }
  const date = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, "0")}${String(now.getDate()).padStart(2, "0")}`;
  return `SP-${date}-${crypto.randomUUID().slice(0, 8).toUpperCase()}`;
}

// Called only when creating a document from a template, never when reopening one.
export function numberDocumentTemplate(html: string, number: string): string {
  if (html.startsWith(CANVAS_PREFIX)) {
    const stored = JSON.parse(html.slice(CANVAS_PREFIX.length)) as { data?: unknown };
    const visit = (value: unknown): void => {
      if (Array.isArray(value)) {
        value.forEach(visit);
        return;
      }
      if (!value || typeof value !== "object") return;
      const element = value as {
        value?: string;
        control?: { conceptId?: string; value?: Array<{ value: string }> };
        tdList?: Array<{ value: Array<{ value: string }> }>;
      };
      if (element.control?.conceptId === "contract_number" || element.control?.conceptId === "quote_number") {
        if (element.control.value && element.control.value.length > 0) {
          element.control.value[0].value = number;
        }
      }
      if (element.control?.conceptId === "payment_memo") {
        if (element.control.value && element.control.value.length > 0 && typeof element.control.value[0].value === "string") {
          element.control.value[0].value = element.control.value[0].value
            .replace(/\b(?:SP|CT)-\d{8}-[A-F0-9]{8}\b/g, number)
            .replace(/\bCT\d{10}\b/g, number)
            .replace(/\bPI-20260919-01\b/g, number)
            .replace(/\[(?:Contract number|合同编号|Quote number)\]/gi, number);
        }
      }
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
          .replace(/\b(?:SP|CT)-\d{8}-[A-F0-9]{8}\b/g, number)
          .replace(/\bCT\d{10}\b/g, number);
      }
      Object.values(value).forEach(visit);
    };
    visit(stored.data);
    return CANVAS_PREFIX + JSON.stringify(stored);
  }
  const document = new DOMParser().parseFromString(html, "text/html");
  document.querySelector("[data-spirax-quotation]")?.setAttribute("data-document-number", number);
  document.querySelector("[data-spirax-contract]")?.setAttribute("data-document-number", number);
  document.querySelectorAll("p").forEach((paragraph) => {
    const label = paragraph.querySelector("strong");
    if (label && NUMBER_LABEL.test(label.textContent?.trim() || "")) {
      paragraph.replaceChildren(label, document.createTextNode(` ${number}`));
    }
  });
  return document.body.innerHTML;
}

export function extractDocumentNumber(source: unknown): string | undefined {
  if (!source) return undefined;

  // 1. If an editor handle or object with getControls is passed
  if (typeof source === "object" && source !== null) {
    const editor = source as {
      getControls?: () => Array<{ conceptId: string; value?: string }>;
      getDocument?: () => string;
    };
    if (typeof editor.getControls === "function") {
      const controls = editor.getControls() || [];
      for (const ctrl of controls) {
        if (ctrl.conceptId === "contract_number" || ctrl.conceptId === "quote_number" || ctrl.conceptId === "pi_number") {
          const val = ctrl.value?.trim();
          if (val && !/^\[.*\]$/.test(val)) {
            return val;
          }
        }
      }
    }
    if (typeof editor.getDocument === "function") {
      return extractDocumentNumber(editor.getDocument());
    }
    return undefined;
  }

  // 2. String source (HTML or Canvas JSON)
  if (typeof source !== "string") return undefined;
  const text = source;
  if (text.startsWith(CANVAS_PREFIX)) {
    try {
      const stored = JSON.parse(text.slice(CANVAS_PREFIX.length)) as { data?: unknown };
      let found: string | undefined;
      const visit = (value: unknown): void => {
        if (found) return;
        if (Array.isArray(value)) {
          value.forEach(visit);
          return;
        }
        if (!value || typeof value !== "object") return;
        const el = value as {
          control?: { conceptId?: string; value?: Array<{ value?: string }> };
        };
        if (el.control?.conceptId === "contract_number" || el.control?.conceptId === "quote_number" || el.control?.conceptId === "pi_number") {
          const val = el.control.value?.[0]?.value?.trim();
          if (val && !/^\[.*\]$/.test(val)) {
            found = val;
            return;
          }
        }
        Object.values(value).forEach(visit);
      };
      visit(stored.data);
      if (found) return found;
    } catch {
      // ignore
    }
  }

  // 3. Match data-document-number attribute
  const matchAttr = text.match(/data-document-number="([^"]+)"/);
  if (matchAttr?.[1] && !/^\[.*\]$/.test(matchAttr[1].trim())) {
    return matchAttr[1].trim();
  }

  // 4. Match common pattern CT[YY][MM][DD][4-digit] or CT/SP UUID
  const patternMatch = text.match(/\b(CT\d{10}|(?:SP|CT)-\d{8}-[A-F0-9]{8})\b/);
  if (patternMatch?.[1]) {
    return patternMatch[1];
  }

  // 5. Match label: value
  const labelMatch = text.match(/(?:Quote No\.|Contract No\.|PI Number|PI \/ Contract No\.|合同编号|报价编号)\s*[:：]?\s*([A-Za-z0-9_-]+)/i);
  if (labelMatch?.[1] && !/^\[.*\]$/.test(labelMatch[1].trim())) {
    return labelMatch[1].trim();
  }

  return undefined;
}

export function extractDocumentCompany(source: unknown): string | undefined {
  if (!source) return undefined;

  // 1. Editor handle or object with getControls
  if (typeof source === "object" && source !== null) {
    const editor = source as {
      getControls?: () => Array<{ conceptId: string; value?: string }>;
      getDocument?: () => string;
    };
    if (typeof editor.getControls === "function") {
      const controls = editor.getControls() || [];
      for (const ctrl of controls) {
        if (ctrl.conceptId === "customer_company" || ctrl.conceptId === "buyer_company") {
          const val = ctrl.value?.trim();
          if (val && !/^\[.*\]$/.test(val)) {
            return val;
          }
        }
      }
    }
    if (typeof editor.getDocument === "function") {
      return extractDocumentCompany(editor.getDocument());
    }
    return undefined;
  }

  // 2. String source (Canvas JSON or HTML)
  if (typeof source !== "string") return undefined;
  const text = source;
  if (text.startsWith(CANVAS_PREFIX)) {
    try {
      const stored = JSON.parse(text.slice(CANVAS_PREFIX.length)) as { data?: unknown };
      let found: string | undefined;
      const visit = (value: unknown): void => {
        if (found) return;
        if (Array.isArray(value)) {
          value.forEach(visit);
          return;
        }
        if (!value || typeof value !== "object") return;
        const el = value as {
          control?: { conceptId?: string; value?: Array<{ value?: string }> };
        };
        if (el.control?.conceptId === "customer_company" || el.control?.conceptId === "buyer_company") {
          const val = el.control.value?.[0]?.value?.trim();
          if (val && !/^\[.*\]$/.test(val)) {
            found = val;
            return;
          }
        }
        Object.values(value).forEach(visit);
      };
      visit(stored.data);
      if (found) return found;
    } catch {
      // ignore
    }
  }

  // 3. Match data-concept-id attribute
  const conceptMatch = text.match(/data-concept-id="(?:customer_company|buyer_company)"[^>]*>([^<]+)/);
  if (conceptMatch?.[1] && !/^\[.*\]$/.test(conceptMatch[1].trim())) {
    return conceptMatch[1].trim();
  }

  // 4. Match label in HTML
  const labelMatch = text.match(/(?:Company|客户公司|买方公司|购货单位|买方|需方)\s*[:：]\s*([^\r\n<>&"]+)/i);
  if (labelMatch?.[1] && !/^\[.*\]$/.test(labelMatch[1].trim())) {
    return labelMatch[1].trim();
  }

  return undefined;
}

