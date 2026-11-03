import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Server-filtered pagination is essential here: completed conversations are
// absent from a refresh, so stale cached rows must never be merged back in.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const accountEmail = 'me@example.com';
let revision = 1;
let draft;
let sent = '';
const conversations = Array.from({ length: 135 }, (_, index) => ({
  id: `thread-${index}`, title: `Person ${index}`, peerEmail: `person${index}@example.com`,
  subject: `Subject ${index}`, preview: `Message ${index}`, note: '', accountEmail,
  date: new Date(Date.now() - index * 60_000).toISOString(), count: 1, unreadCount: 0,
  status: index === 134 ? 'draft' : 'unanswered', hasAttachments: false,
}));
const errors = [];
const listRequests = [];
page.on('pageerror', error => errors.push(error.stack));
await page.addInitScript(() => {
  window.EventSource = class {
    constructor() { window.__mailSource = this; }
    close() {}
  };
  // Keep pagination explicit so the test controls the loaded window size.
  window.IntersectionObserver = class {
    observe() {}
    disconnect() {}
  };
});
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const request = route.request();
  const url = new URL(request.url());
  let json = { models: [], agents: [], bindings: [] };
  if (url.pathname === '/api/conversations') {
    listRequests.push(Object.fromEntries(url.searchParams));
    if (!url.searchParams.has('cursor') && Number(url.searchParams.get('rev')) === revision) {
      json = { unchanged: true, revision };
    } else {
      const all = url.searchParams.get('filter') === 'all'
        ? conversations
        : conversations.filter(item => item.status === 'unanswered' || item.status === 'draft');
      const cursor = url.searchParams.get('cursor');
      const start = cursor ? all.findIndex(item => item.id === cursor) + 1 : 0;
      const rows = all.slice(start, start + Number(url.searchParams.get('limit')));
      const hasMore = start + rows.length < all.length;
      json = { locale: 'zh-CN', accountEmail, accounts: [{ id: 'account', email: accountEmail, label: accountEmail, isActive: true }], folders: [], revision,
        conversations: rows, total: all.length, unreadCount: 0, hasMore,
        nextCursor: hasMore ? rows.at(-1).id : undefined };
    }
  } else if (url.pathname.endsWith('/status')) {
    const item = conversations.find(item => item.id === url.pathname.split('/')[3]);
    item.status = request.postDataJSON().status;
    revision++;
    conversations[130].preview = `Refreshed revision ${revision}`;
    json = { ok: true, status: item.status };
  } else if (/^\/api\/conversations\/[^/]+$/.test(url.pathname)) {
    const item = conversations.find(item => item.id === url.pathname.split('/')[3]);
    json = { conversation: { ...item, messages: [{ id: '1', from: item.peerEmail, to: accountEmail,
      date: item.date, subject: item.subject, body: item.preview, preview: item.preview,
      messageId: `<${item.id}@example.com>`, outgoing: false, flags: ['\\Seen'] }] } };
  } else if (url.pathname === '/api/drafts' && request.method() === 'POST') {
    draft = { ...request.postDataJSON(), id: 'reply-draft', updatedAt: new Date().toISOString() };
    json = { draft };
  } else if (url.pathname === '/api/drafts') json = { drafts: draft ? [draft] : [] };
  else if (url.pathname.startsWith('/api/drafts/') && request.method() === 'DELETE') {
    draft = undefined;
    json = { ok: true };
  } else if (url.pathname === '/api/compose') {
    sent = request.postDataBuffer().toString();
    conversations[0].status = 'answered';
    draft = undefined;
    revision++;
    json = { success: true };
  }
  await route.fulfill({ json });
});
const rows = page.getByTestId('conversation-row');
const row = index => rows.filter({ hasText: `person${index}@example.com` });
const waitCount = count => page.waitForFunction(expected => document.querySelectorAll('[data-testid="conversation-row"]').length === expected, count);
const emit = type => page.evaluate(type => window.__mailSource.onmessage({ data: JSON.stringify({ type }) }), type);
try {
  await page.goto(`${process.env.CONVERSATION_TEST_URL || 'http://localhost:2342'}/inbox?conversation=thread-0`);
  await waitCount(30);
  await page.getByTestId('conversation-detail').getByRole('button', { name: '回复', exact: true }).click();
  const compose = page.getByTestId('compose-inline');
  await compose.locator('.tiptap').fill('Reply to this request');
  await compose.getByRole('button', { name: '发送', exact: true }).click();
  await compose.waitFor({ state: 'hidden' });
  await row(0).waitFor({ state: 'hidden' });
  assert.ok(sent.includes('name="in_reply_to"\r\n\r\n<thread-0@example.com>'));
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'thread-0', 'Sending keeps the transcript open');
  await waitCount(30);
  for (const count of [60, 90, 120, 134]) {
    await page.getByRole('button', { name: '加载更多', exact: true }).click();
    await waitCount(count);
  }
  // A sent reply and a status change in another tab disappear from both the
  // first page and the previously loaded tail. Drafts continue to be visible.
  conversations[1].status = 'answered';
  conversations[110].status = 'no_action';
  revision++;
  await emit('mail.changed');
  await waitCount(132);
  assert.equal(await row(0).count(), 0);
  assert.equal(await row(1).count(), 0);
  assert.equal(await row(110).count(), 0);
  await row(134).waitFor();
  assert.equal(await page.getByRole('button', { name: '加载更多', exact: true }).count(), 0);
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'thread-0');
  assert.ok(listRequests.some(request => request.limit === '100' && !request.cursor), 'Refresh batches the loaded window up to the server page limit');
  assert.ok(listRequests.some(request => request.cursor && Number(request.limit) < 100), 'Refresh revalidates the remaining tail');

  // A local status edit also refreshes the authoritative filtered list.
  await row(5).getByRole('button', { name: '待回复', exact: true }).click();
  const previousRevision = revision;
  const refreshed = page.waitForResponse(response => new URL(response.url()).pathname === '/api/conversations' && new URL(response.url()).searchParams.get('rev') === String(previousRevision));
  await page.getByRole('menuitem', { name: '无需处理', exact: true }).click();
  await row(5).waitFor({ state: 'hidden' });
  await refreshed;
  await row(130).getByText(`Refreshed revision ${previousRevision + 1}`, { exact: true }).waitFor();
  await waitCount(131);

  // Same revision still uses one compact request and preserves every tail row.
  const before = listRequests.length;
  const unchanged = page.waitForResponse(response => new URL(response.url()).pathname === '/api/conversations');
  await emit('resync');
  assert.equal((await (await unchanged).json()).unchanged, true);
  assert.equal(listRequests.length, before + 1);
  assert.equal(await rows.count(), 131);

  const filter = page.getByTestId('conversation-filter');
  await filter.click();
  await page.getByRole('option', { name: '全部', exact: true }).click();
  await row(0).getByRole('button', { name: '已发送', exact: true }).waitFor();
  await filter.click();
  await page.getByRole('option', { name: '待处理', exact: true }).click();
  await row(134).waitFor();
  assert.equal(await row(0).count(), 0);
  assert.equal(await row(110).count(), 0);
  assert.equal(await row(5).count(), 0);
  assert.deepEqual(errors, []);
  console.log('Passed: filtered refresh after sending/status changes, revalidated tail beyond 100 rows, retained drafts and transcript, local status refresh, unchanged revision and filter switching.');
} finally {
  await browser.close();
}
