import { apiFetch } from "./api";

export type ProductCategory = { id: string; name: string; productCount: number };
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
};
export type ProductInput = Pick<Product, "name" | "brand" | "model" | "categoryId" | "version"> & {
  specification: string;
  imageDataUrl?: string;
};
export type ProductHit = Product & { section: number; heading: string; offset: number; snippet: string };

export function listProductCategories() {
  return apiFetch<{ categories: ProductCategory[] }>("/api/product-categories");
}
export function saveProductCategory(name: string, id?: string) {
  return apiFetch<{ category: ProductCategory }>(`/api/product-categories${id ? `/${encodeURIComponent(id)}` : ""}`, { method: id ? "PUT" : "POST", body: JSON.stringify({ name }) });
}
export function deleteProductCategory(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/product-categories/${encodeURIComponent(id)}`, { method: "DELETE" });
}
export function searchProducts(query = "", categoryId = "", offset = 0) {
  const params = new URLSearchParams({ q: query, categoryId, offset: String(offset), limit: "12" });
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
