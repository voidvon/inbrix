import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createAIChatTestStore } from './ai-chat-test-store.mjs';

// Isolated UI test: all uploads, images and model requests are mocked.
const browser = await chromium.launch({ headless: true });
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const base = process.env.COMPOSE_TEST_URL || 'http://127.0.0.1:3097';
const accountEmail = 'sales@example.com', now = new Date().toISOString();
const png = Buffer.from(await page.evaluate(() => {
  const canvas = document.createElement('canvas'); canvas.width = 600; canvas.height = 360;
  const ctx = canvas.getContext('2d');
  ctx.fillStyle = '#eef6ff'; ctx.fillRect(0, 0, 600, 360);
  ctx.fillStyle = '#1e40af'; ctx.fillRect(0, 0, 600, 70);
  ctx.fillStyle = '#fff'; ctx.font = 'bold 28px sans-serif'; ctx.fillText('PRODUCT REQUIREMENTS', 28, 45);
  ctx.fillStyle = '#172554'; ctx.font = '26px sans-serif';
  ['Model: DP27', 'Size: DN25', 'Quantity: 5', 'Please verify catalog price'].forEach((line, i) => ctx.fillText(line, 28, 130 + i * 52));
  return canvas.toDataURL('image/png').split(',')[1];
}), 'base64');
const conversation = { id: 'image-test', title: 'Buyer', peerEmail: 'buyer@example.com', subject: 'Photo inquiry', preview: 'See attached requirement', date: now, count: 1, unreadCount: 0, accountEmail, status: 'unanswered' };
const mail = { id: '1', from: conversation.peerEmail, to: accountEmail, subject: conversation.subject, body: conversation.preview, preview: conversation.preview, date: now, outgoing: false, flags: ['\\Seen'] };
const requests = [], deleted = [], errors = [];
const chatStore = createAIChatTestStore(accountEmail);
const messages = chatStore.mail.messages;
let counter = 0, failChat = false, failUpload = false;
let releaseUpload;
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const req = route.request(), url = new URL(req.url()), path = url.pathname;
  let json = {};
  const sessionRoute = chatStore.routeSessions(req, url);
  if (sessionRoute) { await route.fulfill(sessionRoute); return; }
  if (path === '/api/ai/attachments' && req.method() === 'POST') {
    assert.match(req.headers()['content-type'], /multipart\/form-data/);
    assert.ok(req.postDataBuffer().includes(Buffer.from(accountEmail)), 'Upload has mailbox identity');
    if (failUpload) { failUpload = false; await route.fulfill({ status: 400, json: { error: '测试上传失败' } }); return; }
    if (releaseUpload) await releaseUpload;
    await route.fulfill({ status: 201, json: { id: `image-${++counter}`, filename: '需求.png', contentType: 'image/png', size: png.length } }); return;
  } else if (path.startsWith('/api/ai/attachments/')) {
    assert.equal(url.searchParams.get('accountEmail'), accountEmail);
    if (req.method() === 'DELETE') { deleted.push(path.split('/').at(-1)); await route.fulfill({ status: 204 }); }
    else await route.fulfill({ contentType: 'image/png', body: png });
    return;
  } else if (path === '/api/accounts') json = { accounts: [{ id: 'account', email: accountEmail, label: accountEmail, isActive: true }] };
  else if (path === '/api/accounts/account/brands') json = { brands: [], brandMode: 'prefer' };
  else if (path === '/api/settings/ai/skills') json = { mailSummarySkillId: 'mail-analysis', skills: [{ id: 'email_analysis', name: '邮件需求分析', kind: 'chat', enabled: true }, { id: 'quotation', name: '产品选型与报价', kind: 'chat', enabled: true }, { id: 'contract', name: '销售合同制作', kind: 'chat', enabled: true }] };
  else if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail, conversations: [conversation], folders: [], accounts: [{ id: 'account', email: accountEmail, label: accountEmail, isActive: true }] };
  else if (path.endsWith('/ai-chat')) {
    assert.notEqual(req.method(), 'PUT', 'History is never saved by the browser');
    json = { ok: true, ...chatStore.mail, sessionId: chatStore.mail.id };
  } else if (path.startsWith('/api/conversations/')) json = { conversation: { ...conversation, messages: [mail] } };
  else if (path === '/api/ai/agent-chat') {
    const payload = req.postDataJSON(); requests.push(payload);
    if (failChat) { chatStore.fail(payload); failChat = false; await route.fulfill({ status: 422, json: { error: '模型暂时不可用' } }); return; }
    json = chatStore.complete(payload, { content: `图片已识别 ${requests.length}`, toolCalls: [] });
    if (req.headers().accept?.includes('text/event-stream')) {
      await route.fulfill({ contentType: 'text/event-stream', body: `data: ${JSON.stringify({ type: 'done', response: json })}\n\n` }); return;
    }
  } else if (path === '/api/drafts') json = { drafts: [] };
  else json = { calendar: false, notifications: false, models: [], agents: [], skills: [], bindings: [], documents: [], signatures: [], attachments: [], total: 0, stamps: [], templates: [], brands: [] };
  await route.fulfill({ json });
});
const file = () => page.locator('div:visible > input[type=file][aria-label="选择聊天图片"]');
const upload = async () => {
  await file().setInputFiles({ name: '需求.png', mimeType: 'image/png', buffer: png });
  await page.getByTestId('ai-image-uploads').filter({ visible: true }).getByRole('img').waitFor();
  await page.getByTestId('ai-image-uploads').filter({ visible: true }).locator('.animate-spin').waitFor({ state: 'hidden' });
};
const send = () => page.getByRole('button', { name: '发送', exact: true });
const assertNoOverflow = async () => assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
try {
  await page.goto(`${base}/inbox?conversation=${conversation.id}`);
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await file().setInputFiles(Array.from({ length: 7 }, (_, i) => ({ name: `${i}.png`, mimeType: 'image/png', buffer: png })));
  await page.getByRole('alert').getByText('每条消息最多添加 6 张图片', { exact: true }).waitFor();
  assert.equal(counter, 0, 'Over-limit batches are not uploaded');
  let release;
  releaseUpload = new Promise(resolve => { release = resolve; });
  await file().setInputFiles({ name: '需求.png', mimeType: 'image/png', buffer: png });
  await page.getByTestId('ai-image-uploads').filter({ visible: true }).locator('.animate-spin').waitFor();
  assert.equal(await send().isEnabled(), false, 'Do not send incomplete uploads');
  release(); releaseUpload = undefined;
  await page.getByTestId('ai-image-uploads').filter({ visible: true }).locator('.animate-spin').waitFor({ state: 'hidden' });
  assert.equal(await send().isEnabled(), true, 'Image-only message can be sent');
  await send().click();
  await page.getByText('图片已识别 1', { exact: true }).filter({ visible: true }).first().waitFor();
  assert.equal(requests[0].messages.at(-1).content, '');
  assert.equal(requests[0].messages.at(-1).attachments[0].id, 'image-1');
  assert.equal(messages[0].attachments[0].id, 'image-1');
  assert.equal(JSON.stringify(messages).includes('base64'), false, 'History stores refs only');
  await page.waitForFunction(() => Array.from(document.querySelectorAll('[data-testid=ai-message-images] img')).some(img => img.complete && img.naturalWidth > 1));
  await page.getByTestId('ai-message-images').filter({ visible: true }).getByRole('button').click();
  await page.getByRole('dialog').getByRole('heading', { name: '需求.png', exact: true }).waitFor();
  await page.keyboard.press('Escape');
  await page.reload();
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await page.getByTestId('ai-message-images').filter({ visible: true }).getByRole('img').waitFor();
  const input = page.getByRole('textbox', { name: '询问邮件、草稿或产品资料… (Enter 发送)', exact: true });
  await input.fill('上一张图片的型号？'); await input.press('Enter');
  await page.getByText('图片已识别 2', { exact: true }).filter({ visible: true }).first().waitFor();
  assert.equal(requests[1].messages.length, 1, 'Follow-up sends only its new message');
  assert.equal(messages[0].attachments[0].id, 'image-1', 'Server retains prior images');

  await upload();
  failChat = true;
  await input.fill('识别第二张'); await input.press('Enter');
  await page.getByText('模型暂时不可用', { exact: true }).first().waitFor();
  assert.equal(await page.getByTestId('ai-image-uploads').filter({ visible: true }).getByRole('img').count(), 1, 'Failed chat keeps image draft');
  await send().click(); await page.getByText('图片已识别 4', { exact: true }).filter({ visible: true }).first().waitFor();
  assert.equal(requests[2].requestId, requests[3].requestId, 'Retry reuses the same request');
  assert.equal(messages.filter(m => m.content === '识别第二张').length, 1, 'Retry does not duplicate the persisted user');

  failUpload = true;
  await upload();
  await page.getByRole('alert').getByText('测试上传失败', { exact: true }).waitFor();
  assert.equal(await send().isEnabled(), false);
  await page.getByRole('button', { name: '移除图片: 需求.png', exact: true }).click();
  // Clipboard image paste and removal clean up the unsent uploaded file.
  await input.evaluate((node, base64) => {
    const bytes = Uint8Array.from(atob(base64), c => c.charCodeAt(0));
    const transfer = new DataTransfer(); transfer.items.add(new File([bytes], '粘贴.png', { type: 'image/png' }));
    node.dispatchEvent(new ClipboardEvent('paste', { bubbles: true, clipboardData: transfer }));
  }, png.toString('base64'));
  await page.getByTestId('ai-image-uploads').filter({ visible: true }).locator('.animate-spin').waitFor({ state: 'hidden' });
  await page.getByRole('button', { name: '移除图片: 粘贴.png', exact: true }).click();
  await page.waitForFunction(() => !document.querySelector('[data-testid="ai-image-uploads"]'));
  await assertNoOverflow();
  await page.screenshot({ style: '[data-sonner-toaster] { display: none; }', path: `/private/tmp/inbrix-ai-images-mail-${mobile ? 'mobile' : 'desktop'}.png` });

  await page.goto(`${base}/ai-document`);
  // Drag-and-drop shares the same uploader in document chat.
  const docInput = page.locator('textarea').last();
  await docInput.evaluate((node, base64) => {
    const bytes = Uint8Array.from(atob(base64), c => c.charCodeAt(0));
    const transfer = new DataTransfer(); transfer.items.add(new File([bytes], '拖放.png', { type: 'image/png' }));
    node.dispatchEvent(new DragEvent('drop', { bubbles: true, dataTransfer: transfer }));
  }, png.toString('base64'));
  await page.getByTestId('ai-image-uploads').filter({ visible: true }).getByRole('img').waitFor();
  await page.getByTestId('ai-image-uploads').filter({ visible: true }).locator('.animate-spin').waitFor({ state: 'hidden' });
  await send().click(); await page.getByText('图片已识别 5', { exact: true }).filter({ visible: true }).first().waitFor();
  assert.equal(requests[4].messages.at(-1).attachments.length, 1);
  await page.reload();
  await page.getByTestId('ai-message-images').filter({ visible: true }).getByRole('img').waitFor();
  await docInput.fill('请继续分析图片'); await docInput.press('Enter');
  await page.getByText('图片已识别 6', { exact: true }).filter({ visible: true }).first().waitFor();
  assert.equal(requests[5].messages.length, 1, 'No duplicate image turn');
  assert.equal([...chatStore.sessions.values()].find(s => s.kind === 'document').messages.filter(m => m.attachments?.length).length, 1);
  // A failed server run remains retryable after all browser memory is lost.
  failChat = true;
  await docInput.fill('刷新后恢复这次请求'); await docInput.press('Enter');
  await page.getByText('模型暂时不可用', { exact: true }).first().waitFor();
  const failedId = requests.at(-1).requestId;
  await page.reload();
  await page.getByRole('button', { name: '重试上次回复', exact: true }).click();
  await page.getByText('图片已识别 8', { exact: true }).filter({ visible: true }).first().waitFor();
  assert.equal(requests.at(-1).retryRequestId, failedId, 'Reload retries the persisted server request');
  const savedDocumentChat = [...chatStore.sessions.values()].find(s => s.kind === 'document');
  assert.equal(savedDocumentChat.messages.filter(m => m.content === '刷新后恢复这次请求').length, 1);

  // Updating a Skill must preserve the unsent text and uploaded image.
  await upload();
  const unsentImage = `image-${counter}`;
  await docInput.fill('尚未发送的草稿');
  await page.getByRole('combobox', { name: '任务 Skill', exact: true }).click();
  const selectionRefresh = page.waitForResponse(res => res.request().method() === 'GET' && res.url().includes('/api/ai/chat-sessions/'));
  await page.getByRole('option', { name: '销售合同制作', exact: true }).click();
  await selectionRefresh;
  assert.equal(await docInput.inputValue(), '尚未发送的草稿');
  assert.equal(await page.getByTestId('ai-image-uploads').filter({ visible: true }).getByRole('img').count(), 1);
  assert.equal(deleted.includes(unsentImage), false, 'Business selection keeps draft attachments');
  await page.getByRole('button', { name: '移除图片: 需求.png', exact: true }).click();
  await assertNoOverflow();
  await page.screenshot({ style: '[data-sonner-toaster] { display: none; }', path: `/private/tmp/inbrix-ai-images-document-${mobile ? 'mobile' : 'desktop'}.png` });
  const docImage = requests[4].messages.at(-1).attachments[0].id;
  await page.getByRole('button', { name: '清空对话', exact: true }).click();
  const deleteRequest = page.waitForRequest(req => req.method() === 'DELETE' && req.url().includes('/api/ai/chat-sessions/'));
  await page.getByRole('dialog').getByRole('button', { name: '确认删除', exact: true }).click();
  await deleteRequest;
  await page.getByTestId('ai-message-images').filter({ visible: true }).waitFor({ state: 'hidden' });
  assert.equal(deleted.includes(docImage), false, 'Session deletion releases refs on the server; browser never deletes sent images');
  assert.equal([...chatStore.sessions.values()].filter(s => s.kind === 'document').length, 0);
  assert.ok(deleted.length > 0, 'Removed draft images were deleted');
  assert.ok(!deleted.includes('image-1'), 'Sent images survive unmount');
  // Mail chat uses the same durable recovery path after reload.
  await page.goto(`${base}/inbox?conversation=${conversation.id}`);
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  failChat = true;
  await input.fill('刷新后继续邮件分析'); await input.press('Enter');
  await page.getByText('模型暂时不可用', { exact: true }).first().waitFor();
  const failedMailId = requests.at(-1).requestId;
  await page.reload();
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await page.getByRole('button', { name: '重试上次回复', exact: true }).click();
  await page.getByText('图片已识别 10', { exact: true }).filter({ visible: true }).first().waitFor();
  assert.equal(requests.at(-1).retryRequestId, failedMailId);
  assert.equal(messages.filter(message => message.content === '刷新后继续邮件分析').length, 1);
  assert.deepEqual(errors, []);
  console.log(`Passed (${mobile ? 'mobile' : 'desktop'}): upload/paste/drop, pending and failed uploads, image-only chat, full preview, retry, persistence, follow-up and both chat panels.`);
} finally { await browser.close(); }
