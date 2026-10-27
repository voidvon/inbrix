import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const base = process.env.AI_TEST_URL || 'http://127.0.0.1:2342';
const browser = await chromium.launch({ headless: true });
try {
  for (const mobile of [false, true]) {
    const page = await browser.newPage({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 } });
    const errors = [];
    const writes = [];
    const models = [{ id: 'chat-model', provider: 'openai-compatible', baseUrl: 'https://api.deepseek.com', model: 'custom-vision', reasoningEffort: 'medium', extraParams: '{}', isDefault: true, supportsImages: false }];
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(() => { window.EventSource = class { close() {} }; });
    await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
    await page.route('**/api/**', async route => {
      const request = route.request();
      const { pathname } = new URL(request.url());
      let json = { models: [], agents: [], bindings: [], documents: [], signatures: [], attachments: [], drafts: [], accounts: [], total: 0 };
      if (pathname === '/api/conversations') json = { locale: 'zh-CN', conversations: [], folders: [], accounts: [] };
      if (pathname === '/api/settings/ai/models') {
        if (request.method() === 'POST') {
          const input = request.postDataJSON();
          writes.push(input);
          json = { ...input, id: 'new-model', isDefault: false };
          models.push(json);
        } else json = { models };
      }
      if (pathname === '/api/settings/ai/models/chat-model') {
        const input = request.postDataJSON();
        writes.push(input);
        Object.assign(models[0], input);
        json = models[0];
      }
      await route.fulfill({ json });
    });

    const openModels = async () => {
      await page.goto(`${base}/inbox?setup=1`);
      await page.getByRole('button', { name: '模型管理', exact: true }).click();
      await page.getByRole('button', { name: '编辑模型', exact: true }).first().click();
      return page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '编辑模型', exact: true }) });
    };
    let dialog = await openModels();
    let toggle = dialog.getByRole('switch', { name: '支持图片', exact: true });
    assert.equal(await toggle.isChecked(), false, 'Editing reads the saved capability');
    await toggle.click();
    await dialog.getByRole('button', { name: '编辑模型', exact: true }).click();
    await dialog.waitFor({ state: 'hidden' });
    assert.equal(writes.at(-1).supportsImages, true);
    assert.equal(writes.at(-1).provider, 'openai-compatible', 'Capability changes preserve the selected API format');
    assert.equal(writes.at(-1).apiKey, '', 'Editing keeps the saved API key');

    dialog = await openModels();
    toggle = dialog.getByRole('switch', { name: '支持图片', exact: true });
    assert.equal(await toggle.isChecked(), true, 'Capability survives reload');
    await toggle.click();
    await dialog.getByRole('button', { name: '编辑模型', exact: true }).click();
    await dialog.waitFor({ state: 'hidden' });
    assert.equal(writes.at(-1).supportsImages, false, 'Explicit false is sent');

    await page.getByTestId('settings-dialog').getByRole('button', { name: '新增模型', exact: true }).click();
    dialog = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '新增模型', exact: true }) });
    toggle = dialog.getByRole('switch', { name: '支持图片', exact: true });
    assert.equal(await toggle.isChecked(), true, 'New compatible models keep image support by default');
    for (const format of ['Google Gemini', 'OpenAI Responses', 'OpenAI Chat Completions']) {
      await dialog.getByRole('combobox', { name: 'API 格式', exact: true }).click();
      await page.getByRole('option', { name: format, exact: true }).click();
      assert.equal(await toggle.isChecked(), true, 'Image capability is independent of API format');
    }
    await toggle.click();
    await dialog.getByLabel('API Key', { exact: true }).fill('fake-test-key');
    await dialog.getByRole('button', { name: '新增模型', exact: true }).click();
    await dialog.waitFor({ state: 'hidden' });
    assert.equal(writes.at(-1).supportsImages, false, 'New text models can disable images');
    assert.deepEqual(errors, []);
    await page.close();
    console.log(`Passed (${mobile ? 'mobile' : 'desktop'}): create/edit image capability, reload, API format/key preservation, and all API formats.`);
  }
} finally {
  await browser.close();
}
