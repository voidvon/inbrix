import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Synthetic data only. Every API request is intercepted; no mail is sent.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3198';
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
page.setDefaultTimeout(20000);
const now = new Date().toISOString();
const account = { id: 'sales', email: 'sales@example.invalid', label: 'Sales', isActive: true };
let documents = Array.from({ length: 24 }, (_, i) => ({ id: `doc-${i}`, revision: 2, type: i % 2 ? 'contract' : 'quotation', name: `文档 ${i}`, company: `客户 ${i}`, updatedAt: now, createdAt: now }));
const calls = [], saves = [], errors = [];
let failDetail = false;
let failList = false;
let delayFirstDetail = false;
let releaseFirstDetail;
const firstDetailGate = new Promise(resolve => { releaseFirstDetail = resolve; });
page.on('pageerror', error => errors.push(error.stack));
await page.addInitScript(() => {
  localStorage.setItem('inbrix-documents', JSON.stringify([{ id: 'doc-0', type: 'quotation', name: '过期缓存', revision: 1, html: 'OLD-CONTENT'.repeat(40000), updatedAt: new Date().toISOString() }]));
  window.documentCacheReads = 0;
  window.documentCacheWrites = 0;
  window.documentReadCacheModes = [];
  const getItem = Storage.prototype.getItem, setItem = Storage.prototype.setItem;
  window.hasLegacyDocumentCache = () => getItem.call(localStorage, 'inbrix-documents') !== null;
  Storage.prototype.getItem = function(key) {
    if (key === 'inbrix-documents') { window.documentCacheReads++; throw new Error('Document cache reads are forbidden'); }
    return getItem.call(this, key);
  };
  Storage.prototype.setItem = function(key, value) {
    if (key === 'inbrix-documents') { window.documentCacheWrites++; throw new Error('Document cache writes are forbidden'); }
    return setItem.call(this, key, value);
  };
  const fetch = window.fetch;
  window.fetch = function(input, init) {
    if (typeof input === 'string' && input.startsWith('/api/documents') && (!init?.method || init.method === 'GET')) window.documentReadCacheModes.push(init?.cache);
    return fetch.call(this, input, init);
  };
});
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-csrf' } }));
await page.route('**/api/**', async route => {
  const req = route.request(), path = new URL(req.url()).pathname, method = req.method();
  calls.push({ path, method });
  if (path === '/api/documents' && method === 'GET') return failList ? route.fulfill({ status: 503, json: { error: '文档列表暂不可用' } }) : route.fulfill({ json: { documents } });
  if (path === '/api/documents' && method === 'POST') {
    const input = req.postDataJSON(); saves.push(input);
    const saved = { ...input, revision: input.revision + 1, updatedAt: now };
    documents = documents.map(doc => doc.id === saved.id ? { ...doc, name: saved.name, revision: saved.revision } : doc);
    return route.fulfill({ json: { ok: true, document: saved } });
  }
  if (path === '/api/documents/batch-delete') {
    const { ids } = req.postDataJSON(); documents = documents.filter(doc => !ids.includes(doc.id));
    return route.fulfill({ json: { ok: true, count: ids.length } });
  }
  if (path.startsWith('/api/documents/')) {
    const id = path.split('/').at(-1), doc = documents.find(item => item.id === id);
    if (delayFirstDetail && id === 'doc-0') await firstDetailGate;
    if (failDetail || !doc) return route.fulfill({ status: 404, json: { error: '文档已删除' } });
    return route.fulfill({ json: { document: { ...doc, name: `服务器最新 ${id}`, revision: 7, html: `__INBRIX_CANVAS_DOCUMENT__:${JSON.stringify({ template: 'document', data: { main: [{ value: `LATEST-SERVER-CONTENT-${id}`, size: 16 }] } })}` } } });
  }
  let json = { calendar: false, notifications: false, models: [], agents: [], skills: [], bindings: [], signatures: [], templates: [], stamps: [], documents: [], attachments: [], total: 0 };
  if (path === '/api/accounts') json = { accounts: [account] };
  if (path === '/api/conversations') json = { locale: 'zh-CN', accounts: [account], accountEmail: account.email, conversations: [], folders: [] };
  if (path === '/api/capabilities') json = { role: 'user', calendar: false, notifications: false };
  await route.fulfill({ json });
});
const editor = page.getByTestId('document-editor-dialog');
const clickRow = name => page.getByRole('row').filter({ has: page.getByText(name, { exact: true }) }).click();
try {
  await page.goto(`${base}/documents`);
  await page.getByRole('cell', { name: '文档 0', exact: true }).waitFor();
  assert.equal(calls.some(call => call.path.startsWith('/api/documents/')), false, 'Opening the list must not prefetch details');
  assert.equal(calls.some(call => call.path === '/api/document-templates'), false, 'Templates must be deferred');
  assert.equal(await page.evaluate(() => window.documentCacheReads), 0, 'List must not parse old full-content cache');
  assert.equal(await page.evaluate(() => window.hasLegacyDocumentCache()), false, 'Startup must remove the former document cache');
  assert.equal(await page.getByRole('cell', { name: /^文档 \d+$/ }).count(), 20);
  await page.locator('[data-slot=pagination-link][aria-label="下一页"]').click();
  await page.getByRole('cell', { name: '文档 20', exact: true }).waitFor();
  await page.locator('[data-slot=pagination-link][aria-label="上一页"]').click();
  await page.getByRole('group').getByRole('button', { name: '合同', exact: true }).click();
  assert.equal(await page.getByRole('cell', { name: /^文档 \d+$/ }).count(), 12);
  await page.getByRole('group').getByRole('button').first().click();

  // A later click must win even if an earlier detail response arrives last.
  delayFirstDetail = true;
  const first = page.waitForRequest(req => new URL(req.url()).pathname === '/api/documents/doc-0');
  await clickRow('文档 0'); await first;
  await clickRow('文档 1');
  await editor.getByRole('textbox', { name: '文档名称', exact: true }).waitFor();
  assert.equal(await editor.getByRole('textbox', { name: '文档名称', exact: true }).inputValue(), '服务器最新 doc-1');
  releaseFirstDetail(); delayFirstDetail = false;
  if (mobile) await editor.getByRole('tab', { name: '预览文档', exact: true }).click();
  await editor.locator('canvas').first().waitFor();
  const savedResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/documents' && response.request().method() === 'POST');
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  assert.equal((await (await savedResponse).json()).document.revision, 8);
  assert.equal(saves.at(-1).revision, 7, 'Saving must use the detail revision');
  assert.ok(saves.at(-1).html.includes('LATEST-SERVER-CONTENT-doc-1'), 'Saving must preserve the full fetched content');
  await page.keyboard.press('Escape'); await editor.waitFor({ state: 'hidden' });

  failDetail = true;
  await clickRow('文档 2');
  await page.getByText('文档已删除', { exact: true }).first().waitFor();
  assert.equal(await editor.count(), 0, 'A failed detail request must not create an empty document');
  failDetail = false;
  await page.getByRole('button', { name: '删除文档: 文档 2', exact: true }).click();
  const confirmation = page.getByRole('dialog').last();
  await confirmation.getByRole('button', { name: '删除文档', exact: true }).click();
  await page.getByRole('cell', { name: '文档 2', exact: true }).waitFor({ state: 'detached' });

  await page.goto(`${base}/documents/doc-0`);
  await editor.getByRole('textbox', { name: '文档名称', exact: true }).waitFor();
  assert.equal(await editor.getByRole('textbox', { name: '文档名称', exact: true }).inputValue(), '服务器最新 doc-0', 'Deep links must fetch the authoritative detail');
  await page.keyboard.press('Escape'); await editor.waitFor({ state: 'hidden' });
  // Exercise the real attachment picker independently of the surrounding
  // composer. It must list metadata and fetch only the selected PDF source.
  const beforePicker = calls.filter(call => call.path.startsWith('/api/documents/')).length;
  await page.evaluate(async () => {
    const source = await (await fetch('/src/main.tsx')).text();
    const reactURL = source.match(/from ["']([^"']*\/react\.js[^"']*)["']/)?.[1];
    const rootURL = source.match(/from ["']([^"']*\/react-dom_client\.js[^"']*)["']/)?.[1];
    if (!reactURL || !rootURL) throw new Error('Cannot locate the app React modules');
    const reactModule = await import(reactURL), rootModule = await import(rootURL);
    const React = reactModule.default || reactModule;
    const { createRoot } = rootModule.default || rootModule;
    const { ComposeDocumentAttachment } = await import('/src/components/app/document-editor-dialog.tsx');
    const { zh } = await import('/src/lib/locale.ts');
    const container = document.createElement('div'); document.body.append(container);
    const root = createRoot(container);
    window.testDocumentAttachments = [];
    root.render(React.createElement(ComposeDocumentAttachment, { copy: zh, accountEmail: 'sales@example.invalid', action: 'import',
      onClose: () => { root.unmount(); container.remove(); },
      onAttach: file => window.testDocumentAttachments.push({ name: file.name, size: file.size, type: file.type }),
      onBusyChange: busy => { window.testDocumentImportBusy = busy; },
    }));
  });
  const picker = page.getByTestId('document-attachment-picker');
  await picker.getByRole('button', { name: /文档 0/ }).waitFor();
  assert.equal(calls.filter(call => call.path.startsWith('/api/documents/')).length, beforePicker, 'Picker must not preload full content');
  failDetail = true;
  await picker.getByRole('button', { name: /文档 0/ }).click();
  await page.waitForFunction(() => window.testDocumentImportBusy === false);
  assert.equal(await page.evaluate(() => window.testDocumentAttachments.length), 0, 'Failed detail lookup must not attach an empty PDF');
  failDetail = false;
  await picker.getByRole('button', { name: /文档 0/ }).click();
  await picker.waitFor({ state: 'hidden' });
  const attachments = await page.evaluate(() => window.testDocumentAttachments);
  assert.equal(attachments.length, 1);
  assert.equal(attachments[0].type, 'application/pdf');
  assert.ok(attachments[0].size > 0);
  assert.equal(calls.filter(call => call.path === '/api/documents/doc-0').length >= 3, true);
  await page.screenshot({ path: `/private/tmp/inbrix-document-list-${mobile ? 'mobile' : 'desktop'}.png` });
  failList = true;
  await page.goto(`${base}/documents`);
  await page.getByText('文档列表暂不可用', { exact: true }).first().waitFor();
  assert.equal(await page.getByRole('cell', { name: /^文档 \d+$/ }).count(), 0, 'Failed list reads must not fall back to local content');
  assert.deepEqual(await page.evaluate(() => [window.documentCacheReads, window.documentCacheWrites, window.hasLegacyDocumentCache()]), [0, 0, false]);
  const cacheModes = await page.evaluate(() => window.documentReadCacheModes);
  assert.ok(cacheModes.length > 0 && cacheModes.every(mode => mode === 'no-store'));
  assert.deepEqual(errors, []);
  console.log(`Passed (${mobile ? 'mobile' : 'desktop'}): metadata-only list, deferred templates, no document cache, legacy cleanup, paging/filtering, lazy detail, latest-click-wins, authoritative save, detail failure, deletion, deep link and PDF attachment import.`);
} catch (error) {
  await page.screenshot({ path: `/private/tmp/inbrix-document-list-failure-${mobile ? 'mobile' : 'desktop'}.png` });
  throw error;
} finally { releaseFirstDetail(); await browser.close(); }
