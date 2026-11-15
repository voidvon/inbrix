import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Exercise the real new-document dialog with synthetic data and no model/mailbox.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3097';
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
page.setDefaultTimeout(15000);
const prefix = '__INBRIX_CANVAS_DOCUMENT__:';
const errors = [], writes = [], saves = [];
const account = { id: 'sales', email: 'sales@example.invalid', isActive: true };
let releaseEditor, releaseReply, failNext = false, holdNext = false;
const editorGate = new Promise(resolve => { releaseEditor = resolve; });
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/src/components/app/canvas-document-editor.tsx*', async route => {
  await editorGate;
  await route.continue();
});
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/**', async route => {
  const request = route.request(), path = new URL(request.url()).pathname;
  if (path === '/api/ai/write-document') {
    const input = request.postDataJSON(); writes.push(input);
    assert.ok(input.canvas.startsWith(prefix), 'AI must receive the mounted editor snapshot');
    if (failNext) { failNext = false; return route.fulfill({ status: 503, json: { error: '测试服务暂不可用' } }); }
    if (holdNext) { holdNext = false; await new Promise(resolve => { releaseReply = resolve; }); }
    const payload = JSON.parse(input.canvas.slice(prefix.length));
    payload.business.values.payment_terms = input.instruction;
    return route.fulfill({ json: { changed: true, html: prefix + JSON.stringify(payload), summary: `已更新付款条件：${input.instruction}` } });
  }
  if (path === '/api/documents' && request.method() === 'POST') {
    const input = request.postDataJSON(); saves.push(input);
    return route.fulfill({ json: { ok: true, document: { ...input, revision: 1 } } });
  }
  if (path === '/api/documents/validate') return route.fulfill({ json: { html: request.postDataJSON().html, validation: { status: 'draft', issues: [] } } });
  let json = { documents: [], templates: [], stamps: [], bindings: [], models: [], agents: [] };
  if (path === '/api/accounts') json = { accounts: [account] };
  if (path === '/api/conversations') json = { locale: 'zh-CN', accounts: [account], accountEmail: account.email, conversations: [], folders: [] };
  if (path === '/api/capabilities') json = { role: 'user', calendar: false, notifications: false };
  if (path === '/api/document-templates') json = { templates: [
    { id: 'quote', type: 'quotation', name: '标准报价单模板', html: '<div data-spirax-quotation="reference-v1"></div>', updatedAt: new Date().toISOString() },
    { id: 'invoice', type: 'contract', name: '英文形式发票模板', html: '<div data-spirax-contract="reference-v1"></div>', updatedAt: new Date().toISOString() },
  ] };
  await route.fulfill({ json });
});
const dialog = page.getByTestId('document-editor-dialog');
const box = dialog.getByPlaceholder('输入对当前文档的修改要求…');
const send = async text => { await box.fill(text); await box.press('Enter'); };
try {
  await page.goto(`${base}/documents`);
  // Notifications sit over the small-screen tab bar; keep this check focused
  // on editor/chat lifecycle without waiting for each toast to disappear.
  await page.addStyleTag({ content: '[data-sonner-toaster], [data-sonner-toast] { pointer-events: none !important; }' });
  for (const kind of ['quotation', 'contract']) {
    await page.getByRole('button', { name: '新建文档', exact: true }).click();
    await page.getByRole('menuitem').filter({ hasText: kind === 'quotation' ? '标准报价单模板' : '英文形式发票模板' }).click();
    await dialog.waitFor();
    if (kind === 'quotation') {
      await box.waitFor();
      assert.equal(await box.isDisabled(), true, 'Sending must wait for the lazy editor to initialize');
      releaseEditor();
    }
    await box.waitFor();
    await page.waitForFunction(() => document.querySelector('textarea[placeholder="输入对当前文档的修改要求…"]')?.disabled === false);
    await send('预付30%');
    await dialog.getByText('已更新付款条件：预付30%', { exact: true }).waitFor();
    const canvas = dialog.locator('canvas').first();
    // Keep the same DOM/editor and chat while switching views or hiding the assistant.
    const originalCanvas = await canvas.elementHandle();
    if (mobile) {
      await dialog.getByRole('tab', { name: '预览文档', exact: true }).click();
      await dialog.getByRole('tab', { name: '变量表单', exact: true }).click();
      await dialog.getByRole('tab', { name: 'AI 对话', exact: true }).click();
    } else {
      await dialog.getByTitle('隐藏助手', { exact: true }).click();
      await dialog.getByRole('button', { name: 'AI 文档助手', exact: true }).click();
    }
    assert.equal(await originalCanvas.evaluate(node => node.isConnected), true, 'Switching views must preserve the editor instance');
    await dialog.getByText('已更新付款条件：预付30%', { exact: true }).waitFor();
    await send('发货前付清');
    await dialog.getByText('已更新付款条件：发货前付清', { exact: true }).waitFor();
    assert.equal(JSON.parse(writes.at(-1).canvas.slice(prefix.length)).business.values.payment_terms, '预付30%', 'Follow-up must use the unsaved edited document');
    assert.deepEqual(writes.at(-1).history, [{ role: 'user', content: '预付30%' }, { role: 'assistant', content: '已更新付款条件：预付30%' }]);
    // An in-flight response must survive switching away from mobile chat.
    holdNext = true;
    const pendingRequest = page.waitForRequest(request => request.url().endsWith('/api/ai/write-document') && request.postDataJSON().instruction === '延迟回复');
    await send('延迟回复');
    await pendingRequest;
    if (mobile) await dialog.getByRole('tab', { name: '预览文档', exact: true }).click();
    await page.waitForFunction(() => document.querySelector('textarea[placeholder="输入对当前文档的修改要求…"]')?.disabled === true);
    releaseReply(); releaseReply = undefined;
    if (mobile) await dialog.getByRole('tab', { name: 'AI 对话', exact: true }).click();
    await dialog.getByText('已更新付款条件：延迟回复', { exact: true }).waitFor();
    failNext = true;
    await send('重试付款条件');
    await dialog.getByText('生成失败：测试服务暂不可用', { exact: true }).waitFor();
    await dialog.getByRole('button', { name: '重试', exact: true }).click();
    await dialog.getByText('已更新付款条件：重试付款条件', { exact: true }).waitFor();
    assert.equal(writes.at(-1).history.filter(message => message.content === '重试付款条件').length, 0, 'Retry must not duplicate the current user request in history');
    assert.equal(await dialog.getByText('重试付款条件', { exact: true }).count(), 1, 'Retry must keep one user bubble');
    if (mobile) await dialog.getByRole('tab', { name: '预览文档', exact: true }).click();
    const saved = page.waitForResponse(response => response.url().endsWith('/api/documents') && response.request().method() === 'POST');
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    await saved;
    assert.equal(JSON.parse(saves.at(-1).html.slice(prefix.length)).business.values.payment_terms, '重试付款条件');
    // The first save replaces the new-document dialog with its saved record.
    // Wait for that replacement before closing, so Escape cannot hit the old one.
    await dialog.getByRole('heading', { name: '编辑文档', exact: true }).waitFor({ state: 'attached' });
    await dialog.getByRole('button').filter({ hasText: /^关闭$/ }).click();
    await dialog.waitFor({ state: 'hidden' });
  }
  assert.deepEqual(errors, []);
  console.log(`PASS ${mobile ? 'mobile' : 'desktop'}: lazy editor readiness, quotation/invoice first turn, retained editor/chat, follow-up context, in-flight view switch, retry and save.`);
} finally {
  releaseEditor(); releaseReply?.();
  if (errors.length) console.log(errors);
  await browser.close();
}
