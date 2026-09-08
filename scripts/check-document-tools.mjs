import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { unzipSync } from 'fflate';

// Run against Vite. API fixtures and browser storage are isolated from real accounts.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
page.on('pageerror', error => errors.push(error.message));
await page.route('**/api/**', route => route.fulfill({ json: route.request().url().includes('conversations')
  ? { locale: 'zh-CN', folders: [], accounts: [], accountEmail: 'test@example.com', conversations: [] }
  : { calendar: false, notifications: false, webPush: false } }));
const base = process.env.DOCUMENT_TEST_URL || 'http://localhost:2342';
try {
  await page.goto(`${base}/documents`);
  await page.getByRole('button', { name: '印章管理', exact: true }).click();
  const stampDialog = page.getByRole('dialog');
  const stamp = await page.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width = canvas.height = 160;
    const context = canvas.getContext('2d'); context.strokeStyle = '#cc0000'; context.lineWidth = 8;
    context.beginPath(); context.arc(80, 80, 65, 0, Math.PI * 2); context.stroke();
    context.fillStyle = '#cc0000'; context.font = 'bold 22px Arial'; context.fillText('TEST', 50, 88);
    return canvas.toDataURL().split(',')[1];
  });
  await stampDialog.locator('input[type=file]').setInputFiles({ name: 'test-seal.png', mimeType: 'image/png', buffer: Buffer.from(stamp, 'base64') });
  await stampDialog.getByRole('img', { name: 'test-seal' }).waitFor();
  await stampDialog.getByRole('textbox', { name: '印章名称' }).fill('测试印章');
  await stampDialog.getByText('印章管理', { exact: true }).click();
  await stampDialog.getByRole('img', { name: '测试印章' }).waitFor();
  await page.keyboard.press('Escape');
  await page.reload();
  await page.getByRole('button', { name: '印章管理', exact: true }).click();
  await page.getByRole('img', { name: '测试印章' }).waitFor();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '新建文档', exact: true }).click();
  const editor = page.getByTestId('document-editor-dialog');
  await editor.locator('canvas').first().waitFor();
  await editor.getByRole('button', { name: '印章管理', exact: true }).click();
  await page.getByRole('button', { name: '插入文档', exact: true }).click();
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  await page.waitForFunction(() => localStorage.getItem('inbrix-documents')?.includes('stampId'));
  const records = await page.evaluate(() => JSON.parse(localStorage.getItem('inbrix-documents')));
  assert(records[0].html.includes('data:image/png;base64,'), 'Stamp should be embedded in saved document');
  await page.reload();
  await editor.locator('canvas').first().waitFor();
  async function download(label) {
    await editor.getByRole('button', { name: '导出', exact: true }).click();
    const downloaded = page.waitForEvent('download');
    await page.getByRole('menuitem', { name: label, exact: true }).click();
    const result = await downloaded;
    assert.equal(await result.failure(), null);
    return { name: result.suggestedFilename(), bytes: await readFile(await result.path()) };
  }
  const pdf = await download('下载 PDF');
  assert(pdf.name.endsWith('.pdf'));
  assert(pdf.bytes.subarray(0, 5).equals(Buffer.from('%PDF-')));
  const png = await download('下载 PNG 图片（多页 ZIP）');
  assert(png.name.endsWith('.png'));
  assert.equal(png.bytes.subarray(1, 4).toString(), 'PNG');
  const bounds = await page.evaluate(async (base64) => {
    const img = new Image(); img.src = `data:image/png;base64,${base64}`; await img.decode();
    const canvas = document.createElement('canvas'); canvas.width = img.width; canvas.height = img.height;
    const ctx = canvas.getContext('2d'); ctx.drawImage(img, 0, 0);
    const { data } = ctx.getImageData(0, 0, img.width, img.height);
    let left = img.width, top = img.height, right = 0, bottom = 0;
    for (let y = 0; y < img.height; y++) for (let x = 0; x < img.width; x++) {
      const i = (y * img.width + x) * 4;
      if (data[i] > 130 && data[i + 1] < 70 && data[i + 2] < 70) {
        left = Math.min(left, x); right = Math.max(right, x); top = Math.min(top, y); bottom = Math.max(bottom, y);
      }
    }
    return { width: right - left, height: bottom - top };
  }, png.bytes.toString('base64'));
  assert(bounds.width > 100 && Math.abs(bounds.width - bounds.height) < 4, `Stamp must be visible and unclipped: ${JSON.stringify(bounds)}`);
  await page.screenshot({ path: '/tmp/inbrix-document-tools.png' });
  // A long contract exercises page order and complete PDF/ZIP output.
  await page.evaluate(() => {
    const documents = JSON.parse(localStorage.getItem('inbrix-documents'));
    documents.push({ id: 'test-contract', type: 'contract', name: '多页合同', updatedAt: new Date().toISOString(),
      html: '<h1>合同测试</h1>' + Array.from({ length: 70 }, (_, i) => `<p>第 ${i + 1} 条：服务内容及付款条件。</p>`).join('') });
    localStorage.setItem('inbrix-documents', JSON.stringify(documents));
  });
  await page.goto(`${base}/documents/test-contract`);
  await editor.locator('canvas').first().waitFor();
  await editor.getByRole('button', { name: '印章管理', exact: true }).click();
  await page.getByRole('button', { name: '插入文档', exact: true }).click();
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  await page.waitForFunction(() => JSON.parse(localStorage.getItem('inbrix-documents')).find(x => x.id === 'test-contract').html.includes('stampId'));
  const multiPdf = await download('下载 PDF');
  const archive = await download('下载 PNG 图片（多页 ZIP）');
  assert(archive.name.endsWith('.zip'));
  const entries = unzipSync(archive.bytes);
  const names = Object.keys(entries);
  assert(names.length > 1);
  assert.equal([...multiPdf.bytes.toString('latin1').matchAll(/\/Type \/Page\b/g)].length, names.length);
  for (const name of names) assert.equal(Buffer.from(entries[name]).subarray(1, 4).toString(), 'PNG');
  await editor.getByRole('button', { name: '印章管理', exact: true }).click();
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: '删除', exact: true }).click();
  assert.equal(await page.evaluate(() => JSON.parse(localStorage.getItem('inbrix-document-stamps')).length), 0);
  assert(await page.evaluate(() => JSON.parse(localStorage.getItem('inbrix-documents')).every(x => x.html.includes('stampId'))));
  await page.evaluate(() => {
    const contract = JSON.parse(localStorage.getItem('inbrix-documents')).find(x => x.id === 'test-contract');
    localStorage.setItem('inbrix-document-templates', JSON.stringify([{ ...contract, id: 'stamp-template', name: '带章合同模板' }]));
  });
  await page.goto(`${base}/documents/${records[0].id}`);
  await editor.locator('canvas').first().waitFor();
  await editor.getByRole('combobox').click();
  await page.getByRole('option', { name: '带章合同模板', exact: true }).click();
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  await page.waitForFunction(id => {
    const record = JSON.parse(localStorage.getItem('inbrix-documents')).find(x => x.id === id);
    return record.type === 'contract' && record.html.includes('stampId') && !record.html.includes('spirax-quotation');
  }, records[0].id);
  await page.evaluate(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function(key, value) {
      if (key === 'inbrix-documents') throw new DOMException('Storage full', 'QuotaExceededError');
      return original.call(this, key, value);
    };
  });
  await editor.getByRole('textbox', { name: '文档名称', exact: true }).fill('不能保存的名称');
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByText('保存失败，请检查浏览器存储空间。可先导出 PDF 或图片保留副本。', { exact: true }).waitFor();
  assert(await page.evaluate(() => JSON.parse(localStorage.getItem('inbrix-documents')).every(x => x.name !== '不能保存的名称')));
  assert.deepEqual(errors, []);
  console.log(`PASS: upload, rename, persistence, quotation/contract insertion, saved stamp retention, PNG/PDF and ${names.length}-page ZIP/PDF, deletion, saved template switching, storage failure handling.`);
} finally { await browser.close(); }
