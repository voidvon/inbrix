import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createAIChatTestStore } from './ai-chat-test-store.mjs';

// Synthetic resources and intercepted APIs only; no real mailbox or AI calls.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3200';
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
page.setDefaultTimeout(20000);
page.on('dialog', dialog => dialog.accept());
const email = 'sales@example.invalid', now = new Date().toISOString();
const account = { id: 'account', email, label: 'Sales', isActive: true };
const keys = ['inbrix-documents', 'inbrix-document-templates', 'inbrix-deleted-document-templates', 'inbrix-document-stamps', 'inbrix-ai-document-sessions'];
await page.addInitScript(keys => {
  for (const key of keys) localStorage.setItem(key, JSON.stringify([{ name: 'LOCAL-ONLY-DATA', html: 'LOCAL-CONTENT' }]));
  const get = Storage.prototype.getItem, set = Storage.prototype.setItem;
  window.resourceStorageAccesses = [];
  window.remainingResourceKeys = () => keys.filter(key => get.call(localStorage, key) !== null);
  Storage.prototype.getItem = function(key) { if (keys.includes(key)) { window.resourceStorageAccesses.push(key); throw new Error('Forbidden resource cache read'); } return get.call(this, key); };
  Storage.prototype.setItem = function(key, value) { if (keys.includes(key)) { window.resourceStorageAccesses.push(key); throw new Error('Forbidden resource cache write'); } return set.call(this, key, value); };
  const fetch = window.fetch;
  window.resourceReadModes = [];
  window.fetch = function(input, init) {
    if (typeof input === 'string' && /^\/api\/(documents|document-templates|document-stamps|ai\/chat-sessions)/.test(input) && (!init?.method || init.method === 'GET')) window.resourceReadModes.push(init?.cache);
    return fetch.call(this, input, init);
  };
}, keys);
let templates = [
  { id: 'quote-template', type: 'quotation', name: '服务器报价模板', html: '<div data-spirax-quotation="reference-v1"></div>', updatedAt: now },
  { id: 'contract-template', type: 'contract', name: '服务器合同模板', html: '<div data-spirax-contract="reference-v1"></div>', updatedAt: now },
];
let stamps = [];
let failTemplates = false, failStamps = false, failStampSave = false, failStampDelete = false, failSession = false, failSessions = false;
const calls = [], errors = [];
page.on('pageerror', error => errors.push(error.stack));
const store = createAIChatTestStore(email);
const session = { id: 'resources-session', kind: 'document', title: '服务端文档对话', documentType: 'quotation', version: 0, messages: [{ id: 'response', role: 'assistant', content: 'SERVER-MESSAGE-ONE', createdAt: now }], businessContext: { agentId: '', skillId: '', brands: [] }, createdAt: now, updatedAt: now };
store.sessions.set(session.id, session);
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname, method = req.method();
  calls.push({ path, method });
  if (path.startsWith('/api/ai/chat-sessions')) {
    if (method === 'GET' && (path.endsWith(session.id) ? failSession : failSessions)) return route.fulfill({ status: 503, json: { error: path.endsWith(session.id) ? '会话详情失败' : '会话列表失败' } });
    const response = store.routeSessions(req, url);
    if (response) return route.fulfill(response);
  }
  if (path === '/api/document-templates' && method === 'GET') return failTemplates ? route.fulfill({ status: 503, json: { error: '模板接口失败' } }) : route.fulfill({ json: { templates } });
  if (path === '/api/document-templates' && method === 'POST') {
    const template = { ...req.postDataJSON(), updatedAt: now };
    templates = [template, ...templates.filter(item => item.id !== template.id)];
    return route.fulfill({ json: { ok: true, template } });
  }
  if (path.startsWith('/api/document-templates/') && method === 'DELETE') { templates = templates.filter(item => item.id !== path.split('/').at(-1)); return route.fulfill({ json: { ok: true } }); }
  if (path === '/api/document-stamps' && method === 'GET') return failStamps ? route.fulfill({ status: 503, json: { error: '印章接口失败' } }) : route.fulfill({ json: { stamps } });
  if (path === '/api/document-stamps' && method === 'POST') {
    if (failStampSave) return route.fulfill({ status: 503, json: { error: '印章保存失败' } });
    const input = req.postDataJSON();
    const stamp = { ...input, name: '服务器处理的印章', dataUrl: input.dataUrl || input.value };
    stamps = [stamp, ...stamps.filter(item => item.id !== stamp.id)];
    return route.fulfill({ json: { ok: true, stamp } });
  }
  if (path.startsWith('/api/document-stamps/') && method === 'DELETE') {
    if (failStampDelete) return route.fulfill({ status: 503, json: { error: '印章删除失败' } });
    stamps = stamps.filter(item => item.id !== path.split('/').at(-1));
    return route.fulfill({ json: { ok: true } });
  }
  let json = { documents: [], templates: [], stamps: [], models: [], agents: [], skills: [], bindings: [], signatures: [], brands: [], attachments: [], total: 0 };
  if (path === '/api/accounts') json = { accounts: [account] };
  if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail: email, accounts: [account], conversations: [], folders: [] };
  if (path === '/api/capabilities') json = { role: 'user', calendar: false, notifications: false };
  if (path === '/api/accounts/account/brands') json = { brands: [], brandMode: 'prefer' };
  await route.fulfill({ json });
});
const templateMenu = page.getByRole('button', { name: '模板管理', exact: true });
const stampMenu = page.getByRole('button', { name: '印章管理', exact: true });
const navigate = path => page.evaluate(path => { history.pushState(null, '', path); dispatchEvent(new PopStateEvent('popstate')); }, path);
try {
  await page.goto(`${base}/documents`);
  await templateMenu.click();
  await page.getByRole('button', { name: /^服务器报价模板/ }).waitFor();
  assert.deepEqual(await page.evaluate(() => window.remainingResourceKeys()), []);
  await templateMenu.click();
  templates[0].name = '服务器最新报价模板';
  await templateMenu.click();
  await page.getByRole('button', { name: /^服务器最新报价模板/ }).waitFor();
  await templateMenu.click(); failTemplates = true;
  await templateMenu.click();
  await page.getByText('模板接口失败', { exact: true }).first().waitFor();
  assert.equal(await page.getByRole('button', { name: /^服务器最新报价模板/ }).count(), 0, 'Failed template reads must not show previous or local templates');
  await templateMenu.click(); failTemplates = false;
  await templateMenu.click();
  await page.getByRole('button', { name: '删除模板: 服务器最新报价模板', exact: true }).click();
  const confirmation = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '删除模板', exact: true }) });
  await confirmation.getByRole('button', { name: '删除模板', exact: true }).click();
  await confirmation.waitFor({ state: 'hidden' });
  await page.keyboard.press('Escape');
  await templateMenu.click();
  await page.getByRole('button', { name: /^服务器合同模板/ }).waitFor();
  assert.equal(await page.getByRole('button', { name: /^服务器最新报价模板/ }).count(), 0);
  await templateMenu.click();

  await stampMenu.click();
  let stampDialog = page.getByRole('dialog').last();
  const png = await page.evaluate(() => { const canvas = document.createElement('canvas'); canvas.width = canvas.height = 8; const ctx = canvas.getContext('2d'); ctx.fillStyle = 'red'; ctx.fillRect(0, 0, 8, 8); return canvas.toDataURL().split(',')[1]; });
  await stampDialog.locator('input[type=file]').setInputFiles({ name: 'local-upload.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') });
  await stampDialog.getByRole('img', { name: '服务器处理的印章', exact: true }).waitFor();
  assert.equal(stamps.length, 1, 'Upload must use the server response');
  const width = () => stampDialog.getByRole('spinbutton', { name: '印章宽度（毫米）', exact: true });
  const widthSaved = page.waitForResponse(response => new URL(response.url()).pathname === '/api/document-stamps' && response.request().method() === 'POST');
  await width().fill('57'); await width().blur();
  await widthSaved;
  assert.equal(stamps[0].insertWidth, 57);
  await page.keyboard.press('Escape');
  await stampMenu.click(); stampDialog = page.getByRole('dialog').last();
  await stampDialog.getByRole('img', { name: '服务器处理的印章', exact: true }).waitFor();
  assert.equal(await width().inputValue(), '57');
  failStampSave = true;
  await width().fill('70'); await width().blur();
  await page.getByText('印章保存失败', { exact: true }).first().waitFor();
  assert.equal(await width().inputValue(), '57'); failStampSave = false;
  failStampDelete = true;
  await stampDialog.getByRole('button', { name: '删除印章', exact: true }).click();
  await page.getByText('印章删除失败', { exact: true }).first().waitFor();
  assert.equal(await stampDialog.getByRole('img').count(), 1); failStampDelete = false;
  await stampDialog.getByRole('button', { name: '删除印章', exact: true }).click();
  await stampDialog.getByRole('img').waitFor({ state: 'detached' });
  await page.keyboard.press('Escape'); failStamps = true;
  await stampMenu.click();
  await page.getByText('印章接口失败', { exact: true }).first().waitFor();
  assert.equal(await page.getByRole('dialog').getByRole('img').count(), 0);
  await page.keyboard.press('Escape'); failStamps = false;

  await navigate('/ai-document');
  const panel = page.getByTestId('ai-document-chat-panel');
  await panel.getByText('SERVER-MESSAGE-ONE', { exact: true }).waitFor();
  const before = calls.filter(call => call.path.endsWith(session.id)).length;
  await navigate('/documents'); await templateMenu.waitFor();
  session.messages[0].content = 'SERVER-MESSAGE-TWO';
  await navigate('/ai-document');
  await panel.getByText('SERVER-MESSAGE-TWO', { exact: true }).waitFor();
  assert.ok(calls.filter(call => call.path.endsWith(session.id)).length > before, 'Reentering must fetch the server, not reuse a session snapshot');
  failSession = true;
  await page.evaluate(() => dispatchEvent(new Event('inbrix-documents-changed')));
  await panel.getByRole('alert').filter({ hasText: '会话详情失败' }).waitFor();
  assert.equal(await panel.getByText('SERVER-MESSAGE-TWO', { exact: true }).count(), 0, 'Failed refresh must hide previous session contents');
  failSession = false;
  if (mobile) {
    await page.mouse.move(5, page.viewportSize().height - 10);
    await page.waitForFunction(() => !document.querySelector('[data-sonner-toast]'));
  }
  await panel.getByRole('alert').filter({ hasText: '会话详情失败' }).getByRole('button', { name: '重试', exact: true }).click();
  await panel.getByText('SERVER-MESSAGE-TWO', { exact: true }).waitFor();
  await navigate('/documents'); await templateMenu.waitFor(); failSessions = true;
  await navigate('/ai-document');
  await panel.getByRole('alert').filter({ hasText: '会话列表失败' }).waitFor();
  assert.equal(await panel.getByText('SERVER-MESSAGE-TWO', { exact: true }).count(), 0);
  assert.deepEqual(await page.evaluate(() => window.resourceStorageAccesses), []);
  assert.deepEqual(await page.evaluate(() => window.remainingResourceKeys()), []);
  const modes = await page.evaluate(() => window.resourceReadModes);
  assert.ok(modes.length > 0 && modes.every(mode => mode === 'no-store'));
  assert.deepEqual(errors, []);
  console.log(`PASS ${mobile ? 'mobile' : 'desktop'}: no resource storage access, five legacy keys cleared, fresh template/stamp reads, server deletion and width, mutation errors, AI view reentry, failed refresh without stale contents, explicit retry and no-store requests.`);
} catch (error) {
  await page.screenshot({ path: `/private/tmp/inbrix-document-resources-failure-${mobile ? 'mobile' : 'desktop'}.png` });
  throw error;
} finally { await browser.close(); }
