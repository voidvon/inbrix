// All API calls are intercepted; this never reads or changes real mail or documents.
import assert from 'node:assert/strict';
import { chromium, webkit, devices } from 'playwright';
const engine = process.env.MOTION_BROWSER === 'webkit' ? webkit : chromium;
const browser = await engine.launch({ headless: true });
const page = await browser.newPage(engine === webkit ? devices['iPhone 13'] : devices['Pixel 7']);
page.setDefaultTimeout(10000);
const errors = [];
page.on('pageerror', e => errors.push(e.message));
await page.addInitScript(() => {
    window.EventSource = class {
        close() { }
    };
    window.motionEvents = [];
    document.addEventListener('animationstart', event => {
        if (!event.animationName.startsWith('mobile-page-'))
            return;
        window.motionEvents.push({ name: event.animationName, target: event.target });
        for (const animation of event.target.getAnimations())
            if (animation.animationName === event.animationName) {
                animation.pause();
                animation.currentTime = 90;
            }
    });
});
const summary = { id: 'motion', title: 'Alice', peerEmail: 'alice@example.com', subject: 'Motion check', count: 1, unreadCount: 0, accountEmail: 'me@example.com', status: 'unanswered' };
const message = { id: '1', folder: 'INBOX', from: summary.peerEmail, to: summary.accountEmail, subject: summary.subject, body: 'Motion email body', date: new Date().toISOString(), flags: ['\\Seen'], outgoing: false };
const record = { id: 'motion-doc', name: 'Motion document', type: 'quotation', html: '<h1>Motion document</h1><p>Document body</p>', updatedAt: new Date().toISOString() };
let releaseDetail;
const gate = new Promise(resolve => { releaseDetail = resolve; });
await page.route('**/csrf', r => r.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    let json = { calendar: false, models: [], agents: [], bindings: [], documents: [], templates: [], attachments: [], signatures: [], drafts: [], sessions: [], messages: [], total: 0 };
    if (path === '/api/conversations')
        json = { locale: 'zh-CN', accountEmail: summary.accountEmail, conversations: [summary], folders: [], accounts: [{ id: 'account', email: summary.accountEmail, isActive: true }] };
    if (path === '/api/conversations/motion') {
        await gate;
        json = { conversation: { ...summary, messages: [message], hasMore: false, hasNewer: false } };
    }
    if (path === '/api/documents/motion-doc')
        json = { document: record };
    await route.fulfill({ json });
});
const base = process.env.MOTION_TEST_URL || 'http://127.0.0.1:2342';
async function checkMotion(locator, label) {
    await locator.waitFor();
    await page.waitForFunction(element => element.getAnimations().some(animation => animation.animationName === 'mobile-page-enter' && animation.playState === 'paused'), await locator.elementHandle());
    const during = await locator.evaluate(element => ({ x: element.getBoundingClientRect().x, width: innerWidth, animation: getComputedStyle(element).animationName }));
    assert.ok(during.x > 0 && during.x < during.width, `${label} slides from right: ${JSON.stringify(during)}`);
    await locator.evaluate(element => element.getAnimations().forEach(a => a.finish()));
    await page.waitForFunction(element => getComputedStyle(element).translate === 'none', await locator.elementHandle());
    assert.equal(await locator.evaluate(element => getComputedStyle(element).transform), 'none');
    console.log('Passed', label);
}
async function checkExit(locator, label) {
    await page.waitForFunction(element => element.getAnimations().some(animation => animation.animationName === 'mobile-page-exit' && animation.playState === 'paused'), await locator.elementHandle());
    assert.equal(await locator.isVisible(), true, `${label} stays mounted during exit`);
    assert.equal(await locator.getAttribute('inert'), '');
    const bounds = await locator.evaluate(element => ({ x: element.getBoundingClientRect().x, width: innerWidth }));
    assert.ok(bounds.x > 0 && bounds.x < bounds.width, `${label} slides to the right: ${JSON.stringify(bounds)}`);
    console.log('Passed exit', label);
}
async function finishExit(locator) { await locator.evaluate(element => element.getAnimations().forEach(animation => animation.finish())); }
try {
    await page.goto(`${base}/inbox`);
    await page.getByTestId('conversation-row').first().click();
    const shell = page.locator('section.mobile-page-enter');
    await checkMotion(shell, 'Mail detail');
    const handle = await shell.elementHandle();
    releaseDetail();
    await page.getByTestId('conversation-detail').waitFor();
    assert.equal(await shell.evaluate((element, original) => element === original, handle), true);
    assert.equal(await shell.evaluate(element => window.motionEvents.filter(e => e.target === element).length), 1, 'Loading must not restart animation');
    await page.getByRole('button', { name: 'AI对话', exact: true }).click();
    await checkMotion(page.locator('.mobile-page-enter.app-viewport'), 'Mail AI');
    const ai = page.locator('.mobile-page-enter.app-viewport');
    await page.getByRole('button', { name: '关闭 AI 对话', exact: true }).click();
    await checkExit(ai, 'Mail AI');
    assert.equal(await page.getByTestId('conversation-detail').isVisible(), true, 'Mail stays behind AI during exit');
    await finishExit(ai);
    await ai.waitFor({ state: 'detached' });
    const detail = page.getByTestId('conversation-detail');
    await detail.locator('header button').first().click();
    await checkExit(shell, 'Mail detail');
    assert.equal(await detail.isVisible(), true, 'Retain the email until its exit finishes');
    assert.equal(new URL(page.url()).searchParams.get('conversation'), 'motion', 'Retain selection until exit finishes');
    assert.equal(await page.getByTestId('conversation-list').isVisible(), true, 'Reveal list behind exiting mail');
    await finishExit(shell);
    await detail.waitFor({ state: 'detached' });
    assert.equal(new URL(page.url()).searchParams.get('conversation'), null);
    // Reopening and closing during the entrance must not strand the page.
    await page.getByTestId('conversation-row').first().click();
    await page.waitForFunction(() => document.querySelector('section.mobile-page-enter').getAnimations().some(a => a.playState === 'paused'));
    await detail.locator('header button').first().click();
    await checkExit(shell, 'Interrupted mail entrance');
    await finishExit(shell);
    await detail.waitFor({ state: 'detached' });
    await page.goto(`${base}/ai-document`);
    await checkMotion(page.locator('.mobile-page-enter'), 'Document AI');
    const docAI = page.locator('.mobile-page-enter');
    await page.getByTestId('ai-document-chat-panel').getByRole('button', { name: '返回列表', exact: true }).click();
    await checkExit(docAI, 'Document AI');
    assert.equal(await page.getByRole('button', { name: 'AI 起草', exact: true }).isVisible(), true, 'Reveal document list during AI exit');
    await finishExit(docAI);
    await docAI.waitFor({ state: 'detached' });
    await page.goto(`${base}/documents/motion-doc`);
    const editor = page.getByTestId('document-editor-dialog');
    await checkMotion(editor, 'Document editor');
    const fixed = await editor.evaluate(element => { const probe = document.createElement('div'); probe.style.cssText = 'position:fixed;left:37px;top:41px;width:1px;height:1px'; element.appendChild(probe); const r = probe.getBoundingClientRect(); probe.remove(); return { x: r.x, y: r.y }; });
    assert.deepEqual(fixed, { x: 37, y: 41 }, 'Canvas menus must retain viewport coordinates');
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => document.querySelector('[data-testid="document-editor-dialog"][data-closed]'));
    await page.waitForFunction(element => element.getAnimations().some(a => a.animationName === 'mobile-page-exit' && a.playState === 'paused'), await editor.elementHandle());
    assert.equal(await editor.evaluate(e => getComputedStyle(e).animationName), 'mobile-page-exit');
    await editor.evaluate(e => e.getAnimations().forEach(a => a.finish()));
    await editor.waitFor({ state: 'detached' });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto(`${base}/documents/motion-doc`);
    await editor.waitFor();
    assert.equal(await editor.evaluate(e => getComputedStyle(e).animationName), 'none');
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    assert.notEqual(await editor.evaluate(e => getComputedStyle(e).animationName), 'mobile-page-enter');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto(`${base}/inbox?conversation=motion`);
    await page.getByTestId('conversation-detail').waitFor();
    await page.getByTestId('conversation-detail').locator('header button').first().click();
    await page.getByTestId('conversation-detail').waitFor({ state: 'detached' });
    assert.equal(await page.evaluate(() => window.motionEvents.length), 0, 'Reduced motion skips entering and exiting');
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.goto(`${base}/ai-document`);
    const changing = page.locator('.mobile-page-enter');
    await checkMotion(changing, 'AI before viewport change');
    await page.getByTestId('ai-document-chat-panel').getByRole('button', { name: '返回列表', exact: true }).click();
    await checkExit(changing, 'AI while viewport changes');
    await page.setViewportSize({ width: 1440, height: 1000 });
    await changing.waitFor({ state: 'detached' });
    assert.deepEqual(errors, []);
    console.log('Passed all mobile exits, interrupted entrance, reduced motion, viewport changes, menu coordinates and desktop presentation.');
}
catch (error) {
    console.error(errors);
    throw error;
}
finally {
    releaseDetail();
    await browser.close();
}
