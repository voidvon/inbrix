import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// All APIs are intercepted; these checks never generate real replies or send mail.
const browser = await chromium.launch({ headless: true });
const base = process.env.REPLY_TEST_URL || 'http://127.0.0.1:2342';
const accountEmail = 'me@example.com';
const summary = { id: 'reply', title: 'Alice', peerEmail: 'alice@example.com', subject: 'Reply settings', count: 1, accountEmail, status: 'unanswered' };
const message = {
  id: '1', folder: 'INBOX', from: summary.peerEmail, to: accountEmail,
  subject: summary.subject, body: 'Please confirm the quantity.', date: new Date().toISOString(), outgoing: false,
  suggestedReply: { text: 'Previously saved reply.', status: 'ready' },
};

try {
  for (const mode of ['disabled', 'enabled', 'default']) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = [];
    let generations = 0;
    let releaseBindings;
    const bindingsReady = new Promise(resolve => { releaseBindings = resolve; });
    const bindings = [{ accountEmail: 'other@example.com', taskType: 'reply_suggestion', enabled: false }];
    if (mode !== 'default') bindings.push({ accountEmail, taskType: 'reply_suggestion', enabled: mode === 'enabled' });
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(() => { window.EventSource = class { close() {} }; });
    await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
    await page.route('**/api/**', async route => {
      const { pathname } = new URL(route.request().url());
      let json = { models: [], agents: [], bindings: [], documents: [], signatures: [], attachments: [], drafts: [], total: 0 };
      if (pathname === '/api/conversations') json = { locale: 'zh-CN', accountEmail, conversations: [summary], folders: [], accounts: [{ id: 'account', email: accountEmail, isActive: true }] };
      if (pathname === '/api/conversations/reply') json = { conversation: { ...summary, messages: [message], hasMore: false, hasNewer: false } };
      if (pathname === '/api/settings/ai/models') json = { models: [{ id: 'model', model: 'test-model' }] };
      if (pathname === '/api/settings/ai/task-bindings') {
        await bindingsReady;
        json = { bindings };
      }
      if (pathname === '/api/ai/write-email') {
        generations++;
        json = { body: 'New generated reply.', persisted: true };
      }
      await route.fulfill({ json });
    });
    await page.goto(`${base}/inbox?conversation=reply`);
    const detail = page.getByTestId('conversation-detail');
    await detail.getByText(message.suggestedReply.text, { exact: true }).waitFor();
    const generate = detail.getByRole('button', { name: '生成回复', exact: true });
    const regenerate = detail.getByRole('button', { name: '重新生成建议回复', exact: true });
    assert.equal(await generate.count(), 0, 'Generation must wait for mailbox settings');
    assert.equal(await regenerate.count(), 0, 'Regeneration must wait for mailbox settings');
    const bindingsResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/settings/ai/task-bindings');
    releaseBindings();
    await bindingsResponse;
    if (mode === 'disabled') {
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.equal(await generate.count(), 0, 'Disabled mailbox must hide generation');
      assert.equal(await regenerate.count(), 0, 'Disabled mailbox must hide regeneration');
      assert.equal(generations, 0, 'Disabled mailbox must not request generation');
      assert.equal(await detail.getByText(message.suggestedReply.text, { exact: true }).count(), 1, 'Previously saved replies stay available');
    } else {
      await generate.waitFor();
      await regenerate.click();
      await detail.getByText('New generated reply.', { exact: true }).waitFor();
      assert.equal(generations, 1, 'Enabled and default mailboxes can regenerate replies');
    }
    assert.deepEqual(errors, []);
    await page.close();
  }
  console.log('Reply generation respects mailbox settings, waits for configuration, and preserves saved replies.');
} finally {
  await browser.close();
}
