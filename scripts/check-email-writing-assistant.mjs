import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// All APIs are intercepted. This test cannot generate or send real mail.
const browser = await chromium.launch({ headless: true });
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const autoLanguage = Boolean(process.env.AI_TEST_AUTO_LANGUAGE);
const initialInstruction = autoLanguage ? '请回复客户，只提示查看报价附件，并翻译成中文' : '请用英文回复客户，只提示查看报价附件，并翻译成中文';
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
page.setDefaultTimeout(10000);
const accountEmail = 'me@example.com', now = new Date().toISOString();
const conversation = { id: 'writing', title: 'Alice', peerEmail: 'alice@example.com', subject: 'Quotation request', count: 1, date: now, accountEmail, status: 'unanswered' };
const source = { id: '2', folder: 'INBOX', from: conversation.peerEmail, to: accountEmail, subject: conversation.subject, body: 'Please send the quotation for our review.', mailSummary: { status: 'ready', text: '客户要求提供报价单供审核。' }, date: now, outgoing: false, messageId: '<request@example.com>', flags: ['\\Seen'] };
const first = { body: 'Hello Alice,\n\nPlease review the attached quotation.', translation: 'Alice，您好：\n\n请查看附件中的报价单。', message: '已按要求用英文回复，中文翻译仅供核对。' };
const revised = { body: 'Please see the attached quotation.', translation: '请查阅附件中的报价单。', message: '已缩短正文，并保留查看附件的提示。' };
const clarification = { body: '', translation: '', message: '这句话的意思是请客户查看附件中的报价单。' };
const requests = [], errors = [];
let draft = { id: 'draft', accountEmail, conversationId: conversation.id, mode: 'reply', to: source.from, cc: '', subject: `Re: ${source.subject}`, bodyHtml: `<p>Original editor draft</p><div data-inbrix-reply-quote=""><p>${source.body}</p></div>`, bodyText: 'Original editor draft', inReplyTo: source.messageId, references: source.messageId, updatedAt: now };
let attempt = 0;
page.on('pageerror', error => errors.push(error.stack));
await page.addInitScript(() => { window.EventSource = class { close() {} }; });
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route(/\/(?:api|v1)\//, async route => {
  const request = route.request(), url = new URL(request.url()), path = url.pathname;
  assert.notEqual(path, '/api/compose', 'Writing assistant must never send mail');
  let json = { calendar: false, notifications: false, models: [], agents: [], bindings: [], skills: [], attachments: [], documents: [], messages: [], total: 0 };
  if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail, conversations: [conversation], folders: [], accounts: [{ id: 'account', email: accountEmail, isActive: true }] };
  else if (path === '/api/conversations/writing') json = { conversation: { ...conversation, messages: [source] } };
  else if (path.includes('signatures')) json = { signatures: [{ id: 'sig', name: 'Company', html: '<p>Seller signature</p>', default: true }] };
  else if (path === '/api/drafts') {
    if (request.method() === 'POST') { draft = { ...request.postDataJSON(), id: 'draft', updatedAt: now }; json = { draft }; }
    else json = { drafts: draft ? [draft] : [] };
  } else if (path === '/api/ai/write-email') {
    const input = request.postDataJSON();
    requests.push(input);
    attempt++;
    assert.equal(input.responseFormat, 'structured');
    assert.equal(input.replyLanguage, autoLanguage ? 'auto' : 'en');
    if (attempt === 1) {
      assert.deepEqual(input.messages, []);
      assert.ok(input.draft.includes('Original editor draft'), 'First turn uses the actual editor draft');
      assert.ok(input.context.includes(source.body));
      assert.ok(input.context.includes(source.mailSummary.text), 'Summary is supplementary and must not replace the original email');
      assert.ok(!input.draft?.includes('Seller signature'), 'Signature must not enter candidate body');
      assert.ok(!input.draft?.includes(source.body), 'Quoted mail must not enter candidate body');
      json = first;
    } else {
      assert.equal(input.messages[0].content, initialInstruction);
      assert.equal(input.messages[1].body, first.body);
      assert.equal(input.messages[1].translation, first.translation);
      assert.equal(input.messages[1].content, first.message);
      if (attempt === 2) { assert.equal(input.draft, first.body); json = revised; }
      else if (attempt === 3) { await route.fulfill({ status: 422, json: { error: 'Temporary generation failure' } }); return; }
      else if (attempt === 4) { assert.equal(input.draft, revised.body); json = clarification; }
      else { assert.ok(input.draft.startsWith('Manual correction: '), 'Manual edits supersede the old AI candidate'); json = clarification; }
    }
  }
  await route.fulfill({ json });
});

