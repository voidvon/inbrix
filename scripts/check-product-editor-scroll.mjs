import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// This fixture only belongs in the disposable product-library-harness.go database.
const base = process.env.PRODUCT_TEST_URL || 'http://127.0.0.1:3097';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
try {
  assert.equal((await (await page.request.get(`${base}/api/capabilities`)).json()).productTestHarness, true, 'An isolated product test harness is required');
  const specification = Array.from({ length: 80 }, (_, index) => `## 规格章节 ${index + 1}\n\n长规格用于验证产品编辑与 Markdown 预览的滚动。\n\n| 参数 | 限制 |\n| --- | --- |\n| 压力 | 根据温度与连接方式联合核对 |`).join('\n\n');
  const response = await page.request.post(`${base}/api/products`, { data: { name: '长规格滚动验证', model: 'SCROLL-QA', specification, version: 0 } });
  assert(response.ok());
  const { product } = await response.json();
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 1280, height: 600 }, { width: 390, height: 844 }, { width: 844, height: 390 }]) {
    await page.setViewportSize(viewport);
    await page.goto(`${base}/products?product=${product.id}`);
    const dialog = page.getByRole('dialog');
    const editor = dialog.getByRole('textbox', { name: '产品说明与技术规格' });
    await editor.waitFor();
    assert.equal(await editor.inputValue(), specification);
    const scroll = dialog.locator('[data-slot=scroll-area-viewport]');
    for (const mode of ['edit', 'preview']) {
      if (mode === 'preview') {
        await dialog.getByRole('tab', { name: '预览', exact: true }).click();
        await dialog.getByRole('heading', { name: '规格章节 80', exact: true }).waitFor({ state: 'attached' });
      }
      await scroll.evaluate(element => { element.scrollTop = 0; });
      await page.waitForFunction(() => {
        const viewport = document.querySelector('[role=dialog] [data-slot=scroll-area-viewport]');
        return viewport && viewport.scrollHeight > viewport.clientHeight + 100;
      }, null, { timeout: 3000 });
      const bounds = await scroll.boundingBox();
      assert(bounds && bounds.height > 40, `${mode}: scroll viewport needs usable height`);
      assert(bounds.y >= 0 && bounds.y + bounds.height <= viewport.height, `${mode}: scroll viewport must fit the screen`);
      await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2);
      await page.mouse.wheel(0, 500);
      await page.waitForFunction(() => document.querySelector('[role=dialog] [data-slot=scroll-area-viewport]').scrollTop > 0, null, { timeout: 3000 });
      await scroll.evaluate(element => { element.scrollTop = element.scrollHeight; });
      if (mode === 'preview') await dialog.getByRole('heading', { name: '规格章节 80', exact: true }).scrollIntoViewIfNeeded();
      const bottom = await scroll.evaluate(element => element.scrollTop);
      await page.mouse.wheel(0, -300);
      await page.waitForFunction(previous => document.querySelector('[role=dialog] [data-slot=scroll-area-viewport]').scrollTop < previous, bottom, { timeout: 3000 });
      for (const control of [dialog.getByRole('heading', { name: '查看 / 编辑', exact: true }), dialog.getByRole('button', { name: '保存', exact: true })]) {
        const box = await control.boundingBox();
        assert(box && box.y >= 0 && box.y + box.height <= viewport.height, `${mode}: title and save button must remain visible`);
      }
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
      await page.screenshot({ animations: 'disabled', path: `/tmp/inbrix-product-scroll-${viewport.width}-${viewport.height}-${mode}.png` });
    }
  }
  assert.deepEqual(errors, []);
  console.log('Long product Markdown scrolls up/down in edit and preview at desktop, short-window, mobile and landscape sizes.');
} finally {
  await browser.close();
}
