import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

// Every API is mocked: no production settings, AI requests or notifications.
const base = process.env.IGNORE_TEST_URL || 'http://127.0.0.1:2351';
const browser = await chromium.launch({ headless: true });
await mkdir('output/mail-ignore-rules', { recursive: true });
try {
  for (const mobile of [false, true]) {
    const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
    const errors = [], writes = [], previews = [];
    let accountWrites = 0, rejectSave = false;
    let rules = [{ id: 'tencent-interception-notice', name: '腾讯企业邮箱拦截通知', enabled: true, conditions: [{ field: 'subject', operator: 'equals', value: '系统邮件拦截通知' }, { field: 'message_id_domain', operator: 'equals', value: 'exmail.weixin.qq.com' }], actions: ['analysis', 'reply', 'notification'] }];
    const account = { id: 'account', email: 'sales@example.com', label: 'Sales', imap_server: 'imap.example.com', imap_port: 993, smtp_server: 'smtp.example.com', smtp_port: 465 };
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(() => { window.EventSource = class { close() {} }; });
    await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
    await page.route('**/v1/**', route => route.fulfill({ json: { signatures: [], templates: [], preferences: {} } }));
    await page.route('**/api/**', async route => {
      const request = route.request(), { pathname } = new URL(request.url());
      let json = { models: [], agents: [], bindings: [], documents: [], signatures: [], drafts: [], accounts: [], templates: [], brands: [], categories: [], total: 0 };
      if (pathname === '/api/conversations') json = { locale: 'zh-CN', conversations: [], folders: [], accounts: [{ ...account, isActive: true }], accountEmail: account.email };
      if (pathname === '/api/capabilities') json = { role: 'user', notifications: false, calendar: false };
      if (pathname === '/api/accounts') json = [account];
      if (pathname === '/api/accounts/account' && request.method() !== 'GET') accountWrites++;
      if (pathname === '/api/accounts/account/brands') json = { brands: [], brandMode: 'prefer' };
      if (pathname === '/api/accounts/account/feishu-webhook') json = { enabled: true, url: 'https://open.feishu.cn/open-apis/bot/v2/hook/fake-test' };
      if (pathname === '/api/accounts/account/automation-ignore-rules') {
        if (request.method() === 'PUT') {
          if (rejectSave) { await route.fulfill({ status: 500, json: { error: '测试保存失败' } }); return; }
          rules = request.postDataJSON().rules;
          writes.push(structuredClone(rules));
        }
        json = { rules };
      }
      if (pathname === '/api/accounts/account/automation-ignore-rules/preview') {
        previews.push(request.postDataJSON().rules);
        json = { total: 20, scanned: 21, messages: [{ folder: 'INBOX', messageId: '860', from: '', subject: '系统邮件拦截通知', date: '2026-11-11T03:52:01Z', matches: ['analysis', 'reply', 'notification'].map(action => ({ ruleId: rules[0].id, ruleName: rules[0].name, action })) }] };
      }
      await route.fulfill({ json });
    });

    const open = async () => {
      await page.goto(`${base}/inbox?setup=1`);
      await page.getByRole('button', { name: '邮箱管理', exact: true }).click();
      await page.getByRole('button', { name: '编辑账户', exact: true }).click();
      assert.equal(await page.getByTestId('ignore-rule').count(), 0, 'Rules stay out of the account editor');
      await page.getByRole('button', { name: '管理忽略规则', exact: true }).click();
      await page.getByRole('dialog', { name: '自动处理忽略规则', exact: true }).waitFor();
      await page.getByTestId('ignore-rule').first().waitFor();
    };
    await open();
    const panel = page.getByTestId('mail-ignore-rules');
    await page.keyboard.press('Escape');
    await page.getByRole('dialog', { name: '自动处理忽略规则', exact: true }).waitFor({ state: 'hidden' });
    assert.equal(await page.getByRole('dialog', { name: '编辑账户', exact: true }).isVisible(), true, 'Closing rules keeps account editor open');
    await page.getByRole('button', { name: '管理忽略规则', exact: true }).click();
    await panel.waitFor();
    assert.equal(await panel.getByRole('switch', { name: '启用规则: 腾讯企业邮箱拦截通知', exact: true }).isChecked(), true);
    await panel.getByRole('button', { name: '预览匹配邮件', exact: true }).click();
    await panel.getByText('当前规则匹配 20 封邮件，已检查收件箱缓存中的 21 封邮件。', { exact: true }).waitFor();
    assert.equal(writes.length, 0, 'Preview must not save');
    assert.equal(previews.length, 1);
    assert.equal(previews[0][0].conditions.length, 2, 'Tencent signature requires both conditions');
    await panel.scrollIntoViewIfNeeded();
    await page.screenshot({ path: `output/mail-ignore-rules/${mobile ? 'mobile' : 'desktop'}.png` });
    assert.equal(await panel.evaluate(element => element.scrollWidth <= element.clientWidth + 1), true, 'Rule editor fits the viewport');

    await panel.getByRole('switch', { name: '启用规则: 腾讯企业邮箱拦截通知', exact: true }).click();
    rejectSave = true;
    await panel.getByRole('button', { name: '保存忽略规则', exact: true }).click();
    await panel.getByRole('alert').getByText('测试保存失败').waitFor();
    assert.equal(await panel.getByRole('switch', { name: '启用规则: 腾讯企业邮箱拦截通知', exact: true }).isChecked(), false, 'Failed save preserves draft');
    rejectSave = false;
    await panel.getByRole('button', { name: '保存忽略规则', exact: true }).click();
    await page.waitForFunction(() => !document.querySelector('[data-testid="mail-ignore-rules"]')?.textContent.includes('修改尚未保存'));
    assert.equal(writes.at(-1)[0].enabled, false);
    assert.equal(accountWrites, 0, 'Ignore rule operations must not submit mailbox credentials');
    await open();
    assert.equal(await panel.getByRole('switch', { name: '启用规则: 腾讯企业邮箱拦截通知', exact: true }).isChecked(), false, 'Saved toggle survives reload');

    await panel.getByRole('button', { name: '添加忽略规则', exact: true }).click();
    const senderRule = panel.getByTestId('ignore-rule').nth(1);
    await senderRule.getByRole('textbox', { name: '规则名称', exact: true }).fill('忽略推广邮箱');
    await senderRule.getByRole('textbox', { name: '匹配内容', exact: true }).fill('marketing@example.com');
    await senderRule.getByRole('switch', { name: '跳过 AI 总结', exact: true }).click();
    await senderRule.getByRole('switch', { name: '跳过回复建议', exact: true }).click();
    await panel.getByRole('button', { name: '保存忽略规则', exact: true }).click();
    await page.waitForFunction(() => !document.querySelector('[data-testid="mail-ignore-rules"]')?.textContent.includes('修改尚未保存'));
    assert.deepEqual(writes.at(-1)[1].actions, ['notification'], 'Actions can be configured independently');
    assert.equal(writes.at(-1)[1].conditions[0].field, 'sender');
    for (const name of ['腾讯企业邮箱拦截通知', '忽略推广邮箱']) await panel.getByRole('button', { name: `删除规则: ${name}`, exact: true }).click();
    await panel.getByRole('button', { name: '保存忽略规则', exact: true }).click();
    await panel.getByText('暂无忽略规则，邮件按正常流程自动处理。', { exact: true }).waitFor();
    await page.waitForFunction(() => !document.querySelector('[data-testid="mail-ignore-rules"]')?.textContent.includes('修改尚未保存'));
    assert.deepEqual(writes.at(-1), []);
    assert.equal(accountWrites, 0);
    assert.deepEqual(errors, []);
    await page.close();
    console.log(`Passed (${mobile ? 'mobile' : 'desktop'}): preview, unsaved changes, save failure/retry, persistence, per-action rules, deletion, viewport and no mailbox submissions.`);
  }
} finally { await browser.close(); }
