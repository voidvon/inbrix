import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createAIChatTestStore } from './ai-chat-test-store.mjs';

// All backend operations are intercepted; no real mailbox/model is contacted.
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: process.env.AI_TEST_MOBILE ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const base = process.env.COMPOSE_TEST_URL || 'http://127.0.0.1:3097';
const now = new Date().toISOString();
const accountEmail = 'sales@example.com';
const conversation = { id: 'business-test', title: 'Buyer', peerEmail: 'buyer@example.com', subject: 'Valve inquiry', preview: 'Need brand B valves', date: now, count: 1, unreadCount: 0, accountEmail, status: 'unanswered' };
const mail = { id: '1', from: conversation.peerEmail, to: accountEmail, subject: conversation.subject, body: conversation.preview, preview: conversation.preview, date: now, outgoing: false, flags: ['\\Seen'] };
let agents = [{ id: 'sales-ab', name: 'AB 销售助手', prompt: '请用中文回复，缺少参数时先询问。', outputLabels: ['客户', '需求'], businessProfile: { company: '销售公司', brands: ['C'], brandMode: 'restrict', quotationTemplateId: '', contractTemplateId: '' } }];

let businessContext = { agentId: 'sales-ab', skillId: '', brands: [] };
const chatStore = createAIChatTestStore(accountEmail, businessContext);
const messages = chatStore.mail.messages;
const requests = [], errors = [];
let savedProfile;
let mailboxBrands = { brands: ['A', 'B'], brandMode: 'restrict' };
let savedMailboxBrands;
page.on('pageerror', err => errors.push(err.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'isolated-test' } }));
await page.route('**/api/**', async route => {
  const req = route.request();
  const url = new URL(req.url()); const path = url.pathname;
  const sessionRoute = chatStore.routeSessions(req, url);
  if (sessionRoute) { await route.fulfill(sessionRoute); return; }
  let json = {};
  if (path === '/api/accounts') json = { accounts: [{ id: 'account', email: accountEmail, label: accountEmail, isActive: true, imapServer: 'imap.example.com', imapPort: 993, smtpServer: 'smtp.example.com', smtpPort: 587 }] };
  else if (path === '/api/accounts/account/brands') {
    if (req.method() === 'PUT') savedMailboxBrands = mailboxBrands = req.postDataJSON();
    json = mailboxBrands;
  } else if (path === '/api/accounts/account/feishu-webhook') json = { enabled: false, url: '' };
  else if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail, conversations: [conversation], folders: [], accounts: [{ id: 'account', email: accountEmail, label: accountEmail, isActive: true }] };
  else if (path.endsWith('/ai-chat')) {
    if (req.method() === 'PUT') {
      const payload = req.postDataJSON();
      assert.equal(payload.messages, undefined, 'No history PUT');
      assert.equal(payload.version, chatStore.mail.version);
      chatStore.mail.version++;
      if (payload.businessContext) chatStore.mail.businessContext = businessContext = payload.businessContext;
    }
    json = { ok: true, ...chatStore.mail, sessionId: chatStore.mail.id };
  } else if (path.startsWith('/api/conversations/')) json = { conversation: { ...conversation, messages: [mail] } };
  else if (path === '/api/product-navigation') json = { total: 3, brands: ['A', 'B', 'C'].map(name => ({ name, productCount: 1, categories: [] })) };
  else if (path === '/api/settings/ai/agents') json = { agents, skills: [{ id: 'email_analysis', name: '邮件需求分析', dependsOn: [] }, { id: 'quotation', name: '产品选型与报价', dependsOn: ['email_analysis'] }, { id: 'contract', name: '销售合同制作', dependsOn: ['email_analysis'] }] };
  else if (path === '/api/settings/ai/agents/sales-ab' && req.method() === 'PUT') {
    savedProfile = req.postDataJSON(); agents = [{ id: 'sales-ab', ...savedProfile }]; json = agents[0];
  } else if (path === '/api/settings/ai/task-bindings') json = { bindings: [] };
  else if (path === '/api/document-templates') json = { templates: [{ id: 'quote-custom', type: 'quotation', name: 'A 品牌报价模板', html: '<div data-spirax-quotation="reference-v1"></div>', updatedAt: now }] };
  else if (path === '/api/ai/agent-chat') {
    const payload = req.postDataJSON(); requests.push(payload);
    json = chatStore.complete(payload, { content: '已按所选品牌分析需求，缺少口径，需向客户确认。', toolCalls: [], businessContext: payload.businessContext });
    if (req.headers().accept?.includes('text/event-stream')) {
      await route.fulfill({ contentType: 'text/event-stream', body: `data: ${JSON.stringify({ type: 'done', response: json })}\n\n` }); return;
    }
  } else if (path === '/api/drafts') json = { drafts: [] };
  else json = { calendar: false, notifications: false, models: [], agents: [], bindings: [], documents: [], signatures: [], attachments: [], total: 0, stamps: [] };
  await route.fulfill({ json });
});

