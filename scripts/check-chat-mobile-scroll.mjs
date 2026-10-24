import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
page.setDefaultTimeout(10_000);
const accountEmail = 'me@example.com';
const messages = Array.from({ length: 8 }, (_, index) => ({
  id: String(index + 1), folder: 'INBOX', from: 'alice@example.com', to: accountEmail,
  cc: 'team@example.com', subject: 'Mobile history', body: `Mail ${index + 1}\n${'Message content\n'.repeat(18)}`,
  date: new Date(Date.UTC(2026, 9, 20, index)).toISOString(), outgoing: false, flags: ['\\Seen'],
}));
const summary = { id: 'mobile', title: 'Alice', peerEmail: 'alice@example.com', subject: 'Mobile history', count: messages.length, accountEmail, status: 'unanswered' };
const errors = [];
page.on('pageerror', error => errors.push(error.stack));
await page.addInitScript(() => {
  window.EventSource = class { close() {} };
});
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const { pathname } = new URL(route.request().url());
  let json = { calendar: false, models: [], agents: [], bindings: [], documents: [], attachments: [], signatures: [], total: 0 };
  if (pathname === '/api/conversations') json = { locale: 'zh-CN', accountEmail, conversations: [summary], folders: [], accounts: [{ id: 'account', email: accountEmail, isActive: true }] };
  if (pathname === '/api/conversations/mobile') json = { conversation: { ...summary, messages, hasMore: false, hasNewer: false } };
  if (pathname === '/api/drafts') json = { drafts: [] };
  await route.fulfill({ json });
});

const base = process.env.MESSAGE_TEST_URL || 'http://127.0.0.1:2342';
const detail = page.getByTestId('conversation-detail');
const viewport = detail.locator('[data-slot="scroll-area-viewport"]').first();
const gap = () => viewport.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight);
async function checkBounds(label) {
  const bounds = await detail.evaluate(element => {
    const header = element.querySelector('header');
    const viewport = element.querySelector('[data-slot="scroll-area-viewport"]');
    const buttons = [...header.querySelectorAll('button')].map(button => {
      const rect = button.getBoundingClientRect();
      return { left: rect.left, right: rect.right };
    });
    return { top: header.getBoundingClientRect().top, bottom: viewport.getBoundingClientRect().bottom, height: window.visualViewport.height, offset: window.visualViewport.offsetTop, width: innerWidth, pageTop: scrollY, buttons };
  });
  assert.ok(bounds.top >= bounds.offset - 1, `${label}: toolbar must remain visible: ${JSON.stringify(bounds)}`);
  assert.ok(bounds.bottom <= bounds.offset + bounds.height + 1, `${label}: scroll viewport must fit on screen: ${JSON.stringify(bounds)}`);
  assert.equal(bounds.pageTop, 0, `${label}: only the transcript should scroll`);
  assert.ok(bounds.buttons.every(button => button.left >= 0 && button.right <= bounds.width), `${label}: toolbar actions must fit`);
}
try {
  await page.goto(`${base}/inbox?conversation=mobile`);
  await page.waitForFunction(() => document.querySelectorAll('[data-message-key]').length === 8);
  await page.waitForTimeout(150);
  await checkBounds('portrait');
  assert.ok(await gap() < 3, 'Open at the latest message');

  // Browser chrome can change the height without changing message dimensions.
  await page.setViewportSize({ width: 390, height: 540 });
  await page.waitForTimeout(150);
  assert.ok(await gap() < 3, `Shrinking the visible height must keep the bottom visible, gap ${await gap()}`);
  await checkBounds('short portrait');

  await page.setViewportSize({ width: 844, height: 390 });
  await page.waitForTimeout(150);
  await checkBounds('landscape');
  await viewport.evaluate(element => { element.scrollTop -= 180; });
  await page.waitForTimeout(100);
  const readingTop = await viewport.evaluate(element => element.scrollTop);
  await page.setViewportSize({ width: 844, height: 300 });
  await page.waitForTimeout(150);
  assert.ok(Math.abs(await viewport.evaluate(element => element.scrollTop) - readingTop) < 3, 'Resizing while reading history must preserve the reading position');
  await checkBounds('short landscape');
  await viewport.evaluate(element => { element.scrollTop = element.scrollHeight; });
  await page.waitForTimeout(100);
  const note = await page.getByTestId('conversation-note-entry').boundingBox();
  assert.ok(note.y + note.height <= 300, 'The final note must be reachable');

  // The software keyboard resizes/pans the visual viewport independently of
  // the layout viewport. Simulate those browser events without changing CSS vh.
  await page.evaluate(() => {
    Object.defineProperties(visualViewport, {
      height: { configurable: true, value: 210 },
      offsetTop: { configurable: true, value: 35 },
    });
    visualViewport.dispatchEvent(new Event('resize'));
    visualViewport.dispatchEvent(new Event('scroll'));
  });
  await page.waitForTimeout(150);
  await checkBounds('keyboard');
  assert.ok(await gap() < 3, 'Keyboard resize must retain the bottom');
  await page.evaluate(() => {
    delete visualViewport.height;
    delete visualViewport.offsetTop;
    visualViewport.dispatchEvent(new Event('resize'));
    visualViewport.dispatchEvent(new Event('scroll'));
  });

  await page.goto(`${base}/inbox?conversation=mobile&message=2&messageFolder=INBOX`);
  await detail.locator('[data-message-key]').first().waitFor();
  await page.waitForTimeout(200);
  await checkBounds('search result');
  await page.setViewportSize({ width: 320, height: 568 });
  await page.waitForTimeout(150);
  await checkBounds('narrow phone');
  if (process.env.MESSAGE_TEST_SCREENSHOTS) await page.screenshot({ path: '/private/tmp/inbrix-mobile-chat-scroll.png' });
  await detail.locator('header button').first().click();
  await page.getByTestId('conversation-list').waitFor({ state: 'visible' });
  assert.equal(await page.evaluate(() => scrollY), 0, 'Back navigation must not inherit page scrolling');
  assert.deepEqual(errors, []);
  console.log('Mobile chat toolbar, bottom scrolling, viewport resizing, and search navigation passed.');
} catch (error) {
  console.error(error, errors);
  await page.screenshot({ path: '/private/tmp/inbrix-mobile-chat-scroll-failure.png', timeout: 3000 }).catch(() => {});
  throw error;
} finally {
  await browser.close();
}
