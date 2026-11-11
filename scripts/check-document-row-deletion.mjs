import assert from 'node:assert/strict';
import { chromium } from 'playwright';

// Uses the disposable database in product-library-harness.go, never user data.
const base = process.env.DOCUMENT_TEST_URL || 'http://127.0.0.1:3097';
const prefix = '__INBRIX_CANVAS_DOCUMENT__:';
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
page.on('pageerror', error => errors.push(error.message));
const snapshot = () => page.evaluate(() => JSON.parse(window.rowEditor.current.getDocument().split('__INBRIX_CANVAS_DOCUMENT__:')[1]));
const mount = async (kind, html) => {
  await page.evaluate(async ({ kind, html }) => {
    const source = await (await fetch('/src/components/app/document-editor-dialog.tsx')).text();
    const main = await (await fetch('/src/main.tsx')).text();
    const specifier = (text, name) => text.match(new RegExp(`from ["']([^"']*${name}\\.js[^"']*)["']`))[1];
    const React = (await import(specifier(source, 'react'))).default;
    const { createRoot } = (await import(specifier(main, 'react-dom_client'))).default;
    const { CanvasDocumentEditor } = await import('/src/components/app/canvas-document-editor.tsx');
    const { convertGeneratedToStoredDocument } = await import('/src/lib/ai-document-storage.ts');
    window.rowRoot?.unmount();
    document.querySelector('#row-test')?.remove();
    const host = document.createElement('div'); host.id = 'row-test'; document.body.append(host);
    const stored = html || convertGeneratedToStoredDocument({ type: kind, name: 'Row deletion', values: {
      currency: 'USD', discount: '0', inland_freight: '0', say_total: 'STALE WORDS',
    }, items: ['A', 'B', 'C'].map((model, i) => ({ model, description: `Valve ${model}`, qty: String(i + 1), price: '10', amount: String((i + 1) * 10) })) }).html;
    window.rowEditor = React.createRef(); window.rowReady = false;
    window.rowRoot = createRoot(host);
    window.rowRoot.render(React.createElement(CanvasDocumentEditor, { ref: window.rowEditor, initialHTML: stored, locale: 'en', onReady: () => { window.rowReady = true; } }));
  }, { kind, html });
  await page.waitForFunction(() => window.rowReady);
  // Reach the native editor only from this test, without production debug APIs.
  await page.evaluate(() => {
    const host = document.querySelector('#row-test .canvas-document-editor');
    let fiber = host[Object.keys(host).find(key => key.startsWith('__reactFiber$'))];
    for (; fiber; fiber = fiber.return) {
      for (let hook = fiber.memoizedState; hook; hook = hook.next) {
        if (hook.memoizedState?.current?.command?.executeDeleteTableRow) {
          window.nativeRowEditor = hook.memoizedState.current; return;
        }
      }
    }
    throw new Error('Missing native canvas editor');
  });
};
const remove = async index => {
  await page.evaluate(index => {
    const editor = window.nativeRowEditor;
    const control = editor.command.getControlList().find(element => element.control?.conceptId === `item_qty_${index}`);
    if (!control) throw new Error('Missing row control');
    editor.command.executeLocationControl(control.controlId, { position: 'after' });
    editor.command.executeDeleteTableRow();
  }, index);
};
try {
  await page.goto(base);
  for (const kind of ['contract', 'quotation']) {
    for (const index of [0, 1, 2]) {
      await mount(kind);
      await page.waitForFunction(() => window.rowEditor.current.getDocumentData().values.total_amount === '60.00');
      const original = await snapshot();
      if (kind === 'contract') {
        assert(JSON.stringify(original.data).includes('Please designate Standard Chartered Bank, USA as the intermediary bank. Include \\"DO NOT CONVERT\\" in the payment remarks.'), 'Invoice must include fixed English remittance instructions');
      }
      await remove(index);
      const deleted = await snapshot();
      const expected = original.business.items.filter((_, i) => i !== index);
      assert.deepEqual(deleted.business.items.map(line => line.lineId), expected.map(line => line.lineId));
      assert.equal(deleted.business.values.say_total, '', `Deleting a line invalidates amount words: ${kind}/${index}`);
      await page.waitForFunction(total => window.rowEditor.current.getDocumentData().values.total_amount === total, `${60 - (index + 1) * 10}.00`);
      const restored = await page.evaluate(async ({ original, deleted }) => {
        const { captureDocumentBusiness } = await import('/src/lib/document-business.ts');
        const history = new Map();
        captureDocumentBusiness(deleted.data, original.business, history);
        return captureDocumentBusiness(original.data, deleted.business, history).items;
      }, { original, deleted });
      assert.deepEqual(restored, original.business.items, 'Restored controls retain line data and identity');

      // Save/reload the actual template, then use that template for a document.
      const reopened = await page.evaluate(async kind => {
        const { saveDocumentTemplate, listDocumentTemplates, saveDocument, getDocument } = await import('/src/lib/api.ts');
        const html = window.rowEditor.current.getDocument();
        const { template } = await saveDocumentTemplate({ type: kind, name: `Row deletion ${crypto.randomUUID()}`, html });
        const { templates } = await listDocumentTemplates();
        const loaded = templates.find(record => record.id === template.id);
        const { document } = await saveDocument({ type: kind, name: template.name, html: loaded.html });
        return { template: loaded.html, document: (await getDocument(document.id)).document.html };
      }, kind);
      for (const html of [reopened.template, reopened.document]) {
        await mount(kind, html);
        assert.deepEqual((await snapshot()).business.items, expected, 'Save/reopen must not restore or shift deleted lines');
        assert.deepEqual(await page.evaluate(() => window.rowEditor.current.getDocumentData().items), expected);
      }
    }
    await mount(kind);
    for (const index of [2, 1, 0]) await remove(index);
    assert.deepEqual((await snapshot()).business.items, [], 'Removing all rows leaves an empty list');
    const empty = await page.evaluate(async kind => {
      const { saveDocument } = await import('/src/lib/api.ts');
      return (await saveDocument({ type: kind, name: 'Empty row deletion', html: window.rowEditor.current.getDocument() })).document.html;
    }, kind);
    await mount(kind, empty);
    assert.deepEqual((await snapshot()).business.items, []);
  }
  assert.deepEqual(errors, []);
  console.log('PASS: invoice/quotation row deletion, first/middle/last/all rows, automatic totals, stable line identities, template persistence and document save/reopen.');
} finally { await browser.close(); }
