import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { readFile } from 'node:fs/promises';

// Only run against an isolated backend explicitly identifying itself as a test
// harness. This check creates and deletes test products, never real user data.
const base = process.env.PRODUCT_TEST_URL || 'http://127.0.0.1:3097';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
page.on('pageerror', error => errors.push(error.message));
try {
  assert.equal((await (await page.request.get(`${base}/api/capabilities`)).json()).productTestHarness, true, 'An isolated product test harness is required');
  await page.goto(`${base}/products`);
  await page.getByRole('heading', { name: '产品资料库', exact: true }).waitFor();
  await page.getByRole('button', { name: '分类管理', exact: true }).click();
  await page.getByRole('textbox', { name: '分类名称' }).fill('控制阀');
  await page.getByRole('button', { name: '新增分类', exact: true }).click();
  await page.getByRole('button', { name: '查看 / 编辑 控制阀' }).waitFor();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '新建产品', exact: true }).click();
  await page.getByLabel('产品名称', { exact: true }).fill('测试控制阀');
  await page.getByLabel('型号', { exact: true }).fill('QA-42');
  await page.getByLabel('品牌', { exact: true }).fill('测试品牌');
  await page.getByRole('dialog').getByRole('combobox', { name: '分类', exact: true }).click();
  await page.getByRole('option', { name: '控制阀', exact: true }).click();
  await page.getByRole('dialog').getByRole('combobox', { name: '分类', exact: true }).filter({ hasText: '控制阀' }).waitFor();
  const specification = '## 技术规格\n支持电动执行器。\n\n## 使用限制\n压力与温度需联合核对，不能只看最大值。\n\n| 项目 | 条件 |\n| --- | --- |\n| 介质 | 蒸汽 |';
  await page.getByRole('textbox', { name: '产品说明与技术规格' }).fill(specification);
  const png = await page.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width = 160; canvas.height = 120;
    const ctx = canvas.getContext('2d'); ctx.fillStyle = '#08a080'; ctx.fillRect(10, 10, 140, 100);
    ctx.fillStyle = '#fff'; ctx.font = '24px Arial'; ctx.fillText('QA42', 35, 70);
    return canvas.toDataURL().split(',')[1];
  });
  await page.locator('input[type=file]').setInputFiles({ name: 'product.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') });
  await page.getByRole('tab', { name: '预览', exact: true }).click();
  await page.getByRole('cell', { name: '蒸汽', exact: true }).waitFor();
  assert.equal(await page.locator('[data-slot=tabs]').evaluate(element => getComputedStyle(element).flexDirection), 'column', 'Official shadcn orientation styles must be loaded');
  // Tabs and nested discard confirmation preserve the draft and keyboard focus.
  await page.getByRole('tab', { name: '预览', exact: true }).press('ArrowLeft');
  await page.keyboard.press('Space');
  await page.getByRole('textbox', { name: '产品说明与技术规格' }).waitFor();
  assert.equal(await page.getByRole('textbox', { name: '产品说明与技术规格' }).inputValue(), specification);
  await page.keyboard.press('Escape');
  await page.getByRole('alertdialog').waitFor();
  assert.equal(await page.getByRole('alertdialog').getByRole('button', { name: '取消', exact: true }).evaluate(element => element === document.activeElement), true);
  await page.getByRole('alertdialog').getByRole('button', { name: '取消', exact: true }).click();
  await page.getByRole('alertdialog').waitFor({ state: 'hidden' });
  assert.equal(await page.getByLabel('产品名称', { exact: true }).inputValue(), '测试控制阀');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  await page.getByRole('img', { name: '测试控制阀', exact: true }).waitFor();
  const categoryFilter = page.getByRole('combobox', { name: '分类', exact: true });
  await categoryFilter.focus();
  await categoryFilter.press('ArrowDown');
  await page.getByRole('option', { name: '控制阀 (1)', exact: true }).press('Enter');
  assert.equal(await categoryFilter.innerText(), '控制阀 (1)');
  await categoryFilter.click();
  await page.getByRole('option', { name: '全部分类', exact: true }).click();
  const search = page.getByRole('textbox', { name: '搜索型号、名称或规格关键词' });
  await search.fill('qa42');
  await page.waitForResponse(response => response.url().includes('/api/products?') && response.url().includes('qa42'));
  await page.getByRole('button', { name: '查看 / 编辑 测试控制阀', exact: true }).waitFor();
  await search.fill('电动');
  await page.waitForResponse(response => response.url().includes('/api/products?') && response.url().includes('%E7%94%B5%E5%8A%A8'));
  await page.getByRole('button', { name: '查看 / 编辑 测试控制阀', exact: true }).waitFor();
  await page.screenshot({ animations: 'disabled', path: '/tmp/inbrix-products-desktop.png' });
  const { products } = await (await page.request.get(`${base}/api/products?q=qa42`)).json();
  assert.equal(products.length, 1);
  const product = products[0];
  assert.equal(product.specification, undefined, 'Search must exclude the full specification');
  const read = await (await page.request.get(`${base}/api/products/${product.id}/read?version=1&length=12`)).json();
  assert.equal(Array.from(read.content).length, 12);
  assert.equal(read.hasMore, true);
  await page.getByRole('button', { name: '查看 / 编辑 测试控制阀', exact: true }).click();
  assert.equal(await page.getByRole('textbox', { name: '产品说明与技术规格' }).inputValue(), specification);
  await page.screenshot({ animations: 'disabled', path: '/tmp/inbrix-products-editor.png' });
  await page.getByRole('textbox', { name: '产品说明与技术规格' }).fill('## 技术规格\n只支持气动执行器。');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  const removedHit = await (await page.request.get(`${base}/api/products?q=电动`)).json();
  assert.equal(removedHit.total, 0, 'Old index entries must be removed on update');
  await search.fill('气动');
  await page.getByRole('button', { name: '查看 / 编辑 测试控制阀', exact: true }).waitFor();
  await page.reload();
  await page.getByRole('button', { name: '查看 / 编辑 测试控制阀', exact: true }).waitFor();
  const snapshots = await page.evaluate(async product => {
    const { convertGeneratedToStoredDocument } = await import('/src/lib/ai-document-storage.ts');
    const { collectProductImageValues } = await import('/src/components/app/product-document-images.ts');
    const docs = ['quotation', 'contract'].map(type => convertGeneratedToStoredDocument({
      id: `product-image-${type}`, type, name: `Product ${type}`,
      items: [{ productId: product.id, productVersion: 1, model: product.model, picture: product.imageUrl, description: 'Product image snapshot', qty: '1', price: '100', amount: '100' }],
    }));
    for (const doc of docs) {
      const response = await fetch('/api/documents', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(doc) });
      if (!response.ok) throw new Error('Unable to save document snapshot');
    }
    return docs.map(doc => {
      const payload = JSON.parse(doc.html.slice('__INBRIX_CANVAS_DOCUMENT__:'.length));
      const values = {};
      collectProductImageValues(payload.data.main, values);
      return { type: doc.type, picture: values.item_picture, productId: values.item_product_id };
    });
  }, product);
  assert.deepEqual(snapshots.map(snapshot => snapshot.picture), [product.imageUrl, product.imageUrl]);
  assert(snapshots.every(snapshot => snapshot.productId === product.id));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ animations: 'disabled', path: '/tmp/inbrix-products-mobile.png' });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'Mobile page must not overflow');
  await page.getByRole('button', { name: '查看 / 编辑 测试控制阀', exact: true }).click();
  await page.getByLabel('产品名称', { exact: true }).waitFor();
  await page.screenshot({ animations: 'disabled', path: '/tmp/inbrix-products-mobile-editor.png' });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '删除 测试控制阀', exact: true }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: '删除', exact: true }).click();
  await page.getByRole('alertdialog').waitFor({ state: 'hidden' });
  assert.equal((await (await page.request.get(`${base}/api/products`)).json()).total, 0);
  assert.equal((await page.request.get(`${base}${product.imageUrl}`)).status(), 200, 'A document image snapshot must survive product deletion');
  await page.getByRole('button', { name: '分类管理', exact: true }).click();
  await page.getByRole('button', { name: '查看 / 编辑 控制阀', exact: true }).click();
  await page.getByRole('textbox', { name: '分类名称' }).fill('调节阀');
  await page.getByRole('dialog').getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('button', { name: '删除 调节阀', exact: true }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: '删除', exact: true }).click();
  await page.getByRole('button', { name: '删除 调节阀', exact: true }).waitFor({ state: 'hidden' });
  await page.getByRole('alertdialog').waitFor({ state: 'hidden' });
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '新建产品', exact: true }).click();
  await page.getByLabel('产品名称', { exact: true }).fill('放弃的草稿');
  await page.keyboard.press('Escape');
  await page.getByRole('alertdialog').getByRole('button', { name: '放弃修改', exact: true }).click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  await page.getByRole('alertdialog').waitFor({ state: 'hidden' });
  assert.equal((await (await page.request.get(`${base}/api/products`)).json()).total, 0);

  // Exercise real server pagination, including disabled links and mobile layout.
  const paginationProducts = [];
  for (let i = 0; i < 13; i++) {
    const response = await page.request.post(`${base}/api/products`, { data: { name: `分页产品 ${i}`, specification: '分页验证', version: 0 } });
    assert.equal(response.ok(), true);
    paginationProducts.push((await response.json()).product);
  }
  await page.reload();
  await page.getByRole('button', { name: /^删除 分页产品/ }).first().waitFor();
  const previous = page.locator('[data-slot=pagination-link][aria-label="上一页"]');
  const next = page.locator('[data-slot=pagination-link][aria-label="下一页"]');
  assert.equal(await previous.getAttribute('aria-disabled'), 'true');
  assert.equal(await page.getByRole('button', { name: /^删除 分页产品/ }).count(), 12);
  await next.click();
  await page.waitForResponse(response => response.url().includes('/api/products?') && response.url().includes('offset=12'));
  await page.locator('[aria-current=page]').filter({ hasText: '2' }).waitFor();
  assert.equal(await page.getByRole('button', { name: /^删除 分页产品/ }).count(), 1);
  assert.equal(await next.getAttribute('aria-disabled'), 'true');
  await previous.click();
  await page.locator('[aria-current=page]').filter({ hasText: '1' }).waitFor();
  assert.equal(await page.getByRole('button', { name: /^删除 分页产品/ }).count(), 12);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  for (const item of paginationProducts) {
    assert.equal((await page.request.delete(`${base}/api/products/${item.id}?version=${item.version}`)).ok(), true);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  for (const type of ['quotation', 'contract']) {
    await page.goto(`${base}/documents/product-image-${type}`);
    const editor = page.getByTestId('document-editor-dialog');
    await editor.locator('canvas').first().waitFor();
    await editor.getByRole('button', { name: '导出', exact: true }).click();
    const downloading = page.waitForEvent('download');
    await page.getByRole('menuitem', { name: '下载图片', exact: true }).click();
    const download = await downloading;
    assert.equal(await download.failure(), null);
    const pixels = await page.evaluate(async base64 => {
      const image = new Image(); image.src = `data:image/jpeg;base64,${base64}`; await image.decode();
      const canvas = document.createElement('canvas'); canvas.width = image.width; canvas.height = image.height;
      const ctx = canvas.getContext('2d'); ctx.drawImage(image, 0, 0);
      const { data } = ctx.getImageData(0, 0, canvas.width, canvas.height);
      let count = 0;
      for (let i = 0; i < data.length; i += 4) if (data[i] < 40 && data[i + 1] > 130 && data[i + 1] < 185 && data[i + 2] > 90 && data[i + 2] < 155) count++;
      return count;
    }, (await readFile(await download.path())).toString('base64'));
    assert(pixels > 1000, `${type}: stored product image must be present in exported output after product deletion`);
    await page.screenshot({ animations: 'disabled', path: `/tmp/inbrix-product-image-${type}.png` });
  }
  assert.deepEqual(errors, []);
  console.log('Product library desktop/mobile CRUD, search, index refresh, images and quotation/contract snapshots passed.');
} finally {
  await browser.close();
}
