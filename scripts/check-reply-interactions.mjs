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
const accountEmail = 'me@example.com';
const message = {
  id: '1', folder: 'INBOX', from: 'alice@example.com', to: accountEmail,
  cc: 'team@example.com', subject: 'Reply interactions', body: 'Original email body\n\nOn Monday, Bob wrote:\n> Earlier email body',
  date: new Date().toISOString(), outgoing: false, messageId: '<original@example.com>', flags: ['\\Seen'],
};
let savedDraft;
const summary = { id: 'reply', title: 'Alice', peerEmail: message.from, subject: message.subject, count: 1, accountEmail, status: 'unanswered' };
await page.addInitScript(() => { window.EventSource = class { close() {} }; });
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const { pathname } = new URL(route.request().url());
  let json = { calendar: false, models: [], agents: [], bindings: [], documents: [], attachments: [], signatures: [], total: 0 };
  if (pathname === '/api/conversations') json = { locale: 'zh-CN', accountEmail, conversations: [summary], folders: [], accounts: [{ id: 'account', email: accountEmail, isActive: true }] };
  if (pathname === '/api/conversations/reply') json = { conversation: { ...summary, messages: [message], hasMore: false, hasNewer: false } };
  if (pathname === '/api/drafts') {
    if (route.request().method() === 'POST') {
      savedDraft = route.request().postDataJSON();
      json = { draft: { ...savedDraft, id: 'draft-reply', updatedAt: new Date().toISOString() } };
    } else json = { drafts: [] };
  }
  assert.notEqual(pathname, '/api/compose', 'Opening a reply must not send it');
  await route.fulfill({ json });
});
const base = process.env.REPLY_TEST_URL || 'http://127.0.0.1:2342';
const detail = page.getByTestId('conversation-detail');
const compose = page.getByTestId('compose-inline');
async function checkEditor(label) {
  await compose.locator('.tiptap').waitFor();
  await page.waitForFunction(() => document.querySelector('#compose-subject')?.value === 'Re: Reply interactions');
  await page.getByRole('menu').waitFor({ state: 'hidden' });
  await compose.getByLabel('主题', { exact: true }).fill(`Editable ${label}`);
  assert.equal(await compose.getByLabel('主题', { exact: true }).inputValue(), `Editable ${label}`);
  const quote = compose.locator('.reply-quote').first();
  await quote.getByRole('button', { name: 'Expand quoted message', exact: true }).first().click();
  const paragraph = quote.locator('[data-node-view-content-react] > p').filter({ hasText: 'Original email body' });
  await paragraph.click();
  await page.keyboard.press('End');
  await page.keyboard.insertText(' edited');
  const saved = page.waitForResponse(response => new URL(response.url()).pathname === '/api/drafts' && response.request().method() === 'POST');
  await compose.getByRole('button', { name: '关闭', exact: true }).click();
  await saved;
  assert.ok(savedDraft.bodyHtml.includes('Original email body edited'), 'Quote edits must reach the editor state and draft');
  assert.ok(savedDraft.bodyHtml.includes('Earlier email body'), 'Nested quotes must survive editing');
  assert.equal(savedDraft.inReplyTo, message.messageId, 'Reply headers must survive editing');
  assert.equal(savedDraft.cc, label === 'reply-all' ? 'team@example.com' : '', 'Reply all must preserve Cc recipients');
  await compose.waitFor({ state: 'hidden' });
}
try {
  for (const mode of mobile ? ['toolbar', 'context', 'long-press', 'reply-all'] : ['toolbar', 'context', 'reply-all']) {
    console.log(`Checking ${mobile ? 'mobile' : 'desktop'} ${mode}`);
    await page.goto(`${base}/inbox?conversation=reply`);
    await detail.locator('[data-message-key]').waitFor();
    if (mode === 'toolbar') {
      const reply = detail.getByRole('button', { name: '回复', exact: true });
      if (mobile) await reply.tap();
      else await reply.click();
    }
    else {
      const trigger = detail.locator('[data-message-key]');
      if (mobile && (mode === 'long-press' || mode === 'reply-all')) {
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
      const item = page.getByRole('menuitem', { name: mode === 'reply-all' ? '回复全部' : '回复', exact: true });
      if (mobile) await item.tap();
      else await item.press('Enter');
    }
    await checkEditor(mode);
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
