import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createAIChatTestStore } from './ai-chat-test-store.mjs';

// All requests are intercepted. No real mailbox, provider, or document is changed.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: process.env.AI_TEST_MOBILE ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const base = process.env.COMPOSE_TEST_URL || 'http://127.0.0.1:3097';
const email = 'sales@example.com', now = new Date().toISOString();
const store = createAIChatTestStore(email);
const conversation = { id: 'versions', title: 'Buyer', peerEmail: 'buyer@example.com', subject: 'Quote revisions', date: now, count: 1, unreadCount: 0, accountEmail: email, status: 'unanswered' };
const originalCanvas = '__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify({ template: 'custom', options: { watermark: { data: 'CUSTOM WATERMARK' } }, data: { main: [{ value: 'CUSTOM FREE TEXT' }, { type: 'control', control: { conceptId: 'payment_terms', value: [{ value: 'Net30', bold: true }] } }] } });
const proposedCanvas = originalCanvas.replace('Net30', 'Net60');
const action = (id, kind, name, baseRevision) => ({ id, role: 'assistant', content: name, createdAt: now, documentAction: { kind, documentId: 'quote', type: 'quotation', name, company: 'Buyer', values: { payment_terms: 'Net60' }, baseRevision, html: proposedCanvas } });
const missingCanvas = action('legacy-canvas', 'update', 'Missing original canvas', 2);
delete missingCanvas.documentAction.html;
store.mail.messages = [action('stale-update', 'update', 'Stale update', 1), action('stale-delete', 'delete', 'Stale delete', 1), action('valid-update', 'update', 'Current update', 2), action('legacy-update', 'update', 'Legacy update'), missingCanvas];
let document = { id: 'quote', revision: 2, type: 'quotation', name: 'Human edit', html: originalCanvas, updatedAt: now };
const writes = [], deletes = [], errors = [], composeDocuments = new Map();
const conflict = '文档已修改或删除，请重新打开最新文档，或让 AI 重新生成修改方案';
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname;
  const session = store.routeSessions(req, url);
  if (session) { await route.fulfill(session); return; }
  let json;
  if (path === '/api/documents' && req.method() === 'POST') {
    const input = req.postDataJSON(); writes.push(input);
    if (input.id !== 'quote') {
      const previous = composeDocuments.get(input.id);
      assert.equal(input.revision || 0, previous?.revision || 0);
      const saved = { ...input, revision: (previous?.revision || 0) + 1, updatedAt: now };
      composeDocuments.set(input.id, saved);
      await route.fulfill({ json: { ok: true, document: saved } }); return;
    }
    if (input.revision !== document.revision) { await route.fulfill({ status: 409, body: conflict }); return; }
    document = { ...input, revision: document.revision + 1, updatedAt: now };
    json = { ok: true, document };
  } else if (path === '/api/documents/quote' && req.method() === 'GET') {
    json = { document };
  } else if (path === '/api/documents/quote' && req.method() === 'DELETE') {
    deletes.push(url.searchParams.get('revision'));
    if (Number(url.searchParams.get('revision')) !== document.revision) { await route.fulfill({ status: 409, body: conflict }); return; }
    throw new Error('Stale card must not delete the current document');
  } else if (path === '/api/documents') json = { documents: [document] };
  else if (path === '/api/accounts') json = { accounts: [{ id: 'account', email, label: email, isActive: true }] };
  else if (path === '/api/accounts/account/brands') json = { brands: [], brandMode: 'prefer' };
  else if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail: email, conversations: [conversation], folders: [], accounts: [{ id: 'account', email, label: email, isActive: true }] };
  else if (path.endsWith('/ai-chat')) json = { ok: true, ...store.projectSession(store.mail, url), sessionId: store.mail.id };
  else if (path.startsWith('/api/conversations/')) json = { conversation: { ...conversation, messages: [{ id: '1', from: conversation.peerEmail, to: email, subject: 'Revisions', body: 'Inquiry', date: now, outgoing: false, flags: ['\\Seen'] }] } };
  else json = { calendar: false, notifications: false, models: [], agents: [], skills: [], bindings: [], documents: [], signatures: [], attachments: [], total: 0, stamps: [], templates: [], brands: [] };
  await route.fulfill({ json });
});
try {
  await page.goto(`${base}/inbox?conversation=${conversation.id}`);
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  const card = name => page.getByTestId('conversation-document-card').filter({ hasText: name, visible: true });
  await card('Stale update').getByRole('button', { name: '应用修改', exact: true }).click();
  await page.getByText(conflict, { exact: true }).first().waitFor();
  assert.equal(writes[0].revision, 1);
  assert.equal(document.revision, 2);
  assert.equal(document.name, 'Human edit');
  const deletionResponse = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url().includes('/api/documents/quote'));
  await card('Stale delete').getByRole('button', { name: '确认删除', exact: true }).click();
  assert.equal((await deletionResponse).status(), 409);
  assert.deepEqual(deletes, ['1']);
  await card('Legacy update').getByRole('button', { name: '应用修改', exact: true }).click();
  await page.getByText('此方案缺少文档版本，请让 AI 重新生成', { exact: true }).waitFor();
  assert.equal(writes.length, 1, 'Legacy cards must not create an unguarded write');
  await card('Missing original canvas').getByRole('button', { name: '应用修改', exact: true }).click();
  await page.getByText('此方案缺少原文档内容，请让 AI 重新生成', { exact: true }).waitFor();
  assert.equal(writes.length, 1, 'Old proposals must not replace a custom document with a default template');
  await card('Current update').getByRole('button', { name: '应用修改', exact: true }).click();
  await card('Current update').getByText('已更新《Current update》', { exact: true }).waitFor();
  assert.equal(document.revision, 3);
  assert.equal(writes[1].revision, 2);
  assert.equal(document.html, proposedCanvas, 'Applying a proposal preserves the server-patched custom canvas exactly');
  await card('Current update').getByTestId('conversation-document-view').click();
  const editor = page.getByTestId('document-editor-dialog');
  await editor.waitFor();
  const saved4 = page.waitForResponse(response => new URL(response.url()).pathname === '/api/documents' && response.request().method() === 'POST');
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  assert.equal((await (await saved4).json()).document.revision, 4);
  assert.equal(writes.at(-1).revision, 3, 'Reopening a saved proposal uses its new version');
  assert.equal(document.revision, 4);
  if (!process.env.AI_TEST_MOBILE) {
    await page.keyboard.press('Escape');
    await editor.waitFor({ state: 'hidden' });
    await page.getByTestId('conversation-detail').getByText('Inquiry', { exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: '回复', exact: true }).click();
    const compose = page.getByTestId('compose-inline');
    await compose.locator('.tiptap').waitFor();
    await compose.getByRole('button', { name: '新建报价单', exact: true }).click();
    await editor.waitFor();
    const saved1 = page.waitForResponse(response => new URL(response.url()).pathname === '/api/documents' && response.request().method() === 'POST');
    await editor.getByRole('button', { name: '保存', exact: true }).click();
    assert.equal((await (await saved1).json()).document.revision, 1);
    const createdId = writes.at(-1).id;
    const saved2 = page.waitForResponse(response => new URL(response.url()).pathname === '/api/documents' && response.request().method() === 'POST');
    await editor.getByRole('button', { name: '保存', exact: true }).click();
    assert.equal((await (await saved2).json()).document.revision, 2);
    assert.equal(writes.at(-1).revision, 1, 'Compose keeps the revision returned by its first save');
    assert.equal(writes.at(-1).id, createdId, 'Repeated save edits the same compose document');
  }
  assert.deepEqual(errors, []);
  console.log(`PASS ${process.env.AI_TEST_MOBILE ? 'mobile' : 'desktop'}: stale/legacy proposals blocked, original canvas retained, confirmed update and editor preserve revisions.`);
} finally { await browser.close(); }
