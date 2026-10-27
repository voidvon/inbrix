import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Isolated editor components; an optional local mock-model backend can exercise
// the real HTTP mutation path. No real mailbox, model or saved record is used.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: process.env.AI_TEST_MOBILE ? { width: 390, height: 844 } : { width: 1400, height: 1000 } });
const base = process.env.COMPOSE_TEST_URL || 'http://127.0.0.1:3097';
const prefix = '__INBRIX_CANVAS_DOCUMENT__:';
const writes = [], errors = [];
let held, release, missingCanvas = false;
const paymentQuestion = '缺少付款条件，这个一般填写什么？付款比例吗，还是啥';
const paymentAnswer = '付款条件包括付款比例、付款时间和触发条件。例如：确认订单后预付 30%，发货前付清余款。';
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const request = route.request();
  if (new URL(request.url()).pathname !== '/api/ai/write-document') {
    await route.fulfill({ json: { accounts: [], conversations: [], documents: [], sessions: [], models: [], agents: [], skills: [], brands: [] } }); return;
  }
  const input = request.postDataJSON(); writes.push(input);
  assert.ok(input.canvas.startsWith(prefix), 'Editor sends its full current snapshot');
  assert.ok(input.canvas.includes('HUMAN TERM'), 'AI receives actual document, including first turn');
  if (input.instruction === 'delay') { held = true; await new Promise(resolve => { release = resolve; }); }
  if (missingCanvas) { await route.fulfill({ json: { mode: 'variables', values: { total_amount: '999' } } }); return; }
  if (process.env.DOCUMENT_MUTATION_BACKEND) {
    const response = await route.fetch({ url: `${process.env.DOCUMENT_MUTATION_BACKEND}/api/ai/write-document` });
    await route.fulfill({ response }); return;
  }
  if (input.instruction === paymentQuestion || input.instruction === 'empty reply') {
    await route.fulfill({ json: { mode: 'variables', changed: false, summary: input.instruction === paymentQuestion ? paymentAnswer : '本次未修改文档。请补充需要填写的内容或具体问题。' } }); return;
  }
  // The fixture stands for an already validated server reply. The Go tests
  // independently exercise the actual arithmetic, catalog and layout checks.
  const payload = JSON.parse(input.canvas.slice(prefix.length));
  if (input.mode === 'apply') {
    for (const command of input.commands) {
      if (command.type === 'set_fields') Object.assign(payload.business.values, command.fields);
      if (command.type === 'update_line') Object.assign(payload.business.items.find(line => line.lineId === command.lineId), command.fields);
    }
  } else payload.business.items[0].qty = '2';
  for (const line of payload.business.items) line.amount = (Number(line.qty) * Number(line.price)).toFixed(2);
  const subtotal = payload.business.items.reduce((sum, line) => sum + Number(line.amount), 0).toFixed(2);
  const total = (Number(subtotal) - Number(payload.business.values.discount || 0) + Number(payload.business.values.inland_freight || 0)).toFixed(2);
  Object.assign(payload.business.calculated, { subtotal, total_amount: total, contract_amount: total });
  payload.business.values.say_total = '';
  const fields = { ...payload.business.values, ...payload.business.calculated, item_qty: '2', item_qty_0: '2', item_amount: '200.00', item_amount_0: '200.00' };
  const walk = node => {
    if (Array.isArray(node)) for (const child of node) walk(child);
    else if (node && typeof node === 'object') {
      if (node.control?.conceptId in fields) node.control.value = [{ value: fields[node.control.conceptId] }];
      for (const [key, value] of Object.entries(node)) if (key !== 'control') walk(value);
    }
  };
  walk(payload.data);
  await route.fulfill({ json: { mode: 'variables', changed: true, html: prefix + JSON.stringify(payload), summary: '已核算总额 190.00；已清空金额大写，请核对后补填' } });
});
const mount = async kind => {
  await page.evaluate(async kind => {
    const sidebarSource = await (await fetch('/src/components/app/document-editor-dialog.tsx')).text();
    const mainSource = await (await fetch('/src/main.tsx')).text();
    const specifier = (source, name) => {
      const pattern = new RegExp(`from ["']([^"']*${name}\\.js[^"']*)["']`);
      const url = source.match(pattern)?.[1];
      if (!url) throw new Error(`Missing module URL for ${name}`);
      return url;
    };
    const React = (await import(specifier(sidebarSource, 'react'))).default;
    const { createRoot } = (await import(specifier(mainSource, 'react-dom_client'))).default;
    const { QueryClient, QueryClientProvider } = await import(specifier(sidebarSource, '@tanstack_react-query'));
    const { CanvasDocumentEditor } = await import('/src/components/app/canvas-document-editor.tsx');
    const { DocumentAISidebar } = await import('/src/components/app/document-editor-dialog.tsx');
    const { DocumentVariableForm } = await import('/src/components/app/document-variable-form.tsx');
    const { convertGeneratedToStoredDocument } = await import('/src/lib/ai-document-storage.ts');
    const { zh } = await import('/src/lib/locale.ts');
    window.mutationRoot?.unmount(); document.querySelector('#mutation-test')?.remove();
    const host = document.createElement('div'); host.id = 'mutation-test'; host.style.cssText = 'position:fixed;inset:0;z-index:99999;overflow:auto;background:white'; document.body.append(host);
    const ref = React.createRef(); window.mutationEditor = ref;
    const stored = convertGeneratedToStoredDocument({ id: 'test', type: kind, name: 'Test', html: '', updatedAt: new Date().toISOString(), values: { currency: 'CNY', tax_rate: '13%', tax_mode: 'included', discount: '10.00', subtotal: '100.00', total_amount: '90.00', say_total: '人民币玖拾元整' }, items: [{ model: 'MANUAL', description: 'Manual valve', qty: '1', price: '100.00', amount: '100.00' }] });
    const payload = JSON.parse(stored.html.slice('__INBRIX_CANVAS_DOCUMENT__:'.length));
    payload.data.main.push({ value: 'HUMAN TERM', bold: true });
    payload.data.header = [{ value: 'CUSTOM HEADER' }];
    payload.options = { watermark: { data: 'WATERMARK' } };
    const initialHTML = '__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify(payload);
    const root = createRoot(host); window.mutationRoot = root;
    function Harness() {
      const [ready, setReady] = React.useState(false);
      return React.createElement(QueryClientProvider, { client: new QueryClient() },
        React.createElement('div', { style: { height: '600px', overflow: 'auto' } }, React.createElement(CanvasDocumentEditor, { ref, locale: 'zh-CN', initialHTML, onReady: () => setReady(true) })),
        ready && React.createElement('div', { 'data-testid': 'mutation-sidebar', style: { height: '600px', display: 'flex' } }, React.createElement(DocumentAISidebar, { copy: zh, editor: ref.current, accountEmail: 'me@example.com', type: kind, title: 'Test', isMobile: true, onPreviewDocument: () => {} })),
        ready && React.createElement('div', { 'data-testid': 'mutation-form' }, React.createElement(DocumentVariableForm, { copy: zh, editor: ref.current, accountEmail: 'me@example.com', documentType: kind, title: 'Test' })));
    }
    root.render(React.createElement(Harness));
  }, kind);
  await page.getByTestId('mutation-sidebar').waitFor();
};
const snapshot = () => page.evaluate(() => ({ html: window.mutationEditor.current.getDocument(), data: window.mutationEditor.current.getDocumentData() }));
const send = async text => {
  const box = page.getByPlaceholder('输入对当前文档的修改要求…');
  await box.fill(text); await box.press('Enter');
};
try {
  await page.goto(base);
  for (const kind of ['quotation', 'contract']) {
    await mount(kind);
    assert.equal((await snapshot()).data.values.currency, 'CNY');
    assert.ok(!(await snapshot()).html.includes('Unit Price (USD)'), 'Template must display the document currency');
    const identity = await page.evaluate(async () => {
      const editor = window.mutationEditor.current, original = editor.getDocument();
      const payload = JSON.parse(original.slice('__INBRIX_CANVAS_DOCUMENT__:'.length));
      const { projectDocumentBusiness } = await import('/src/lib/document-business.ts');
      Object.assign(payload.business.items[0], { model: 'CATALOG', productId: 'p', productVersion: 2, variantId: 'v', priceId: 'price' });
      projectDocumentBusiness(payload.data, payload.business);
      editor.setHTML('__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify(payload));
      editor.setHTML(editor.getDocument());
      const item = editor.getDocumentData().items[0];
      editor.setHTML(original);
      return item;
    });
    assert.equal(identity.productId, 'p'); assert.equal(identity.productVersion, 2);
    assert.equal(identity.variantId, 'v'); assert.equal(identity.priceId, 'price');
    const beforeQuestion = await snapshot();
    await send(paymentQuestion);
    const sidebar = page.getByTestId('mutation-sidebar');
    await sidebar.getByText(paymentAnswer, { exact: true }).waitFor();
    assert.equal((await snapshot()).html, beforeQuestion.html, 'Advice leaves the entire document unchanged');
    assert.equal(await sidebar.getByText(/生成失败|点击预览最新文档/).count(), 0, 'Question is an answer without an update badge');
    await send('empty reply');
    await sidebar.getByText('本次未修改文档。请补充需要填写的内容或具体问题。', { exact: true }).waitFor();
    assert.equal((await snapshot()).html, beforeQuestion.html, 'Empty reply cannot replace or clear the document');
    await send('数量改成 2');
    await page.getByTestId('mutation-sidebar').getByText(/请核对后补填/).waitFor();
    assert.deepEqual(writes.at(-1).history.slice(0, 2), [{ role: 'user', content: paymentQuestion }, { role: 'assistant', content: paymentAnswer }], 'Follow-up includes actual conversation history');
    assert.equal(writes.at(-1).history.at(-1).role, 'assistant', 'Current instruction is not duplicated in history');
    await sidebar.getByText('点击预览最新文档', { exact: true }).waitFor();
    const result = await snapshot();
    assert.ok(result.html.includes('HUMAN TERM') && result.html.includes('CUSTOM HEADER') && result.html.includes('WATERMARK'), 'Human layout survives editor AI mutation');
    assert.equal(result.data.items[0].qty, '2'); assert.equal(result.data.items[0].amount, '200.00'); assert.equal(result.data.values.total_amount, '190.00');
    if (kind === 'contract') assert.equal(result.data.values.say_total, '');
    // The variable form uses the same service and displays freight without tax inputs.
    const form = page.getByTestId('mutation-form');
    await form.getByRole('button', { name: '重新读取', exact: true }).click();
    await form.locator('#discount').fill('11.00');
    const before = writes.length;
    const applied = page.waitForResponse(r => r.url().includes('/api/ai/write-document') && r.request().postDataJSON().mode === 'apply');
    await form.getByRole('button', { name: '仅填充数据', exact: true }).first().click();
    await applied;
    assert.equal(writes.length, before + 1); assert.equal(writes.at(-1).mode, 'apply');
    await form.locator('#inland_freight').waitFor();
    assert.equal(await form.locator('#tax_rate, #tax_mode, #freight_tax_rate, #freight_tax_mode').count(), 0);
  }
  await mount('contract');
  const before = await snapshot();
  missingCanvas = true;
  await send('缺少校验文档');
  await page.getByTestId('mutation-sidebar').getByText(/缺少经过校验的文档/).waitFor();
  assert.equal((await snapshot()).html, before.html, 'Legacy variable-only replies cannot mutate document');
  missingCanvas = false;
  await send('delay');
  while (!held) await new Promise(resolve => setTimeout(resolve, 20));
  await page.evaluate(() => window.mutationEditor.current.setControlValues([{ conceptId: 'payment_terms', value: 'HUMAN LATEST' }]));
  release();
  await page.getByTestId('mutation-sidebar').getByText(/文档在处理期间已修改/).waitFor();
  assert.ok((await snapshot()).html.includes('HUMAN LATEST'), 'Late AI reply cannot overwrite manual edits');
  assert.deepEqual(errors, []);
  console.log(`PASS ${process.env.AI_TEST_MOBILE ? 'mobile' : 'desktop'}: payment question without mutation, follow-up history, shared AI/form mutation, totals, retained human content, missing-canvas and stale-reply protection.`);
} finally { if (errors.length) console.log(errors); if (release) release(); await browser.close(); }
