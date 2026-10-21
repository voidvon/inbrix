import { chromium } from 'playwright';
import assert from 'node:assert/strict';

// Isolated browser regression: all mail APIs and realtime events are mocked.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const accountEmail = 'me@example.com';
const messages = Array.from({ length: 10 }, (_, index) => ({
  id: String(index + 1), folder: 'INBOX', from: 'alice@example.com', to: accountEmail,
  subject: 'History', preview: `Mail ${index + 1}`, body: `Mail ${index + 1}\n${'Message content\n'.repeat(18)}`,
  // Older HTML frames resize after being prepended; anchoring must survive it.
  html: index < 7 ? `<div style="height:600px">HTML mail ${index + 1}</div>${index === 6 ? [400, 850, 1350].map(delay => `<img src="/__scroll-test/${delay}" style="display:block;max-width:100%">`).join('') : ''}` : '',
  date: new Date(Date.UTC(2026, 9, 20, index)).toISOString(), outgoing: false, flags: ['\\Seen'],
}));
const summary = { id: 'history', title: 'Alice', peerEmail: 'alice@example.com', subject: 'History', preview: 'Mail 10', date: messages.at(-1).date, count: 10, unreadCount: 0, accountEmail, status: 'unanswered' };
const requests = [];
const errors = [];
let failOlder = false;
let failNewer = false;
let delayOlder = 120;
let delayHead = 0;
let trackPreload = false;
const preloadRequests = [];
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/__scroll-test/*', async route => {
  await new Promise(resolve => setTimeout(resolve, Number(new URL(route.request().url()).pathname.split('/').at(-1))));
  await route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="600" height="720"><rect width="600" height="720" fill="#888"/></svg>' });
});
await page.addInitScript(() => {
  window.EventSource = class {
    constructor() { window.__mailSource = this; }
    close() {}
  };
});
await page.route('**/api/**', async route => {
  const url = new URL(route.request().url());
  let json = {};
  if (url.pathname === '/api/conversations') {
    json = { locale: 'zh-CN', accountEmail, conversations: [{ ...summary, count: messages.length }], folders: [], accounts: [{ id: 'account', email: accountEmail, label: accountEmail, isActive: true }] };
  } else if (url.pathname === '/api/conversations/history') {
    const cursor = url.searchParams.get('cursor');
    const through = url.searchParams.get('through');
    const after = url.searchParams.get('after');
    const until = url.searchParams.get('until');
    const around = url.searchParams.get('around');
    requests.push({ cursor, through, limit: url.searchParams.get('limit'), ...(after ? { after } : {}), ...(until ? { until } : {}), ...(around ? { around } : {}) });
    if (cursor) {
      if (trackPreload) preloadRequests.push({ cursor, top: await page.getByTestId('conversation-detail').locator('[data-slot="scroll-area-viewport"]').first().evaluate(element => element.scrollTop) });
      await new Promise(resolve => setTimeout(resolve, delayOlder));
      if (failOlder) return route.fulfill({ status: 500, json: { error: 'History temporarily unavailable' } });
    } else if (after) {
      await new Promise(resolve => setTimeout(resolve, delayOlder));
      if (failNewer) return route.fulfill({ status: 500, json: { error: 'Newer history temporarily unavailable' } });
    } else if (delayHead) await new Promise(resolve => setTimeout(resolve, delayHead));
    const atLeast = id => { const i = messages.findIndex(message => Number(message.id) >= Number(id)); return i < 0 ? messages.length : i; };
    const greater = id => { const i = messages.findIndex(message => Number(message.id) > Number(id)); return i < 0 ? messages.length : i; };
    let end = cursor ? atLeast(cursor) : messages.length;
    let start = through ? Math.min(Math.max(0, end - 3), atLeast(through)) : Math.max(0, end - 3);
    if (until) { start = atLeast(through); end = greater(until); }
    if (after) { start = greater(after); end = Math.min(messages.length, start + 3); }
    if (around) {
      const target = messages.findIndex(message => message.id === around && message.folder === (url.searchParams.get('folder') || 'INBOX'));
      if (target < 0) return route.fulfill({ status: 404, json: { error: 'Message not found' } });
      start = Math.max(0, Math.min(target - 1, messages.length - 3));
      end = Math.min(messages.length, start + 3);
    }
    const slice = messages.slice(start, end);
    json = { conversation: { ...summary, count: messages.length, messages: slice, hasMore: start > 0, hasNewer: end < messages.length, oldestCursor: slice[0]?.id, newestCursor: slice.at(-1)?.id } };
  } else if (url.pathname === '/api/drafts') json = { drafts: [] };
  else json = { calendar: false, notifications: false, models: [], agents: [], bindings: [], documents: [], attachments: [], signatures: [], total: 0 };
  await route.fulfill({ json });
});

const base = process.env.MESSAGE_TEST_URL || 'http://127.0.0.1:2342';
const bubbles = page.locator('[data-message-key]');
const viewport = page.getByTestId('conversation-detail').locator('[data-slot="scroll-area-viewport"]').first();
async function waitCount(count) {
  await page.waitForFunction(expected => document.querySelectorAll('[data-message-key]').length === expected, count);
}
async function metrics(id) {
  return viewport.evaluate((element, targetId) => {
    const target = [...element.querySelectorAll('[data-message-key]')].find(node => node.dataset.messageKey === `INBOX\0${targetId}`);
    return { top: target.getBoundingClientRect().top - element.getBoundingClientRect().top, scrollTop: element.scrollTop, bottom: element.scrollHeight - element.scrollTop - element.clientHeight };
  }, id);
}
async function realtime() {
  await page.evaluate(() => window.__mailSource.onmessage({ data: JSON.stringify({ type: 'mail.new' }) }));
}
try {
  await page.goto(`${base}/inbox?conversation=history`);
  await waitCount(3);
  assert.deepEqual(requests[0], { cursor: null, through: null, limit: '3' });
  await page.waitForTimeout(250);
  assert.equal(await bubbles.count(), 3, 'Initial render must not drain older pages');
  assert.ok((await metrics('10')).bottom < 3, 'Open at the latest mail');

  // Small wheel steps must release bottom following immediately. A threshold
  // here used to pull every movement smaller than 80px back to the bottom.
  await viewport.hover();
  await page.mouse.wheel(0, -12);
  await page.waitForTimeout(250);
  const gentleScroll = await metrics('10');
  assert.ok(gentleScroll.bottom >= 8, `A gentle upward wheel must leave the bottom, actual gap ${gentleScroll.bottom}px`);
  await realtime();
  await page.waitForTimeout(350);
  assert.ok(Math.abs((await metrics('10')).scrollTop - gentleScroll.scrollTop) < 2, 'A refresh must not reattach a reader who moved just 12px upwards');
  await page.mouse.wheel(0, -12);
  await page.waitForTimeout(150);
  assert.ok((await metrics('10')).bottom >= 20, 'Successive small wheel steps must accumulate normally');
  await page.mouse.wheel(0, 8);
  await page.waitForTimeout(150);
  assert.ok((await metrics('10')).bottom >= 12, 'Moving slightly towards the bottom must not reattach early');
  await page.mouse.wheel(0, 100);
  await page.waitForTimeout(150);
  assert.ok((await metrics('10')).bottom < 3, 'Scrolling all the way down must reach the bottom');
  const resumedFollowing = await viewport.evaluate(async element => {
    const content = element.querySelector('[data-testid="conversation-note-entry"]').parentElement;
    const addition = document.createElement('div');
    addition.style.height = '40px';
    content.append(addition);
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    const gap = element.scrollHeight - element.scrollTop - element.clientHeight;
    addition.remove();
    return gap;
  });
  assert.ok(resumedFollowing < 3, 'Bottom following must resume after the user scrolls back to the bottom');

  await viewport.evaluate(element => { element.scrollTop = 0; });
  const anchorBefore = await metrics('8');
  await viewport.evaluate(element => {
    const anchor = [...element.querySelectorAll('[data-message-key]')].find(node => node.dataset.messageKey === 'INBOX\u00008');
    window.__scrollSamples = [];
    const started = performance.now();
    const sample = () => {
      window.__scrollSamples.push(anchor.getBoundingClientRect().top - element.getBoundingClientRect().top);
      if (performance.now() - started < 1900) requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
  await waitCount(6);
  await page.waitForTimeout(2000);
  const maxShift = await page.evaluate(initial => Math.max(...window.__scrollSamples.map(top => Math.abs(top - initial))), anchorBefore.top);
  console.log('maximum frame shift', maxShift);
  assert.ok(maxShift < 4, `Every frame must retain the anchor while three delayed images load, maximum shift ${maxShift}px`);
  assert.ok(Math.abs((await metrics('8')).top - anchorBefore.top) < 4, 'Prepending and iframe resizing must preserve the reading position');
  assert.equal(requests.filter(request => request.cursor === '8').length, 1, 'One request per boundary');

  // A realtime refresh must preserve history, remove deleted mail, and include new mail.
  messages.splice(messages.findIndex(message => message.id === '6'), 1);
  messages.push({ ...messages.at(-1), id: '11', body: 'New mail 11' });
  await realtime();
  await page.getByText('New mail 11', { exact: true }).waitFor();
  assert.equal(await bubbles.count(), 6);
  assert.ok(requests.some(request => request.through === '5'), 'Refresh the whole loaded window');
  assert.ok(Math.abs((await metrics('8')).top - anchorBefore.top) < 4, 'Refresh while reading history must preserve the anchor');

  failOlder = true;
  await viewport.evaluate(element => { element.scrollTop = 0; });
  await page.getByTestId('load-older-messages').getByRole('button', { name: '重试', exact: true }).waitFor();
  const failedRequests = requests.length;
  await page.waitForTimeout(300);
  assert.equal(requests.length, failedRequests, 'A failed page must wait for retry');
  failOlder = false;
  await page.getByTestId('load-older-messages').getByRole('button').click();
  await waitCount(9);

  // Race a head refresh against another older page. The refreshed range must
  // widen to include the page that arrived while the head request was pending.
  delayHead = 500;
  delayOlder = 50;
  await realtime();
  await page.waitForTimeout(180);
  await page.getByTestId('load-older-messages').getByRole('button').click();
  await waitCount(10);
  await page.waitForTimeout(1100);
  assert.equal(await bubbles.count(), 10);
  assert.equal(await page.getByTestId('load-older-messages').count(), 0);

  // Once the reader returns to the bottom, incoming mail follows normally.
  delayHead = 0;
  await viewport.evaluate(element => { element.scrollTop = element.scrollHeight; });
  await page.waitForTimeout(50);
  messages.push({ ...messages.at(-1), id: '12', body: 'New mail 12' });
  await realtime();
  await waitCount(11);
  await page.waitForTimeout(100);
  assert.ok((await metrics('12')).bottom < 3, 'Follow new mail when already at the bottom');

  // Search opens directly around the hit, without loading the intervening mail.
  const searchStart = requests.length;
  await page.goto(`${base}/inbox?conversation=history&message=2&messageFolder=INBOX`);
  await page.waitForFunction(() => [...document.querySelectorAll('[data-message-key]')].some(node => node.dataset.messageKey === 'INBOX\u00002'));
  await page.waitForTimeout(700);
  assert.equal(await bubbles.count(), 3, 'Search must load only the local 3-message window');
  assert.ok(requests.slice(searchStart).some(request => request.around === '2'), 'Search must request the target window directly');
  assert.ok(requests.slice(searchStart).every(request => !request.cursor && !request.after), 'Search must not walk from the latest page');
  const focused = await metrics('2');
  assert.ok(focused.top < 900 && focused.top > -300, 'Search target must be scrolled into view');

  // Navigate to another result in the same mounted conversation.
  await page.evaluate(() => {
    history.pushState(null, '', '/inbox?conversation=history&message=5&messageFolder=INBOX');
    dispatchEvent(new PopStateEvent('popstate'));
  });
  await page.waitForFunction(() => [...document.querySelectorAll('[data-message-key]')].some(node => node.dataset.messageKey === 'INBOX\u00005'));
  await page.waitForTimeout(1700);
  assert.equal(await bubbles.count(), 3);
  assert.ok(requests.some(request => request.around === '5'));
  if (process.env.MESSAGE_TEST_SCREENSHOTS) await page.screenshot({ path: '/private/tmp/inbrix-search-window.png' });
  const middleBefore = await metrics('5');
  await realtime();
  await page.waitForTimeout(350);
  assert.equal(await bubbles.count(), 3, 'Realtime refresh of a middle window must not fill the gap to latest');
  assert.ok(requests.some(request => request.through === '4' && request.until === '7'));
  assert.ok(Math.abs((await metrics('5')).top - middleBefore.top) < 4);

  await viewport.evaluate(element => { element.scrollTop = 300; });
  await page.waitForTimeout(40);
  const beforeOlder = await metrics('5');
  await waitCount(6);
  await page.waitForTimeout(200);
  assert.ok(Math.abs((await metrics('5')).top - beforeOlder.top) < 4, 'Loading older mail must not recenter the search hit');

  failNewer = true;
  await viewport.evaluate(element => { element.scrollTop = element.scrollHeight - element.clientHeight - 300; });
  await page.getByTestId('load-newer-messages').getByRole('button', { name: '重试', exact: true }).waitFor();
  const failedNewerRequests = requests.length;
  await page.waitForTimeout(300);
  assert.equal(requests.length, failedNewerRequests, 'A failed forward page must wait for retry');
  failNewer = false;
  delayHead = 500;
  delayOlder = 50;
  await realtime();
  await page.waitForTimeout(180);
  const beforeNewer = await viewport.evaluate(element => element.scrollTop);
  await page.getByTestId('load-newer-messages').getByRole('button').dispatchEvent('click');
  await waitCount(9);
  await page.waitForTimeout(1100);
  assert.equal(await bubbles.count(), 9, 'A concurrent refresh must retain the forward extension');
  assert.ok(requests.some(request => request.through === '1' && request.until === '10'));
  delayHead = 0;
  assert.ok(Math.abs(await viewport.evaluate(element => element.scrollTop) - beforeNewer) < 4, 'Appending newer mail must preserve the reading position');

  // Jump to latest while a forward page is in flight. Its late result must
  // never append to the replacement window or resurrect the old search focus.
  delayOlder = 700;
  await page.getByTestId('load-newer-messages').getByRole('button').click();
  const beforeLatest = requests.length;
  delayHead = 450;
  await page.getByTestId('return-to-latest').click();
  await page.waitForTimeout(100);
  await realtime();
  await waitCount(3);
  await page.waitForTimeout(900);
  assert.equal(await bubbles.count(), 3, 'Jump to latest must replace the window and discard stale pages');
  assert.deepEqual(await bubbles.evaluateAll(nodes => nodes.map(node => node.dataset.messageKey)), ['INBOX\u000010', 'INBOX\u000011', 'INBOX\u000012']);
  assert.ok((await metrics('12')).bottom < 3, 'Jump to latest must land at the actual bottom');
  assert.equal(new URL(page.url()).searchParams.get('message'), null, 'The old search target must be removed from the URL');
  assert.ok(requests.slice(beforeLatest).some(request => !request.cursor && !request.after && !request.through && !request.around), 'Jump requests only the latest page');
  delayOlder = 120;
  delayHead = 0;

  // Keep scrolling while several image loads change heights. Compensation must
  // preserve each user movement instead of snapping back to an old anchor.
  await page.goto(`${base}/inbox?conversation=history`);
  await waitCount(3);
  await viewport.evaluate(element => { element.scrollTop = 0; });
  await waitCount(6);
  const scrollingShift = await viewport.evaluate(async element => {
    const anchor = [...element.querySelectorAll('[data-message-key]')].find(node => node.dataset.messageKey === 'INBOX\u000010');
    let expected = anchor.getBoundingClientRect().top - element.getBoundingClientRect().top;
    let maximum = 0;
    const started = performance.now();
    await new Promise(resolve => {
      const sample = () => {
        const actual = anchor.getBoundingClientRect().top - element.getBoundingClientRect().top;
        maximum = Math.max(maximum, Math.abs(actual - expected));
        const previous = element.scrollTop;
        element.scrollTop -= 3;
        expected -= element.scrollTop - previous;
        if (performance.now() - started < 1900) requestAnimationFrame(sample);
        else resolve();
      };
      requestAnimationFrame(sample);
    });
    return maximum;
  });
  console.log('maximum shift while scrolling', scrollingShift);
  assert.ok(scrollingShift < 4, `Delayed height changes must preserve ongoing scroll input, maximum error ${scrollingShift}px`);

  // Read text inside a long email whose images above the reading point have
  // not loaded yet. Preserving just the outer bubble's top is insufficient.
  messages.find(message => message.id === '7').html = `<div style="height:160px">Before images</div>${[400, 850, 1350].map(delay => `<img src="/__scroll-test/${delay}" style="display:block;max-width:100%">`).join('')}<p id="reading-point">Keep this sentence in the same place.</p><div style="height:900px">After the reading point</div>`;
  await page.goto(`${base}/inbox?conversation=history`);
  await waitCount(3);
  await viewport.evaluate(element => { element.scrollTop = 0; });
  await waitCount(6);
  await page.waitForFunction(() => [...document.querySelectorAll('[data-message-key]')].find(node => node.dataset.messageKey === 'INBOX\u00007')?.querySelector('iframe')?.contentDocument?.getElementById('reading-point'));
  const innerShift = await viewport.evaluate(async element => {
    const frame = [...element.querySelectorAll('[data-message-key]')].find(node => node.dataset.messageKey === 'INBOX\u00007').querySelector('iframe');
    const text = frame.contentDocument.getElementById('reading-point');
    const position = () => frame.getBoundingClientRect().top + text.getBoundingClientRect().top - element.getBoundingClientRect().top;
    element.scrollTop += position() - 16;
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    const expected = position();
    const initialTop = element.scrollTop;
    let maximum = 0;
    const started = performance.now();
    await new Promise(resolve => {
      const sample = () => {
        maximum = Math.max(maximum, Math.abs(position() - expected));
        if (performance.now() - started < 1900) requestAnimationFrame(sample);
        else resolve();
      };
      requestAnimationFrame(sample);
    });
    return {
      maximum,
      compensated: element.scrollTop - initialTop,
      loadedImages: [...frame.contentDocument.images].filter(image => image.complete && image.naturalHeight > 0).length,
    };
  });
  console.log('maximum shift inside a long email', innerShift.maximum);
  assert.equal(innerShift.loadedImages, 3, 'All delayed images must have loaded during the check');
  assert.ok(innerShift.compensated > 0, 'The test must exercise a height change after the reader reaches the text');
  assert.ok(innerShift.maximum < 4, `Images inside the email must preserve the visible text, maximum shift ${innerShift.maximum}px`);

  // Wheel events inside an email iframe do not bubble to the parent document;
  // the scroll handler must also release following when it observes movement.
  messages.at(-1).html = '<div style="height:1600px">Long latest HTML email</div>';
  await page.goto(`${base}/inbox?conversation=history`);
  await waitCount(3);
  await page.waitForFunction(() => [...document.querySelectorAll('[data-message-key]')].find(node => node.dataset.messageKey === 'INBOX\u000012')?.querySelector('iframe')?.getBoundingClientRect().height > 1500);
  const framePoint = await viewport.evaluate(element => {
    const frame = [...element.querySelectorAll('[data-message-key]')].find(node => node.dataset.messageKey === 'INBOX\u000012').querySelector('iframe').getBoundingClientRect();
    const rect = element.getBoundingClientRect();
    return { x: (Math.max(frame.left, rect.left) + Math.min(frame.right, rect.right)) / 2, y: (Math.max(frame.top, rect.top) + Math.min(frame.bottom, rect.bottom)) / 2 };
  });
  await page.mouse.move(framePoint.x, framePoint.y);
  await page.mouse.wheel(0, -12);
  await page.waitForTimeout(250);
  assert.ok((await metrics('12')).bottom >= 8, 'A gentle wheel over HTML mail must release bottom following too');

  // Leave runway before the top, including across successive pages. Simulate
  // continued trackpad movement with 450ms network latency, rather than jumping
  // straight to zero (which cannot prove that requests start early enough).
  messages.forEach(message => { message.html = `<div style="height:600px">History ${message.id}</div>`; });
  delayOlder = 450;
  await page.goto(`${base}/inbox?conversation=history`);
  await waitCount(3);
  await page.waitForFunction(() => [...document.querySelectorAll('[data-message-key] iframe')].length === 3 && [...document.querySelectorAll('[data-message-key] iframe')].every(frame => frame.getBoundingClientRect().height >= 600));
  await page.waitForTimeout(150);
  assert.equal(await bubbles.count(), 3, 'Preloading must still wait for upward intent');
  const runway = await viewport.evaluate(element => Math.max(600, element.clientHeight));
  trackPreload = true;
  await viewport.evaluate((element, distance) => { element.scrollTop = distance + 120; }, runway);
  await page.waitForTimeout(50);
  assert.equal(preloadRequests.length, 0, 'Stay idle while outside the preload range');
  const minimumTop = await viewport.evaluate(async element => {
    let minimum = element.scrollTop;
    const started = performance.now();
    await new Promise(resolve => {
      const step = () => {
        element.scrollTop -= 14;
        minimum = Math.min(minimum, element.scrollTop);
        if (performance.now() - started < 3600) requestAnimationFrame(step);
        else resolve();
      };
      requestAnimationFrame(step);
    });
    return minimum;
  });
  trackPreload = false;
  console.log('preload request positions', preloadRequests, 'minimum distance to top', minimumTop);
  assert.ok(preloadRequests.length >= 2, 'Continued upward scrolling must preload successive pages');
  assert.ok(preloadRequests.every(request => request.top > 150 && request.top <= runway), 'Each request must start with runway remaining before the top');
  assert.ok(minimumTop > 0, 'Continued scrolling must not hit the boundary while delayed pages load');
  assert.equal(new Set(preloadRequests.map(request => request.cursor)).size, preloadRequests.length, 'Preloading must not duplicate a boundary request');

  // Even with no scrollable runway, wheel input inside a short HTML email
  // must reach the pagination logic in the parent transcript.
  messages.forEach(message => { message.html = `<p>Short HTML ${message.id}</p>`; });
  delayOlder = 120;
  await page.goto(`${base}/inbox?conversation=history&message=5&messageFolder=INBOX`);
  await waitCount(3);
  await page.waitForTimeout(200);
  assert.equal(await viewport.evaluate(element => element.scrollHeight <= element.clientHeight), true, 'The fixture must fit without scrolling');
  await bubbles.nth(1).locator('iframe').hover();
  await page.mouse.wheel(0, 12);
  await waitCount(6);
  assert.ok(await bubbles.evaluateAll(nodes => nodes.some(node => node.dataset.messageKey === 'INBOX\u000010')), 'Wheel over a short iframe must load forward');

  // Mobile pull at the top of a short transcript still triggers another page.
  await page.setViewportSize({ width: 390, height: 844 });
  messages.forEach(message => { message.body = `Short ${message.id}`; message.html = ''; });
  await page.goto(`${base}/inbox?conversation=history`);
  await waitCount(3);
  await viewport.evaluate(element => {
    element.dispatchEvent(new TouchEvent('touchstart', { touches: [new Touch({ identifier: 1, target: element, clientY: 100 })], bubbles: true }));
    element.dispatchEvent(new TouchEvent('touchmove', { touches: [new Touch({ identifier: 1, target: element, clientY: 140 })], bubbles: true }));
  });
  await waitCount(6);
  await page.goto(`${base}/inbox?conversation=history&message=5&messageFolder=INBOX`);
  await waitCount(3);
  if (process.env.MESSAGE_TEST_SCREENSHOTS) await page.screenshot({ path: '/private/tmp/inbrix-mobile-search-window.png' });
  await viewport.evaluate(element => {
    element.dispatchEvent(new TouchEvent('touchstart', { touches: [new Touch({ identifier: 1, target: element, clientY: 140 })], bubbles: true }));
    element.dispatchEvent(new TouchEvent('touchmove', { touches: [new Touch({ identifier: 1, target: element, clientY: 100 })], bubbles: true }));
  });
  await waitCount(6);
  assert.ok(await bubbles.evaluateAll(nodes => nodes.some(node => node.dataset.messageKey === 'INBOX\u000010')), 'Mobile upward swipe must load newer mail');
  assert.deepEqual(errors, []);
  console.log('Passed: early loading across successive pages without hitting the top, gentle wheel releases bottom following, return to bottom resumes following, wheel inside HTML mail, initial 3, upward loading, every-frame anchoring with delayed images, ongoing scroll input, reading inside a long email, realtime refresh/deletion, retry, concurrent refresh, follow latest, direct search positioning, bidirectional pagination and refresh, forward retry, jump to latest during a pending page, short iframe wheel, mobile swipes in both directions.');
} catch (error) {
  console.error('Recent requests', requests.slice(-16), 'rendered keys', await bubbles.evaluateAll(nodes => nodes.map(node => node.dataset.messageKey)));
  await page.screenshot({ path: '/private/tmp/inbrix-pagination-failure.png' });
  throw error;
} finally {
  await browser.close();
}
