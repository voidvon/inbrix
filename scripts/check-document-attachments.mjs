import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { unzipSync } from 'fflate';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// Run against the disposable product-library-harness.go backend and Vite UI.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3097';
const artifacts = await mkdtemp(join(tmpdir(), 'inbrix-document-attachments-'));
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
page.setDefaultTimeout(30000);
const errors = [];
page.on('pageerror', error => errors.push(error.message));
let uploads = 0;
page.on('request', request => {
  if (new URL(request.url()).pathname === '/api/document-attachments' && request.method() === 'POST') uploads++;
});
try {
  await page.goto(`${base}/documents`);
  const fixture = await page.evaluate(async () => {
    const { attachDocumentImages } = await import('/src/lib/document-images.ts');
    const { createSpiraxQuotationCanvasDocument } = await import('/src/components/app/spirax-quotation-canvas.ts');
    const { createDocumentBusiness, projectDocumentBusiness } = await import('/src/lib/document-business.ts');
    const { saveDocument, saveDocumentStamp } = await import('/src/lib/api.ts');
    const makeImage = color => {
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 64;
      const ctx = canvas.getContext('2d');
      ctx.fillStyle = color; ctx.fillRect(0, 0, 64, 64);
      return canvas.toDataURL('image/png');
    };
    const green = makeImage('#13a855'), magenta = makeImage('#e319bb');
    const refs = await attachDocumentImages({
      picture: green,
      nested: { rowPrototype: [{ value: green }] },
      background: { image: green },
    });
    if (refs.picture !== refs.nested.rowPrototype[0].value || refs.picture !== refs.background.image) throw new Error('Repeated image references differ');
    const { stamp } = await saveDocumentStamp({ id: crypto.randomUUID(), name: 'Attachment test seal', value: magenta, width: 64, height: 64 });
    const values = { customer_company: 'Attachment Buyer', seller_company: 'Attachment Seller', currency: 'CNY', payment_terms: 'Net30', lead_time: '2 weeks', validity: '30 days', quote_number: 'ATTACHMENT-Q-1', issue_date: '2026-11-17' };
    const business = createDocumentBusiness(values, [{ model: 'Image valve', description: 'Product image', qty: '1', price: '100', picture: refs.picture }]);
    const data = { main: createSpiraxQuotationCanvasDocument('2026-11-17', 'ATTACHMENT-Q-1', values, business.items) };
    data.main.push({ type: 'image', value: stamp.value, width: 64, height: 64, extension: { stampId: stamp.id } });
    projectDocumentBusiness(data, business);
    return { green, stamp, document: (await saveDocument({ type: 'quotation', name: `Image attachments ${crypto.randomUUID()}`, html: '__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify({ template: 'spirax-quotation', data, business }) })).document };
  });
  const saved = fixture.document;
  assert.equal(uploads, 1, 'An import helper must upload a repeated image only once');
  assert.ok(!saved.html.includes('data:image/'), 'Saved snapshot must contain only image references');
  assert.ok(saved.html.includes('lineTemplate') && saved.html.includes('/api/document-attachments/'));
  await page.goto(`${base}/documents/${saved.id}`);
  const dialog = page.getByTestId('document-editor-dialog');
  await dialog.locator('canvas').first().waitFor();
  await page.waitForFunction(() => {
    const host = document.querySelector('.canvas-document-editor');
    if (!host) return false;
    let fiber = host[Object.keys(host).find(key => key.startsWith('__reactFiber$'))];
    for (; fiber; fiber = fiber.return) {
      if (fiber.ref?.current?.getDocument) { window.attachmentEditor = fiber.ref.current; return true; }
    }
    return false;
  });
  // Source deletion must not break an existing document's stamp reference.
  await page.evaluate(async stamp => {
    const { deleteDocumentStamp } = await import('/src/lib/api.ts');
    await deleteDocumentStamp(stamp.id);
    const image = new Image(); image.src = stamp.value; await image.decode();
  }, fixture.stamp);
  const rendering = await page.evaluate(async () => {
    const pages = await window.attachmentEditor.getPageImages();
    const counts = { green: 0, magenta: 0 };
    for (const src of pages) {
      const image = new Image(); image.src = src; await image.decode();
      const canvas = document.createElement('canvas'); canvas.width = image.width; canvas.height = image.height;
      const ctx = canvas.getContext('2d'); ctx.drawImage(image, 0, 0);
      const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
      for (let i = 0; i < pixels.length; i += 4) {
        if (pixels[i] === 19 && pixels[i + 1] === 168 && pixels[i + 2] === 85) counts.green++;
        if (pixels[i] === 227 && pixels[i + 1] === 25 && pixels[i + 2] === 187) counts.magenta++;
      }
    }
    const { createDocumentPDF } = await import('/src/components/app/document-export.ts');
    const pdf = await createDocumentPDF(pages, 'Attachments');
    return { counts, pages: pages.length, pdfHeader: await pdf.slice(0, 4).text() };
  });
  assert.ok(rendering.counts.green > 1000 && rendering.counts.magenta > 1000, 'Canvas/PDF must render both product and stamp images');
  assert.equal(rendering.pdfHeader, '%PDF');
  const htmlExport = await page.evaluate(async () => {
    const { embedDocumentHTMLImages } = await import('/src/lib/document-images.ts');
    const html = await embedDocumentHTMLImages(window.attachmentEditor.getHTML());
    const document = new DOMParser().parseFromString(html, 'text/html');
    return { images: document.querySelectorAll('img[src^="data:image/"]').length, references: document.querySelectorAll('img[src^="/api/"]').length };
  });
  assert.ok(htmlExport.images >= 4 && htmlExport.references === 0, 'Downloaded HTML must embed image bytes for offline use');
  await dialog.screenshot({ path: join(artifacts, 'attachments.png') });
  const downloadPromise = page.waitForEvent('download');
  await page.evaluate(() => window.attachmentEditor.exportDocx('Attachment round trip'));
  const download = await downloadPromise;
  const docxPath = join(artifacts, 'attachments.docx');
  await download.saveAs(docxPath);
  const docx = await readFile(docxPath);
  const media = Object.entries(unzipSync(docx)).filter(([name]) => /^word\/media\/.+/.test(name) && !name.endsWith('/'));
  assert.ok(media.length >= 4, 'Word output must embed the referenced images');
  for (const [name, bytes] of media) assert.deepEqual([...bytes.slice(0, 8)], [137, 80, 78, 71, 13, 10, 26, 10], `Embedded PNG: ${name}`);
  const imported = await page.evaluate(async bytes => {
    await window.attachmentEditor.importDocx(new File([new Uint8Array(bytes)], 'round-trip.docx'));
    return window.attachmentEditor.getDocument();
  }, [...docx]);
  assert.ok(!imported.includes('data:image/') && imported.includes('/api/document-attachments/'), 'DOCX import must attach every image before applying the snapshot');
  // An upload failure during import must preserve the previous editor content.
  await page.route('**/api/document-attachments', route => route.fulfill({ status: 503, json: { error: 'Image upload unavailable' } }));
  const failedImport = await page.evaluate(async bytes => {
    const before = window.attachmentEditor.getDocument();
    try {
      await window.attachmentEditor.importDocx(new File([new Uint8Array(bytes)], 'round-trip.docx'));
      return { failed: false };
    } catch {
      return { failed: true, unchanged: window.attachmentEditor.getDocument() === before };
    }
  }, [...docx]);
  assert.deepEqual(failedImport, { failed: true, unchanged: true });
  await page.unroute('**/api/document-attachments');
  const inserted = await page.evaluate(async source => {
    const blob = await fetch(source).then(response => response.blob());
    window.attachmentEditor.setReadOnly(false);
    window.attachmentEditor.focus();
    await window.attachmentEditor.insertAttachment(new File([blob], 'inserted.png', { type: 'image/png' }));
    return window.attachmentEditor.getDocument();
  }, fixture.green);
  assert.ok(!inserted.includes('data:image/') && inserted.includes('inserted.png'), 'Inserted attachments must retain their name and store an image URL');
  assert.deepEqual(errors, []);
  console.log(`PASS: image upload deduplication, source deletion, canvas pixels, PDF, embedded Word images, DOCX import and upload-failure preservation. Artifacts: ${artifacts}`);
} finally {
  await browser.close();
}
