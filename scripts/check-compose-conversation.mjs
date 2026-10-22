import { chromium } from 'playwright';
import assert from 'node:assert/strict';

// Browser regression for recipient selection and composing inside a transcript.
// Every API is intercepted; no email is sent.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const accountEmail = 'me@example.com';
const now = new Date().toISOString();
const existing = { id: 'existing', title: 'Alice', peerEmail: 'alice@example.com', subject: 'Request', preview: 'Original message', date: now, count: 1, unreadCount: 0, accountEmail, status: 'unanswered' };
const message = { id: '1', from: existing.peerEmail, to: accountEmail, subject: existing.subject, body: existing.preview, preview: existing.preview, date: now, outgoing: false, messageId: '<original@example.com>', flags: ['\\Seen'] };
const conversations = [{ ...existing, messages: [message] }];
const drafts = new Map();
const aiChats = new Map();
const aiRequests = [];
const aiReply = '已核对 TD62LM 产品资料，请进一步确认入口压力和背压。';
const errors = [];
let sent = '';
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const request = route.request();
  const url = new URL(request.url());
  const path = url.pathname;
  let json = {};
  if (path === '/api/conversations' && request.method() === 'POST') {
    const { to } = request.postDataJSON();
    let conversation = conversations.find(item => item.peerEmail === to.toLowerCase());
    if (!conversation) {
      conversation = { ...existing, id: `new-${conversations.length}`, title: to, peerEmail: to, subject: '', preview: '', count: 0, messages: [] };
      conversations.unshift(conversation);
      drafts.set(conversation.id, { id: `draft-${conversation.id}`, accountEmail, conversationId: conversation.id, mode: 'new', to, cc: '', subject: '', bodyHtml: '', bodyText: '', inReplyTo: '', references: '', updatedAt: now });
    }
    json = { conversation };
  } else if (path === '/api/conversations') {
    json = { locale: 'zh-CN', accountEmail, conversations: conversations.map(conversation => ({ ...conversation, status: drafts.has(conversation.id) ? 'draft' : conversation.status })), folders: [], accounts: [{ id: 'account', email: accountEmail, label: accountEmail, isActive: true }] };
  } else if (path.endsWith('/ai-chat')) {
    const id = path.split('/')[3];
    if (request.method() === 'PUT') aiChats.set(id, request.postDataJSON().messages);
    json = { ok: true, messages: aiChats.get(id) || [] };
  } else if (path === '/api/ai/agent-chat') {
    aiRequests.push(request.postDataJSON());
    json = { content: aiReply, toolCalls: [{ name: 'search_products', status: 'success', summary: 'TD62LM' }, { name: 'read_product', status: 'success', summary: '使用限制' }] };
  } else if (path.startsWith('/api/conversations/')) {
    json = { conversation: conversations.find(item => item.id === path.split('/')[3]) };
  } else if (path === '/api/drafts' && request.method() === 'POST') {
    const input = request.postDataJSON();
    const draft = { inReplyTo: '', references: '', cc: '', ...input, id: input.id || `draft-${input.conversationId}`, updatedAt: now };
    drafts.set(input.conversationId, draft);
    json = { draft };
  } else if (path === '/api/drafts') {
    json = { drafts: [...drafts.values()].filter(draft => !url.searchParams.get('conversation_id') || draft.conversationId === url.searchParams.get('conversation_id')) };
  } else if (path.startsWith('/api/drafts/') && request.method() === 'DELETE') {
    for (const [id, draft] of drafts) if (draft.id === path.split('/').at(-1)) drafts.delete(id);
  } else if (path === '/api/compose') {
    sent = request.postDataBuffer().toString();
    json = { success: true };
  } else if (path.includes('signatures')) json = { signatures: [] };
  else json = { calendar: false, notifications: false, models: [], agents: [], bindings: [], documents: [], attachments: [], total: 0 };
  await route.fulfill({ json });
});

