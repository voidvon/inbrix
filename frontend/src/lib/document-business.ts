import type { QuotationItem } from "./api";
import { calculateLineAmount } from "./document-amounts";

export const calculatedDocumentFields = new Set(["subtotal", "tax_amount", "total_amount", "contract_amount"]);
// Old saved documents may still contain these fields; discard them on read.
export const retiredDocumentFields = new Set(["tax_rate", "tax_mode", "tax_amount", "freight_tax_rate", "freight_tax_mode"]);
export function documentFieldKey(key: string) {
  const aliases: Record<string, string> = { buyer_company: "customer_company", buyer_contact: "customer_contact", buyer_email: "customer_email", buyer_phone: "customer_phone", buyer_address: "customer_address" };
  return aliases[key] || key;
}
const pricingFields = new Set(["discount", "currency", "inland_freight"]);
export type DocumentBusiness = {
  version: 2;
  values: Record<string, string>;
  calculated: Record<string, string>;
  items: QuotationItem[];
};

type Node = Record<string, unknown>;
function walk(node: unknown, visit: (element: Node) => void) {
  if (Array.isArray(node)) { node.forEach(child => walk(child, visit)); return; }
  if (!node || typeof node !== "object") return;
  const element = node as Node;
  visit(element);
  for (const key of ["main", "header", "footer", "valueList", "trList", "tdList", "value"]) {
    if (Array.isArray(element[key])) walk(element[key], visit);
  }
}
type ItemField = "model" | "description" | "qty" | "price" | "amount" | "picture";
function itemConcept(id: string): { field: ItemField; index: number } | null {
  const match = /^item_(model|description|desc|qty|price|amount|picture)_(\d+)$/.exec(id);
  return match ? { field: match[1] === "desc" ? "description" : match[1] as ItemField, index: Number(match[2]) } : null;
}
function controlText(control: Node) {
  return ((control.value || []) as Array<{ value?: string }>).map(run => run.value || "").join("").trim();
}

// Remove dedicated legacy tax rows/cells while preserving the table width.
function removeRetiredControls(data: unknown) {
  const concepts = (node: unknown) => {
    const ids: string[] = [];
    walk(node, element => {
      const control = element.control as Node | undefined;
      if (typeof control?.conceptId === "string") ids.push(control.conceptId);
    });
    return ids;
  };
  walk(data, element => {
    if (typeof element.value === "string" && element.value.startsWith("计税方式填写 included（含税）或 excluded（未税）")) {
      element.value = "明细金额按数量 × 单价计算；合计为明细金额之和减折扣、加运费。";
    }
    if (!Array.isArray(element.trList)) return;
    element.trList = (element.trList as Node[]).filter(row => {
      const ids = concepts(row);
      if (!ids.some(id => retiredDocumentFields.has(id))) return true;
      if (ids.every(id => retiredDocumentFields.has(id))) return false;
      let removedSpan = 0;
      row.tdList = (row.tdList as Node[]).filter(cell => {
        const cellIds = concepts(cell);
        if (!cellIds.length || !cellIds.every(id => retiredDocumentFields.has(id))) return true;
        removedSpan += Number(cell.colspan || 1);
        return false;
      });
      const cells = row.tdList as Node[];
      if (removedSpan && cells.length) cells[cells.length - 1].colspan = Number(cells[cells.length - 1].colspan || 1) + removedSpan;
      return true;
    });
  });
}

export function readDocumentBusiness(value: unknown): DocumentBusiness {
  const b = value as DocumentBusiness | undefined;
  if (!b || b.version !== 2 || !b.values || typeof b.values !== "object" || Array.isArray(b.values) || !b.calculated || typeof b.calculated !== "object" || Array.isArray(b.calculated) || !Array.isArray(b.items) || b.items.length > 500
    || Object.entries(b.values).some(([key, v]) => typeof v !== "string" || key.startsWith("item_") || calculatedDocumentFields.has(key) || documentFieldKey(key) !== key)
    || Object.entries(b.calculated).some(([key, v]) => typeof v !== "string" || !calculatedDocumentFields.has(key))) {
    throw new Error("需要新版商业文档，请重新创建 / A version 2 commercial snapshot is required");
  }
  const ids = new Set<string>();
  for (const line of b.items) {
    if (!line || typeof line.lineId !== "string" || !line.lineId || ids.has(line.lineId)) throw new Error("产品明细 lineId 缺失或重复 / Invalid line identity");
    ids.add(line.lineId);
  }
  const business = structuredClone(b);
  for (const key of retiredDocumentFields) {
    delete business.values[key];
    delete business.calculated[key];
  }
  business.items.forEach(item => {
    Reflect.deleteProperty(item, "taxRate");
    Reflect.deleteProperty(item, "taxMode");
  });
  return business;
}

