// Keep aligned with handlers/web/document_business.go. Arbitrary template fields
// belong in controls, so an absent control cannot masquerade as an applied edit.
const businessKeys = new Set([
  "inland_freight", "freight_tax_rate", "freight_tax_mode", "tax_rate", "tax_mode", "discount", "currency", "subtotal", "tax_amount",
  "total_amount", "contract_amount", "say_total", "source_quotation_id",
  "source_quotation_revision", "source_quotation_name", "source_quotation_updated_at",
]);

export type DocumentBusiness = { version: 1; values: Record<string, string> };

export function readDocumentBusiness(business: unknown): Record<string, string> {
  if (business == null) return {};
  if (!business || typeof business !== "object" || !("version" in business) || business.version !== 1
    || !("values" in business) || !business.values || typeof business.values !== "object" || Array.isArray(business.values)) {
    throw new Error("文档商业数据版本不受支持 / Unsupported document business data");
  }
  const values: Record<string, string> = {};
  for (const [key, value] of Object.entries(business.values)) {
    if (typeof value !== "string") throw new Error("无效的文档商业数据 / Invalid document business data");
    if (businessKeys.has(key)) values[key] = value;
  }
  return values;
}

export function createDocumentBusiness(values: Record<string, string> = {}): DocumentBusiness | undefined {
  const selected = Object.fromEntries(Object.entries(values).filter(([key]) => businessKeys.has(key)));
  return Object.keys(selected).length ? { version: 1, values: selected } : undefined;
}

// Control values override metadata even when a user cleared a field by hand.
export function collectBusinessControlValues(node: unknown, values: Record<string, string> = {}): Record<string, string> {
  if (Array.isArray(node)) {
    for (const child of node) collectBusinessControlValues(child, values);
  } else if (node && typeof node === "object") {
    const element = node as Record<string, unknown>;
    const control = element.control as { conceptId?: string; value?: { value?: string }[] } | undefined;
    if (control?.conceptId && businessKeys.has(control.conceptId)) {
      values[control.conceptId] = (control.value || []).map(run => run.value || "").join("").trim();
    }
    for (const key of ["main", "header", "footer", "valueList", "trList", "tdList", "value"]) {
      if (Array.isArray(element[key])) collectBusinessControlValues(element[key], values);
    }
  }
  return values;
}