const editor = page.getByTestId('compose-inline').locator('.tiptap');
const assistant = page.getByTestId('email-writing-assistant');
const ask = async text => {
  await assistant.getByLabel('补充说明', { exact: true }).fill(text);
  const response = page.waitForResponse(response => new URL(response.url()).pathname === '/api/ai/write-email').catch(error => error);
  await assistant.getByRole('button', { name: /^(生成|再次生成)$/ }).click();
  const result = await response;
  if (result instanceof Error) throw result;
};
try {
  await page.goto(`${process.env.COMPOSE_TEST_URL || 'http://localhost:2342'}/inbox?conversation=writing`);
  await page.getByTestId('conversation-detail').getByRole('button', { name: '回复', exact: true }).click();
  await editor.getByText('Seller signature', { exact: true }).waitFor();
  const before = await editor.innerText();
  await page.getByRole('button', { name: 'AI 编写邮件', exact: true }).click();
  if (!autoLanguage) {
    await assistant.getByRole('combobox', { name: '邮件语言', exact: true }).click();
    await page.getByRole('option', { name: 'English', exact: true }).click();
  }
  await ask(initialInstruction);
  await assistant.getByTestId('email-writing-body').getByText('Please review the attached quotation.', { exact: false }).waitFor();
  assert.equal(await editor.innerText(), before, 'Generation must leave the editor untouched');
  assert.equal(await assistant.getByTestId('email-writing-translation').innerText(), first.translation);
  await ask('再简短一点');
  await assistant.getByTestId('email-writing-body').last().getByText(revised.body, { exact: true }).waitFor();
  assert.equal(requests[1].messages.length, 2, 'Second turn includes the complete previous discussion');
  assert.equal(await editor.innerText(), before, 'Revisions still need manual adoption');

  // A failed turn restores the request and keeps the successful discussion.
  await ask('中文是什么意思？');
  await assistant.getByLabel('补充说明', { exact: true }).waitFor();
  await page.waitForFunction(() => document.querySelector('#ai-compose-instruction')?.value === '中文是什么意思？');
  await ask('中文是什么意思？');
  await assistant.getByTestId('email-writing-discussion').last().getByText(clarification.message, { exact: true }).waitFor();
  assert.equal(requests[3].messages.length, 4, 'Failed requests must not pollute writing history');
  assert.equal(await assistant.getByRole('button', { name: '采用', exact: true }).count(), 2, 'Discussion-only answers have no insert action');

  await assistant.screenshot({ path: `/tmp/inbrix-email-writing-${mobile ? 'mobile' : 'desktop'}.png` });
  const bounds = await assistant.boundingBox();
  assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= (mobile ? 390 : 1440), 'Popover fits the viewport');
  assert.ok(bounds.y >= 0 && bounds.y + bounds.height <= (mobile ? 844 : 1000), 'Popover height fits the viewport');
  await assistant.getByRole('button', { name: '采用', exact: true }).last().click();
  await assistant.waitFor({ state: 'hidden' });
  await editor.getByText(revised.body, { exact: true }).waitFor();
  const inserted = await editor.innerText();
  assert.ok(!inserted.includes(revised.translation) && !inserted.includes(revised.message) && !inserted.includes(clarification.message), 'Only the email body is inserted');
  assert.ok(inserted.includes('Seller signature'), 'Adopting preserves the editor signature');
  assert.ok((await editor.textContent()).includes(source.body), 'Adopting preserves quoted mail');
  await page.getByRole('button', { name: 'AI 编写邮件', exact: true }).click();
  assert.equal(await assistant.getByTestId('email-writing-answer').count(), 3, 'Reopening preserves the writing discussion');
  await page.getByRole('button', { name: 'AI 编写邮件', exact: true }).click();
  await editor.press('Control+Home');
  await editor.pressSequentially('Manual correction: ');
  await page.getByRole('button', { name: 'AI 编写邮件', exact: true }).click();
  await ask('根据我手动修改后的正文继续核对');
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="email-writing-answer"]').length === 4);
  assert.deepEqual(errors, []);
  console.log(`Passed (${mobile ? 'mobile' : 'desktop'}): multi-turn history, separate reply language, translation, discussion, failure recovery, manual body-only adoption, signature and quote preservation.`);
} catch (error) {
  console.error('Browser errors:', errors);
  console.error('Writing requests:', requests.length);
  console.error(await page.locator('body').innerText());
  await page.screenshot({ path: '/tmp/inbrix-email-writing-failure.png' });
  throw error;
} finally {
  await browser.close();
}
