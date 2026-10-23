import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createAIChatTestStore } from './ai-chat-test-store.mjs';

const browser = await chromium.launch({ headless: true });
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const base = process.env.COMPOSE_TEST_URL || 'http://127.0.0.1:3097';
const email = 'sales@example.com', now = new Date().toISOString();
const store = createAIChatTestStore(email);
const message = (id, content) => ({ id, role: 'assistant', content, createdAt: now });
store.mail.messages = Array.from({ length: 105 }, (_, i) => message(`mail-${i}`, `Mail history ${i}`));
for (let i = 0; i < 35; i++) store.sessions.set(`doc-${i}`, {
  id: `doc-${i}`, kind: 'document', title: `History chat ${i}`, documentType: 'quotation', version: 0,
  businessContext: { agentId: '', skillId: '', brands: [] }, createdAt: now, updatedAt: now,
  messages: i === 0 ? Array.from({ length: 105 }, (_, j) => message(`doc-message-${j}`, `Document history ${j}`)) : [message(`selected-${i}`, `Selected chat ${i}`)],
});
const conversation = { id: 'paging', title: 'Buyer', peerEmail: 'buyer@example.com', subject: 'Paging', date: now, count: 1, unreadCount: 0, accountEmail: email, status: 'unanswered' };
const errors = [], requests = [];
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname;
  requests.push(url);
  const sessionRoute = store.routeSessions(req, url);
  if (sessionRoute) { await route.fulfill(sessionRoute); return; }
  let json;
  if (path === '/api/accounts') json = { accounts: [{ id: 'account', email, label: email, isActive: true }] };
  else if (path === '/api/accounts/account/brands') json = { brands: [], brandMode: 'prefer' };
  else if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail: email, conversations: [conversation], folders: [], accounts: [{ id: 'account', email, label: email, isActive: true }] };
  else if (path.endsWith('/ai-chat')) json = { ok: true, ...store.projectSession(store.mail, url), sessionId: store.mail.id };
  else if (path.startsWith('/api/conversations/')) json = { conversation: { ...conversation, messages: [{ id: '1', from: conversation.peerEmail, to: email, subject: 'Paging', body: 'Inquiry', date: now, outgoing: false, flags: ['\\Seen'] }] } };
  else json = { calendar: false, notifications: false, models: [], agents: [], skills: [], bindings: [], documents: [], signatures: [], attachments: [], total: 0, stamps: [], templates: [], brands: [] };
  await route.fulfill({ json });
});
try {
  await page.goto(`${base}/ai-document`);
  await page.getByText('Document history 104', { exact: true }).waitFor();
  assert.equal(await page.getByText('Document history 0', { exact: true }).count(), 0);
  await page.getByRole('button', { name: '加载更早消息', exact: true }).filter({ visible: true }).click();
  await page.getByText('Document history 0', { exact: true }).waitFor();
  assert.equal(await page.getByText('Document history 104', { exact: true }).count(), 1);
  await page.getByRole('button', { name: '历史对话', exact: true }).click();
  assert.equal(await page.getByRole('menuitem').filter({ hasText: 'History chat 34' }).count(), 0);
  await page.getByRole('menuitem', { name: '加载更多会话', exact: true }).click();
  await page.getByRole('menuitem').filter({ hasText: 'History chat 34' }).click();
  await page.getByText('Selected chat 34', { exact: true }).waitFor();
  assert.equal(await page.getByText('Document history 104', { exact: true }).count(), 0);
  await page.reload();
  await page.getByText('Document history 104', { exact: true }).waitFor();

  await page.goto(`${base}/inbox?conversation=paging`);
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await page.getByText('Mail history 104', { exact: true }).filter({ visible: true }).waitFor();
  assert.equal(await page.getByText('Mail history 0', { exact: true }).filter({ visible: true }).count(), 0);
  await page.getByRole('button', { name: '加载更早消息', exact: true }).filter({ visible: true }).click();
  await page.getByText('Mail history 0', { exact: true }).filter({ visible: true }).waitFor();
  assert.equal(await page.getByText('Mail history 104', { exact: true }).filter({ visible: true }).count(), 1);
  assert.ok(requests.some(url => url.searchParams.get('cursor') === '30'), 'Session list requests another summary page');
  assert.ok(requests.some(url => url.pathname === '/api/ai/chat-sessions/doc-0' && url.searchParams.get('before') === '5'), 'Document history uses message cursor');
  assert.ok(requests.some(url => url.pathname.endsWith('/ai-chat') && url.searchParams.get('before') === '5'), 'Mail history uses message cursor');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  assert.deepEqual(errors, []);
  console.log(`Passed (${mobile ? 'mobile' : 'desktop'}): 35 session summaries, message pagination in both chats, session selection and no duplicate messages.`);
} finally { await browser.close(); }
