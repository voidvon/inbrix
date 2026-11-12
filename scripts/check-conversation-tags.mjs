import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Only the last page has tagged conversations. The tag filter must reach it
// without relying on client-side filtering of the initially loaded rows.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const accountEmail = 'me@example.com';
const conversations = Array.from({ length: 65 }, (_, index) => ({
  id: `thread-${index}`, title: `Person ${index}`, peerEmail: `person${index}@example.com`, accountEmail,
  subject: `Subject ${index}`, preview: `Message ${index}`, note: '',
  date: new Date(Date.now() - index * 60_000).toISOString(), count: 1, unreadCount: index === 60 ? 3 : 0,
  status: index === 61 ? 'answered' : 'unanswered', hasAttachments: false,
  tags: index === 60 ? ['后续页', '重点'] : index === 61 ? ['后续页'] : [],
}));
let revision = 1;
let failNextSave = false;
const errors = [];
const requests = [];
page.on('pageerror', error => errors.push(error.stack));
await page.addInitScript(() => {
  window.EventSource = class { close() {} };
  window.IntersectionObserver = class { observe() {} disconnect() {} };
});
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const request = route.request();
  const url = new URL(request.url());
  const path = url.pathname;
  let json = { models: [], agents: [], bindings: [] };
  if (path === '/api/conversations') {
    requests.push(Object.fromEntries(url.searchParams));
    if (!url.searchParams.has('cursor') && Number(url.searchParams.get('rev')) === revision) {
      json = { unchanged: true, revision };
    } else {
      const all = conversations.filter(item =>
        (url.searchParams.get('filter') === 'all' || item.status === 'unanswered' || item.status === 'draft') &&
        (!url.searchParams.get('tag') || item.tags.includes(url.searchParams.get('tag'))));
      const start = url.searchParams.has('cursor') ? all.findIndex(item => item.id === url.searchParams.get('cursor')) + 1 : 0;
      const rows = all.slice(start, start + Number(url.searchParams.get('limit')));
      const hasMore = start + rows.length < all.length;
      json = { locale: 'zh-CN', accountEmail, accounts: [], folders: [{ name: 'Archive', attributes: [], unreadCount: 0 }], revision,
        tags: [...new Set(conversations.flatMap(item => item.tags))].sort(),
        conversations: rows, total: all.length, unreadCount: 0, hasMore,
        nextCursor: hasMore ? rows.at(-1).id : undefined };
    }
  } else if (path === '/api/mail/messages') json = { messages: [], syncComplete: true };
  else if (path.endsWith('/tags')) {
    if (failNextSave) {
      failNextSave = false;
      return route.fulfill({ status: 500, json: { error: '标签保存失败，请重试' } });
    }
    const tags = request.postDataJSON().tags.sort();
    conversations.find(item => item.id === path.split('/')[3]).tags = tags;
    revision++;
    json = { ok: true, tags };
  } else if (/^\/api\/conversations\/[^/]+$/.test(path)) {
    const item = conversations.find(item => item.id === path.split('/')[3]);
    json = { conversation: { ...item, messages: [{ id: '1', from: item.peerEmail, to: accountEmail,
      date: item.date, subject: item.subject, body: item.preview, preview: item.preview,
      outgoing: false, flags: ['\\Seen'] }] } };
  } else if (path === '/api/drafts') json = { drafts: [] };
  await route.fulfill({ json });
});
const rows = page.getByTestId('conversation-row');
const row = index => rows.filter({ hasText: `person${index}@example.com` });
const sidebarTags = page.getByTestId('sidebar-conversation-tags');
const openSidebar = async () => {
  if (page.viewportSize().width < 1024) await page.getByTestId('conversation-list').getByRole('button', { name: '文件夹', exact: true }).click();
};
const setTagFilter = async name => {
  await openSidebar();
  if (name === '全部') await page.getByTestId('conversation-filter-all').click();
  else await sidebarTags.getByRole('button', { name, exact: true }).click();
};
const setScope = async name => {
  await openSidebar();
  await page.getByTestId(name === '全部' ? 'conversation-filter-all' : 'conversation-filter-open').click();
};
const editor = page.locator('[data-slot="popover-content"][data-open]');
const nameInput = editor.getByRole('textbox', { name: '新标签名称', exact: true });
const editTags = page.getByTestId('edit-conversation-tags');
const selected = id => page.waitForFunction(expected => new URL(location.href).searchParams.get('conversation') === expected, `thread-${id}`);
const base = process.env.CONVERSATION_TEST_URL || 'http://localhost:2342';

