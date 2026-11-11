import { calculateLineAmount } from "./document-amounts";

const exWorks = /^(?:EXW\b|EX[\s-]*WORKS\b|工厂交货|工厂交付|工厂提货|出厂交货|出厂价|厂交价)/i;

export function documentDeliveryTerm(values?: Record<string, string>): string {
  const term = values?.trade_term ?? "EXW (Ex Works)";
  const freight = calculateLineAmount("1", values?.inland_freight || "");
  if (/[1-9]/.test(freight) && (!term.trim() || exWorks.test(term.trim()))) return "Freight included";
  return term;
}
