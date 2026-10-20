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
try {
  await page.goto(`${base}/inbox`);
  await chooseRecipient('new@example.com');
  assert.equal(conversations.length, 2);
  const newId = new URL(page.url()).searchParams.get('conversation');
  assert.ok(newId.startsWith('new-'));
  assert.equal(await page.getByTestId('conversation-detail').locator('[data-testid="compose-inline"]').count(), 1);
  await inline.getByLabel('主题', { exact: true }).fill('New subject');
  await inline.locator('.tiptap').fill('Draft body');
  await inline.getByRole('button', { name: '关闭', exact: true }).click();
  await page.getByTestId('thread-draft-entry').waitFor();
  await page.getByTestId('thread-draft-entry').getByText('Draft body', { exact: true }).waitFor();
  assert.ok(await page.getByTestId('conversation-note-entry').evaluate(note => Boolean(document.querySelector('[data-testid="thread-draft-entry"]').compareDocumentPosition(note) & Node.DOCUMENT_POSITION_FOLLOWING)), 'The note stays below a saved draft');
  await page.getByTestId('conversation-row').filter({ hasText: 'new@example.com' }).getByTestId('conversation-status').getByText('草稿', { exact: true }).waitFor();
  assert.equal(drafts.get(newId).bodyText, 'Draft body');
  assert.equal(drafts.get(newId).mode, 'new');
  await page.reload();
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
  await inline.locator('.tiptap').fill('A new email in the existing conversation');
  await inline.getByRole('button', { name: '发送', exact: true }).click();
  await inline.waitFor({ state: 'hidden' });
  await page.getByTestId('conversation-row').filter({ hasText: 'alice@example.com' }).getByRole('button', { name: '未回复', exact: true }).waitFor();
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
  assert.deepEqual(errors, []);
  console.log('Passed: new and existing recipients, draft restore and status, send clears draft status, cross-page navigation, mobile.');
} finally {
  await browser.close();
}
