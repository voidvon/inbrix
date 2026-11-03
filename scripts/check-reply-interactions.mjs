import assert from 'node:assert/strict';
import { chromium, devices, webkit } from 'playwright';

// All APIs are intercepted; replying here never sends mail.
const browserType = process.env.REPLY_TEST_BROWSER === 'webkit' ? webkit : chromium;
const browser = await browserType.launch({ headless: true });
const mobile = process.env.REPLY_TEST_DEVICE !== 'desktop';
const page = await browser.newPage(mobile
  ? browserType === webkit ? devices['iPhone 13'] : devices['Pixel 7']
  : { viewport: { width: 1440, height: 1000 } });
page.setDefaultTimeout(10_000);
const errors = [];
page.on('pageerror', error => errors.push(error.stack));
const accountEmail = 'me@our-company.com';
const message = {
  id: '1', folder: 'INBOX', from: 'alice@example.com', to: `${accountEmail}, other@external.com, coworker@example.com`,
  cc: `team@example.com; Outside <outside@other.com>; Upper <UPPER@EXAMPLE.COM>; team@example.com; ${accountEmail}; alice@example.com; sub@sub.example.com; lookalike@notexample.com`, subject: 'Reply interactions', body: 'Original email body\n\nOn Monday, Bob wrote:\n> Earlier email body',
  date: new Date().toISOString(), outgoing: false, messageId: '<original@example.com>', flags: ['\\Seen'],
};
let savedDraft;
let restoredDraft;
const summary = { id: 'reply', title: 'Alice', peerEmail: message.from, subject: message.subject, count: 1, accountEmail, status: 'unanswered' };
await page.addInitScript(() => { window.EventSource = class { close() {} }; });
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const { pathname } = new URL(route.request().url());
  let json = { calendar: false, models: [], agents: [], bindings: [], documents: [], attachments: [], signatures: [], total: 0 };
  if (pathname === '/api/conversations') json = { locale: 'zh-CN', accountEmail: 'active@other.com', conversations: [summary], folders: [], accounts: [{ id: 'account', email: accountEmail, isActive: false }, { id: 'active', email: 'active@other.com', isActive: true }] };
  if (pathname === '/api/conversations/reply') json = { conversation: { ...summary, messages: [message], hasMore: false, hasNewer: false } };
  if (pathname === '/api/drafts') {
    if (route.request().method() === 'POST') {
      savedDraft = route.request().postDataJSON();
      json = { draft: { ...savedDraft, id: 'draft-reply', updatedAt: new Date().toISOString() } };
    } else json = { drafts: restoredDraft ? [restoredDraft] : [] };
  }
  assert.notEqual(pathname, '/api/compose', 'Opening a reply must not send it');
  await route.fulfill({ json });
});
const base = process.env.REPLY_TEST_URL || 'http://127.0.0.1:2342';
const detail = page.getByTestId('conversation-detail');
const compose = page.getByTestId('compose-inline');
async function checkEditor(label, expectedCc, hasFilteredCc = true) {
  await compose.locator('.tiptap').waitFor();
  await page.waitForFunction(() => document.querySelector('#compose-subject')?.value === 'Re: Reply interactions');
  await page.getByRole('menu').waitFor({ state: 'hidden' });
  const filteredCc = compose.getByTestId('compose-filtered-cc');
  if (!hasFilteredCc) assert.equal(await filteredCc.count(), 0, 'Same-domain or absent Cc must not be marked as filtered');
  else {
    const text = await filteredCc.innerText();
    assert.equal(text.includes('Outside <outside@other.com>'), label !== 'multi-domain', 'Only addresses outside every reply recipient domain should be filtered');
    assert.ok(text.includes('sub@sub.example.com'), 'Filtered subdomains must be visible');
    assert.ok(!text.includes('team@example.com'), 'Kept addresses must not appear as filtered');
    assert.ok(!text.includes(accountEmail), 'The mailbox address must not appear as filtered');
    assert.ok(!text.includes('alice@example.com'), 'The primary recipient must not appear as filtered');
    const positions = await Promise.all([compose.getByRole('textbox', { name: '抄送', exact: true }).boundingBox(), filteredCc.boundingBox(), compose.getByLabel('主题', { exact: true }).boundingBox()]);
    assert.ok(positions.every(Boolean));
    assert.ok(positions[1].y >= positions[0].y + positions[0].height && positions[1].y + positions[1].height <= positions[2].y, 'Filtered addresses must appear below Cc and above Subject');
  }
  await compose.getByLabel('主题', { exact: true }).fill(`Editable ${label}`);
  assert.equal(await compose.getByLabel('主题', { exact: true }).inputValue(), `Editable ${label}`);
  const quote = compose.locator('.reply-quote').first();
  await quote.getByRole('button', { name: 'Expand quoted message', exact: true }).first().click();
  const paragraph = quote.locator('[data-node-view-content-react] > p').filter({ hasText: 'Original email body' });
  await paragraph.click();
  await paragraph.evaluate(element => {
    const range = document.createRange();
    range.selectNodeContents(element);
    range.collapse(false);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
  });
  await page.keyboard.insertText(' edited');
  const saved = page.waitForResponse(response => new URL(response.url()).pathname === '/api/drafts' && response.request().method() === 'POST');
  await compose.getByRole('button', { name: '关闭', exact: true }).click();
  await saved;
  assert.ok(savedDraft.bodyHtml.includes('Original email body edited'), 'Quote edits must reach the editor state and draft');
  assert.ok(savedDraft.bodyHtml.includes('Earlier email body'), 'Nested quotes must survive editing');
  assert.equal(savedDraft.inReplyTo, message.messageId, 'Reply headers must survive editing');
  assert.equal(savedDraft.mode, 'reply', 'Replies with Cc must use the regular reply mode');
  assert.equal(savedDraft.to, message.outgoing ? message.to : message.from, 'Reply must address the sender without copying the original To list');
  assert.equal(savedDraft.accountEmail, accountEmail, 'Reply must use the conversation mailbox');
  assert.equal(savedDraft.cc, expectedCc, 'Cc must follow the reply recipient domains and preserve existing draft edits');
  await compose.waitFor({ state: 'hidden' });
}
try {
  const modes = mobile ? ['toolbar', 'context', 'long-press'] : ['toolbar', 'context'];
  for (const mode of [...modes, 'outgoing', 'multi-domain', 'external-only', 'tecnica', 'restored', 'no-cc']) {
    let expectedCc = 'team@example.com, Upper <UPPER@EXAMPLE.COM>';
    let hasFilteredCc = true;
    if (mode === 'outgoing') {
      message.outgoing = true;
      message.from = accountEmail;
      message.to = 'alice@example.com';
    }
    if (mode === 'multi-domain') {
      message.to = 'alice@example.com, partner@other.com';
      expectedCc += ', Outside <outside@other.com>';
      message.cc = 'team@example.com; Upper <UPPER@EXAMPLE.COM>; Outside <outside@other.com>; sub@sub.example.com';
    }
    if (mode === 'external-only') {
      message.to = 'alice@example.com';
      message.cc = 'Outside <outside@other.com>; sub@sub.example.com';
      expectedCc = '';
    }
    if (mode === 'tecnica') {
      message.outgoing = false;
      message.from = 'cnajarro@tecnica.com.gt';
      message.to = `${accountEmail}, logistics@tecnica.com.gt`;
      message.cc = 'gerencia@tecnica.com.gt';
      summary.peerEmail = 'cnajarro@tecnica.com.gt, logistics@tecnica.com.gt';
      summary.primaryPeerEmail = message.from;
    }
    if (['tecnica', 'restored'].includes(mode)) {
      expectedCc = 'gerencia@tecnica.com.gt';
      hasFilteredCc = false;
    }
    if (mode === 'restored') {
      expectedCc = 'manual@chosen.com';
      restoredDraft = { ...savedDraft, to: message.from, cc: expectedCc, subject: 'Re: Reply interactions' };
    }
    if (mode === 'no-cc') {
      restoredDraft = undefined;
      message.cc = '';
      expectedCc = '';
      hasFilteredCc = false;
    }
    console.log(`Checking ${mobile ? 'mobile' : 'desktop'} ${mode}`);
    await page.goto(`${base}/inbox?conversation=reply`);
    await detail.locator('[data-message-key]').waitFor();
    assert.equal(await detail.getByRole('button', { name: '回复全部', exact: true }).count(), 0, 'Toolbar must only offer Reply');
    if (mode === 'toolbar') {
      const reply = detail.getByRole('button', { name: '回复', exact: true });
      if (mobile) await reply.tap();
      else await reply.click();
    }
    else {
      const trigger = detail.locator('[data-message-key]');
      if (mobile && mode === 'long-press') {
        await trigger.scrollIntoViewIfNeeded();
        const box = await trigger.boundingBox();
        const touch = { x: box.x + 20, y: box.y + 20 };
        if (browserType === chromium) {
          const input = await page.context().newCDPSession(page);
          await input.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [touch] });
          await page.getByRole('menu').waitFor();
          await input.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
          await input.detach();
        } else {
          await trigger.evaluate((element, touch) => {
            const event = new Event('touchstart', { bubbles: true });
            Object.defineProperty(event, 'touches', { value: [{ clientX: touch.x, clientY: touch.y }] });
            element.dispatchEvent(event);
          }, touch);
          await page.getByRole('menu').waitFor();
          await trigger.dispatchEvent('touchend');
        }
      } else await trigger.click({ button: 'right' });
      assert.equal(await page.getByRole('menuitem', { name: '回复全部', exact: true }).count(), 0, 'Message menu must only offer Reply');
      const item = page.getByRole('menuitem', { name: '回复', exact: true });
      if (mobile) await item.tap();
      else await item.press('Enter');
    }
    await checkEditor(mode, expectedCc, hasFilteredCc);
  }
  assert.deepEqual(errors, []);
  console.log('Reply opens an editable composer, dismisses menus, preserves quote edits, and closes successfully.');
} catch (error) {
  console.error(errors);
  console.error(await page.locator('body').innerText({ timeout: 3000 }).catch(() => 'Page stopped responding'));
  await page.screenshot({ path: '/private/tmp/inbrix-reply-interactions-failure.png', timeout: 3000 }).catch(() => {});
  throw error;
} finally {
  await browser.close();
}
