import { ElementType, type IElement } from "@hufe921/canvas-editor";

// Recover line-item images when an existing canvas is edited or converted to
// another template. The immutable image reference is part of the document.
export function collectProductImageValues(elements: IElement[], values: Record<string, string>) {
  for (const element of elements) {
    for (const row of element.trList || []) {
      const cells = row.tdList.flatMap(cell => cell.value || []);
      const control = cells.find(cell => /^item_(model|description|desc)(?:_\d+)?$/.test(cell.control?.conceptId || ""));
      const image = cells.find(cell => cell.type === ElementType.IMAGE);
      if (control && image) {
        const suffix = control.control!.conceptId!.match(/_\d+$/)?.[0] || "";
        values[`item_picture${suffix}`] = image.value;
        const extension = image.extension as { productId?: string; productVersion?: number } | undefined;
        if (extension?.productId) values[`item_product_id${suffix}`] = extension.productId;
        if (extension?.productVersion) values[`item_product_version${suffix}`] = String(extension.productVersion);
      }
      collectProductImageValues(cells, values);
    }
    if (element.valueList) collectProductImageValues(element.valueList, values);
  }
}
