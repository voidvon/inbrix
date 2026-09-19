export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

export function documentFilename(name: string) {
  return name.trim().replace(/[\\/:*?"<>|\u0000-\u001f]/g, "-").replace(/\.(docx?|pdf|html|jpe?g|png)$/i, "") || "document";
}

export async function createDocumentPDF(pages: string[], name: string): Promise<File> {
  if (!pages.length) throw new Error("No document pages to export");
  const { jsPDF } = await import("jspdf");
  const pdf = new jsPDF({ unit: "mm", format: "a4", compress: true });
  pdf.setProperties({ title: name });
  pages.forEach((page, index) => {
    if (index) pdf.addPage();
    pdf.addImage(page, "PNG", 0, 0, 210, 297, undefined, "FAST");
  });
  return new File([pdf.output("blob")], `${documentFilename(name)}.pdf`, { type: "application/pdf" });
}

async function renderPageToCanvas(dataURL: string): Promise<HTMLCanvasElement> {
  const image = new Image();
  await new Promise<void>((resolve, reject) => {
    image.onload = () => resolve();
    image.onerror = () => reject(new Error("Failed to load page image for export"));
    image.src = dataURL;
    if (image.complete && image.naturalWidth) resolve();
  });

  const canvas = document.createElement("canvas");
  canvas.width = image.naturalWidth || image.width;
  canvas.height = image.naturalHeight || image.height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("Cannot render document image");

  // Solid white background to eliminate transparency
  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.drawImage(image, 0, 0, canvas.width, canvas.height);

  return canvas;
}

export async function renderPageAsJpgBlob(dataURL: string, quality = 0.9): Promise<Blob> {
  const canvas = await renderPageToCanvas(dataURL);
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob);
      else reject(new Error("Canvas toBlob failed"));
    }, "image/jpeg", quality);
  });
}

export async function renderPageAsJpgDataUrl(dataURL: string, quality = 0.92): Promise<string> {
  const canvas = await renderPageToCanvas(dataURL);
  return canvas.toDataURL("image/jpeg", quality);
}

export async function createDocumentImagePDF(pages: string[], name: string): Promise<File> {
  if (!pages.length) throw new Error("No document pages to export");
  const jpgPages = await Promise.all(pages.map((page) => renderPageAsJpgDataUrl(page, 0.92)));
  const { jsPDF } = await import("jspdf");
  const pdf = new jsPDF({ unit: "mm", format: "a4", compress: true });
  pdf.setProperties({ title: name, subject: "Flattened Image PDF (Read-only)" });
  jpgPages.forEach((page, index) => {
    if (index) pdf.addPage();
    pdf.addImage(page, "JPEG", 0, 0, 210, 297, undefined, "FAST");
  });
  return new File([pdf.output("blob")], `${documentFilename(name)}.pdf`, { type: "application/pdf" });
}

export async function exportDocumentPages(pages: string[], name: string, format: "pdf" | "image-pdf" | "jpg" | "png" = "jpg") {
  if (!pages.length) throw new Error("No document pages to export");
  const filename = documentFilename(name);
  if (format === "pdf") {
    const file = await createDocumentPDF(pages, name);
    downloadBlob(file, file.name);
    return;
  }
  if (format === "image-pdf") {
    const file = await createDocumentImagePDF(pages, name);
    downloadBlob(file, file.name);
    return;
  }
  const blobs = await Promise.all(pages.map((page) => renderPageAsJpgBlob(page, 0.9)));
  if (blobs.length === 1) {
    downloadBlob(blobs[0], `${filename}.jpg`);
    return;
  }
  const { zip } = await import("fflate");
  const entries = Object.fromEntries(await Promise.all(blobs.map(async (blob, index) => [
    `${filename}-${String(index + 1).padStart(3, "0")}.jpg`, new Uint8Array(await blob.arrayBuffer()),
  ] as const)));
  const archive = await new Promise<Uint8Array<ArrayBuffer>>((resolve, reject) => {
    zip(entries, { level: 0 }, (error, data) => error ? reject(error) : resolve(new Uint8Array(data)));
  });
  downloadBlob(new Blob([archive], { type: "application/zip" }), `${filename}.zip`);
}
