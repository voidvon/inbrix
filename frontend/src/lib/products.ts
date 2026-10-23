import { apiFetch } from "./api";

export type ProductCategory = { id: string; name: string; parentId: string; productCount: number };
export type ProductBrandGroup = { name: string; productCount: number; categories: { categoryId: string; productCount: number }[] };
export type ProductNavigation = { brands: ProductBrandGroup[]; total: number };
export type ProductSelection = { brand: string | null; categoryId: string; uncategorized: boolean };
export const allProductsSelection: ProductSelection = { brand: null, categoryId: "", uncategorized: false };

export function listProductNavigation() { return apiFetch<ProductNavigation>("/api/product-navigation"); }

export function productCategoryRows(categories: ProductCategory[], parentId = "", path = "", depth = 0): (ProductCategory & { path: string; depth: number })[] {
  return categories.filter(category => category.parentId === parentId).flatMap(category => {
    const label = path ? `${path} / ${category.name}` : category.name;
    return [{ ...category, path: label, depth }, ...productCategoryRows(categories, category.id, label, depth + 1)];
  });
}
export type Product = {
  id: string;
  name: string;
  brand: string;
  model: string;
  categoryId: string;
  categoryName: string;
  specification?: string;
  imageId: string;
  imageUrl?: string;
  version: number;
  updatedAt: string;
  catalog?: ProductCatalog;
};
export type ProductInput = Pick<Product, "name" | "brand" | "model" | "categoryId" | "version"> & {
  specification: string;
  catalog?: ProductCatalog;
  imageDataUrl?: string;
};
export type ProductHit = Product & { section: number; heading: string; offset: number; snippet: string };

export function listProductCategories() {
  return apiFetch<{ categories: ProductCategory[] }>("/api/product-categories");
}
export function saveProductCategory(name: string, id?: string, parentId?: string) {
  return apiFetch<{ category: ProductCategory }>(`/api/product-categories${id ? `/${encodeURIComponent(id)}` : ""}`, { method: id ? "PUT" : "POST", body: JSON.stringify({ name, parentId }) });
}
export function deleteProductCategory(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/product-categories/${encodeURIComponent(id)}`, { method: "DELETE" });
}
export function searchProducts(query = "", categoryId = "", offset = 0, brand: string | null = null, uncategorized = false) {
  const params = new URLSearchParams({ q: query, categoryId, offset: String(offset), limit: "12" });
  if (brand !== null) params.set("brand", brand);
  if (uncategorized) params.set("uncategorized", "true");
  return apiFetch<{ products: ProductHit[]; total: number; hasMore: boolean; nextOffset: number }>(`/api/products?${params}`);
}
export function getProduct(id: string) {
  return apiFetch<{ product: Product }>(`/api/products/${encodeURIComponent(id)}`);
}
export function saveProduct(input: ProductInput, id?: string) {
  return apiFetch<{ product: Product }>(`/api/products${id ? `/${encodeURIComponent(id)}` : ""}`, { method: id ? "PUT" : "POST", body: JSON.stringify(input) });
}
export function deleteProduct(product: Product) {
  return apiFetch<{ ok: boolean }>(`/api/products/${encodeURIComponent(product.id)}?version=${product.version}`, { method: "DELETE" });
}

// Normalize uploaded images so database storage and later document rendering
// share a bounded, browser-decodable PNG/JPEG image.
export async function prepareProductImage(file: File): Promise<string> {
  if (!["image/png", "image/jpeg", "image/webp"].includes(file.type) || file.size > 10 * 1024 * 1024) throw new Error("Please choose an image smaller than 10 MiB");
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    const scale = Math.min(1, 1000 / Math.max(image.naturalWidth, image.naturalHeight));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("Unable to process image");
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    const result = canvas.toDataURL(file.type === "image/png" ? "image/png" : "image/jpeg", 0.88);
    if (result.length > 2 * 1024 * 1024 * 4 / 3) throw new Error("Processed image exceeds 2 MiB");
    return result;
  } finally {
    URL.revokeObjectURL(url);
  }
}

export type ProductDimension = { key: string; label: string; type: "text" | "number" | "select"; unit: string; options?: string[] };
export type ProductPrice = { id: string; amount: string; taxRate: string; taxMode: string; minQty: string; maxQty: string; leadTime: string; source: string; validFrom: string; validUntil: string };
export type ProductVariant = { id: string; sku: string; attributes: Record<string, string>; prices: ProductPrice[] };
export type ProductCatalog = { dimensions: ProductDimension[]; variants: ProductVariant[] };
export const emptyCatalog = (): ProductCatalog => ({ dimensions: [], variants: [] });
export const emptyProductPrice = (): ProductPrice => ({ id: "", amount: "", taxRate: "", taxMode: "", minQty: "", maxQty: "", leadTime: "", source: "", validFrom: "", validUntil: "" });
// Re-entering a price replaces legacy tiers/conditions with one plain CNY amount.
// Merely opening or editing other product fields leaves legacy records intact.
export function setProductVariantPrice(variant: ProductVariant, amount: string) {
  const id = variant.prices.length === 1 ? variant.prices[0].id : "";
  variant.prices = amount === "" ? [] : [{ ...emptyProductPrice(), id, amount }];
}
export type PendingProductPrice = { rowId: string; listName: string; model: string; name: string; fields: Record<string, string>; columns: { key: string; label: string; role: string }[] };
export function pendingProductPrices(offset = 0) {
  return apiFetch<{ rows: PendingProductPrice[]; total: number; hasMore: boolean; nextOffset: number }>(`/api/product-price-imports?offset=${offset}`);
}
export function importProductPrice(rowId: string, productId: string, version: number, price: ProductPrice) {
  return apiFetch<{ product: Product }>(`/api/product-price-imports/${encodeURIComponent(rowId)}`, { method: "POST", body: JSON.stringify({ productId, version, price }) });
}

export type ProductDimensionRecord = ProductDimension & { version: number; productCount: number; updatedAt: string };
export function listProductDimensions() { return apiFetch<{ dimensions: ProductDimensionRecord[] }>("/api/product-dimensions"); }
export function saveProductDimension(input: ProductDimension, key?: string, version = 0) {
 return apiFetch<{ dimension: ProductDimensionRecord }>(`/api/product-dimensions${key ? `/${encodeURIComponent(key)}` : ""}`, { method: key ? "PUT" : "POST", body: JSON.stringify({ ...input, version }) });
}
export function deleteProductDimension(dimension: ProductDimensionRecord) {
 return apiFetch<{ ok: boolean }>(`/api/product-dimensions/${encodeURIComponent(dimension.key)}?version=${dimension.version}`, { method: "DELETE" });
}
