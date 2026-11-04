import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { unzipSync, strFromU8 } from 'fflate';
import { readFile } from 'node:fs/promises';

// Mount the real editor against synthetic documents; no production data is read.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3200';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1200, height: 1200 } });
page.setDefaultTimeout(15000);
const errors = [];
page.on('pageerror', error => errors.push(error.message));
const entry = await (await page.request.get(`${base}/src/main.tsx`)).text();
const dependency = name => entry.match(new RegExp(`from "([^"]*/${name}\\.js[^\"]*)"`))?.[1];
assert.ok(dependency('react') && dependency('react-dom_client'), 'Run this check against the Vite development server');
await page.route('**/stamp-regression', route => route.fulfill({ contentType: 'text/html', body: `
  <div id="root"></div>
  <script type="module">
    import RefreshRuntime from '/@react-refresh';
    RefreshRuntime.injectIntoGlobalHook(window);
    window.$RefreshReg$ = () => {};
    window.$RefreshSig$ = () => type => type;
    window.__vite_plugin_react_preamble_installed__ = true;
    const { default: React } = await import(${JSON.stringify(dependency('react'))});
    const { default: { createRoot } } = await import(${JSON.stringify(dependency('react-dom_client'))});
    const { CanvasDocumentEditor } = await import('/src/components/app/canvas-document-editor.tsx');
    window.stampEditor = React.createRef();
    createRoot(document.getElementById('root')).render(React.createElement(CanvasDocumentEditor, {
      ref: window.stampEditor, locale: 'zh-CN', initialHTML: '<div data-spirax-contract="reference-v1"></div>',
    }));
  </script>` }));
await page.route('**/csrf', route => route.fulfill({ json: { token: 'test' } }));
await page.route('**/api/documents/validate', route => route.fulfill({ json: {
  html: route.request().postDataJSON().html,
  validation: { status: 'draft', issues: [] },
} }));

