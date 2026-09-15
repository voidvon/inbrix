import { chromium } from 'playwright';
import { unzipSync, strFromU8 } from 'fflate';
import assert from 'node:assert/strict';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1600, height: 1100 } });
page.on('pageerror', error => console.error(error.stack));
page.on('console', message => { if (message.type() === 'error') console.error(message.text()); });
const artifacts = await mkdtemp(join(tmpdir(), 'inbrix-docx-'));
const summary = { id: 'docx-thread', subject: 'DOCX test', peerEmail: 'alice@example.com', preview: 'Please quote', date: new Date().toISOString(), count: 1, unreadCount: 0, status: 'unanswered' };
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test-token' } }));
await page.route('**/v1/**', route => route.fulfill({ json: { signatures: [], models: [], agents: [], bindings: [] } }));
await page.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname;
  const json = path === '/api/conversations' ? { locale: 'zh-CN', conversations: [summary], folders: [], accounts: [], accountEmail: 'test@example.com' }
    : path === '/api/conversations/docx-thread' ? { conversation: { ...summary, accountEmail: 'test@example.com', messages: [{ id: '1', folder: 'INBOX', from: 'alice@example.com', to: 'test@example.com', subject: summary.subject, body: summary.preview, preview: summary.preview, date: summary.date, outgoing: false, flags: [] }] } }
    : path.includes('signatures') ? { signatures: [] } : { models: [], agents: [], bindings: [], accounts: [], conversations: [], folders: [] };
  return route.fulfill({ json });
});
page.on('dialog', dialog => dialog.accept());
try {
  await page.goto(`${process.env.DOCUMENT_TEST_URL || 'http://localhost:2342'}/inbox?conversation=docx-thread`);
  await page.getByTestId('conversation-detail').getByText('Please quote', { exact: true }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: '回复', exact: true }).click();
  await page.getByTestId('compose-dialog').getByRole('button', { name: '新建报价单', exact: true }).click();
  const editor = page.getByTestId('document-editor-dialog');
  await editor.locator('canvas').first().waitFor();
  console.log('Editor bounds', await editor.evaluate(e => ({ rect: e.getBoundingClientRect().toJSON(), translate: getComputedStyle(e).translate, top: getComputedStyle(e).top, left: getComputedStyle(e).left })));
  await editor.getByRole('textbox', { name: '文档名称', exact: true }).fill('Word round trip');
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  const saved = () => page.evaluate(() => JSON.parse(localStorage.getItem('inbrix-documents') || '[]').find(d => d.name === 'Word round trip'));
  const original = await saved();
  const number = original.html.match(/SP-\d{8}-[A-F0-9]{8}/)[0];
  await editor.getByRole('button', { name: '导出', exact: true }).click();
  const downloadReady = page.waitForEvent('download', { timeout: 30000 });
  await page.getByRole('menuitem', { name: '下载 Word（DOCX）', exact: true }).click();
  const download = await downloadReady;
  assert.equal(download.suggestedFilename(), 'Word round trip.docx');
  const path = join(artifacts, download.suggestedFilename());
  await download.saveAs(path);
  const files = unzipSync(await readFile(path));
  const xml = strFromU8(files['word/document.xml']);
  assert.ok(xml.includes(number) && xml.includes('Customer Information') && xml.includes('SP400'));
  assert.ok(xml.includes('<w:tbl>'), 'DOCX must contain editable tables');
  const media = Object.entries(files).filter(([name]) => name.startsWith('word/media/') && !name.endsWith('/'));
  assert.ok(media.length >= 5, 'Images must be embedded inside the DOCX');
  for (const [name, bytes] of media) assert.deepEqual([...bytes.slice(0, 8)], [137, 80, 78, 71, 13, 10, 26, 10], `Valid PNG payload: ${name}`);
  assert.ok(Object.keys(files).some(name => /^word\/header\d+\.xml$/.test(name) && strFromU8(files[name]).includes('behindDoc="1"')), 'Page artwork must be embedded behind the text');
  await editor.getByTestId('document-docx-input').setInputFiles(path);
  await page.getByText('Word 文档已导入，请检查排版后保存。', { exact: true }).waitFor();
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  const imported = await saved();
  assert.ok(imported.html.includes(number) && imported.html.includes('SP400') && imported.html.includes('data:image/'));
  const payload = JSON.parse(imported.html.slice('__INBRIX_CANVAS_DOCUMENT__:'.length));
  console.log('Imported page artwork', { width: payload.options.width, height: payload.options.height, watermark: { ...payload.options.watermark, data: payload.options.watermark?.data?.slice(0, 25) }, background: payload.options.background?.image?.slice(0, 25) });
  assert.equal(payload.template, 'document', 'Imported documents must not depend on the built-in quotation template');
  assert.ok(payload.options.width && payload.options.margins, 'Imported page settings must persist');
  await editor.screenshot({ path: join(artifacts, 'imported.png') });
  await editor.getByTestId('document-docx-input').setInputFiles({ name: 'broken.docx', mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', buffer: Buffer.from('not a zip') });
  await page.getByText('导入失败，请检查文件是否为有效的 DOCX 文档。当前内容已保留。', { exact: true }).waitFor();
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  assert.equal((await saved()).html, imported.html, 'Failed imports must preserve current contents');
  console.log(`PASS: editable DOCX, embedded images/background, reimport with number/page settings, failed-import preservation. Artifacts: ${artifacts}`);
} finally {
  await browser.close();
}
