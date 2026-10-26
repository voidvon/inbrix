import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createAIChatTestStore } from './ai-chat-test-store.mjs';

// All mailbox, AI and draft APIs are intercepted. This test cannot send real mail.
const browser = await chromium.launch({ headless: true });
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const base = process.env.COMPOSE_TEST_URL || 'http://localhost:2342';
const accountEmail = 'me@example.com', now = new Date().toISOString();
const store = createAIChatTestStore(accountEmail);
const conversation = { id: 'ai-reply', title: 'Alice', peerEmail: 'alice@example.com', subject: 'Account recovered', count: 2, date: now, accountEmail, status: 'unanswered' };
const original = { id: '2', folder: 'INBOX', from: 'alice@example.com', to: accountEmail, subject: 'Account recovered', body: 'Original recovery notice', date: now, outgoing: false, messageId: '<original@example.com>', flags: ['\\Seen'] };
// Same UID in another folder must never be mistaken for the requested source.
const latest = { ...original, folder: 'Sent', from: accountEmail, to: 'other@example.com', subject: 'Different message', body: 'Latest message', outgoing: true, messageId: '<other@example.com>' };
const firstBody = '您好：\n\n感谢，我已想起密码，不需要重置密码了。';
const revisedBody = '感谢提醒，我已想起密码，无需重置。';
const explanation = '回复正文已准备好。这里是解释，不应写入邮件。';
const tool = { id: 'reply-call', name: 'prepare_email_reply', status: 'success', summary: '已准备回复正文（未发送）' };
let draft, aroundReads = 0;
const requests = [], errors = [];
page.on('pageerror', error => errors.push(error.stack));
await page.addInitScript(() => { window.EventSource = class { close() {} }; });
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const request = route.request(), url = new URL(request.url()), path = url.pathname;
  assert.notEqual(path, '/api/compose', 'Preparing an AI reply must never send mail');
  let json = { calendar: false, notifications: false, models: [], agents: [], skills: [], bindings: [], documents: [], signatures: [], attachments: [], brands: [], total: 0 };
  if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail, conversations: [conversation], folders: [], accounts: [{ id: 'account', email: accountEmail, isActive: true }] };
  else if (path.endsWith('/ai-chat')) json = { ...store.projectSession(store.mail, url), sessionId: store.mail.id };
  else if (path === '/api/conversations/ai-reply') {
    if (url.searchParams.has('around')) {
      assert.equal(url.searchParams.get('around'), original.id);
      assert.equal(url.searchParams.get('folder'), original.folder);
      aroundReads++;
      json = { conversation: { ...conversation, messages: [original, latest] } };
    } else json = { conversation: { ...conversation, messages: [latest], hasMore: true } };
  } else if (path === '/api/drafts') {
    if (request.method() === 'POST') {
      draft = { ...request.postDataJSON(), id: 'reply-draft', updatedAt: now };
      json = { draft };
    } else json = { drafts: draft ? [draft] : [] };
  } else if (path === '/api/ai/agent-chat') {
    const payload = request.postDataJSON();
    requests.push(payload);
    store.begin(payload);
    if (requests.length === 1) json = store.complete(payload, { content: '这是讨论，无需打开编辑器。', toolCalls: [] });
    else if (requests.length === 2) {
      store.fail(payload);
      const frame = event => `data: ${JSON.stringify(event)}\n\n`;
      await route.fulfill({ contentType: 'text/event-stream', body: frame({ type: 'tool_end', tool }) + frame({ type: 'error', error: 'Interrupted after preparing a reply' }) });
      return;
    } else json = store.complete(payload, { content: explanation, toolCalls: [tool], emailDraft: { replyTo: { uid: original.id, folder: original.folder }, bodyText: requests.length === 3 ? firstBody : revisedBody } });
  }
  await route.fulfill({ json });
});

const compose = page.getByTestId('compose-inline');
const chat = page.locator('[data-ai-message-role="assistant"]:visible');
const openAI = async () => {
  const input = page.getByRole('textbox', { name: '询问邮件、草稿或产品资料… (Enter 发送)', exact: true });
  if (!await input.isVisible()) await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await input.waitFor();
  return input;
};
const ask = async question => {
  const input = await openAI();
  await input.fill(question);
  const response = page.waitForResponse(response => new URL(response.url()).pathname === '/api/ai/agent-chat');
  await input.press('Enter');
  await response;
};
try {
  await page.goto(`${base}/inbox?conversation=${conversation.id}`);
  await ask('解释一下这封邮件');
  await chat.getByText('这是讨论，无需打开编辑器。', { exact: true }).waitFor();
  assert.equal(await compose.count(), 0, 'Discussion alone must not open an editor');

  await ask('生成回复，但模拟中断');
  await page.getByRole('alert').filter({ hasText: '回复未完成' }).waitFor();
  assert.equal(await compose.count(), 0, 'A tool event without a completed turn must not fill the editor');

  await ask('帮我编辑回复邮件，感谢，我已想起密码，不需要重置密码了');
  await compose.locator('.tiptap').getByText('感谢，我已想起密码，不需要重置密码了。', { exact: true }).waitFor();
  assert.equal(await compose.getByLabel('主题', { exact: true }).inputValue(), 'Re: Account recovered');
  assert.ok(aroundReads > 0, 'The exact reply source outside the loaded transcript is fetched');
  assert.ok(!(await compose.locator('.tiptap').innerText()).includes(explanation), 'Only structured email text is inserted');
  if (mobile) await page.getByRole('textbox', { name: '询问邮件、草稿或产品资料… (Enter 发送)', exact: true }).waitFor({ state: 'hidden' });

  await compose.getByLabel('主题', { exact: true }).fill('My edited subject');
  await ask('再简短一点');
  await compose.locator('.tiptap').getByText(revisedBody, { exact: true }).waitFor();
  assert.equal(requests.at(-1).activeDraft.subject, 'My edited subject', 'The agent sees the live unsaved email draft');
  assert.equal(await compose.getByLabel('主题', { exact: true }).inputValue(), 'My edited subject', 'Revising the body preserves subject edits');
  if (!mobile) await page.getByRole('button', { name: '关闭 AI 对话', exact: true }).click();
  const saved = page.waitForResponse(response => new URL(response.url()).pathname === '/api/drafts' && response.request().method() === 'POST');
  await compose.getByRole('button', { name: '关闭', exact: true }).click();
  await saved;
  await compose.waitFor({ state: 'hidden' });
  assert.equal(draft.to, original.from);
  assert.equal(draft.inReplyTo, original.messageId);
  assert.ok(draft.bodyText.includes(revisedBody));
  assert.ok(!draft.bodyText.includes(explanation));

  await page.reload();
  await openAI();
  await chat.getByText(explanation, { exact: true }).last().waitFor();
  assert.equal(await compose.count(), 0, 'Loading reply history must never replay an old editor action');
  await page.getByRole('button', { name: '写入草稿', exact: true }).last().click();
  await compose.locator('.tiptap').getByText(revisedBody, { exact: true }).waitFor();
  assert.ok(!(await compose.locator('.tiptap').innerText()).includes(explanation), 'Manual reuse also inserts only the structured reply');
  assert.deepEqual(errors, []);
  console.log(`Passed (${mobile ? 'mobile' : 'desktop'}): automatic reply editor, exact paginated source, body-only insertion, revision, saved reply headers, no history replay, interrupted turn and no sending.`);
} catch (error) {
  console.error(errors);
  console.error(await page.locator('body').innerText().catch(() => 'Page unavailable'));
  throw error;
} finally {
  await browser.close();
}
