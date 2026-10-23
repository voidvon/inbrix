import { ElementType, type IElement } from "@hufe921/canvas-editor";

// Recover line-item images when an existing canvas is edited or converted to
// another template. The immutable image reference is part of the document.
export function collectProductImageValues(elements: IElement[], values: Record<string, string>) {
  for (const element of elements) {
    for (const row of element.trList || []) {
      const cells = row.tdList.flatMap(cell => cell.value || []);
      const control = cells.find(cell => /^item_(model|description|desc)(?:_\d+)?$/.test(cell.control?.conceptId || ""));
      const image = cells.find(cell => cell.type === ElementType.IMAGE);
      if (control) {
        const suffix = control.control!.conceptId!.match(/_\d+$/)?.[0] || "";
        if (image) values[`item_picture${suffix}`] = image.value;
        const extension = (control.control?.extension || control.extension || control.control?.value?.find(run => run.extension)?.extension || image?.extension) as { model?: string; productId?: string; productVersion?: number; variantId?: string; priceId?: string } | undefined;
        if (extension?.model && !values[`item_model${suffix}`]) values[`item_model${suffix}`] = extension.model;
        if (extension?.variantId) values[`item_variant_id${suffix}`] = extension.variantId;
        if (extension?.priceId) values[`item_price_id${suffix}`] = extension.priceId;
        if (extension?.productId) values[`item_product_id${suffix}`] = extension.productId;
        if (extension?.productVersion) values[`item_product_version${suffix}`] = String(extension.productVersion);
      }
      collectProductImageValues(cells, values);
    }
    if (element.valueList) collectProductImageValues(element.valueList, values);
  }
}
