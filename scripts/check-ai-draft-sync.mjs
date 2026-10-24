import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { chromium } from 'playwright';
import { createAIChatTestStore } from './ai-chat-test-store.mjs';

// Isolated UI fixture: no real documents, mailbox or AI provider are used.
const browser = await chromium.launch({ headless: true });
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const base = process.env.COMPOSE_TEST_URL || 'http://127.0.0.1:3097';
const email = 'sales@example.com', now = new Date().toISOString();
const store = createAIChatTestStore(email);
const generated = (id, title, type = 'quotation') => ({ id, role: 'assistant', content: title, createdAt: now, document: { type, title, counterparty: 'Buyer', values: { customer_company: 'Buyer', payment_terms: 'Net30', tax_rate: '13%', tax_mode: 'included', discount: '10.00', subtotal: '100.00', tax_amount: '10.35', total_amount: '90.00', currency: 'CNY' }, items: [{ model: 'MANUAL', description: 'Manual valve', qty: '1', price: '100', amount: '100' }] } });
store.mail.messages = [generated('mail-draft', 'Mail generated')];
const docs = { id: 'doc-session', kind: 'document', title: 'Draft sync', documentType: 'contract', version: 0, messages: [generated('doc-draft', 'Document generated', 'contract')], businessContext: { agentId: '', skillId: '', brands: [] }, createdAt: now, updatedAt: now };
store.sessions.set(docs.id, docs);
const conversation = { id: 'draft-sync', title: 'Buyer', peerEmail: 'buyer@example.com', subject: 'Draft sync', date: now, count: 1, unreadCount: 0, accountEmail: email, status: 'unanswered' };
const documents = new Map(), origins = new Map(), writes = [], errors = [];
const changeSession = id => {
  const origin = origins.get(id); if (!origin) return;
  const session = store.sessions.get(origin.sessionId);
  session.version++;
  session.messages.find(message => message.id === origin.messageId).document.savedDocumentId = id;
};
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname;
  const session = store.routeSessions(req, url);
  if (session) { await route.fulfill(session); return; }
  let json;
  if (path === '/api/documents' && req.method() === 'POST') {
    const input = req.postDataJSON(), previous = documents.get(input.id);
    const canvas = JSON.parse(input.html.slice('__INBRIX_CANVAS_DOCUMENT__:'.length));
    assert.equal(canvas.business.version, 1);
    for (const [key, value] of Object.entries({ tax_rate: '13%', tax_mode: 'included', discount: '10.00', total_amount: '90.00', currency: 'CNY' })) {
      assert.equal(canvas.business.values[key], value, `Commercial condition ${key} survives every save`);
    }
    assert.equal(input.revision || 0, previous?.revision || 0, 'Editor carries its actual saved revision');
    if (!previous) {
      assert.deepEqual(input.aiOrigin, { sessionId: input.id === 'mail-draft' ? store.mail.id : docs.id, messageId: input.id }, 'First save carries durable session/card origin');
      origins.set(input.id, input.aiOrigin);
    }
    const saved = { ...input, aiOrigin: undefined, revision: (previous?.revision || 0) + 1, updatedAt: now };
    documents.set(input.id, saved); writes.push(input); changeSession(input.id);
    json = { ok: true, document: saved };
  } else if (path.startsWith('/api/documents/')) {
    const document = documents.get(decodeURIComponent(path.split('/').at(-1)));
    if (!document) { await route.fulfill({ status: 404, json: { error: 'Document not found' } }); return; }
    json = { document };
  } else if (path === '/api/documents') json = { documents: [...documents.values()] };
  else if (path === '/api/accounts') json = { accounts: [{ id: 'account', email, label: email, isActive: true }] };
  else if (path === '/api/accounts/account/brands') json = { brands: [], brandMode: 'prefer' };
  else if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail: email, conversations: [conversation], folders: [], accounts: [{ id: 'account', email, label: email, isActive: true }] };
  else if (path.endsWith('/ai-chat')) json = { ok: true, ...store.projectSession(store.mail, url), sessionId: store.mail.id };
  else if (path.startsWith('/api/conversations/')) json = { conversation: { ...conversation, messages: [{ id: '1', from: conversation.peerEmail, to: email, subject: 'Draft sync', body: 'Inquiry', date: now, outgoing: false, flags: ['\\Seen'] }] } };
  else json = { calendar: false, notifications: false, models: [], agents: [], skills: [], bindings: [], documents: [], signatures: [], attachments: [], total: 0, stamps: [], templates: [], brands: [] };
  await route.fulfill({ json });
});
const editor = page.getByTestId('document-editor-dialog');
const saveEditor = async (id, revision) => {
  if (mobile) {
    // A preceding success toast covers the narrow header. Move away so its
    // hover pause ends, then let it dismiss before using the header again.
    await page.mouse.move(5, page.viewportSize().height - 10);
    await page.waitForFunction(() => !document.querySelector('[data-sonner-toast]'));
    await editor.getByRole('tab', { name: '预览文档', exact: true }).click();
  }
  const response = page.waitForResponse(response => new URL(response.url()).pathname === '/api/documents' && response.request().method() === 'POST');
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  const { document } = await (await response).json();
  assert.equal(document.id, id);
  assert.equal(document.revision, revision);
};
const closeEditor = async () => { await page.keyboard.press('Escape'); await editor.waitFor({ state: 'hidden' }); };
try {
  await page.goto(`${base}/inbox?conversation=${conversation.id}`);
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  const card = () => page.getByTestId('conversation-document-card').filter({ hasText: 'Mail generated', visible: true });
  await card().getByRole('button', { name: '保存到报价单与合同', exact: true }).click();
  await card().getByText('已保存《Mail generated》', { exact: true }).waitFor();
  assert.equal(documents.get('mail-draft').revision, 1);
  await page.reload();
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await card().getByRole('button', { name: '保存到报价单与合同', exact: true }).click();
  await card().getByText('已保存《Mail generated》', { exact: true }).waitFor();
  assert.equal(writes.length, 1, 'Saving the same card after refresh creates no duplicate or overwrite');
  // Simulate a later library edit, then reopen the old generated card.
  documents.set('mail-draft', { ...documents.get('mail-draft'), name: 'Mail library latest', revision: 2 }); changeSession('mail-draft');
  await card().getByTestId('conversation-document-view').click();
  await editor.waitFor();
  assert.equal(await editor.getByRole('textbox', { name: '文档名称', exact: true }).inputValue(), 'Mail library latest');
  await editor.getByRole('textbox', { name: '文档名称', exact: true }).fill('Mail human edited');
  await saveEditor('mail-draft', 3);
  assert.equal(writes.at(-1).revision, 2, 'Mail card opens current saved content with matching revision');
  await closeEditor();

  await page.goto(`${base}/ai-document`);
  await page.getByRole('button', { name: '在文档编辑器中打开', exact: true }).filter({ visible: true }).click();
  await editor.waitFor();
  await editor.getByRole('textbox', { name: '文档名称', exact: true }).fill('Document human edited');
  await saveEditor('doc-draft', 1);
  assert.deepEqual(writes.at(-1).aiOrigin, { sessionId: docs.id, messageId: 'doc-draft' });
  await saveEditor('doc-draft', 2);
  await closeEditor();
  documents.set('doc-draft', { ...documents.get('doc-draft'), name: 'Document library latest', revision: 3 }); changeSession('doc-draft');
  await page.goto(`${base}/ai-document`);
  const before = writes.length;
  await page.getByRole('button', { name: '保存到文档库', exact: true }).filter({ visible: true }).click();
  await page.getByText('已保存到文档库', { exact: true }).waitFor();
  assert.equal(writes.length, before, 'Document card does not overwrite the saved document on repeated save');
  await page.getByRole('button', { name: '在文档编辑器中打开', exact: true }).filter({ visible: true }).click();
  await editor.waitFor();
  assert.equal(await editor.getByRole('textbox', { name: '文档名称', exact: true }).inputValue(), 'Document library latest');
  await saveEditor('doc-draft', 4);
  assert.equal(writes.at(-1).revision, 3);
  assert.equal(documents.size, 2, 'Stable card IDs remain stable across editor saves and reloads');
  assert.deepEqual(errors, []);
  if (process.env.INBRIX_ROUNDTRIP_RECORDS) await writeFile(process.env.INBRIX_ROUNDTRIP_RECORDS, JSON.stringify([...documents.values()]));
  console.log(`PASS ${mobile ? 'mobile' : 'desktop'}: durable origins, editor first save, refreshed cards, latest library content, no duplicate or stale overwrite.`);
} finally { await browser.close(); }
