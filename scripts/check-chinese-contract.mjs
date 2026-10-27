import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { chromium } from 'playwright';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
const errors = [];
page.on('pageerror', error => errors.push(error.message));
try {
 await page.goto('http://127.0.0.1:3219/documents');
 const result = await page.evaluate(async () => {
  const api = await import('/src/lib/api.ts');
  const { templates } = await api.listDocumentTemplates('zh-CN');
  const template = templates.find(t => t.name === '中文购销合同模板');
  if (!template) throw new Error('Chinese template missing');
  const prefix = '__INBRIX_CANVAS_DOCUMENT__:';
  const doc = JSON.parse(template.html.slice(prefix.length));
  Object.assign(doc.business.values, {customer_company:'上海示例采购有限公司',seller_company:'上海示例设备有限公司',contract_number:'HT-20261027-001',contract_date:'2026-10-27',payment_terms:'合同签订后支付30%预付款，验收合格后支付余款。',lead_time:'收到预付款后30日内交货',tax_rate:'13%',tax_mode:'excluded',delivery_address:'买方指定收货地址',acceptance_terms:'到货后按双方确认的技术文件验收。',warranty_terms:'验收合格后12个月。'});
  doc.business.items[0] = {...doc.business.items[0], model:'DN50',description:'蒸汽调节阀，法兰连接',qty:'2',price:'1000'};
  const saved = await api.saveDocument({type:'contract',name:'中文购销合同模板预览',html:prefix+JSON.stringify(doc)});
  return {saved,templateId:template.id};
 });
 assert.equal(result.saved.validation.status,'ready');
 const doc=JSON.parse(result.saved.document.html.split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
 assert.equal(doc.business.calculated.total_amount,'2260.00');
 assert(!result.saved.document.html.includes('Banking Circle'));
 await page.goto(`http://127.0.0.1:3219/documents/${result.saved.document.id}`);
 await page.getByTestId('document-editor-dialog').locator('canvas').first().waitFor();
 await page.waitForTimeout(700);
 const canvases=page.getByTestId('document-editor-dialog').locator('.ce-page-container canvas');
 console.log('Pages:',await canvases.count());
 await page.screenshot({path:'/tmp/inbrix-chinese-contract-preview.png'});
 const pages=page.locator('.ce-page-container canvas');
 assert.equal(await pages.count(),2);
 for (let i=0;i<2;i++) {
  await pages.nth(i).evaluate(canvas => canvas.scrollIntoView({block:'center'}));
  await page.waitForTimeout(300);
  const image=await pages.nth(i).evaluate(canvas=>canvas.toDataURL('image/png'));
  await writeFile(`/tmp/inbrix-chinese-contract-page-${i+1}.png`,Buffer.from(image.split(',')[1],'base64'));
 }
 assert.deepEqual(errors,[]);
 console.log('Chinese template seeding, canonical snapshot, contract calculation, and browser rendering passed.');
} finally {await browser.close();}