async function chooseSkill(name) {
  await page.getByRole('combobox', { name: '任务 Skill' }).click();
  await page.getByRole('option', { name, exact: true }).click();
}
try {
  await page.goto(`${base}/inbox?conversation=${conversation.id}`);
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await page.getByRole('button', { name: '品牌: 限定 A、B', exact: true }).waitFor();
  await chooseSkill('产品选型与报价');
  await page.getByRole('button', { name: '品牌: 限定 A、B', exact: true }).click();
  assert.equal(await page.getByRole('menuitemcheckbox', { name: 'C', exact: true }).count(), 0);
  await page.getByRole('menuitemcheckbox', { name: 'B', exact: true }).click();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '品牌: B', exact: true }).waitFor();
  await page.getByRole('button', { name: 'AB 销售助手', exact: true }).click();
  await page.getByRole('menuitem', { name: '默认智能体', exact: true }).click();
  await page.getByRole('button', { name: '品牌: B', exact: true }).waitFor();
  await page.getByRole('button', { name: '默认智能体', exact: true }).click();
  const agentSaved = page.waitForResponse(response => response.url().endsWith('/ai-chat') && response.request().method() === 'PUT' && response.request().postDataJSON().businessContext?.agentId === 'sales-ab');
  await page.getByRole('menuitem', { name: 'AB 销售助手', exact: true }).click();
  await agentSaved;
  await page.reload();
  await page.getByRole('button', { name: 'AI对话', exact: true }).click();
  await page.getByRole('button', { name: '品牌: B', exact: true }).waitFor();
  await page.getByRole('combobox', { name: '任务 Skill' }).filter({ hasText: '产品选型与报价' }).waitFor();
  const input = page.getByRole('textbox', { name: '询问邮件、草稿或产品资料… (Enter 发送)', exact: true });
  await input.fill('请分析并报价'); await input.press('Enter');
  await page.getByText('已按所选品牌分析需求，缺少口径，需向客户确认。', { exact: true }).filter({ visible: true }).first().waitFor();
  assert.deepEqual(requests[0].businessContext, { agentId: 'sales-ab', skillId: 'quotation', brands: ['B'] });
  assert.equal(messages.length, 2);
  await page.screenshot({ path: `/private/tmp/inbrix-business-mail-${process.env.AI_TEST_MOBILE ? 'mobile' : 'desktop'}.png` });

  await page.goto(`${base}/ai-document`);
  // Use the existing assistant menu; task and brand controls are shared.
  await page.getByRole('button', { name: '默认文档助手', exact: false }).click();
  await page.getByRole('menuitem', { name: 'AB 销售助手', exact: true }).click();
  const contractSaved = page.waitForResponse(response => response.request().method() === 'PATCH' && response.request().postDataJSON().businessContext?.skillId === 'contract');
  await chooseSkill('销售合同制作');
  await contractSaved;
  await page.getByRole('button', { name: '品牌: 限定 A、B', exact: true }).waitFor();
  await page.reload();
  await page.getByRole('combobox', { name: '任务 Skill' }).filter({ hasText: '销售合同制作' }).waitFor();
  await page.getByRole('button', { name: 'AB 销售助手', exact: false }).waitFor();
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.screenshot({ path: `/private/tmp/inbrix-business-document-${process.env.AI_TEST_MOBILE ? 'mobile' : 'desktop'}.png` });

  await page.goto(`${base}/inbox?setup=1`);
  await page.getByRole('button', { name: '智能体', exact: true }).click();
  await page.getByRole('button', { name: '编辑智能体', exact: true }).click();
  await page.getByLabel('卖方公司', { exact: true }).fill('新的销售公司');
  await page.getByRole('combobox', { name: 'quotationTemplateId' }).click();
  await page.getByRole('option', { name: 'A 品牌报价模板', exact: true }).click();
  await page.screenshot({ path: `/private/tmp/inbrix-business-settings-${process.env.AI_TEST_MOBILE ? 'mobile' : 'desktop'}.png` });
  await page.getByRole('button', { name: '编辑智能体', exact: true }).last().click();
  await page.getByRole('heading', { name: '编辑智能体', exact: true }).waitFor({ state: 'hidden' });
  assert.equal(savedProfile.businessProfile.company, '新的销售公司');
  assert.equal(savedProfile.businessProfile.quotationTemplateId, 'quote-custom');
  // Retained legacy agent data has no influence on the mailbox's A/B scope.
  assert.deepEqual(savedProfile.businessProfile.brands, ['C']);
  assert.equal(await page.getByRole('combobox', { name: '品牌范围模式' }).count(), 0);
  await page.getByRole('button', { name: '邮箱管理', exact: true }).click();
  await page.getByRole('button', { name: '编辑账户', exact: true }).click();
  await page.getByRole('heading', { name: '邮箱品牌绑定', exact: true }).waitFor();
  await page.getByRole('button', { name: 'A、B', exact: true }).click();
  await page.getByRole('menuitemcheckbox', { name: 'A', exact: true }).click();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '保存品牌绑定', exact: true }).click();
  await page.getByText('邮箱品牌已保存', { exact: true }).waitFor();
  assert.deepEqual(savedMailboxBrands, { brands: ['B'], brandMode: 'restrict' });
  await page.screenshot({ path: `/private/tmp/inbrix-mailbox-brands-${process.env.AI_TEST_MOBILE ? 'mobile' : 'desktop'}.png` });
  await page.goto(`${base}/ai-document`);
  await page.getByRole('button', { name: '品牌: 限定 B', exact: true }).waitFor();
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  assert.deepEqual(errors, []);
  console.log('Passed: assistant settings, template defaults, shared task selectors, restricted brand choices, mail and document context persistence, chat request payload.');
} finally { await browser.close(); }
