import type { IElement } from "@hufe921/canvas-editor";

// A control can contain images as well as text. Image/LaTeX values are source
// data, and must never become business fields or be laid out as characters.
export function isDocumentTextRun(run: { type?: unknown }) {
  return !run.type || (typeof run.type === "string" && ["text", "hyperlink", "superscript", "subscript", "date"].includes(run.type));
}

export function documentControlText(runs: IElement[] | null | undefined) {
  return (runs || []).filter(isDocumentTextRun).map(run => run.value || "").join("").trim();
}

export function projectDocumentControlText(runs: IElement[] | null | undefined, value: string): IElement[] {
  const current = runs || [];
  if (documentControlText(current) === value) return current;
  const projected: IElement[] = [];
  let replaced = false;
  for (const run of current) {
    if (!isDocumentTextRun(run)) projected.push(run);
    else if (!replaced) {
      projected.push({ ...run, value });
      replaced = true;
    }
  }
  // Never copy an image's type, dimensions or stamp metadata onto new text.
  if (!replaced && value) projected.unshift({ value });
  return projected;
}