try {
  await page.goto(`${base}/inbox?conversation=thread-0`);
  await row(0).waitFor();
  assert.equal(await rows.count(), 30);
  assert.equal(await page.getByTestId('conversation-filter-open').getAttribute('aria-current'), 'page');
  assert.equal(await page.getByTestId('conversation-list').getByRole('combobox').count(), 0, 'Filters are absent from the list toolbar');
  assert.equal(await sidebarTags.getByRole('button').count(), 2, 'Tags beyond the first page appear in the sidebar');
  assert.equal(await row(0).getByTestId('conversation-tags').count(), 0, 'Untagged rows have no tag badge or editor');
  const editBounds = await editTags.boundingBox();
  const aiBounds = await page.getByRole('button', { name: 'AI对话', exact: true }).boundingBox();
  assert.ok(editBounds.x + editBounds.width <= aiBounds.x, 'The tag editor sits to the left of AI Chat');
  await setTagFilter('后续页');
  await row(60).waitFor();
  await row(61).waitFor();
  assert.equal(await rows.count(), 2, 'A tag opens all matching statuses, including later-page conversations');
  assert.ok(requests.some(request => request.tag === '后续页' && request.filter === 'all'));
  assert.equal(await sidebarTags.getByRole('button', { name: '后续页', exact: true }).getAttribute('aria-current'), 'page');
  assert.equal(await page.getByTestId('sidebar-conversation-filters').locator('[aria-current="page"]').count(), 0);
  await setScope('待回复');
  await row(0).waitFor();
  assert.equal(await rows.count(), 30, 'Pending clears the selected tag');
  await setScope('全部');
  await row(0).waitFor();
  assert.equal(await rows.count(), 30, 'All clears the selected tag');
  await setTagFilter('后续页');
  await row(61).waitFor();

  assert.equal(await row(61).getByTestId('conversation-tags').evaluate(element => element.tagName), 'SPAN', 'List tags are display-only');
  await row(61).getByTestId('conversation-tags').click();
  await selected(61);
  assert.equal(await editor.count(), 0, 'Clicking a displayed tag opens the conversation');
  await editTags.click();
  await nameInput.fill('需报价');
  await nameInput.press('Enter');
  await row(61).getByTestId('conversation-tags').filter({ hasText: '+1' }).waitFor();
  assert.ok(conversations[61].tags.includes('需报价'));
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'thread-61', 'Creating tags keeps the current conversation');
  await page.keyboard.press('Escape');
  await row(60).locator('strong').click();
  await selected(60);
  await editTags.click();
  await editor.getByRole('button', { name: '需报价', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('[data-testid="conversation-row"] [data-testid="conversation-tags"]')?.textContent.includes('+2'));
  assert.ok(conversations[60].tags.includes('需报价'));

  // Failure leaves the current tags intact and keeps the name ready to retry.
  failNextSave = true;
  await nameInput.fill('失败后重试');
  await nameInput.press('Enter');
  await page.getByText('标签保存失败，请重试', { exact: true }).waitFor();
  assert.equal(await nameInput.inputValue(), '失败后重试');
  assert.ok(!conversations[60].tags.includes('失败后重试'));
  await nameInput.press('Enter');
  await row(60).getByTestId('conversation-tags').filter({ hasText: '+3' }).waitFor();
  assert.ok(conversations[60].tags.includes('失败后重试'));
  await editor.getByRole('button', { name: '后续页', exact: true }).click();
  await row(60).waitFor({ state: 'hidden' });
  assert.equal(await rows.count(), 1, 'Removing the active tag immediately removes that row');
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'thread-60');
  await page.keyboard.press('Escape');
  await setTagFilter('全部');
  await row(0).waitFor();
  assert.equal(await rows.count(), 30, 'All tags restores normal pagination');

  await page.reload();
  await row(0).waitFor();
  await setTagFilter('需报价');
  await row(60).waitFor();
  await row(61).waitFor();
  assert.equal(await rows.count(), 2, 'Saved tags survive reload and open all matching statuses');
  await editTags.click();
  const selectedTag = editor.getByRole('button', { name: '需报价', exact: true });
  assert.equal(await selectedTag.getAttribute('aria-pressed'), 'true');
  await page.keyboard.press('Escape');
  await editor.waitFor({ state: 'hidden' });
  await page.screenshot({ path: '/tmp/inbrix-conversation-tags-desktop.png' });
  await page.locator('aside a[href="/folder/Archive"]').click();
  await page.waitForURL('**/folder/Archive');
  assert.equal(await sidebarTags.locator('[aria-current="page"]').count(), 0, 'A folder does not highlight a conversation filter');
  await setTagFilter('需报价');
  await page.waitForURL('**/inbox');
  await row(61).waitFor();
  assert.equal(await rows.count(), 2, 'Sidebar tags navigate from another view and select all matching statuses');

  await page.setViewportSize({ width: 320, height: 740 });
  await page.goto(`${base}/inbox`);
  await row(0).waitFor();
  await setTagFilter('失败后重试');
  await row(60).waitFor();
  const bounds = await page.getByTestId('conversation-list').boundingBox();
  assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= 320, 'The list fits a narrow mobile screen with a selected tag');
  await page.waitForFunction(() => document.querySelector('aside').getBoundingClientRect().right <= 0);
  const titleBounds = await row(60).locator('strong').boundingBox();
  const timeBounds = await row(60).locator('time').boundingBox();
  const unreadBounds = await row(60).locator('[data-slot="badge"]').boundingBox();
  const previewBounds = await row(60).locator('p').first().boundingBox();
  const tagsBounds = await row(60).getByTestId('conversation-tags').boundingBox();
  const centerY = bounds => bounds.y + bounds.height / 2;
  assert.ok(Math.abs(centerY(titleBounds) - centerY(timeBounds)) < 1 && Math.abs(centerY(titleBounds) - centerY(unreadBounds)) < 1, 'Email, time and unread count share the first line');
  assert.ok(tagsBounds.y >= titleBounds.y + titleBounds.height && Math.abs(centerY(previewBounds) - centerY(tagsBounds)) < 1 && tagsBounds.x >= previewBounds.x + previewBounds.width, 'Preview and tags share the second line');
  assert.equal(await row(60).evaluate(element => element.scrollWidth <= element.clientWidth), true);
  await page.screenshot({ path: '/tmp/inbrix-conversation-tags-mobile.png' });
  await openSidebar();
  await sidebarTags.getByRole('button', { name: '失败后重试', exact: true }).waitFor();
  await page.waitForFunction(() => document.querySelector('aside').getBoundingClientRect().left >= -1);
  await page.screenshot({ path: '/tmp/inbrix-conversation-sidebar-mobile.png' });
  await sidebarTags.getByRole('button', { name: '失败后重试', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('aside').getBoundingClientRect().right <= 0);
  await row(60).getByTestId('conversation-tags').click();
  await selected(60);
  await editTags.click();
  const mobileEditBounds = await editTags.boundingBox();
  const mobileAIBounds = await page.getByRole('button', { name: 'AI对话', exact: true }).boundingBox();
  assert.ok(mobileEditBounds.x + mobileEditBounds.width <= mobileAIBounds.x && mobileAIBounds.x + mobileAIBounds.width <= 320, 'The detail toolbar fits on mobile');
  // Removing the last tag hides it in the list while the detail editor remains.
  for (const tag of [...conversations[60].tags]) {
    const option = editor.getByRole('button', { name: tag, exact: true });
    const saved = page.waitForResponse(response => response.url().endsWith('/thread-60/tags') && response.ok());
    await option.click();
    await saved;
    await page.waitForFunction(name => [...document.querySelectorAll('[data-slot="popover-content"][data-open] button')].some(element => element.textContent.trim() === name && element.getAttribute('aria-pressed') === 'false'), tag);
  }
  await page.keyboard.press('Escape');
  assert.equal(await editTags.isVisible(), true, 'The editor remains available without tags');
  await page.getByRole('button', { name: '返回列表', exact: true }).click();
  await setTagFilter('全部');
  await row(0).waitFor();
  assert.equal(await row(0).getByTestId('conversation-tags').count(), 0);
  assert.deepEqual(errors, []);
  console.log('Passed: flat sidebar filters, exclusive status/tag views, all-status tag results beyond the first page, detail tag editing, failure/retry, retained conversation, persistence, mobile drawer and narrow layout.');
} catch (error) {
  await page.screenshot({ path: '/tmp/inbrix-conversation-tags-failure.png' });
  throw error;
} finally {
  await browser.close();
}
