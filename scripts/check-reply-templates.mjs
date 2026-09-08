import { chromium } from 'playwright';
import assert from 'node:assert/strict';

// All API calls, including send, are intercepted: no real messages are sent.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1600, height: 1100 } });
await page.addInitScript(() => {
  const NativeFile = window.File;
  window.File = class extends NativeFile {
    constructor(...args) { super(...args); if (this.type === 'application/pdf') (window.testPDFSizes ||= []).push(this.size); }
  };
});
const errors = [];
page.on('pageerror', error => errors.push(error.stack));
const summary = { id: 'test-thread', title: 'Alice', peerEmail: 'alice@example.com', subject: 'Quotation request', preview: 'Please quote these items', date: new Date().toISOString(), count: 1, unreadCount: 0, status: 'unanswered' };
const message = { id: '1', folder: 'INBOX', from: 'alice@example.com', to: 'test@example.com', subject: summary.subject, body: summary.preview, preview: summary.preview, date: summary.date, outgoing: false, messageId: '<request@example.com>', flags: ['\\Seen'] };
let sent = null;
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const path = new URL(route.request().url()).pathname;
  if (path === '/api/compose') {
    sent = route.request().postDataBuffer();
    return route.fulfill({ json: { success: true } });
  }
  const json = path === '/api/conversations' ? { locale: 'zh-CN', conversations: [summary], folders: [], accounts: [], accountEmail: 'test@example.com' }
    : path === '/api/conversations/test-thread' ? { conversation: { ...summary, accountEmail: 'test@example.com', messages: [message] } }
    : path.includes('signatures') ? { signatures: [] }
    : { calendar: false, notifications: false, webPush: false, models: [], agents: [], bindings: [] };
  await route.fulfill({ json });
});
const base = process.env.DOCUMENT_TEST_URL || 'http://localhost:2342';
await page.route('**/v1/settings/signatures', route => route.fulfill({ json: { signatures: [
  { id: 'test-signature', name: 'Test signature', html: '<p>Best regards, Test Sender</p>', default: true },
] } }));
try {
  await page.goto(`${base}/inbox?conversation=test-thread`);
  async function openReply() {
    await page.getByTestId('conversation-detail').getByText('Please quote these items', { exact: true }).click({ button: 'right' });
    await page.getByRole('menuitem', { name: '回复', exact: true }).click();
  }
  await openReply();
  const compose = page.getByTestId('compose-dialog');
  const editor = compose.locator('.tiptap');
  const manager = page.getByTestId('reply-template-dialog');
  await editor.getByText('Best regards, Test Sender').waitFor();
  await editor.locator(':scope > p').first().click();
  await editor.press('Home');
  await editor.pressSequentially('My original reply.');
  await compose.locator('input[type=file][multiple]').first().setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('Notes') });
  const before = await editor.innerHTML();
  const quote = await editor.locator('.reply-quote').innerHTML();
  const signature = await editor.locator('[data-inbrix-signature]').innerHTML();
  await compose.getByRole('button', { name: '回复模板', exact: true }).click();
  await manager.getByRole('button', { name: '使用模板: 确认收到', exact: true }).click();
  await manager.waitFor({ state: 'hidden' });
  assert((await editor.textContent()).includes('您的邮件已收到'));
  assert(!(await editor.textContent()).includes('My original reply.'));
  assert.equal(await editor.locator('.reply-quote').innerHTML(), quote);
  assert.equal(await editor.locator('[data-inbrix-signature]').innerHTML(), signature);
  await compose.getByText('notes.txt', { exact: true }).waitFor();
  await page.getByRole('button', { name: '撤销', exact: true }).click();
  assert.equal(await editor.innerHTML(), before, 'Undo restores the complete original draft');
  await compose.getByRole('button', { name: '回复模板', exact: true }).click();
  await manager.getByRole('button', { name: '将当前正文存为模板', exact: true }).click();
  assert.equal(await manager.getByLabel('模板正文', { exact: true }).inputValue(), 'My original reply.');
  await manager.getByLabel('模板名称', { exact: true }).fill('自定义回复');
  await manager.getByLabel('模板正文', { exact: true }).fill('您好，感谢来信。\n\n已收到您的需求。<script>unsafe</script>');
  await manager.getByRole('button', { name: '保存模板', exact: true }).click();
  await manager.getByRole('button', { name: '编辑模板: 自定义回复', exact: true }).click();
  await manager.getByLabel('模板名称', { exact: true }).fill('已编辑模板');
  await manager.getByRole('button', { name: '保存模板', exact: true }).click();
  await manager.getByRole('button', { name: '使用模板: 已编辑模板', exact: true }).click();
  assert((await editor.textContent()).includes('<script>unsafe</script>'), 'Template text is not interpreted as HTML');
  assert.equal(await editor.locator('script').count(), 0);
  assert.equal(await editor.locator('.reply-quote').innerHTML(), quote);
  assert.equal(await editor.locator('[data-inbrix-signature]').innerHTML(), signature);
  await compose.getByRole('button', { name: '显示源代码', exact: true }).click();
  assert(await compose.getByRole('button', { name: '回复模板', exact: true }).isDisabled());
  await compose.getByRole('button', { name: '返回富文本', exact: true }).click();
  await compose.getByRole('button', { name: '回复模板', exact: true }).click();
  await page.screenshot({ path: '/tmp/inbrix-reply-templates.png' });
  await page.keyboard.press('Escape');
  assert.equal(sent, null, 'Template actions must never send an email');
  await compose.getByRole('button', { name: '发送', exact: true }).click();
  await compose.waitFor({ state: 'hidden' });
  assert(sent);
  const multipart = sent.toString('utf8');
  assert(multipart.includes('您好，感谢来信。'));
  assert(multipart.includes('Best regards, Test Sender'));
  assert(multipart.includes('Please quote these items'));
  assert(multipart.includes('filename="notes.txt"'));
  await page.reload();
  await openReply();
  await compose.getByRole('button', { name: '回复模板', exact: true }).click();
  await manager.getByRole('button', { name: '使用模板: 已编辑模板', exact: true }).waitFor();
  await manager.getByRole('textbox', { name: '搜索模板', exact: true }).fill('已编辑');
  assert.equal(await manager.getByRole('button', { name: /^使用模板:/ }).count(), 1);
  page.once('dialog', dialog => dialog.accept());
  await manager.getByRole('button', { name: '删除模板: 已编辑模板', exact: true }).click();
  assert.equal(await page.evaluate(() => JSON.parse(localStorage.getItem('inbrix-reply-templates')).some(t => t.name === '已编辑模板')), false);
  // A failed storage write must retain the unsaved template instead of reporting success.
  await page.evaluate(() => { const original = Storage.prototype.setItem; Storage.prototype.setItem = function(key, value) {
    if (key === 'inbrix-reply-templates') throw new DOMException('Full', 'QuotaExceededError');
    return original.call(this, key, value);
  }; });
  await manager.getByRole('button', { name: '新建模板', exact: true }).click();
  await manager.getByLabel('模板名称', { exact: true }).fill('无法保存');
  await manager.getByLabel('模板正文', { exact: true }).fill('内容');
  await manager.getByRole('button', { name: '保存模板', exact: true }).click();
  await page.getByText('模板保存失败，请检查浏览器存储空间或权限。', { exact: true }).waitFor();
  assert.equal(await manager.getByLabel('模板正文', { exact: true }).inputValue(), '内容');
  assert.deepEqual(errors, []);
  console.log('PASS: built-in and custom templates, create/edit/search/delete/persistence, body replacement and undo, signature/quote/attachment preservation, plain text safety, source-mode guard, storage failure, mocked reply send.');
} catch (error) { await page.screenshot({ path: '/tmp/inbrix-reply-template-failure.png' }); console.log(errors); throw error; }
finally { await browser.close(); }
