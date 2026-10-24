import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Exercise the settings UI against a revision-aware mock. Real API Key
// authentication and owner isolation are tested by the Go HTTP integration tests.
const mobile = Boolean(process.env.AI_TEST_MOBILE);
const base = process.env.COMPOSE_TEST_URL || 'http://127.0.0.1:3097';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
const now = new Date().toISOString();
const account = { id: 'sales', email: 'sales@example.com', label: 'Sales', isActive: true };
const defaults = [
  { id: 'email_analysis', name: '邮件需求分析', kind: 'chat', dependsOn: [] },
  { id: 'quotation', name: '产品选型与报价', kind: 'chat', dependsOn: ['email_analysis'] },
  { id: 'contract', name: '销售合同制作', kind: 'chat', dependsOn: ['email_analysis'] },
  { id: 'mail-analysis', name: '邮件总结', kind: 'mail_summary', dependsOn: [] },
];
let skills = defaults.map(skill => ({ description: 'Reusable workflow', instructions: 'Read source evidence before summarizing.', enabled: true, builtIn: true, revision: 1, createdAt: now, updatedAt: now, ...skill }));
let summary = 'mail-analysis';
const versions = new Map();
const remember = skill => {
  if (!versions.has(skill.id)) versions.set(skill.id, new Map());
  versions.get(skill.id).set(skill.revision, { skill: structuredClone(skill), action: 'save', source: 'browser', recordedAt: now });
};
skills.forEach(remember);
const requests = [], errors = [];
page.on('pageerror', error => errors.push(error.stack));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-csrf' } }));
await page.route('**/api/**', async route => {
  const req = route.request();
  const path = new URL(req.url()).pathname;
  const method = req.method();
  if (path === '/api/settings/ai/skills') {
    if (method === 'POST') {
      const input = req.postDataJSON(); requests.push({ method, path, input });
      const skill = { ...input, id: input.id || `custom-${skills.length}`, enabled: input.enabled ?? true, dependsOn: input.dependsOn || [], builtIn: false, revision: 1, createdAt: now, updatedAt: now };
      skills.push(skill);
      remember(skill);
      await route.fulfill({ status: 201, json: skill });
    } else await route.fulfill({ json: { skills, mailSummarySkillId: summary } });
    return;
  }
  const historyMatch = path.match(/^\/api\/settings\/ai\/skills\/([^/]+)\/(versions(?:\/(\d+))?|restore)$/);
  if (historyMatch) {
    const skill = skills.find(item => item.id === historyMatch[1]);
    const history = versions.get(historyMatch[1]);
    if (!history) { await route.fulfill({ status: 404, json: { error: 'Not found' } }); return; }
    if (historyMatch[2] === 'restore') {
      const input = req.postDataJSON();
      requests.push({ method, path, input });
      if (input.expectedRevision !== skill.revision) { await route.fulfill({ status: 409, json: { error: 'Revision conflict' } }); return; }
      Object.assign(skill, structuredClone(history.get(input.revision).skill), { revision: skill.revision + 1 });
      remember(skill);
      await route.fulfill({ json: skill });
    } else if (historyMatch[3]) await route.fulfill({ json: history.get(Number(historyMatch[3])) });
    else {
      const before = Number(new URL(req.url()).searchParams.get('before')) || Infinity;
      await route.fulfill({ json: { versions: [...history.values()].filter(version => version.skill.revision < before).sort((a, b) => b.skill.revision - a.skill.revision).slice(0, 50) } });
    }
    return;
  }
  const match = path.match(/^\/api\/settings\/ai\/skills\/([^/]+)(\/use-for-mail-summary)?$/);
  if (match) {
    const skill = skills.find(item => item.id === match[1]);
    if (!skill) { await route.fulfill({ status: 404, json: { error: 'Skill not found' } }); return; }
    const input = method === 'DELETE' ? null : req.postDataJSON();
    requests.push({ method, path, input });
    if (input?.revision && input.revision !== skill.revision) { await route.fulfill({ status: 409, json: { error: 'Skill 已被修改，请刷新后重试' } }); return; }
    if (match[2]) { summary = skill.id; await route.fulfill({ json: { mailSummarySkillId: summary } }); }
    else if (method === 'PATCH') { Object.assign(skill, input, { revision: skill.revision + 1 }); remember(skill); await route.fulfill({ json: skill }); }
    else if (method === 'DELETE') { skills = skills.filter(item => item.id !== skill.id); await route.fulfill({ json: { ok: true } }); }
    else await route.fulfill({ json: skill });
    return;
  }
  let json;
  if (path === '/api/accounts') json = { accounts: [account] };
  else if (path === '/api/conversations') json = { locale: 'zh-CN', accountEmail: account.email, accounts: [account], conversations: [], folders: [] };
  else if (path === '/api/capabilities') json = { role: 'user', calendar: false, notifications: false };
  else if (path === '/api/settings/ai/agents') json = { agents: [], skills: skills.filter(item => item.kind === 'chat' && item.enabled) };
  else json = { accounts: [account], conversations: [], folders: [], drafts: [], agents: [], models: [], signatures: [], bindings: [], documents: [], attachments: [], total: 0 };
  await route.fulfill({ json });
});

