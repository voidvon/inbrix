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
  subject: `Subject ${id}`, preview: `Preview ${id}`, note: id === 'alice' ? '重点客户 · Note alice' : `Note ${id}`,
  date, count: 1, unreadCount: 0, status: id === 'carol' ? 'answered' : id === 'doris' ? 'draft' : 'unanswered',
  hasAttachments: id === 'alice' || id === 'bob', latestHasAttachments: id === 'alice',
}));
conversations[0].peerEmail = 'alice@example.com, logistics@example.com';
conversations[0].primaryPeerEmail = 'alice@example.com';
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
    const query = new URL(request.url()).searchParams.get('q');
    const conversationLookup = ['alice@example.com', '重点客户', '北方客户'].includes(query);
    json = {
      query, terms: [query], hasMore: false,
      results: conversationLookup ? (query === 'alice@example.com' || conversations[0].note.includes(query) ? [{
        kind: 'conversation', conversationId: 'alice', conversation: conversations[0],
        messageId: '', folder: '', matchedNote: query !== 'alice@example.com',
        conversationTitle: conversations[0].title,
      }] : []) : conversations.map(item => ({
        kind: 'message',
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
  await row('alice').locator('strong').click();
  await selected('alice');
};
try {
  await page.goto(`${base}/inbox?conversation=alice`);
  await row('bob').waitFor();
  await row('alice').getByRole('img', { name: '最新邮件含有附件' }).waitFor();
  assert.equal(await row('bob').getByRole('img', { name: '最新邮件含有附件' }).count(), 0, 'Older attachments do not mark the latest email');
  assert.equal(await row('alice').getByRole('img', { name: '最新邮件含有附件' }).evaluate(element => element.parentElement.querySelector('p')?.textContent.trim()), 'Preview alice', 'Attachment indicator shares the second line with the preview');
  assert.equal(await row('alice').locator('strong').innerText(), 'alice@example.com', 'The row must show the sender without other To recipients');
  assert.equal(await row('alice').locator('strong').evaluate(element => element.closest('button') === null), true, 'The title is plain content inside the clickable item');
  assert.equal(await row('alice').locator('p').first().evaluate(element => element.closest('button') === null), true, 'The preview is plain content inside the clickable item');
  assert.equal(await row('alice').getByTestId('conversation-tags').count(), 0, 'Untagged conversations show no tag control in the list');
  const filter = page.getByTestId('conversation-filter-open');
  const setFilter = async value => {
    if (page.viewportSize().width < 1024) await page.getByTestId('conversation-list').getByRole('button', { name: '文件夹', exact: true }).click();
    await page.getByTestId(value ? 'conversation-filter-all' : 'conversation-filter-open').click();
  };
  assert.equal(await filter.getAttribute('aria-current'), 'page');
  assert.equal(await row('carol').count(), 0, 'Answered conversations are hidden by default');
  await row('doris').waitFor();
  await setFilter(true);
  // The space between the summary and note belongs to the whole item.
  const gap = await row('bob').evaluate(element => {
    const summary = element.querySelector('[data-testid="conversation-summary"]').getBoundingClientRect();
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
  await row('bob').getByText('Note bob', { exact: true }).click();
  await selected('bob');
  await selectAlice();
  const bob = row('bob');
  const statusBox = await bob.getByTestId('conversation-status').boundingBox();
  const noteBox = await bob.getByText('Note bob', { exact: true }).boundingBox();
  assert.ok(Math.abs((statusBox.y + statusBox.height / 2) - (noteBox.y + noteBox.height / 2)) < 1, 'Status and note text are vertically centered on the third line');
  await bob.hover();
  const editBox = await bob.getByRole('button', { name: '添加备注', exact: true }).boundingBox();
  assert.ok(Math.abs((statusBox.y + statusBox.height / 2) - (editBox.y + editBox.height / 2)) < 1, 'The note edit icon shares the third-line center');
  await bob.getByRole('button', { name: '添加备注', exact: true }).click();
  const input = bob.getByRole('textbox', { name: '添加备注', exact: true });
  await input.fill('Updated note');
  await input.click();
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'alice', 'Editing a note must not navigate');
  await input.press('Enter');
  await bob.getByText('Updated note', { exact: true }).waitFor();
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
  await bob.focus();
  await page.keyboard.press('Enter');
  await selected('bob');
  await selectAlice();
  await bob.focus();
  await page.keyboard.press('Space');
  await selected('bob');
  const noteEntry = page.getByTestId('conversation-note-entry');
  const noteInput = noteEntry.getByRole('textbox', { name: '对话备注', exact: true });
  const noteValue = async value => page.waitForFunction(expected => document.querySelector('[data-testid="conversation-note-entry"] textarea')?.value === expected, value);
  await noteValue('Updated note');
  assert.equal(await noteEntry.getByRole('button').count(), 0, 'The note has no edit, save or cancel buttons');
  assert.equal(await noteInput.getAttribute('placeholder'), '对话备注');
  await noteInput.fill('Note edited in transcript\nSecond line');
  await bob.focus();
  await bob.getByText('Note edited in transcript Second line', { exact: true }).waitFor();
  assert.deepEqual(notes, ['Updated note', 'Note edited in transcript\nSecond line']);
  // Editing from the list updates the currently open transcript immediately.
  await bob.hover();
  await bob.getByRole('button', { name: '添加备注', exact: true }).click();
  await bob.getByRole('textbox', { name: '添加备注', exact: true }).fill('Synced from list');
  await bob.getByRole('textbox', { name: '添加备注', exact: true }).press('Enter');
  await noteValue('Synced from list');
  await page.reload();
  await noteValue('Synced from list');
  await setFilter(true);
  await noteInput.fill('Saved when leaving Bob');
  await selectAlice();
  await noteValue('重点客户 · Note alice');
  await bob.getByText('Saved when leaving Bob', { exact: true }).waitFor();
  await bob.locator('p').first().click();
  await selected('bob');
  await noteValue('Saved when leaving Bob');
  const clearedNoteSaved = page.waitForResponse(response => response.url().endsWith('/api/conversations/bob/note') && response.request().postDataJSON()?.note === '');
  await noteInput.fill('');
  await bob.focus();
  await clearedNoteSaved;
  await page.reload();
  await noteValue('');

  const rowIds = () => page.getByTestId('conversation-row').evaluateAll(elements => elements.map(element => element.textContent.match(/(\w+)@example\.com/)[1]));
  // Reload defaults to unanswered and drafts. Bob is now marked no_action.
  assert.equal(await filter.getAttribute('aria-current'), 'page');
  assert.deepEqual(await rowIds(), ['alice', 'doris']);
  await setFilter(true);
  assert.equal(await page.getByTestId('conversation-filter-all').getAttribute('aria-current'), 'page');
  assert.deepEqual(await rowIds(), ['alice', 'bob', 'carol', 'doris']);
  await setFilter(false);
  assert.equal(await filter.getAttribute('aria-current'), 'page');
  assert.deepEqual(await rowIds(), ['alice', 'doris'], 'Drafts stay visible alongside unanswered conversations');
  assert.equal(new URL(page.url()).searchParams.get('conversation'), 'bob', 'Filtering keeps the current conversation open');
  // Status updates immediately remove completed items from the filtered list.
  await row('alice').getByRole('button', { name: '待回复', exact: true }).click();
  await page.getByRole('menuitem', { name: '已发送', exact: true }).click();
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="conversation-row"]').length === 1);
  assert.deepEqual(await rowIds(), ['doris']);
  assert.equal(await page.getByTestId('mail-search').count(), 0, 'The list toolbar has no search input');
  assert.equal(await page.getByTestId('open-mail-search').evaluate(element => element.tagName), 'BUTTON', 'The search field is a button, so typing happens in the global dialog');
  assert.equal(await page.getByTestId('open-mail-search').getAttribute('aria-haspopup'), 'dialog');
  assert.equal(await page.getByTestId('conversation-list').getByRole('textbox').count(), 0, 'The search entry is not an editable input');
  await page.screenshot({ path: '/tmp/inbrix-search-entry-desktop.png' });
  await page.getByTestId('open-mail-search').click();
  const searchDialog = page.getByTestId('mail-search-dialog');
  const search = page.getByTestId('mail-search');
  await search.fill('Subject');
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="mail-search-result"]').length === 4);
  assert.equal(await search.evaluate(element => element === document.activeElement), true, 'Spotlight focuses its search input');
  await page.screenshot({ path: '/tmp/inbrix-spotlight-desktop.png' });
  assert.equal(await filter.getAttribute('aria-current'), 'page', 'Searching all statuses preserves the list filter');
  assert.deepEqual(await page.getByTestId('mail-search-result').evaluateAll(elements => elements.map(element => element.textContent.match(/(\w+)@example\.com/)[1])), ['alice', 'bob', 'carol', 'doris']);
  await search.press('ArrowDown');
  await search.press('Enter');
  await selected('bob');
  await searchDialog.waitFor({ state: 'hidden' });
  assert.equal(new URL(page.url()).searchParams.get('message'), '1', 'Search opens the matching message');
  assert.deepEqual(await rowIds(), ['doris'], 'Closing search preserves the list filter');
  await page.keyboard.press('Control+k');
  await searchDialog.waitFor();
  await search.press('Escape');
  await searchDialog.waitFor({ state: 'hidden' });
  await setFilter(true);
  await row('bob').waitFor();
  assert.deepEqual(await rowIds(), ['alice', 'bob', 'carol', 'doris']);
  const checkConversationLookup = async mobile => {
    for (const query of ['alice@example.com', '重点客户']) {
      await page.getByTestId('open-mail-search').click();
      await search.fill(query);
      const result = page.locator('[data-testid="mail-search-result"][data-result-kind="conversation"]');
      await result.waitFor();
      assert.equal(await page.getByTestId('mail-search-result').count(), 1, 'Conversation lookup has one result per conversation');
      assert.equal(await result.locator('strong').innerText(), await row('alice').locator('strong').innerText(), 'Search uses the inbox display name');
      assert.equal(await result.locator('p').first().innerText(), await row('alice').locator('p').first().innerText(), 'Search uses the latest inbox preview');
      assert.equal(await result.locator('mark').first().innerText(), query, 'Address and note matches are highlighted');
      await result.getByRole('img', { name: '最新邮件含有附件' }).waitFor();
      assert.ok((await result.innerText()).includes('重点客户'), 'Conversation notes appear with the summary');
      await page.screenshot({ path: `/tmp/inbrix-search-conversation-${mobile ? 'mobile' : 'desktop'}.png` });
      if (!mobile) {
        await search.press('Enter');
        await selected('alice');
        await searchDialog.waitFor({ state: 'hidden' });
        assert.equal(new URL(page.url()).searchParams.has('message'), false, 'Conversation lookup opens the thread without an unrelated message target');
        assert.equal(new URL(page.url()).searchParams.has('folder'), false, 'Conversation lookup clears the previous message folder');
      } else {
        const box = await result.boundingBox();
        assert.ok(box && box.x >= 0 && box.x + box.width <= 390, 'Conversation result fits on mobile');
        await search.press('Escape');
      }
    }
  };
  await checkConversationLookup(false);
  const editAliceNote = async note => {
    await row('alice').hover();
    await row('alice').getByRole('button', { name: '添加备注', exact: true }).click();
    const input = row('alice').getByRole('textbox', { name: '添加备注', exact: true });
    await input.fill(note);
    const saved = page.waitForResponse(response => response.url().endsWith('/api/conversations/alice/note') && response.request().postDataJSON()?.note === note);
    await input.press('Enter');
    await saved;
  };
  await editAliceNote('北方客户');
  await page.getByTestId('open-mail-search').click();
  await search.fill('重点客户');
  await searchDialog.getByText('没有找到匹配的对话或邮件', { exact: true }).waitFor();
  await search.fill('北方客户');
  await page.locator('[data-result-kind="conversation"]').waitFor();
  assert.equal(await searchDialog.locator('mark').innerText(), '北方客户', 'Saving a note refreshes cached search membership and highlighting');
  await search.press('Escape');
  await editAliceNote('重点客户 · Note alice');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${base}/inbox`);
  await row('doris').waitFor();
  const searchBox = await page.getByTestId('open-mail-search').boundingBox();
  assert.ok(searchBox && searchBox.x >= 0 && searchBox.x + searchBox.width <= 390, 'Search fits on mobile');
  assert.equal(await page.getByTestId('conversation-list').getByRole('combobox').count(), 0, 'Dropdown filters have moved out of the toolbar');
  await page.screenshot({ path: '/tmp/inbrix-search-entry-mobile.png' });
  assert.deepEqual(await rowIds(), ['doris']);
  assert.equal(await filter.getAttribute('aria-current'), 'page');
  await setFilter(true);
  assert.deepEqual(await rowIds(), ['alice', 'bob', 'carol', 'doris']);
  const attachmentBox = await row('alice').getByRole('img', { name: '最新邮件含有附件' }).boundingBox();
  assert.ok(attachmentBox && attachmentBox.width > 0 && attachmentBox.x >= 0 && attachmentBox.x + attachmentBox.width <= 390, 'Attachment indicator fits on mobile');
  await page.getByTestId('open-mail-search').click();
  await search.fill('Subject');
  await page.getByTestId('mail-search-result').first().waitFor();
  const dialogBox = await searchDialog.boundingBox();
  assert.ok(dialogBox.x >= 0 && dialogBox.x + dialogBox.width <= 390 && dialogBox.y + dialogBox.height <= 844, 'Spotlight fits the mobile viewport');
  await page.screenshot({ path: '/tmp/inbrix-spotlight-mobile.png' });
  await search.press('Escape');
  await checkConversationLookup(true);
  assert.deepEqual(errors, []);
  console.log('Passed: row interactions, note autosave, filters, message navigation, conversation lookup by address/note, shared inbox summary, note search invalidation, keyboard and mobile.');
} finally {
  await browser.close();
}
