import { uploadDocumentImage } from "./api";

// Each call owns its cache, so no image references survive an account switch.
// Imported DOCX files can repeat an image in several text runs or backgrounds.
export async function attachDocumentImages<T>(snapshot: T): Promise<T> {
  const cache = new Map<string, Promise<string>>();
  const visit = async (node: unknown): Promise<unknown> => {
    if (typeof node === "string" && node.startsWith("data:image/")) {
      let uploaded = cache.get(node);
      if (!uploaded) {
        uploaded = fetch(node).then(response => response.blob()).then(uploadDocumentImage);
        cache.set(node, uploaded);
      }
      return uploaded;
    }
    if (Array.isArray(node)) {
      const result = [];
      for (const child of node) result.push(await visit(child));
      return result;
    }
    if (node && typeof node === "object") {
      const result: Record<string, unknown> = {};
      for (const [key, child] of Object.entries(node)) result[key] = await visit(child);
      return result;
    }
    return node;
  };
  return await visit(snapshot) as T;
}

// Downloaded HTML must remain usable outside the authenticated application.
// Only owned application image endpoints are fetched; arbitrary external URLs
// retain their existing behavior.
export async function embedDocumentHTMLImages(html: string): Promise<string> {
  const document = new DOMParser().parseFromString(html, "text/html");
  const cache = new Map<string, Promise<string>>();
  for (const image of document.querySelectorAll<HTMLImageElement>("img[src]")) {
    const source = image.getAttribute("src") || "";
    if (!/^\/api\/(document-attachments|product-images)\//.test(source)) continue;
    let embedded = cache.get(source);
    if (!embedded) {
      embedded = fetch(source, { credentials: "include" }).then(async response => {
        if (!response.ok) throw new Error("Unable to load document image");
        const blob = await response.blob();
        return new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => typeof reader.result === "string"
            ? resolve(reader.result)
            : reject(new Error("Unable to embed document image"));
          reader.onerror = () => reject(new Error("Unable to embed document image"));
          reader.readAsDataURL(blob);
        });
      });
      cache.set(source, embedded);
    }
    image.setAttribute("src", await embedded);
  }
  return document.body.innerHTML;
}
