import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';
import { chromium } from 'playwright';

const source = await readFile(new URL('../frontend/src/lib/ai-chat-stream.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { readAIChatStream } = await import(`data:text/javascript;base64,${Buffer.from(compiled.outputText).toString('base64')}`);
const partsSource = await readFile(new URL('../frontend/src/lib/ai-chat-parts.ts', import.meta.url), 'utf8');
const partsJS = ts.transpileModule(partsSource, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } });
const { appendAIChatText, upsertAIChatTool, getAIChatParts, finishAIChatParts } = await import(`data:text/javascript;base64,${Buffer.from(partsJS.outputText).toString('base64')}`);
let parts = appendAIChatText([], '开始', true);
parts = appendAIChatText(parts, '检索', false);
parts = upsertAIChatTool(parts, { id: 'one', name: 'read_product' });
parts = appendAIChatText(parts, '继续分析', true);
parts = upsertAIChatTool(parts, { id: 'two', name: 'read_product' });
const beforeResult = structuredClone(parts);
parts = upsertAIChatTool(parts, { id: 'one', name: 'read_product', status: 'success' });
assert.deepEqual(parts.map(part => part.type === 'tool' ? part.tool.id : part.content), ['开始检索', 'one', '继续分析', 'two'], 'Late results update their original row');
assert.equal(beforeResult[1].tool.status, undefined, 'Updates must not mutate previous snapshots');
assert.deepEqual(getAIChatParts({ content: '旧回答', toolCalls: [{ id: 'old', name: 'read_product' }] }).map(part => part.type), ['tool', 'text']);
assert.deepEqual(finishAIChatParts(parts, { content: '完成' }).map(part => part.type), ['text', 'tool', 'text', 'tool', 'text']);
const authoritative = [{ type: 'text', content: '服务端回答' }];
assert.deepEqual(finishAIChatParts(parts, { content: '服务端回答', parts: authoritative }), authoritative);
console.log('Passed: ordered text parts, stable tool result updates, repeated tool IDs, legacy history and final reconciliation.');
const frame = event => `data: ${JSON.stringify(event)}\r\n\r\n`;
const finalResponse = { content: '你好，世界', toolCalls: [] };
const wire = ': heartbeat\r\n\r\n' + frame({ type: 'text_delta', delta: '你好，' }) + frame({ type: 'text_delta', delta: '世界' }) + frame({ type: 'done', response: finalResponse });
const bytes = new TextEncoder().encode(wire);
const events = [];
const result = await readAIChatStream(new Response(new ReadableStream({
  start(controller) {
    // Every UTF-8 character and every CRLF delimiter spans reads.
    for (const byte of bytes) controller.enqueue(Uint8Array.of(byte));
    controller.close();
  },
})), event => events.push(event));
assert.deepEqual(result, finalResponse);
assert.deepEqual(events.filter(event => event.type === 'text_delta').map(event => event.delta), ['你好，', '世界']);
for (const [body, pattern] of [
  [frame({ type: 'text_delta', delta: 'partial' }), /interrupted/],
  [frame({ type: 'error', error: 'provider failed' }), /provider failed/],
  ['data: invalid\n\n', /JSON/],
  [frame({ type: 'done', response: {} }), /Invalid/],
]) {
  await assert.rejects(readAIChatStream(new Response(body), () => {}), pattern);
}
console.log('Passed: UTF-8/CRLF fragmentation, completion, malformed frames and interrupted streams.');
if (process.argv.includes('--parser-only')) process.exit(0);

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: process.env.AI_TEST_MOBILE ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const now = new Date().toISOString();
const accountEmail = 'me@example.com';
const conversation = { id: 'stream-test', title: 'Alice', peerEmail: 'alice@example.com', subject: 'Stream test', preview: 'Original message', date: now, count: 1, unreadCount: 0, accountEmail, status: 'unanswered' };
const mail = { id: '1', from: conversation.peerEmail, to: accountEmail, subject: conversation.subject, body: conversation.preview, preview: conversation.preview, date: now, outgoing: false, messageId: '<original@example.com>', flags: ['\\Seen'] };
let savedMessages = [];
let saveCount = 0;
const errors = [];
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/api/**', async route => {
  const path = new URL(route.request().url()).pathname;
  let json = {};
  if (path === '/api/conversations') {
    json = { locale: 'zh-CN', accountEmail, conversations: [conversation], folders: [], accounts: [{ id: 'account', email: accountEmail, label: accountEmail, isActive: true }] };
  } else if (path.endsWith('/ai-chat')) {
    if (route.request().method() === 'PUT') {
      savedMessages = route.request().postDataJSON().messages;
      saveCount++;
    }
    json = { ok: true, messages: savedMessages };
  } else if (path.startsWith('/api/conversations/')) {
    json = { conversation: { ...conversation, messages: [mail] } };
  } else if (path === '/api/drafts') json = { drafts: [] };
  else json = { calendar: false, notifications: false, models: [], agents: [], bindings: [], documents: [], signatures: [], attachments: [], total: 0 };
  await route.fulfill({ json });
});
await page.addInitScript(() => {
  const original = window.fetch;
  window.aiStreamRequests = [];
  window.fetch = async (url, options) => {
    if (url !== '/api/ai/agent-chat') return original(url, options);
    window.aiStreamRequests.push(JSON.parse(options.body));
    if (new Headers(options.headers).get('Accept') !== 'text/event-stream') throw new Error('Stream was not requested');
    return new Response(new ReadableStream({
      start(controller) {
        const encoder = new TextEncoder();
        window.emitAIEvent = event => controller.enqueue(encoder.encode(`data: ${JSON.stringify(event)}\r\n\r\n`));
        window.closeAIStream = () => controller.close();
        options.signal.addEventListener('abort', () => {
          window.aiStreamAborted = true;
          controller.error(new DOMException('Aborted', 'AbortError'));
        });
      },
    }), { headers: { 'Content-Type': 'text/event-stream' } });
  };
});
const emit = event => page.evaluate(event => window.emitAIEvent(event), event);
const base = process.env.COMPOSE_TEST_URL || 'http://localhost:2342';
try {
  await page.goto(`${base}/inbox?conversation=${conversation.id}`);
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  const input = page.getByRole('textbox', { name: '询问邮件、草稿或产品资料… (Enter 发送)', exact: true });
  await input.fill('请核对产品');
  await input.press('Enter');
  await page.waitForFunction(() => window.aiStreamRequests.length === 1);
  await emit({ type: 'started' });
  await emit({ type: 'text_reset' });
  await emit({ type: 'text_delta', delta: '我先检索产品。' });
  await emit({ type: 'tool_start', tool: { id: 'call_1', name: 'search_products' } });
  const pending = page.getByTestId('ai-streaming-message');
  await pending.getByText('检索产品资料', { exact: true }).waitFor();
  await emit({ type: 'tool_end', tool: { id: 'call_1', name: 'search_products', status: 'success', summary: '已找到产品' } });
  await emit({ type: 'text_reset' });
  await emit({ type: 'text_delta', delta: '已找到产品，继续读取规格。' });
  await emit({ type: 'tool_start', tool: { id: 'call_2', name: 'read_product' } });
  await emit({ type: 'tool_end', tool: { id: 'call_2', name: 'read_product', status: 'success', summary: '第一部分' } });
  await emit({ type: 'tool_start', tool: { id: 'call_3', name: 'read_product' } });
  await emit({ type: 'tool_end', tool: { id: 'call_3', name: 'read_product', status: 'success', summary: '第二部分' } });
  await emit({ type: 'text_reset' });
  await emit({ type: 'text_delta', delta: '已核对' });
  await pending.getByText('已核对', { exact: true }).waitFor();
  assert.equal(saveCount, 0, 'Partial output must not be saved as a completed answer');
  assert.equal(await pending.getByRole('button', { name: '写入草稿', exact: true }).isDisabled(), true);
  await emit({ type: 'text_delta', delta: '，规格正确。' });
  await pending.getByText('已核对，规格正确。', { exact: true }).waitFor();
  const order = element => element.locator('[data-ai-part]').evaluateAll(parts => parts.map(part => part.dataset.aiPart === 'tool' ? `tool:${part.dataset.toolId}` : `text:${part.querySelector('p')?.textContent}`));
  const expectedOrder = ['text:我先检索产品。', 'tool:call_1', 'text:已找到产品，继续读取规格。', 'tool:call_2', 'tool:call_3', 'text:已核对，规格正确。'];
  assert.deepEqual(await order(pending), expectedOrder, 'Tools and model text must remain in execution order during streaming');
  await page.screenshot({ path: process.env.AI_TEST_MOBILE ? '/private/tmp/inbrix-ai-stream-mobile.png' : '/private/tmp/inbrix-ai-stream-desktop.png' });
  const tools = [
    { id: 'call_1', name: 'search_products', status: 'success', summary: '已找到产品' },
    { id: 'call_2', name: 'read_product', status: 'success', summary: '第一部分' },
    { id: 'call_3', name: 'read_product', status: 'success', summary: '第二部分' },
  ];
  const response = {
    content: '已核对，规格正确。', toolCalls: tools,
    parts: [
      { type: 'text', content: '我先检索产品。' }, { type: 'tool', tool: tools[0] },
      { type: 'text', content: '已找到产品，继续读取规格。' },
      { type: 'tool', tool: tools[1] }, { type: 'tool', tool: tools[2] },
      { type: 'text', content: '已核对，规格正确。' },
    ],
  };
  const saved = page.waitForResponse(response => response.url().endsWith('/ai-chat') && response.request().method() === 'PUT');
  await emit({ type: 'done', response });
  await saved;
  await pending.waitFor({ state: 'hidden' });
  assert.equal(savedMessages.length, 2);
  assert.equal(savedMessages[1].content, response.content);
  assert.equal(savedMessages[1].toolCalls[0].name, 'search_products');
  assert.deepEqual(savedMessages[1].parts, response.parts);
  const assistant = page.locator('[data-ai-message-role="assistant"]:visible');
  assert.deepEqual(await order(assistant.last()), expectedOrder, 'Completion must not regroup tools at the bottom');

  await page.reload();
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await page.locator('p:visible').filter({ hasText: /^已核对，规格正确。$/ }).waitFor();
  assert.deepEqual(await order(assistant.last()), expectedOrder, 'Persisted history must preserve chronological parts after reload');

  // An incomplete stream leaves received text visible and restores the question.
  await input.fill('第二个问题');
  await input.press('Enter');
  await page.waitForFunction(() => window.aiStreamRequests.length === 1);
  await emit({ type: 'text_delta', delta: '未完成的内容' });
  await pending.getByText('未完成的内容', { exact: true }).waitFor();
  await page.evaluate(() => window.closeAIStream());
  await page.getByRole('alert').filter({ hasText: '回复未完成' }).waitFor();
  assert.equal(await input.inputValue(), '第二个问题');
  assert.equal(await pending.getByText('未完成的内容', { exact: true }).isVisible(), true);
  assert.equal(saveCount, 1, 'Incomplete output must not corrupt persisted history');
  // Keep the pointer off the top-center toast so its hover pause does not
  // cover the mobile panel's close control during the cancellation check.
  await page.mouse.move(5, 500);
  await page.locator('[data-sonner-toast][data-type="error"]').waitFor({ state: 'hidden' });

  // Retry uses completed history only; close cancels the active fetch.
  await input.press('Enter');
  await page.waitForFunction(() => window.aiStreamRequests.length === 2);
  assert.equal(await page.evaluate(() => window.aiStreamRequests.at(-1).messages.length), 3);
  await emit({ type: 'text_delta', delta: '重试中' });
  await pending.getByText('重试中', { exact: true }).waitFor();
  await page.getByRole('button', { name: '关闭 AI 对话', exact: true }).click();
  await page.waitForFunction(() => window.aiStreamAborted);
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await page.locator('p:visible').filter({ hasText: /^已核对，规格正确。$/ }).waitFor();
  assert.equal(await page.getByText('重试中', { exact: true }).count(), 0);
  assert.deepEqual(errors, []);
  console.log('Passed: text/tool chronology, repeated tools, stable completion and reloaded history, incremental rendering, errors, retry and cancellation.');
} finally {
  await browser.close();
}