const row = id => page.locator(`[data-skill-id="${id}"]`);
const editor = () => page.getByRole('dialog').last();
try {
  await page.goto(`${base}/inbox?setup=1`);
  await page.getByRole('button', { name: 'Skill 管理', exact: true }).click();
  await page.getByTestId('skill-settings').waitFor();
  await row('mail-analysis').getByText('当前邮件总结', { exact: true }).waitFor();
  assert.equal(await row('mail-analysis').getByRole('switch').isEnabled(), false);

  await page.getByRole('button', { name: '新增 Skill', exact: true }).click();
  await editor().getByLabel('名称', { exact: true }).fill('客户跟进');
  await editor().getByLabel('标识', { exact: true }).fill('customer-follow-up');
  await editor().getByLabel('说明', { exact: true }).fill('核对客户的待办和承诺');
  await editor().getByLabel('流程指令', { exact: true }).fill('Read the latest email and identify pending actions with sources.');
  await page.screenshot({ path: `/private/tmp/inbrix-skill-editor-${mobile ? 'mobile' : 'desktop'}.png` });
  await editor().getByRole('button', { name: '保存', exact: true }).click();
  await row('customer-follow-up').waitFor();

  await row('customer-follow-up').getByRole('button', { name: '编辑 Skill 客户跟进', exact: true }).click();
  // An API client changes the same record after the form was opened.
  const current = skills.find(skill => skill.id === 'customer-follow-up');
  current.instructions = 'An external API client changed this workflow.';
  current.revision++;
  remember(current);
  await editor().getByLabel('流程指令', { exact: true }).fill('Keep this unsaved edit on conflict.');
  await editor().getByRole('button', { name: '保存', exact: true }).click();
  await page.getByText('Skill 已被修改，请刷新后重试', { exact: true }).first().waitFor();
  assert.equal(await editor().getByLabel('流程指令', { exact: true }).inputValue(), 'Keep this unsaved edit on conflict.');
  await editor().getByRole('button', { name: '取消', exact: true }).click();
  await page.getByTestId('skill-settings').getByRole('button', { name: '重试', exact: true }).click();
  await row('customer-follow-up').getByText('customer-follow-up · v2', { exact: true }).waitFor();
  await row('customer-follow-up').getByRole('button', { name: '编辑 Skill 客户跟进', exact: true }).click();
  await editor().getByLabel('流程指令', { exact: true }).fill('Updated workflow: verify all promised dates against source emails.');
  await editor().getByRole('button', { name: '保存', exact: true }).click();
  await row('customer-follow-up').getByText('customer-follow-up · v3', { exact: true }).waitFor();
  await row('customer-follow-up').getByRole('button', { name: '版本历史', exact: true }).click();
  await editor().locator('summary').filter({ hasText: /^v1 ·/ }).click();
  assert.equal(await editor().locator('details[open] pre').textContent(), 'Read the latest email and identify pending actions with sources.');
  await page.screenshot({ path: `/private/tmp/inbrix-skill-history-${mobile ? 'mobile' : 'desktop'}.png`, animations: 'disabled' });
  await editor().getByRole('button', { name: '恢复此版本', exact: true }).click();
  await row('customer-follow-up').getByText('customer-follow-up · v4', { exact: true }).waitFor();
  assert.equal(skills.find(skill => skill.id === 'customer-follow-up').instructions, 'Read the latest email and identify pending actions with sources.');
  await row('customer-follow-up').getByRole('switch', { name: '启用 客户跟进', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('[data-skill-id="customer-follow-up"] [role="switch"]').getAttribute('aria-checked') === 'false');

  await page.getByRole('button', { name: '新增 Skill', exact: true }).click();
  await editor().getByLabel('名称', { exact: true }).fill('新的邮件总结');
  await editor().getByLabel('标识', { exact: true }).fill('new-summary');
  await editor().getByRole('combobox', { name: '用途', exact: true }).click();
  await page.getByRole('option', { name: '邮件总结', exact: true }).click();
  await editor().getByLabel('流程指令', { exact: true }).fill('Read related correspondence and summarize changes and next actions.');
  await editor().getByRole('button', { name: '保存', exact: true }).click();
  await row('new-summary').getByRole('button', { name: '用于邮件总结', exact: true }).click();
  await row('new-summary').getByText('当前邮件总结', { exact: true }).waitFor();
  assert.equal(summary, 'new-summary');
  await row('mail-analysis').getByRole('button', { name: '删除 Skill 邮件总结', exact: true }).click();
  await editor().getByRole('button', { name: '删除 Skill', exact: true }).click();
  await row('mail-analysis').waitFor({ state: 'detached' });
  await page.reload();
  await page.getByRole('button', { name: 'Skill 管理', exact: true }).click();
  await row('new-summary').getByText('当前邮件总结', { exact: true }).waitFor();
  assert.equal(await row('mail-analysis').count(), 0, 'Deleted built-ins must not reappear in the list');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.screenshot({ path: `/private/tmp/inbrix-skills-${mobile ? 'mobile' : 'desktop'}.png` });
  assert.ok(requests.some(request => request.method === 'PATCH' && request.input.revision === 2));
  assert.ok(requests.some(request => request.method === 'DELETE'));
  assert.ok(requests.some(request => request.path.endsWith('/restore') && request.input.expectedRevision === 3));
  assert.deepEqual(errors, []);
  console.log(`Passed (${mobile ? 'mobile' : 'desktop'}): skill CRUD, revision conflict, history inspection, restore, enabled state, summary binding, persistent deletion and layout.`);
} finally { await browser.close(); }