const base = process.env.COMPOSE_TEST_URL || 'http://localhost:2342';
const inline = page.getByTestId('compose-inline');
const recipientDialog = page.getByTestId('start-conversation-dialog');
async function chooseRecipient(email) {
  await page.getByTestId('compose-button').click();
  await recipientDialog.waitFor();
  assert.equal(await page.getByTestId('compose-bubble').count(), 0);
  await recipientDialog.getByLabel('收件人', { exact: true }).fill(email);
  await recipientDialog.getByRole('button', { name: '开始写邮件', exact: true }).click();
  await recipientDialog.waitFor({ state: 'hidden' });
  await inline.waitFor();
  await inline.locator('.tiptap').waitFor();
  assert.equal(await inline.getByText('收件人', { exact: true }).count(), 0, 'The recipient is chosen before composing and is hidden in the draft');
  assert.ok(await page.getByTestId('conversation-note-entry').evaluate(note => {
    const compose = document.querySelector('[data-testid="compose-inline"]');
    return compose.parentElement === note.parentElement && Boolean(compose.compareDocumentPosition(note) & Node.DOCUMENT_POSITION_FOLLOWING) && getComputedStyle(note).position === 'static';
  }), 'The note follows the compose editor inside the same scrolling transcript');
}
async function askDraftAI(question) {
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  const input = page.getByRole('textbox', { name: '询问邮件、草稿或产品资料… (Enter 发送)', exact: true });
  await input.fill(question);
  const saved = page.waitForResponse(response => response.url().endsWith('/ai-chat') && response.request().method() === 'PUT');
  await input.press('Enter');
  await saved;
  await page.getByRole('button', { name: '写入草稿', exact: true }).last().waitFor();
  await page.locator('span:visible').filter({ hasText: /^检索产品资料$/ }).last().waitFor();
  await page.locator('span:visible').filter({ hasText: /^读取产品规格$/ }).last().waitFor();
}
try {
  await page.goto(`${base}/inbox`);
  await chooseRecipient('new@example.com');
  assert.equal(conversations.length, 2);
  const newId = new URL(page.url()).searchParams.get('conversation');
  assert.ok(newId.startsWith('new-'));
  assert.equal(await page.getByTestId('conversation-detail').locator('[data-testid="compose-inline"]').count(), 1);
  await inline.getByLabel('主题', { exact: true }).fill('New subject');
  await inline.locator('.tiptap').fill('Draft body');
  assert.equal(await page.getByTestId('conversation-detail').getByRole('button', { name: '回复', exact: true }).count(), 0, 'A draft-only conversation has AI chat without reply controls');
  await askDraftAI('核对草稿里的产品');
  assert.equal(aiRequests.at(-1).conversationId, newId);
  assert.deepEqual(aiRequests.at(-1).activeDraft, { to: 'new@example.com', cc: '', subject: 'New subject', bodyText: 'Draft body' });
  assert.equal(aiChats.get(newId).length, 2, 'Draft-only AI discussion must persist');
  await page.getByRole('button', { name: '写入草稿', exact: true }).click();
  await inline.locator('.tiptap').getByText(aiReply, { exact: true }).waitFor();
  assert.equal(await inline.getByLabel('主题', { exact: true }).inputValue(), 'New subject', 'AI insertion preserves the unsaved subject');
  assert.equal(sent, '', 'Writing AI output into a draft must not send email');
  await inline.locator('.tiptap').fill('Draft body');
  await page.getByRole('button', { name: '关闭 AI 对话', exact: true }).click();
  await inline.getByRole('button', { name: '关闭', exact: true }).click();
  await page.getByTestId('thread-draft-entry').waitFor();
  await page.getByTestId('thread-draft-entry').getByText('Draft body', { exact: true }).waitFor();
  assert.ok(await page.getByTestId('conversation-note-entry').evaluate(note => Boolean(document.querySelector('[data-testid="thread-draft-entry"]').compareDocumentPosition(note) & Node.DOCUMENT_POSITION_FOLLOWING)), 'The note stays below a saved draft');
  await page.getByTestId('conversation-row').filter({ hasText: 'new@example.com' }).getByTestId('conversation-status').getByText('草稿', { exact: true }).waitFor();
  assert.equal(drafts.get(newId).bodyText, 'Draft body');
  assert.equal(drafts.get(newId).mode, 'new');
  await page.reload();
  await page.getByTestId('thread-draft-entry').waitFor();
  await askDraftAI('继续核对已保存的草稿');
  assert.equal(aiRequests.at(-1).activeDraft.bodyText, 'Draft body');
  assert.equal(aiRequests.at(-1).messages.length, 3, 'Restored discussion includes the previous turn');
  await page.getByRole('button', { name: '写入草稿', exact: true }).last().click();
  await inline.locator('.tiptap').getByText(aiReply, { exact: true }).waitFor();
  assert.equal(await inline.getByLabel('主题', { exact: true }).inputValue(), 'New subject');
  await inline.locator('.tiptap').fill('Draft body');
  await page.getByRole('button', { name: '关闭 AI 对话', exact: true }).click();
  await inline.getByRole('button', { name: '关闭', exact: true }).click();
  await page.getByTestId('thread-draft-entry').getByRole('button').click();
  await inline.waitFor();
  await inline.locator('.tiptap').getByText('Draft body').waitFor();
  assert.equal(await inline.getByLabel('主题', { exact: true }).inputValue(), 'New subject');
  await inline.getByRole('button', { name: '关闭', exact: true }).click();
  await page.screenshot({ path: '/tmp/inbrix-draft-status-light.png' });
  await page.evaluate(() => document.documentElement.classList.add('dark'));
  await page.screenshot({ path: '/tmp/inbrix-draft-status-dark.png' });
  await page.evaluate(() => document.documentElement.classList.remove('dark'));
  await chooseRecipient('new@example.com');
  assert.equal(conversations.length, 2, 'Reopening the same recipient must reuse the conversation');
  await inline.locator('.tiptap').getByText('Draft body').waitFor();
  await inline.getByRole('button', { name: '关闭', exact: true }).click();
  await chooseRecipient('ALICE@example.com');
  assert.equal(new URL(page.url()).searchParams.get('conversation'), existing.id);
  await page.getByTestId('conversation-detail').getByText('Original message', { exact: true }).waitFor({ state: 'attached' });
  await inline.getByLabel('主题', { exact: true }).fill('New message to Alice');
  await inline.locator('.tiptap').fill('A new email in the existing conversation');
  await inline.getByRole('button', { name: '发送', exact: true }).click();
  await inline.waitFor({ state: 'hidden' });
  await page.getByTestId('conversation-row').filter({ hasText: 'alice@example.com' }).getByRole('button', { name: '待回复', exact: true }).waitFor();
  assert.ok(sent.includes('name="conversation_id"\r\n\r\nexisting'));
  assert.ok(sent.includes('name="to"\r\n\r\nalice@example.com'));
  assert.ok(!sent.includes('name="in_reply_to"'), 'A new email must not acquire reply headers');

  // Replies also hide the recipient; Cc remains editable.
  await page.getByTestId('conversation-detail').getByRole('button', { name: '回复', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('#compose-subject')?.value === 'Re: Request');
  assert.equal(await inline.getByText('收件人', { exact: true }).count(), 0);
  await inline.getByRole('textbox', { name: '抄送', exact: true }).fill('cc@example.com');
  await inline.locator('.tiptap').fill('Reply with a fixed recipient');
  await inline.getByRole('button', { name: '发送', exact: true }).click();
  await inline.waitFor({ state: 'hidden' });
  assert.ok(sent.includes('name="to"\r\n\r\nalice@example.com'));
  assert.ok(sent.includes('name="cc"\r\n\r\ncc@example.com'));
  assert.ok(sent.includes('name="in_reply_to"\r\n\r\n<original@example.com>'));

  // Starting from another page must navigate to the inbox and open the draft.
  await page.goto(`${base}/attachments`);
  await chooseRecipient('new@example.com');
  assert.equal(new URL(page.url()).pathname, '/inbox');
  await inline.locator('.tiptap').getByText('Draft body').waitFor();
  await inline.getByRole('button', { name: '关闭', exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await page.getByTestId('thread-draft-entry').getByRole('button').click();
  await inline.waitFor();
  await inline.locator('.tiptap').getByText('Draft body').waitFor();
  assert.equal(await inline.isVisible(), true, 'Mobile transcript must show the draft');
  const previousSend = sent;
  await askDraftAI('手机端核对产品资料');
  assert.equal(aiRequests.at(-1).activeDraft.bodyText, 'Draft body');
  await page.getByRole('button', { name: '写入草稿', exact: true }).last().click();
  await inline.locator('.tiptap').getByText(aiReply, { exact: true }).waitFor();
  assert.equal(await inline.isVisible(), true, 'Mobile AI insertion returns to the draft');
  assert.equal(sent, previousSend);
  assert.deepEqual(errors, []);
  console.log('Passed: draft-only AI chat and history, live/saved draft context, product tool labels, AI draft insertion, recipient/reply/send flows, cross-page navigation and mobile.');
} finally {
  await browser.close();
}
