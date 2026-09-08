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
try {
  await page.goto(`${base}/inbox?conversation=test-thread`);
  await page.getByTestId('conversation-detail').getByText('Please quote these items', { exact: true }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: '回复', exact: true }).click();
  const compose = page.getByTestId('compose-dialog');
  await compose.locator('.tiptap').press('Home');
  await compose.locator('.tiptap').pressSequentially('Please find the quotation attached.');
  const before = await compose.locator('.tiptap').innerHTML();
  console.log('Reply opened');
  await compose.getByRole('button', { name: '新建报价单', exact: true }).click();
  const editor = page.getByTestId('document-editor-dialog');
  await editor.locator('canvas').first().waitFor();
  await editor.getByRole('textbox', { name: '文档名称', exact: true }).fill('报价2026.doc');
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  await page.waitForFunction(() => localStorage.getItem('inbrix-documents')?.includes('报价2026.doc'));
  await editor.getByRole('button', { name: '作为 PDF 添加到邮件', exact: true }).click();
  await editor.waitFor({ state: 'hidden' });
  console.log('Document editor closed');
  await compose.getByText('报价2026.pdf', { exact: true }).waitFor();
  assert.equal(await compose.locator('.tiptap').innerHTML(), before, 'Body must survive child document editor');
  assert.equal(sent, null, 'Attaching must not submit the email');
  await compose.getByRole('button', { name: '引入附件', exact: true }).click();
  const picker = page.getByTestId('document-attachment-picker');
  await picker.getByRole('button', { name: /报价2026.doc/ }).click();
  await picker.waitFor({ state: 'hidden' });
  console.log('Saved document attached');
  assert.equal(await compose.getByText('报价2026.pdf', { exact: true }).count(), 2);
  assert.equal(await compose.locator('.tiptap').innerHTML(), before);
  await compose.getByRole('button', { name: /报价2026.pdf/ }).last().click();
  assert.equal(await compose.getByText('报价2026.pdf', { exact: true }).count(), 1);
  // Canceling the new editor should not add or send anything.
  await compose.getByRole('button', { name: '新建报价单', exact: true }).click();
  await editor.locator('canvas').first().waitFor();
  await page.keyboard.press('Escape');
  await editor.waitFor({ state: 'hidden' });
  console.log('Document editor closed');
  assert.equal(await compose.getByText('报价2026.pdf', { exact: true }).count(), 1);
  await page.screenshot({ path: '/tmp/inbrix-compose-documents.png' });
  // Verify the existing attachment size limit applies to generated PDFs too.
  await compose.locator('input[type=file][multiple]').first().setInputFiles({ name: 'large.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(18 * 1024 * 1024 - await page.evaluate(() => window.testPDFSizes[0]) - 1) });
  await compose.getByText('large.bin', { exact: true }).waitFor();
  await compose.getByRole('button', { name: '引入附件', exact: true }).click();
  await picker.getByRole('button', { name: /报价2026.doc/ }).click();
  await page.getByText('附件总大小不能超过 18 MB', { exact: true }).waitFor();
  await page.keyboard.press('Escape');
  assert.equal(await compose.getByText('报价2026.pdf', { exact: true }).count(), 1);
  await compose.getByRole('button', { name: /large.bin/ }).click();
  // Legacy HTML-backed contracts also render through the canvas engine before attachment.
  await page.evaluate(() => {
    const records = JSON.parse(localStorage.getItem('inbrix-documents'));
    records.push({ id: 'legacy-contract', type: 'contract', name: '历史合同.doc', updatedAt: new Date().toISOString(),
      html: '<h1>合同</h1>' + '<p>付款与交付约定</p>'.repeat(70) });
    localStorage.setItem('inbrix-documents', JSON.stringify(records));
  });
  await compose.getByRole('button', { name: '引入附件', exact: true }).click();
  await picker.getByRole('textbox', { name: '搜索文档' }).fill('历史合同');
  await picker.getByRole('button', { name: /历史合同.doc/ }).click();
  await picker.waitFor({ state: 'hidden' });
  await compose.getByText('历史合同.pdf', { exact: true }).waitFor();
  await compose.getByRole('button', { name: '发送', exact: true }).click();
  await compose.waitFor({ state: 'hidden' });
  assert(sent, 'Mock send should receive multipart data');
  const multipart = sent.toString('latin1');
  assert(multipart.includes('Content-Type: application/pdf'));
  assert.equal([...multipart.matchAll(/%PDF-/g)].length, 2);
  assert([...multipart.matchAll(/\/Type \/Page\b/g)].length >= 4, 'Multipart reply must include all contract pages');
  assert(multipart.includes('name="in_reply_to"\r\n\r\n<request@example.com>'));
  assert(multipart.includes('Please find the quotation attached.'));
  assert.deepEqual(errors, []);
  console.log('PASS: reply draft preservation, create/save/attach PDF, import .doc-named saved document as PDF, cancellation, removal, size limit, intercepted multipart reply with PDF.');
} catch (error) { await page.screenshot({ path: '/tmp/inbrix-compose-failure.png' }); console.log(errors); throw error; } finally { await browser.close(); }
