import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// All APIs are intercepted. Exercise actual pointer hit areas and ensure the
// row's status, note editor and context menu keep their own interactions.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const accountEmail = 'me@example.com';
const date = new Date().toISOString();
const conversations = ['alice', 'bob', 'carol', 'doris'].map(id => ({
  id, title: id, peerEmail: `${id}@example.com`, accountEmail,
  subject: `Subject ${id}`, preview: `Preview ${id}`, note: `Note ${id}`,
  date, count: 1, unreadCount: 0, status: id === 'carol' ? 'answered' : id === 'doris' ? 'draft' : 'unanswered', hasAttachments: false,
}));
const errors = [];
const notes = [];
const statuses = [];
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const request = route.request();
  const path = new URL(request.url()).pathname;
  let json = { calendar: false, notifications: false, models: [], agents: [], bindings: [] };
  if (path === '/api/conversations') {
    json = { locale: 'zh-CN', accountEmail, conversations, folders: [], accounts: [] };
  } else if (path === '/api/mail/search') {
    json = {
      query: 'Subject', terms: ['Subject'], hasMore: false,
      results: conversations.map(item => ({
        conversationId: item.id, messageId: '1', folder: 'INBOX',
        subject: item.subject, from: item.peerEmail, to: accountEmail,
        date, snippet: item.preview, conversationTitle: item.title,
        outgoing: false, matchedSubject: true, matchedBody: false,
      })),
    };
  } else if (path.endsWith('/note')) {
    const note = request.postDataJSON().note;
    notes.push(note);
    conversations.find(item => item.id === path.split('/')[3]).note = note;
    json = { ok: true, note };
  } else if (path.endsWith('/status')) {
    const status = request.postDataJSON().status;
    statuses.push(status);
    conversations.find(item => item.id === path.split('/')[3]).status = status;
    json = { ok: true, status };
  } else if (path.startsWith('/api/conversations/')) {
    const summary = conversations.find(item => item.id === path.split('/')[3]);
    json = { conversation: { ...summary, messages: [{ id: '1', from: summary.peerEmail, to: accountEmail, date, subject: summary.subject, body: summary.preview, preview: summary.preview, outgoing: false, flags: ['\\Seen'] }] } };
  } else if (path === '/api/drafts') json = { drafts: [] };
  await route.fulfill({ json });
});
const base = process.env.CONVERSATION_TEST_URL || 'http://localhost:2342';
const row = id => page.getByTestId('conversation-row').filter({ hasText: `${id}@example.com` });
const selected = async id => {
  await page.waitForFunction(expected => new URL(location.href).searchParams.get('conversation') === expected, id, { timeout: 2000 });
};
const selectAlice = async () => {
  await row('alice').getByRole('button').first().click();
  await selected('alice');
};
try {
  await page.goto(`${base}/inbox?conversation=alice`);
  await row('bob').waitFor();
  const filter = page.getByRole('switch', { name: '全部', exact: true });
  assert.equal(await filter.isChecked(), false);
  assert.equal(await row('carol').count(), 0, 'Answered conversations are hidden by default');
  await row('doris').waitFor();
  await filter.click();
  // The 6px margin between preview and note was outside either button.
  const gap = await row('bob').evaluate(element => {
    const summary = element.querySelector('button').getBoundingClientRect();
    return { x: summary.x + summary.width / 2, y: summary.bottom + 3 };
  });
  await page.mouse.click(gap.x, gap.y);
  await selected('bob');
  console.log('Preview/note gap selects the conversation');
  // The row padding should select too. Stay inside the adjacent splitters'
  // intentional 6px drag targets at the left and right boundaries.
  for (const edge of ['top', 'left', 'right', 'bottom']) {
    await selectAlice();
    const box = await row('bob').boundingBox();
    const x = edge === 'left' ? box.x + 10 : edge === 'right' ? box.x + box.width - 10 : box.x + box.width / 2;
    const y = edge === 'top' ? box.y + 3 : edge === 'bottom' ? box.y + box.height - 3 : box.y + box.height / 2;
    await page.mouse.click(x, y);
    try {
      await selected('bob');
    } catch (error) {
      console.error('Unresponsive row edge:', edge, await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.outerHTML, { x, y }));
      throw error;
    }
  }
  await selectAlice();
  await row('bob').getByRole('button', { name: 'Note bob', exact: true }).click();
  await selected('bob');
  await selectAlice();
  const bob = row('bob');
  await bob.hover();
  await bob.getByRole('button', { name: '添加备注', exact: true }).click();
  const input = bob.getByRole('textbox', { name: '添加备注', exact: true });
  await input.fill('Updated note');
  await input.click();
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'alice', 'Editing a note must not navigate');
  await input.press('Enter');
  await bob.getByRole('button', { name: 'Updated note', exact: true }).waitFor();
  assert.deepEqual(notes, ['Updated note']);
  await bob.getByRole('button', { name: '待回复', exact: true }).click();
  await page.getByRole('menuitem', { name: '无需处理', exact: true }).click();
  await bob.getByRole('button', { name: '无需处理', exact: true }).waitFor();
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'alice', 'Changing status must not navigate');
  assert.deepEqual(statuses, ['no_action']);
  await bob.click({ button: 'right' });
  await page.getByRole('menuitem', { name: '标为未读', exact: true }).waitFor();
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'alice', 'Opening a context menu must not navigate');
  await page.keyboard.press('Escape');
  await bob.getByRole('button').first().focus();
  await page.keyboard.press('Enter');
  await selected('bob');
  const noteEntry = page.getByTestId('conversation-note-entry');
  const noteInput = noteEntry.getByRole('textbox', { name: '对话备注', exact: true });
  const noteValue = async value => page.waitForFunction(expected => document.querySelector('[data-testid="conversation-note-entry"] textarea')?.value === expected, value);
  await noteValue('Updated note');
  assert.equal(await noteEntry.getByRole('button').count(), 0, 'The note has no edit, save or cancel buttons');
  assert.equal(await noteInput.getAttribute('placeholder'), '对话备注');
  await noteInput.fill('Note edited in transcript\nSecond line');
  await bob.getByRole('button').first().focus();
  await bob.getByRole('button', { name: 'Note edited in transcript Second line', exact: true }).waitFor();
  assert.deepEqual(notes, ['Updated note', 'Note edited in transcript\nSecond line']);
  // Editing from the list updates the currently open transcript immediately.
  await bob.hover();
  await bob.getByRole('button', { name: '添加备注', exact: true }).click();
  await bob.getByRole('textbox', { name: '添加备注', exact: true }).fill('Synced from list');
  await bob.getByRole('textbox', { name: '添加备注', exact: true }).press('Enter');
  await noteValue('Synced from list');
  await page.reload();
  await noteValue('Synced from list');
  await filter.click();
  await noteInput.fill('Saved when leaving Bob');
  await selectAlice();
  await noteValue('Note alice');
  await bob.getByRole('button', { name: 'Saved when leaving Bob', exact: true }).waitFor();
  await bob.getByRole('button').first().click();
  await selected('bob');
  await noteValue('Saved when leaving Bob');
  const clearedNoteSaved = page.waitForResponse(response => response.url().endsWith('/api/conversations/bob/note') && response.request().postDataJSON()?.note === '');
  await noteInput.fill('');
  await bob.getByRole('button').first().focus();
  await clearedNoteSaved;
  await page.reload();
  await noteValue('');

  const rowIds = () => page.getByTestId('conversation-row').evaluateAll(elements => elements.map(element => element.textContent.match(/(\w+)@example\.com/)[1]));
  // Reload defaults to unanswered and drafts. Bob is now marked no_action.
  assert.equal(await filter.isChecked(), false);
  assert.deepEqual(await rowIds(), ['alice', 'doris']);
  await filter.click();
  assert.equal(await filter.isChecked(), true);
  assert.deepEqual(await rowIds(), ['alice', 'bob', 'carol', 'doris']);
  await filter.click();
  assert.equal(await filter.isChecked(), false);
  assert.deepEqual(await rowIds(), ['alice', 'doris'], 'Drafts stay visible alongside unanswered conversations');
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'bob', 'Filtering keeps the current conversation open');
  // Status updates immediately remove completed items from the filtered list.
  await row('alice').getByRole('button', { name: '待回复', exact: true }).click();
  await page.getByRole('menuitem', { name: '已发送', exact: true }).click();
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="conversation-row"]').length === 1);
  assert.deepEqual(await rowIds(), ['doris']);
  await page.getByTestId('mail-search').fill('Subject');
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="mail-search-result"]').length === 4);
  assert.equal(await filter.isChecked(), false, 'Searching all statuses preserves the switch state');
  assert.deepEqual(await page.getByTestId('mail-search-result').evaluateAll(elements => elements.map(element => element.textContent.match(/(\w+)@example\.com/)[1])), ['alice', 'bob', 'carol', 'doris']);
  await filter.focus();
  await page.keyboard.press('Space');
  assert.equal(await page.getByTestId('mail-search-result').count(), 4);
  await page.keyboard.press('Space');
  assert.equal(await page.getByTestId('mail-search-result').count(), 4, 'Switch changes do not filter search results');
  await page.getByTestId('mail-search').fill('');
  await row('doris').waitFor();
  assert.deepEqual(await rowIds(), ['doris'], 'Clearing search restores the existing list filter');
  await filter.click();
  await row('bob').waitFor();
  assert.deepEqual(await rowIds(), ['alice', 'bob', 'carol', 'doris']);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${base}/inbox`);
  await row('doris').waitFor();
  const filterBox = await filter.boundingBox();
  const searchBox = await page.getByTestId('mail-search').boundingBox();
  assert.ok(filterBox && searchBox && filterBox.x >= searchBox.x + searchBox.width && filterBox.x + filterBox.width <= 390, 'The switch fits to the right of search on mobile');
  assert.deepEqual(await rowIds(), ['doris']);
  assert.equal(await filter.isChecked(), false);
  await filter.click();
  assert.deepEqual(await rowIds(), ['alice', 'bob', 'carol', 'doris']);
  assert.deepEqual(errors, []);
  console.log('Passed: row hit areas, note autosave, sync, menus, unanswered filter, retained drafts, live status updates, search across all statuses, restored list filter, keyboard and mobile.');
} finally {
  await browser.close();
}
