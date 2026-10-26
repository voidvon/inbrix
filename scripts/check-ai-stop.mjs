import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createAIChatTestStore } from './ai-chat-test-store.mjs';

// Isolated UI fixture. No real model, mailbox or document is contacted.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: process.env.AI_TEST_MOBILE ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const base = process.env.COMPOSE_TEST_URL || 'http://localhost:2342';
const email = 'me@example.com', now = new Date().toISOString();
const store = createAIChatTestStore(email);
const session = { ...store.mail, id: 'stop-document', kind: 'document', title: 'Stop document', documentType: 'quotation', createdAt: now, updatedAt: now };
store.sessions.set(session.id, session);
const requests = [], stops = [], errors = [];
page.on('pageerror', error => errors.push(error.stack));
await page.exposeFunction('recordAIGeneration', (path, payload) => {
  requests.push({ path, payload });
  if (path === '/api/ai/agent-chat') store.begin(payload);
});
await page.addInitScript(() => {
  window.EventSource = class { close() {} };
  const fetch = window.fetch;
  window.fetch = async (url, options) => {
    if (!['/api/ai/agent-chat', '/api/ai/write-document'].includes(url)) return fetch(url, options);
    await window.recordAIGeneration(url, JSON.parse(options.body));
    window.aiRequestCount = (window.aiRequestCount || 0) + 1;
    return new Promise((resolve, reject) => {
      const abort = () => { window.lastAIAborted = true; reject(new DOMException('Stopped', 'AbortError')); };
      if (options.signal.aborted) abort();
      else options.signal.addEventListener('abort', abort, { once: true });
    });
  };
});
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const req = route.request(), url = new URL(req.url());
  const sessions = store.routeSessions(req, url);
  if (sessions) { await route.fulfill(sessions); return; }
  let json = { calendar: false, notifications: false, conversations: [], documents: [], templates: [], agents: [], skills: [], bindings: [], brands: [], models: [], signatures: [], attachments: [], total: 0 };
  if (url.pathname === '/api/conversations') json = { ...json, locale: 'zh-CN', accountEmail: email, folders: [], accounts: [{ id: 'account', email, isActive: true }] };
  else if (url.pathname === '/api/accounts') json = { accounts: [{ id: 'account', email, isActive: true }] };
  else if (url.pathname === '/api/ai/stop') {
    const payload = req.postDataJSON();
    stops.push(payload);
    const current = requests.at(-1);
    assert.deepEqual(payload, { accountEmail: email, requestId: current.payload.requestId, ...(current.payload.sessionId ? { sessionId: current.payload.sessionId } : {}) });
    if (current.path === '/api/ai/agent-chat') {
      store.fail(current.payload);
      session.lastRun.retryable = false;
    }
    await route.fulfill({ status: 204 }); return;
  }
  await route.fulfill({ json });
});

try {
  await page.goto(`${base}/ai-document`);
  const panel = page.getByTestId('ai-document-chat-panel');
  const input = panel.locator('textarea');
  await input.fill('起草报价单');
  await input.press('Enter');
  await page.waitForFunction(() => window.aiRequestCount === 1);
  await panel.getByRole('button', { name: '停止生成', exact: true }).waitFor();
  const stopped = page.waitForResponse(response => response.url().endsWith('/api/ai/stop'));
  await panel.getByRole('button', { name: '停止生成', exact: true }).click();
  await stopped;
  await panel.getByText('已停止生成。', { exact: true }).waitFor();
  assert.equal(await input.isEnabled(), true);
  assert.equal(stops.length, 1);
  assert.equal(await page.evaluate(() => window.lastAIAborted), true);

  // Mount the real document sidebar with a minimal editor, keeping the document observable.
  await page.evaluate(async () => {
    const sidebarSource = await (await fetch('/src/components/app/document-editor-dialog.tsx')).text();
    const mainSource = await (await fetch('/src/main.tsx')).text();
    const specifier = (source, name) => source.match(new RegExp(`from ["']([^"']*${name}\\.js[^"']*)["']`))[1];
    const React = (await import(specifier(sidebarSource, 'react'))).default;
    const { createRoot } = (await import(specifier(mainSource, 'react-dom_client'))).default;
    const { QueryClient, QueryClientProvider } = await import(specifier(sidebarSource, '@tanstack_react-query'));
    const { DocumentAISidebar } = await import('/src/components/app/document-editor-dialog.tsx');
    const { zh } = await import('/src/lib/locale.ts');
    window.editorDocument = '__INBRIX_CANVAS_DOCUMENT__:original';
    const editor = { getDocument: () => window.editorDocument, setHTML: value => { window.editorDocument = value; }, focus() {} };
    const host = document.createElement('div');
    host.style.cssText = 'position:fixed;inset:0;z-index:99999;display:flex;background:white';
    document.body.append(host);
    createRoot(host).render(React.createElement(QueryClientProvider, { client: new QueryClient() }, React.createElement(DocumentAISidebar, { copy: zh, editor, accountEmail: 'me@example.com', type: 'quotation', title: 'Test', isMobile: true })));
    window.lastAIAborted = false;
  });
  const sidebarInput = page.getByPlaceholder('输入对当前文档的修改要求…');
  await sidebarInput.fill('修改交货期');
  await sidebarInput.press('Enter');
  await page.waitForFunction(() => window.aiRequestCount === 2);
  const stopSidebar = page.getByRole('button', { name: '停止生成', exact: true });
  await stopSidebar.waitFor();
  const stoppedSidebar = page.waitForResponse(response => response.url().endsWith('/api/ai/stop'));
  await stopSidebar.click();
  await stoppedSidebar;
  await page.getByText('已停止生成。', { exact: true }).last().waitFor();
  assert.equal(await sidebarInput.isEnabled(), true);
  assert.equal(await page.evaluate(() => window.lastAIAborted), true);
  assert.equal(await page.evaluate(() => window.editorDocument), '__INBRIX_CANVAS_DOCUMENT__:original', 'Stopped generation must leave the document intact');
  assert.equal(stops.length, 2);
  assert.deepEqual(errors, []);
  console.log('Passed: document chat and editor stop buttons abort local requests, cancel exact backend requests, unlock input and preserve documents.');
} catch (error) {
  console.error(await page.locator('body').innerText().catch(() => 'Page unavailable'));
  throw error;
} finally {
  await browser.close();
}
