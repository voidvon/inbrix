import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

// Exercise the real new-document dialog with synthetic data and no model/mailbox.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3097';
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
page.setDefaultTimeout(15000);
const prefix = '__INBRIX_CANVAS_DOCUMENT__:';
const errors = [], writes = [], saves = [];
const account = { id: 'sales', email: 'sales@example.invalid', isActive: true };
let releaseEditor, releaseReply, reusedInvoiceTemplate, failNext = false, holdNext = false;
const editorGate = new Promise(resolve => { releaseEditor = resolve; });
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/src/components/app/canvas-document-editor.tsx*', async route => {
  await editorGate;
  await route.continue();
});
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const request = route.request(), path = new URL(request.url()).pathname;
  if (path === '/api/ai/write-document') {
    const input = request.postDataJSON(); writes.push(input);
    assert.ok(input.canvas.startsWith(prefix), 'AI must receive the mounted editor snapshot');
    if (failNext) { failNext = false; return route.fulfill({ status: 503, json: { error: '测试服务暂不可用' } }); }
    if (holdNext) { holdNext = false; await new Promise(resolve => { releaseReply = resolve; }); }
    const payload = JSON.parse(input.canvas.slice(prefix.length));
    payload.business.values.payment_terms = input.instruction;
    if (payload.template === 'spirax-invoice') Object.assign(payload.business.values, {
      seller_company: 'Example Exporter Co., Ltd.', seller_address: '18 Export Road, Shanghai, China',
      seller_contact: 'Export Sales', seller_phone: '+86 21 5555 0100', seller_email: 'sales@example.invalid',
      customer_company: 'Example Buyer Ltd.', customer_address: '25 Import Street, London, United Kingdom',
      customer_contact: 'Purchasing Department', customer_phone: '+44 20 5555 0200', customer_email: 'buyer@example.invalid',
      currency: 'USD', origin: 'China',
    });
    return route.fulfill({ json: { changed: true, html: prefix + JSON.stringify(payload), summary: `已更新付款条件：${input.instruction}` } });
  }
  if (path === '/api/documents' && request.method() === 'POST') {
    const input = request.postDataJSON(); saves.push(input);
    return route.fulfill({ json: { ok: true, document: { ...input, revision: 1 } } });
  }
  if (path === '/api/documents/validate') return route.fulfill({ json: { html: request.postDataJSON().html, validation: { status: 'draft', issues: [] } } });
  let json = { documents: [], templates: [], stamps: [], bindings: [], models: [], agents: [] };
  if (path === '/api/accounts') json = { accounts: [account] };
  if (path === '/api/conversations') json = { locale: 'zh-CN', accounts: [account], accountEmail: account.email, conversations: [], folders: [] };
  if (path === '/api/capabilities') json = { role: 'user', calendar: false, notifications: false };
  if (path === '/api/document-templates') json = { templates: [
    { id: 'quote', type: 'quotation', name: '标准报价单模板', html: '<div data-spirax-quotation="reference-v1"></div>', updatedAt: new Date().toISOString() },
    { id: 'invoice', type: 'contract', name: '英文形式发票模板', html: '<div data-spirax-contract="reference-v1"></div>', updatedAt: new Date().toISOString() },
    { id: 'commercial-invoice', type: 'contract', name: '英文正式发票模板', html: '<div data-spirax-invoice="reference-v1"></div>', updatedAt: new Date().toISOString() },
    ...(reusedInvoiceTemplate ? [reusedInvoiceTemplate] : []),
  ] };
  await route.fulfill({ json });
});
const dialog = page.getByTestId('document-editor-dialog');
const box = dialog.getByPlaceholder('输入对当前文档的修改要求…');
const send = async text => { await box.fill(text); await box.press('Enter'); };
function invoiceFields(snapshot) {
  const fields = [];
  const visit = value => {
    if (Array.isArray(value)) return value.forEach(visit);
    if (!value || typeof value !== 'object') return;
    if (value.control?.conceptId) fields.push({ id: value.control.conceptId, value: (value.control.value || []).map(run => run.value || '').join('') });
    Object.values(value).forEach(visit);
  };
  visit(snapshot.data);
  return fields;
}
function checkInvoiceStructure(snapshot) {
  const fields = invoiceFields(snapshot), content = JSON.stringify(snapshot.data);
  for (const id of ['buyer_company', 'buyer_address', 'buyer_contact', 'buyer_phone', 'buyer_email', 'seller_company', 'seller_address', 'seller_contact', 'seller_phone', 'seller_email', 'currency', 'payment_terms']) {
    assert.ok(fields.some(field => field.id === id), `CI must display ${id}`);
  }
  assert.ok(content.includes('Seller / Exporter') && content.includes('Buyer / Bill To'), 'Both parties need explicit information blocks');
  assert.ok(!fields.some(field => field.id.startsWith('bank_') || field.id.startsWith('order_note_') || field.id === 'payment_memo'), 'CI must omit the detailed PI remittance and order form');
  for (const obsolete of ['Remittance Instructions', 'MT103', 'DO NOT CONVERT', 'Accepted & Confirmed By (Buyer)']) assert.ok(!content.includes(obsolete), `CI must omit ${obsolete}`);
}
function checkInvoiceDates(snapshot, today) {
  const fields = invoiceFields(snapshot);
  for (const id of ['contract_date', 'seller_sign_date']) {
    assert.equal(snapshot.business.values[id], today, `New CI ${id} must use today's local date`);
    assert.ok(fields.some(field => field.id === id && field.value === today), `${id} must display today's date`);
  }
}
try {
  await page.goto(`${base}/documents`);
  // Notifications sit over the small-screen tab bar; keep this check focused
  // on editor/chat lifecycle without waiting for each toast to disappear.
  await page.addStyleTag({ content: '[data-sonner-toaster], [data-sonner-toast] { pointer-events: none !important; }' });
  const today = await page.evaluate(() => {
    const now = new Date();
    return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  });
  for (const kind of ['quotation', 'contract', 'commercial-invoice', 'reused-invoice']) {
    const isInvoice = kind.endsWith('-invoice');
    if (kind === 'reused-invoice') {
      await page.reload();
      await page.addStyleTag({ content: '[data-sonner-toaster], [data-sonner-toast] { pointer-events: none !important; }' });
    }
    await page.getByRole('button', { name: '新建文档', exact: true }).click();
    await page.getByRole('menuitem').filter({ hasText: kind === 'quotation' ? '标准报价单模板' : kind === 'contract' ? '英文形式发票模板' : kind === 'reused-invoice' ? '复用正式发票模板' : '英文正式发票模板' }).click();
    await dialog.waitFor();
    if (kind === 'quotation') {
      await box.waitFor();
      assert.equal(await box.isDisabled(), true, 'Sending must wait for the lazy editor to initialize');
      releaseEditor();
    }
    await box.waitFor();
    await page.waitForFunction(() => document.querySelector('textarea[placeholder="输入对当前文档的修改要求…"]')?.disabled === false);
    await send('预付30%');
    await dialog.getByText('已更新付款条件：预付30%', { exact: true }).waitFor();
    if (isInvoice) {
      const snapshot = JSON.parse(writes.at(-1).canvas.slice(prefix.length));
      assert.equal(snapshot.template, 'spirax-invoice');
      assert.equal(snapshot.business.values.contract_title, 'COMMERCIAL INVOICE');
      checkInvoiceStructure(snapshot);
      checkInvoiceDates(snapshot, today);
      assert.notEqual(snapshot.business.values.contract_number, 'CI-OLD-001', 'Reusing a saved CI template must create a new invoice number');
      assert.ok(snapshot.business.values.contract_number, 'New invoices must receive a document number');
      assert.ok(JSON.stringify(snapshot.data).includes('Invoice Number:'));
      assert.ok(!JSON.stringify(snapshot.data).includes('PI Number:'));
      assert.ok(decodeURIComponent(snapshot.options.background.image).includes('COMMERCIAL INVOICE'));
      assert.ok(!decodeURIComponent(snapshot.options.background.image).includes('PROFORMA INVOICE'));
      if (!mobile) await dialog.getByText('正式发票', { exact: true }).waitFor();
    }
    const canvas = dialog.locator('canvas').first();
    // Keep the same DOM/editor and chat while switching views or hiding the assistant.
    const originalCanvas = await canvas.elementHandle();
    if (mobile) {
      await dialog.getByRole('tab', { name: '预览文档', exact: true }).click();
      await dialog.getByRole('tab', { name: '变量表单', exact: true }).click();
      await dialog.getByRole('tab', { name: 'AI 对话', exact: true }).click();
    } else {
      await dialog.getByTitle('隐藏助手', { exact: true }).click();
      await dialog.getByRole('button', { name: 'AI 文档助手', exact: true }).click();
    }
    assert.equal(await originalCanvas.evaluate(node => node.isConnected), true, 'Switching views must preserve the editor instance');
    await dialog.getByText('已更新付款条件：预付30%', { exact: true }).waitFor();
    await send('发货前付清');
    await dialog.getByText('已更新付款条件：发货前付清', { exact: true }).waitFor();
    assert.equal(JSON.parse(writes.at(-1).canvas.slice(prefix.length)).business.values.payment_terms, '预付30%', 'Follow-up must use the unsaved edited document');
    assert.deepEqual(writes.at(-1).history, [{ role: 'user', content: '预付30%' }, { role: 'assistant', content: '已更新付款条件：预付30%' }]);
    // An in-flight response must survive switching away from mobile chat.
    holdNext = true;
    const pendingRequest = page.waitForRequest(request => request.url().endsWith('/api/ai/write-document') && request.postDataJSON().instruction === '延迟回复');
    await send('延迟回复');
    await pendingRequest;
    if (mobile) await dialog.getByRole('tab', { name: '预览文档', exact: true }).click();
    await page.waitForFunction(() => document.querySelector('textarea[placeholder="输入对当前文档的修改要求…"]')?.disabled === true);
    releaseReply(); releaseReply = undefined;
    if (mobile) await dialog.getByRole('tab', { name: 'AI 对话', exact: true }).click();
    await dialog.getByText('已更新付款条件：延迟回复', { exact: true }).waitFor();
    failNext = true;
    await send('重试付款条件');
    await dialog.getByText('生成失败：测试服务暂不可用', { exact: true }).waitFor();
    await dialog.getByRole('button', { name: '重试', exact: true }).click();
    await dialog.getByText('已更新付款条件：重试付款条件', { exact: true }).waitFor();
    assert.equal(writes.at(-1).history.filter(message => message.content === '重试付款条件').length, 0, 'Retry must not duplicate the current user request in history');
    assert.equal(await dialog.getByText('重试付款条件', { exact: true }).count(), 1, 'Retry must keep one user bubble');
    if (mobile) await dialog.getByRole('tab', { name: '预览文档', exact: true }).click();
    const saved = page.waitForResponse(response => response.url().endsWith('/api/documents') && response.request().method() === 'POST');
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    await saved;
    assert.equal(JSON.parse(saves.at(-1).html.slice(prefix.length)).business.values.payment_terms, '重试付款条件');
    if (isInvoice) {
      const snapshot = JSON.parse(saves.at(-1).html.slice(prefix.length));
      assert.equal(saves.at(-1).type, 'contract');
      assert.equal(snapshot.template, 'spirax-invoice');
      assert.equal(snapshot.business.values.contract_title, 'COMMERCIAL INVOICE');
      checkInvoiceStructure(snapshot);
      checkInvoiceDates(snapshot, today);
      const fields = invoiceFields(snapshot);
      for (const [id, value] of Object.entries({
        seller_company: 'Example Exporter Co., Ltd.', seller_address: '18 Export Road, Shanghai, China',
        buyer_company: 'Example Buyer Ltd.', buyer_address: '25 Import Street, London, United Kingdom',
      })) assert.ok(fields.some(field => field.id === id && field.value === value), `Saved CI must display ${id} correctly`);
      assert.ok(fields.filter(field => field.id === 'seller_company').every(field => field.value === 'Example Exporter Co., Ltd.'), 'Seller signature must use the same company as the seller block');
      assert.ok(decodeURIComponent(snapshot.options.background.image).includes('COMMERCIAL INVOICE'));
      if (kind === 'commercial-invoice') reusedInvoiceTemplate = {
        id: 'reused-invoice', type: 'contract', name: '复用正式发票模板', updatedAt: new Date().toISOString(),
        html: saves.at(-1).html.replaceAll(today, '2000-01-01').replaceAll(snapshot.business.values.contract_number, 'CI-OLD-001'),
      };
    }
    // The first save replaces the new-document dialog with its saved record.
    // Wait for that replacement before closing, so Escape cannot hit the old one.
    await dialog.getByRole('heading', { name: '编辑文档', exact: true }).waitFor({ state: 'attached' });
    if (isInvoice) {
      await dialog.getByRole('button', { name: '导出', exact: true }).click();
      const downloaded = page.waitForEvent('download');
      await page.getByRole('menuitem', { name: '下载 PDF', exact: true }).click();
      const pdf = await downloaded;
      assert.equal(await pdf.failure(), null);
      assert.ok(pdf.suggestedFilename().endsWith('.pdf'));
      assert.equal((await readFile(await pdf.path())).subarray(0, 5).toString(), '%PDF-');
      if (process.env.DOCUMENT_TEST_SCREENSHOT) await page.screenshot({ path: process.env.DOCUMENT_TEST_SCREENSHOT });
    }
    await dialog.getByRole('button').filter({ hasText: /^关闭$/ }).click();
    await dialog.waitFor({ state: 'hidden' });
  }
  assert.deepEqual(errors, []);
  console.log(`PASS ${mobile ? 'mobile' : 'desktop'}: quotation/proforma/CI creation, saved CI template date and number refresh, invoice contents, retained editor/chat, retry, save and PDF export.`);
} finally {
  releaseEditor(); releaseReply?.();
  if (errors.length) console.log(errors);
  await browser.close();
}