try {
  await page.goto(`${base}/stamp-regression`);
  await page.waitForFunction(() => window.stampEditor?.current).catch(error => { console.error(errors); throw error; });
  const result = await page.evaluate(() => {
    const host = document.querySelector('.canvas-document-editor');
    let fiber = host[Object.keys(host).find(key => key.startsWith('__reactFiber$'))];
    for (; fiber; fiber = fiber.return) {
      for (let hook = fiber.memoizedState; hook; hook = hook.next) {
        const native = hook.memoizedState?.current;
        if (native?.command?.executeLocationControl) window.nativeStampEditor = native;
      }
    }
    const native = window.nativeStampEditor;
    if (!native) throw new Error('Missing native canvas editor');
    const canvas = document.createElement('canvas');
    canvas.width = canvas.height = 128;
    const ctx = canvas.getContext('2d');
    ctx.strokeStyle = '#d00';
    ctx.lineWidth = 4;
    ctx.strokeRect(4, 4, 120, 120);
    const stamp = { id: 'regression-stamp', name: 'Test stamp', value: canvas.toDataURL('image/png'), width: 128, height: 128 };
    window.testStamp = stamp;
    const control = native.command.getControlList().find(element => element.control?.conceptId === 'seller_sign_company');
    native.command.executeLocationControl(control.controlId, { position: 'after' });
    const editor = window.stampEditor.current;
    const before = editor.getDocumentData().values.seller_sign_company;
    editor.insertStamp(stamp, 100);
    const snapshot = JSON.parse(editor.getDocument().split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
    window.inspectStamps = () => {
      const doc = JSON.parse(editor.getDocument().split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
      const stamps = [], textSources = [], fields = [];
      const walk = node => {
        if (Array.isArray(node)) { node.forEach(walk); return; }
        if (!node || typeof node !== 'object') return;
        if (node.type === 'image' && node.extension?.stampId) stamps.push(node);
        if ((!node.type || node.type === 'text') && typeof node.value === 'string' && node.value.includes('data:image/')) textSources.push(node.value);
        if (node.control?.conceptId) fields.push(node.control);
        for (const key of ['main', 'header', 'footer', 'valueList', 'trList', 'tdList', 'value', 'control']) walk(node[key]);
      };
      walk(doc.data);
      return { stamps, textSources, fields, business: doc.business, pages: document.querySelectorAll('.ce-page-container > canvas').length,
        controls: editor.getControls(), dataControls: editor.getDocumentData().controls };
    };
    return { before, after: snapshot.business.values.seller_sign_company };
  });
  assert.equal(result.after, result.before, 'Inserting a stamp into a field must not append its Base64 to business text');
  const original = await page.evaluate(() => window.inspectStamps());
  assert.equal(original.stamps.length, 1, 'Inserted stamp must remain an image');
  const verify = async (expected, count = 1) => {
    const state = await page.evaluate(() => window.inspectStamps());
    assert.equal(state.business.values.seller_sign_company, expected);
    assert.equal(state.stamps.length, count, 'Field synchronization must preserve stamp images');
    assert.deepEqual(state.textSources, [], 'Image source must never appear in text runs');
    assert(!JSON.stringify(state.business).includes('data:image/'), 'Business fields must exclude image sources');
    assert(!JSON.stringify([state.controls, state.dataControls]).includes('data:image/'), 'Variable forms must exclude image sources');
    assert(state.pages <= original.pages + 1, 'Stamp must not create runaway pagination');
    assert.equal(state.stamps[0].value, original.stamps[0].value);
    assert.equal(state.stamps[0].imgDisplay, original.stamps[0].imgDisplay);
    assert.equal(state.stamps[0].width, original.stamps[0].width);
    assert.equal(state.stamps[0].height, original.stamps[0].height);
  };
  await verify(result.before);
  for (let i = 0; i < 3; i++) {
    await page.evaluate(() => window.stampEditor.current.setHTML(window.stampEditor.current.getDocument()));
    await verify(result.before);
  }
  await page.evaluate(() => window.stampEditor.current.setControlValues([{ conceptId: 'seller_sign_company', value: 'Updated Seller' }]));
  await verify('Updated Seller');
  // A control with an image before its first text must not lend the image's
  // type or metadata to the projected text.
  await page.evaluate(() => {
    const editor = window.stampEditor.current;
    const doc = JSON.parse(editor.getDocument().split('__INBRIX_CANVAS_DOCUMENT__:')[1]);
    const walk = node => {
      if (Array.isArray(node)) { node.forEach(walk); return; }
      if (!node || typeof node !== 'object') return;
      if (node.control?.conceptId === 'seller_sign_company') node.control.value.sort((a, b) => Number(b.type === 'image') - Number(a.type === 'image'));
      for (const key of ['main', 'trList', 'tdList', 'value']) walk(node[key]);
    };
    walk(doc.data);
    doc.business.values.seller_sign_company = 'Projected Seller';
    editor.setHTML('__INBRIX_CANVAS_DOCUMENT__:' + JSON.stringify(doc));
  });
  await verify('Projected Seller');
  // The contract repeats buyer_company; each occurrence must keep its own
  // media when the shared business field changes.
  const repeated = await page.evaluate(() => {
    const native = window.nativeStampEditor;
    const controls = native.command.getControlList().filter(element => element.control?.conceptId === 'buyer_company');
    native.command.executeLocationControl(controls.at(-1).controlId, { position: 'after' });
    window.stampEditor.current.insertStamp({ ...window.testStamp, id: 'buyer-stamp' }, 80);
    window.stampEditor.current.setControlValues([{ conceptId: 'buyer_company', value: 'Updated Buyer' }]);
    return window.inspectStamps();
  });
  assert.equal(repeated.fields.filter(control => control.conceptId === 'buyer_company' && control.value.some(run => run.type === 'image')).length, 1,
    'Shared fields must not duplicate the stamp into every occurrence');
  await verify('Projected Seller', 2);
  await page.evaluate(() => window.stampEditor.current.setHTML(window.stampEditor.current.getDocument()));
  await verify('Projected Seller', 2);
  const exported = await page.evaluate(async () => {
    const images = await window.stampEditor.current.getPageImages();
    let redPixels = 0;
    for (const source of images) {
      const image = new Image();
      image.src = source;
      await image.decode();
      const canvas = document.createElement('canvas');
      canvas.width = image.width; canvas.height = image.height;
      const ctx = canvas.getContext('2d');
      ctx.drawImage(image, 0, 0);
      const { data } = ctx.getImageData(0, 0, canvas.width, canvas.height);
      for (let i = 0; i < data.length; i += 4) if (data[i] > 150 && data[i + 1] < 80 && data[i + 2] < 80) redPixels++;
    }
    return { pages: images.length, redPixels };
  });
  assert(exported.redPixels > 200, 'Exported page images must visibly contain the red stamps');
  assert(exported.pages <= original.pages + 1);
  const downloading = page.waitForEvent('download');
  await page.evaluate(() => window.stampEditor.current.exportDocx('stamp-regression'));
  const download = await downloading;
  const files = unzipSync(await readFile(await download.path()));
  const xml = strFromU8(files['word/document.xml']);
  assert(!xml.includes('data:image/'), 'DOCX text must not contain image sources');
  assert(xml.includes('Projected Seller') && xml.includes('Updated Buyer'));
  assert(Object.entries(files).some(([name, bytes]) => name.startsWith('word/media/') && Buffer.from(bytes).equals(Buffer.from(original.stamps[0].value.split(',')[1], 'base64'))),
    'DOCX must embed the original stamp as image media');
  await page.screenshot({ path: '/tmp/inbrix-stamp-regression.png', fullPage: true });
  const quotation = await page.evaluate(() => {
    const editor = window.stampEditor.current;
    const native = window.nativeStampEditor;
    editor.setHTML('<div data-spirax-quotation="reference-v1"></div>');
    for (const conceptId of ['seller_company', 'customer_company']) {
      const control = native.command.getControlList().find(element => element.control?.conceptId === conceptId);
      native.command.executeLocationControl(control.controlId, { position: 'after' });
      editor.insertStamp({ ...window.testStamp, id: conceptId }, 80);
    }
    const before = window.inspectStamps();
    editor.setControlValues([{ conceptId: 'customer_company', value: 'Quotation Buyer' }]);
    editor.setHTML(editor.getDocument());
    return { before, after: window.inspectStamps() };
  });
  assert.equal(quotation.before.business.values.customer_company, '', 'An image-only field must remain empty business text');
  assert.equal(quotation.after.business.values.customer_company, 'Quotation Buyer');
  assert.equal(quotation.before.stamps.length, 2);
  assert.equal(quotation.after.stamps.length, 2, 'Quotation stamps must survive text edits and reopen');
  assert.deepEqual(quotation.after.textSources, []);
  assert(quotation.after.pages <= quotation.before.pages + 1);
  assert.deepEqual(errors, []);
  console.log('PASS: contract and quotation stamps, empty fields, repeated reopen, field updates, image-first runs, repeated fields, bounded pagination, visible page-image export and DOCX image media.');
} finally {
  await browser.close();
}
