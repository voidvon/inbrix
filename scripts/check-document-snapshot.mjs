import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Uses only the temporary database in product-library-harness.go.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3219';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
page.setDefaultTimeout(20000);
const errors = [];
page.on('pageerror', error => errors.push(error.message));
try {
  await page.goto(`${base}/documents`);
  const saved = await page.evaluate(async () => {
    const { createSpiraxQuotationCanvasDocument } = await import('/src/components/app/spirax-quotation-canvas.ts');
    const { createDocumentBusiness, projectDocumentBusiness } = await import('/src/lib/document-business.ts');
    const { saveDocument } = await import('/src/lib/api.ts');
    const values = { customer_company: 'Snapshot Buyer', seller_company: 'Snapshot Seller', currency: 'CNY', payment_terms: 'Net30', lead_time: '2 weeks', validity: '30 days', quote_number: 'SNAPSHOT-Q-1', issue_date: '2026-10-27', tax_rate: '13%', tax_mode: 'excluded' };
    const business = createDocumentBusiness(values, [
      { model: 'A', description: 'Valve A', qty: '1', price: '100', amount: '' },
      { model: 'B', description: 'Valve B', qty: '2', price: '50', amount: '' },
    ]);
    const data = { main: createSpiraxQuotationCanvasDocument('2026-10-27', 'SNAPSHOT-Q-1', values, business.items) };
    projectDocumentBusiness(data, business);
    const html = '__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify({ template: 'spirax-quotation', data, business });
    return (await saveDocument({ type: 'quotation', name: `Snapshot regression ${crypto.randomUUID()}`, html })).document;
  });
  await page.goto(`${base}/documents/${saved.id}`);
  const dialog = page.getByTestId('document-editor-dialog');
  await dialog.locator('canvas').first().waitFor();
  // Read the real imperative editor handle through React's test-only fiber
  // traversal, without exposing debug state in production components.
  await page.waitForFunction(() => {
    const host = document.querySelector('.canvas-document-editor');
    if (!host) return false;
    let fiber = host[Object.keys(host).find(key => key.startsWith('__reactFiber$'))];
    for (; fiber; fiber = fiber.return) {
      if (fiber.ref?.current?.getDocument) { window.snapshotEditor = fiber.ref.current; return true; }
    }
    return false;
  });
  const initial = await page.evaluate(() => JSON.parse(window.snapshotEditor.getDocument().split('__INBRIX_CANVAS_DOCUMENT__:')[1]));
  assert.equal(initial.business.version, 2);
  assert.equal(initial.business.calculated.total_amount, '200.00');
  assert.notEqual(initial.business.items[0].lineId, initial.business.items[1].lineId);
  await page.evaluate(() => window.snapshotEditor.setControlValues([{ conceptId: 'item_qty_0', value: '3' }]));
  await page.waitForFunction(() => {
    const doc = JSON.parse(window.snapshotEditor.getDocument().split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
    return doc.business.items[0].qty === '3' && !doc.business.calculated.total_amount;
  });
  const changed = await page.evaluate(() => ({ data: window.snapshotEditor.getDocumentData(), controls: window.snapshotEditor.getControls() }));
  assert.equal(changed.data.items[0].lineId, initial.business.items[0].lineId);
  assert.equal(changed.data.items[0].price, '100');
  assert.equal(changed.data.items[1].qty, '2');
  assert.equal(changed.controls.find(control => control.conceptId === 'total_amount')?.value || '', '');
  // Totals update without saving or pressing Check completeness.
  await page.waitForFunction(() => window.snapshotEditor.getDocumentData().values.total_amount === '400.00');
  const automatic = await page.evaluate(() => window.snapshotEditor.getControls());
  assert.equal(automatic.find(control => control.conceptId === 'item_amount_0')?.value, '300.00');
  assert.equal(automatic.find(control => control.conceptId === 'total_amount')?.value, '400.00');

  // A slower calculation for an older quantity must not replace newer edits.
  let releaseOld;
  let oldStarted;
  const oldRequest = new Promise(resolve => { oldStarted = resolve; });
  const delayed = new Promise(resolve => { releaseOld = resolve; });
  await page.route('**/api/documents/validate', async route => {
    const input = route.request().postDataJSON();
    const snapshot = JSON.parse(input.html.split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
    if (snapshot.business.items[0].qty === '4') {
      const response = await route.fetch();
      oldStarted();
      await delayed;
      await route.fulfill({ response });
    } else await route.continue();
  });
  await page.evaluate(() => window.snapshotEditor.setControlValues([{ conceptId: 'item_qty_0', value: '4' }]));
  await oldRequest;
  await page.evaluate(() => window.snapshotEditor.setControlValues([{ conceptId: 'item_qty_0', value: '5' }]));
  await page.waitForFunction(() => window.snapshotEditor.getDocumentData().values.total_amount === '600.00');
  releaseOld();
  await page.waitForTimeout(200);
  assert.equal(await page.evaluate(() => window.snapshotEditor.getDocumentData().values.total_amount), '600.00');
  await page.unroute('**/api/documents/validate');
  await page.evaluate(() => window.snapshotEditor.setControlValues([{ conceptId: 'item_qty_0', value: '3' }]));
  await page.waitForFunction(() => window.snapshotEditor.getDocumentData().values.total_amount === '400.00');

  const lineAmounts = await page.evaluate(async () => {
    const { calculateLineAmount } = await import('/src/lib/document-amounts.ts');
    return [['1', '1.005'], ['2.5', '8'], ['3', '0.1'], ['1,000', '0.005'], ['1', '0'], ['', '10'], ['0', '10'], ['1.', '10']].map(([qty, price]) => calculateLineAmount(qty, price));
  });
  assert.deepEqual(lineAmounts, ['1.01', '20.00', '0.30', '5.00', '0.00', '', '', '']);

  // Exercise typing across a completed background calculation, including the
  // editor's active control and caret, rather than only imperative updates.
  await page.evaluate(() => {
    const host = document.querySelector('.canvas-document-editor');
    let fiber = host[Object.keys(host).find(key => key.startsWith('__reactFiber$'))];
    for (; fiber; fiber = fiber.return) {
      for (let hook = fiber.memoizedState; hook; hook = hook.next) {
        const native = hook.memoizedState?.current;
        if (!native?.command?.executeLocationControl) continue;
        const control = native.command.getControlList().find(element => element.control?.conceptId === 'item_qty_0');
        native.command.executeLocationControl(control.controlId, { position: 'after' });
        return;
      }
    }
    throw new Error('Missing native canvas editor');
  });
  await page.keyboard.press('Backspace');
  await page.keyboard.type('12', { delay: 30 });
  await page.waitForFunction(() => window.snapshotEditor.getDocumentData().values.total_amount === '1300.00').catch(async error => {
    console.log(await page.evaluate(() => ({ data: window.snapshotEditor.getDocumentData(), focus: document.activeElement?.outerHTML })));
    throw error;
  });
  await page.keyboard.type('3');
  await page.waitForFunction(() => window.snapshotEditor.getDocumentData().values.total_amount === '12400.00');
  assert.equal(await page.evaluate(() => window.snapshotEditor.getDocumentData().items[0].qty), '123');
  await page.evaluate(() => window.snapshotEditor.setControlValues([{ conceptId: 'item_qty_0', value: '3' }]));
  await page.waitForFunction(() => window.snapshotEditor.getDocumentData().values.total_amount === '400.00');

  await dialog.getByRole('tab', { name: '变量列表', exact: true }).click();
  const lineInputs = dialog.locator('table tbody tr').first().locator('input');
  await lineInputs.nth(2).fill('2.5');
  await lineInputs.nth(3).fill('1.005');
  assert.equal(await lineInputs.nth(4).inputValue(), '2.51');
  assert.equal(await lineInputs.nth(4).getAttribute('readonly'), '');
  assert.equal(await dialog.locator('#tax_rate, #tax_mode, #freight_tax_rate, #freight_tax_mode, #tax_amount').count(), 0);
  await dialog.locator('#inland_freight').waitFor();
  await dialog.getByRole('button', { name: '重新读取', exact: true }).click();
  await dialog.getByRole('tab', { name: '文档排版', exact: true }).click();
  const reviewed = page.waitForResponse(response => response.url().endsWith('/api/documents/validate') && response.status() === 200);
  await dialog.getByRole('button', { name: '检查完整性', exact: true }).click();
  const checked = await (await reviewed).json();
  assert.equal(checked.validation.status, 'ready');
  assert.equal(JSON.parse(checked.html.split('__INBRIX_CANVAS_DOCUMENT__:')[1]).business.calculated.total_amount, '400.00');
  await page.waitForFunction(() => window.snapshotEditor.getDocumentData().values.total_amount === '400.00');
  const saving = page.waitForResponse(response => response.url().endsWith('/api/documents') && response.request().method() === 'POST');
  await dialog.getByRole('button', { name: '保存', exact: true }).click();
  const result = await (await saving).json();
  assert.equal(result.document.revision, saved.revision + 1);
  assert.equal(result.validation.status, 'ready');
  await page.waitForTimeout(200);
  const readyDownload = page.waitForEvent('download');
  await dialog.getByRole('button', { name: '导出', exact: true }).click();
  await page.getByRole('menuitem', { name: '下载 PDF', exact: true }).click();
  const pdf = await readyDownload;
  assert.equal(await pdf.failure(), null);
  assert.match(pdf.suggestedFilename(), /\.pdf$/);
  await page.waitForTimeout(200);
  await page.evaluate(() => {
    const doc = JSON.parse(window.snapshotEditor.getDocument().split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
    doc.business.items[0].price = '';
    window.snapshotEditor.setHTML('__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify(doc));
  });
  await page.getByText('合计待补充：', { exact: false }).waitFor();
  assert.equal(await page.evaluate(() => window.snapshotEditor.getDocumentData().items[0].amount), '');
  assert.equal(await page.evaluate(() => window.snapshotEditor.getDocumentData().values.total_amount), '');
  const incompleteResponse = page.waitForResponse(response => response.url().endsWith('/api/documents/validate') && response.status() === 200);
  await dialog.getByRole('button', { name: '检查完整性', exact: true }).click();
  const incomplete = await (await incompleteResponse).json();
  assert.equal(incomplete.validation.status, 'draft');
  assert(incomplete.validation.issues.some(issue => issue.code === 'missing_price'));
  await dialog.getByText('部分信息尚未填写（仅提示）', { exact: false }).waitFor();
  for (const [label, extension] of [
    ['下载 PDF', /\.pdf$/],
    ['下载 PDF（图片）', /\.pdf$/],
    ['下载图片', /\.(jpg|zip)$/],
    ['下载 docx', /\.docx$/],
    ['下载 HTML', /\.html$/],
  ]) {
    const downloading = page.waitForEvent('download');
    await dialog.getByRole('button', { name: '导出', exact: true }).click();
    await page.getByRole('menuitem', { name: label, exact: true }).click();
    const file = await downloading;
    assert.equal(await file.failure(), null, `Incomplete documents can export: ${label}`);
    assert.match(file.suggestedFilename(), extension);
    await dialog.getByRole('button', { name: '导出', exact: true }).waitFor({ state: 'visible' });
  }
  // Exercise the real print button without opening a native OS print dialog.
  await page.evaluate(() => {
    window.draftPrintCount = 0;
    const host = document.querySelector('.canvas-document-editor');
    let fiber = host[Object.keys(host).find(key => key.startsWith('__reactFiber$'))];
    for (; fiber; fiber = fiber.return) {
      for (let hook = fiber.memoizedState; hook; hook = hook.next) {
        const native = hook.memoizedState?.current;
        if (!native?.command?.executePrint) continue;
        native.command.executePrint = async () => { window.draftPrintCount++; };
        return;
      }
    }
    throw new Error('Missing native canvas editor');
  });
  await dialog.getByRole('button', { name: '打印', exact: true }).click();
  await page.waitForFunction(() => window.draftPrintCount === 1);
  const saveDraft = page.waitForResponse(response => response.url().endsWith('/api/documents') && response.request().method() === 'POST');
  await dialog.getByRole('button', { name: '保存', exact: true }).click();
  const savedDraft = await (await saveDraft).json();
  assert.equal(savedDraft.validation.status, 'draft');
  await page.screenshot({ path: '/tmp/inbrix-document-snapshot-desktop.png' });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await page.getByRole('tab', { name: '预览文档', exact: true }).click();
  await page.getByTestId('document-editor-dialog').locator('canvas').first().waitFor();
  await page.waitForTimeout(500);
  await page.screenshot({ path: '/tmp/inbrix-document-snapshot-mobile.png' });

  // Both attachment entry points use real draft validation and PDF rendering.
  // Capture their File callbacks; no email is sent.
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/documents`);
  for (const mode of ['editor', 'import']) {
    await page.evaluate(async ({ mode, record }) => {
      const source = await (await fetch('/src/components/app/document-editor-dialog.tsx')).text();
      const main = await (await fetch('/src/main.tsx')).text();
      const specifier = (text, name) => text.match(new RegExp(`from ["']([^"']*${name}\\.js[^"']*)["']`))[1];
      const React = (await import(specifier(source, 'react'))).default;
      const { createRoot } = (await import(specifier(main, 'react-dom_client'))).default;
      const { QueryClient, QueryClientProvider } = await import(specifier(source, '@tanstack_react-query'));
      const { DocumentEditorDialog, ComposeDocumentAttachment } = await import('/src/components/app/document-editor-dialog.tsx');
      const { zh } = await import('/src/lib/locale.ts');
      window.draftAttachmentRoot?.unmount();
      document.getElementById('draft-attachment-test')?.remove();
      const host = document.createElement('div'); host.id = 'draft-attachment-test'; document.body.append(host);
      window.draftAttachmentFiles = [];
      const common = { copy: zh, accountEmail: 'test@example.com', onAttach: file => window.draftAttachmentFiles.push({ name: file.name, size: file.size, type: file.type }) };
      const element = mode === 'editor'
        ? React.createElement(DocumentEditorDialog, { ...common, target: { kind: 'document', record }, templates: [], initialTab: 'document', onOpenChange: () => {}, onSave: () => {} })
        : React.createElement(ComposeDocumentAttachment, { ...common, action: 'import', onClose: () => {}, onBusyChange: () => {} });
      window.draftAttachmentRoot = createRoot(host);
      window.draftAttachmentRoot.render(React.createElement(QueryClientProvider, { client: new QueryClient() }, element));
    }, { mode, record: savedDraft.document });
    if (mode === 'editor') {
      await page.getByTestId('document-editor-dialog').getByRole('button', { name: /^(作为 PDF 添加到邮件|Attach PDF to email)$/, exact: true }).click();
    } else {
      await page.getByTestId('document-attachment-picker').getByRole('button', { name: new RegExp(savedDraft.document.name) }).click();
    }
    await page.waitForFunction(() => window.draftAttachmentFiles.length === 1);
    const [file] = await page.evaluate(() => window.draftAttachmentFiles);
    assert.equal(file.type, 'application/pdf');
    assert.match(file.name, /\.pdf$/);
    assert(file.size > 0, `Incomplete document PDF attachment was empty: ${mode}`);
  }
  assert.deepEqual(errors, []);
  console.log('Automatic amounts and totals, stale response protection, decimal rounding, advisory completeness checks, all draft export formats, printing, draft saving, and both PDF attachment paths passed.');
} finally { await browser.close(); }
