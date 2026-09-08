export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

export function documentFilename(name: string) {
  return name.trim().replace(/[\\/:*?"<>|\u0000-\u001f]/g, "-").replace(/\.(docx?|pdf|html)$/i, "") || "document";
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

export async function exportDocumentPages(pages: string[], name: string, format: "pdf" | "png") {
  if (!pages.length) throw new Error("No document pages to export");
  const filename = documentFilename(name);
  if (format === "pdf") {
    const file = await createDocumentPDF(pages, name);
    downloadBlob(file, file.name);
    return;
  }
  const blobs = await Promise.all(pages.map(async (page) => (await fetch(page)).blob()));
  if (blobs.length === 1) {
    downloadBlob(blobs[0], `${filename}.png`);
    return;
  }
  const { zip } = await import("fflate");
  const entries = Object.fromEntries(await Promise.all(blobs.map(async (blob, index) => [
    `${filename}-${String(index + 1).padStart(3, "0")}.png`, new Uint8Array(await blob.arrayBuffer()),
  ] as const)));
  const archive = await new Promise<Uint8Array<ArrayBuffer>>((resolve, reject) => {
    zip(entries, { level: 0 }, (error, data) => error ? reject(error) : resolve(new Uint8Array(data)));
  });
  downloadBlob(new Blob([archive], { type: "application/zip" }), `${filename}.zip`);
}
