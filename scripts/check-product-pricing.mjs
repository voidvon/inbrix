import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const base = process.env.PRODUCT_TEST_URL || 'http://127.0.0.1:3097';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = []; page.on('pageerror', error => errors.push(error.message));
try {
  assert.equal((await (await page.request.get(`${base}/api/capabilities`)).json()).productTestHarness, true);
  await page.goto(`${base}/products`);
  await page.getByRole('heading', { name: '产品库', exact: true }).waitFor();
  assert.equal(await page.getByRole('link', { name: '价格表', exact: true }).count(), 0);
  async function choose(label, option, last = false) {
    const input = page.getByRole('combobox', { name: label, exact: true });
    await (last ? input.last() : input).click();
    await page.getByRole('option', { name: option, exact: true }).click();
  }
  await page.getByRole('button', { name: '全局规格维度', exact: true }).click();
  const manager = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '全局规格维度', exact: true }) });
  for (const [name, type, unit, options] of [['连接方式', '选项', '', '法兰\n螺纹'], ['口径', '数字', 'mm', ''], ['材质', '文本', '', '']]) {
    await page.getByRole('button', { name: '新建全局维度', exact: true }).click();
    const dialog = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: '新建全局维度', exact: true }) });
    await dialog.getByLabel('维度名称', { exact: true }).fill(name);
    await choose('数据类型', type);
    await dialog.getByLabel('规格单位（可选）', { exact: true }).fill(unit);
    if (options) await dialog.getByLabel('可选值（每行一项）', { exact: true }).fill(options);
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    await dialog.waitFor({ state: 'hidden' });
    assert.equal((await (await page.request.get(`${base}/api/products?q=BV-QA`)).json()).total, 0, 'Creating a shared dimension must not submit the parent product form');
  }
  await page.keyboard.press('Escape');
  await manager.waitFor({ state: 'hidden' });
  await page.getByRole('button', { name: '新建产品', exact: true }).click();
  await page.getByLabel('产品名称', { exact: true }).fill('多规格球阀');
  await page.getByLabel('型号', { exact: true }).fill('BV-QA');
  await page.getByRole('tab', { name: '规格与价格', exact: true }).click();
  assert.equal(await page.getByRole('button', { name: '新建全局维度', exact: true }).count(), 0);
  for (const label of ['连接方式', '口径', '材质']) {
    await page.getByRole('button', { name: '添加维度', exact: true }).click();
    await page.getByRole('menuitem').filter({ hasText: label }).click();
    await page.getByRole('menu').waitFor({ state: 'hidden' });
  }
  await page.getByRole('button', { name: '添加规格', exact: true }).click();
  assert.equal(await page.getByRole('columnheader', { name: /^(币种|计价单位)$/ }).count(), 0);
  await page.getByLabel('规格编号 / SKU 1', { exact: true }).fill('BV-F50');
  await choose('连接方式 1', '法兰');
  await page.getByLabel('口径 1', { exact: true }).fill('50');
  await page.getByLabel('材质 1', { exact: true }).fill('304');
  await page.getByLabel('单价（元） 1', { exact: true }).fill('1280.00');
  assert.equal(await page.getByRole('button', { name: /价格条件|添加价格|导入.*CSV|导出.*CSV/ }).count(), 0);
  assert.equal(await page.locator('input[accept*="csv"]').count(), 0);
  await page.getByRole('button', { name: '添加规格', exact: true }).click();
  await page.getByLabel('规格编号 / SKU 2', { exact: true }).fill('BV-T25');
  await choose('连接方式 2', '螺纹');
  await page.getByLabel('口径 2', { exact: true }).fill('25');
  await page.getByLabel('材质 2', { exact: true }).fill('316');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  const search = await (await page.request.get(`${base}/api/products?q=BV-QA`)).json();
  assert.equal(search.total, 1);
  let product = (await (await page.request.get(`${base}/api/products/${search.products[0].id}`)).json()).product;
  assert.equal(product.catalog.variants.length, 2);
  assert.equal(product.catalog.variants[0].prices.length, 1);
  assert.equal(product.catalog.variants[0].prices[0].currency, 'CNY');
  assert.equal(Object.hasOwn(product.catalog.variants[0].prices[0], 'unit'), false);
  const attrs = product.catalog.variants[0].attributes;
  const query = async body => (await page.request.post(`${base}/api/products/query-prices`, { data: { productId: product.id, ...body } })).json();
  const ambiguous = await query({ limit: 1 });
  assert.equal(ambiguous.status, 'needs_clarification'); assert.equal(ambiguous.total, 2); assert.equal(ambiguous.hasMore, true);
  const priced = await query({ attributes: attrs, qty: '10' });
  assert.equal(priced.status, 'matched'); assert.equal(priced.matches[0].prices[0].amount, '1280.00');
  assert.equal((await query({ attributes: product.catalog.variants[1].attributes })).status, 'no_price');
  assert.equal((await query({ attributes: { ...attrs, [product.catalog.dimensions[2].key]: '316' } })).status, 'no_match');
  await page.getByRole('button', { name: '查看 / 编辑 多规格球阀', exact: true }).click();
  await page.getByRole('tab', { name: '规格与价格', exact: true }).click();
  assert.equal(await page.getByLabel('材质 1', { exact: true }).inputValue(), '304');
  assert.equal(await page.getByLabel('单价（元） 1', { exact: true }).inputValue(), '1280.00');
  // Clearing a price means unpriced, while an explicit zero remains a price.
  await page.getByLabel('单价（元） 1', { exact: true }).fill('');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  assert.equal((await query({ attributes: attrs })).status, 'no_price');
  await page.getByRole('button', { name: '查看 / 编辑 多规格球阀', exact: true }).click();
  await page.getByRole('tab', { name: '规格与价格', exact: true }).click();
  await page.getByLabel('单价（元） 1', { exact: true }).fill('0');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  assert.equal((await query({ attributes: attrs })).matches[0].prices[0].amount, '0');
  // Preserve existing tiers on unrelated edits, then explicitly replace with one amount.
  product = (await (await page.request.get(`${base}/api/products/${product.id}`)).json()).product;
  product.catalog.variants[0].prices = [
    { amount: '1280.00', minQty: '1', maxQty: '9' },
    { amount: '1200.00', minQty: '10' },
  ];
  const seed = await page.request.put(`${base}/api/products/${product.id}`, { data: product }); assert.equal(seed.status(), 200);
  await page.getByRole('button', { name: '查看 / 编辑 多规格球阀', exact: true }).click();
  await page.getByRole('tab', { name: '规格与价格', exact: true }).click();
  await page.getByText('旧价格含多条记录或附加条件，重新填写单价后将统一为该金额。', { exact: true }).waitFor();
  assert.equal(await page.getByLabel('单价（元） 1', { exact: true }).inputValue(), '');
  await page.getByLabel('规格编号 / SKU 1', { exact: true }).fill('BV-F50-UPDATED');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  product = (await (await page.request.get(`${base}/api/products/${product.id}`)).json()).product;
  assert.equal(product.catalog.variants[0].prices.length, 2);
  assert.equal((await query({ attributes: attrs })).status, 'needs_clarification');
  await page.getByRole('button', { name: '查看 / 编辑 多规格球阀', exact: true }).click();
  await page.getByRole('tab', { name: '规格与价格', exact: true }).click();
  await page.getByLabel('单价（元） 1', { exact: true }).fill('1250.00');
  await page.screenshot({ path: '/tmp/inbrix-product-pricing-desktop.png', animations: 'disabled' });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.screenshot({ path: '/tmp/inbrix-product-pricing-mobile.png', animations: 'disabled' });
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  product = (await (await page.request.get(`${base}/api/products/${product.id}`)).json()).product;
  assert.equal(product.catalog.variants[0].prices.length, 1);
  for (const qty of ['1', '10', '100']) {
    const result = await query({ attributes: attrs, qty });
    assert.equal(result.status, 'matched'); assert.equal(result.matches[0].prices[0].amount, '1250.00');
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  const pending = await (await page.request.get(`${base}/api/product-price-imports`)).json();
  assert.equal(pending.total, 1);
  await page.goto(`${base}/price-lists`);
  await page.getByRole('dialog').getByRole('heading', { name: '待关联价格 (1)', exact: true }).waitFor();
  await page.getByRole('button', { name: /待关联球阀/ }).click();
  await page.getByRole('button', { name: '确认关联', exact: true }).click();
  await page.getByText('没有待关联价格', { exact: true }).waitFor();
  assert.equal((await (await page.request.get(`${base}/api/product-price-imports`)).json()).total, 0);
  const legacy = await (await page.request.get(`${base}/api/products?q=LEGACY-50`)).json(); assert.equal(legacy.total, 1);
  const duplicate = await page.request.post(`${base}/api/product-price-imports/${pending.rows[0].rowId}`, { data: {} }); assert.equal(duplicate.status(), 400);
  await page.keyboard.press('Escape');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  // Configuration references survive quotation and contract save/re-read without requiring an image.
  const references = await page.evaluate(async product => {
    const { convertGeneratedToStoredDocument } = await import('/src/lib/ai-document-storage.ts');
    const { collectProductImageValues } = await import('/src/components/app/product-document-images.ts');
    const { createSpiraxContractCanvasDocument } = await import('/src/components/app/spirax-contract-canvas.ts');
    const contract = createSpiraxContractCanvasDocument('', '', { total_amount: '' }, [{ model: 'BV', description: 'Pending tax terms', qty: '1', price: '10', amount: '10' }]);
    const fields = [];
    function inspect(elements) { for (const element of elements) { if (element.control?.conceptId === 'total_amount') fields.push(element.control.value); for (const row of element.trList || []) for (const cell of row.tdList) inspect(cell.value || []); } }
    inspect(contract);
    if (fields.length !== 1 || fields[0] !== null) throw new Error('Unknown contract total was silently filled from a line amount');
    const v = product.catalog.variants[0], price = v.prices[0];
    return ['quotation', 'contract'].map(type => {
      const doc = convertGeneratedToStoredDocument({ id: `catalog-${type}`, type, name: type, items: [{ productId: product.id, productVersion: product.version, variantId: v.id, priceId: price.id, model: v.sku, description: '法兰 DN50 304', qty: '10', price: price.amount, amount: '12000.00' }] });
      const values = {}; collectProductImageValues(JSON.parse(doc.html.slice('__INBRIX_CANVAS_DOCUMENT__:'.length)).data.main, values);
      return values;
    });
  }, product);
  assert(references.every(v => v.item_model === product.catalog.variants[0].sku && v.item_variant_id === product.catalog.variants[0].id && v.item_price_id === product.catalog.variants[0].prices[0].id));
  assert.deepEqual(errors, []);
  console.log('Single-price CRUD, blank/zero prices, legacy price preservation/replacement, removed CSV/conditions, legacy linking and document references passed.');
} finally { await browser.close(); }