export function createDocumentBusiness(values: Record<string, string> = {}, items: QuotationItem[] = []): DocumentBusiness {
  const inputs: Record<string, string> = {}, calculated: Record<string, string> = {};
  for (const [key, value] of Object.entries(values)) {
    if (retiredDocumentFields.has(key)) continue;
    if (key.startsWith("item_")) continue;
    const canonical = documentFieldKey(key), target = calculatedDocumentFields.has(key) ? calculated : inputs;
    if (canonical in target && target[canonical] !== value) throw new Error(`字段 ${canonical} 与模板别名冲突 / Conflicting party fields`);
    target[canonical] = value;
  }
  return { version: 2, values: inputs, calculated, items: items.map(item => ({ ...item, lineId: item.lineId || crypto.randomUUID() })) };
}

// Only new template construction reads defaults from controls. Saved documents
// must have a canonical snapshot; there is no control-to-data fallback on load.
export function createBusinessFromTemplate(data: unknown): DocumentBusiness {
  const values: Record<string, string> = {};
  const lines = new Map<number, QuotationItem>();
  walk(data, element => {
    const c = element.control as Node | undefined;
    if (!c || typeof c.conceptId !== "string") return;
    const item = itemConcept(c.conceptId);
    if (item) {
      const line = lines.get(item.index) || { model: "", description: "", qty: "", price: "", amount: "" };
      Object.assign(line, { [item.field]: controlText(c) });
      lines.set(item.index, line);
    } else if (!c.conceptId.startsWith("item_")) values[c.conceptId] = controlText(c);
  });
  return createDocumentBusiness(values, [...lines.entries()].sort(([a], [b]) => a - b).map(([, item]) => item));
}

// Bind the projection to stable business identities. All control values are
// rendered from the canonical snapshot, including explicitly empty values.
export function projectDocumentBusiness(data: unknown, business: DocumentBusiness) {
  removeRetiredControls(data);
  walk(data, element => {
    const c = element.control as Node | undefined;
    if (!c || typeof c.conceptId !== "string") return;
    const item = itemConcept(c.conceptId);
    let value: string;
    if (item) {
      const line = business.items[item.index];
      if (!line) return;
      c.extension = { ...(c.extension && typeof c.extension === "object" ? c.extension : {}), lineId: line.lineId };
      value = line[item.field] || "";
    } else value = business.calculated[c.conceptId] ?? business.values[documentFieldKey(c.conceptId)] ?? "";
    if (item?.field === "amount" || calculatedDocumentFields.has(c.conceptId)) c.disabled = true;
    const runs = (c.value || []) as Node[];
    c.value = [{ ...(runs[0] || {}), value }];
  });
}

// Treat typing in a business control as a field edit. It cannot change IDs,
// price provenance, remove lines or write calculated amounts.
export function captureDocumentBusiness(data: unknown, previous: DocumentBusiness): DocumentBusiness {
  const next = readDocumentBusiness(previous);
  let pricingChanged = false;
  const changed = new Map<string, string>();
  const remember = (key: string, value: string) => {
    if (changed.has(key) && changed.get(key) !== value) throw new Error("同一字段的多个控件内容冲突 / Conflicting copies of a field");
    changed.set(key, value);
  };
  walk(data, element => {
    const c = element.control as Node | undefined;
    if (!c || typeof c.conceptId !== "string") return;
    const id = documentFieldKey(c.conceptId), value = controlText(c), item = itemConcept(id);
    if (retiredDocumentFields.has(id)) return;
    if (item) {
      if (["amount", "picture"].includes(item.field)) return;
      const lineId = (c.extension as Node | undefined)?.lineId;
      if (typeof lineId !== "string") throw new Error("明细控件未绑定产品行 / Unbound line control");
      const line = next.items.find(row => row.lineId === lineId);
      if (!line) throw new Error("明细控件未绑定产品行，请使用产品表单调整 / Unbound line control");
      const key = item.field as "model" | "description" | "qty" | "price";
      const base = previous.items.find(row => row.lineId === lineId)!;
      if (base[key] !== value) { remember(`${lineId}:${key}`, value); line[key] = value; pricingChanged = true; }
    } else if (!calculatedDocumentFields.has(id) && !id.startsWith("source_quotation_") && !id.startsWith("item_")) {
      if (previous.values[id] !== value) {
        remember(id, value);
        next.values[id] = value;
        if (pricingFields.has(id)) pricingChanged = true;
      }
    }
  });
  if (pricingChanged) {
    next.calculated = {};
    next.values.say_total = "";
    next.items.forEach(line => { line.amount = calculateLineAmount(line.qty, line.price); });
  }
  return next;
}

export function documentControlUpdates(data: unknown, business: DocumentBusiness) {
  const updates = new Map<string, string>();
  walk(data, element => {
    const c = element.control as Node | undefined;
    if (!c || typeof c.conceptId !== "string") return;
    const item = itemConcept(c.conceptId);
    const value = item
      ? business.items.find(line => line.lineId === (c.extension as Node | undefined)?.lineId)?.[item.field] || ""
      : business.calculated[c.conceptId] ?? business.values[documentFieldKey(c.conceptId)] ?? "";
    if (controlText(c) !== value) updates.set(c.conceptId, value);
  });
  return [...updates].map(([conceptId, value]) => ({ conceptId, value, isSubmitHistory: false }));
}
