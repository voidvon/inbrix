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
  // The fixture stands for an already validated server reply. The Go tests
  // independently exercise the actual arithmetic, catalog and layout checks.
  if (input.values?.currency === 'USD') { await route.fulfill({ status: 422, body: '已有金额的币种不能直接更改' }); return; }
  const payload = JSON.parse(input.canvas.slice(prefix.length));
  const fields = { item_qty: '2', item_qty_0: '2', item_amount: '200.00', item_amount_0: '200.00', subtotal: '200.00', total_amount: '190.00', contract_amount: '190.00', say_total: '' };
  if (input.mode === 'apply') Object.assign(fields, input.values);
  const walk = node => {
    if (Array.isArray(node)) for (const child of node) walk(child);
    else if (node && typeof node === 'object') {
      if (node.control?.conceptId in fields) node.control.value = [{ value: fields[node.control.conceptId] }];
      for (const [key, value] of Object.entries(node)) if (key !== 'control') walk(value);
    }
  };
  walk(payload.data);
  Object.assign(payload.business.values, fields);
  await route.fulfill({ json: { mode: 'variables', html: prefix + JSON.stringify(payload), summary: '已核算总额 190.00；已清空金额大写，请核对后补填' } });
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
        ready && React.createElement('div', { 'data-testid': 'mutation-sidebar', style: { height: '600px', display: 'flex' } }, React.createElement(DocumentAISidebar, { copy: zh, editor: ref.current, accountEmail: 'me@example.com', type: kind, title: 'Test', isMobile: true })),
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
    const identity = await page.evaluate(() => {
      const editor = window.mutationEditor.current, original = editor.getDocument();
      const payload = JSON.parse(original.slice('__INBRIX_CANVAS_DOCUMENT__:'.length));
      const visit = node => {
        if (Array.isArray(node)) return node.forEach(visit);
        if (!node || typeof node !== 'object') return;
        if (/^item_(model|description)$/.test(node.control?.conceptId || '')) {
          node.control.extension = { model: 'CATALOG', productId: 'p', productVersion: 2, variantId: 'v', priceId: 'price' };
        }
        Object.values(node).forEach(visit);
      };
      visit(payload.data);
      editor.setHTML('__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify(payload));
      editor.setHTML(editor.getDocument());
      const item = editor.getDocumentData().items[0];
      editor.setHTML(original);
      return item;
    });
    assert.equal(identity.productId, 'p'); assert.equal(identity.productVersion, 2);
    assert.equal(identity.variantId, 'v'); assert.equal(identity.priceId, 'price');
    await send('数量改成 2');
    await page.getByTestId('mutation-sidebar').getByText(/请核对后补填/).waitFor();
    const result = await snapshot();
    assert.ok(result.html.includes('HUMAN TERM') && result.html.includes('CUSTOM HEADER') && result.html.includes('WATERMARK'), 'Human layout survives editor AI mutation');
    assert.equal(result.data.items[0].qty, '2'); assert.equal(result.data.items[0].amount, '200.00'); assert.equal(result.data.values.total_amount, '190.00');
    if (kind === 'contract') assert.equal(result.data.values.say_total, '');
    // The variable form uses the same service and displays tax/freight inputs.
    const form = page.getByTestId('mutation-form');
    await form.getByRole('button', { name: '重新读取', exact: true }).click();
    await form.locator('#discount').fill('10.00');
    const before = writes.length;
    const applied = page.waitForResponse(r => r.url().includes('/api/ai/write-document') && r.request().postDataJSON().mode === 'apply');
    await form.getByRole('button', { name: '仅填充数据', exact: true }).first().click();
    await applied;
    assert.equal(writes.length, before + 1); assert.equal(writes.at(-1).mode, 'apply');
    await form.locator('#freight_tax_rate').waitFor();
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
  console.log(`PASS ${process.env.AI_TEST_MOBILE ? 'mobile' : 'desktop'}: shared AI/form mutation, decimal totals, retained human content, missing-canvas and stale-reply protection.`);
} finally { if (errors.length) console.log(errors); if (release) release(); await browser.close(); }
